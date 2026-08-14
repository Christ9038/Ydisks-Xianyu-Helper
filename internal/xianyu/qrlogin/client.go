// Package qrlogin 使用纯 Go HTTP 复刻闲鱼扫码登录与人脸验证流程。
package qrlogin

import (
	"context"
	"crypto/md5"
	rand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"xianyu-go/internal/xianyu/cookierefresh"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// maxQRResponseBytes 保存maxQR响应Bytes，供当前处理流程使用
const (
	maxQRResponseBytes = 2 << 20
	qrPollInterval     = 2 * time.Second
	maxQRServerErrors  = 5
	qrTopSite          = "https://goofish.com"
)

// host 保存host，供当前处理流程使用
const (
	host          = "https://passport.goofish.com"
	apiMiniLogin  = host + "/mini_login.htm"
	apiGenerateQR = host + "/newlogin/qrcode/generate.do"
	apiScanStatus = host + "/newlogin/qrcode/query.do"
	apiFaceCheck  = host + "/iv/photoVerify/check.do"
	apiH5TK       = "https://h5api.m.goofish.com/h5/mtop.gaia.nodejs.gaia.idle.data.gw.v2.index.get/1.0/"
	appKey        = "34839810"
)

// qrVerifyTargetURL 保存qrVerifyTargetURL，供当前处理流程使用
var qrVerifyTargetURL = "https://www.goofish.com/im"

// qrHeaders 保存qrHeaders，供当前处理流程使用
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

// isExpired 负责isExpired相关处理。
func (s *Session) isExpired() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.createdTime) > s.expireTime
}

// sessionSnapshot 保存会话Snapshot，供当前处理流程使用
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

// snapshot 负责snapshot相关处理。
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
	// sess 保存sess，供当前处理流程使用
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
	// q 保存q，供当前处理流程使用
	q := req.URL.Query()
	// k、v 表示当前遍历过程中的k、v
	for k, v := range loginParams {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()
	m.setHeaders(req)
	if // cookieStr 保存登录凭证Str，供当前处理流程使用
	cookieStr := sessionCookieHeader(sess, req.URL.String()); cookieStr != "" {
		req.Header.Set("Cookie", cookieStr)
	}

	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := m.httpc.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("请求二维码接口失败: %w", err)
	}
	defer resp.Body.Close()
	absorbSessionResponse(sess, apiGenerateQR, resp)
	// body、err 保存body、err，供当前处理流程使用
	body, err := readQRBody(resp.Body)
	if err != nil {
		return "", "", err
	}

	// result 保存结果，供当前处理流程使用
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
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(body, &result); err != nil {
		return "", "", fmt.Errorf("解析二维码响应失败: %w (body=%s)", err, truncate(string(body), 300))
	}
	if !result.Content.Success {
		return "", "", fmt.Errorf("获取登录二维码失败 (body=%s)", truncate(string(body), 300))
	}

	// t 是毫秒时间戳数字，必须转成纯数字字符串（不能用科学计数法）。
	var tStr string
	switch // tv 保存tv，供当前处理流程使用
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
	// pngBytes 保存pngBytes，供当前处理流程使用
	pngBytes, _ := png.PNG(256)
	// pngBytes64 保存pngBytes64，供当前处理流程使用
	pngBytes64 := base64.StdEncoding.EncodeToString(pngBytes)
	qrCodeURL = "data:image/png;base64," + pngBytes64

	sess.QRCodeURL = qrCodeURL

	m.mu.Lock()
	m.sessions[sessionID] = sess
	m.mu.Unlock()

	// 5. 扫码会话独立于生成二维码的 HTTP 请求，但受会话有效期约束。
	// #nosec G118 -- 后台任务必须跨越原请求，且由超时上下文保证退出。
	go func() {
		// monitorCtx、cancel 保存monitorCtx、cancel，供当前处理流程使用
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
	// sess、ok 保存sess、ok，供当前处理流程使用
	sess, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return map[string]any{"status": "not_found"}
	}
	sess.mu.Lock()
	if time.Since(sess.createdTime) > sess.expireTime && sess.Status != "success" {
		sess.Status = "expired"
	}
	// snapshot 保存snapshot，供当前处理流程使用
	snapshot := sessionSnapshot{
		status: sess.Status, cookies: cloneCookieMap(sess.cookies), unb: sess.unb,
		cookieSnapshot:  cloneCookieSnapshot(sess.cookieSnapshot),
		verificationURL: sess.verificationURL, verificationScreenshot: sess.verificationScreenshot,
		faceQRURL: sess.faceQRURL,
	}
	sess.mu.Unlock()
	// result 保存结果，供当前处理流程使用
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
	// sess 保存sess，供当前处理流程使用
	sess := m.sessions[sessionID]
	m.mu.Unlock()
	if sess == nil {
		return
	}

	// maxWait 保存maxWait，供当前处理流程使用
	maxWait := 5 * time.Minute
	// start 保存开始，供当前处理流程使用
	start := time.Now()
	// serverErrors 保存server错误列表，供当前处理流程使用
	serverErrors := 0

	for time.Since(start) < maxWait {
		select {
		case <-ctx.Done():
			return
		default:
		}

		m.mu.Lock()
		if // ok 保存ok，供当前处理流程使用
		_, ok := m.sessions[sessionID]; !ok {
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()

		// resp、err 保存resp、err，供当前处理流程使用
		resp, err := m.pollQRCodeStatus(ctx, sess)
		if err != nil {
			m.logger.Error("轮询扫码状态异常", "err", err)
			if !waitQRRetry(ctx, qrPollInterval) {
				return
			}
			continue
		}
		absorbSessionResponse(sess, apiScanStatus, resp)
		// body、readErr 保存body、readErr，供当前处理流程使用
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		// closeErr 保存closeErr，供当前处理流程使用
		closeErr := resp.Body.Close()
		if readErr != nil || closeErr != nil {
			m.logger.Warn("读取扫码状态响应失败", "read_err", readErr, "close_err", closeErr)
			if !waitQRRetry(ctx, qrPollInterval) {
				return
			}
			continue
		}

		// qrResult 保存qr结果，供当前处理流程使用
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
		if // err 保存err，供当前处理流程使用
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

		// status 保存状态，供当前处理流程使用
		status := qrResult.Content.Data.QRCodeStatus
		switch status {
		case "CONFIRMED":
			if qrResult.Content.Data.IframeRedirect {
				// 风控验证：记录状态和 URL，提取响应 cookie（临时 cookie），
				// 验证完成后用这些临时 cookie 访问 goofish.com/im 换真实 cookie。
				sess.mu.Lock()
				if sess.Status == "success" {
					sess.mu.Unlock()
					return
				}
				if sess.Status != "verification_required" {
					sess.Status = "verification_required"
					sess.verificationURL = qrResult.Content.Data.IframeRedirectURL
					// 已有权威快照交给可导出的 Go Cookie Jar；它会继续吸收
					// 人脸跳转链每个重定向响应的 Set-Cookie。
					// 人脸验证会额外占用用户手机端操作时间。这里重置窗口，避免普通
					// 扫码 5 分钟窗口在用户扫人脸二维码时把会话误标为 expired。
					sess.createdTime = time.Now()
					sess.expireTime = 5 * time.Minute
					// verURL 保存verURL，供当前处理流程使用
					verURL := sess.verificationURL
					// expireTime 保存expire时间，供当前处理流程使用
					expireTime := sess.expireTime
					// cookieCount 保存登录凭证数量，供当前处理流程使用
					cookieCount := len(sess.cookies)
					sess.mu.Unlock()
					m.logger.Warn("扫码登录需要风控验证，使用 Go HTTP 保持原登录会话", "session_id", sessionID, "verification_url", logsafe.URL(verURL), "tmp_cookie_count", cookieCount)
					// #nosec G118 -- 验证必须跨越轮询请求，且由独立五分钟上下文保证退出。
					go func() {
						// verifyCtx、cancel 保存verifyCtx、cancel，供当前处理流程使用
						verifyCtx, cancel := context.WithTimeout(context.Background(), expireTime)
						defer cancel()
						m.runGoVerification(verifyCtx, sessionID, verURL)
					}()
				} else {
					sess.mu.Unlock()
				}
				return
			}
			// 二维码组件确认成功后，真实网页还会进入 /im 并跟随登录
			// 重定向。部分账号的长登录 Cookie 只在这一步下发，不能只
			// 保存 query.do 的响应头。
			if // err 保存err，供当前处理流程使用
			err := m.completeConfirmedLogin(ctx, sess); err != nil {
				m.logger.Warn("扫码确认后的官网登录跳转未完成，保留当前登录凭证", "session_id", sessionID, "err", err)
			}
			if // err 保存err，供当前处理流程使用
			err := m.enableConfirmedLongLogin(ctx, sess); err != nil {
				m.logger.Warn("扫码登录已成功，但官网保持登录开启失败", "session_id", sessionID, "err", err)
			}
			sess.mu.Lock()
			sess.Status = "success"
			finalizeSessionCredentialsLocked(sess)
			// unb 保存unb，供当前处理流程使用
			unb := sess.unb
			// cookieCount 保存登录凭证数量，供当前处理流程使用
			cookieCount := len(sess.cookies)
			// hasHavanaLongLogin 保存hasHavanaLong登录，供当前处理流程使用
			hasHavanaLongLogin := sess.cookies["havana_lgc_exp"] != ""
			// hasCookie3Backup 保存hasCookie3Backup，供当前处理流程使用
			hasCookie3Backup := sess.cookies["cookie3_bak_exp"] != ""
			// snapshotComplete 保存snapshotComplete，供当前处理流程使用
			snapshotComplete := sess.cookieSnapshot != nil
			sess.mu.Unlock()
			m.logger.Info("扫码登录成功", "session_id", sessionID, "account_hash", logsafe.ID(unb),
				"cookie_count", cookieCount, "cookie_snapshot_complete", snapshotComplete,
				"has_havana_lgc_exp", hasHavanaLongLogin, "has_cookie3_bak_exp", hasCookie3Backup)
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
// completeConfirmedLogin 负责completeConfirmed登录相关处理。
func (m *Manager) completeConfirmedLogin(ctx context.Context, sess *Session) error {
	if sess == nil {
		return errors.New("扫码会话为空")
	}
	// state 保存状态，供当前处理流程使用
	state := sess.snapshot()
	// client、jar、err 保存client、jar、err，供当前处理流程使用
	client, jar, err := m.faceHTTPClient(state.cookies, state.cookieSnapshot, apiScanStatus, qrVerifyTargetURL)
	if err != nil {
		return fmt.Errorf("创建扫码完成 Cookie Jar: %w", err)
	}
	// req、err 保存req、err，供当前处理流程使用
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, qrVerifyTargetURL, nil)
	if err != nil {
		return fmt.Errorf("创建扫码完成请求: %w", err)
	}
	m.setDocumentHeaders(req)
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("访问闲鱼消息页完成登录: %w", err)
	}
	defer resp.Body.Close()
	if // err 保存err，供当前处理流程使用
	_, err := io.Copy(io.Discard, io.LimitReader(resp.Body, 2<<20)); err != nil {
		return fmt.Errorf("读取扫码完成响应: %w", err)
	}

	// urls 保存urls，供当前处理流程使用
	urls := []*url.URL{mustParseURL(qrVerifyTargetURL)}
	if resp.Request != nil && resp.Request.URL != nil {
		urls = append(urls, resp.Request.URL)
	}
	// finalCookies 保存finalCookies，供当前处理流程使用
	finalCookies := collectJarCookies(jar, urls...)
	if finalCookies["unb"] == "" {
		return errors.New("扫码完成跳转未保留账号标识")
	}
	// finalSnapshot、snapshotComplete 保存finalSnapshot、snapshotComplete，供当前处理流程使用
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
// enableConfirmedLongLogin 负责enableConfirmedLong登录相关处理。
func (m *Manager) enableConfirmedLongLogin(ctx context.Context, sess *Session) error {
	if sess == nil {
		return errors.New("扫码会话为空")
	}
	// state 保存状态，供当前处理流程使用
	state := sess.snapshot()
	// service 保存service，供当前处理流程使用
	service := xrenew.Service{HTTPClient: m.httpc, DocumentReferer: qrVerifyTargetURL}
	// settings 保存设置，供当前处理流程使用
	var settings *xrenew.LongLoginSettings
	// err 保存err，供当前处理流程使用
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
			if // unb 保存unb，供当前处理流程使用
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

// runGoVerification 负责运行GoVerification相关处理。
func (m *Manager) runGoVerification(ctx context.Context, sessionID, verificationURL string) {
	if !strings.Contains(verificationURL, "/iv/") && !strings.Contains(verificationURL, "identity_verify") {
		m.logger.Warn("扫码验证地址不属于已支持的人脸流程，保持人工验证状态", "session_id", sessionID, "verification_url", logsafe.URL(verificationURL))
		return
	}
	if // err 保存err，供当前处理流程使用
	err := m.runFaceVerification(ctx, sessionID, verificationURL); err != nil {
		m.logger.Warn("扫码人脸验证 Go HTTP 流程未完成，保持人工验证状态", "session_id", sessionID, "err", err)
	}
}

// waitQRRetry 负责waitQR重试相关处理。
func waitQRRetry(ctx context.Context, delay time.Duration) bool {
	// timer 保存定时器，供当前处理流程使用
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
// CompleteVerification 负责CompleteVerification相关处理。
func (m *Manager) CompleteVerification(ctx context.Context, sessionID string) (cookies string, unb string, err error) {
	m.mu.Lock()
	// sess、ok 保存sess、ok，供当前处理流程使用
	sess, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return "", "", fmt.Errorf("会话不存在")
	}
	// state 保存状态，供当前处理流程使用
	state := sess.snapshot()
	if len(state.cookies) == 0 {
		return "", "", fmt.Errorf("无扫码临时 cookie")
	}
	if state.status == "success" && state.unb != "" {
		return snapshotCookieHeader(state, qrVerifyTargetURL), state.unb, nil
	}
	m.logger.Info("开始用临时 cookie 换取真实 cookie", "session_id", sessionID, "tmp_cookie_count", len(state.cookies))

	// targetURL 保存targetURL，供当前处理流程使用
	targetURL := qrVerifyTargetURL
	// seedURL 保存seedURL，供当前处理流程使用
	seedURL := state.verificationURL
	if strings.TrimSpace(seedURL) == "" {
		seedURL = targetURL
	}
	// jarClient、jar、err 保存jarClient、jar、err，供当前处理流程使用
	jarClient, jar, err := m.faceHTTPClient(state.cookies, state.cookieSnapshot, seedURL, targetURL)
	if err != nil {
		return "", "", fmt.Errorf("创建登录 Cookie Jar: %w", err)
	}
	jarClient.Timeout = 30 * time.Second
	// target、err 保存target、err，供当前处理流程使用
	target, err := url.Parse(targetURL)
	if err != nil {
		return "", "", fmt.Errorf("解析登录凭证换取地址: %w", err)
	}
	// req、err 保存req、err，供当前处理流程使用
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", "", err
	}
	m.setDocumentHeaders(req)
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := jarClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("访问 goofish.com/im 换取登录凭证失败: %w", err)
	}
	defer resp.Body.Close()
	if // err 保存err，供当前处理流程使用
	_, err := io.Copy(io.Discard, resp.Body); err != nil {
		return "", "", fmt.Errorf("读取登录凭证换取响应失败: %w", err)
	}
	// responseURL 保存响应URL，供当前处理流程使用
	var responseURL *url.URL
	if resp.Request != nil {
		responseURL = resp.Request.URL
	}
	// finalCookies 保存finalCookies，供当前处理流程使用
	finalCookies := collectJarCookies(jar, target, responseURL)
	// finalUNB 保存finalUNB，供当前处理流程使用
	finalUNB := finalCookies["unb"]
	if finalUNB == "" {
		return "", "", fmt.Errorf("纯 Go 登录凭证换取未获取到 unb，验证可能尚未完成或临时 Cookie 已失效")
	}
	sess.mu.Lock()
	sess.cookies = finalCookies
	if // finalSnapshot、complete 保存finalSnapshot、complete，供当前处理流程使用
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
	// m 保存m，供当前处理流程使用
	m := make(map[string]string)
	// part 表示当前遍历过程中的part
	for _, part := range strings.Split(s, "; ") {
		if // eq 保存eq，供当前处理流程使用
		eq := strings.Index(part, "="); eq >= 0 {
			m[part[:eq]] = part[eq+1:]
		}
	}
	return m
}

// getMH5TK 获取 m_h5_tk。
func (m *Manager) getMH5TK(ctx context.Context, sess *Session) error {
	// req 保存req，供当前处理流程使用
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiH5TK, nil)
	m.setHeaders(req)

	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := m.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	absorbSessionResponse(sess, apiH5TK, resp)
	if // err 保存err，供当前处理流程使用
	_, err := io.Copy(io.Discard, resp.Body); err != nil {
		return err
	}
	// mH5TK 保存mH5TK，供当前处理流程使用
	mH5TK := protocolCookieValue(sessionCookieHeader(sess, apiH5TK), "_m_h5_tk")
	// token 保存令牌，供当前处理流程使用
	token := ""
	if // parts 保存parts，供当前处理流程使用
	parts := strings.SplitN(mH5TK, "_", 2); len(parts) > 0 {
		token = parts[0]
	}

	// t 保存t，供当前处理流程使用
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// dataStr 保存数据Str，供当前处理流程使用
	dataStr := `{"bizScene":"home"}`
	// signInput 保存signInput，供当前处理流程使用
	signInput := token + "&" + t + "&" + appKey + "&" + dataStr
	// sign 保存sign，供当前处理流程使用
	sign := md5hex(signInput)

	// params 保存params，供当前处理流程使用
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

	// req2 保存req2，供当前处理流程使用
	req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiH5TK+"?"+params.Encode(), nil)
	m.setHeaders(req2)
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// cookieStr 保存登录凭证Str，供当前处理流程使用
	cookieStr := sessionCookieHeader(sess, req2.URL.String())
	if cookieStr != "" {
		req2.Header.Set("Cookie", cookieStr)
	}

	// resp2、err 保存resp2、err，供当前处理流程使用
	resp2, err := m.httpc.Do(req2)
	if err != nil {
		return err
	}
	defer resp2.Body.Close()
	absorbSessionResponse(sess, req2.URL.String(), resp2)
	if // err 保存err，供当前处理流程使用
	_, err := io.Copy(io.Discard, resp2.Body); err != nil {
		return err
	}

	return nil
}

// getLoginParams 获取登录表单参数。
func (m *Manager) getLoginParams(ctx context.Context, sess *Session) (map[string]string, error) {
	// params 保存params，供当前处理流程使用
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

	// req 保存req，供当前处理流程使用
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiMiniLogin+"?"+params.Encode(), nil)
	m.setHeaders(req)

	// 带上已有 cookie。
	cookieStr := sessionCookieHeader(sess, req.URL.String())
	if cookieStr != "" {
		req.Header.Set("Cookie", cookieStr)
	}

	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := m.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	absorbSessionResponse(sess, req.URL.String(), resp)
	// body、err 保存body、err，供当前处理流程使用
	body, err := readQRBody(resp.Body)
	if err != nil {
		return nil, err
	}

	// 调试：打印响应状态和 body 前 200 字符

	// 从 HTML 里提取 window.viewData = {...};
	re := regexp.MustCompile(`window\.viewData\s*=\s*(\{.*?\});`)
	// match 保存match，供当前处理流程使用
	match := re.FindSubmatch(body)
	if match == nil {
		return nil, fmt.Errorf("获取登录参数失败：未找到 viewData")
	}

	// viewData 保存view数据，供当前处理流程使用
	var viewData struct {
		LoginFormData map[string]any `json:"loginFormData"`
	}
	if // err 保存err，供当前处理流程使用
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
	// form 保存表单，供当前处理流程使用
	form := url.Values{}
	// state 保存状态，供当前处理流程使用
	state := sess.snapshot()
	// k、v 表示当前遍历过程中的k、v
	for k, v := range state.params {
		form.Set(k, v)
	}
	// fingerprint 保存fingerprint，供当前处理流程使用
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

	// req 保存req，供当前处理流程使用
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiScanStatus, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	m.setHeaders(req)
	// cookieStr 保存登录凭证Str，供当前处理流程使用
	cookieStr := sessionCookieHeader(sess, req.URL.String())
	if cookieStr != "" {
		req.Header.Set("Cookie", cookieStr)
	}

	return m.httpc.Do(req)
}

// navigatorPlatform 负责navigatorPlatform相关处理。
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

// setHeaders 负责setHeaders相关处理。
func (m *Manager) setHeaders(req *http.Request) {
	xianyu.ApplyBrowserFingerprint(req.Header)
	// k、v 表示当前遍历过程中的k、v
	for k, v := range qrHeaders {
		req.Header.Set(k, v)
	}
}

// setDocumentHeaders 复刻浏览器从闲鱼首页进入 /im 的文档请求头。这里只
// 发送 HTTP 请求并接收 Set-Cookie，不加载或校验任何页面 DOM。
// setDocumentHeaders 负责setDocumentHeaders相关处理。
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

// sessionCookieHeader 负责会话登录凭证Header相关处理。
func sessionCookieHeader(sess *Session, requestURL string) string {
	if sess == nil {
		return ""
	}
	return snapshotCookieHeader(sess.snapshot(), requestURL)
}

// snapshotCookieHeader 负责snapshot登录凭证Header相关处理。
func snapshotCookieHeader(state sessionSnapshot, requestURL string) string {
	if state.cookieSnapshot != nil {
		if // value、authoritative 保存value、authoritative，供当前处理流程使用
		value, authoritative := cookierefresh.ScopedCookieHeaderForRequest(
			state.cookieSnapshot, requestURL, qrTopSite, time.Now(),
		); authoritative {
			return value
		}
	}
	return cookieMarshal(state.cookies)
}

// absorbSessionResponse 负责absorb会话响应相关处理。
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

// finalizeSessionCredentialsLocked 负责finalize会话CredentialsLocked相关处理。
func finalizeSessionCredentialsLocked(sess *Session) {
	if sess == nil {
		return
	}
	if sess.cookieSnapshot != nil {
		// value 保存值，供当前处理流程使用
		value, _ := cookierefresh.ScopedCookieHeaderForRequest(
			sess.cookieSnapshot, qrVerifyTargetURL, qrTopSite, time.Now(),
		)
		sess.cookies = parseCookieStr(value)
	}
	if // unb 保存unb，供当前处理流程使用
	unb := sess.cookies["unb"]; unb != "" {
		sess.unb = unb
	}
}

// cloneCookieSnapshot 负责clone登录凭证Snapshot相关处理。
func cloneCookieSnapshot(in []cookierefresh.BrowserCookie) []cookierefresh.BrowserCookie {
	if in == nil {
		return nil
	}
	// out 保存out，供当前处理流程使用
	out := cookierefresh.NormalizeSnapshot(in)
	if out == nil {
		return []cookierefresh.BrowserCookie{}
	}
	return out
}

// protocolCookieValue 负责protocol登录凭证值相关处理。
func protocolCookieValue(cookieHeader, name string) string {
	// part 表示当前遍历过程中的part
	for _, part := range strings.Split(cookieHeader, ";") {
		// key、value、ok 保存key、value、ok，供当前处理流程使用
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && key == name {
			return value
		}
	}
	return ""
}

// ---- 工具函数 ----

// md5hex 负责md5hex相关处理。
func md5hex(s string) string {
	// #nosec G401 -- 闲鱼登录协议明确要求 MD5，不能替换为其他摘要算法。
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// cookieMarshal 负责登录凭证Marshal相关处理。
func cookieMarshal(cookies map[string]string) string {
	// parts 保存parts，供当前处理流程使用
	parts := make([]string, 0, len(cookies))
	// k、v 表示当前遍历过程中的k、v
	for k, v := range cookies {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}

// randomUUID 负责randomUUID相关处理。
func randomUUID() (string, error) {
	// b 保存b，供当前处理流程使用
	b := make([]byte, 16)
	if // err 保存err，供当前处理流程使用
	_, err := io.ReadFull(randReader, b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// randReader 保存randReader，供当前处理流程使用
var randReader io.Reader = rand.Reader

// randFloat 保存randFloat，供当前处理流程使用
var randFloat = func() float64 { return float64(time.Now().UnixNano()%1e9) / 1e9 }

// readQRBody 负责readQR请求体相关处理。
func readQRBody(r io.Reader) ([]byte, error) {
	// body、err 保存body、err，供当前处理流程使用
	body, err := io.ReadAll(io.LimitReader(r, maxQRResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxQRResponseBytes {
		return nil, fmt.Errorf("扫码登录响应体超过 %d MiB", maxQRResponseBytes>>20)
	}
	return body, nil
}

// truncate 负责truncate相关处理。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
