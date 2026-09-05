package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestListChannels 列出通知渠道。
func TestListChannels(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// 先创建一个渠道。
	body := `{"name":"钉钉","type":"dingtalk","config":"{}","enabled":true}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/notification-channels", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 列出。
	req2 := httptest.NewRequest(http.MethodGet, "/notification-channels", nil)
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("list status=%d", rec2.Code)
	}
	// arr 用于本次流程后续判断的arr
	var arr []map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &arr)
	if len(arr) != 1 || arr[0]["name"] != "钉钉" {
		t.Fatalf("渠道列表异常: %+v", arr)
	}
}

// TestGetChannelEditorReturnsEmailRecipient 验证编辑接口回显收件邮箱且不泄露 SMTP 密码。
func TestGetChannelEditorReturnsEmailRecipient(t *testing.T) {
	// srv、store、cleanup 保存当前测试 HTTP 服务、数据库和资源清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整 HTTP 路由。
	handler := srv.Router()
	// sessionCookie 是当前测试用户的认证 Cookie。
	sessionCookie := loginHelper(t, handler)
	// insertErr 保存写入含敏感字段的测试邮件渠道结果。
	_, insertErr := store.DB.ExecContext(context.Background(), `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES (?,?,?,?,?)`, "邮件", "email", `{"to_email":"receiver@example.com","use_custom_smtp":false,"smtp_password":"secret"}`, 1, 1)
	if insertErr != nil {
		t.Fatalf("写入邮件渠道失败: %v", insertErr)
	}
	// request 是读取邮件渠道脱敏编辑配置的请求。
	request := httptest.NewRequest(http.MethodGet, "/notification-channels/1", nil)
	request.AddCookie(sessionCookie)
	// recorder 捕获编辑接口响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("编辑渠道 status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"to_email":"receiver@example.com"`) || strings.Contains(recorder.Body.String(), "smtp_password") || strings.Contains(recorder.Body.String(), "secret") {
		t.Fatalf("编辑渠道响应异常或泄露秘密: %s", recorder.Body.String())
	}
}

// TestCreateChannelMissingName 缺 name/type 400。
func TestCreateChannelMissingName(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"type":"dingtalk"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/notification-channels", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 name 应 400，got %d", rec.Code)
	}
}

// TestCreateChannelBadJSON 非法 JSON 400。
func TestCreateChannelBadJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/notification-channels", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestUpdateChannel 更新渠道。
func TestUpdateChannel(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// 创建。
	body := `{"name":"钉钉","type":"dingtalk","config":"{\"webhook_url\":\"https://example.com\"}","event_types":"[\"account_offline\"]","enabled":true}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/notification-channels", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// cr 用于本次流程后续判断的cr
	var cr map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cr)
	// id 用于本次流程后续判断的标识
	id := int64(cr["id"].(float64))

	// 部分更新：只切换 enabled，不应清空 name/type/config/event_types。
	upd := `{"enabled":false}`
	// req2 用于本次流程后续判断的req2
	req2 := httptest.NewRequest(http.MethodPut, "/notification-channels/"+itoa(id), strings.NewReader(upd))
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("update status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	// row、err 用于本次流程后续判断的row、err
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
	// rec3 用于本次流程后续判断的rec3
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
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/notification-channels/abc", strings.NewReader(`{}`))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}
}

// TestUpdateChannelBadJSON 非法 JSON 400。
func TestUpdateChannelBadJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/notification-channels/1", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestDeleteChannel 删除渠道。
func TestDeleteChannel(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"name":"钉钉","type":"dingtalk","config":"{}","enabled":true}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/notification-channels", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// cr 用于本次流程后续判断的cr
	var cr map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cr)
	// id 用于本次流程后续判断的标识
	id := int64(cr["id"].(float64))

	// req2 用于本次流程后续判断的req2
	req2 := httptest.NewRequest(http.MethodDelete, "/notification-channels/"+itoa(id), nil)
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("delete status=%d", rec2.Code)
	}
}

// TestDeleteChannelBadID 无效 ID 400。
func TestDeleteChannelBadID(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodDelete, "/notification-channels/abc", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}
}

// TestTestChannelNoNotifier 未注入通知器 503。
func TestTestChannelNoNotifier(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/notification-channels/1/test", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("无通知器应 503，got %d", rec.Code)
	}
}

// TestTestChannelBadID 无效 ID 400。
func TestTestChannelBadID(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/notification-channels/abc/test", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}
}

// TestListMessageNotifications 列出消息通知绑定。
func TestListMessageNotifications(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// 创建渠道 + 绑定。
	store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name, type, config, enabled, user_id) VALUES ('钉钉','dingtalk','{}',1,1)`)
	store.DB.ExecContext(ctx, `INSERT INTO message_notifications (cookie_id, channel_id, enabled) VALUES ('acc1',1,1)`)
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/message-notifications", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// m 用于本次流程后续判断的m
	var m map[string][]map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	if len(m["acc1"]) != 1 {
		t.Fatalf("绑定列表异常: %+v", m)
	}
}

// TestMessageNotificationsFilterCrossUserChannels 封装Test消息通知列表FilterCross用户渠道列表业务协调。
func TestMessageNotificationsFilterCrossUserChannels(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	_, err := store.Users.Create(ctx, "notif2", "notif2@example.com", "pw"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	// u2 用于本次流程后续判断的u2
	u2, _ := store.Users.GetByUsername(ctx, "notif2")
	// ownID、err 用于本次流程后续判断的ownID、err
	ownID, err := store.Notifications.CreateChannel(ctx, &db.NotificationChannelRow{
		Name: "own", Type: "webhook", Config: `{}`, Enabled: true, UserID: 1,
	})
	if err != nil {
		t.Fatalf("create own channel: %v", err)
	}
	// otherID、err 用于本次流程后续判断的otherID、err
	otherID, err := store.Notifications.CreateChannel(ctx, &db.NotificationChannelRow{
		Name: "other-secret", Type: "webhook", Config: `{}`, Enabled: true, UserID: u2.ID,
	})
	if err != nil {
		t.Fatalf("create other channel: %v", err)
	}
	if // err 用于本次流程后续判断的err
	_, err := store.DB.ExecContext(ctx, `INSERT INTO message_notifications (cookie_id, channel_id, enabled) VALUES ('acc1', ?, 1)`, ownID); err != nil {
		t.Fatalf("insert own binding: %v", err)
	}
	if // err 用于本次流程后续判断的err
	_, err := store.DB.ExecContext(ctx, `INSERT INTO message_notifications (cookie_id, channel_id, enabled) VALUES ('acc1', ?, 1)`, otherID); err != nil {
		t.Fatalf("insert dirty binding: %v", err)
	}

	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// listReq 用于本次流程后续判断的listReq
	listReq := httptest.NewRequest(http.MethodGet, "/message-notifications", nil)
	listReq.AddCookie(cookie)
	// listRec 用于本次流程后续判断的listRec
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	// listed 用于本次流程后续判断的listed
	var listed map[string][]map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed["acc1"]) != 1 || listed["acc1"][0]["channel_name"] != "own" {
		t.Fatalf("list should filter dirty binding: %+v", listed)
	}

	// getReq 用于本次流程后续判断的getReq
	getReq := httptest.NewRequest(http.MethodGet, "/message-notifications/acc1", nil)
	getReq.AddCookie(cookie)
	// getRec 用于本次流程后续判断的getRec
	getRec := httptest.NewRecorder()
	h.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	// got 用于本次流程后续判断的got
	var got map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	// ids 用于本次流程后续判断的ids
	ids, _ := got["channel_ids"].([]any)
	if len(ids) != 1 || int64(ids[0].(float64)) != ownID {
		t.Fatalf("bindings should filter dirty binding: %+v", got)
	}

	// updateOtherReq 用于本次流程后续判断的updateOtherReq
	updateOtherReq := httptest.NewRequest(http.MethodPut, "/notification-channels/"+itoa(otherID), strings.NewReader(`{"enabled":false}`))
	updateOtherReq.AddCookie(cookie)
	// updateOtherRec 用于本次流程后续判断的updateOtherRec
	updateOtherRec := httptest.NewRecorder()
	h.ServeHTTP(updateOtherRec, updateOtherReq)
	if updateOtherRec.Code != http.StatusForbidden {
		t.Fatalf("cross-user update should be 403, got %d body=%s", updateOtherRec.Code, updateOtherRec.Body.String())
	}
	// otherRow、err 用于本次流程后续判断的otherRow、err
	otherRow, err := store.Notifications.GetChannelRowForUser(ctx, otherID, u2.ID)
	if err != nil {
		t.Fatalf("get other channel: %v", err)
	}
	if otherRow == nil || !otherRow.Enabled {
		t.Fatalf("cross-user update should not mutate other channel: %+v", otherRow)
	}

	// deleteOtherReq 用于本次流程后续判断的deleteOtherReq
	deleteOtherReq := httptest.NewRequest(http.MethodDelete, "/notification-channels/"+itoa(otherID), nil)
	deleteOtherReq.AddCookie(cookie)
	// deleteOtherRec 用于本次流程后续判断的deleteOtherRec
	deleteOtherRec := httptest.NewRecorder()
	h.ServeHTTP(deleteOtherRec, deleteOtherReq)
	if deleteOtherRec.Code != http.StatusForbidden {
		t.Fatalf("cross-user delete should be 403, got %d body=%s", deleteOtherRec.Code, deleteOtherRec.Body.String())
	}
}

// TestDeleteMessageNotification 删除单条消息通知。
func TestDeleteMessageNotification(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name, type, config, enabled, user_id) VALUES ('钉钉','dingtalk','{}',1,1)`)
	store.DB.ExecContext(ctx, `INSERT INTO message_notifications (cookie_id, channel_id, enabled) VALUES ('acc1',1,1)`)
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodDelete, "/message-notifications/1", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestDeleteMessageNotificationBadID 无效 ID 400。
func TestDeleteMessageNotificationBadID(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodDelete, "/message-notifications/abc", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}
}

// TestDeleteAccountNotifications 删除账号的所有通知绑定。
func TestDeleteAccountNotifications(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name, type, config, enabled, user_id) VALUES ('钉钉','dingtalk','{}',1,1)`)
	store.DB.ExecContext(ctx, `INSERT INTO message_notifications (cookie_id, channel_id, enabled) VALUES ('acc1',1,1)`)
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodDelete, "/message-notifications/account/acc1", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestSetAccountBindingsBadJSON 非法 JSON 400。
func TestSetAccountBindingsBadJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/message-notifications/acc1", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetAccountBindingsSingleChannel 单渠道绑定。
func TestSetAccountBindingsSingleChannel(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name, type, config, enabled, user_id) VALUES ('钉钉','dingtalk','{}',1,1)`)
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"channel_id":1,"enabled":true}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/message-notifications/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestNotificationRecipientPatchContract 验证真实版本化接口仅更新收件地址，保留旧 SMTP，并拒绝越权和歧义输入。
func TestNotificationRecipientPatchContract(t *testing.T) {
	// srv、store、cleanup 提供隔离 HTTP 服务、仓储和生命周期清理。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler、sessionCookie 是当前用户的真实 Router 和测试会话，不输出会话凭据。
	handler := srv.Router()
	// sessionCookie 只用于本机测试请求授权。
	sessionCookie := loginHelper(t, handler)
	// channelID、err 创建带旧版逐字段 SMTP 和虚构密码的邮件渠道。
	channelID, err := store.Notifications.CreateChannel(context.Background(), &db.NotificationChannelRow{Name: "legacy", Type: "email", Config: `{"email":"old@example.com","smtp_server":"fixture.invalid","smtp_password":"fixture-secret"}`, Enabled: true, UserID: 1})
	if err != nil {
		t.Fatal(err)
	}
	// path 是本轮仅供测试操作的渠道路径。
	path := "/api/v1/notifications/channels/" + strconv.FormatInt(channelID, 10)
	// request、recorder 记录真实更新成功响应并参与 OpenAPI 契约校验。
	request := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"name":"renamed","email_recipient":"new@example.com"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(sessionCookie)
	// recorder 接收不含渠道配置的成功结果。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertOpenAPISuccessResponse(t, request, recorder)
	// channel、err 仅在测试仓储断言边界读取已保存的虚构配置，不输出其内容。
	channel, err := store.Notifications.GetChannel(context.Background(), channelID)
	if err != nil || channel == nil {
		t.Fatal("未读取到保存后的渠道")
	}
	if !strings.Contains(channel.Config, `"to_email":"new@example.com"`) || !strings.Contains(channel.Config, `"smtp_password":"fixture-secret"`) || strings.Contains(channel.Config, "use_custom_smtp") {
		t.Fatal("收件更新未保留旧 SMTP 配置")
	}
	// body 枚举不可同时发送的配置替换与不兼容渠道类型。
	for _, body := range []string{`{"email_recipient":"new@example.com","config":"{}"}`, `{"email_recipient":"new@example.com","type":"webhook"}`, `{"email_recipient":" "}`} {
		request = httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(sessionCookie)
		recorder = httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assertOpenAPIExpectedStatusResponse(t, request, recorder, http.StatusBadRequest)
	}
	request = httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"email_recipient":"new@example.com"}`))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertOpenAPIExpectedStatusResponse(t, request, recorder, http.StatusUnauthorized)
	// created、createErr 建立真实外键目标，仅使用虚构测试身份。
	created, createErr := store.Users.Create(context.Background(), "recipient-other", "recipient-other@example.com", "fixture-password")
	if createErr != nil || !created {
		t.Fatal("创建其他用户失败")
	}
	// otherUser、lookupErr 读取另一用户标识以验证渠道所有权边界。
	otherUser, lookupErr := store.Users.GetByUsername(context.Background(), "recipient-other")
	if lookupErr != nil || otherUser == nil {
		t.Fatal("读取其他用户失败")
	}
	// ownerErr 模拟另一个用户持有渠道，当前登录用户不应修改收件地址。
	if _, ownerErr := store.DB.ExecContext(context.Background(), `UPDATE notification_channels SET user_id=? WHERE id=?`, otherUser.ID, channelID); ownerErr != nil {
		t.Fatal(ownerErr)
	}
	request = httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"email_recipient":"new@example.com"}`))
	request.AddCookie(sessionCookie)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertOpenAPIExpectedStatusResponse(t, request, recorder, http.StatusForbidden)
}
