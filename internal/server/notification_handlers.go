package server

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	notificationsapp "xianyu-go/internal/application/notifications"
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

// notificationChannelsApplication 返回当前 Server 绑定的通知渠道应用服务。
func (s *Server) notificationChannelsApplication() NotificationChannelsPort {
	return s.applicationServiceSet().notificationChannels
}

// uncertainNotificationsApplication 返回当前 Server 绑定的通知不确定状态应用服务。
func (s *Server) uncertainNotificationsApplication() UncertainNotificationsPort {
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

// listChannels 封装list渠道列表业务协调。
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

// createChannel 封装create渠道业务协调。
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

// updateChannel 封装update渠道业务协调。
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
		if errors.Is(err, notificationsapp.ErrChannelForbidden) || errors.Is(err, notificationsapp.ErrChannelNotFound) {
			writeErr(w, http.StatusForbidden, "无权操作该通知渠道")
			return
		}
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteChannel 封装delete渠道业务协调。
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
		if errors.Is(err, notificationsapp.ErrChannelForbidden) || errors.Is(err, notificationsapp.ErrChannelNotFound) {
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

// getAccountBindings 封装get账号Bindings业务协调。
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

// listMessageNotifications 封装list消息通知列表业务协调。
func (s *Server) listMessageNotifications(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证用户，用于应用层归属隔离。
	sess := authSess(r)
	// rows、err 保存通知绑定摘要及查询错误。
	rows, err := s.notificationChannelsApplication().ListBindings(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// out 用于本次流程后续判断的out
	out := make(notificationBindingListResponse, len(rows))
	// cookieID、bindings 表示当前遍历过程中的登录凭证ID、bindings
	// binding 表示当前遍历到的通知绑定摘要。
	for _, binding := range rows {
		out[binding.CookieID] = append(out[binding.CookieID], notificationBindingResponse{ID: binding.ID, ChannelID: binding.ChannelID, ChannelName: binding.ChannelName, Enabled: binding.Enabled})
	}
	writeJSON(w, http.StatusOK, out)
}

// setAccountBindings 封装set账号Bindings业务协调。
func (s *Server) setAccountBindings(w http.ResponseWriter, r *http.Request) {
	// cid 保存路径中的账号标识。
	cid := chi.URLParam(r, "cid")
	// req 保存具名账号通知绑定请求。
	var req notificationBindingRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.ChannelID != 0 {
		// enabled 用于本次流程后续判断的启用状态
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

// deleteMessageNotification 封装delete消息通知业务协调。
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

// deleteAccountNotifications 封装delete账号通知列表业务协调。
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
