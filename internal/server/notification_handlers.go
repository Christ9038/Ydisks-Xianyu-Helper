package server

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/db"
)

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
	items, err := s.Store.Notifications.ListUncertainOutboxForUser(r.Context(), sess.UserID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询通知状态失败")
		return
	}
	// total 保存当前用户渠道的不确定通知总数。
	total, err := s.Store.Notifications.CountUncertainOutboxForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "统计通知状态失败")
		return
	}
	writeJSON(w, http.StatusOK, newNotificationUncertainOutboxResponse(items, total, false))
}

// listAdminUncertainNotifications 返回全局不确定通知摘要，仅管理员路由可访问。
func (s *Server) listAdminUncertainNotifications(w http.ResponseWriter, r *http.Request) {
	// limit 保存管理员运维查询的最大条数，超出范围时使用数据库默认上限。
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	// items 保存所有用户渠道的不确定通知摘要，但不包含正文和错误原文。
	items, err := s.Store.Notifications.ListUncertainOutboxForAdmin(r.Context(), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询通知状态失败")
		return
	}
	// total 保存全局不确定通知总数。
	total, err := s.Store.Notifications.CountUncertainOutboxForAdmin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "统计通知状态失败")
		return
	}
	writeJSON(w, http.StatusOK, newNotificationUncertainOutboxResponse(items, total, true))
}

// listChannels 负责list渠道列表相关处理。
func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := authSess(r)
	// chs、err 保存chs、err，供当前处理流程使用
	chs, err := s.communicationApplication().ListNotificationChannels(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, newNotificationChannelResponses(chs))
}

// createChannel 负责create渠道相关处理。
func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := authSess(r)
	// req 保存req，供当前处理流程使用
	var req struct {
		Name       string `json:"name"`
		Type       string `json:"type"`
		Config     string `json:"config"`
		EventTypes string `json:"event_types"`
		Enabled    bool   `json:"enabled"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil || req.Name == "" || req.Type == "" {
		writeErr(w, http.StatusBadRequest, "name 和 type 必填")
		return
	}
	// id、err 保存id、err，供当前处理流程使用
	id, err := s.communicationApplication().CreateNotificationChannel(r.Context(), db.NotificationChannelRow{
		Name: req.Name, Type: req.Type, Config: req.Config, EventTypes: req.EventTypes, Enabled: req.Enabled, UserID: sess.UserID,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建失败")
		return
	}
	writeJSON(w, http.StatusOK, mutationIDResponse{Success: true, ID: id})
}

// updateChannel 负责update渠道相关处理。
func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request) {
	// id、err 保存id、err，供当前处理流程使用
	id, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// req 保存req，供当前处理流程使用
	var req struct {
		Name       *string `json:"name"`
		Type       *string `json:"type"`
		Config     *string `json:"config"`
		EventTypes *string `json:"event_types"`
		Enabled    *bool   `json:"enabled"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// sess 保存sess，供当前处理流程使用
	sess := authSess(r)
	// row、err 保存row、err，供当前处理流程使用
	row, err := s.communicationApplication().GetNotificationChannel(r.Context(), id, sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if row == nil {
		writeErr(w, http.StatusForbidden, "无权操作该通知渠道")
		return
	}
	if req.Name != nil {
		row.Name = *req.Name
	}
	if req.Type != nil {
		row.Type = *req.Type
	}
	if req.Config != nil {
		row.Config = *req.Config
	}
	if req.EventTypes != nil {
		row.EventTypes = *req.EventTypes
	}
	if req.Enabled != nil {
		row.Enabled = *req.Enabled
	}
	if row.Name == "" || row.Type == "" {
		writeErr(w, http.StatusBadRequest, "name 和 type 必填")
		return
	}
	if // err 保存err，供当前处理流程使用
	err := s.communicationApplication().UpdateNotificationChannel(r.Context(), *row, sess.UserID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
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
	// id、err 保存id、err，供当前处理流程使用
	id, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// sess 保存sess，供当前处理流程使用
	sess := authSess(r)
	if // err 保存err，供当前处理流程使用
	err := s.communicationApplication().DeleteNotificationChannel(r.Context(), id, sess.UserID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
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
	// id、err 保存id、err，供当前处理流程使用
	id, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	if s.notifier == nil {
		writeErr(w, http.StatusServiceUnavailable, "通知器未启用")
		return
	}
	if !s.requireChannelOwner(w, r, id) {
		return
	}
	// body 保存请求体，供当前处理流程使用
	body := "🧪 通知渠道测试\n\n这是一条来自Ydisks闲鱼助手的测试通知，收到说明渠道配置正常。\n时间: " +
		time.Now().Format("2006-01-02 15:04:05")
	if // err 保存err，供当前处理流程使用
	err := s.communicationApplication().TestNotificationChannel(r.Context(), id, authSess(r).UserID, body); err != nil {
		if errors.Is(err, errCommunicationUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "通知器未启用")
			return
		}
		writeErr(w, http.StatusInternalServerError, "发送失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// getAccountBindings 负责get账号Bindings相关处理。
func (s *Server) getAccountBindings(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// ids、err 保存ids、err，供当前处理流程使用
	ids, err := s.communicationApplication().GetNotificationBindingIDs(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, accountBindingsResponse{CookieID: cid, ChannelIDs: ids})
}

// listMessageNotifications 负责list消息通知列表相关处理。
func (s *Server) listMessageNotifications(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := authSess(r)
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := s.communicationApplication().ListNotificationBindings(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// out 保存out，供当前处理流程使用
	out := make(notificationBindingListResponse, len(rows))
	// cookieID、bindings 表示当前遍历过程中的登录凭证ID、bindings
	for cookieID, bindings := range rows {
		// binding 表示当前遍历过程中的binding
		for _, binding := range bindings {
			out[cookieID] = append(out[cookieID], notificationBindingResponse{ID: binding.ID, ChannelID: binding.ChannelID, ChannelName: binding.ChannelName, Enabled: binding.Enabled})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// setAccountBindings 负责set账号Bindings相关处理。
func (s *Server) setAccountBindings(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// req 保存req，供当前处理流程使用
	var req struct {
		ChannelIDs []int64 `json:"channel_ids"`
		ChannelID  int64   `json:"channel_id"`
		Enabled    *bool   `json:"enabled"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.ChannelID != 0 {
		if !s.requireChannelOwner(w, r, req.ChannelID) {
			return
		}
		// enabled 保存启用状态，供当前处理流程使用
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		// err 保存err，供当前处理流程使用
		err := s.communicationApplication().SetSingleNotificationBinding(r.Context(), cid, req.ChannelID, enabled)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "保存失败")
			return
		}
		writeJSON(w, http.StatusOK, operationResponse{Success: true})
		return
	}
	// channelID 表示当前遍历过程中的渠道ID
	for _, channelID := range req.ChannelIDs {
		if !s.requireChannelOwner(w, r, channelID) {
			return
		}
	}
	if // err 保存err，供当前处理流程使用
	err := s.communicationApplication().SetNotificationBindings(r.Context(), cid, req.ChannelIDs); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteMessageNotification 负责delete消息通知相关处理。
func (s *Server) deleteMessageNotification(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := authSess(r)
	// id、err 保存id、err，供当前处理流程使用
	id, err := strconv.ParseInt(chi.URLParam(r, "notification_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	err = s.communicationApplication().DeleteNotificationBinding(r.Context(), sess.UserID, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteAccountNotifications 负责delete账号通知列表相关处理。
func (s *Server) deleteAccountNotifications(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := authSess(r)
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cid")
	// err 保存err，供当前处理流程使用
	err := s.communicationApplication().DeleteAccountNotificationBindings(r.Context(), sess.UserID, cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}
