package qrlogin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"xianyu-go/internal/logsafe"
)

// runFaceVerification 参照闲鱼浏览器端风控跳转链，纯 HTTP 复现人脸验证流程。
//
// 关键点：必须用同一个 Cookie Jar 贯穿 normal_validate -> verify_modes ->
// identity_verify -> check.do -> ivCheckLogin。服务端会在跳转链路中写入身份
// 锚点 Cookie；如果拆成多个无状态请求，最终通常拿不到 unb。
func (m *Manager) runFaceVerification(ctx context.Context, sessionID, iframeURL string) error {
	m.mu.Lock()
	sess := m.sessions[sessionID]
	if sess == nil {
		m.mu.Unlock()
		return fmt.Errorf("会话不存在")
	}
	initialCookies := cloneCookieMap(sess.cookies)
	m.mu.Unlock()
	if len(initialCookies) == 0 {
		return fmt.Errorf("无扫码临时 cookie")
	}

	client, jar, err := m.faceHTTPClient(initialCookies, iframeURL)
	if err != nil {
		return err
	}

	normalHTML, err := m.faceGetHTML(ctx, client, iframeURL, "")
	if err != nil {
		return fmt.Errorf("请求 normal_validate: %w", err)
	}
	htoken, err := extractFaceHToken(normalHTML)
	if err != nil {
		return err
	}
	verifyModesURL, err := extractVerifyModesURL(normalHTML)
	if err != nil {
		return err
	}

	identityHTML, err := m.faceGetHTML(ctx, client, verifyModesURL, "")
	if err != nil {
		return fmt.Errorf("请求 verify_modes: %w", err)
	}
	faceContent, err := extractFaceQRCodeContent(identityHTML)
	if err != nil {
		return err
	}
	faceQRURL, err := renderQRDataURL(faceContent)
	if err != nil {
		return fmt.Errorf("渲染人脸二维码: %w", err)
	}

	m.mu.Lock()
	if s := m.sessions[sessionID]; s != nil {
		s.faceQRContent = faceContent
		s.faceQRURL = faceQRURL
		s.Status = "verification_required"
	}
	m.mu.Unlock()
	m.logger.Info("人脸验证二维码已生成，等待用户手机扫码", "session_id", sessionID)

	ivCheckURL, err := m.waitFaceVerification(ctx, client, sessionID, htoken)
	if err != nil {
		return err
	}
	if _, err := m.faceGetHTML(ctx, client, ivCheckURL, faceIdentityReferer(htoken)); err != nil {
		return fmt.Errorf("请求 ivCheckLogin: %w", err)
	}

	finalCookies := collectJarCookies(jar,
		mustParseURL(host),
		mustParseURL("https://www.goofish.com/"),
		mustParseURL(iframeURL),
		mustParseURL(ivCheckURL),
	)
	if finalCookies["unb"] == "" {
		return fmt.Errorf("人脸验证完成但未获取到 unb")
	}

	m.mu.Lock()
	if s := m.sessions[sessionID]; s != nil {
		s.cookies = finalCookies
		s.unb = finalCookies["unb"]
		s.Status = "success"
	}
	m.mu.Unlock()
	m.logger.Info("人脸验证登录成功", "session_id", sessionID, "account_hash", logsafe.ID(finalCookies["unb"]))
	return nil
}

// fallbackBrowserVerification 保留旧浏览器验证作为协议变化时的兜底。
// 主路径不再截图展示，只有 API 链路失败且浏览器可用时才尝试。
func (m *Manager) fallbackBrowserVerification(ctx context.Context, sessionID, verificationURL string) {
	if m.browser == nil {
		return
	}
	m.mu.Lock()
	sess := m.sessions[sessionID]
	if sess == nil {
		m.mu.Unlock()
		return
	}
	cookieStr := cookieMarshal(sess.cookies)
	m.mu.Unlock()

	onScreenshot := func(dataURL string) {
		m.mu.Lock()
		if s, ok := m.sessions[sessionID]; ok {
			s.verificationScreenshot = dataURL
		}
		m.mu.Unlock()
	}
	realCookies, unb, err := m.browser(ctx, cookieStr, verificationURL, onScreenshot)
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return
	}
	if err != nil {
		m.logger.Error("浏览器验证兜底失败", "session_id", sessionID, "err", err)
		return
	}
	s.cookies = parseCookieStr(realCookies)
	s.unb = unb
	if s.unb == "" {
		s.unb = s.cookies["unb"]
	}
	if s.unb != "" {
		s.Status = "success"
		m.logger.Info("浏览器验证兜底成功", "session_id", sessionID, "account_hash", logsafe.ID(s.unb))
	}
}

func (m *Manager) faceHTTPClient(cookies map[string]string, iframeURL string) (*http.Client, http.CookieJar, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, nil, err
	}
	setJarCookies(jar, mustParseURL(host), cookies)
	if u, err := url.Parse(iframeURL); err == nil {
		setJarCookies(jar, u, cookies)
	}
	hc := *m.httpc
	hc.Jar = jar
	return &hc, jar, nil
}

func (m *Manager) faceGetHTML(ctx context.Context, client *http.Client, targetURL, referer string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", err
	}
	m.setHeaders(req)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := readQRBody(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (m *Manager) waitFaceVerification(ctx context.Context, client *http.Client, sessionID, htoken string) (string, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		sess := m.sessions[sessionID]
		expired := sess == nil || sess.isExpired()
		m.mu.Unlock()
		if expired {
			return "", fmt.Errorf("人脸验证超时或会话已过期")
		}
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

func (m *Manager) checkFaceVerification(ctx context.Context, client *http.Client, htoken string) (ivCheckURL string, done bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiFaceCheck, nil)
	if err != nil {
		return "", false, err
	}
	q := req.URL.Query()
	q.Set("htoken", htoken)
	req.URL.RawQuery = q.Encode()
	m.setHeaders(req)
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", faceIdentityReferer(htoken))
	resp, err := client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	body, err := readQRBody(resp.Body)
	if err != nil {
		return "", false, err
	}
	var result struct {
		Content struct {
			Code any    `json:"code"`
			URL  string `json:"url"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", false, fmt.Errorf("解析人脸验证状态失败: %w", err)
	}
	if fmt.Sprint(result.Content.Code) == "3" && result.Content.URL != "" {
		return result.Content.URL, true, nil
	}
	return "", false, nil
}

func faceIdentityReferer(htoken string) string {
	return host + "/iv/mini/identity_verify.htm?htoken=" + url.QueryEscape(htoken)
}

func cloneCookieMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func setJarCookies(jar http.CookieJar, u *url.URL, cookies map[string]string) {
	if jar == nil || u == nil {
		return
	}
	cs := make([]*http.Cookie, 0, len(cookies))
	for k, v := range cookies {
		cs = append(cs, &http.Cookie{Name: k, Value: v, Path: "/"})
	}
	jar.SetCookies(u, cs)
}

func collectJarCookies(jar http.CookieJar, urls ...*url.URL) map[string]string {
	out := make(map[string]string)
	if jar == nil {
		return out
	}
	for _, u := range urls {
		if u == nil {
			continue
		}
		for _, c := range jar.Cookies(u) {
			out[c.Name] = c.Value
		}
	}
	return out
}

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	return u
}

func renderQRDataURL(content string) (string, error) {
	png, err := qrcode.New(content, qrcode.Low)
	if err != nil {
		return "", err
	}
	png.DisableBorder = false
	pngBytes, err := png.PNG(256)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes), nil
}

func extractFaceHToken(pageHTML string) (string, error) {
	re := regexp.MustCompile(`htoken=([A-Za-z0-9_\-]+)`)
	match := re.FindStringSubmatch(pageHTML)
	if match == nil {
		return "", fmt.Errorf("人脸验证：未能提取 htoken")
	}
	return match[1], nil
}

func extractVerifyModesURL(pageHTML string) (string, error) {
	re := regexp.MustCompile(`window\.location\.href\s*=\s*"((?:https?:)?//[^"]*?/iv/mini/verify_modes\.htm\?[^"]*)"`)
	match := re.FindStringSubmatch(pageHTML)
	if match == nil {
		return "", fmt.Errorf("人脸验证：未能提取 verify_modes 链接")
	}
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

func extractFaceQRCodeContent(pageHTML string) (string, error) {
	re := regexp.MustCompile(`new\s+Qrcode\(\{\s*text:\s*"((?:\\.|[^"\\])*)"`)
	match := re.FindStringSubmatch(pageHTML)
	if match == nil {
		return "", fmt.Errorf("人脸验证：未能提取人脸验证二维码 URL")
	}
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
