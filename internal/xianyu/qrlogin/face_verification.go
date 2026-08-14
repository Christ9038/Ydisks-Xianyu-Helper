package qrlogin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"xianyu-go/internal/logsafe"
	"xianyu-go/internal/xianyu/cookierefresh"
)

// runFaceVerification 参照闲鱼浏览器端风控跳转链，纯 HTTP 复现人脸验证流程。
//
// 关键点：必须用同一个 Cookie Jar 贯穿 normal_validate -> verify_modes ->
// identity_verify -> check.do -> ivCheckLogin。服务端会在跳转链路中写入身份
// 锚点 Cookie；如果拆成多个无状态请求，最终通常拿不到 unb。
// runFaceVerification 负责运行FaceVerification相关处理。
func (m *Manager) runFaceVerification(ctx context.Context, sessionID, iframeURL string) error {
	m.mu.Lock()
	// sess 保存sess，供当前处理流程使用
	sess := m.sessions[sessionID]
	if sess == nil {
		m.mu.Unlock()
		return fmt.Errorf("会话不存在")
	}
	m.mu.Unlock()
	// state 保存状态，供当前处理流程使用
	state := sess.snapshot()
	// initialCookies 保存initialCookies，供当前处理流程使用
	initialCookies := state.cookies
	if len(initialCookies) == 0 {
		return fmt.Errorf("无扫码临时 cookie")
	}

	// client、jar、err 保存client、jar、err，供当前处理流程使用
	client, jar, err := m.faceHTTPClient(initialCookies, state.cookieSnapshot, iframeURL)
	if err != nil {
		return err
	}

	// normalHTML、err 保存normalHTML、err，供当前处理流程使用
	normalHTML, err := m.faceGetHTML(ctx, client, iframeURL, "")
	if err != nil {
		return fmt.Errorf("请求 normal_validate: %w", err)
	}
	// htoken、err 保存htoken、err，供当前处理流程使用
	htoken, err := extractFaceHToken(normalHTML)
	if err != nil {
		return err
	}
	// verifyModesURL、err 保存verifyModesURL、err，供当前处理流程使用
	verifyModesURL, err := extractVerifyModesURL(normalHTML)
	if err != nil {
		return err
	}

	// identityHTML、err 保存identityHTML、err，供当前处理流程使用
	identityHTML, err := m.faceGetHTML(ctx, client, verifyModesURL, "")
	if err != nil {
		return fmt.Errorf("请求 verify_modes: %w", err)
	}
	// faceContent、err 保存faceContent、err，供当前处理流程使用
	faceContent, err := extractFaceQRCodeContent(identityHTML)
	if err != nil {
		return err
	}
	// faceQRURL、err 保存faceQRURL、err，供当前处理流程使用
	faceQRURL, err := renderQRDataURL(faceContent)
	if err != nil {
		return fmt.Errorf("渲染人脸二维码: %w", err)
	}

	m.mu.Lock()
	// s 保存s，供当前处理流程使用
	s := m.sessions[sessionID]
	m.mu.Unlock()
	if s != nil {
		s.mu.Lock()
		s.faceQRContent = faceContent
		s.faceQRURL = faceQRURL
		s.Status = "verification_required"
		s.mu.Unlock()
	}
	m.logger.Info("人脸验证二维码已生成，等待用户手机扫码", "session_id", sessionID)

	// ivCheckURL、err 保存ivCheckURL、err，供当前处理流程使用
	ivCheckURL, err := m.waitFaceVerification(ctx, client, sessionID, htoken)
	if err != nil {
		return err
	}
	if // err 保存err，供当前处理流程使用
	_, err := m.faceGetHTML(ctx, client, ivCheckURL, faceIdentityReferer(htoken)); err != nil {
		return fmt.Errorf("请求 ivCheckLogin: %w", err)
	}

	// finalCookies 保存finalCookies，供当前处理流程使用
	finalCookies := collectJarCookies(jar, mustParseURL(qrVerifyTargetURL))
	if finalCookies["unb"] == "" {
		return fmt.Errorf("人脸验证完成但未获取到 unb")
	}
	// finalSnapshot、snapshotComplete 保存finalSnapshot、snapshotComplete，供当前处理流程使用
	finalSnapshot, snapshotComplete := jar.Snapshot()

	m.mu.Lock()
	s = m.sessions[sessionID]
	m.mu.Unlock()
	if s != nil {
		s.mu.Lock()
		s.cookies = finalCookies
		if snapshotComplete {
			s.cookieSnapshot = finalSnapshot
		} else {
			s.cookieSnapshot = nil
		}
		s.unb = finalCookies["unb"]
		s.Status = "success"
		s.mu.Unlock()
	}
	m.logger.Info("人脸验证登录成功", "session_id", sessionID, "account_hash", logsafe.ID(finalCookies["unb"]))
	return nil
}

// faceHTTPClient 负责faceHTTPClient相关处理。
func (m *Manager) faceHTTPClient(cookies map[string]string, snapshot []cookierefresh.BrowserCookie, seedURLs ...string) (*http.Client, *faceCookieJar, error) {
	// jar 保存jar，供当前处理流程使用
	jar := newFaceCookieJar(cookies, snapshot)
	if snapshot == nil {
		setJarCookies(jar, mustParseURL(host), cookies)
		// rawURL 表示当前遍历过程中的原始URL
		for _, rawURL := range seedURLs {
			if // u、err 保存u、err，供当前处理流程使用
			u, err := url.Parse(rawURL); err == nil {
				setJarCookies(jar, u, cookies)
			}
		}
	}
	// hc 保存hc，供当前处理流程使用
	hc := *m.httpc
	hc.Jar = jar
	return &hc, jar, nil
}

// faceCookieJar 是人脸验证与登录凭证换取专用的 Go Cookie Jar。标准库
// cookiejar 能正确发送 Cookie，但没有导出完整 Jar 的接口；这个实现直接以
// BrowserCookie 为权威状态，使自动重定向中的每个 Set-Cookie 都能原样进入
// 最终持久化快照。
// faceCookieJar 保存face登录凭证Jar，供当前处理流程使用
type faceCookieJar struct {
	mu            sync.Mutex
	snapshot      []cookierefresh.BrowserCookie
	authoritative bool
}

// newFaceCookieJar 负责newFace登录凭证Jar相关处理。
func newFaceCookieJar(cookies map[string]string, snapshot []cookierefresh.BrowserCookie) *faceCookieJar {
	// jar 保存jar，供当前处理流程使用
	jar := &faceCookieJar{authoritative: snapshot != nil}
	if snapshot != nil {
		jar.snapshot = cookierefresh.NormalizeSnapshot(snapshot)
		if jar.snapshot == nil {
			jar.snapshot = []cookierefresh.BrowserCookie{}
		}
		return jar
	}
	// 只有历史/异常会话才会走这里。推断快照仅用于维持 HTTP 会话，
	// authoritative=false 保证调用方不会把推断属性持久化成完整 Jar。
	jar.snapshot = cookierefresh.SnapshotFromCookieString(cookieMarshal(cookies), ".goofish.com")
	return jar
}

// Cookies 负责Cookies相关处理。
func (j *faceCookieJar) Cookies(u *url.URL) []*http.Cookie {
	if j == nil || u == nil {
		return nil
	}
	j.mu.Lock()
	// header 保存header，供当前处理流程使用
	header, _ := cookierefresh.ScopedCookieHeaderForRequest(j.snapshot, u.String(), qrTopSite, time.Now())
	j.mu.Unlock()
	if header == "" {
		return nil
	}
	// parts 保存parts，供当前处理流程使用
	parts := strings.Split(header, ";")
	// out 保存out，供当前处理流程使用
	out := make([]*http.Cookie, 0, len(parts))
	// part 表示当前遍历过程中的part
	for _, part := range parts {
		// name、value、ok 保存name、value、ok，供当前处理流程使用
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || name == "" {
			continue
		}
		out = append(out, &http.Cookie{Name: name, Value: value})
	}
	return out
}

// SetCookies 设置Cookies。
func (j *faceCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if j == nil || u == nil || len(cookies) == 0 {
		return
	}
	// raw 保存原始，供当前处理流程使用
	raw := make([]string, 0, len(cookies))
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range cookies {
		if cookie == nil || strings.TrimSpace(cookie.Name) == "" {
			continue
		}
		// line 保存line，供当前处理流程使用
		line := strings.TrimSpace(cookie.Raw)
		if line == "" {
			line = cookie.String()
		}
		if line != "" {
			raw = append(raw, line)
		}
	}
	if len(raw) == 0 {
		return
	}
	j.mu.Lock()
	j.snapshot = cookierefresh.ApplySetCookies(j.snapshot, u.String(), raw, time.Now(), qrTopSite)
	if j.snapshot == nil {
		j.snapshot = []cookierefresh.BrowserCookie{}
	}
	j.mu.Unlock()
}

// Snapshot 负责Snapshot相关处理。
func (j *faceCookieJar) Snapshot() ([]cookierefresh.BrowserCookie, bool) {
	if j == nil {
		return nil, false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.authoritative {
		return nil, false
	}
	// out 保存out，供当前处理流程使用
	out := cookierefresh.NormalizeSnapshot(j.snapshot)
	if out == nil {
		out = []cookierefresh.BrowserCookie{}
	}
	return out, true
}

// faceGetHTML 负责faceGetHTML相关处理。
func (m *Manager) faceGetHTML(ctx context.Context, client *http.Client, targetURL, referer string) (string, error) {
	// req、err 保存req、err，供当前处理流程使用
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", err
	}
	m.setHeaders(req)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	// body、err 保存body、err，供当前处理流程使用
	body, err := readQRBody(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// waitFaceVerification 负责waitFaceVerification相关处理。
func (m *Manager) waitFaceVerification(ctx context.Context, client *http.Client, sessionID, htoken string) (string, error) {
	// ticker 保存ticker，供当前处理流程使用
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		// sess 保存sess，供当前处理流程使用
		sess := m.sessions[sessionID]
		m.mu.Unlock()
		// expired 保存expired，供当前处理流程使用
		expired := sess == nil || sess.isExpired()
		if expired {
			return "", fmt.Errorf("人脸验证超时或会话已过期")
		}
		// ivCheckURL、done、err 保存ivCheckURL、done、err，供当前处理流程使用
		ivCheckURL, done, err := m.checkFaceVerification(ctx, client, htoken)
		if err != nil {
			m.logger.Warn("人脸验证轮询异常", "session_id", sessionID, "err", err)
		} else if done {
			return ivCheckURL, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}

// checkFaceVerification 负责checkFaceVerification相关处理。
func (m *Manager) checkFaceVerification(ctx context.Context, client *http.Client, htoken string) (ivCheckURL string, done bool, err error) {
	// req、err 保存req、err，供当前处理流程使用
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiFaceCheck, nil)
	if err != nil {
		return "", false, err
	}
	// q 保存q，供当前处理流程使用
	q := req.URL.Query()
	q.Set("htoken", htoken)
	req.URL.RawQuery = q.Encode()
	m.setHeaders(req)
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", faceIdentityReferer(htoken))
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	// body、err 保存body、err，供当前处理流程使用
	body, err := readQRBody(resp.Body)
	if err != nil {
		return "", false, err
	}
	// result 保存结果，供当前处理流程使用
	var result struct {
		Content struct {
			Code any    `json:"code"`
			URL  string `json:"url"`
		} `json:"content"`
	}
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(body, &result); err != nil {
		return "", false, fmt.Errorf("解析人脸验证状态失败: %w", err)
	}
	if fmt.Sprint(result.Content.Code) == "3" {
		return result.Content.URL, true, nil
	}
	return "", false, nil
}

// faceIdentityReferer 负责faceIdentityReferer相关处理。
func faceIdentityReferer(htoken string) string {
	return host + "/iv/mini/identity_verify.htm?htoken=" + url.QueryEscape(htoken)
}

// cloneCookieMap 负责clone登录凭证Map相关处理。
func cloneCookieMap(in map[string]string) map[string]string {
	// out 保存out，供当前处理流程使用
	out := make(map[string]string, len(in))
	// k、v 表示当前遍历过程中的k、v
	for k, v := range in {
		out[k] = v
	}
	return out
}

// setJarCookies 负责setJarCookies相关处理。
func setJarCookies(jar http.CookieJar, u *url.URL, cookies map[string]string) {
	if jar == nil || u == nil {
		return
	}
	// cs 保存cs，供当前处理流程使用
	cs := make([]*http.Cookie, 0, len(cookies))
	// k、v 表示当前遍历过程中的k、v
	for k, v := range cookies {
		cs = append(cs, &http.Cookie{Name: k, Value: v, Path: "/"})
	}
	jar.SetCookies(u, cs)
}

// collectJarCookies 负责collectJarCookies相关处理。
func collectJarCookies(jar http.CookieJar, urls ...*url.URL) map[string]string {
	// out 保存out，供当前处理流程使用
	out := make(map[string]string)
	if jar == nil {
		return out
	}
	// u 表示当前遍历过程中的u
	for _, u := range urls {
		if u == nil {
			continue
		}
		// c 表示当前遍历过程中的c
		for _, c := range jar.Cookies(u) {
			out[c.Name] = c.Value
		}
	}
	return out
}

// mustParseURL 负责mustParseURL相关处理。
func mustParseURL(raw string) *url.URL {
	// u、err 保存u、err，供当前处理流程使用
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	return u
}

// renderQRDataURL 负责renderQR数据URL相关处理。
func renderQRDataURL(content string) (string, error) {
	// png、err 保存png、err，供当前处理流程使用
	png, err := qrcode.New(content, qrcode.Low)
	if err != nil {
		return "", err
	}
	png.DisableBorder = false
	// pngBytes、err 保存pngBytes、err，供当前处理流程使用
	pngBytes, err := png.PNG(256)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes), nil
}

// extractFaceHToken 负责extractFaceH令牌相关处理。
func extractFaceHToken(pageHTML string) (string, error) {
	// re 保存re，供当前处理流程使用
	re := regexp.MustCompile(`htoken=([A-Za-z0-9_\-]+)`)
	// match 保存match，供当前处理流程使用
	match := re.FindStringSubmatch(pageHTML)
	if match == nil {
		return "", fmt.Errorf("人脸验证：未能提取 htoken")
	}
	return match[1], nil
}

// extractVerifyModesURL 负责extractVerifyModesURL相关处理。
func extractVerifyModesURL(pageHTML string) (string, error) {
	// re 保存re，供当前处理流程使用
	re := regexp.MustCompile(`window\.location\.href\s*=\s*"((?:https?:)?//[^"]*?/iv/mini/verify_modes\.htm\?[^"]*)"`)
	// match 保存match，供当前处理流程使用
	match := re.FindStringSubmatch(pageHTML)
	if match == nil {
		return "", fmt.Errorf("人脸验证：未能提取 verify_modes 链接")
	}
	// raw 保存原始，供当前处理流程使用
	raw := html.UnescapeString(match[1])
	raw = strings.ReplaceAll(raw, `\/`, `/`)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	if strings.HasSuffix(raw, "_umidfg=") {
		raw += "1"
	}
	return raw, nil
}

// extractFaceQRCodeContent 负责extractFaceQRCode内容相关处理。
func extractFaceQRCodeContent(pageHTML string) (string, error) {
	// re 保存re，供当前处理流程使用
	re := regexp.MustCompile(`new\s+Qrcode\(\{\s*text:\s*"((?:\\.|[^"\\])*)"`)
	// match 保存match，供当前处理流程使用
	match := re.FindStringSubmatch(pageHTML)
	if match == nil {
		return "", fmt.Errorf("人脸验证：未能提取人脸验证二维码 URL")
	}
	// content、err 保存content、err，供当前处理流程使用
	content, err := strconv.Unquote(`"` + match[1] + `"`)
	if err != nil {
		content = match[1]
	}
	content = html.UnescapeString(strings.ReplaceAll(content, `\/`, `/`))
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("人脸验证二维码内容为空")
	}
	return content, nil
}
