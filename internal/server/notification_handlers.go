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

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	sess := authSess(r)
	chs, err := s.communicationApplication().ListNotificationChannels(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, newNotificationChannelResponses(chs))
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	sess := authSess(r)
	var req struct {
		Name       string `json:"name"`
		Type       string `json:"type"`
		Config     string `json:"config"`
		EventTypes string `json:"event_types"`
		Enabled    bool   `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Name == "" || req.Type == "" {
		writeErr(w, http.StatusBadRequest, "name 和 type 必填")
		return
	}
	id, err := s.communicationApplication().CreateNotificationChannel(r.Context(), db.NotificationChannelRow{
		Name: req.Name, Type: req.Type, Config: req.Config, EventTypes: req.EventTypes, Enabled: req.Enabled, UserID: sess.UserID,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建失败")
		return
	}
	writeJSON(w, http.StatusOK, mutationIDResponse{Success: true, ID: id})
}

func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	var req struct {
		Name       *string `json:"name"`
		Type       *string `json:"type"`
		Config     *string `json:"config"`
		EventTypes *string `json:"event_types"`
		Enabled    *bool   `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	sess := authSess(r)
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
	if err := s.communicationApplication().UpdateNotificationChannel(r.Context(), *row, sess.UserID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusForbidden, "无权操作该通知渠道")
			return
		}
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	sess := authSess(r)
	if err := s.communicationApplication().DeleteNotificationChannel(r.Context(), id, sess.UserID); err != nil {
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
	id, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	if !s.requireChannelOwner(w, r, id) {
		return
	}
	body := "🧪 通知渠道测试\n\n这是一条来自Ydisks闲鱼助手的测试通知，收到说明渠道配置正常。\n时间: " +
		time.Now().Format("2006-01-02 15:04:05")
	if err := s.communicationApplication().TestNotificationChannel(r.Context(), id, authSess(r).UserID, body); err != nil {
		if errors.Is(err, errCommunicationUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "通知器未启用")
			return
		}
		writeErr(w, http.StatusInternalServerError, "发送失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

func (s *Server) getAccountBindings(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	ids, err := s.communicationApplication().GetNotificationBindingIDs(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, accountBindingsResponse{CookieID: cid, ChannelIDs: ids})
}

func (s *Server) listMessageNotifications(w http.ResponseWriter, r *http.Request) {
	sess := authSess(r)
	rows, err := s.communicationApplication().ListNotificationBindings(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	out := make(notificationBindingListResponse, len(rows))
	for cookieID, bindings := range rows {
		for _, binding := range bindings {
			out[cookieID] = append(out[cookieID], notificationBindingResponse{ID: binding.ID, ChannelID: binding.ChannelID, ChannelName: binding.ChannelName, Enabled: binding.Enabled})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) setAccountBindings(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	var req struct {
		ChannelIDs []int64 `json:"channel_ids"`
		ChannelID  int64   `json:"channel_id"`
		Enabled    *bool   `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.ChannelID != 0 {
		if !s.requireChannelOwner(w, r, req.ChannelID) {
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		err := s.communicationApplication().SetSingleNotificationBinding(r.Context(), cid, req.ChannelID, enabled)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "保存失败")
			return
		}
		writeJSON(w, http.StatusOK, operationResponse{Success: true})
		return
	}
	for _, channelID := range req.ChannelIDs {
		if !s.requireChannelOwner(w, r, channelID) {
			return
		}
	}
	if err := s.communicationApplication().SetNotificationBindings(r.Context(), cid, req.ChannelIDs); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

func (s *Server) deleteMessageNotification(w http.ResponseWriter, r *http.Request) {
	sess := authSess(r)
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

func (s *Server) deleteAccountNotifications(w http.ResponseWriter, r *http.Request) {
	sess := authSess(r)
	cid := chi.URLParam(r, "cid")
	err := s.communicationApplication().DeleteAccountNotificationBindings(r.Context(), sess.UserID, cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}
