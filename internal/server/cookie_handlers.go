package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/xianyu/cookierefresh"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// mountCookies 账号 cookie 管理端点。
func (s *Server) mountCookies(r chi.Router) {
	r.Get("/cookies", s.listCookies)
	r.Get("/cookies/details", s.listCookieDetails)
	r.Get("/cookies/runtime-status", s.listCookieRuntimeStatus)
	r.Post("/cookies", s.addCookie)
	r.Put("/cookies/{cid}", s.updateCookie)
	r.Put("/cookies/{cid}/login-info", s.updateCookieLoginInfo)
	r.Put("/cookies/{cid}/settings", s.updateCookieSettings)
	r.Get("/cookies/{cid}/long-login", s.getLongLoginSettings)
	r.Put("/cookies/{cid}/long-login", s.setLongLoginSettings)
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

func (s *Server) getLongLoginSettings(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	ownedDetail, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	credentialUnlock := s.Store.LockAccountCredentials(cid)
	detail, err := s.Store.Cookies.GetDetails(r.Context(), cid)
	if err != nil || detail == nil || detail.UserID != ownedDetail.UserID {
		credentialUnlock()
		writeErr(w, http.StatusNotFound, "账号不存在")
		return
	}
	requestCookies := scopedCookieHeader(detail, xrenew.QueryLoginSettingsURL)
	var result *xrenew.LongLoginSettings
	var requestErr error
	if snapshot, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); complete {
		result, requestErr = s.CookieRenew.QueryLongLoginSettings(r.Context(), requestCookies, snapshot)
	} else {
		result, requestErr = s.CookieRenew.QueryLongLoginSettings(r.Context(), requestCookies)
	}
	credentialChanged, persistErr := s.persistLongLoginCookies(r.Context(), detail, result, xrenew.QueryLoginSettingsURL)
	credentialUnlock()
	if persistErr != nil {
		writeErr(w, http.StatusInternalServerError, "保存续期 Cookie 失败")
		return
	}
	if credentialChanged {
		s.updateRunningCookie(r.Context(), cid, result.NewCookies)
	}
	if requestErr != nil {
		writeErr(w, http.StatusBadGateway, requestErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) setLongLoginSettings(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	ownedDetail, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Enabled == nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	credentialUnlock := s.Store.LockAccountCredentials(cid)
	detail, err := s.Store.Cookies.GetDetails(r.Context(), cid)
	if err != nil || detail == nil || detail.UserID != ownedDetail.UserID {
		credentialUnlock()
		writeErr(w, http.StatusNotFound, "账号不存在")
		return
	}
	requestCookies := scopedCookieHeader(detail, xrenew.SetLoginSettingsURL)
	var result *xrenew.LongLoginSettings
	var requestErr error
	if snapshot, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); complete {
		result, requestErr = s.CookieRenew.SetLongLoginSettings(r.Context(), requestCookies, *req.Enabled, snapshot)
	} else {
		result, requestErr = s.CookieRenew.SetLongLoginSettings(r.Context(), requestCookies, *req.Enabled)
	}
	credentialChanged, persistErr := s.persistLongLoginCookies(r.Context(), detail, result, xrenew.SetLoginSettingsURL)
	credentialUnlock()
	if persistErr != nil {
		writeErr(w, http.StatusInternalServerError, "保存续期 Cookie 失败")
		return
	}
	if credentialChanged {
		s.updateRunningCookie(r.Context(), cid, result.NewCookies)
	}
	if requestErr != nil {
		writeErr(w, http.StatusBadGateway, requestErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// persistLongLoginCookies 必须在账号凭证锁内调用。它只更新持久化状态；
// 运行中实例的 Cookie 更新必须等调用方释放凭证锁后再执行。
func (s *Server) persistLongLoginCookies(ctx context.Context, detail *db.CookieDetail, result *xrenew.LongLoginSettings, requestURL string) (bool, error) {
	if result == nil || detail == nil {
		return false, nil
	}
	metadata := detail.MetadataJSON
	snapshot, hasSnapshot := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	if result.CookieSnapshotComplete {
		snapshot = cookierefresh.NormalizeSnapshot(result.CookieSnapshot)
		if snapshot == nil {
			snapshot = []cookierefresh.BrowserCookie{}
		}
		result.NewCookies, _ = cookierefresh.ScopedCookieHeaderForRequest(
			snapshot, "https://www.goofish.com/im", "https://goofish.com", time.Now(),
		)
		metadata = cookierefresh.MetadataWithSnapshot(metadata, snapshot)
	} else if hasSnapshot {
		// 兼容非标准 Service 实现：只有未返回最终 Jar 时才按当前请求 URL
		// 应用响应头；官方 Service 会在 SET→QUERY 间逐请求维护完整 Jar。
		snapshot = cookierefresh.ApplySetCookies(snapshot, requestURL, result.SetCookies, time.Now(), "https://goofish.com")
		if snapshot == nil {
			snapshot = []cookierefresh.BrowserCookie{}
		}
		result.NewCookies, _ = cookierefresh.ScopedCookieHeaderForRequest(
			snapshot, "https://www.goofish.com/im", "https://goofish.com", time.Now(),
		)
		metadata = cookierefresh.MetadataWithSnapshot(metadata, snapshot)
	} else {
		// 历史账号只有扁平 Cookie 时只能合并兼容值，不能凭空推断完整
		// Domain/Path/HttpOnly/PartitionKey 属性。
		result.NewCookies = xrenew.MergeSetCookies(detail.Value, result.SetCookies)
		metadata = cookierefresh.MetadataWithoutSnapshot(metadata)
	}
	credentialChanged := result.NewCookies != detail.Value || metadata != detail.MetadataJSON
	if !credentialChanged && len(result.SetCookies) == 0 {
		return false, nil
	}
	if err := s.Store.Cookies.UpdateRenewalCookie(ctx, detail.ID, result.NewCookies, metadata, time.Now().Unix()); err != nil {
		s.Logger.Warn("保存长登录 Cookie 失败", "cookie_id", detail.ID, "err", err)
		return false, err
	}
	if credentialChanged && s.Store.Tokens != nil {
		if err := s.Store.Tokens.Clear(ctx, detail.ID); err != nil {
			s.Logger.Warn("长登录 Cookie 保存后清理旧连接凭证失败", "cookie_id", detail.ID, "err", err)
		}
	}
	return credentialChanged, nil
}

// updateFlatCookieOwnedLocked 用新的扁平 Cookie 覆盖账号时同步移除旧浏览器
// 快照，避免下一次浏览器取 token 又把旧 Cookie 注入回来。调用方必须持有
// 对应账号的凭证锁，并已验证 detail 的归属。
func (s *Server) updateFlatCookieOwnedLocked(ctx context.Context, detail *db.CookieDetail, value string) error {
	if detail == nil {
		return db.ErrNotFound
	}
	metadata := cookierefresh.MetadataWithoutSnapshot(detail.MetadataJSON)
	return s.Store.Cookies.UpdateRenewalCookie(ctx, detail.ID, value, metadata, time.Now().Unix())
}

func (s *Server) updateRunningCookie(ctx context.Context, cookieID, value string) {
	s.wakeCredentialBlockedAutomation(ctx, cookieID)
	if s.Manager == nil || !s.Store.Cookies.GetStatus(ctx, cookieID) {
		return
	}
	if sender, ok := s.Manager.GetInstance(cookieID); ok {
		sender.UpdateCookie(value)
	}
}

func (s *Server) wakeCredentialBlockedAutomation(ctx context.Context, cookieID string) {
	if s == nil || s.Store == nil || s.Store.Automation == nil {
		return
	}
	if err := s.Store.Automation.WakeCredentialBlocked(ctx, cookieID); err != nil && s.Logger != nil {
		s.Logger.Warn("Cookie 更新后唤醒自动化任务失败", "account", cookieID, "err", err)
	}
}

func scopedCookieHeader(detail *db.CookieDetail, requestURL string) string {
	if detail == nil {
		return ""
	}
	if snapshot, ok := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); ok {
		if header, authoritative := cookierefresh.ScopedCookieHeaderForRequest(snapshot, requestURL, "https://goofish.com", time.Now()); authoritative {
			return header
		}
	}
	return detail.Value
}

type updateCookieSettingsRequest struct {
	Cookie        *string  `json:"cookie"`
	Remark        *string  `json:"remark"`
	AutoConfirm   *bool    `json:"auto_confirm"`
	PauseDuration *int     `json:"pause_duration"`
	Username      *string  `json:"username"`
	LoginPassword *string  `json:"login_password"`
	ClearPassword bool     `json:"clear_password"`
	ShowBrowser   *bool    `json:"show_browser"`
	ChannelIDs    *[]int64 `json:"channel_ids"`
}

// updateCookieSettings 原子保存编辑弹窗中的账号字段和通知绑定。
func (s *Server) updateCookieSettings(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	detail, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	var req updateCookieSettingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.Cookie != nil && strings.TrimSpace(*req.Cookie) == "" {
		writeErr(w, http.StatusBadRequest, "Cookie 不能为空")
		return
	}
	if req.Remark != nil && utf8.RuneCountInString(*req.Remark) > 500 {
		writeErr(w, http.StatusBadRequest, "备注不能超过 500 个字符")
		return
	}
	if req.Username != nil && utf8.RuneCountInString(*req.Username) > 256 {
		writeErr(w, http.StatusBadRequest, "登录账号不能超过 256 个字符")
		return
	}
	if req.LoginPassword != nil && len(*req.LoginPassword) > 1024 {
		writeErr(w, http.StatusBadRequest, "登录密码长度超出限制")
		return
	}

	credentialUnlock := s.Store.LockAccountCredentials(cid)
	latestDetail, err := s.Store.Cookies.GetDetails(r.Context(), cid)
	if err != nil || latestDetail == nil || latestDetail.UserID != detail.UserID {
		credentialUnlock()
		writeErr(w, http.StatusNotFound, "账号不存在")
		return
	}
	detail = latestDetail

	input := db.AccountSettingsUpdate{
		UserID:        detail.UserID,
		Value:         req.Cookie,
		Remark:        req.Remark,
		AutoConfirm:   req.AutoConfirm,
		PauseDuration: req.PauseDuration,
		ChannelIDs:    req.ChannelIDs,
	}
	loginChanged := req.Username != nil || req.LoginPassword != nil || req.ShowBrowser != nil || req.ClearPassword
	if loginChanged {
		username := detail.Username
		if req.Username != nil {
			username = *req.Username
		}
		password := detail.Password
		if req.LoginPassword != nil && *req.LoginPassword != "" {
			password = *req.LoginPassword
		}
		if req.ClearPassword {
			password = ""
		}
		showBrowser := detail.ShowBrowser
		if req.ShowBrowser != nil {
			showBrowser = *req.ShowBrowser
		}
		input.Username = &username
		input.Password = &password
		input.ShowBrowser = &showBrowser
	}
	pausedUntil, err := s.Store.Cookies.UpdateSettings(r.Context(), cid, input)
	if err != nil {
		credentialUnlock()
		switch {
		case errors.Is(err, db.ErrForbidden):
			writeErr(w, http.StatusForbidden, "账号设置包含无权限使用的资源")
		case errors.Is(err, db.ErrNotFound):
			writeErr(w, http.StatusNotFound, "账号不存在")
		default:
			writeErr(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	if req.Cookie != nil {
		// UpdateSettings 已在同一事务中写入 Cookie 并清除旧快照，不能再做
		// 第二次非原子覆盖。
		if s.Store.Tokens != nil {
			if err := s.Store.Tokens.Clear(r.Context(), cid); err != nil {
				s.Logger.Warn("账号设置保存后清理旧连接凭证失败", "cookie_id", cid, "err", err)
			}
		}
	}
	credentialUnlock()
	if req.Cookie != nil && s.Manager != nil && s.Store.Cookies.GetStatus(r.Context(), cid) {
		if err := s.Manager.Restart(r.Context(), cid); err != nil {
			s.Logger.Error("账号设置保存后重启失败", "cookie_id", cid, "err", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true, "paused_until": pausedUntil, "paused": pausedUntil > time.Now().UTC().Unix(),
	})
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
		tasks, _ := s.Store.AccountTasks.Get(r.Context(), cid)
		result = append(result, map[string]any{
			"id":                  d.ID,
			"has_cookie":          true,
			"enabled":             s.Store.Cookies.GetStatus(r.Context(), cid),
			"auto_confirm":        d.AutoConfirm,
			"remark":              d.Remark,
			"pause_duration":      d.PauseDuration,
			"paused_until":        d.PausedUntil,
			"paused":              d.PausedUntil > time.Now().UTC().Unix(),
			"show_browser":        d.ShowBrowser,
			"username":            d.Username,
			"nickname":            cachedAccountNickname(d),
			"avatar_url":          d.AvatarURL,
			"login_method":        d.LoginMethod,
			"last_login_at":       d.LastLoginAt,
			"profile_error":       "",
			"ai_enabled":          false,
			"auto_rate_enabled":   tasks.AutoRateEnabled,
			"rate_content":        tasks.RateContent,
			"auto_polish_enabled": tasks.AutoPolishEnabled,
			"polish_time":         tasks.PolishTime,
			"last_rate_scan_at":   tasks.LastRateScanAt,
			"last_polish_date":    tasks.LastPolishDate,
			"last_polish_at":      tasks.LastPolishAt,
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
	tasks, _ := s.Store.AccountTasks.Get(r.Context(), cid)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":                  d.ID,
		"enabled":             s.Store.Cookies.GetStatus(r.Context(), cid),
		"auto_confirm":        d.AutoConfirm,
		"remark":              d.Remark,
		"pause_duration":      d.PauseDuration,
		"paused_until":        d.PausedUntil,
		"paused":              d.PausedUntil > time.Now().UTC().Unix(),
		"show_browser":        d.ShowBrowser,
		"username":            d.Username,
		"nickname":            cachedAccountNickname(d),
		"avatar_url":          d.AvatarURL,
		"login_method":        d.LoginMethod,
		"last_login_at":       d.LastLoginAt,
		"profile_error":       "",
		"has_cookie":          true,
		"auto_rate_enabled":   tasks.AutoRateEnabled,
		"rate_content":        tasks.RateContent,
		"auto_polish_enabled": tasks.AutoPolishEnabled,
		"polish_time":         tasks.PolishTime,
		"last_rate_scan_at":   tasks.LastRateScanAt,
		"last_polish_date":    tasks.LastPolishDate,
		"last_polish_at":      tasks.LastPolishAt,
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
	credentialUnlock := s.Store.LockAccountCredentials(req.ID)
	if err := s.Store.Cookies.CreateOwned(r.Context(), req.ID, req.Value, sess.UserID); err != nil {
		credentialUnlock()
		if errors.Is(err, db.ErrForbidden) {
			writeErr(w, http.StatusForbidden, "该账号ID已存在且不属于当前用户")
			return
		}
		if errors.Is(err, db.ErrAlreadyExists) {
			writeErr(w, http.StatusConflict, "该账号ID已存在，请使用更新账号功能")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.Store.Tokens != nil {
		if err := s.Store.Tokens.Clear(r.Context(), req.ID); err != nil {
			s.Logger.Warn("新增账号后清理旧连接凭证失败", "cookie_id", req.ID, "err", err)
		}
	}
	loginMethod := normalizeLoginMethod(req.LoginMethod)
	if loginMethod == "" {
		loginMethod = loginMethodManual
	}
	s.markSuccessfulLogin(r.Context(), req.ID, sess.UserID, loginMethod, "账号登录成功")
	credentialUnlock()
	if d, err := s.Store.Cookies.GetDetails(r.Context(), req.ID); err == nil {
		s.refreshAccountProfile(r.Context(), d)
	}
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
	ownedDetail, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
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
	credentialUnlock := s.Store.LockAccountCredentials(cid)
	detail, err := s.Store.Cookies.GetDetails(r.Context(), cid)
	if err != nil || detail == nil || detail.UserID != ownedDetail.UserID || detail.UserID != sess.UserID {
		credentialUnlock()
		writeErr(w, http.StatusNotFound, "账号不存在")
		return
	}
	if err := s.updateFlatCookieOwnedLocked(r.Context(), detail, req.Value); err != nil {
		credentialUnlock()
		if errors.Is(err, db.ErrForbidden) {
			writeErr(w, http.StatusForbidden, "无权限操作该账号")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.Store.Tokens != nil {
		if err := s.Store.Tokens.Clear(r.Context(), cid); err != nil {
			s.Logger.Warn("更新账号后清理旧连接凭证失败", "cookie_id", cid, "err", err)
		}
	}
	if loginMethod := normalizeLoginMethod(req.LoginMethod); loginMethod != "" {
		s.markSuccessfulLogin(r.Context(), cid, sess.UserID, loginMethod, "账号登录成功")
	}
	credentialUnlock()
	if d, err := s.Store.Cookies.GetDetails(r.Context(), cid); err == nil {
		s.refreshAccountProfile(r.Context(), d)
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
	ownedDetail, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	reason := ""
	if !req.Enabled {
		reason = db.DisableReasonManual
	}
	credentialUnlock := s.Store.LockAccountCredentials(cid)
	latest, err := s.Store.Cookies.GetDetails(r.Context(), cid)
	if err != nil || latest == nil || latest.UserID != ownedDetail.UserID {
		credentialUnlock()
		writeErr(w, http.StatusNotFound, "账号不存在")
		return
	}
	if err := s.Store.Cookies.SetStatusWithReason(r.Context(), cid, req.Enabled, reason); err != nil {
		credentialUnlock()
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	credentialUnlock()
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
	ownedDetail, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	credentialUnlock := s.Store.LockAccountCredentials(cid)
	latest, err := s.Store.Cookies.GetDetails(r.Context(), cid)
	if err != nil || latest == nil || latest.UserID != ownedDetail.UserID {
		credentialUnlock()
		writeErr(w, http.StatusNotFound, "账号不存在")
		return
	}
	if err := s.Store.Cookies.Delete(r.Context(), cid); err != nil {
		credentialUnlock()
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	credentialUnlock()
	s.Logger.Info("账号已删除",
		"cookie_id", cid,
		"nickname", cachedAccountNickname(latest),
		"user_id", latest.UserID,
	)
	// Stop 可能需要等待运行中任务收尾，不应阻塞删除 HTTP 请求。
	// 数据库事务已完成，先向前端确认删除，再在后台精确停止该 cid 的实例。
	if s.Manager != nil {
		go s.Manager.Stop(cid)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// setCookieAutoConfirm 设置自动确认发货。
func (s *Server) setCookieAutoConfirm(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
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
	if !s.requireCookieOwnership(w, r, cid) {
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
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	var req struct {
		PauseDuration int `json:"pause_duration"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.PauseDuration < 0 || req.PauseDuration > 1440 {
		writeErr(w, http.StatusBadRequest, "暂停时长必须在 0 到 1440 分钟之间")
		return
	}
	pausedUntil, err := s.Store.Cookies.SetPause(r.Context(), cid, req.PauseDuration)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存暂停时长失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true, "paused_until": pausedUntil, "paused": pausedUntil > time.Now().UTC().Unix(),
	})
}

// getCookiePauseDuration 获取暂停时长。
func (s *Server) getCookiePauseDuration(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	paused, pausedUntil, _ := s.Store.Cookies.IsPaused(r.Context(), cid)
	writeJSON(w, http.StatusOK, map[string]any{
		"pause_duration": s.Store.Cookies.GetPauseDuration(r.Context(), cid),
		"paused_until":   pausedUntil,
		"paused":         paused,
	})
}

func (s *Server) refreshAccountProfile(ctx context.Context, d *db.CookieDetail) (string, string, string) {
	if d == nil {
		return "", "", ""
	}
	if s.MTop == nil {
		return cachedAccountNickname(d), d.AvatarURL, "账号资料客户端未初始化"
	}

	credentialUnlock := s.Store.LockAccountCredentials(d.ID)
	latest, latestErr := s.Store.Cookies.GetDetails(ctx, d.ID)
	if latestErr != nil || latest == nil || latest.UserID != d.UserID {
		credentialUnlock()
		if latestErr == nil {
			latestErr = db.ErrNotFound
		}
		return cachedAccountNickname(d), d.AvatarURL, truncate(latestErr.Error(), 180)
	}
	mtopCtx, cookieSession := withMTopCookieSnapshot(ctx, latest)
	profile, callErr := s.MTop.FetchUserProfile(mtopCtx, latest.Value)
	runtimeCookie := ""
	runtimeCookieChanged := false
	value, valueChanged, handled, persistErr := s.persistMTopCookieSessionLocked(ctx, latest, cookieSession)
	if persistErr != nil {
		if s.Logger != nil {
			s.Logger.Warn("保存账号资料响应 Cookie Jar 失败", "account", d.ID, "err", persistErr)
		}
		callErr = errors.Join(callErr, fmt.Errorf("保存账号资料响应凭证: %w", persistErr))
	} else if handled {
		if valueChanged {
			runtimeCookie = value
			runtimeCookieChanged = true
		}
		d.Value = value
	} else if callErr == nil && profile != nil && profile.UpdatedCookies != "" && profile.UpdatedCookies != latest.Value {
		// 注入 mock 或没有权威快照的历史账号继续沿用扁平 Cookie 路径；
		// 该路径必须清除旧 snapshot，不能伪造完整 Jar。
		if err := s.updateFlatCookieOwnedLocked(ctx, latest, profile.UpdatedCookies); err != nil {
			if s.Logger != nil {
				s.Logger.Warn("保存账号刷新 cookie 失败", "account", d.ID, "err", err)
			}
			callErr = errors.Join(callErr, fmt.Errorf("保存账号资料响应凭证: %w", err))
		} else {
			runtimeCookie = profile.UpdatedCookies
			runtimeCookieChanged = true
			d.Value = profile.UpdatedCookies
		}
	}
	credentialUnlock()
	if runtimeCookieChanged {
		s.updateRunningCookie(ctx, d.ID, runtimeCookie)
	}
	if callErr != nil {
		s.recoverExpiredMTOPSession(ctx, d.ID, callErr)
		if s.Logger != nil {
			s.Logger.Warn("刷新账号资料失败", "account", d.ID, "err", callErr)
		}
		return cachedAccountNickname(d), d.AvatarURL, truncate(callErr.Error(), 180)
	}
	if profile == nil {
		return cachedAccountNickname(d), d.AvatarURL, "账号资料接口未返回结果"
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
