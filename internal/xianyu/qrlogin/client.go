// Package qrlogin 使用纯 Go HTTP 复刻闲鱼扫码登录与人脸验证流程。
package qrlogin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"xianyu-go/internal/logsafe"
	"xianyu-go/internal/xianyu/cookierefresh"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// maxQRResponseBytes 用于本次流程后续判断的maxQR响应Bytes
const (
	maxQRResponseBytes = 2 << 20
	qrPollInterval     = 2 * time.Second
	maxQRServerErrors  = 5
	qrTopSite          = "https://goofish.com"
)

// host 用于本次流程后续判断的host
const (
	host          = "https://passport.goofish.com"
	apiMiniLogin  = host + "/mini_login.htm"
	apiGenerateQR = host + "/newlogin/qrcode/generate.do"
	apiScanStatus = host + "/newlogin/qrcode/query.do"
	apiFaceCheck  = host + "/iv/photoVerify/check.do"
	apiH5TK       = "https://h5api.m.goofish.com/h5/mtop.gaia.nodejs.gaia.idle.data.gw.v2.index.get/1.0/"
	appKey        = "34839810"
)

// qrVerifyTargetURL 用于本次流程后续判断的qrVerifyTargetURL
var qrVerifyTargetURL = "https://www.goofish.com/im"

// qrHeaders 用于本次流程后续判断的qrHeaders
var qrHeaders = map[string]string{
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
	mu                     sync.RWMutex
	SessionID              string `json:"session_id"`
	Status                 string `json:"status"` // waiting/scanned/success/expired/cancelled/error/verification_required
	QRCodeURL              string `json:"qr_code_url"`
	qrContent              string
	cookies                map[string]string
	cookieSnapshot         []cookierefresh.BrowserCookie
	unb                    string
	createdTime            time.Time
	expireTime             time.Duration
	params                 map[string]string
	verificationURL        string
	verificationScreenshot string // 历史兼容字段；纯 Go 人脸流程直接返回二维码
	faceQRURL              string // 人脸验证二维码 data URL，优先展示给前端
	faceQRContent          string // 人脸验证二维码原始内容，便于排查协议变化
}

// isExpired 封装isExpired业务协调。
func (s *Session) isExpired() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.createdTime) > s.expireTime
}

// sessionSnapshot 用于本次流程后续判断的会话Snapshot
type sessionSnapshot struct {
	status                 string
	cookies                map[string]string
	cookieSnapshot         []cookierefresh.BrowserCookie
	unb                    string
	params                 map[string]string
	verificationURL        string
	verificationScreenshot string
	faceQRURL              string
	expireTime             time.Duration
	createdTime            time.Time
}

// snapshot 封装snapshot业务协调。
func (s *Session) snapshot() sessionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sessionSnapshot{
		status: s.Status, cookies: cloneCookieMap(s.cookies), unb: s.unb,
		cookieSnapshot: cloneCookieSnapshot(s.cookieSnapshot),
		params:         cloneCookieMap(s.params), verificationURL: s.verificationURL,
		verificationScreenshot: s.verificationScreenshot, faceQRURL: s.faceQRURL,
		expireTime: s.expireTime, createdTime: s.createdTime,
	}
}

// Manager 扫码登录管理器。
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	httpc    *http.Client
	logger   *slog.Logger
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
	sessionID, err = randomUUID()
	if err != nil {
		return "", "", fmt.Errorf("生成扫码会话 ID: %w", err)
	}
	// sess 用于本次流程后续判断的sess
	sess := &Session{
		SessionID:      sessionID,
		Status:         "waiting",
		cookies:        make(map[string]string),
		cookieSnapshot: []cookierefresh.BrowserCookie{},
		createdTime:    time.Now(),
		expireTime:     5 * time.Minute,
		params:         make(map[string]string),
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
	// q 用于本次流程后续判断的q
	q := req.URL.Query()
	// k、v 表示当前遍历过程中的k、v
	for k, v := range loginParams {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()
	m.setHeaders(req)
	if // cookieStr 用于本次流程后续判断的登录凭证Str
	cookieStr := sessionCookieHeader(sess, req.URL.String()); cookieStr != "" {
		req.Header.Set("Cookie", cookieStr)
	}

	// resp、err 用于本次流程后续判断的resp、err
	resp, err := m.httpc.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("请求二维码接口失败: %w", err)
	}
	defer resp.Body.Close()
	absorbSessionResponse(sess, apiGenerateQR, resp)
	// body、err 用于本次流程后续判断的body、err
	body, err := readQRBody(resp.Body)
	if err != nil {
		return "", "", err
	}

	// result 用于本次流程后续判断的结果
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
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(body, &result); err != nil {
		return "", "", fmt.Errorf("解析二维码响应失败: %w (body=%s)", err, truncate(string(body), 300))
	}
	if !result.Content.Success {
		return "", "", fmt.Errorf("获取登录二维码失败 (body=%s)", truncate(string(body), 300))
	}

	// t 是毫秒时间戳数字，必须转成纯数字字符串（不能用科学计数法）。
	var tStr string
	switch // tv 用于本次流程后续判断的tv
	tv := result.Content.Data.T.(type) {
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
	// pngBytes 用于本次流程后续判断的pngBytes
	pngBytes, _ := png.PNG(256)
	// pngBytes64 用于本次流程后续判断的pngBytes64
	pngBytes64 := base64.StdEncoding.EncodeToString(pngBytes)
	qrCodeURL = "data:image/png;base64," + pngBytes64

	sess.QRCodeURL = qrCodeURL

	m.mu.Lock()
	m.sessions[sessionID] = sess
	m.mu.Unlock()

	// 5. 扫码会话独立于生成二维码的 HTTP 请求，但受会话有效期约束。
	// #nosec G118 -- 后台任务必须跨越原请求，且由超时上下文保证退出。
	go func() {
		// monitorCtx、cancel 用于本次流程后续判断的monitorCtx、cancel
		monitorCtx, cancel := context.WithTimeout(context.Background(), sess.snapshot().expireTime)
		defer cancel()
		m.monitorQRStatus(monitorCtx, sessionID)
	}()

	m.logger.Info("二维码生成成功", "session_id", sessionID)
	return sessionID, qrCodeURL, nil
}

// GetSessionStatus 查询扫码状态。
func (m *Manager) GetSessionStatus(sessionID string) map[string]any {
	m.mu.Lock()
	// sess、ok 用于本次流程后续判断的sess、ok
	sess, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return map[string]any{"status": "not_found"}
	}
	sess.mu.Lock()
	if time.Since(sess.createdTime) > sess.expireTime && sess.Status != "success" {
		sess.Status = "expired"
	}
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := sessionSnapshot{
		status: sess.Status, cookies: cloneCookieMap(sess.cookies), unb: sess.unb,
		cookieSnapshot:  cloneCookieSnapshot(sess.cookieSnapshot),
		verificationURL: sess.verificationURL, verificationScreenshot: sess.verificationScreenshot,
		faceQRURL: sess.faceQRURL,
	}
	sess.mu.Unlock()
	// result 用于本次流程后续判断的结果
	result := map[string]any{
		"status":     snapshot.status,
		"session_id": sessionID,
	}
	if snapshot.status == "verification_required" && snapshot.verificationURL != "" {
		result["verification_url"] = snapshot.verificationURL
		result["message"] = "账号被风控，需要手机验证"
		if snapshot.faceQRURL != "" {
			result["face_qr_url"] = snapshot.faceQRURL
			result["message"] = "需要人脸验证，请使用手机闲鱼扫描二维码"
		}
		if snapshot.verificationScreenshot != "" {
			result["verification_screenshot"] = snapshot.verificationScreenshot
		}
	}
	if snapshot.status == "success" && snapshot.cookies != nil && snapshot.unb != "" {
		result["cookies"] = snapshotCookieHeader(snapshot, qrVerifyTargetURL)
		result["unb"] = snapshot.unb
		if snapshot.cookieSnapshot != nil {
			result["cookie_snapshot"] = cloneCookieSnapshot(snapshot.cookieSnapshot)
		}
	}
	if snapshot.status == "error" {
		result["message"] = "扫码登录接口连续返回异常，请刷新二维码重试"
	}
	return result
}

// DeleteSession 主动释放终态/过期扫码会话中的二维码、Cookie 和验证截图。
func (m *Manager) DeleteSession(sessionID string) {
	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()
}

// monitorQRStatus 后台轮询扫码状态。
func (m *Manager) monitorQRStatus(ctx context.Context, sessionID string) {
	m.mu.Lock()
	// sess 用于本次流程后续判断的sess
	sess := m.sessions[sessionID]
	m.mu.Unlock()
	if sess == nil {
		return
	}

	// maxWait 用于本次流程后续判断的maxWait
	maxWait := 5 * time.Minute
	// start 用于本次流程后续判断的开始
	start := time.Now()
	// serverErrors 用于本次流程后续判断的server错误列表
	serverErrors := 0

	for time.Since(start) < maxWait {
		select {
		case <-ctx.Done():
			return
		default:
		}

		m.mu.Lock()
		if // ok 用于本次流程后续判断的ok
		_, ok := m.sessions[sessionID]; !ok {
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()

		// resp、err 用于本次流程后续判断的resp、err
		resp, err := m.pollQRCodeStatus(ctx, sess)
		if err != nil {
			m.logger.Error("轮询扫码状态异常", "err", err)
			if !waitQRRetry(ctx, qrPollInterval) {
				return
			}
			continue
		}
		absorbSessionResponse(sess, apiScanStatus, resp)
		// body、readErr 用于本次流程后续判断的body、readErr
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		// closeErr 用于本次流程后续判断的closeErr
		closeErr := resp.Body.Close()
		if readErr != nil || closeErr != nil {
			m.logger.Warn("读取扫码状态响应失败", "read_err", readErr, "close_err", closeErr)
			if !waitQRRetry(ctx, qrPollInterval) {
				return
			}
			continue
		}

		// qrResult 用于本次流程后续判断的qr结果
		var qrResult struct {
			HasError bool `json:"hasError"`
			Content  struct {
				Data struct {
					QRCodeStatus      string `json:"qrCodeStatus"`
					IframeRedirect    bool   `json:"iframeRedirect"`
					IframeRedirectURL string `json:"iframeRedirectUrl"`
				} `json:"data"`
			} `json:"content"`
		}
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal(body, &qrResult); err != nil {
			m.logger.Warn("解析扫码状态响应失败", "err", err)
			if !waitQRRetry(ctx, qrPollInterval) {
				return
			}
			continue
		}
		if qrResult.HasError {
			serverErrors++
			if serverErrors >= maxQRServerErrors {
				sess.mu.Lock()
				sess.Status = "error"
				sess.mu.Unlock()
				m.logger.Warn("扫码登录接口连续返回异常", "session_id", sessionID, "failures", serverErrors)
				return
			}
			// 官网脚本对业务层 hasError 立即重试，最多五次。
			continue
		}
		serverErrors = 0

		// status 用于本次流程后续判断的状态
		status := qrResult.Content.Data.QRCodeStatus
		switch status {
		case "CONFIRMED":
			m.handleConfirmedQRStatus(ctx, sess, sessionID, qrResult.Content.Data.IframeRedirect, qrResult.Content.Data.IframeRedirectURL)
			return
		case "NEW":
			// 未扫码，继续
		case "EXPIRED":
			sess.mu.Lock()
			sess.Status = "expired"
			sess.mu.Unlock()
			m.logger.Info("二维码已过期", "session_id", sessionID)
			return
		case "SCANED":
			sess.mu.Lock()
			if sess.Status == "waiting" {
				sess.Status = "scanned"
				m.logger.Info("二维码已扫描，等待确认", "session_id", sessionID)
			}
			sess.mu.Unlock()
		case "CANCELED":
			sess.mu.Lock()
			sess.Status = "cancelled"
			sess.mu.Unlock()
			m.logger.Info("用户取消登录", "session_id", sessionID)
			return
		case "ERROR":
			sess.mu.Lock()
			sess.Status = "error"
			sess.mu.Unlock()
			m.logger.Warn("扫码登录接口返回错误状态", "session_id", sessionID)
			return
		default:
			// 官网脚本对未识别状态按普通未扫码状态处理，等待下一轮，
			// 不能擅自推断成用户取消。
		}
		if !waitQRRetry(ctx, qrPollInterval) {
			return
		}
	}

	// 超时
	sess.mu.Lock()
	if sess.Status != "success" && sess.Status != "expired" && sess.Status != "cancelled" {
		sess.Status = "expired"
	}
	sess.mu.Unlock()
}

// completeConfirmedLogin 对齐二维码组件确认后的顶层页面跳转。专用 Cookie
// Jar 会捕获自动重定向链上的全部 Set-Cookie，并保留 Domain/Path/HttpOnly。
// completeConfirmedLogin 封装completeConfirmed登录业务协调。
func (m *Manager) completeConfirmedLogin(ctx context.Context, sess *Session) error {
	if sess == nil {
		return errors.New("扫码会话为空")
	}
	// state 用于本次流程后续判断的状态
	state := sess.snapshot()
	// client、jar、err 用于本次流程后续判断的client、jar、err
	client, jar, err := m.faceHTTPClient(state.cookies, state.cookieSnapshot, apiScanStatus, qrVerifyTargetURL)
	if err != nil {
		return fmt.Errorf("创建扫码完成 Cookie Jar: %w", err)
	}
	// req、err 用于本次流程后续判断的req、err
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, qrVerifyTargetURL, nil)
	if err != nil {
		return fmt.Errorf("创建扫码完成请求: %w", err)
	}
	m.setDocumentHeaders(req)
	// resp、err 用于本次流程后续判断的resp、err
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("访问闲鱼消息页完成登录: %w", err)
	}
	defer resp.Body.Close()
	if // err 用于本次流程后续判断的err
	_, err := io.Copy(io.Discard, io.LimitReader(resp.Body, 2<<20)); err != nil {
		return fmt.Errorf("读取扫码完成响应: %w", err)
	}

	// urls 用于本次流程后续判断的urls
	urls := []*url.URL{mustParseURL(qrVerifyTargetURL)}
	if resp.Request != nil && resp.Request.URL != nil {
		urls = append(urls, resp.Request.URL)
	}
	// finalCookies 用于本次流程后续判断的finalCookies
	finalCookies := collectJarCookies(jar, urls...)
	if finalCookies["unb"] == "" {
		return errors.New("扫码完成跳转未保留账号标识")
	}
	// finalSnapshot、snapshotComplete 用于本次流程后续判断的finalSnapshot、snapshotComplete
	finalSnapshot, snapshotComplete := jar.Snapshot()
	sess.mu.Lock()
	sess.cookies = finalCookies
	if snapshotComplete {
		sess.cookieSnapshot = finalSnapshot
	} else {
		sess.cookieSnapshot = nil
	}
	sess.unb = finalCookies["unb"]
	sess.mu.Unlock()
	return nil
}

// enableConfirmedLongLogin 对齐官网账号安全页的“保持登录”开关：先提交
// status=0，再查询 hasLongTokenLogin，并保存两次响应更新后的完整 Cookie Jar。
// enableConfirmedLongLogin 封装enableConfirmedLong登录业务协调。
func (m *Manager) enableConfirmedLongLogin(ctx context.Context, sess *Session) error {
	if sess == nil {
		return errors.New("扫码会话为空")
	}
	// state 用于本次流程后续判断的状态
	state := sess.snapshot()
	// service 用于本次流程后续判断的service
	service := xrenew.Service{HTTPClient: m.httpc, DocumentReferer: qrVerifyTargetURL}
	// settings 用于本次流程后续判断的设置
	var settings *xrenew.LongLoginSettings
	// err 用于本次流程后续判断的err
	var err error
	if state.cookieSnapshot != nil {
		settings, err = service.SetLongLoginSettings(ctx, cookieMarshal(state.cookies), true, state.cookieSnapshot)
	} else {
		settings, err = service.SetLongLoginSettings(ctx, cookieMarshal(state.cookies), true)
	}
	if settings != nil {
		sess.mu.Lock()
		if settings.CookieSnapshotComplete {
			sess.cookieSnapshot = cookierefresh.NormalizeSnapshot(settings.CookieSnapshot)
			if sess.cookieSnapshot == nil {
				sess.cookieSnapshot = []cookierefresh.BrowserCookie{}
			}
			finalizeSessionCredentialsLocked(sess)
		} else if strings.TrimSpace(settings.NewCookies) != "" {
			sess.cookies = parseCookieStr(settings.NewCookies)
			if // unb 用于本次流程后续判断的unb
			unb := sess.cookies["unb"]; unb != "" {
				sess.unb = unb
			}
		}
		sess.mu.Unlock()
	}
	if err != nil {
		return err
	}
	if settings == nil || !settings.CanOpenLongLogin || !settings.Enabled {
		return errors.New("官网未确认当前会话已开启保持登录")
	}
	return nil
}

// runGoVerification 封装运行GoVerification业务协调。
func (m *Manager) runGoVerification(ctx context.Context, sessionID, verificationURL string) {
	if !strings.Contains(verificationURL, "/iv/") && !strings.Contains(verificationURL, "identity_verify") {
		m.logger.Warn("扫码验证地址不属于已支持的人脸流程，保持人工验证状态", "session_id", sessionID, "verification_url", logsafe.URL(verificationURL))
		return
	}
	if // err 用于本次流程后续判断的err
	err := m.runFaceVerification(ctx, sessionID, verificationURL); err != nil {
		m.logger.Warn("扫码人脸验证 Go HTTP 流程未完成，保持人工验证状态", "session_id", sessionID, "err", err)
	}
}

// waitQRRetry 封装waitQR重试业务协调。
func waitQRRetry(ctx context.Context, delay time.Duration) bool {
	// timer 用于本次流程后续判断的定时器
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// CompleteVerification 用户完成风控验证后调用。整个凭证换取过程只使用
// Go Cookie Jar；不得导航浏览器页面或通过 DOM 判断登录状态。
// CompleteVerification 封装CompleteVerification业务协调。
