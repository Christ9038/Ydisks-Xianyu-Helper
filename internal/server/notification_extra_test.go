package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestListChannels 列出通知渠道。
func TestListChannels(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// 先创建一个渠道。
	body := `{"name":"钉钉","type":"dingtalk","config":"{}","enabled":true}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/notification-channels", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 列出。
	req2 := httptest.NewRequest(http.MethodGet, "/notification-channels", nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("list status=%d", rec2.Code)
	}
	// arr 保存arr，供当前处理流程使用
	var arr []map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &arr)
	if len(arr) != 1 || arr[0]["name"] != "钉钉" {
		t.Fatalf("渠道列表异常: %+v", arr)
	}
}

// TestCreateChannelMissingName 缺 name/type 400。
func TestCreateChannelMissingName(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"type":"dingtalk"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/notification-channels", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 name 应 400，got %d", rec.Code)
	}
}

// TestCreateChannelBadJSON 非法 JSON 400。
func TestCreateChannelBadJSON(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/notification-channels", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestUpdateChannel 更新渠道。
func TestUpdateChannel(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// 创建。
	body := `{"name":"钉钉","type":"dingtalk","config":"{\"webhook_url\":\"https://example.com\"}","event_types":"[\"account_offline\"]","enabled":true}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/notification-channels", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// cr 保存cr，供当前处理流程使用
	var cr map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cr)
	// id 保存标识，供当前处理流程使用
	id := int64(cr["id"].(float64))

	// 部分更新：只切换 enabled，不应清空 name/type/config/event_types。
	upd := `{"enabled":false}`
	// req2 保存req2，供当前处理流程使用
	req2 := httptest.NewRequest(http.MethodPut, "/notification-channels/"+itoa(id), strings.NewReader(upd))
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("update status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	// row、err 保存row、err，供当前处理流程使用
	row, err := store.Notifications.GetChannelRowForUser(context.Background(), id, 1)
	if err != nil {
		t.Fatalf("get channel row: %v", err)
	}
	if row == nil {
		t.Fatal("channel row missing")
	}
	if row.Name != "钉钉" || row.Type != "dingtalk" || row.Config != `{"webhook_url":"https://example.com"}` ||
		row.EventTypes != `["account_offline"]` || row.Enabled {
		t.Fatalf("partial update should preserve omitted fields and change enabled: %+v", row)
	}

	// 显式传空 event_types 表示恢复为接收全部事件，其它字段仍保留。
	req3 := httptest.NewRequest(http.MethodPut, "/notification-channels/"+itoa(id), strings.NewReader(`{"event_types":""}`))
	req3.AddCookie(cookie)
	// rec3 保存rec3，供当前处理流程使用
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("clear event_types status=%d body=%s", rec3.Code, rec3.Body.String())
	}
	row, err = store.Notifications.GetChannelRowForUser(context.Background(), id, 1)
	if err != nil {
		t.Fatalf("get channel row after clear: %v", err)
	}
	if row.EventTypes != "" || row.Name != "钉钉" || row.Config != `{"webhook_url":"https://example.com"}` {
		t.Fatalf("event_types clear should not affect other fields: %+v", row)
	}
}

// TestUpdateChannelBadID 无效 ID 400。
func TestUpdateChannelBadID(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/notification-channels/abc", strings.NewReader(`{}`))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}
}

// TestUpdateChannelBadJSON 非法 JSON 400。
func TestUpdateChannelBadJSON(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/notification-channels/1", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestDeleteChannel 删除渠道。
func TestDeleteChannel(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"name":"钉钉","type":"dingtalk","config":"{}","enabled":true}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/notification-channels", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// cr 保存cr，供当前处理流程使用
	var cr map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cr)
	// id 保存标识，供当前处理流程使用
	id := int64(cr["id"].(float64))

	// req2 保存req2，供当前处理流程使用
	req2 := httptest.NewRequest(http.MethodDelete, "/notification-channels/"+itoa(id), nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("delete status=%d", rec2.Code)
	}
}

// TestDeleteChannelBadID 无效 ID 400。
func TestDeleteChannelBadID(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodDelete, "/notification-channels/abc", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}
}

// TestTestChannelNoNotifier 未注入通知器 503。
func TestTestChannelNoNotifier(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/notification-channels/1/test", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("无通知器应 503，got %d", rec.Code)
	}
}

// TestTestChannelBadID 无效 ID 400。
func TestTestChannelBadID(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/notification-channels/abc/test", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}
}

// TestListMessageNotifications 列出消息通知绑定。
func TestListMessageNotifications(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// 创建渠道 + 绑定。
	store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name, type, config, enabled, user_id) VALUES ('钉钉','dingtalk','{}',1,1)`)
	store.DB.ExecContext(ctx, `INSERT INTO message_notifications (cookie_id, channel_id, enabled) VALUES ('acc1',1,1)`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/message-notifications", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// m 保存m，供当前处理流程使用
	var m map[string][]map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	if len(m["acc1"]) != 1 {
		t.Fatalf("绑定列表异常: %+v", m)
	}
}

// TestMessageNotificationsFilterCrossUserChannels 负责Test消息通知列表FilterCross用户渠道列表相关处理。
func TestMessageNotificationsFilterCrossUserChannels(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	if // err 保存err，供当前处理流程使用
	_, err := store.Users.Create(ctx, "notif2", "notif2@example.com", "pw"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	// u2 保存u2，供当前处理流程使用
	u2, _ := store.Users.GetByUsername(ctx, "notif2")
	// ownID、err 保存ownID、err，供当前处理流程使用
	ownID, err := store.Notifications.CreateChannel(ctx, &db.NotificationChannelRow{
		Name: "own", Type: "webhook", Config: `{}`, Enabled: true, UserID: 1,
	})
	if err != nil {
		t.Fatalf("create own channel: %v", err)
	}
	// otherID、err 保存otherID、err，供当前处理流程使用
	otherID, err := store.Notifications.CreateChannel(ctx, &db.NotificationChannelRow{
		Name: "other-secret", Type: "webhook", Config: `{}`, Enabled: true, UserID: u2.ID,
	})
	if err != nil {
		t.Fatalf("create other channel: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	_, err := store.DB.ExecContext(ctx, `INSERT INTO message_notifications (cookie_id, channel_id, enabled) VALUES ('acc1', ?, 1)`, ownID); err != nil {
		t.Fatalf("insert own binding: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	_, err := store.DB.ExecContext(ctx, `INSERT INTO message_notifications (cookie_id, channel_id, enabled) VALUES ('acc1', ?, 1)`, otherID); err != nil {
		t.Fatalf("insert dirty binding: %v", err)
	}

	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// listReq 保存listReq，供当前处理流程使用
	listReq := httptest.NewRequest(http.MethodGet, "/message-notifications", nil)
	listReq.AddCookie(cookie)
	// listRec 保存listRec，供当前处理流程使用
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	// listed 保存listed，供当前处理流程使用
	var listed map[string][]map[string]any
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed["acc1"]) != 1 || listed["acc1"][0]["channel_name"] != "own" {
		t.Fatalf("list should filter dirty binding: %+v", listed)
	}

	// getReq 保存getReq，供当前处理流程使用
	getReq := httptest.NewRequest(http.MethodGet, "/message-notifications/acc1", nil)
	getReq.AddCookie(cookie)
	// getRec 保存getRec，供当前处理流程使用
	getRec := httptest.NewRecorder()
	h.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	// got 保存got，供当前处理流程使用
	var got map[string]any
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	// ids 保存ids，供当前处理流程使用
	ids, _ := got["channel_ids"].([]any)
	if len(ids) != 1 || int64(ids[0].(float64)) != ownID {
		t.Fatalf("bindings should filter dirty binding: %+v", got)
	}

	// updateOtherReq 保存updateOtherReq，供当前处理流程使用
	updateOtherReq := httptest.NewRequest(http.MethodPut, "/notification-channels/"+itoa(otherID), strings.NewReader(`{"enabled":false}`))
	updateOtherReq.AddCookie(cookie)
	// updateOtherRec 保存updateOtherRec，供当前处理流程使用
	updateOtherRec := httptest.NewRecorder()
	h.ServeHTTP(updateOtherRec, updateOtherReq)
	if updateOtherRec.Code != http.StatusForbidden {
		t.Fatalf("cross-user update should be 403, got %d body=%s", updateOtherRec.Code, updateOtherRec.Body.String())
	}
	// otherRow、err 保存otherRow、err，供当前处理流程使用
	otherRow, err := store.Notifications.GetChannelRowForUser(ctx, otherID, u2.ID)
	if err != nil {
		t.Fatalf("get other channel: %v", err)
	}
	if otherRow == nil || !otherRow.Enabled {
		t.Fatalf("cross-user update should not mutate other channel: %+v", otherRow)
	}

	// deleteOtherReq 保存deleteOtherReq，供当前处理流程使用
	deleteOtherReq := httptest.NewRequest(http.MethodDelete, "/notification-channels/"+itoa(otherID), nil)
	deleteOtherReq.AddCookie(cookie)
	// deleteOtherRec 保存deleteOtherRec，供当前处理流程使用
	deleteOtherRec := httptest.NewRecorder()
	h.ServeHTTP(deleteOtherRec, deleteOtherReq)
	if deleteOtherRec.Code != http.StatusForbidden {
		t.Fatalf("cross-user delete should be 403, got %d body=%s", deleteOtherRec.Code, deleteOtherRec.Body.String())
	}
}

// TestDeleteMessageNotification 删除单条消息通知。
func TestDeleteMessageNotification(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name, type, config, enabled, user_id) VALUES ('钉钉','dingtalk','{}',1,1)`)
	store.DB.ExecContext(ctx, `INSERT INTO message_notifications (cookie_id, channel_id, enabled) VALUES ('acc1',1,1)`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodDelete, "/message-notifications/1", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestDeleteMessageNotificationBadID 无效 ID 400。
func TestDeleteMessageNotificationBadID(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodDelete, "/message-notifications/abc", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}
}

// TestDeleteAccountNotifications 删除账号的所有通知绑定。
func TestDeleteAccountNotifications(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name, type, config, enabled, user_id) VALUES ('钉钉','dingtalk','{}',1,1)`)
	store.DB.ExecContext(ctx, `INSERT INTO message_notifications (cookie_id, channel_id, enabled) VALUES ('acc1',1,1)`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodDelete, "/message-notifications/account/acc1", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestSetAccountBindingsBadJSON 非法 JSON 400。
func TestSetAccountBindingsBadJSON(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/message-notifications/acc1", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetAccountBindingsSingleChannel 单渠道绑定。
func TestSetAccountBindingsSingleChannel(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name, type, config, enabled, user_id) VALUES ('钉钉','dingtalk','{}',1,1)`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"channel_id":1,"enabled":true}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/message-notifications/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
