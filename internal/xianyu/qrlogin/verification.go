package qrlogin

import (
	"context"
	"crypto/md5"
	rand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/logsafe"
	"xianyu-go/internal/xianyu"
	"xianyu-go/internal/xianyu/cookierefresh"
)

// CompleteVerification 完成已确认扫码会话的人工验证并返回最终凭证与平台账号标识。
func (m *Manager) CompleteVerification(ctx context.Context, sessionID string) (cookies string, unb string, err error) {
	m.mu.Lock()
	// sess、ok 用于本次流程后续判断的sess、ok
	sess, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return "", "", fmt.Errorf("会话不存在")
	}
	// state 用于本次流程后续判断的状态
	state := sess.snapshot()
	if len(state.cookies) == 0 {
		return "", "", fmt.Errorf("无扫码临时 cookie")
	}
	if state.status == "success" && state.unb != "" {
		return snapshotCookieHeader(state, qrVerifyTargetURL), state.unb, nil
	}
	m.logger.Info("开始用临时 cookie 换取真实 cookie", "session_id", sessionID, "tmp_cookie_count", len(state.cookies))

	// targetURL 用于本次流程后续判断的targetURL
	targetURL := qrVerifyTargetURL
	// seedURL 用于本次流程后续判断的seedURL
	seedURL := state.verificationURL
	if strings.TrimSpace(seedURL) == "" {
		seedURL = targetURL
	}
	// jarClient、jar、err 用于本次流程后续判断的jarClient、jar、err
	jarClient, jar, err := m.faceHTTPClient(state.cookies, state.cookieSnapshot, seedURL, targetURL)
	if err != nil {
		return "", "", fmt.Errorf("创建登录 Cookie Jar: %w", err)
	}
	jarClient.Timeout = 30 * time.Second
	// target、err 用于本次流程后续判断的target、err
	target, err := url.Parse(targetURL)
	if err != nil {
		return "", "", fmt.Errorf("解析登录凭证换取地址: %w", err)
	}
	// req、err 用于本次流程后续判断的req、err
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", "", err
	}
	m.setDocumentHeaders(req)
	// resp、err 用于本次流程后续判断的resp、err
	resp, err := jarClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("访问 goofish.com/im 换取登录凭证失败: %w", err)
	}
	defer resp.Body.Close()
	if // err 用于本次流程后续判断的err
	_, err := io.Copy(io.Discard, resp.Body); err != nil {
		return "", "", fmt.Errorf("读取登录凭证换取响应失败: %w", err)
	}
	// responseURL 用于本次流程后续判断的响应URL
	var responseURL *url.URL
	if resp.Request != nil {
		responseURL = resp.Request.URL
	}
	// finalCookies 用于本次流程后续判断的finalCookies
	finalCookies := collectJarCookies(jar, target, responseURL)
	// finalUNB 用于本次流程后续判断的finalUNB
	finalUNB := finalCookies["unb"]
	if finalUNB == "" {
		return "", "", fmt.Errorf("纯 Go 登录凭证换取未获取到 unb，验证可能尚未完成或临时 Cookie 已失效")
	}
	sess.mu.Lock()
	sess.cookies = finalCookies
	if // finalSnapshot、complete 用于本次流程后续判断的finalSnapshot、complete
	finalSnapshot, complete := jar.Snapshot(); complete {
		sess.cookieSnapshot = finalSnapshot
	} else {
		sess.cookieSnapshot = nil
	}
	sess.unb = finalUNB
	sess.Status = "success"
	sess.verificationScreenshot = ""
	sess.mu.Unlock()
	m.logger.Info("纯 Go 提取登录凭证成功", "session_id", sessionID, "account_hash", logsafe.ID(finalUNB), "cookie_count", len(finalCookies))
	return snapshotCookieHeader(sess.snapshot(), qrVerifyTargetURL), finalUNB, nil
}

// parseCookieStr 把 "k=v; k2=v2" 解析回 map。
func parseCookieStr(s string) map[string]string {
	// m 用于本次流程后续判断的m
	m := make(map[string]string)
	// part 表示当前遍历过程中的part
	for _, part := range strings.Split(s, "; ") {
		if // eq 用于本次流程后续判断的eq
		eq := strings.Index(part, "="); eq >= 0 {
			m[part[:eq]] = part[eq+1:]
		}
	}
	return m
}

// getMH5TK 获取 m_h5_tk。
func (m *Manager) getMH5TK(ctx context.Context, sess *Session) error {
	// req 用于本次流程后续判断的req
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiH5TK, nil)
	m.setHeaders(req)

	// resp、err 用于本次流程后续判断的resp、err
	resp, err := m.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	absorbSessionResponse(sess, apiH5TK, resp)
	if // err 用于本次流程后续判断的err
	_, err := io.Copy(io.Discard, resp.Body); err != nil {
		return err
	}
	// mH5TK 用于本次流程后续判断的mH5TK
	mH5TK := protocolCookieValue(sessionCookieHeader(sess, apiH5TK), "_m_h5_tk")
	// token 用于本次流程后续判断的令牌
	token := ""
	if // parts 用于本次流程后续判断的parts
	parts := strings.SplitN(mH5TK, "_", 2); len(parts) > 0 {
		token = parts[0]
	}

	// t 用于本次流程后续判断的t
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// dataStr 用于本次流程后续判断的数据Str
	dataStr := `{"bizScene":"home"}`
	// signInput 用于本次流程后续判断的signInput
	signInput := token + "&" + t + "&" + appKey + "&" + dataStr
	// sign 用于本次流程后续判断的sign
	sign := md5hex(signInput)

	// params 用于本次流程后续判断的params
	params := url.Values{}
	params.Set("jsv", "2.7.2")
	params.Set("appKey", appKey)
	params.Set("t", t)
	params.Set("sign", sign)
	params.Set("v", "1.0")
	params.Set("type", "originaljson")
	params.Set("dataType", "json")
	params.Set("timeout", "20000")
	params.Set("api", "mtop.gaia.nodejs.gaia.idle.data.gw.v2.index.get")
	params.Set("data", dataStr)

	// req2 用于本次流程后续判断的req2
	req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiH5TK+"?"+params.Encode(), nil)
	m.setHeaders(req2)
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// cookieStr 用于本次流程后续判断的登录凭证Str
	cookieStr := sessionCookieHeader(sess, req2.URL.String())
	if cookieStr != "" {
		req2.Header.Set("Cookie", cookieStr)
	}

	// resp2、err 用于本次流程后续判断的resp2、err
	resp2, err := m.httpc.Do(req2)
	if err != nil {
		return err
	}
	defer resp2.Body.Close()
	absorbSessionResponse(sess, req2.URL.String(), resp2)
	if // err 用于本次流程后续判断的err
	_, err := io.Copy(io.Discard, resp2.Body); err != nil {
		return err
	}

	return nil
}

// getLoginParams 获取登录表单参数。
func (m *Manager) getLoginParams(ctx context.Context, sess *Session) (map[string]string, error) {
	// params 用于本次流程后续判断的params
	params := url.Values{}
	params.Set("lang", "zh_cn")
	params.Set("appName", "xianyu")
	params.Set("appEntrance", "web")
	params.Set("styleType", "vertical")
	params.Set("bizParams", "")
	params.Set("notLoadSsoView", "false")
	params.Set("notKeepLogin", "false")
	params.Set("isMobile", "false")
	params.Set("qrCodeFirst", "false")
	params.Set("stie", "77")
	params.Set("rnd", strconv.FormatFloat(randFloat(), 'f', -1, 64))

	// req 用于本次流程后续判断的req
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiMiniLogin+"?"+params.Encode(), nil)
	m.setHeaders(req)

	// 带上已有 cookie。
	cookieStr := sessionCookieHeader(sess, req.URL.String())
	if cookieStr != "" {
		req.Header.Set("Cookie", cookieStr)
	}

	// resp、err 用于本次流程后续判断的resp、err
	resp, err := m.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	absorbSessionResponse(sess, req.URL.String(), resp)
	// body、err 用于本次流程后续判断的body、err
	body, err := readQRBody(resp.Body)
	if err != nil {
		return nil, err
	}

	// 调试：打印响应状态和 body 前 200 字符

	// 从 HTML 里提取 window.viewData = {...};
	re := regexp.MustCompile(`window\.viewData\s*=\s*(\{.*?\});`)
	// match 用于本次流程后续判断的match
	match := re.FindSubmatch(body)
	if match == nil {
		return nil, fmt.Errorf("获取登录参数失败：未找到 viewData")
	}

	// viewData 用于本次流程后续判断的view数据
	var viewData struct {
		LoginFormData map[string]any `json:"loginFormData"`
	}
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(match[1], &viewData); err != nil {
		return nil, fmt.Errorf("解析 viewData 失败: %w", err)
	}
	if viewData.LoginFormData == nil {
		return nil, fmt.Errorf("loginFormData 为空")
	}
	// 把所有值转为字符串（有些是 bool/number）。
	strParams := make(map[string]string, len(viewData.LoginFormData))
	// k、v 表示当前遍历过程中的k、v
	for k, v := range viewData.LoginFormData {
		strParams[k] = fmt.Sprintf("%v", v)
	}
	strParams["umidTag"] = "SERVER"
	sess.mu.Lock()
	sess.params = strParams
	sess.mu.Unlock()
	return strParams, nil
}

// pollQRCodeStatus 轮询二维码状态。
func (m *Manager) pollQRCodeStatus(ctx context.Context, sess *Session) (*http.Response, error) {
	// form 用于本次流程后续判断的表单
	form := url.Values{}
	// state 用于本次流程后续判断的状态
	state := sess.snapshot()
	// k、v 表示当前遍历过程中的k、v
	for k, v := range state.params {
		form.Set(k, v)
	}
	// fingerprint 用于本次流程后续判断的fingerprint
	fingerprint := xianyu.CurrentBrowserFingerprint()
	// 对齐 havana-nlogin 二维码组件 query.do 的浏览器环境字段。
	// ua 是 AWSC/UAB 的可选结果；纯 Go 客户端在该值尚未生成时与官网
	// 脚本一样发送空值，不借助 Chromium 执行业务页面脚本。
	form.Set("ua", "")
	form.Set("navlanguage", "zh-CN")
	form.Set("navUserAgent", fingerprint.UserAgent)
	form.Set("navPlatform", navigatorPlatform(fingerprint.Platform))
	form.Set("isIframe", "true")
	form.Set("documentReferer", qrVerifyTargetURL)
	form.Set("defaultView", "qrcode")

	// req 用于本次流程后续判断的req
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiScanStatus, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	m.setHeaders(req)
	// cookieStr 用于本次流程后续判断的登录凭证Str
	cookieStr := sessionCookieHeader(sess, req.URL.String())
	if cookieStr != "" {
		req.Header.Set("Cookie", cookieStr)
	}

	return m.httpc.Do(req)
}

// navigatorPlatform 封装navigatorPlatform业务协调。
func navigatorPlatform(secCHPlatform string) string {
	switch strings.ToLower(strings.TrimSpace(secCHPlatform)) {
	case "windows":
		return "Win32"
	case "macos":
		return "MacIntel"
	case "linux":
		return "Linux x86_64"
	case "android":
		return "Linux armv8l"
	case "ios":
		return "iPhone"
	default:
		return strings.TrimSpace(secCHPlatform)
	}
}

// setHeaders 封装setHeaders业务协调。
func (m *Manager) setHeaders(req *http.Request) {
	xianyu.ApplyBrowserFingerprint(req.Header)
	// k、v 表示当前遍历过程中的k、v
	for k, v := range qrHeaders {
		req.Header.Set(k, v)
	}
}

// setDocumentHeaders 复刻浏览器从闲鱼首页进入 /im 的文档请求头。这里只
// 发送 HTTP 请求并接收 Set-Cookie，不加载或校验任何页面 DOM。
// setDocumentHeaders 封装setDocumentHeaders业务协调。
func (m *Manager) setDocumentHeaders(req *http.Request) {
	xianyu.ApplyBrowserFingerprint(req.Header)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Referer", "https://www.goofish.com/")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
}

// sessionCookieHeader 封装会话登录凭证Header业务协调。
func sessionCookieHeader(sess *Session, requestURL string) string {
	if sess == nil {
		return ""
	}
	return snapshotCookieHeader(sess.snapshot(), requestURL)
}

// snapshotCookieHeader 封装snapshot登录凭证Header业务协调。
func snapshotCookieHeader(state sessionSnapshot, requestURL string) string {
	if state.cookieSnapshot != nil {
		if // value、authoritative 用于本次流程后续判断的value、authoritative
		value, authoritative := cookierefresh.ScopedCookieHeaderForRequest(
			state.cookieSnapshot, requestURL, qrTopSite, time.Now(),
		); authoritative {
			return value
		}
	}
	return cookieMarshal(state.cookies)
}

// absorbSessionResponse 封装absorb会话响应业务协调。
func absorbSessionResponse(sess *Session, requestURL string, resp *http.Response) {
	if sess == nil || resp == nil {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.cookieSnapshot != nil {
		sess.cookieSnapshot = cookierefresh.ApplySetCookies(
			sess.cookieSnapshot, requestURL, resp.Header.Values("Set-Cookie"), time.Now(), qrTopSite,
		)
		if sess.cookieSnapshot == nil {
			sess.cookieSnapshot = []cookierefresh.BrowserCookie{}
		}
	}
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range resp.Cookies() {
		if cookie == nil || cookie.Name == "" {
			continue
		}
		if cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && !cookie.Expires.After(time.Now())) {
			delete(sess.cookies, cookie.Name)
			if cookie.Name == "unb" {
				sess.unb = ""
			}
			continue
		}
		sess.cookies[cookie.Name] = cookie.Value
		if cookie.Name == "unb" && cookie.Value != "" {
			sess.unb = cookie.Value
		}
	}
}

// finalizeSessionCredentialsLocked 封装finalize会话CredentialsLocked业务协调。
func finalizeSessionCredentialsLocked(sess *Session) {
	if sess == nil {
		return
	}
	if sess.cookieSnapshot != nil {
		// value 用于本次流程后续判断的值
		value, _ := cookierefresh.ScopedCookieHeaderForRequest(
			sess.cookieSnapshot, qrVerifyTargetURL, qrTopSite, time.Now(),
		)
		sess.cookies = parseCookieStr(value)
	}
	if // unb 用于本次流程后续判断的unb
	unb := sess.cookies["unb"]; unb != "" {
		sess.unb = unb
	}
}

// cloneCookieSnapshot 封装clone登录凭证Snapshot业务协调。
func cloneCookieSnapshot(in []cookierefresh.BrowserCookie) []cookierefresh.BrowserCookie {
	if in == nil {
		return nil
	}
	// out 用于本次流程后续判断的out
	out := cookierefresh.NormalizeSnapshot(in)
	if out == nil {
		return []cookierefresh.BrowserCookie{}
	}
	return out
}

// protocolCookieValue 封装protocol登录凭证值业务协调。
func protocolCookieValue(cookieHeader, name string) string {
	// part 表示当前遍历过程中的part
	for _, part := range strings.Split(cookieHeader, ";") {
		// key、value、ok 用于本次流程后续判断的key、value、ok
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && key == name {
			return value
		}
	}
	return ""
}

// ---- 工具函数 ----

// md5hex 封装md5hex业务协调。
func md5hex(s string) string {
	// #nosec G401 -- 闲鱼登录协议明确要求 MD5，不能替换为其他摘要算法。
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// cookieMarshal 封装登录凭证Marshal业务协调。
func cookieMarshal(cookies map[string]string) string {
	// parts 用于本次流程后续判断的parts
	parts := make([]string, 0, len(cookies))
	// k、v 表示当前遍历过程中的k、v
	for k, v := range cookies {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}

// randomUUID 封装randomUUID业务协调。
func randomUUID() (string, error) {
	// b 用于本次流程后续判断的b
	b := make([]byte, 16)
	if // err 用于本次流程后续判断的err
	_, err := io.ReadFull(randReader, b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// randReader 用于本次流程后续判断的randReader
var randReader io.Reader = rand.Reader

// randFloat 用于本次流程后续判断的randFloat
var randFloat = func() float64 { return float64(time.Now().UnixNano()%1e9) / 1e9 }

// readQRBody 封装readQR请求体业务协调。
func readQRBody(r io.Reader) ([]byte, error) {
	// body、err 用于本次流程后续判断的body、err
	body, err := io.ReadAll(io.LimitReader(r, maxQRResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxQRResponseBytes {
		return nil, fmt.Errorf("扫码登录响应体超过 %d MiB", maxQRResponseBytes>>20)
	}
	return body, nil
}

// truncate 封装truncate业务协调。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
