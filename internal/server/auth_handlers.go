package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/auth"
)

// loginRequest 是登录请求体。
type loginRequest struct {
	Username         string `json:"username,omitempty"`
	Email            string `json:"email,omitempty"`
	Password         string `json:"password,omitempty"`
	VerificationCode string `json:"verification_code,omitempty"`
}

// loginResponse 是登录响应体。
type loginResponse struct {
	Success  bool    `json:"success"`
	Token    *string `json:"token"`
	Message  string  `json:"message"`
	UserID   int64   `json:"user_id,omitempty"`
	Username string  `json:"username,omitempty"`
	IsAdmin  bool    `json:"is_admin"`
}

// initializeRequest 是首次启动创建管理员的 HTTP 请求 DTO。
type initializeRequest struct {
	// Password 是首次管理员账户必须设置的明文密码，仅在本次请求作用域内使用。
	Password string `json:"password"`
}

// passwordChangeRequest 是已登录用户修改密码的 HTTP 请求 DTO。
type passwordChangeRequest struct {
	// CurrentPassword 是确认当前会话所有权所需的现有密码。
	CurrentPassword string `json:"current_password"`
	// NewPassword 是长度校验后替换现有秘密的新密码。
	NewPassword string `json:"new_password"`
}

// credentialUpdateRequest 是当前用户修改用户名或密码的 HTTP 请求 DTO。
type credentialUpdateRequest struct {
	// CurrentPassword 是敏感凭证变更前的身份确认密码。
	CurrentPassword string `json:"current_password"`
	// NewUsername 是可选的新登录用户名。
	NewUsername string `json:"new_username"`
	// NewPassword 是可选的新登录密码。
	NewPassword string `json:"new_password"`
}

// initialize 首次启动时通过 Web UI 创建管理员账号。
// 该接口只允许在系统尚未存在管理员时调用，避免把初始化能力变成重置密码入口。
// initialize 封装initialize业务协调。
func (s *Server) initialize(w http.ResponseWriter, r *http.Request) {
	// req 是首次管理员初始化请求的具名传输 DTO。
	var req initializeRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if utf8.RuneCountInString(req.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "密码至少需要 8 个字符")
		return
	}

	// 同一进程内串行化初始化，避免两个首次打开的页面同时重置管理员密码。
	s.initializationMu.Lock()
	defer s.initializationMu.Unlock()

	// initialized、err 用于本次流程后续判断的initialized、err
	initialized, err := s.authenticationApplication().IsSystemInitialized(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "检查系统初始化状态失败")
		return
	}
	if initialized {
		writeErr(w, http.StatusConflict, "系统已经初始化，请直接登录")
		return
	}

	if // err 用于本次流程后续判断的err
	_, err := s.authenticationApplication().InitializeAdmin(r.Context(), "admin@example.com", req.Password); err != nil {
		writeErr(w, http.StatusInternalServerError, "初始化管理员失败")
		return
	}

	// 初始化完成后立即建立会话，用户无需再手动输入一次 admin 账号和密码。
	sid, user, err := s.authenticationApplication().Login(r.Context(), "admin", req.Password)
	if err != nil || user == nil || sid == "" {
		writeErr(w, http.StatusInternalServerError, "初始化完成，但自动登录失败，请使用 admin 账号登录")
		return
	}
	s.Auth.SetSessionCookie(w, sid)
	writeJSON(w, http.StatusOK, loginResponse{
		Success: true, Message: "初始化成功", UserID: user.ID,
		Username: user.Username, IsAdmin: user.IsAdmin,
	})
}

// login 用户名密码登录（邮箱登录同走此接口，按字段判断）。
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	// req 用于本次流程后续判断的req
	var req loginRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// clientIP 用于本次流程后续判断的clientIP
	clientIP := loginClientIP(r)
	// principal 用于本次流程后续判断的principal
	principal := loginPrincipal(req.Username, req.Email)
	if // allowed、retry 用于本次流程后续判断的allowed、retry
	allowed, retry := s.loginLimiter.allow(clientIP, principal, time.Now()); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retry.Round(time.Second)/time.Second))))
		writeErr(w, http.StatusTooManyRequests, "登录尝试过于频繁，请稍后再试")
		return
	}

	// resp 用于本次流程后续判断的resp
	var resp loginResponse

	switch {
	case req.Username != "" && req.Password != "":
		// sid、user、err 用于本次流程后续判断的sid、user、err
		sid, user, err := s.authenticationApplication().Login(r.Context(), req.Username, req.Password)
		if err != nil || user == nil || sid == "" {
			s.loginLimiter.failure(clientIP, principal, time.Now())
			writeErrCode(w, http.StatusUnauthorized, "authentication_failed",
				"用户名或密码错误", "")
			return
		}
		resp = loginResponse{
			Success: true, Token: nil, Message: "登录成功",
			UserID: user.ID, Username: user.Username,
			IsAdmin: user.IsAdmin,
		}
		s.Auth.SetSessionCookie(w, sid)
		s.loginLimiter.success(clientIP, principal)
		writeJSON(w, http.StatusOK, resp)
		return
	case req.Email != "" && req.Password != "":
		// username、err 用于本次流程后续判断的邮箱映射得到的登录用户名和查询错误
		username, err := s.authenticationApplication().UsernameByEmail(r.Context(), req.Email)
		if err != nil || username == "" {
			s.loginLimiter.failure(clientIP, principal, time.Now())
			writeErrCode(w, http.StatusUnauthorized, "authentication_failed",
				"邮箱或密码错误", "")
			return
		}
		// sid、loginUser、lerr 用于本次流程后续判断的sid、loginUser、lerr
		sid, loginUser, lerr := s.authenticationApplication().Login(r.Context(), username, req.Password)
		if lerr != nil || loginUser == nil || sid == "" {
			s.loginLimiter.failure(clientIP, principal, time.Now())
			writeErrCode(w, http.StatusUnauthorized, "authentication_failed",
				"邮箱或密码错误", "")
			return
		}
		resp = loginResponse{
			Success: true, Token: nil, Message: "登录成功",
			UserID: loginUser.ID, Username: loginUser.Username,
			IsAdmin: loginUser.IsAdmin,
		}
		s.Auth.SetSessionCookie(w, sid)
		s.loginLimiter.success(clientIP, principal)
		writeJSON(w, http.StatusOK, resp)
		return
	default:
		writeErr(w, http.StatusBadRequest, "缺少登录凭据")
	}
}

// verify 校验会话有效性。返回 authenticated / initialized / is_admin。
func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	// ctx 用于本次流程后续判断的ctx
	ctx := r.Context()
	// initialized 用于本次流程后续判断的initialized
	initialized, _ := s.authenticationApplication().IsSystemInitialized(ctx)
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(ctx)
	if sess != nil {
		writeJSON(w, http.StatusOK, sessionVerificationResponse{
			Authenticated: true,
			UserID:        sess.UserID,
			Username:      sess.Username,
			IsAdmin:       sess.IsAdmin,
			Initialized:   initialized,
		})
		return
	}
	writeJSON(w, http.StatusOK, sessionVerificationResponse{
		Authenticated: false,
		Initialized:   initialized,
	})
}

// logout 登出。
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	if sess != nil {
		s.Auth.Logout(r.Context(), sess.SessionID)
	}
	s.Auth.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, messageResponse{Message: "已登出"})
}

// changeAdminPassword 修改管理员密码。
func (s *Server) changeAdminPassword(w http.ResponseWriter, r *http.Request) {
	// req 是管理员密码修改请求的具名传输 DTO。
	var req passwordChangeRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if utf8.RuneCountInString(req.NewPassword) < 8 {
		writeErr(w, http.StatusBadRequest, "新密码至少需要 8 个字符")
		return
	}
	// ctx 用于本次流程后续判断的ctx
	ctx := r.Context()
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(ctx)
	if sess == nil || !sess.IsAdmin {
		writeErr(w, http.StatusForbidden, "仅管理员可执行此操作")
		return
	}
	// 校验当前密码。
	_, ok, _ := s.authenticationApplication().VerifyPassword(ctx, sess.Username, req.CurrentPassword)
	if !ok {
		writeErrCode(w, http.StatusUnauthorized, "authentication_failed", "当前密码错误", "")
		return
	}
	if // err 用于本次流程后续判断的err
	_, err := s.authenticationApplication().UpdatePassword(ctx, sess.Username, req.NewPassword); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	s.Auth.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, operationResponse{Success: true, Message: "密码修改成功，请重新登录", RequiresRelogin: true})
}

// changePassword 修改当前用户密码。
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	// req 是当前用户密码修改请求的具名传输 DTO。
	var req passwordChangeRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if utf8.RuneCountInString(req.NewPassword) < 8 {
		writeErr(w, http.StatusBadRequest, "新密码至少需要 8 个字符")
		return
	}
	// ctx 用于本次流程后续判断的ctx
	ctx := r.Context()
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(ctx)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// ok 保存当前密码是否匹配，应用层不会向 HTTP 层暴露密码哈希。
	_, ok, _ := s.authenticationApplication().VerifyPassword(ctx, sess.Username, req.CurrentPassword)
	if !ok {
		writeErrCode(w, http.StatusUnauthorized, "authentication_failed", "当前密码错误", "")
		return
	}
	if // err 用于本次流程后续判断的err
	_, err := s.authenticationApplication().UpdatePassword(ctx, sess.Username, req.NewPassword); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	s.Auth.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, operationResponse{Success: true, Message: "密码修改成功，请重新登录", RequiresRelogin: true})
}

// updateCredentials 修改当前登录用户的用户名和/或密码，并撤销全部旧会话。
func (s *Server) updateCredentials(w http.ResponseWriter, r *http.Request) {
	// req 是当前用户登录凭证修改请求的具名传输 DTO。
	var req credentialUpdateRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// username 用于本次流程后续判断的username
	username := strings.TrimSpace(req.NewUsername)
	// usernameLength 用于本次流程后续判断的usernameLength
	usernameLength := utf8.RuneCountInString(username)
	if usernameLength < 3 || usernameLength > 64 {
		writeErr(w, http.StatusBadRequest, "用户名长度必须为 3 到 64 个字符")
		return
	}
	if strings.TrimSpace(req.CurrentPassword) == "" {
		writeErr(w, http.StatusBadRequest, "请输入当前密码")
		return
	}
	if req.NewPassword != "" && utf8.RuneCountInString(req.NewPassword) < 8 {
		writeErr(w, http.StatusBadRequest, "新密码至少需要 8 个字符")
		return
	}
	if username == sess.Username && req.NewPassword == "" {
		writeErr(w, http.StatusBadRequest, "用户名和密码均未修改")
		return
	}
	// user、ok、err 用于本次流程后续判断的user、ok、err
	user, ok, err := s.authenticationApplication().VerifyPassword(r.Context(), sess.Username, req.CurrentPassword)
	if err != nil {
		writeCredentialVerificationError(w, err)
		return
	}
	if !ok || user.ID != sess.UserID {
		writeErrCode(w, http.StatusUnauthorized, "authentication_failed", "当前密码错误", "")
		return
	}
	if // err 用于本次流程后续判断的err
	err := s.authenticationApplication().UpdateCredentials(r.Context(), sess.UserID, username, req.NewPassword); err != nil {
		if errors.Is(err, accountapp.ErrUsernameTaken) {
			writeErrCode(w, http.StatusConflict, "username_taken", "用户名已被占用", "")
			return
		}
		writeErr(w, http.StatusInternalServerError, "更新登录凭据失败")
		return
	}
	s.Auth.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, operationResponse{
		Success:         true,
		Message:         "登录凭据已更新，请使用新用户名和密码重新登录",
		RequiresRelogin: true,
	})
}
