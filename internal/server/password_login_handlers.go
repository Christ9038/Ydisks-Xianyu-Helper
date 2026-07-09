package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/browser"
	"xianyu-go/internal/db"
)

type passwordLoginRunner interface {
	PasswordLogin(ctx context.Context, account, password, cookieID, userDataDir string, headless bool) (map[string]string, error)
}

type passwordLoginEventRunner interface {
	PasswordLoginWithEvents(ctx context.Context, account, password, cookieID, userDataDir string, headless bool, onEvent browser.PasswordLoginEventHandler) (map[string]string, error)
}

const (
	passwordLoginSessionMaxAge       = time.Hour
	passwordLoginProcessingTimeout   = 5 * time.Minute
	passwordLoginBaxiaPunishReason   = "baxia_punish_captcha"
	passwordLoginBaxiaPunishCooldown = 5
)

type passwordLoginSession struct {
	ID              string
	AccountID       string
	Account         string
	UserID          int64
	Status          string
	Message         string
	Error           string
	Reason          string
	VerificationURL string
	ScreenshotPath  string
	QRCodeURL       string
	CooldownHours   int
	IsNewAccount    bool
	CookieCount     int
	ShowBrowser     bool
	CreatedAt       time.Time
	cancel          context.CancelFunc
}

func (s *Server) mountPasswordLogin(r chi.Router) {
	r.Post("/password-login", s.startPasswordLogin)
	r.Get("/password-login/check/{session_id}", s.checkPasswordLogin)
	r.Delete("/password-login/cancel/{session_id}", s.cancelPasswordLogin)
}

func (s *Server) startPasswordLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID   string `json:"account_id"`
		CookieID    string `json:"cookie_id"`
		Account     string `json:"account"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		ShowBrowser bool   `json:"show_browser"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": "请求格式错误"})
		return
	}
	accountID := firstNonEmpty(req.AccountID, req.CookieID)
	account := firstNonEmpty(req.Account, req.Username)
	accountID = strings.TrimSpace(accountID)
	account = strings.TrimSpace(account)
	if accountID == "" || account == "" || req.Password == "" {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": "账号ID、登录账号和密码不能为空"})
		return
	}
	if s.PasswordLogin == nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": "浏览器登录服务不可用"})
		return
	}
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	if ok, message := s.canStartPasswordLogin(r.Context(), sess.UserID, accountID); !ok {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": message})
		return
	}
	s.cleanupExpiredPasswordLoginSessions()
	if sid := s.currentPasswordLoginSession(accountID); sid != "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"success":    true,
			"session_id": sid,
			"status":     "processing",
			"message":    "账号正在处理中，请稍候...",
		})
		return
	}

	sessionID, err := newPasswordSessionID()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": "创建登录会话失败"})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	loginSession := &passwordLoginSession{
		ID:          sessionID,
		AccountID:   accountID,
		Account:     account,
		UserID:      sess.UserID,
		Status:      "processing",
		Message:     "登录处理中，请稍候...",
		ShowBrowser: req.ShowBrowser,
		CreatedAt:   time.Now(),
		cancel:      cancel,
	}
	s.passwordMu.Lock()
	s.passwordSessions[sessionID] = loginSession
	s.passwordProcessing[accountID] = sessionID
	s.passwordMu.Unlock()

	go s.runPasswordLoginSession(ctx, loginSession, req.Password)

	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"session_id": sessionID,
		"status":     "processing",
		"message":    "登录任务已启动，请等待...",
	})
}

func (s *Server) checkPasswordLogin(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	s.cleanupExpiredPasswordLoginSessions()

	s.passwordMu.Lock()
	session, ok := s.passwordSessions[sessionID]
	if !ok {
		s.passwordMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"status": "not_found", "message": "会话不存在或已过期"})
		return
	}
	response := passwordSessionResponse(session)
	if session.Status == "success" || session.Status == "failed" {
		delete(s.passwordSessions, sessionID)
		delete(s.passwordProcessing, session.AccountID)
	}
	s.passwordMu.Unlock()
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) cancelPasswordLogin(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	s.passwordMu.Lock()
	session, ok := s.passwordSessions[sessionID]
	if !ok {
		s.passwordMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "code": 404, "message": "会话不存在", "data": nil})
		return
	}
	if session.cancel != nil {
		session.cancel()
	}
	delete(s.passwordSessions, sessionID)
	delete(s.passwordProcessing, session.AccountID)
	s.passwordMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "code": 200, "message": "登录会话已取消", "data": nil})
}

func (s *Server) runPasswordLoginSession(ctx context.Context, session *passwordLoginSession, password string) {
	var (
		cookies map[string]string
		err     error
	)
	headless := !session.ShowBrowser
	if runner, ok := s.PasswordLogin.(passwordLoginEventRunner); ok {
		cookies, err = runner.PasswordLoginWithEvents(ctx, session.Account, password, session.AccountID, "", headless, func(event browser.PasswordLoginEvent) {
			s.applyPasswordLoginEvent(session, event)
		})
	} else {
		cookies, err = s.PasswordLogin.PasswordLogin(ctx, session.Account, password, session.AccountID, "", headless)
	}
	if err != nil {
		event := browser.PasswordLoginEventFromError(err)
		status := event.Status
		if status == "" {
			status = "failed"
		}
		reason := passwordLoginReason(err)
		s.failPasswordLoginSession(session, status, err.Error(), reason, event.CooldownHours)
		s.addLoginLog(context.Background(), session.AccountID, session.UserID, loginMethodPassword, loginStatusFailed, reason, err.Error(), 0)
		return
	}
	accountID, isNew, err := s.savePasswordLoginCookies(ctx, session, password, cookies)
	if err != nil {
		s.failPasswordLoginSession(session, "failed", "保存登录结果失败: "+err.Error(), "", 0)
		s.addLoginLog(context.Background(), session.AccountID, session.UserID, loginMethodPassword, loginStatusFailed, "cookie_update_failed", err.Error(), 0)
		return
	}
	s.completePasswordLoginSession(session, func(current *passwordLoginSession) {
		current.Status = "success"
		current.Message = "账号 " + accountID + " 登录成功"
		current.AccountID = accountID
		current.IsNewAccount = isNew
		current.CookieCount = len(cookies)
	})
}

func (s *Server) savePasswordLoginCookies(ctx context.Context, session *passwordLoginSession, password string, cookies map[string]string) (string, bool, error) {
	if len(cookies) == 0 {
		return "", false, errors.New("浏览器未返回 cookie")
	}
	accountID := cookieAccountID(cookies, session.AccountID)
	cookieValue := browser.MarshalCookies(cookies)
	_, err := s.Store.Cookies.GetDetails(ctx, accountID)
	isNew := errors.Is(err, db.ErrNotFound)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return "", false, err
	}
	if err := s.Store.Cookies.Save(ctx, accountID, cookieValue, session.UserID); err != nil {
		return "", false, err
	}
	if err := s.Store.Cookies.UpdateLoginInfo(ctx, accountID, session.Account, password, session.ShowBrowser); err != nil {
		return "", false, err
	}
	s.markSuccessfulLogin(ctx, accountID, session.UserID, loginMethodPassword, "账号密码登录成功")
	if s.Store.Tokens != nil {
		_ = s.Store.Tokens.Clear(ctx, accountID)
	}
	if d, err := s.Store.Cookies.GetDetails(ctx, accountID); err == nil {
		s.refreshAccountProfile(ctx, d)
	}
	if s.Manager != nil && s.Store.Cookies.GetStatus(ctx, accountID) {
		if err := s.Manager.Restart(ctx, accountID); err != nil && s.Logger != nil {
			s.Logger.Warn("密码登录后重启账号失败", "cookie_id", accountID, "err", err)
		}
	}
	return accountID, isNew, nil
}

func (s *Server) canStartPasswordLogin(ctx context.Context, userID int64, accountID string) (bool, string) {
	d, err := s.Store.Cookies.GetDetails(ctx, accountID)
	if err == nil {
		if d.UserID != userID {
			return false, "无权限操作该账号"
		}
		if !s.Store.Cookies.GetStatus(ctx, accountID) {
			return false, "账号已禁用，请先在账号管理中启用"
		}
		return true, ""
	}
	if errors.Is(err, db.ErrNotFound) {
		return true, ""
	}
	return false, "查询账号失败"
}

func (s *Server) currentPasswordLoginSession(accountID string) string {
	s.passwordMu.Lock()
	defer s.passwordMu.Unlock()
	return s.passwordProcessing[accountID]
}

func (s *Server) completePasswordLoginSession(session *passwordLoginSession, mutate func(*passwordLoginSession)) {
	s.finishPasswordLoginSession(session, mutate)
}

func (s *Server) updatePasswordLoginSession(session *passwordLoginSession, mutate func(*passwordLoginSession)) bool {
	s.passwordMu.Lock()
	defer s.passwordMu.Unlock()
	current, ok := s.passwordSessions[session.ID]
	if !ok || current != session {
		return false
	}
	mutate(current)
	return true
}

func (s *Server) finishPasswordLoginSession(session *passwordLoginSession, mutate func(*passwordLoginSession)) {
	s.passwordMu.Lock()
	defer s.passwordMu.Unlock()
	current, ok := s.passwordSessions[session.ID]
	if !ok || current != session {
		return
	}
	originalAccountID := current.AccountID
	mutate(current)
	delete(s.passwordProcessing, originalAccountID)
	delete(s.passwordProcessing, current.AccountID)
}

func (s *Server) applyPasswordLoginEvent(session *passwordLoginSession, event browser.PasswordLoginEvent) {
	if event.Status == "" {
		return
	}
	apply := func(current *passwordLoginSession) {
		current.Status = event.Status
		current.Message = firstNonEmpty(event.Message, current.Message)
		current.Error = firstNonEmpty(event.Error, current.Error)
		current.Reason = firstNonEmpty(event.Reason, current.Reason)
		current.VerificationURL = firstNonEmpty(event.VerificationURL, current.VerificationURL)
		current.ScreenshotPath = firstNonEmpty(event.ScreenshotPath, current.ScreenshotPath)
		current.QRCodeURL = firstNonEmpty(event.QRCodeURL, current.QRCodeURL)
		if event.CooldownHours > 0 {
			current.CooldownHours = event.CooldownHours
		}
	}
	if passwordLoginEventIsTerminal(event.Status) {
		s.finishPasswordLoginSession(session, apply)
		return
	}
	s.updatePasswordLoginSession(session, apply)
}

func (s *Server) failPasswordLoginSession(session *passwordLoginSession, status, message, reason string, cooldownHours int) {
	s.completePasswordLoginSession(session, func(current *passwordLoginSession) {
		current.Status = status
		current.Message = message
		current.Error = message
		current.Reason = reason
		current.CooldownHours = cooldownHours
	})
}

func passwordLoginEventIsTerminal(status string) bool {
	return status == "success" || status == browser.PasswordLoginStatusFailed
}

func (s *Server) cleanupExpiredPasswordLoginSessions() {
	now := time.Now()
	s.passwordMu.Lock()
	defer s.passwordMu.Unlock()
	for sessionID, session := range s.passwordSessions {
		age := now.Sub(session.CreatedAt)
		if session.Status == "processing" && age > passwordLoginProcessingTimeout {
			if session.cancel != nil {
				session.cancel()
			}
			session.Status = "failed"
			session.Message = "登录处理超时，请稍后重试"
			session.Error = session.Message
			delete(s.passwordProcessing, session.AccountID)
		}
		if age <= passwordLoginSessionMaxAge {
			continue
		}
		if session.cancel != nil {
			session.cancel()
		}
		delete(s.passwordSessions, sessionID)
		delete(s.passwordProcessing, session.AccountID)
	}
}

func passwordSessionResponse(session *passwordLoginSession) map[string]any {
	resp := map[string]any{
		"status":  session.Status,
		"message": session.Message,
	}
	if session.Status == "success" {
		resp["account_id"] = session.AccountID
		resp["is_new_account"] = session.IsNewAccount
		resp["cookie_count"] = session.CookieCount
	}
	if session.Status == "failed" {
		resp["error"] = session.Error
		if session.Reason != "" {
			resp["reason"] = session.Reason
		}
		if session.CooldownHours > 0 {
			resp["cooldown_hours"] = session.CooldownHours
		}
	}
	if session.Status == "verification_required" {
		resp["verification_url"] = session.VerificationURL
		resp["screenshot_path"] = session.ScreenshotPath
		resp["qr_code_url"] = session.QRCodeURL
		resp["error"] = session.Error
		if session.CooldownHours > 0 {
			resp["cooldown_hours"] = session.CooldownHours
		}
	}
	return resp
}

func newPasswordSessionID() (string, error) {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func cookieAccountID(cookies map[string]string, fallback string) string {
	for k, v := range cookies {
		if strings.EqualFold(strings.TrimSpace(k), "unb") && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return fallback
}

func loginStatusForPasswordError(err error) string {
	return browser.PasswordLoginEventFromError(err).Status
}

func passwordLoginReason(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if strings.Contains(msg, "账密错误") || strings.Contains(msg, "账号密码错误") ||
		strings.Contains(msg, "用户名或密码错误") || strings.Contains(msg, "密码错误") {
		return "bad_credentials"
	}
	event := browser.PasswordLoginEventFromError(err)
	if event.Reason != "" {
		return event.Reason
	}
	if event.Status == browser.PasswordLoginStatusVerificationRequired {
		return "verification_required"
	}
	return "other"
}
