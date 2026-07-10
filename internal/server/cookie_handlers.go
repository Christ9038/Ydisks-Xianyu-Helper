package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
)

// mountCookies 账号 cookie 管理端点。
func (s *Server) mountCookies(r chi.Router) {
	r.Get("/cookies", s.listCookies)
	r.Get("/cookies/details", s.listCookieDetails)
	r.Get("/cookies/runtime-status", s.listCookieRuntimeStatus)
	r.Post("/cookies", s.addCookie)
	r.Put("/cookies/{cid}", s.updateCookie)
	r.Put("/cookies/{cid}/login-info", s.updateCookieLoginInfo)
	r.Post("/cookies/{cid}/refresh-profile", s.refreshCookieProfile)
	r.Get("/cookie/{cid}/details", s.getCookieDetails)
	r.Put("/cookies/{cid}/status", s.setCookieStatus)
	r.Delete("/cookies/{cid}", s.deleteCookie)
	r.Put("/cookies/{cid}/auto-confirm", s.setCookieAutoConfirm)
	r.Get("/cookies/{cid}/auto-confirm", s.getCookieAutoConfirm)
	r.Put("/cookies/{cid}/remark", s.setCookieRemark)
	r.Put("/cookies/{cid}/pause-duration", s.setCookiePauseDuration)
	r.Get("/cookies/{cid}/pause-duration", s.getCookiePauseDuration)
}

// listCookieRuntimeStatus 返回本地账号引擎状态，不请求闲鱼 API，可安全用于前端轮询。
func (s *Server) listCookieRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	all, err := s.Store.Cookies.AllForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "获取账号失败")
		return
	}
	runtime := map[string]engine.RuntimeStatus{}
	if s.Manager != nil {
		runtime = s.Manager.RuntimeStatuses()
	}
	result := make(map[string]engine.RuntimeStatus, len(all))
	for cid := range all {
		if !s.Store.Cookies.GetStatus(r.Context(), cid) {
			result[cid] = engine.RuntimeStatus{State: "disabled", Message: "账号已停用", UpdatedAt: time.Now()}
			continue
		}
		if status, ok := runtime[cid]; ok {
			result[cid] = status
			continue
		}
		result[cid] = engine.RuntimeStatus{State: engine.RuntimeError, Message: "账号服务未运行", UpdatedAt: time.Now()}
	}
	writeJSON(w, http.StatusOK, result)
}

// listCookies 列出当前用户的 cookie_id。
func (s *Server) listCookies(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	all, err := s.Store.Cookies.AllForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "获取账号失败")
		return
	}
	ids := make([]string, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	writeJSON(w, http.StatusOK, ids)
}

// listCookieDetails 账号非敏感详情（不含 cookie 明文/密码，遵循 Fork 安全基线）。
func (s *Server) listCookieDetails(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	all, err := s.Store.Cookies.AllForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "获取账号失败")
		return
	}
	result := make([]map[string]any, 0, len(all))
	for cid := range all {
		d, err := s.Store.Cookies.GetDetails(r.Context(), cid)
		if err != nil || d == nil {
			continue
		}
		result = append(result, map[string]any{
			"id":             d.ID,
			"has_cookie":     true,
			"enabled":        s.Store.Cookies.GetStatus(r.Context(), cid),
			"auto_confirm":   d.AutoConfirm,
			"remark":         d.Remark,
			"pause_duration": d.PauseDuration,
			"show_browser":   d.ShowBrowser,
			"username":       d.Username,
			"nickname":       cachedAccountNickname(d),
			"avatar_url":     d.AvatarURL,
			"login_method":   d.LoginMethod,
			"last_login_at":  d.LastLoginAt,
			"profile_error":  "",
			"ai_enabled":     false,
		})
	}
	writeJSON(w, http.StatusOK, result)
}

// getCookieDetails 单个账号非敏感详情。
func (s *Server) getCookieDetails(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	sess := auth.SessionFromContext(r.Context())
	all, _ := s.Store.Cookies.AllForUser(r.Context(), sess.UserID)
	if _, ok := all[cid]; !ok {
		writeErr(w, http.StatusForbidden, "无权限操作该Cookie")
		return
	}
	d, err := s.Store.Cookies.GetDetails(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusNotFound, "账号不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":             d.ID,
		"enabled":        s.Store.Cookies.GetStatus(r.Context(), cid),
		"auto_confirm":   d.AutoConfirm,
		"remark":         d.Remark,
		"pause_duration": d.PauseDuration,
		"show_browser":   d.ShowBrowser,
		"username":       d.Username,
		"nickname":       cachedAccountNickname(d),
		"avatar_url":     d.AvatarURL,
		"login_method":   d.LoginMethod,
		"last_login_at":  d.LastLoginAt,
		"profile_error":  "",
		"has_cookie":     true,
	})
}

// refreshCookieProfile 主动刷新账号昵称/头像。列表接口不自动刷新，避免 100 个账号时对闲鱼打 100 次请求。
func (s *Server) refreshCookieProfile(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	sess := auth.SessionFromContext(r.Context())
	all, _ := s.Store.Cookies.AllForUser(r.Context(), sess.UserID)
	if _, ok := all[cid]; !ok {
		writeErr(w, http.StatusForbidden, "无权限操作该账号")
		return
	}
	d, err := s.Store.Cookies.GetDetails(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusNotFound, "账号不存在")
		return
	}
	nickname, avatarURL, profileErr := s.refreshAccountProfile(r.Context(), d)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":       profileErr == "",
		"id":            d.ID,
		"nickname":      nickname,
		"avatar_url":    avatarURL,
		"profile_error": profileErr,
	})
}

// addCookie 添加账号 cookie。
func (s *Server) addCookie(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID          string `json:"id"`
		Value       string `json:"value"`
		LoginMethod string `json:"login_method"`
	}
	if err := decodeJSON(r, &req); err != nil || req.ID == "" || req.Value == "" {
		writeErr(w, http.StatusBadRequest, "缺少 id 或 value")
		return
	}
	sess := auth.SessionFromContext(r.Context())
	if err := s.Store.Cookies.Save(r.Context(), req.ID, req.Value, sess.UserID); err != nil {
		if errors.Is(err, db.ErrForbidden) {
			writeErr(w, http.StatusForbidden, "该账号ID已存在且不属于当前用户")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d, err := s.Store.Cookies.GetDetails(r.Context(), req.ID); err == nil {
		s.refreshAccountProfile(r.Context(), d)
	}
	loginMethod := normalizeLoginMethod(req.LoginMethod)
	if loginMethod == "" {
		loginMethod = loginMethodManual
	}
	s.markSuccessfulLogin(r.Context(), req.ID, sess.UserID, loginMethod, "账号登录成功")
	if s.Manager != nil && s.Store.Cookies.GetStatus(r.Context(), req.ID) {
		if err := s.Manager.Restart(r.Context(), req.ID); err != nil {
			s.Logger.Error("更新后重启账号失败", "cookie_id", req.ID, "err", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "id": req.ID})
}

// updateCookie 更新 cookie 值。
func (s *Server) updateCookie(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	var req struct {
		Value       string `json:"value"`
		LoginMethod string `json:"login_method"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	sess := auth.SessionFromContext(r.Context())
	if err := s.Store.Cookies.Save(r.Context(), cid, req.Value, sess.UserID); err != nil {
		if errors.Is(err, db.ErrForbidden) {
			writeErr(w, http.StatusForbidden, "无权限操作该账号")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d, err := s.Store.Cookies.GetDetails(r.Context(), cid); err == nil {
		s.refreshAccountProfile(r.Context(), d)
	}
	if loginMethod := normalizeLoginMethod(req.LoginMethod); loginMethod != "" {
		s.markSuccessfulLogin(r.Context(), cid, sess.UserID, loginMethod, "账号登录成功")
	}
	if s.Manager != nil && s.Store.Cookies.GetStatus(r.Context(), cid) {
		if err := s.Manager.Restart(r.Context(), cid); err != nil {
			s.Logger.Error("更新后重启账号失败", "cookie_id", cid, "err", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// updateCookieLoginInfo 更新账号登录信息（用户名/密码/显示浏览器）。
func (s *Server) updateCookieLoginInfo(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	detail, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	var req struct {
		Username      string `json:"username"`
		Password      string `json:"password"`
		LoginPassword string `json:"login_password"`
		ShowBrowser   bool   `json:"show_browser"`
		ClearPassword bool   `json:"clear_password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	password := req.Password
	if password == "" {
		password = req.LoginPassword
	}
	if req.ClearPassword {
		password = ""
	} else if password == "" && detail != nil {
		password = detail.Password
	}
	if err := s.Store.Cookies.UpdateLoginInfo(r.Context(), cid, req.Username, password, req.ShowBrowser); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// setCookieStatus 启用/禁用账号。
func (s *Server) setCookieStatus(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := s.Store.Cookies.SetStatus(r.Context(), cid, req.Enabled); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	// 启停引擎实例。
	if s.Manager != nil {
		if req.Enabled {
			// 重启拉取最新 cookie。
			if _, e := s.Store.Cookies.GetDetails(r.Context(), cid); e == nil {
				if err := s.Manager.Restart(r.Context(), cid); err != nil {
					s.Logger.Error("启用后重启账号失败", "cookie_id", cid, "err", err)
				}
			}
		} else {
			s.Manager.Stop(cid)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// deleteCookie 删除账号。
func (s *Server) deleteCookie(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	if s.Manager != nil {
		s.Manager.Stop(cid)
	}
	if err := s.Store.Cookies.Delete(r.Context(), cid); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// setCookieAutoConfirm 设置自动确认发货。
func (s *Server) setCookieAutoConfirm(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	var req struct {
		AutoConfirm bool `json:"auto_confirm"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	v := 0
	if req.AutoConfirm {
		v = 1
	}
	if _, err := s.Store.DB.ExecContext(r.Context(),
		`UPDATE cookies SET auto_confirm=? WHERE id=?`, v, cid); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存自动确认设置失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// getCookieAutoConfirm 获取自动确认发货设置。
func (s *Server) getCookieAutoConfirm(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	d, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"auto_confirm": d.AutoConfirm})
}

// setCookieRemark 设置备注。
func (s *Server) setCookieRemark(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	var req struct {
		Remark string `json:"remark"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if _, err := s.Store.DB.ExecContext(r.Context(),
		`UPDATE cookies SET remark=? WHERE id=?`, req.Remark, cid); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存账号备注失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// setCookiePauseDuration 设置暂停时长。
func (s *Server) setCookiePauseDuration(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	var req struct {
		PauseDuration int `json:"pause_duration"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.PauseDuration < 0 {
		writeErr(w, http.StatusBadRequest, "暂停时长不能为负数")
		return
	}
	if _, err := s.Store.DB.ExecContext(r.Context(),
		`UPDATE cookies SET pause_duration=? WHERE id=?`, req.PauseDuration, cid); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存暂停时长失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// getCookiePauseDuration 获取暂停时长。
func (s *Server) getCookiePauseDuration(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pause_duration": s.Store.Cookies.GetPauseDuration(r.Context(), cid)})
}

func (s *Server) refreshAccountProfile(ctx context.Context, d *db.CookieDetail) (string, string, string) {
	if d == nil {
		return "", "", ""
	}
	if s.MTop == nil {
		return cachedAccountNickname(d), d.AvatarURL, "账号资料客户端未初始化"
	}

	profile, err := s.MTop.FetchUserProfile(ctx, d.Value)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("刷新账号资料失败", "account", d.ID, "err", err)
		}
		return cachedAccountNickname(d), d.AvatarURL, truncate(err.Error(), 180)
	}

	if profile.UpdatedCookies != "" && profile.UpdatedCookies != d.Value {
		if err := s.Store.Cookies.Save(ctx, d.ID, profile.UpdatedCookies, d.UserID); err != nil && s.Logger != nil {
			s.Logger.Warn("保存账号刷新 cookie 失败", "account", d.ID, "err", err)
		}
		d.Value = profile.UpdatedCookies
	}

	apiNickname := strings.TrimSpace(profile.Nickname)
	apiAvatarURL := normalizeProfileAvatarURL(profile.AvatarURL)
	if err := s.Store.Cookies.UpdateProfile(ctx, d.ID, apiNickname, apiAvatarURL); err != nil && s.Logger != nil {
		s.Logger.Warn("保存账号资料失败", "account", d.ID, "err", err)
	}
	if apiNickname == "" {
		apiNickname = cachedAccountNickname(d)
	}
	if apiAvatarURL == "" {
		apiAvatarURL = d.AvatarURL
	}
	return apiNickname, apiAvatarURL, ""
}

func cachedAccountNickname(d *db.CookieDetail) string {
	if strings.TrimSpace(d.Remark) != "" {
		return strings.TrimSpace(d.Remark)
	}
	if strings.TrimSpace(d.Nickname) != "" {
		return strings.TrimSpace(d.Nickname)
	}
	return "账号 " + truncate(d.ID, 6)
}

func normalizeProfileAvatarURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") {
		return "https://" + strings.TrimPrefix(raw, "http://")
	}
	return raw
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
