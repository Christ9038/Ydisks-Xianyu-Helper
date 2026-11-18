package qrlogin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/cookierefresh"
)

// ErrCredentialVerificationInvalid 表示创建凭证验证会话所需的输入不完整或验证地址不可信。
var ErrCredentialVerificationInvalid = errors.New("凭证验证输入无效")

// StartCredentialVerification 为“定时续期被平台要求安全验证”的已有账号创建一个人工验证会话。
//
// cookieHeader 与 snapshot 是账号当前凭证（snapshot 为 nil 表示没有完整快照，改用扁平 Cookie 播种 Jar），
// verificationURL 是续期响应给出的验证地址。会话直接进入与扫码确认后风控分流相同的
// verification_required 分支：支持的 /iv/ 人脸流程会生成 face_qr_url 供用户手机扫码，
// 其他形态保持人工验证状态，由用户在平台完成后调用 CompleteVerification 换取凭证。
// 本方法不发起任何网络请求；验证任务沿用会话根 Context，由 Manager 统一取消和等待。
// 返回的会话标识交给既有 /qr-login/status 与 /qr-login/complete-verification 接口继续使用。
func (m *Manager) StartCredentialVerification(ctx context.Context, cookieHeader string, snapshot []cookierefresh.BrowserCookie, verificationURL string) (string, error) {
	if m == nil {
		return "", errors.New("二维码管理器未初始化")
	}
	if ctx != nil {
		// err 是 ctx.Err 返回的失败原因，用于断言成功或被拒绝的路径。
		if err := ctx.Err(); err != nil {
			return "", err
		}
	}
	// target 是去除空白后的验证地址；必须是 https 且带主机名，避免被诱导访问非预期协议。
	target := strings.TrimSpace(verificationURL)
	// parseErr 是 url.Parse 返回的失败原因，用于断言成功或被拒绝的路径；parsed 保存 url.Parse 的结果，供后续分支和断言使用。
	parsed, parseErr := url.Parse(target)
	if parseErr != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return "", fmt.Errorf("%w: 验证地址不是有效的 https 地址", ErrCredentialVerificationInvalid)
	}
	// cookies 是从扁平 Cookie 头还原的 Cookie 表，作为验证会话的“临时 Cookie”。
	cookies := parseCookieStr(strings.TrimSpace(cookieHeader))
	if len(cookies) == 0 && len(snapshot) == 0 {
		return "", fmt.Errorf("%w: 账号当前没有可用凭证", ErrCredentialVerificationInvalid)
	}
	// sessionID 是新建验证会话的随机标识。
	sessionID, err := randomUUID()
	if err != nil {
		return "", fmt.Errorf("生成验证会话 ID: %w", err)
	}
	// sess 复用扫码会话结构，以便 /qr-login/status 与 complete-verification 无需任何改动。
	sess := &Session{
		SessionID:   sessionID,
		Status:      "waiting",
		cookies:     cookies,
		createdTime: time.Now(),
		expireTime:  5 * time.Minute,
		params:      make(map[string]string),
	}
	if snapshot != nil {
		sess.cookieSnapshot = cloneCookieSnapshot(snapshot)
	}

	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return "", errors.New("二维码管理器已关闭")
	}
	// rootCtx 是进程根 Context；构造期默认值保证非空，这里仍兜底避免空指针。
	rootCtx := m.lifecycleCtx
	if rootCtx == nil {
		rootCtx = context.Background()
	}
	// sessionCtx、sessionCancel 把会话绑定到进程根 Context；DeleteSession 和 CloseContext 拥有取消权。
	sessionCtx, sessionCancel := context.WithCancel(rootCtx)
	sess.lifecycleCtx = sessionCtx
	if m.sessions == nil {
		m.sessions = make(map[string]*Session)
	}
	if m.sessionCancels == nil {
		m.sessionCancels = make(map[string]context.CancelFunc)
	}
	m.sessions[sessionID] = sess
	m.sessionCancels[sessionID] = sessionCancel
	m.mu.Unlock()

	// 进入与扫码确认后风控分流完全相同的状态机：登记验证地址、重置有效期并启动人脸验证任务。
	m.handleConfirmedQRStatus(ctx, sess, sessionID, true, target)
	m.logger.Info("已为续期安全验证创建人工验证会话", "session_id", sessionID, "cookie_count", len(cookies), "snapshot_complete", snapshot != nil)
	return sessionID, nil
}
