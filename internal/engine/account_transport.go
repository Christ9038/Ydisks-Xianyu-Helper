package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/protocol"
)

// acquireToken 为当前账号取得连接级 Token 与其绑定 Cookie 快照，供 WebSocket 注册使用。
func (a *Account) acquireToken(ctx context.Context) (string, string, error) {
	return a.acquireTokenWithMinGap(ctx, false)
}

// acquireRuntimeToken is retained as a compatibility wrapper for focused
// tests and older internal callers. It follows the same fresh-token rule.
// acquireRuntimeToken 封装acquireRuntime令牌业务协调。
func (a *Account) acquireRuntimeToken(ctx context.Context) (string, string, error) {
	return a.acquireFreshConnectionToken(ctx)
}

// acquireTokenWithMinGap 封装acquire令牌WithMinGap业务协调。
func (a *Account) acquireTokenWithMinGap(ctx context.Context, _ bool) (string, string, error) {
	// Invalidate any access token left by an older process/attempt before asking
	// MTOP for the token that will be bound to this connection.
	a.clearTokenCache(ctx)
	return a.refreshToken(ctx)
}

// setLastTokenStatus 封装setLast令牌状态业务协调。
func (c *credentialCoordinator) setLastTokenStatus(status string) {
	// a 是本凭证协调器绑定的账号 facade，持有刷新诊断状态。
	a := c.account
	a.mu.Lock()
	a.lastTokenStatus = status
	a.mu.Unlock()
}

// classifyTokenFailure 封装classify令牌Failure业务协调。
func classifyTokenFailure(err error) string {
	if err == nil {
		return tokenRefreshFailedAPI
	}
	if mtop.IsSessionExpiredErr(err) {
		return tokenRefreshFailedSession
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") || strings.Contains(err.Error(), "超时") {
		return tokenRefreshFailedTimeout
	}
	// msg 用于本次流程后续判断的msg
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "network") || strings.Contains(msg, "connection") || strings.Contains(msg, "请求失败") {
		return tokenRefreshFailedNetwork
	}
	return tokenRefreshFailedAPI
}

// tokenFailureIsNonCounted 封装令牌FailureIsNonCounted业务协调。
func tokenFailureIsNonCounted(status string) bool {
	switch status {
	case tokenRefreshFailedCaptcha, tokenRefreshFailedCaptchaError, tokenRefreshSkippedCooldown:
		return true
	default:
		return false
	}
}

// RuntimeStatus 返回账号当前连接状态的线程安全快照。
func (a *Account) RuntimeStatus() RuntimeStatus {
	a.runtimeMu.Lock()
	// state 是当前运行状态枚举快照。
	state := a.runtimeState
	// message 是当前运行状态说明快照。
	message := a.runtimeMessage
	// connected 是当前连接存在且状态为 online 的快照。
	connected := a.conn != nil && a.runtimeState == RuntimeOnline
	// failures 是连续连接失败次数快照。
	failures := a.connFailures
	// updatedAt 是状态最近更新时间快照。
	updatedAt := a.runtimeUpdatedAt
	a.runtimeMu.Unlock()

	a.mu.Lock()
	// remaining 用于本次流程后续判断的remaining
	remaining := int64(0)
	if !a.tokenExpiresAt.IsZero() {
		remaining = int64(time.Until(a.tokenExpiresAt).Seconds())
		if remaining < 0 {
			remaining = 0
		}
	}
	// status 用于本次流程后续判断的状态
	status := RuntimeStatus{
		State:                 state,
		Message:               message,
		Connected:             connected,
		Failures:              failures,
		UpdatedAt:             updatedAt,
		TokenAcquiredAt:       a.tokenAcquiredAt,
		TokenExpiresAt:        a.tokenExpiresAt,
		TokenRefreshAt:        a.tokenRefreshAt,
		TokenRemainingSeconds: remaining,
		TokenRefreshStatus:    a.lastTokenStatus,
	}
	a.mu.Unlock()
	return status
}

// recordMessageReceived 更新最近收到消息时间，供 messageDispatcher 使用。
func (a *Account) recordMessageReceived(receivedAt time.Time) {
	a.runtimeMu.Lock()
	a.lastMsgReceived = receivedAt
	a.runtimeMu.Unlock()
}

// tokenRetryDelay 封装令牌重试延迟业务协调。
func (a *Account) tokenRetryDelay() time.Duration {
	a.mu.Lock()
	// expiresAt 用于本次流程后续判断的expiresAt
	expiresAt := a.tokenExpiresAt
	// failures 用于本次流程后续判断的failures
	failures := a.tokenFetchFailures
	a.mu.Unlock()
	// delay 用于本次流程后续判断的延迟
	delay := time.Minute
	if failures > 1 {
		delay = 2 * time.Minute
	}
	if !expiresAt.IsZero() && time.Until(expiresAt) <= 2*time.Minute {
		delay = 30 * time.Second
	}
	return delay
}

// notifyTransportReady 封装notifyTransportReady业务协调。
func (a *Account) notifyTransportReady(ctx context.Context) {
	if // handler、ok 用于本次流程后续判断的handler、ok
	handler, ok := a.handler.(transportReadyHandler); ok {
		handler.OnTransportReady(ctx, a.CookieID)
	}
}

// setRuntimeState 封装setRuntime状态业务协调。
func (a *Account) setRuntimeState(state, message string) {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	a.runtimeState = state
	a.runtimeMessage = message
	a.runtimeUpdatedAt = time.Now()
}

// setRuntimeError 封装setRuntime错误业务协调。
func (a *Account) setRuntimeError(ctx context.Context, err error) {
	// msg 用于本次流程后续判断的msg
	msg := strings.ToLower(errString(err))
	a.runtimeMu.Lock()
	// prev 用于本次流程后续判断的prev
	prev := a.runtimeState
	a.runtimeMu.Unlock()
	switch {
	case strings.Contains(msg, "验证"), strings.Contains(msg, "captcha"), strings.Contains(msg, "risk"), strings.Contains(msg, "rgv587"), strings.Contains(msg, "fail_sys_user_validate"):
		a.setRuntimeState(RuntimeVerificationRequired, "闲鱼要求安全验证，请重新扫码并完成验证")
		// 仅在从非验证状态转入时告警一次，避免重复刷屏。
		if prev != RuntimeVerificationRequired {
			a.alertEvent(ctx, EventSecurityVerification, AlertLevelWarn, "闲鱼要求安全验证",
				"账号触发闲鱼风控验证（滑块/短信/人脸等）。系统可能无法自动恢复，请前往后台扫码完成验证。")
		}
	case strings.Contains(msg, "登录凭证已失效"), strings.Contains(msg, "fail_sys_token_exoired"), strings.Contains(msg, "fail_sys_token_expired"), strings.Contains(msg, "cookie 缺少 unb"):
		a.setRuntimeState(RuntimeAuthExpired, "登录凭证已失效，请重新扫码登录")
	default:
		a.setRuntimeState(RuntimeReconnecting, "连接异常，系统将在限速后自动重试")
	}
}

// SendText 通过当前 WebSocket 给买家发送文本消息。
func (a *Account) SendText(ctx context.Context, chatID, toUserID, text string) error {
	return a.outgoing.sendText(ctx, chatID, toUserID, text)
}

// MarkChatRead 向已连接的 WebSocket 上报会话消息已读状态，调用方负责用户所有权校验。
func (a *Account) MarkChatRead(ctx context.Context, chatID string, messageIDs []map[string]any) error {
	// conn、err 保存当前连接快照及不可发送时的生命周期错误。
	conn, _, err := a.outgoing.currentSenderState()
	if err != nil {
		return err
	}
	// readCtx、cancel 为平台上报设置固定超时，取消责任在本方法返回时释放。
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// reader、ok 保存连接是否支持可选的已读上报能力。
	reader, ok := conn.(interface {
		MarkChatRead(context.Context, string, []map[string]any) error
	})
	if !ok {
		return fmt.Errorf("当前 WebSocket 不支持已读上报")
	}
	return reader.MarkChatRead(readCtx, chatID, messageIDs)
}

// SendImage 通过当前 WebSocket 给买家发送图片消息。当前仅支持可直接访问的 CDN/公网 URL。
// width/height 为图片真实尺寸，单位为像素；传入非正值时由 WebSocket 使用默认尺寸。
func (a *Account) SendImage(ctx context.Context, chatID, toUserID, imageURL string, cardID int64, width, height int) error {
	return a.outgoing.sendImage(ctx, chatID, toUserID, imageURL, cardID, width, height)
}

// FetchChatHistory reuses the account's registered IM connection. Keeping this
// optional capability outside WSConn avoids forcing non-chat test transports to
// implement history retrieval.
// FetchChatHistory 封装Fetch聊天History业务协调。
func (a *Account) FetchChatHistory(ctx context.Context, chatID string, cursor int64, limit int) (map[string]any, string, error) {
	return a.outgoing.fetchChatHistory(ctx, chatID, cursor, limit)
}

// FetchChatConversations 封装Fetch聊天Conversations业务协调。
func (a *Account) FetchChatConversations(ctx context.Context, cursor int64, limit int) (map[string]any, string, error) {
	return a.outgoing.fetchChatConversations(ctx, cursor, limit)
}

// AutomationReady 报告自动化消息是否可以立即使用当前 WS 连接发送。
func (a *Account) AutomationReady() bool {
	return a.outgoing.automationReady()
}

// rotatePageDeviceID 对应官网 auto-login 成功后的 location.reload()：
// 新 FishEngine 使用新的 UUID-userID，普通 Set-Cookie 与自然重连不会调用它。
// rotatePageDeviceID 封装rotate页码DeviceID业务协调。
func (a *Account) rotatePageDeviceID() {
	a.mu.Lock()
	// userID 用于本次流程后续判断的用户ID
	userID := a.UserID
	if userID == "" {
		userID = protocol.TransCookies(a.CookieStr)["unb"]
	}
	a.deviceID = protocol.GenerateDeviceID(userID)
	a.mu.Unlock()
}
