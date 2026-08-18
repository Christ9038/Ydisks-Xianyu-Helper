package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	accountapp "xianyu-go/internal/application/account"
	automationapp "xianyu-go/internal/application/automation"
	"xianyu-go/internal/auth"
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

// getLongLoginSettings 封装getLong登录设置业务协调。
func (s *Server) getLongLoginSettings(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cid")
	// ownedDetail、ok 保存账号摘要和归属校验结果。
	ownedDetail, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	// result 保存应用服务返回的不含凭证的长登录状态。
	result, err := s.accountLongLoginApplication().Query(r.Context(), ownedDetail.UserID, cid)
	if err != nil {
		s.writeLongLoginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, longLoginResponse{CanOpenLongLogin: result.CanOpenLongLogin, Enabled: result.Enabled})
}

// setLongLoginSettings 封装setLong登录设置业务协调。
func (s *Server) setLongLoginSettings(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cid")
	// ownedDetail、ok 用于本次流程后续判断的ownedDetail、ok
	ownedDetail, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	// req 用于本次流程后续判断的req
	var req longLoginSettingsRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil || req.Enabled == nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// result 保存应用服务返回的不含凭证的长登录状态。
	result, err := s.accountLongLoginApplication().Set(r.Context(), ownedDetail.UserID, cid, *req.Enabled)
	if err != nil {
		s.writeLongLoginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, longLoginResponse{CanOpenLongLogin: result.CanOpenLongLogin, Enabled: result.Enabled})
}

// writeLongLoginError 将长登录应用错误映射为兼容现有客户端的 HTTP 状态。
func (s *Server) writeLongLoginError(w http.ResponseWriter, err error) {
	if errors.Is(err, accountapp.ErrForbidden) || errors.Is(err, accountapp.ErrNotFound) || errors.Is(err, accountapp.ErrCredentialNotFound) {
		writeErr(w, http.StatusNotFound, "账号不存在")
		return
	}
	if errors.Is(err, accountapp.ErrLongLoginPlatform) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if strings.Contains(err.Error(), "平台") || strings.Contains(err.Error(), "HTTP") {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeErr(w, http.StatusInternalServerError, "保存续期 Cookie 失败")
}

// updateRunningCookie 封装updateRunning登录凭证业务协调。
func (s *Server) updateRunningCookie(ctx context.Context, cookieID, value string) {
	// runtimeService 负责唤醒凭证阻塞任务并将 Cookie 同步到账号运行实例。
	runtimeService := s.accountRuntimeApplication()
	if runtimeService == nil {
		return
	}
	// runtimeErr 保存账号运行时同步或自动化唤醒错误。
	if runtimeErr := runtimeService.UpdateCookie(ctx, cookieID, value); runtimeErr != nil && s.Logger != nil {
		s.Logger.Warn("Cookie 更新后同步账号运行时失败", "cookie_id", cookieID, "err", runtimeErr)
	}
}

// updateCookieSettingsRequest 用于本次流程后续判断的update登录凭证设置请求
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

// longLoginSettingsRequest 是更新账号长登录开关的 HTTP 请求 DTO。
type longLoginSettingsRequest struct {
	// Enabled 是必须明确提供的长登录开关值。
	Enabled *bool `json:"enabled"`
}

// createCookieRequest 是创建账号 Cookie 的 HTTP 请求 DTO。
type createCookieRequest struct {
	// ID 是调用方提供的账号标识。
	ID string `json:"id"`
	// Value 是仅在本次请求作用域内写入的明文 Cookie。
	Value string `json:"value"`
	// LoginMethod 是用于审计的可选登录方式。
	LoginMethod string `json:"login_method"`
}

// cookieLoginInfoRequest 是更新账号登录资料的 HTTP 请求 DTO。
type cookieLoginInfoRequest struct {
	// Username 是可选的平台登录用户名。
	Username string `json:"username"`
	// Password 是当前客户端使用的登录密码字段。
	Password string `json:"password"`
	// LoginPassword 是历史客户端使用的兼容密码字段。
	LoginPassword string `json:"login_password"`
	// ShowBrowser 表示密码登录时是否展示浏览器窗口。
	ShowBrowser bool `json:"show_browser"`
	// ClearPassword 明确请求清除已保存的登录密码。
	ClearPassword bool `json:"clear_password"`
}

// cookieStatusRequest 是启用或停用账号运行时的 HTTP 请求 DTO。
type cookieStatusRequest struct {
	// Enabled 表示账号是否应保持启用状态。
	Enabled bool `json:"enabled"`
}

// cookieAutoConfirmRequest 是更新自动确认发货开关的 HTTP 请求 DTO。
type cookieAutoConfirmRequest struct {
	// AutoConfirm 表示是否允许系统自动确认发货。
	AutoConfirm bool `json:"auto_confirm"`
}

// cookieRemarkRequest 是更新账号备注的 HTTP 请求 DTO。
type cookieRemarkRequest struct {
	// Remark 是用户维护的非敏感账号显示备注。
	Remark string `json:"remark"`
}

// cookiePauseDurationRequest 是更新账号自动化暂停时长的 HTTP 请求 DTO。
type cookiePauseDurationRequest struct {
	// PauseDuration 是暂停自动化操作的分钟数。
	PauseDuration int `json:"pause_duration"`
}

// updateCookieSettings 原子保存编辑弹窗中的账号字段和通知绑定。
func (s *Server) updateCookieSettings(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cid")
	// detail、ok 用于本次流程后续判断的detail、ok
	ownedDetail, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	// req 用于本次流程后续判断的req
	var req updateCookieSettingsRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
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

	// password 保存待写入的登录密码；nil 表示保留已有密码，空字符串指令表示清除密码。
	var password *string
	if req.LoginPassword != nil && *req.LoginPassword != "" {
		password = req.LoginPassword
	}
	if req.ClearPassword {
		// clearedPassword 是明确清除登录密码的空值指令，不读取既有秘密。
		clearedPassword := ""
		password = &clearedPassword
	}
	// settingsResult、err 保存应用服务返回的暂停截止时间、补偿错误和主写入错误。
	settingsResult, err := s.accountSettingsApplication().UpdateSettings(r.Context(), accountapp.SettingsUpdateInput{
		UserID: ownedDetail.UserID, AccountID: cid, Cookie: req.Cookie, Remark: req.Remark,
		AutoConfirm: req.AutoConfirm, PauseDuration: req.PauseDuration, Username: req.Username,
		Password: password, ShowBrowser: req.ShowBrowser, ChannelIDs: req.ChannelIDs,
	})
	if err != nil {
		switch {
		case errors.Is(err, accountapp.ErrForbidden):
			writeErr(w, http.StatusForbidden, "账号设置包含无权限使用的资源")
		case errors.Is(err, accountapp.ErrNotFound):
			writeErr(w, http.StatusNotFound, "账号不存在")
		default:
			writeErr(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	if settingsResult.TokenCleanupError != nil && s.Logger != nil {
		s.Logger.Warn("账号设置保存后清理旧连接凭证失败", "cookie_id", cid, "err", settingsResult.TokenCleanupError)
	}
	if settingsResult.RuntimeError != nil && s.Logger != nil {
		s.Logger.Error("账号设置保存后重启失败", "cookie_id", cid, "err", settingsResult.RuntimeError)
	}
	writeJSON(w, http.StatusOK, cookieSettingsResponse{
		Success: true, PausedUntil: settingsResult.PausedUntil, Paused: settingsResult.PausedUntil > time.Now().UTC().Unix(),
	})
}

// listCookieRuntimeStatus 返回本地账号引擎状态，不请求闲鱼 API，可安全用于前端轮询。
func (s *Server) listCookieRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context()) // sess 是当前认证会话。
	accountSummary := s.accountSummaryApplication()
	cookieIDs, err := accountSummary.ListOwnedIDs(r.Context(), sess.UserID) // cookieIDs 和 err 是账号 ID 列表及查询错误。
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "获取账号失败")
		return
	}
	// runtime、runtimeErr 保存应用层返回的账号运行状态快照及读取错误。
	runtime, runtimeErr := s.accountRuntimeApplication().RuntimeStatuses(r.Context())
	if runtimeErr != nil {
		writeErr(w, http.StatusInternalServerError, "获取账号运行状态失败")
		return
	}
	result := make(map[string]accountapp.RuntimeStatus, len(cookieIDs)) // result 是返回给前端的状态映射。
	for _, cid := range cookieIDs {                                     // cid 是当前账号 ID。
		enabled, statusErr := accountSummary.StatusOwned(r.Context(), sess.UserID, cid)
		if statusErr != nil || !enabled {
			result[cid] = accountapp.RuntimeStatus{State: "disabled", Message: "账号已停用", UpdatedAt: time.Now()}
			continue
		}
		if status, ok := runtime[cid]; ok { // status 和 ok 表示已记录的运行状态及存在性。
			result[cid] = status
			continue
		}
		result[cid] = accountapp.RuntimeStatus{State: "error", Message: "账号服务未运行", UpdatedAt: time.Now()}
	}
	writeJSON(w, http.StatusOK, result)
}

// listCookies 列出当前用户的 cookie_id。
func (s *Server) listCookies(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())                                     // sess 是当前认证会话。
	ids, err := s.accountSummaryApplication().ListOwnedIDs(r.Context(), sess.UserID) // ids 和 err 是账号 ID 列表及查询错误。
	if err != nil {
		writeErrRequest(w, r, http.StatusInternalServerError, "获取账号失败")
		return
	}
	writeJSON(w, http.StatusOK, ids)
}

// listCookieDetails 账号非敏感详情（不含 cookie 明文/密码，遵循 Fork 安全基线）。
func (s *Server) listCookieDetails(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())                                            // sess 是当前认证会话。
	summaries, err := s.accountSummaryApplication().ListSummaries(r.Context(), sess.UserID) // summaries 和 err 是账号摘要及查询错误。
	if err != nil {
		writeErrRequest(w, r, http.StatusInternalServerError, "获取账号失败")
		return
	}
	result := make([]cookieSummaryResponse, 0, len(summaries)) // result 是非敏感详情响应列表。
	for _, summary := range summaries {                        // summary 是当前账号的非敏感摘要。
		// tasks 保存通过账号任务应用 Port 读取的非敏感任务设置。
		tasks := automationapp.AccountTaskSettings{CookieID: summary.ID}
		// service 提供账号任务设置的应用层查询能力。
		if service := s.accountTaskApplication(); service != nil {
			// loadedTasks、taskErr 保存应用层任务设置及读取错误；旧接口读取失败时仍保留默认值。
			loadedTasks, taskErr := service.GetSettings(r.Context(), summary.ID)
			if taskErr == nil {
				tasks = loadedTasks
			}
		}
		// enabled、statusErr 保存当前账号启用状态及查询错误。
		enabled, statusErr := s.accountSummaryApplication().StatusOwned(r.Context(), sess.UserID, summary.ID)
		result = append(result, cookieSummaryResponse{
			ID:                summary.ID,
			HasCookie:         true,
			Enabled:           statusErr == nil && enabled,
			AutoConfirm:       summary.AutoConfirm,
			Remark:            summary.Remark,
			PauseDuration:     summary.PauseDuration,
			PausedUntil:       summary.PausedUntil,
			Paused:            summary.PausedUntil > time.Now().UTC().Unix(),
			ShowBrowser:       summary.ShowBrowser,
			Username:          summary.Username,
			Nickname:          cachedCookieSummaryNickname(summary),
			AvatarURL:         summary.AvatarURL,
			LoginMethod:       summary.LoginMethod,
			LastLoginAt:       summary.LastLoginAt,
			ProfileError:      "",
			AIEnabled:         false,
			AutoRateEnabled:   tasks.AutoRateEnabled,
			RateContent:       tasks.RateContent,
			AutoPolishEnabled: tasks.AutoPolishEnabled,
			PolishTime:        tasks.PolishTime,
			LastRateScanAt:    tasks.LastRateScanAt,
			LastPolishDate:    tasks.LastPolishDate,
			LastPolishAt:      tasks.LastPolishAt,
		})
	}
	writeJSON(w, http.StatusOK, result)
}

// getCookieDetails 单个账号非敏感详情。
func (s *Server) getCookieDetails(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")                                                                // cid 是请求路径中的账号 ID。
	sess := auth.SessionFromContext(r.Context())                                                 // sess 是当前认证会话。
	summary, err := s.accountSummaryApplication().GetOwnedSummary(r.Context(), sess.UserID, cid) // summary 和 err 是账号摘要及查询错误。
	if err != nil {
		writeErr(w, http.StatusForbidden, "无权限操作该Cookie")
		return
	}
	// tasks 保存通过账号任务应用 Port 读取的非敏感任务设置。
	tasks := automationapp.AccountTaskSettings{CookieID: cid}
	// service 提供账号任务设置的应用层查询能力。
	if service := s.accountTaskApplication(); service != nil {
		// loadedTasks、taskErr 保存应用层任务设置及读取错误；旧接口读取失败时仍保留默认值。
		loadedTasks, taskErr := service.GetSettings(r.Context(), cid)
		if taskErr == nil {
			tasks = loadedTasks
		}
	}
	// enabled、statusErr 保存当前账号启用状态及查询错误。
	enabled, statusErr := s.accountSummaryApplication().StatusOwned(r.Context(), sess.UserID, cid)
	writeJSON(w, http.StatusOK, cookieDetailResponse{
		ID: summary.ID, Enabled: statusErr == nil && enabled, AutoConfirm: summary.AutoConfirm,
		Remark: summary.Remark, PauseDuration: summary.PauseDuration, PausedUntil: summary.PausedUntil,
		Paused: summary.PausedUntil > time.Now().UTC().Unix(), ShowBrowser: summary.ShowBrowser,
		Username: summary.Username, Nickname: cachedCookieSummaryNickname(summary), AvatarURL: summary.AvatarURL,
		LoginMethod: summary.LoginMethod, LastLoginAt: summary.LastLoginAt, ProfileError: "", HasCookie: true,
		AutoRateEnabled: tasks.AutoRateEnabled, RateContent: tasks.RateContent,
		AutoPolishEnabled: tasks.AutoPolishEnabled, PolishTime: tasks.PolishTime,
		LastRateScanAt: tasks.LastRateScanAt, LastPolishDate: tasks.LastPolishDate, LastPolishAt: tasks.LastPolishAt,
	})
}

// 账号详情 DTO 迁移保留用户可见字段，不返回 Cookie 明文或登录密码。
// 账号资料刷新 DTO 仅暴露昵称、头像和可展示错误。
// 账号设置 DTO 继续保留 paused_until 与 paused 两个旧字段。
// 自动确认和暂停时长查询分别使用独立具名 DTO。
// 简单变更统一复用 operationResponse，字段名称保持 success。
// 这些 DTO 不改变账号所有权校验和凭证锁边界。
// 前端可在版本化路径迁移时直接复用相同字段。
// 旧路径仍由当前 handler 提供，避免复制业务逻辑。
// 本次切片只调整成功响应的静态类型。
// 列表、详情和资料刷新保持原有 HTTP 状态码。
// 响应结构迁移不影响后台运行实例重启行为。
// 后续兼容清理需先完成客户端调用方迁移。
// 该说明与 API 版本化迁移文档保持一致。

// refreshCookieProfile 主动刷新账号昵称/头像。列表接口不自动刷新，避免 100 个账号时对闲鱼打 100 次请求。
func (s *Server) refreshCookieProfile(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")                // cid 是请求路径中的账号 ID。
	sess := auth.SessionFromContext(r.Context()) // sess 是当前认证会话。
	// profile、err 用于本次流程后续判断的profile、err
	profile, err := s.accountProfileApplication().RefreshProfile(r.Context(), sess.UserID, cid)
	if err != nil {
		if errors.Is(err, accountapp.ErrForbidden) {
			writeErr(w, http.StatusForbidden, "无权限操作该账号")
			return
		}
		if errors.Is(err, accountapp.ErrNotFound) {
			// 旧路径将不存在与无权账号统一视为不可操作，保留既有客户端状态码兼容性。
			writeErr(w, http.StatusForbidden, "无权限操作该账号")
			return
		}
		writeErr(w, http.StatusInternalServerError, "刷新账号资料失败")
		return
	}
	writeJSON(w, http.StatusOK, cookieProfileResponse{
		Success: profile.ErrorMessage == "", ID: cid, Nickname: profile.Nickname, AvatarURL: profile.AvatarURL, ProfileError: profile.ErrorMessage,
	})
}

// 账号新增和资料刷新仍使用旧路径薄适配，业务逻辑不复制。
// 账号凭证写入始终在凭证锁内完成。
// 资料刷新成功响应已转换为 cookieProfileResponse。
// 兼容字段的删除必须等前端发布版本完成迁移。
/*
账号摘要迁移说明：列表和详情接口只依赖非敏感字段。
凭证字段由独立的单值查询按需读取。
账号状态仍由运行状态查询单独提供。
任务配置仍按账号 ID 查询，保持原有响应结构。
摘要查询不触发 Cookie、密码或 metadata 解密。
列表顺序由 repository 统一定义，避免 map 遍历的不确定性。
详情接口使用用户与账号 ID 联合过滤。
跨用户账号不会暴露摘要字段。
刷新资料流程仍在通过所有权校验后读取完整凭证。
本次切片不改变 HTTP 字段名称和错误响应格式。
后续凭证流程继续迁移到按用户过滤的单值接口。
*/

// addCookie 添加账号 cookie。
func (s *Server) addCookie(w http.ResponseWriter, r *http.Request) {
	// req 用于本次流程后续判断的req
	var req createCookieRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil || req.ID == "" || req.Value == "" {
		writeErr(w, http.StatusBadRequest, "缺少 id 或 value")
		return
	}
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	if // err 用于本次流程后续判断的err
	err := s.accountLoginApplication().CreateCookie(r.Context(), req.ID, req.Value, sess.UserID, req.LoginMethod); err != nil {
		if errors.Is(err, accountapp.ErrForbidden) {
			writeErr(w, http.StatusForbidden, "该账号ID已存在且不属于当前用户")
			return
		}
		if errors.Is(err, accountapp.ErrAlreadyExists) {
			writeErr(w, http.StatusConflict, "该账号ID已存在，请使用更新账号功能")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, accountMutationResponse{Success: true, ID: req.ID})
}

// updateCookieRequest 是更新账号 Cookie 的 HTTP 请求 DTO；last_refresh_at 用于兼容客户端的并发覆盖检测。
type updateCookieRequest struct {
	// Value 是本次请求携带的明文 Cookie；只在 Server 请求作用域内传递。
	Value string `json:"value"`
	// LoginMethod 是可选登录方式，用于成功审计和账号启用语义。
	LoginMethod string `json:"login_method"`
	// LastRefreshAt 是客户端读取到的最近凭证刷新时间；零值表示旧客户端不启用冲突检查。
	LastRefreshAt int64 `json:"last_refresh_at"`
}

// updateCookie 更新 cookie 值。
func (s *Server) updateCookie(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cid")
	// ok 用于本次流程后续判断的ok
	_, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	// req 保存请求中的 Cookie、登录方式和可选凭证版本。
	var req updateCookieRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	if // err 用于本次流程后续判断的err
	err := s.accountLoginApplication().UpdateCookie(r.Context(), cid, req.Value, sess.UserID, req.LoginMethod, req.LastRefreshAt); err != nil {
		if errors.Is(err, accountapp.ErrCredentialConflict) {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, accountapp.ErrForbidden) {
			writeErr(w, http.StatusForbidden, "无权限操作该账号")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// updateCookieLoginInfo 更新账号登录信息（用户名/密码/显示浏览器）。
func (s *Server) updateCookieLoginInfo(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cid")
	// detail、ok 保存非敏感账号摘要和归属校验结果。
	detail, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	// req 用于本次流程后续判断的req
	var req cookieLoginInfoRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// password 保存待写入的登录密码；nil 表示兼容地保留既有密码。
	passwordValue := req.Password
	if passwordValue == "" {
		passwordValue = req.LoginPassword
	}
	// password 保存可选登录密码指针；nil 表示保留数据库中的既有秘密。
	var password *string
	if passwordValue != "" || req.ClearPassword {
		password = &passwordValue
	}
	// err 保存应用层登录信息写入错误。
	if err := s.accountSettingsApplication().UpdateLoginInfo(r.Context(), accountapp.LoginInfoUpdateInput{
		UserID: detail.UserID, AccountID: cid, Username: req.Username, Password: password, ShowBrowser: req.ShowBrowser,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// setCookieStatus 启用/禁用账号。
func (s *Server) setCookieStatus(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cid")
	// ownedDetail、ok 用于本次流程后续判断的ownedDetail、ok
	ownedDetail, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	// req 用于本次流程后续判断的req
	var req cookieStatusRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// result、err 保存应用层启停结果和持久化错误。
	result, err := s.accountSettingsApplication().SetStatus(r.Context(), ownedDetail.UserID, cid, req.Enabled)
	if err != nil {
		if errors.Is(err, accountapp.ErrForbidden) {
			writeErr(w, http.StatusForbidden, "无权限操作该账号")
			return
		}
		if errors.Is(err, accountapp.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "账号不存在")
			return
		}
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	if result.RuntimeError != nil && s.Logger != nil {
		s.Logger.Error("启停账号运行实例失败", "cookie_id", cid, "err", result.RuntimeError)
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteCookie 删除账号。
func (s *Server) deleteCookie(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cid")
	// ownedDetail、ok 用于本次流程后续判断的ownedDetail、ok
	ownedDetail, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	// sess 保存当前认证用户；删除服务会在持久化边界再次确认账号归属。
	sess := authSess(r)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// deleteService 负责归属复核、停止 fencing、Context 限制和最终删除。
	deleteService := s.accountDeleteApplication()
	if deleteService == nil {
		writeErr(w, http.StatusInternalServerError, "账号删除服务未启用")
		return
	}
	// deleteErr 保存账号删除应用用例的统一错误结果。
	deleteErr := deleteService.Delete(r.Context(), sess.UserID, cid)
	if errors.Is(deleteErr, accountapp.ErrDeleteConflict) {
		writeErr(w, http.StatusConflict, "账号运行时尚未停止，请稍后重试")
		return
	}
	if errors.Is(deleteErr, accountapp.ErrForbidden) {
		writeErr(w, http.StatusForbidden, "无权限操作该账号")
		return
	}
	if errors.Is(deleteErr, accountapp.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "账号不存在")
		return
	}
	if deleteErr != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	s.Logger.Info("账号已删除",
		"cookie_id", cid,
		"nickname", cachedAccountSummaryNickname(ownedDetail),
		"user_id", ownedDetail.UserID,
	)
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// setCookieAutoConfirm 设置自动确认发货。
func (s *Server) setCookieAutoConfirm(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// req 用于本次流程后续判断的req
	var req cookieAutoConfirmRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// sess 是当前请求的认证会话，用于让应用服务再次确认账号归属。
	sess := authSess(r)
	if // err 保存应用层自动确认设置错误。
	_, err := s.accountSettingsApplication().SetAutoConfirm(r.Context(), sess.UserID, cid, req.AutoConfirm); err != nil {
		if errors.Is(err, accountapp.ErrForbidden) || errors.Is(err, accountapp.ErrNotFound) {
			writeErr(w, http.StatusForbidden, "无权操作该账号")
			return
		}
		writeErr(w, http.StatusInternalServerError, "保存自动确认设置失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// getCookieAutoConfirm 获取自动确认发货设置。
func (s *Server) getCookieAutoConfirm(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cid")
	// d、ok 用于本次流程后续判断的d、ok
	d, ok := s.requireCookieOwner(w, r, cid)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, autoConfirmResponse{AutoConfirm: d.AutoConfirm})
}

// setCookieRemark 设置备注。
func (s *Server) setCookieRemark(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// req 用于本次流程后续判断的req
	var req cookieRemarkRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// sess 是当前请求的认证会话，用于让应用服务再次确认账号归属。
	sess := authSess(r)
	if // err 保存应用层账号备注错误。
	_, err := s.accountSettingsApplication().SetRemark(r.Context(), sess.UserID, cid, req.Remark); err != nil {
		if errors.Is(err, accountapp.ErrForbidden) || errors.Is(err, accountapp.ErrNotFound) {
			writeErr(w, http.StatusForbidden, "无权操作该账号")
			return
		}
		writeErr(w, http.StatusInternalServerError, "保存账号备注失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// setCookiePauseDuration 设置暂停时长。
func (s *Server) setCookiePauseDuration(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// req 用于本次流程后续判断的req
	var req cookiePauseDurationRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.PauseDuration < 0 || req.PauseDuration > 1440 {
		writeErr(w, http.StatusBadRequest, "暂停时长必须在 0 到 1440 分钟之间")
		return
	}
	// sess 是当前请求的认证会话，用于让应用服务再次确认账号归属。
	sess := authSess(r)
	// result、err 保存应用层暂停设置结果和错误。
	result, err := s.accountSettingsApplication().SetPause(r.Context(), sess.UserID, cid, req.PauseDuration)
	if err != nil {
		if errors.Is(err, accountapp.ErrForbidden) || errors.Is(err, accountapp.ErrNotFound) {
			writeErr(w, http.StatusForbidden, "无权操作该账号")
			return
		}
		writeErr(w, http.StatusInternalServerError, "保存暂停时长失败")
		return
	}
	writeJSON(w, http.StatusOK, cookieSettingsResponse{
		Success: true, PausedUntil: result.PausedUntil, Paused: result.PausedUntil > time.Now().UTC().Unix(),
	})
}

// getCookiePauseDuration 获取暂停时长。
func (s *Server) getCookiePauseDuration(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// sess 是当前请求的认证会话，用于让应用服务再次确认账号归属。
	sess := authSess(r)
	// pauseState 保存应用层返回的非敏感暂停状态。
	pauseState, _ := s.accountSettingsApplication().GetPause(r.Context(), sess.UserID, cid)
	writeJSON(w, http.StatusOK, pauseDurationResponse{
		PauseDuration: pauseState.Duration, PausedUntil: pauseState.PausedUntil, Paused: pauseState.Paused,
	})
}

// cachedAccountSummaryNickname 从非敏感应用摘要生成删除日志展示名，不构造或读取数据库凭证模型。
func cachedAccountSummaryNickname(summary accountapp.AccountSummary) string {
	if strings.TrimSpace(summary.Remark) != "" {
		return strings.TrimSpace(summary.Remark)
	}
	if strings.TrimSpace(summary.Nickname) != "" {
		return strings.TrimSpace(summary.Nickname)
	}
	return "账号 " + truncate(summary.ID, 6)
}

// normalizeProfileAvatarURL 封装normalizeProfileAvatarURL业务协调。
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

// truncate 封装truncate业务协调。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// cachedCookieSummaryNickname 根据账号摘要生成展示名称，不依赖敏感凭证字段。
func cachedCookieSummaryNickname(summary accountapp.AccountSummary) string {
	if strings.TrimSpace(summary.Remark) != "" {
		return strings.TrimSpace(summary.Remark)
	}
	if strings.TrimSpace(summary.Nickname) != "" {
		return strings.TrimSpace(summary.Nickname)
	}
	return "账号 " + truncate(summary.ID, 6)
}
