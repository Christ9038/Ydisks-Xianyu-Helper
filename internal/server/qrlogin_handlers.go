package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/auth"
)

// qrLoginGenerateTimeout 用于本次流程后续判断的qr登录GenerateTimeout
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
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	s.cleanupQRLoginSessions()
	// 风控后的闲鱼匿名 token 接口偶尔会明显变慢。二维码生成需要连续完成
	// token、登录参数和二维码请求，不能沿用前端通用接口的短超时。
	// generateCtx、cancel 用于本次流程后续判断的generateCtx、cancel
	generateCtx, cancel := context.WithTimeout(r.Context(), qrLoginGenerateTimeout)
	defer cancel()
	// qrService 是通过显式平台依赖边界取得的二维码服务；为空时拒绝执行平台调用。
	qrService := s.qrLoginService()
	if qrService == nil {
		writeErr(w, http.StatusInternalServerError, "二维码服务未初始化")
		return
	}
	// sessionID、qrCodeURL、err 用于本次流程后续判断的会话ID、qrCodeURL、err
	sessionID, qrCodeURL, err := qrService.GenerateQRCode(generateCtx)
	if err != nil {
		// message 用于本次流程后续判断的消息
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
	// qrSessions 保存扫码会话所有权；平台二维码服务只负责平台会话本身。
	qrSessions := s.accountLoginApplication().qrSessions
	if qrSessions == nil {
		writeErr(w, http.StatusInternalServerError, "扫码会话服务未初始化")
		return
	}
	qrSessions.Register(sessionID, sess.UserID, time.Now().UTC())
	writeJSON(w, http.StatusOK, qrLoginGenerateResponse{
		Success: true, SessionID: sessionID, QRCodeURL: qrCodeURL,
		// 生成成功响应只暴露会话标识和二维码地址。
		// 服务端仍然保留二维码会话所有权校验。
	})
}

// checkQRLoginStatus 检查扫码登录状态。
func (s *Server) checkQRLoginStatus(w http.ResponseWriter, r *http.Request) {
	// sessionID 用于本次流程后续判断的会话ID
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 session_id")
		return
	}
	if !s.requireQRSessionOwner(w, r, sessionID) {
		return
	}
	// qrService 是已完成平台依赖校验的二维码服务。
	qrService := s.qrLoginService()
	if qrService == nil {
		writeErr(w, http.StatusInternalServerError, "二维码服务未初始化")
		return
	}
	// result 用于本次流程后续判断的结果
	result := publicQRStatus(qrService.GetSessionStatus(sessionID))
	writeJSON(w, http.StatusOK, result)
}

// checkQRLoginStatusAndPersist 兼容上游 /status 语义：扫码成功后由后端幂等保存账号。
func (s *Server) checkQRLoginStatusAndPersist(w http.ResponseWriter, r *http.Request) {
	// sessionID 用于本次流程后续判断的会话ID
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 session_id")
		return
	}
	if !s.requireQRSessionOwner(w, r, sessionID) {
		return
	}
	// qrService 是已完成平台依赖校验的二维码服务。
	qrService := s.qrLoginService()
	if qrService == nil {
		writeErr(w, http.StatusInternalServerError, "二维码服务未初始化")
		return
	}
	// result 用于本次流程后续判断的结果
	result := cloneQRStatus(qrService.GetSessionStatus(sessionID))
	if qrStatus(result) != "success" {
		writeJSON(w, http.StatusOK, publicQRStatus(result))
		return
	}
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// persisted、err 用于本次流程后续判断的persisted、err
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
	// sessionID 用于本次流程后续判断的会话ID
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 session_id")
		return
	}
	if !s.requireQRSessionOwner(w, r, sessionID) {
		return
	}
	// qrService 是已完成平台依赖校验的二维码服务。
	qrService := s.qrLoginService()
	if qrService == nil {
		writeErr(w, http.StatusInternalServerError, "二维码服务未初始化")
		return
	}
	// cookies、unb、err 用于本次流程后续判断的cookies、unb、err
	cookies, unb, err := qrService.CompleteVerification(r.Context(), sessionID)
	if err != nil {
		s.Logger.Error("验证完成处理失败", "err", err)
		writeErrCode(
			w,
			http.StatusBadGateway, "qr_verification_failed",
			err.Error(), "")
		return
	}
	// req 保存具名扫码验证完成请求。
	var req qrVerificationRequestDTO
	if r.Body != nil && r.ContentLength != 0 {
		if // err 用于本次流程后续判断的err
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
	// resp 用于本次流程后续判断的resp
	resp := qrLoginVerificationResponse{
		Success: true, UNB: unb,
		// 验证完成响应保留平台账号标识，兼容旧客户端。
	}
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	if sess != nil {
		// result 用于本次流程后续判断的结果
		result := map[string]any{
			"status":  "success",
			"cookies": cookies,
			"unb":     unb,
		}
		if // current 用于本次流程后续判断的current
		current := qrService.GetSessionStatus(sessionID); current != nil {
			if // snapshot、ok 用于本次流程后续判断的snapshot、ok
			snapshot, ok := current["cookie_snapshot"]; ok {
				result["cookie_snapshot"] = snapshot
			}
		}
		// persisted、persistErr 用于本次流程后续判断的persisted、persistErr
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

// requireQRSessionOwner 封装requireQR会话所有者业务协调。
func (s *Server) requireQRSessionOwner(w http.ResponseWriter, r *http.Request, sessionID string) bool {
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return false
	}
	// qrSessions 负责所有权、过期判断和幂等状态清理，HTTP 层不再持有可变会话表。
	qrSessions := s.accountLoginApplication().qrSessions
	if qrSessions == nil {
		writeErr(w, http.StatusInternalServerError, "扫码会话服务未初始化")
		return false
	}
	// err 保存扫码会话所有权校验结果。
	if err := qrSessions.Authorize(sessionID, sess.UserID); err != nil {
		if errors.Is(err, accountapp.ErrQRLoginSessionNotFound) {
			// cleaner、cleanable 分别表示平台会话清理器及其接口是否可用。
			if cleaner, cleanable := s.qrLoginService().(interface{ DeleteSession(string) }); cleanable {
				cleaner.DeleteSession(sessionID)
			}
		}
		writeErr(w, http.StatusNotFound, "扫码会话不存在或已过期")
		return false
	}
	return true
}

// cleanupQRLoginSessions 封装cleanupQR登录Sessions业务协调。
func (s *Server) cleanupQRLoginSessions() {
	// qrSessions 提供应用层扫码会话过期清理能力。
	qrSessions := s.accountLoginApplication().qrSessions
	if qrSessions == nil {
		return
	}
	// expired 保存应用层报告的过期扫码会话标识。
	expired := qrSessions.Cleanup(time.Now().UTC())
	if // cleaner、ok 用于本次流程后续判断的cleaner、ok
	cleaner, ok := s.qrLoginService().(interface{ DeleteSession(string) }); ok {
		// id 表示当前遍历过程中的标识
		for _, id := range expired {
			cleaner.DeleteSession(id)
		}
	}
}

// cloneQRStatus 封装cloneQR状态业务协调。
func cloneQRStatus(src map[string]any) map[string]any {
	// dst 用于本次流程后续判断的dst
	dst := make(map[string]any, len(src))
	// k、v 表示当前遍历过程中的k、v
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// publicQRStatus 返回可暴露给浏览器的扫码状态。闲鱼 Cookie 只在服务端持久化，
// 永远不进入前端、浏览器日志或代理响应。
// publicQRStatus 封装publicQR状态业务协调。
func publicQRStatus(src map[string]any) qrLoginStatusResponse {
	// status 保存允许传输到浏览器的扫码状态；Cookie 和快照字段刻意不映射。
	status := qrLoginStatusResponse{Status: qrStatus(src)}
	// success 表示上游是否报告扫码流程成功；ok 表示字段类型确实为布尔值。
	if success, ok := src["success"].(bool); ok {
		status.Success = success
	}
	// message 保存上游返回的用户提示文本；ok 表示字段类型确实为字符串。
	if message, ok := src["message"].(string); ok {
		status.Message = message
	}
	// sessionID 保存扫码会话标识；ok 表示字段类型确实为字符串。
	if sessionID, ok := src["session_id"].(string); ok {
		status.SessionID = sessionID
	}
	// accountID 保存扫码成功后关联的本地账号标识；ok 表示字段类型确实为字符串。
	if accountID, ok := src["account_id"].(string); ok {
		status.AccountID = accountID
	}
	// isNew 表示是否新建本地账号；ok 表示字段类型确实为布尔值。
	if isNew, ok := src["is_new_account"].(bool); ok {
		status.IsNewAccount = isNew
	}
	return status
}

// qrStatus 封装qr状态业务协调。
func qrStatus(result map[string]any) string {
	// status 用于本次流程后续判断的状态
	status, _ := result["status"].(string)
	return status
}

// qrString 封装qrString业务协调。
func qrString(result map[string]any, key string) string {
	// value 用于本次流程后续判断的值
	value, _ := result[key].(string)
	return strings.TrimSpace(value)
}
