package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/protocol"
)

// mountQRLoginReal 扫码登录端点（纯 HTTP，不需要浏览器）。
func (s *Server) mountQRLoginReal(r chi.Router) {
	r.Post("/qr-login/generate", s.generateQRLogin)
	r.Get("/qr-login/check/{session_id}", s.checkQRLoginStatus)
	r.Get("/qr-login/status/{session_id}", s.checkQRLoginStatusAndPersist)
	r.Post("/qr-login/complete-verification/{session_id}", s.completeQRVerification)
}

// generateQRLogin 生成扫码登录二维码。
func (s *Server) generateQRLogin(w http.ResponseWriter, r *http.Request) {
	sessionID, qrCodeURL, err := s.QRLogin.GenerateQRCode(r.Context())
	if err != nil {
		s.Logger.Error("生成二维码失败", "err", err)
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"message": "生成二维码失败: " + err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"session_id":  sessionID,
		"qr_code_url": qrCodeURL,
	})
}

// checkQRLoginStatus 检查扫码登录状态。
func (s *Server) checkQRLoginStatus(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 session_id")
		return
	}
	result := s.QRLogin.GetSessionStatus(sessionID)
	writeJSON(w, http.StatusOK, result)
}

// checkQRLoginStatusAndPersist 兼容上游 /status 语义：扫码成功后由后端幂等保存账号。
func (s *Server) checkQRLoginStatusAndPersist(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 session_id")
		return
	}
	result := cloneQRStatus(s.QRLogin.GetSessionStatus(sessionID))
	if qrStatus(result) != "success" {
		writeJSON(w, http.StatusOK, result)
		return
	}
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	persisted, err := s.persistQRLoginSuccess(r.Context(), sess.UserID, sessionID, result)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("保存扫码登录结果失败", "session_id", sessionID, "err", err)
		}
		result["success"] = false
		result["status"] = "error"
		result["message"] = "保存扫码登录结果失败: " + err.Error()
		writeJSON(w, http.StatusOK, result)
		return
	}
	result["success"] = true
	result["account_id"] = persisted.AccountID
	result["is_new_account"] = persisted.IsNew
	writeJSON(w, http.StatusOK, result)
}

// completeQRVerification 用户完成风控验证后调用，提取真实 cookie 并入库。
func (s *Server) completeQRVerification(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 session_id")
		return
	}
	cookies, unb, err := s.QRLogin.CompleteVerification(r.Context(), sessionID)
	if err != nil {
		s.Logger.Error("验证完成处理失败", "err", err)
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	var req struct {
		TargetAccountID string `json:"target_account_id"`
		ConfirmMismatch bool   `json:"confirm_mismatch"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "请求格式错误")
			return
		}
	}
	req.TargetAccountID = strings.TrimSpace(req.TargetAccountID)
	if req.TargetAccountID != "" && req.TargetAccountID != unb && !req.ConfirmMismatch {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false, "requires_confirmation": true,
			"scanned_account_id": unb,
			"message":            "扫码账号与待重新授权账号不一致，需要确认后才能覆盖",
		})
		return
	}
	resp := map[string]any{
		"success": true,
		"cookies": cookies,
		"unb":     unb,
	}
	sess := auth.SessionFromContext(r.Context())
	if sess != nil {
		persisted, persistErr := s.persistQRLoginSuccessFor(r.Context(), sess.UserID, sessionID, map[string]any{
			"status":  "success",
			"cookies": cookies,
			"unb":     unb,
		}, req.TargetAccountID)
		if persistErr != nil {
			if s.Logger != nil {
				s.Logger.Warn("保存扫码验证结果失败", "session_id", sessionID, "err", persistErr)
			}
			resp["success"] = false
			resp["message"] = "保存扫码登录结果失败: " + persistErr.Error()
			writeJSON(w, http.StatusOK, resp)
			return
		}
		resp["account_id"] = persisted.AccountID
		resp["is_new_account"] = persisted.IsNew
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) persistQRLoginSuccess(ctx context.Context, userID int64, sessionID string, result map[string]any) (qrLoginPersistence, error) {
	return s.persistQRLoginSuccessFor(ctx, userID, sessionID, result, "")
}

func (s *Server) persistQRLoginSuccessFor(ctx context.Context, userID int64, sessionID string, result map[string]any, targetAccountID string) (qrLoginPersistence, error) {
	s.qrMu.Lock()
	defer s.qrMu.Unlock()
	if s.qrPersisted == nil {
		s.qrPersisted = make(map[string]qrLoginPersistence)
	}
	if persisted, ok := s.qrPersisted[sessionID]; ok {
		return persisted, nil
	}
	cookies := qrString(result, "cookies")
	scannedAccountID := strings.TrimSpace(firstNonEmpty(qrString(result, "unb"), protocol.TransCookies(cookies)["unb"]))
	if cookies == "" || scannedAccountID == "" {
		return qrLoginPersistence{}, errors.New("扫码结果缺少 cookies 或 unb")
	}
	accountID := strings.TrimSpace(targetAccountID)
	if accountID == "" {
		accountID = scannedAccountID
	} else {
		target, err := s.Store.Cookies.GetDetails(ctx, accountID)
		if err != nil {
			return qrLoginPersistence{}, errors.New("待重新授权账号不存在")
		}
		if target.UserID != userID {
			return qrLoginPersistence{}, errors.New("待重新授权账号不属于当前用户")
		}
	}

	_, err := s.Store.Cookies.GetDetails(ctx, accountID)
	isNew := errors.Is(err, db.ErrNotFound)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return qrLoginPersistence{}, err
	}
	if err := s.Store.Cookies.Save(ctx, accountID, cookies, userID); err != nil {
		if errors.Is(err, db.ErrForbidden) {
			return qrLoginPersistence{}, errors.New("该账号ID已存在且不属于当前用户")
		}
		return qrLoginPersistence{}, err
	}
	s.markSuccessfulLogin(ctx, accountID, userID, loginMethodQRScan, "扫码登录成功")
	if s.Store.Tokens != nil {
		_ = s.Store.Tokens.Clear(ctx, accountID)
	}
	if d, err := s.Store.Cookies.GetDetails(ctx, accountID); err == nil {
		s.refreshAccountProfile(ctx, d)
	}
	if s.Manager != nil && s.Store.Cookies.GetStatus(ctx, accountID) {
		if err := s.Manager.Restart(ctx, accountID); err != nil && s.Logger != nil {
			s.Logger.Warn("扫码登录后重启账号失败", "cookie_id", accountID, "err", err)
		}
	}
	persisted := qrLoginPersistence{AccountID: accountID, IsNew: isNew}
	s.qrPersisted[sessionID] = persisted
	return persisted, nil
}

func cloneQRStatus(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func qrStatus(result map[string]any) string {
	status, _ := result["status"].(string)
	return status
}

func qrString(result map[string]any, key string) string {
	value, _ := result[key].(string)
	return strings.TrimSpace(value)
}
