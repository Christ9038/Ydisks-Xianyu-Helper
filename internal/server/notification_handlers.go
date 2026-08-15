package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	notificationsapp "xianyu-go/internal/application/notifications"
	"xianyu-go/internal/db"
)

// notificationChannelCreateRequest 是创建通知渠道的具名 HTTP 请求 DTO；Config 只写入应用端口。
type notificationChannelCreateRequest struct {
	// Name 是渠道名称。
	Name string `json:"name"`
	// Type 是渠道协议类型。
	Type string `json:"type"`
	// Config 是渠道敏感配置 JSON，禁止进入响应或日志。
	Config string `json:"config"`
	// EventTypes 是渠道订阅事件类型编码。
	EventTypes string `json:"event_types"`
	// Enabled 表示渠道是否启用。
	Enabled bool `json:"enabled"`
}

// notificationChannelPatchRequest 是更新通知渠道的具名部分更新 DTO。
type notificationChannelPatchRequest struct {
	// Name 是可选的新渠道名称。
	Name *string `json:"name"`
	// Type 是可选的新渠道协议类型。
	Type *string `json:"type"`
	// Config 是可选的新敏感配置 JSON，禁止进入响应或日志。
	Config *string `json:"config"`
	// EventTypes 是可选的新订阅事件类型编码。
	EventTypes *string `json:"event_types"`
	// Enabled 是可选的新启用状态。
	Enabled *bool `json:"enabled"`
}

// notificationBindingRequest 是账号通知绑定更新的具名 HTTP 请求 DTO。
type notificationBindingRequest struct {
	// ChannelIDs 是覆盖式保存的渠道 ID 列表。
	ChannelIDs []int64 `json:"channel_ids"`
	// ChannelID 是单条绑定更新的渠道 ID。
	ChannelID int64 `json:"channel_id"`
	// Enabled 是单条绑定是否启用；省略时默认为启用。
	Enabled *bool `json:"enabled"`
}

// storeNotificationChannelRepository 将通知数据库能力适配为通知应用端口。
type storeNotificationChannelRepository struct {
	// store 保存数据库聚合入口，仅在 Server 基础设施适配器内使用。
	store *db.Store
}

// ListChannels 查询渠道摘要并丢弃配置，避免敏感配置进入应用展示模型。
func (r storeNotificationChannelRepository) ListChannels(ctx context.Context, userID int64) ([]notificationsapp.ChannelSummary, error) {
	// rows、err 保存数据库渠道非敏感摘要；列表路径不会读取或解密 Config。
	rows, err := r.store.Notifications.ListChannelSummariesForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	// summaries 保存不含渠道配置的应用摘要。
	summaries := make([]notificationsapp.ChannelSummary, 0, len(rows))
	// row 表示当前待转换的数据库渠道行。
	for _, row := range rows {
		summaries = append(summaries, notificationsapp.ChannelSummary{ID: row.ID, Name: row.Name, Type: row.Type, EventTypes: row.EventTypes, Enabled: row.Enabled, UserID: row.UserID})
	}
	return summaries, nil
}

// GetChannelForUpdate 查询归属渠道的完整记录，仅供应用层合并部分更新。
func (r storeNotificationChannelRepository) GetChannelForUpdate(ctx context.Context, channelID, userID int64) (*notificationsapp.ChannelRecord, error) {
	// row、err 保存带用户归属的数据库渠道记录。
	row, err := r.store.Notifications.GetChannelRowForUser(ctx, channelID, userID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return &notificationsapp.ChannelRecord{ID: row.ID, Name: row.Name, Type: row.Type, Config: row.Config, EventTypes: row.EventTypes, Enabled: row.Enabled, UserID: row.UserID}, nil
}

// CreateChannel 将应用输入转换为数据库行并创建渠道。
func (r storeNotificationChannelRepository) CreateChannel(ctx context.Context, userID int64, input notificationsapp.ChannelInput) (int64, error) {
	// row 保存待加密写入数据库的渠道行；Config 不向 HTTP 返回。
	row := db.NotificationChannelRow{Name: input.Name, Type: input.Type, Config: input.Config, EventTypes: input.EventTypes, Enabled: input.Enabled, UserID: userID}
	return r.store.Notifications.CreateChannel(ctx, &row)
}

// UpdateChannel 将应用层完整渠道记录转换为数据库更新。
func (r storeNotificationChannelRepository) UpdateChannel(ctx context.Context, userID int64, record notificationsapp.ChannelRecord) error {
	// row 保存待加密写入数据库的渠道行；调用方已完成归属和字段校验。
	row := db.NotificationChannelRow{ID: record.ID, Name: record.Name, Type: record.Type, Config: record.Config, EventTypes: record.EventTypes, Enabled: record.Enabled, UserID: userID}
	return r.store.Notifications.UpdateChannelForUser(ctx, &row, userID)
}

// DeleteChannel 删除用户拥有的渠道并统一转换不存在错误。
func (r storeNotificationChannelRepository) DeleteChannel(ctx context.Context, channelID, userID int64) error {
	return normalizeNotificationRepositoryError(r.store.Notifications.DeleteChannelForUser(ctx, channelID, userID))
}

// OwnsChannel 查询通知渠道归属，不读取配置内容。
func (r storeNotificationChannelRepository) OwnsChannel(ctx context.Context, channelID, userID int64) (bool, error) {
	return r.store.Notifications.OwnsChannel(ctx, channelID, userID)
}

// OwnsAccount 查询账号归属，不读取或解密 Cookie。
func (r storeNotificationChannelRepository) OwnsAccount(ctx context.Context, userID int64, cookieID string) (bool, error) {
	return r.store.Cookies.ExistsOwned(ctx, userID, cookieID)
}

// ListBindings 查询绑定摘要并转换为应用模型。
func (r storeNotificationChannelRepository) ListBindings(ctx context.Context, userID int64) ([]notificationsapp.BindingSummary, error) {
	// rows、err 保存数据库绑定摘要及错误。
	rows, err := r.store.Notifications.ListBindingsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	// bindings 保存不含配置的应用绑定摘要。
	bindings := make([]notificationsapp.BindingSummary, 0, len(rows))
	// row 表示当前待转换的数据库绑定行。
	for _, row := range rows {
		bindings = append(bindings, notificationsapp.BindingSummary{ID: row.ID, CookieID: row.CookieID, ChannelID: row.ChannelID, ChannelName: row.ChannelName, Enabled: row.Enabled})
	}
	return bindings, nil
}

// GetBindingIDs 查询账号启用的通知渠道 ID。
func (r storeNotificationChannelRepository) GetBindingIDs(ctx context.Context, cookieID string) ([]int64, error) {
	return r.store.Notifications.AccountBindings(ctx, cookieID)
}

// SetBindings 覆盖保存账号绑定，并保留数据库事务和跨归属校验。
func (r storeNotificationChannelRepository) SetBindings(ctx context.Context, cookieID string, channelIDs []int64) error {
	return normalizeNotificationRepositoryError(r.store.Notifications.SetBindings(ctx, cookieID, channelIDs))
}

// SetSingleBinding 更新单个账号绑定状态。
func (r storeNotificationChannelRepository) SetSingleBinding(ctx context.Context, cookieID string, channelID int64, enabled bool) error {
	return r.store.Notifications.SetSingleBinding(ctx, cookieID, channelID, enabled)
}

// DeleteBinding 删除用户的一条绑定。
func (r storeNotificationChannelRepository) DeleteBinding(ctx context.Context, userID, bindingID int64) error {
	return r.store.Notifications.DeleteBinding(ctx, userID, bindingID)
}

// DeleteAccountBindings 删除用户账号的全部绑定。
func (r storeNotificationChannelRepository) DeleteAccountBindings(ctx context.Context, userID int64, cookieID string) error {
	return r.store.Notifications.DeleteAccountBindings(ctx, userID, cookieID)
}

// normalizeNotificationRepositoryError 将数据库归属错误转换为应用错误，隐藏基础设施类型。
func normalizeNotificationRepositoryError(err error) error {
	if errors.Is(err, db.ErrNotFound) {
		return notificationsapp.ErrChannelNotFound
	}
	if errors.Is(err, db.ErrForbidden) {
		return notificationsapp.ErrChannelForbidden
	}
	return err
}

// newStoreNotificationChannelRepository 构造通知渠道应用端口适配器。
func newStoreNotificationChannelRepository(store *db.Store) notificationsapp.ChannelRepository {
	if store == nil || store.Notifications == nil || store.Cookies == nil {
		return nil
	}
	return storeNotificationChannelRepository{store: store}
}

// notificationChannelsApplication 返回当前 Server 绑定的通知渠道应用服务。
func (s *Server) notificationChannelsApplication() *notificationsapp.ChannelService {
	return s.applicationServiceSet().notificationChannels
}

// ensureStoreNotificationChannelRepository 保证通知渠道应用端口覆盖全部能力。
var _ notificationsapp.ChannelRepository = storeNotificationChannelRepository{}

// storeNotificationUncertainRepository 将数据库通知摘要查询适配为应用层端口。
type storeNotificationUncertainRepository struct {
	// store 保存数据库聚合入口，仅在脱敏摘要适配器内使用。
	store *db.Store
}

// ListUncertainForUser 查询指定用户的不确定通知摘要，并转换为应用模型。
func (r storeNotificationUncertainRepository) ListUncertainForUser(ctx context.Context, userID int64, limit int) ([]notificationsapp.UncertainSummary, error) {
	// rows、err 保存数据库摘要查询结果及错误。
	rows, err := r.store.Notifications.ListUncertainOutboxForUser(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	return newNotificationUncertainApplicationSummaries(rows), nil
}

// CountUncertainForUser 统计指定用户的不确定通知数量。
func (r storeNotificationUncertainRepository) CountUncertainForUser(ctx context.Context, userID int64) (int, error) {
	return r.store.Notifications.CountUncertainOutboxForUser(ctx, userID)
}

// ListUncertainForAdmin 查询全局不确定通知摘要，并转换为应用模型。
func (r storeNotificationUncertainRepository) ListUncertainForAdmin(ctx context.Context, limit int) ([]notificationsapp.UncertainSummary, error) {
	// rows、err 保存数据库全局摘要查询结果及错误。
	rows, err := r.store.Notifications.ListUncertainOutboxForAdmin(ctx, limit)
	if err != nil {
		return nil, err
	}
	return newNotificationUncertainApplicationSummaries(rows), nil
}

// CountUncertainForAdmin 统计全局不确定通知数量。
func (r storeNotificationUncertainRepository) CountUncertainForAdmin(ctx context.Context) (int, error) {
	return r.store.Notifications.CountUncertainOutboxForAdmin(ctx)
}

// newStoreNotificationUncertainRepository 创建通知不确定状态应用服务使用的数据库适配器。
func newStoreNotificationUncertainRepository(store *db.Store) notificationsapp.Repository {
	if store == nil || store.Notifications == nil {
		return nil
	}
	return storeNotificationUncertainRepository{store: store}
}

// newNotificationUncertainApplicationSummaries 将数据库摘要转换为不含正文的应用模型。
func newNotificationUncertainApplicationSummaries(rows []db.NotificationUncertainSummary) []notificationsapp.UncertainSummary {
	// summaries 保存脱离数据库模型的非敏感通知摘要。
	summaries := make([]notificationsapp.UncertainSummary, 0, len(rows))
	// row 表示当前待转换的数据库通知摘要。
	for _, row := range rows {
		summaries = append(summaries, notificationsapp.UncertainSummary{
			ID: row.ID, ChannelID: row.ChannelID, OwnerUserID: row.OwnerUserID,
			EventType: row.EventType, AttemptCount: row.AttemptCount,
			UncertainAt: row.UncertainAt, HasError: row.HasError,
		})
	}
	return summaries
}

// 确保数据库适配器覆盖通知不确定状态应用端口的全部能力。
var _ notificationsapp.Repository = storeNotificationUncertainRepository{}

// uncertainNotificationsApplication 返回当前 Server 绑定的通知不确定状态应用服务。
func (s *Server) uncertainNotificationsApplication() *notificationsapp.Service {
	return s.applicationServiceSet().uncertainNotifications
}

// mountNotificationsReal 通知渠道 + 账号绑定。
func (s *Server) mountNotificationsReal(r chi.Router) {
	r.Get("/notification-channels", s.listChannels)
	r.Post("/notification-channels", s.createChannel)
	r.Put("/notification-channels/{channel_id}", s.updateChannel)
	r.Delete("/notification-channels/{channel_id}", s.deleteChannel)
	r.Post("/notification-channels/{channel_id}/test", s.testChannel)
	r.Get("/message-notifications", s.listMessageNotifications)
	r.Delete("/message-notifications/account/{cid}", s.deleteAccountNotifications)
	r.Delete("/message-notifications/{notification_id}", s.deleteMessageNotification)
	r.Get("/message-notifications/{cid}", s.getAccountBindings)
	r.Post("/message-notifications/{cid}", s.setAccountBindings)
}

// listUncertainNotifications 返回当前用户渠道对应的不确定通知摘要，不暴露正文或凭证。
func (s *Server) listUncertainNotifications(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前已认证用户，用于限制通知渠道归属范围。
	sess := authSess(r)
	// limit 保存运维列表页请求的最大条数，超出范围时使用数据库默认上限。
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 20)
	// items 保存当前用户可见的不确定通知摘要。
	items, total, err := s.uncertainNotificationsApplication().ListForUser(r.Context(), sess.UserID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询通知状态失败")
		return
	}
	writeJSON(w, http.StatusOK, newNotificationUncertainOutboxResponse(items, total, false))
}

// listAdminUncertainNotifications 返回全局不确定通知摘要，仅管理员路由可访问。
func (s *Server) listAdminUncertainNotifications(w http.ResponseWriter, r *http.Request) {
	// limit 保存管理员运维查询的最大条数，超出范围时使用数据库默认上限。
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	// items 保存所有用户渠道的不确定通知摘要，但不包含正文和错误原文。
	items, total, err := s.uncertainNotificationsApplication().ListForAdmin(r.Context(), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询通知状态失败")
		return
	}
	writeJSON(w, http.StatusOK, newNotificationUncertainOutboxResponse(items, total, true))
}

// listChannels 负责list渠道列表相关处理。
func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证用户，用于应用层归属隔离。
	sess := authSess(r)
	// chs、err 保存通知渠道非敏感摘要及查询错误。
	chs, err := s.notificationChannelsApplication().ListChannels(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, newNotificationChannelResponses(chs))
}

// createChannel 负责create渠道相关处理。
func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证用户，用于应用层归属隔离。
	sess := authSess(r)
	// req 保存具名通知渠道创建请求；Config 只写入应用端口。
	var req notificationChannelCreateRequest
	if // err 保存 JSON 解码错误。
	err := decodeJSON(r, &req); err != nil || req.Name == "" || req.Type == "" {
		writeErr(w, http.StatusBadRequest, "name 和 type 必填")
		return
	}
	// id、err 保存渠道创建结果及应用错误。
	id, err := s.notificationChannelsApplication().CreateChannel(r.Context(), sess.UserID, notificationsapp.ChannelInput{
		Name: req.Name, Type: req.Type, Config: req.Config, EventTypes: req.EventTypes, Enabled: req.Enabled,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建失败")
		return
	}
	writeJSON(w, http.StatusOK, mutationIDResponse{Success: true, ID: id})
}

// updateChannel 负责update渠道相关处理。
func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request) {
	// id、err 保存路径中的渠道 ID 及解析错误。
	id, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// req 保存具名通知渠道部分更新请求；Config 不会进入响应。
	var req notificationChannelPatchRequest
	if // err 保存 JSON 解码错误。
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// sess 保存当前认证用户，用于应用层归属隔离。
	sess := authSess(r)
	// err 保存应用层部分更新结果。
	if err := s.notificationChannelsApplication().UpdateChannel(r.Context(), sess.UserID, id, notificationsapp.ChannelPatch{
		Name: req.Name, Type: req.Type, Config: req.Config, EventTypes: req.EventTypes, Enabled: req.Enabled,
	}); err != nil {
		if errors.Is(err, notificationsapp.ErrChannelInvalidInput) {
			writeErr(w, http.StatusBadRequest, "name 和 type 必填")
			return
		}
		if errors.Is(err, notificationsapp.ErrChannelForbidden) || errors.Is(err, notificationsapp.ErrChannelNotFound) || errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusForbidden, "无权操作该通知渠道")
			return
		}
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteChannel 负责delete渠道相关处理。
func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	// id、err 保存路径中的渠道 ID 及解析错误。
	id, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// sess 保存当前认证用户，用于应用层归属隔离。
	sess := authSess(r)
	// err 保存应用层删除结果。
	if err := s.notificationChannelsApplication().DeleteChannel(r.Context(), sess.UserID, id); err != nil {
		if errors.Is(err, notificationsapp.ErrChannelForbidden) || errors.Is(err, notificationsapp.ErrChannelNotFound) || errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusForbidden, "无权操作该通知渠道")
			return
		}
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// testChannel 向指定渠道发送一条测试通知，便于用户验证配置是否正确。
func (s *Server) testChannel(w http.ResponseWriter, r *http.Request) {
	// id、err 保存路径中的渠道 ID 及解析错误。
	id, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// notifier 表示当前 Server 是否装配了实际通知发送器；未装配时保持旧接口的 503 语义，避免先执行归属查询。
	if s.notifier == nil {
		writeErr(w, http.StatusServiceUnavailable, "通知器未启用")
		return
	}
	// err 保存应用层测试发送结果。
	if err := s.notificationChannelsApplication().TestChannel(r.Context(), authSess(r).UserID, id, time.Now()); err != nil {
		if errors.Is(err, notificationsapp.ErrNotifierUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "通知器未启用")
			return
		}
		if errors.Is(err, notificationsapp.ErrChannelForbidden) || errors.Is(err, notificationsapp.ErrChannelNotFound) {
			writeErr(w, http.StatusForbidden, "无权操作该通知渠道")
			return
		}
		writeErr(w, http.StatusInternalServerError, "发送失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// getAccountBindings 负责get账号Bindings相关处理。
func (s *Server) getAccountBindings(w http.ResponseWriter, r *http.Request) {
	// cid 保存路径中的账号标识。
	cid := chi.URLParam(r, "cid")
	// sess 保存当前认证用户，用于应用层账号归属校验。
	sess := authSess(r)
	// ids、err 保存账号启用渠道 ID 及查询错误。
	ids, err := s.notificationChannelsApplication().GetBindingIDs(r.Context(), sess.UserID, cid)
	if err != nil {
		if errors.Is(err, notificationsapp.ErrAccountForbidden) || errors.Is(err, notificationsapp.ErrChannelNotFound) {
			writeErr(w, http.StatusNotFound, "账号不存在")
			return
		}
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, accountBindingsResponse{CookieID: cid, ChannelIDs: ids})
}

// listMessageNotifications 负责list消息通知列表相关处理。
func (s *Server) listMessageNotifications(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证用户，用于应用层归属隔离。
	sess := authSess(r)
	// rows、err 保存通知绑定摘要及查询错误。
	rows, err := s.notificationChannelsApplication().ListBindings(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// out 保存out，供当前处理流程使用
	out := make(notificationBindingListResponse, len(rows))
	// cookieID、bindings 表示当前遍历过程中的登录凭证ID、bindings
	// binding 表示当前遍历到的通知绑定摘要。
	for _, binding := range rows {
		out[binding.CookieID] = append(out[binding.CookieID], notificationBindingResponse{ID: binding.ID, ChannelID: binding.ChannelID, ChannelName: binding.ChannelName, Enabled: binding.Enabled})
	}
	writeJSON(w, http.StatusOK, out)
}

// setAccountBindings 负责set账号Bindings相关处理。
func (s *Server) setAccountBindings(w http.ResponseWriter, r *http.Request) {
	// cid 保存路径中的账号标识。
	cid := chi.URLParam(r, "cid")
	// req 保存具名账号通知绑定请求。
	var req notificationBindingRequest
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.ChannelID != 0 {
		// enabled 保存启用状态，供当前处理流程使用
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		// err 保存单条绑定应用用例结果。
		err := s.notificationChannelsApplication().SetSingleBinding(r.Context(), authSess(r).UserID, cid, req.ChannelID, enabled)
		if err != nil {
			if errors.Is(err, notificationsapp.ErrAccountForbidden) {
				writeErr(w, http.StatusNotFound, "账号不存在")
				return
			}
			if errors.Is(err, notificationsapp.ErrChannelForbidden) || errors.Is(err, notificationsapp.ErrChannelNotFound) {
				writeErr(w, http.StatusForbidden, "无权操作该通知渠道")
				return
			}
			writeErr(w, http.StatusInternalServerError, "保存失败")
			return
		}
		writeJSON(w, http.StatusOK, operationResponse{Success: true})
		return
	}
	// err 保存批量绑定应用用例结果。
	if err := s.notificationChannelsApplication().SetBindings(r.Context(), authSess(r).UserID, cid, req.ChannelIDs); err != nil {
		if errors.Is(err, notificationsapp.ErrAccountForbidden) {
			writeErr(w, http.StatusNotFound, "账号不存在")
			return
		}
		if errors.Is(err, notificationsapp.ErrChannelForbidden) || errors.Is(err, notificationsapp.ErrChannelNotFound) {
			writeErr(w, http.StatusForbidden, "无权操作该通知渠道")
			return
		}
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteMessageNotification 负责delete消息通知相关处理。
func (s *Server) deleteMessageNotification(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证用户，用于应用层归属隔离。
	sess := authSess(r)
	// id、err 保存路径中的绑定 ID 及解析错误。
	id, err := strconv.ParseInt(chi.URLParam(r, "notification_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// err 保存应用层绑定删除结果。
	err = s.notificationChannelsApplication().DeleteBinding(r.Context(), sess.UserID, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteAccountNotifications 负责delete账号通知列表相关处理。
func (s *Server) deleteAccountNotifications(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证用户，用于应用层归属隔离。
	sess := authSess(r)
	// cid 保存路径中的账号标识。
	cid := chi.URLParam(r, "cid")
	// err 保存应用层账号绑定删除结果。
	err := s.notificationChannelsApplication().DeleteAccountBindings(r.Context(), sess.UserID, cid)
	if err != nil {
		if errors.Is(err, notificationsapp.ErrAccountForbidden) || errors.Is(err, notificationsapp.ErrChannelNotFound) {
			writeErr(w, http.StatusNotFound, "账号不存在")
			return
		}
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}
