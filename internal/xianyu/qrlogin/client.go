// Package qrlogin 实现闲鱼扫码登录；风控验证场景可通过浏览器提取真实 cookie。
// 移植自 Python utils/qr_login.py 的 QRLoginManager。
package qrlogin

import (
	"bytes"
	"context"
	"crypto/md5"
	rand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"xianyu-go/internal/logsafe"
	"xianyu-go/internal/xianyu"
)

const (
	host          = "https://passport.goofish.com"
	apiMiniLogin  = host + "/mini_login.htm"
	apiGenerateQR = host + "/newlogin/qrcode/generate.do"
	apiScanStatus = host + "/newlogin/qrcode/query.do"
	apiH5TK       = "https://h5api.m.goofish.com/h5/mtop.gaia.nodejs.gaia.idle.data.gw.v2.index.get/1.0/"
	appKey        = "34839810"
)

var qrVerifyTargetURL = "https://www.goofish.com/im"

var qrHeaders = map[string]string{
	"User-Agent":      xianyu.BrowserUA,
	"Accept":          "application/json, text/plain, */*",
	"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
	"Connection":      "keep-alive",
	"Sec-Fetch-Dest":  "empty",
	"Sec-Fetch-Mode":  "cors",
	"Sec-Fetch-Site":  "same-origin",
	"Referer":         "https://passport.goofish.com/",
	"Origin":          "https://passport.goofish.com",
}

// Session 一个扫码登录会话。
type Session struct {
	SessionID              string `json:"session_id"`
	Status                 string `json:"status"` // waiting/scanned/success/expired/cancelled/verification_required
	QRCodeURL              string `json:"qr_code_url"`
	qrContent              string
	cookies                map[string]string
	unb                    string
	createdTime            time.Time
	expireTime             time.Duration
	params                 map[string]string
	verificationURL        string
	verificationScreenshot string // 最新截图 data URL，前端轮询时显示
}

func (s *Session) isExpired() bool {
	return time.Since(s.createdTime) > s.expireTime
}

// SidecarRefresher 由外部注入的浏览器刷新函数。
// tmpCookies: 扫码临时 cookie；verificationURL: Chromium 打开后持有 session 等待验证。
// onScreenshot: 每次截图时回调（data:image/png;base64,...），前端实时显示验证页画面。
type SidecarRefresher func(ctx context.Context, tmpCookies, verificationURL string, onScreenshot func(string)) (realCookies string, unb string, err error)

// Manager 扫码登录管理器。
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	httpc    *http.Client
	logger   *slog.Logger
	sidecar  SidecarRefresher // 可选：风控验证后用浏览器提取真实 cookie
}

// SetSidecarRefresher 注入 sidecar 刷新函数（风控验证场景必需）。
func (m *Manager) SetSidecarRefresher(f SidecarRefresher) {
	m.sidecar = f
}

// NewManager 构造。
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		sessions: make(map[string]*Session),
		httpc:    &http.Client{Timeout: 60 * time.Second},
		logger:   logger,
	}
}

// GenerateQRCode 生成扫码登录二维码。返回 session_id + qr_code_url（base64 data URL）。
func (m *Manager) GenerateQRCode(ctx context.Context) (sessionID string, qrCodeURL string, err error) {
	sessionID = randomUUID()
	sess := &Session{
		SessionID:   sessionID,
		Status:      "waiting",
		cookies:     make(map[string]string),
		createdTime: time.Now(),
		expireTime:  5 * time.Minute,
		params:      make(map[string]string),
	}

	// 1. 获取 m_h5_tk。
	if err := m.getMH5TK(ctx, sess); err != nil {
		return "", "", fmt.Errorf("获取 m_h5_tk 失败: %w", err)
	}

	// 2. 获取登录参数。
	loginParams, err := m.getLoginParams(ctx, sess)
	if err != nil {
		return "", "", fmt.Errorf("获取登录参数失败: %w", err)
	}

	// 3. 生成二维码。
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiGenerateQR, nil)
	q := req.URL.Query()
	for k, v := range loginParams {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()
	m.setHeaders(req)

	resp, err := m.httpc.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("请求二维码接口失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Content struct {
			Success bool `json:"success"`
			Data    struct {
				T           any    `json:"t"`
				Ck          string `json:"ck"`
				CodeContent string `json:"codeContent"`
			} `json:"data"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", fmt.Errorf("解析二维码响应失败: %w (body=%s)", err, truncate(string(body), 300))
	}
	if !result.Content.Success {
		return "", "", fmt.Errorf("获取登录二维码失败 (body=%s)", truncate(string(body), 300))
	}

	// t 是毫秒时间戳数字，必须转成纯数字字符串（不能用科学计数法）。
	var tStr string
	switch tv := result.Content.Data.T.(type) {
	case float64:
		tStr = strconv.FormatInt(int64(tv), 10)
	case string:
		tStr = tv
	default:
		tStr = fmt.Sprintf("%d", tv)
	}
	sess.params["t"] = tStr
	sess.params["ck"] = result.Content.Data.Ck
	sess.qrContent = result.Content.Data.CodeContent

	// 4. 生成二维码图片 base64。
	png, err := qrcode.New(result.Content.Data.CodeContent, qrcode.Low)
	if err != nil {
		return "", "", fmt.Errorf("生成二维码失败: %w", err)
	}
	png.DisableBorder = false
	pngBytes, _ := png.PNG(256)
	pngBytes64 := base64.StdEncoding.EncodeToString(pngBytes)
	qrCodeURL = "data:image/png;base64," + pngBytes64

	sess.QRCodeURL = qrCodeURL

	m.mu.Lock()
	m.sessions[sessionID] = sess
	m.mu.Unlock()

	// 5. 后台轮询扫码状态。
	go m.monitorQRStatus(context.Background(), sessionID)

	m.logger.Info("二维码生成成功", "session_id", sessionID)
	return sessionID, qrCodeURL, nil
}

// GetSessionStatus 查询扫码状态。
func (m *Manager) GetSessionStatus(sessionID string) map[string]any {
	m.mu.Lock()
	sess, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return map[string]any{"status": "not_found"}
	}
	if sess.isExpired() && sess.Status != "success" {
		sess.Status = "expired"
	}
	result := map[string]any{
		"status":     sess.Status,
		"session_id": sessionID,
	}
	if sess.Status == "verification_required" && sess.verificationURL != "" {
		result["verification_url"] = sess.verificationURL
		result["message"] = "账号被风控，需要手机验证"
		if sess.verificationScreenshot != "" {
			result["verification_screenshot"] = sess.verificationScreenshot
		}
	}
	if sess.Status == "success" && sess.cookies != nil && sess.unb != "" {
		result["cookies"] = cookieMarshal(sess.cookies)
		result["unb"] = sess.unb
	}
	return result
}

// CleanupExpired 清理过期会话。
func (m *Manager) CleanupExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if s.isExpired() && s.Status != "success" {
			delete(m.sessions, id)
			m.logger.Info("清理过期扫码会话", "session_id", id)
		}
	}
}

// monitorQRStatus 后台轮询扫码状态（移植自 Python _monitor_qr_status）。
func (m *Manager) monitorQRStatus(ctx context.Context, sessionID string) {
	m.mu.Lock()
	sess := m.sessions[sessionID]
	m.mu.Unlock()
	if sess == nil {
		return
	}

	maxWait := 5 * time.Minute
	start := time.Now()

	for time.Since(start) < maxWait {
		select {
		case <-ctx.Done():
			return
		default:
		}

		m.mu.Lock()
		if _, ok := m.sessions[sessionID]; !ok {
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()

		resp, err := m.pollQRCodeStatus(ctx, sess)
		if err != nil {
			m.logger.Error("轮询扫码状态异常", "err", err)
			time.Sleep(2 * time.Second)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var qrResult struct {
			Content struct {
				Data struct {
					QRCodeStatus      string `json:"qrCodeStatus"`
					IframeRedirect    bool   `json:"iframeRedirect"`
					IframeRedirectURL string `json:"iframeRedirectUrl"`
				} `json:"data"`
			} `json:"content"`
		}
		json.Unmarshal(body, &qrResult)

		status := qrResult.Content.Data.QRCodeStatus
		switch status {
		case "CONFIRMED":
			if qrResult.Content.Data.IframeRedirect {
				// 风控验证：记录状态和 URL，提取响应 cookie（临时 cookie），
				// 验证完成后用这些临时 cookie 访问 goofish.com/im 换真实 cookie。
				if sess.Status != "verification_required" {
					sess.Status = "verification_required"
					sess.verificationURL = qrResult.Content.Data.IframeRedirectURL
					for _, c := range resp.Cookies() {
						sess.cookies[c.Name] = c.Value
					}
					m.logger.Warn("扫码登录需要风控验证，立即启动浏览器等待验证", "session_id", sessionID, "verification_url", logsafe.URL(sess.verificationURL), "tmp_cookie_count", len(sess.cookies))

					// 关键：必须在检测到风控时立即启动浏览器，带临时 cookie 打开验证页面。
					// 用户在手机完成验证后，服务端回调打到这个 Chromium 页面（redirect 到 ivCheckLogin.htm），
					// Chromium 才能拿到授权 session，再访问 /im 得到 unb。
					// 不能等用户点按钮——那时验证已绑定到用户浏览器 session，与此 Chromium 无关。
					if m.sidecar != nil {
						sidecarFn := m.sidecar
						cookieStr := cookieMarshal(sess.cookies)
						verURL := sess.verificationURL
						go func() {
							onScreenshot := func(dataURL string) {
								m.mu.Lock()
								if s, ok := m.sessions[sessionID]; ok {
									s.verificationScreenshot = dataURL
								}
								m.mu.Unlock()
							}
							realCookies, unb, err := sidecarFn(context.Background(), cookieStr, verURL, onScreenshot)
							m.mu.Lock()
							defer m.mu.Unlock()
							s, ok := m.sessions[sessionID]
							if !ok {
								return
							}
							if err != nil {
								m.logger.Error("浏览器等待验证失败", "err", err)
								return
							}
							s.cookies = parseCookieStr(realCookies)
							s.unb = unb
							s.Status = "success"
							m.logger.Info("浏览器验证成功，已自动完成登录", "session_id", sessionID, "account_hash", logsafe.ID(unb))
						}()
					}
				}
				time.Sleep(800 * time.Millisecond)
				continue
			}
			sess.Status = "success"
			for _, c := range resp.Cookies() {
				sess.cookies[c.Name] = c.Value
				if c.Name == "unb" {
					sess.unb = c.Value
				}
			}
			m.logger.Info("扫码登录成功", "session_id", sessionID, "account_hash", logsafe.ID(sess.unb))
			return
		case "NEW":
			// 未扫码，继续
		case "EXPIRED":
			// 验证中的 EXPIRED 是正常的（验证通过后二维码 session 结束），
			// 保持 verification_required 状态，等用户点"我已完成验证"。
			if sess.Status != "verification_required" {
				sess.Status = "expired"
				m.logger.Info("二维码已过期", "session_id", sessionID)
				return
			}
			m.logger.Info("验证中收到 EXPIRED，保持验证状态", "session_id", sessionID)
			time.Sleep(2 * time.Second)
			continue
		case "SCANED":
			if sess.Status == "waiting" {
				sess.Status = "scanned"
				m.logger.Info("二维码已扫描，等待确认", "session_id", sessionID)
			}
		default:
			sess.Status = "cancelled"
			m.logger.Info("用户取消登录", "session_id", sessionID)
			return
		}
		time.Sleep(800 * time.Millisecond)
	}

	// 超时
	if sess.Status != "success" && sess.Status != "expired" && sess.Status != "cancelled" {
		sess.Status = "expired"
	}
}

// CompleteVerification 用户完成风控验证后调用：先用扫码临时 cookie 访问 goofish.com/im，
// 必要时再使用浏览器提取真实 cookie（含 unb）。
// 移植自 Python refresh_cookies_from_qr_login 的核心，并保留浏览器兜底。
func (m *Manager) CompleteVerification(ctx context.Context, sessionID string) (cookies string, unb string, err error) {
	m.mu.Lock()
	sess, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return "", "", fmt.Errorf("会话不存在")
	}
	if len(sess.cookies) == 0 {
		return "", "", fmt.Errorf("无扫码临时 cookie")
	}

	m.logger.Info("开始用临时 cookie 换取真实 cookie", "session_id", sessionID, "tmp_cookie_count", len(sess.cookies))

	// 用带 cookie jar 的 client，访问 goofish.com/im 触发真实 cookie 下发。
	jarClient := &http.Client{Timeout: 30 * time.Second}
	targetURL := qrVerifyTargetURL
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	m.setHeaders(req)
	req.Header.Set("Cookie", cookieMarshal(sess.cookies))
	// im 页面 referer
	req.Header.Set("Referer", "https://www.goofish.com/")

	resp, err := jarClient.Do(req)
	if err != nil {
		if m.sidecar == nil {
			return "", "", fmt.Errorf("访问 goofish.com/im 失败: %w", err)
		}
		m.logger.Warn("纯 HTTP 访问 goofish.com/im 失败，改用 sidecar 提取", "session_id", sessionID, "err", err)
	} else {
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)

		// 收集 Set-Cookie。
		for _, c := range resp.Cookies() {
			sess.cookies[c.Name] = c.Value
			if c.Name == "unb" {
				sess.unb = c.Value
			}
		}
	}

	// 如果还没 unb 且没有 sidecar，再访问 mtop token API 触发（有时需要额外请求）。
	// 有 sidecar 时直接走浏览器路径，避免在风控场景中重复纯 HTTP 请求。
	if sess.unb == "" && m.sidecar == nil {
		m.logger.Info("首次访问未拿到 unb，尝试 mtop token API", "session_id", sessionID)
		if err := m.getMH5TK(ctx, sess); err != nil {
			m.logger.Warn("mtop token API 失败", "err", err)
		}
	}

	m.logger.Info("验证完成，提取 cookie", "session_id", sessionID, "cookie_count", len(sess.cookies), "has_unb", sess.unb != "")
	if sess.unb != "" {
		sess.Status = "success"
		m.logger.Info("纯 HTTP 提取 cookie 成功", "session_id", sessionID, "account_hash", logsafe.ID(sess.unb))
		return cookieMarshal(sess.cookies), sess.unb, nil
	}

	// 纯 HTTP 拿不到 unb（闲鱼真实 cookie 由页面 JS 异步下发），
	// 风控验证后的 cookie 提取必须依赖 sidecar（playwright 浏览器）。
	if m.sidecar == nil {
		return "", "", fmt.Errorf("风控验证后的 cookie 提取需要浏览器支持（请勿使用 -no-browser 启动）")
	}

	m.logger.Info("纯 HTTP 未拿到 unb，调用 sidecar 浏览器提取", "session_id", sessionID)
	realCookies, sidecarUNB, err := m.sidecar(ctx, cookieMarshal(sess.cookies), sess.verificationURL, nil)
	if err != nil {
		m.logger.Error("sidecar 提取失败", "err", err)
		return "", "", fmt.Errorf("浏览器提取 cookie 失败: %w", err)
	}
	parsedCookies := parseCookieStr(realCookies)
	if len(parsedCookies) == 0 {
		return "", "", fmt.Errorf("浏览器提取 cookie 失败: sidecar 未返回有效 cookie")
	}
	sess.cookies = parsedCookies
	sess.unb = sidecarUNB
	if sess.unb == "" {
		sess.unb = sess.cookies["unb"]
	}
	if sess.unb == "" {
		return "", "", fmt.Errorf("验证后仍未获取到 unb，可能验证未完成或临时 cookie 已失效")
	}
	m.logger.Info("sidecar 提取成功", "session_id", sessionID, "account_hash", logsafe.ID(sess.unb), "cookie_count", len(sess.cookies))

	sess.Status = "success"
	return cookieMarshal(sess.cookies), sess.unb, nil
}

// parseCookieStr 把 "k=v; k2=v2" 解析回 map。
func parseCookieStr(s string) map[string]string {
	m := make(map[string]string)
	for _, part := range strings.Split(s, "; ") {
		if eq := strings.Index(part, "="); eq >= 0 {
			m[part[:eq]] = part[eq+1:]
		}
	}
	return m
}

// getMH5TK 获取 m_h5_tk（移植自 Python _get_mh5tk）。
func (m *Manager) getMH5TK(ctx context.Context, sess *Session) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiH5TK, nil)
	m.setHeaders(req)

	resp, err := m.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	// 合并 Set-Cookie。
	for _, c := range resp.Cookies() {
		sess.cookies[c.Name] = c.Value
	}

	mH5TK := sess.cookies["_m_h5_tk"]
	token := ""
	if parts := strings.SplitN(mH5TK, "_", 2); len(parts) > 0 {
		token = parts[0]
	}

	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	dataStr := `{"bizScene":"home"}`
	signInput := token + "&" + t + "&" + appKey + "&" + dataStr
	sign := md5hex(signInput)

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

	req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiH5TK+"?"+params.Encode(), nil)
	m.setHeaders(req2)
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp2, err := m.httpc.Do(req2)
	if err != nil {
		return err
	}
	defer resp2.Body.Close()
	io.Copy(io.Discard, resp2.Body)

	// 更新 cookie。
	for _, c := range resp2.Cookies() {
		sess.cookies[c.Name] = c.Value
	}
	return nil
}

// getLoginParams 获取登录表单参数（移植自 Python _get_login_params）。
func (m *Manager) getLoginParams(ctx context.Context, sess *Session) (map[string]string, error) {
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
	params.Set("rnd", fmt.Sprintf("%f", randFloat()))

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiMiniLogin+"?"+params.Encode(), nil)
	m.setHeaders(req)

	// 带上已有 cookie。
	cookieStr := cookieMarshal(sess.cookies)
	if cookieStr != "" {
		req.Header.Set("Cookie", cookieStr)
	}

	resp, err := m.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// 调试：打印响应状态和 body 前 200 字符

	// 从 HTML 里提取 window.viewData = {...};
	re := regexp.MustCompile(`window\.viewData\s*=\s*(\{.*?\});`)
	match := re.FindSubmatch(body)
	if match == nil {
		return nil, fmt.Errorf("获取登录参数失败：未找到 viewData")
	}

	var viewData struct {
		LoginFormData map[string]any `json:"loginFormData"`
	}
	if err := json.Unmarshal(match[1], &viewData); err != nil {
		return nil, fmt.Errorf("解析 viewData 失败: %w", err)
	}
	if viewData.LoginFormData == nil {
		return nil, fmt.Errorf("loginFormData 为空")
	}
	// 把所有值转为字符串（有些是 bool/number）。
	strParams := make(map[string]string, len(viewData.LoginFormData))
	for k, v := range viewData.LoginFormData {
		strParams[k] = fmt.Sprintf("%v", v)
	}
	strParams["umidTag"] = "SERVER"
	sess.params = strParams
	return strParams, nil
}

// pollQRCodeStatus 轮询二维码状态。
func (m *Manager) pollQRCodeStatus(ctx context.Context, sess *Session) (*http.Response, error) {
	form := url.Values{}
	for k, v := range sess.params {
		form.Set(k, v)
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiScanStatus, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	m.setHeaders(req)
	cookieStr := cookieMarshal(sess.cookies)
	if cookieStr != "" {
		req.Header.Set("Cookie", cookieStr)
	}

	return m.httpc.Do(req)
}

func (m *Manager) setHeaders(req *http.Request) {
	for k, v := range qrHeaders {
		req.Header.Set(k, v)
	}
}

// ---- 工具函数 ----

func md5hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func cookieMarshal(cookies map[string]string) string {
	parts := make([]string, 0, len(cookies))
	for k, v := range cookies {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}

func randomUUID() string {
	b := make([]byte, 16)
	if _, err := io.ReadFull(randReader, b); err != nil {
		// 降级到时间种子（极少见）
		for i := range b {
			b[i] = byte(time.Now().UnixNano() >> (i * 8))
		}
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

var randReader io.Reader = rand.Reader
var randFloat = func() float64 { return float64(time.Now().UnixNano()%1e9) / 1e9 }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// 包级别引用，避免 import 报错。
var _ = bytes.NewReader
