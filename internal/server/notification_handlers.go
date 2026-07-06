package server

import (
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
	chs, err := s.Store.Notifications.AllChannelsForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, chs)
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	sess := authSess(r)
	var req struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Config  string `json:"config"`
		Enabled bool   `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Name == "" || req.Type == "" {
		writeErr(w, http.StatusBadRequest, "name 和 type 必填")
		return
	}
	id, err := s.Store.Notifications.CreateChannel(r.Context(), &db.NotificationChannelRow{
		Name: req.Name, Type: req.Type, Config: req.Config, Enabled: req.Enabled, UserID: sess.UserID,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "id": id})
}

func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	var req struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Config  string `json:"config"`
		Enabled bool   `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := s.Store.Notifications.UpdateChannel(r.Context(), &db.NotificationChannelRow{
		ID: id, Name: req.Name, Type: req.Type, Config: req.Config, Enabled: req.Enabled,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	if err := s.Store.Notifications.DeleteChannel(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// testChannel 向指定渠道发送一条测试通知，便于用户验证配置是否正确。
func (s *Server) testChannel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	if s.Notifier == nil {
		writeErr(w, http.StatusServiceUnavailable, "通知器未启用")
		return
	}
	body := "🧪 通知渠道测试\n\n这是一条来自闲鱼助手的测试通知，收到说明渠道配置正常。\n时间: " +
		time.Now().Format("2006-01-02 15:04:05")
	if err := s.Notifier.SendToChannel(id, body); err != nil {
		writeErr(w, http.StatusInternalServerError, "发送失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) getAccountBindings(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	ids, err := s.Store.Notifications.AccountBindings(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cookie_id": cid, "channel_ids": ids})
}

func (s *Server) listMessageNotifications(w http.ResponseWriter, r *http.Request) {
	sess := authSess(r)
	rows, err := s.Store.DB.QueryContext(r.Context(),
		`SELECT mn.id, mn.cookie_id, mn.channel_id, COALESCE(nc.name, ''), mn.enabled
		   FROM message_notifications mn
		   JOIN cookies c ON c.id=mn.cookie_id
		   LEFT JOIN notification_channels nc ON nc.id=mn.channel_id
		  WHERE c.user_id=?
		  ORDER BY mn.id DESC`, sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()
	out := make(map[string][]map[string]any)
	for rows.Next() {
		var id, channelID int64
		var cookieID, channelName string
		var enabled int
		if err := rows.Scan(&id, &cookieID, &channelID, &channelName, &enabled); err != nil {
			writeErr(w, http.StatusInternalServerError, "查询失败")
			return
		}
		out[cookieID] = append(out[cookieID], map[string]any{
			"id":           id,
			"channel_id":   channelID,
			"channel_name": channelName,
			"enabled":      enabled != 0,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) setAccountBindings(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
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
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		_, err := s.Store.DB.ExecContext(r.Context(),
			`INSERT INTO message_notifications (cookie_id, channel_id, enabled)
			 VALUES (?, ?, ?)`+db.DialectUpsert(s.Store.Dialect, []string{"cookie_id", "channel_id"}, map[string]string{
				"enabled":     "EXCLUDED.enabled",
				"updated_at": "CURRENT_TIMESTAMP",
			}),
			cid, req.ChannelID, btoi(enabled))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "保存失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
		return
	}
	if err := s.Store.Notifications.SetBindings(r.Context(), cid, req.ChannelIDs); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) deleteMessageNotification(w http.ResponseWriter, r *http.Request) {
	sess := authSess(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "notification_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	_, err = s.Store.DB.ExecContext(r.Context(),
		`DELETE FROM message_notifications
		  WHERE id=? AND cookie_id IN (SELECT id FROM cookies WHERE user_id=?)`,
		id, sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) deleteAccountNotifications(w http.ResponseWriter, r *http.Request) {
	sess := authSess(r)
	cid := chi.URLParam(r, "cid")
	_, err := s.Store.DB.ExecContext(r.Context(),
		`DELETE FROM message_notifications
		  WHERE cookie_id=? AND cookie_id IN (SELECT id FROM cookies WHERE user_id=?)`,
		cid, sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
