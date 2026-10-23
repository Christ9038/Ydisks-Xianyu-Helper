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
	m.finishLogin(ctx, sess, sessionID, "qr_scan", true)
}

// finishLogin 完成登录成功后的统一官网收口、保持登录设置和状态记录。
// ctx 沿用登录任务的取消范围；sess 保存凭证，sessionID 标识会话，loginPath 区分诊断来源。
// navigateToIM 为 true 时先访问 /im；网络请求不持有 sess.mu，保持登录失败不推翻已完成的身份认证。
func (m *Manager) finishLogin(ctx context.Context, sess *Session, sessionID, loginPath string, navigateToIM bool) {
	if navigateToIM {
		// 二维码和人脸验证后的真实网页都会进入 /im；部分账号的长登录 Cookie
		// 只在这一步下发，不能只保存二维码或身份验证接口的响应头。
		if /* err 是官网重定向收尾失败原因，仅用于诊断，不清空已获取凭证。 */ err := m.completeConfirmedLogin(ctx, sess); err != nil {
			m.logger.Warn("登录后的官网登录跳转未完成，保留当前登录凭证", "session_id", sessionID, "login_path", loginPath, "err", err)
		}
	}
	// err 表示保持登录请求或状态确认失败，保留当前有效的短登录会话。
	if err := m.enableConfirmedLongLogin(ctx, sess); err != nil {
		m.logger.Warn("登录已成功，但官网保持登录开启失败", "session_id", sessionID, "login_path", loginPath, "err", err)
	}
	sess.mu.Lock()
	sess.Status = "success"
	finalizeSessionCredentialsLocked(sess)
	// unb 用于本次流程后续判断的unb
	unb := sess.unb
	// cookieCount 用于本次流程后续判断的登录凭证数量
	cookieCount := len(sess.cookies)
	// snapshotComplete 用于本次流程后续判断的snapshotComplete
	snapshotComplete := sess.cookieSnapshot != nil
	// hasHavanaLongLogin、havanaLongLoginHTTPOnly、hasCookie3Backup、cookie3BackupHTTPOnly
	// 只记录 Cookie 名称和属性，不记录 Cookie 值。
	hasHavanaLongLogin, havanaLongLoginHTTPOnly, hasCookie3Backup, cookie3BackupHTTPOnly := longLoginCookieFlagsLocked(sess)
	sess.mu.Unlock()
	m.logger.Info("扫码登录成功", "session_id", sessionID, "account_hash", logsafe.ID(unb),
		"login_path", loginPath, "cookie_count", cookieCount, "cookie_snapshot_complete", snapshotComplete,
		"has_havana_lgc_exp", hasHavanaLongLogin, "havana_lgc_exp_http_only", havanaLongLoginHTTPOnly,
		"has_cookie3_bak_exp", hasCookie3Backup, "cookie3_bak_exp_http_only", cookie3BackupHTTPOnly)
}

// longLoginCookieFlagsLocked 返回长登录标志的存在性和 HttpOnly 属性；调用方必须持有 sess.mu。
// sess 的完整快照优先于兼容 Cookie 映射；四个返回值依次对应 havana 和 cookie3 的存在性及 HttpOnly 属性。
func longLoginCookieFlagsLocked(sess *Session) (bool, bool, bool, bool) {
	if sess == nil {
		return false, false, false, false
	}
	// hasHavana、hasCookie3 记录标志存在性；havanaHTTPOnly、cookie3HTTPOnly 记录快照中的原始属性。
	var hasHavana, havanaHTTPOnly, hasCookie3, cookie3HTTPOnly bool
	if sess.cookieSnapshot != nil {
		// cookie 是当前快照中的单个凭证，只检查名称与属性，不向日志传递内容。
		for _, cookie := range sess.cookieSnapshot {
			switch cookie.Name {
			case "havana_lgc_exp":
				hasHavana = true
				havanaHTTPOnly = cookie.HTTPOnly
			case "cookie3_bak_exp":
				hasCookie3 = true
				cookie3HTTPOnly = cookie.HTTPOnly
			}
		}
		return hasHavana, havanaHTTPOnly, hasCookie3, cookie3HTTPOnly
	}
	return sess.cookies["havana_lgc_exp"] != "", false, sess.cookies["cookie3_bak_exp"] != "", false
}
