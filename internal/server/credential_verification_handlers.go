package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/auth"
)

// credentialVerificationResponse 是启动续期安全验证会话的具名响应 DTO。
// 返回的会话标识交给既有二维码状态与完成验证接口继续使用，不包含验证地址或任何凭证。
type credentialVerificationResponse struct {
	// Success 表示验证会话是否已创建。
	Success bool `json:"success"`
	// SessionID 是人工验证会话标识，沿用二维码登录会话的轮询与完成接口。
	SessionID string `json:"session_id"`
}

// startCredentialVerification 为“定时续期被平台要求安全验证”的账号启动人工验证会话。
// 仅账号所有者可调用；没有待验证登记（未触发、已使用或已过期）时返回 409，前端据此回退到重新扫码授权。
func (s *Server) startCredentialVerification(w http.ResponseWriter, r *http.Request) {
	// cid 是路径中的账号标识。
	cid := chi.URLParam(r, "cid")
	// ownedDetail、ok 保存账号摘要和归属校验结果。
	ownedDetail, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	// sess 是当前认证会话；requireCookieOwner 已保证其存在，这里再防御一次。
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// accountLogin 持有二维码会话归属登记；缺失时无法让后续轮询接口授权该会话。
	accountLogin := s.accountLoginApplication()
	if accountLogin == nil {
		writeErr(w, http.StatusInternalServerError, "扫码会话服务未初始化")
		return
	}
	// sessionID、err 是应用服务创建的验证会话标识及错误。
	sessionID, err := s.credentialVerificationApplication().Start(r.Context(), ownedDetail.UserID, cid)
	if err != nil {
		s.writeCredentialVerificationError(w, err)
		return
	}
	// 登记会话归属后，既有 /qr-login/status 与 /qr-login/complete-verification 才会放行该会话。
	accountLogin.RegisterQRSession(sessionID, sess.UserID, time.Now().UTC())
	writeJSON(w, http.StatusOK, credentialVerificationResponse{Success: true, SessionID: sessionID})
}

// writeCredentialVerificationError 把验证用例错误映射为统一错误响应。
func (s *Server) writeCredentialVerificationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, accountapp.ErrVerificationNotPending):
		writeErrCode(w, http.StatusConflict, "verification_expired", "安全验证已过期或不存在，请重新扫码授权该账号", "")
	case errors.Is(err, accountapp.ErrForbidden), errors.Is(err, accountapp.ErrNotFound), errors.Is(err, accountapp.ErrCredentialNotFound):
		writeErr(w, http.StatusNotFound, "账号不存在")
	default:
		if s.Logger != nil {
			s.Logger.Warn("创建续期安全验证会话失败", "err", err)
		}
		writeErrCode(w, http.StatusBadGateway, "credential_verification_failed", "创建安全验证会话失败: "+err.Error(), "")
	}
}

// markVerificationPending 在运行状态映射 statuses 上补充 verification_pending 标记。
// accountIDs 已按当前用户归属过滤；只读内存登记，不发起任何平台请求。
func (s *Server) markVerificationPending(statuses map[string]accountapp.RuntimeStatus, accountIDs []string) {
	// port 是续期安全验证用例；缺失时保持原状态不变。
	port := s.credentialVerificationApplication()
	if port == nil {
		return
	}
	// accountID 是已确认归属当前用户且在登记表中待验证的账号标识。
	for accountID := range port.PendingAmong(accountIDs) {
		// status、exists 是该账号的运行状态及其存在标记。
		if status, exists := statuses[accountID]; exists {
			status.VerificationPending = true
			statuses[accountID] = status
		}
	}
}
