package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// mountQRLoginReal 扫码登录端点（纯 HTTP，不需要浏览器）。
func (s *Server) mountQRLoginReal(r chi.Router) {
	r.Post("/qr-login/generate", s.generateQRLogin)
	r.Get("/qr-login/check/{session_id}", s.checkQRLoginStatus)
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
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"cookies": cookies,
		"unb":     unb,
	})
}
