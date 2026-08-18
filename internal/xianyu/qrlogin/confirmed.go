package qrlogin

import (
	"context"
	"time"

	"xianyu-go/internal/logsafe"
)

// handleConfirmedQRStatus 完成确认扫码后的风控分流或官网登录收口；两条路径都终止当前轮询。
func (m *Manager) handleConfirmedQRStatus(ctx context.Context, sess *Session, sessionID string, iframeRedirect bool, verificationURL string) {
	if iframeRedirect {
		// 风控验证：记录状态和 URL，提取响应 cookie（临时 cookie），
		// 验证完成后用这些临时 cookie 访问 goofish.com/im 换真实 cookie。
		sess.mu.Lock()
		if sess.Status == "success" {
			sess.mu.Unlock()
			return
		}
		if sess.Status != "verification_required" {
			sess.Status = "verification_required"
			sess.verificationURL = verificationURL
			// 已有权威快照交给可导出的 Go Cookie Jar；它会继续吸收
			// 人脸跳转链每个重定向响应的 Set-Cookie。
			// 人脸验证会额外占用用户手机端操作时间。这里重置窗口，避免普通
			// 扫码 5 分钟窗口在用户扫人脸二维码时把会话误标为 expired。
			sess.createdTime = time.Now()
			sess.expireTime = 5 * time.Minute
			// verURL 用于本次流程后续判断的verURL
			verURL := sess.verificationURL
			// expireTime 用于本次流程后续判断的expire时间
			expireTime := sess.expireTime
			// cookieCount 用于本次流程后续判断的登录凭证数量
			cookieCount := len(sess.cookies)
			sess.mu.Unlock()
			m.logger.Warn("扫码登录需要风控验证，使用 Go HTTP 保持原登录会话", "session_id", sessionID, "verification_url", logsafe.URL(verURL), "tmp_cookie_count", cookieCount)
			// 人脸验证任务沿用会话根 Context，由 Manager 统一取消和等待，不能脱离进程生命周期。
			_ = m.startSessionTask(sessionID, expireTime, func(verifyCtx context.Context) {
				m.runGoVerification(verifyCtx, sessionID, verURL)
			})
		} else {
			sess.mu.Unlock()
		}
		return
	}
	// 二维码组件确认成功后，真实网页还会进入 /im 并跟随登录
	// 重定向。部分账号的长登录 Cookie 只在这一步下发，不能只
	// 保存 query.do 的响应头。
	if // err 用于本次流程后续判断的err
	err := m.completeConfirmedLogin(ctx, sess); err != nil {
		m.logger.Warn("扫码确认后的官网登录跳转未完成，保留当前登录凭证", "session_id", sessionID, "err", err)
	}
	if // err 用于本次流程后续判断的err
	err := m.enableConfirmedLongLogin(ctx, sess); err != nil {
		m.logger.Warn("扫码登录已成功，但官网保持登录开启失败", "session_id", sessionID, "err", err)
	}
	sess.mu.Lock()
	sess.Status = "success"
	finalizeSessionCredentialsLocked(sess)
	// unb 用于本次流程后续判断的unb
	unb := sess.unb
	// cookieCount 用于本次流程后续判断的登录凭证数量
	cookieCount := len(sess.cookies)
	// hasHavanaLongLogin 用于本次流程后续判断的hasHavanaLong登录
	hasHavanaLongLogin := sess.cookies["havana_lgc_exp"] != ""
	// hasCookie3Backup 用于本次流程后续判断的hasCookie3Backup
	hasCookie3Backup := sess.cookies["cookie3_bak_exp"] != ""
	// snapshotComplete 用于本次流程后续判断的snapshotComplete
	snapshotComplete := sess.cookieSnapshot != nil
	sess.mu.Unlock()
	m.logger.Info("扫码登录成功", "session_id", sessionID, "account_hash", logsafe.ID(unb),
		"cookie_count", cookieCount, "cookie_snapshot_complete", snapshotComplete,
		"has_havana_lgc_exp", hasHavanaLongLogin, "has_cookie3_bak_exp", hasCookie3Backup)
}
