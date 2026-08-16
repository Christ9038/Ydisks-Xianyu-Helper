package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// qrLoginGenerateTimeout 保存qr登录GenerateTimeout，供当前处理流程使用
const qrLoginGenerateTimeout = 2 * time.Minute

// mountQRLoginReal 扫码登录端点（纯 HTTP，不需要浏览器）。
func (s *Server) mountQRLoginReal(r chi.Router) {
	r.Post("/qr-login/generate", s.generateQRLogin)
	r.Get("/qr-login/check/{session_id}", s.checkQRLoginStatus)
	r.Get("/qr-login/status/{session_id}", s.checkQRLoginStatusAndPersist)
	r.Post("/qr-login/complete-verification/{session_id}", s.completeQRVerification)
}

// generateQRLogin 生成扫码登录二维码。
func (s *Server) generateQRLogin(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	s.cleanupQRLoginSessions()
	// 风控后的闲鱼匿名 token 接口偶尔会明显变慢。二维码生成需要连续完成
	// token、登录参数和二维码请求，不能沿用前端通用接口的短超时。
	// generateCtx、cancel 保存generateCtx、cancel，供当前处理流程使用
	generateCtx, cancel := context.WithTimeout(r.Context(), qrLoginGenerateTimeout)
	defer cancel()
	// sessionID、qrCodeURL、err 保存会话ID、qrCodeURL、err，供当前处理流程使用
	sessionID, qrCodeURL, err := s.QRLogin.GenerateQRCode(generateCtx)
	if err != nil {
		// message 保存消息，供当前处理流程使用
		message := "生成二维码失败: " + err.Error()
		switch {
		case errors.Is(err, context.Canceled):
			s.Logger.Info("二维码生成请求已取消")
			message = "二维码生成请求已取消，请重新获取"
		case errors.Is(err, context.DeadlineExceeded):
			s.Logger.Error("生成二维码超时", "err", err)
			message = "闲鱼二维码接口响应超时，请稍后重新获取"
		default:
			s.Logger.Error("生成二维码失败", "err", err)
		}
		writeErrCode(
			w,
			http.StatusBadGateway, "qr_login_generate_failed",
			message, "")
		return
	}
	s.qrMu.Lock()
	s.qrOwners[sessionID] = qrLoginOwner{UserID: sess.UserID, CreatedAt: time.Now().UTC()}
	s.qrMu.Unlock()
	writeJSON(w, http.StatusOK, qrLoginGenerateResponse{
		Success: true, SessionID: sessionID, QRCodeURL: qrCodeURL,
		// 生成成功响应只暴露会话标识和二维码地址。
		// 服务端仍然保留二维码会话所有权校验。
	})
}

// checkQRLoginStatus 检查扫码登录状态。
func (s *Server) checkQRLoginStatus(w http.ResponseWriter, r *http.Request) {
	// sessionID 保存会话ID，供当前处理流程使用
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 session_id")
		return
	}
	if !s.requireQRSessionOwner(w, r, sessionID) {
		return
	}
	// result 保存结果，供当前处理流程使用
	result := publicQRStatus(s.QRLogin.GetSessionStatus(sessionID))
	writeJSON(w, http.StatusOK, result)
}

// checkQRLoginStatusAndPersist 兼容上游 /status 语义：扫码成功后由后端幂等保存账号。
func (s *Server) checkQRLoginStatusAndPersist(w http.ResponseWriter, r *http.Request) {
	// sessionID 保存会话ID，供当前处理流程使用
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 session_id")
		return
	}
	if !s.requireQRSessionOwner(w, r, sessionID) {
		return
	}
	// result 保存结果，供当前处理流程使用
	result := cloneQRStatus(s.QRLogin.GetSessionStatus(sessionID))
	if qrStatus(result) != "success" {
		writeJSON(w, http.StatusOK, publicQRStatus(result))
		return
	}
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// persisted、err 保存persisted、err，供当前处理流程使用
	persisted, err := s.accountLoginApplication().PersistQRLoginSuccess(r.Context(), sess.UserID, sessionID, result, "")
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("保存扫码登录结果失败", "session_id", sessionID, "err", err)
		}
		writeErrCode(
			w,
			http.StatusInternalServerError, "qr_login_persist_failed",
			"保存扫码登录结果失败: "+err.Error(), "")
		return
	}
	result["success"] = true
	result["account_id"] = persisted.AccountID
	result["is_new_account"] = persisted.IsNew
	writeJSON(w, http.StatusOK, publicQRStatus(result))
}

// completeQRVerification 用户完成风控验证后调用，提取真实 cookie 并入库。
func (s *Server) completeQRVerification(w http.ResponseWriter, r *http.Request) {
	// sessionID 保存会话ID，供当前处理流程使用
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 session_id")
		return
	}
	if !s.requireQRSessionOwner(w, r, sessionID) {
		return
	}
	// cookies、unb、err 保存cookies、unb、err，供当前处理流程使用
	cookies, unb, err := s.QRLogin.CompleteVerification(r.Context(), sessionID)
	if err != nil {
		s.Logger.Error("验证完成处理失败", "err", err)
		writeErrCode(
			w,
			http.StatusBadGateway, "qr_verification_failed",
			err.Error(), "")
		return
	}
	// req 保存req，供当前处理流程使用
	var req struct {
		TargetAccountID string `json:"target_account_id"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if // err 保存err，供当前处理流程使用
		err := decodeJSON(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "请求格式错误")
			return
		}
	}
	req.TargetAccountID = strings.TrimSpace(req.TargetAccountID)
	if req.TargetAccountID != "" && req.TargetAccountID != unb {
		writeErrDetails(
			w, http.StatusConflict,
			"qr_account_mismatch",
			"扫码账号与待重新授权账号不一致，已拒绝覆盖；请使用正确账号重新扫码",
			"", map[string]any{"scanned_account_id": unb})
		return
	}
	// resp 保存resp，供当前处理流程使用
	resp := qrLoginVerificationResponse{
		Success: true, UNB: unb,
		// 验证完成响应保留平台账号标识，兼容旧客户端。
	}
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	if sess != nil {
		// result 保存结果，供当前处理流程使用
		result := map[string]any{
			"status":  "success",
			"cookies": cookies,
			"unb":     unb,
		}
		if // current 保存current，供当前处理流程使用
		current := s.QRLogin.GetSessionStatus(sessionID); current != nil {
			if // snapshot、ok 保存snapshot、ok，供当前处理流程使用
			snapshot, ok := current["cookie_snapshot"]; ok {
				result["cookie_snapshot"] = snapshot
			}
		}
		// persisted、persistErr 保存persisted、persistErr，供当前处理流程使用
		persisted, persistErr := s.accountLoginApplication().PersistQRLoginSuccess(r.Context(), sess.UserID, sessionID, result, req.TargetAccountID)
		if persistErr != nil {
			if s.Logger != nil {
				s.Logger.Warn("保存扫码验证结果失败", "session_id", sessionID, "err", persistErr)
			}
			writeErrCode(
				w, http.StatusInternalServerError, "qr_login_persist_failed",
				"保存扫码登录结果失败: "+persistErr.Error(), "")
			return
		}
		resp.AccountID = persisted.AccountID
		resp.IsNewAccount = persisted.IsNew
	}
	writeJSON(w, http.StatusOK, resp)
}

// requireQRSessionOwner 负责requireQR会话所有者相关处理。
func (s *Server) requireQRSessionOwner(w http.ResponseWriter, r *http.Request, sessionID string) bool {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return false
	}
	s.qrMu.Lock()
	// owner、ok 保存owner、ok，供当前处理流程使用
	owner, ok := s.qrOwners[sessionID]
	// expired 保存expired，供当前处理流程使用
	expired := ok && owner.CreatedAt.Before(time.Now().UTC().Add(-30*time.Minute))
	if expired {
		delete(s.qrOwners, sessionID)
		delete(s.qrPersisted, sessionID)
		s.qrPersistLocks.Delete(sessionID)
	}
	s.qrMu.Unlock()
	if expired {
		if // cleaner、cleanable 保存cleaner、cleanable，供当前处理流程使用
		cleaner, cleanable := s.QRLogin.(interface{ DeleteSession(string) }); cleanable {
			cleaner.DeleteSession(sessionID)
		}
	}
	if !ok || expired || owner.UserID != sess.UserID {
		writeErr(w, http.StatusNotFound, "扫码会话不存在或已过期")
		return false
	}
	return true
}

// cleanupQRLoginSessions 负责cleanupQR登录Sessions相关处理。
func (s *Server) cleanupQRLoginSessions() {
	// cutoff 保存cutoff，供当前处理流程使用
	cutoff := time.Now().UTC().Add(-30 * time.Minute)
	// expired 保存expired，供当前处理流程使用
	expired := make([]string, 0)
	s.qrMu.Lock()
	// id、owner 表示当前遍历过程中的id、owner
	for id, owner := range s.qrOwners {
		if owner.CreatedAt.Before(cutoff) {
			delete(s.qrOwners, id)
			delete(s.qrPersisted, id)
			s.qrPersistLocks.Delete(id)
			expired = append(expired, id)
		}
	}
	s.qrMu.Unlock()
	if // cleaner、ok 保存cleaner、ok，供当前处理流程使用
	cleaner, ok := s.QRLogin.(interface{ DeleteSession(string) }); ok {
		// id 表示当前遍历过程中的标识
		for _, id := range expired {
			cleaner.DeleteSession(id)
		}
	}
}

// cloneQRStatus 负责cloneQR状态相关处理。
func cloneQRStatus(src map[string]any) map[string]any {
	// dst 保存dst，供当前处理流程使用
	dst := make(map[string]any, len(src))
	// k、v 表示当前遍历过程中的k、v
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// publicQRStatus 返回可暴露给浏览器的扫码状态。闲鱼 Cookie 只在服务端持久化，
// 永远不进入前端、浏览器日志或代理响应。
// publicQRStatus 负责publicQR状态相关处理。
func publicQRStatus(src map[string]any) qrLoginStatusResponse {
	// dst 保存dst，供当前处理流程使用
	dst := qrLoginStatusResponse(cloneQRStatus(src))
	delete(dst, "cookies")
	delete(dst, "cookie_snapshot")
	return dst
}

// qrStatus 负责qr状态相关处理。
func qrStatus(result map[string]any) string {
	// status 保存状态，供当前处理流程使用
	status, _ := result["status"].(string)
	return status
}

// qrString 负责qrString相关处理。
func qrString(result map[string]any, key string) string {
	// value 保存值，供当前处理流程使用
	value, _ := result[key].(string)
	return strings.TrimSpace(value)
}
