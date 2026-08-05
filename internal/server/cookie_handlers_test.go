package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	xrenew "xianyu-go/internal/xianyu/renew"
)

func seedStaleCookieSnapshot(t *testing.T, store *db.Store, cookieID string) {
	t.Helper()
	ctx := context.Background()
	detail, err := store.Cookies.GetDetails(ctx, cookieID)
	if err != nil {
		t.Fatalf("GetDetails before seeding snapshot: %v", err)
	}
	metadata := cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, []cookierefresh.BrowserCookie{{
		Name: "stale_snapshot", Value: "old", Domain: ".goofish.com", Path: "/",
	}})
	if err := store.Cookies.UpdateRenewalCookie(ctx, cookieID, detail.Value, metadata, time.Now().Unix()); err != nil {
		t.Fatalf("seed stale snapshot: %v", err)
	}
}

func requireCookieSnapshotCleared(t *testing.T, store *db.Store, cookieID string) {
	t.Helper()
	detail, err := store.Cookies.GetDetails(context.Background(), cookieID)
	if err != nil {
		t.Fatalf("GetDetails after cookie overwrite: %v", err)
	}
	if snapshot, ok := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); ok {
		t.Fatalf("扁平 Cookie 覆盖后必须清除旧快照: %+v", snapshot)
	}
}

func TestLongLoginSettingsProxyAndPersistCookieSnapshot(t *testing.T) {
	passport := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fromSite") != "77" || r.URL.Query().Get("appName") != "xianyu" || r.URL.Query().Get("bizEntrance") != "web" {
			t.Fatalf("query=%s", r.URL.RawQuery)
		}
		requestCookies := r.Header.Get("Cookie")
		if !strings.Contains(requestCookies, "passport_only=allowed") || strings.Contains(requestCookies, "www_only=blocked") {
			http.Error(w, "Cookie 未按 passport 域作用域发送: "+requestCookies, http.StatusBadRequest)
			return
		}
		if strings.Contains(r.URL.Path, "set") {
			if err := r.ParseForm(); err != nil || r.Form.Get("status") != "0" {
				t.Fatalf("form=%v err=%v", r.Form, err)
			}
		}
		w.Header().Add("Set-Cookie", "havana_lgc_exp=4102444800000; Domain=.goofish.com; Path=/; Secure; HttpOnly; SameSite=None")
		if strings.Contains(r.URL.Path, "set") {
			_, _ = w.Write([]byte(`{"data":{"success":true}}`))
			return
		}
		_, _ = w.Write([]byte(`{"content":{"data":{"returnValue":{"canOpenLongLogin":true,"hasLongTokenLogin":true}}}}`))
	}))
	defer passport.Close()

	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.CookieRenew = xrenew.Service{
		HTTPClient:            passport.Client(),
		QueryLoginSettingsURL: passport.URL + "/queryLoginSettings.do",
		SetLoginSettingsURL:   passport.URL + "/setLoginSettings.do",
	}
	detail, err := store.Cookies.GetDetails(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := cookierefresh.SnapshotFromCookieString(detail.Value, ".goofish.com")
	snapshot = append(snapshot,
		cookierefresh.BrowserCookie{Name: "passport_only", Value: "allowed", Domain: ".goofish.com", Path: "/", Secure: true},
		cookierefresh.BrowserCookie{Name: "www_only", Value: "blocked", Domain: "www.goofish.com", Path: "/", Secure: true},
	)
	metadata := cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, snapshot)
	if err := store.Cookies.UpdateRenewalCookie(context.Background(), "acc1", detail.Value, metadata, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	h := srv.Router()
	session := loginHelper(t, h)

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/cookies/acc1/long-login", nil),
		httptest.NewRequest(http.MethodPut, "/cookies/acc1/long-login", strings.NewReader(`{"enabled":true}`)),
	} {
		request.AddCookie(session)
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", request.Method, recorder.Code, recorder.Body.String())
		}
		var result xrenew.LongLoginSettings
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || !result.CanOpenLongLogin || !result.Enabled {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}

	detail, err = store.Cookies.GetDetails(context.Background(), "acc1")
	if err != nil || !strings.Contains(detail.Value, "havana_lgc_exp=4102444800000") {
		t.Fatalf("cookie detail=%+v err=%v", detail, err)
	}
	snapshot = cookierefresh.SnapshotFromMetadata(detail.MetadataJSON)
	var longLoginCookie *cookierefresh.BrowserCookie
	for i := range snapshot {
		if snapshot[i].Name == "havana_lgc_exp" {
			longLoginCookie = &snapshot[i]
			break
		}
	}
	if longLoginCookie == nil || longLoginCookie.Domain != ".goofish.com" || !longLoginCookie.Secure || !longLoginCookie.HTTPOnly {
		t.Fatalf("未保留 Set-Cookie 属性: %+v", snapshot)
	}
}

func TestLongLoginFailureStillPersistsResponseCookiesWithoutInventingSnapshot(t *testing.T) {
	passport := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "rotated=fresh; Domain=.goofish.com; Path=/; Secure; HttpOnly")
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer passport.Close()

	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.CookieRenew = xrenew.Service{
		HTTPClient:            passport.Client(),
		QueryLoginSettingsURL: passport.URL + "/queryLoginSettings.do",
	}
	h := srv.Router()
	session := loginHelper(t, h)
	req := httptest.NewRequest(http.MethodGet, "/cookies/acc1/long-login", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	detail, err := store.Cookies.GetDetails(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail.Value, "rotated=fresh") {
		t.Fatalf("失败响应头的 Cookie 未持久化: %q", detail.Value)
	}
	if _, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); complete {
		t.Fatal("历史扁平 Cookie 不得因长登录响应伪造成完整浏览器 Jar")
	}
}

func TestLongLoginAuthoritativeSnapshotCanBeDeletedToEmpty(t *testing.T) {
	passport := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "only_cookie=; Domain=.goofish.com; Path=/; Max-Age=0; Secure")
		_, _ = w.Write([]byte(`{"content":{"data":{"returnValue":{"canOpenLongLogin":true,"hasLongTokenLogin":true}}}}`))
	}))
	defer passport.Close()

	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	detail, err := store.Cookies.GetDetails(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	metadata := cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, []cookierefresh.BrowserCookie{{
		Name: "only_cookie", Value: "old", Domain: ".goofish.com", Path: "/", Secure: true,
	}})
	if err := store.Cookies.UpdateRenewalCookie(context.Background(), "acc1", "only_cookie=old", metadata, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	srv.CookieRenew = xrenew.Service{
		HTTPClient:            passport.Client(),
		QueryLoginSettingsURL: passport.URL + "/queryLoginSettings.do",
	}
	h := srv.Router()
	session := loginHelper(t, h)
	req := httptest.NewRequest(http.MethodGet, "/cookies/acc1/long-login", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	detail, err = store.Cookies.GetDetails(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Value != "" {
		t.Fatalf("权威 Jar 删除后扁平值=%q want empty", detail.Value)
	}
	snapshot, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	if !complete || len(snapshot) != 0 {
		t.Fatalf("应保留权威空 Jar，complete=%v snapshot=%+v", complete, snapshot)
	}
}

// TestListCookies 列表 cookie_id。
func TestListCookies(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/cookies", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var ids []string
	json.Unmarshal(rec.Body.Bytes(), &ids)
	if len(ids) != 1 || ids[0] != "acc1" {
		t.Fatalf("cookies 列表异常: %+v", ids)
	}
}

// TestRefreshCookieProfile 主动刷新账号资料。
func TestRefreshCookieProfile(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/cookies/acc1/refresh-profile", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] != true {
		t.Fatalf("刷新资料应成功: %+v", res)
	}
	if res["nickname"] != "测试账号" {
		t.Fatalf("昵称异常: %v", res["nickname"])
	}
}

// TestRefreshCookieProfileBadCookie 无权账号 403。
func TestRefreshCookieProfileBadCookie(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/cookies/other/refresh-profile", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权账号应 403，got %d", rec.Code)
	}
}

// TestGetCookieDetails 单账号详情。
func TestGetCookieDetails(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/cookie/acc1/details", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var d map[string]any
	json.Unmarshal(rec.Body.Bytes(), &d)
	if d["id"] != "acc1" || d["has_cookie"] != true {
		t.Fatalf("详情异常: %+v", d)
	}
}

func TestListCookieDetailsIncludesShowBrowser(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	if err := store.Cookies.UpdateLoginInfo(ctx, "acc1", "login-user", "secret", true); err != nil {
		t.Fatalf("UpdateLoginInfo: %v", err)
	}
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/cookies/details", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var details []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &details); err != nil {
		t.Fatalf("decode details: %v", err)
	}
	if len(details) != 1 || details[0]["show_browser"] != true {
		t.Fatalf("账号列表应返回 show_browser=true: %+v", details)
	}
	if _, ok := details[0]["login_password"]; ok {
		t.Fatalf("账号列表不应返回登录密码: %+v", details[0])
	}
}

func TestUpdateCookieSettingsAtomically(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	channelID, err := store.Notifications.CreateChannel(context.Background(), &db.NotificationChannelRow{
		Name: "owned", Type: "webhook", Config: `{}`, Enabled: true, UserID: admin.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"remark":"atomic","auto_confirm":false,"pause_duration":3,"username":"login-user","show_browser":true,"channel_ids":[` + jsonInt(channelID) + `]}`
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/settings", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	detail, _ := store.Cookies.GetDetails(context.Background(), "acc1")
	bindings, _ := store.Notifications.AccountBindings(context.Background(), "acc1")
	if detail.Remark != "atomic" || detail.AutoConfirm || detail.PauseDuration != 3 || detail.Username != "login-user" || !detail.ShowBrowser {
		t.Fatalf("detail=%+v", detail)
	}
	if len(bindings) != 1 || bindings[0] != channelID {
		t.Fatalf("bindings=%v", bindings)
	}
}

func TestUpdateCookieSettingsClearsTokenButKeepsDeviceID(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	if err := store.Tokens.Save(ctx, "acc1", "permanent-device", "old-token", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	seedStaleCookieSnapshot(t, store, "acc1")
	h := srv.Router()
	cookie := loginHelper(t, h)
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/settings", strings.NewReader(`{"cookie":"unb=123; _m_h5_tk=new_1;"}`))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	token, err := store.Tokens.Get(ctx, "acc1")
	if err != nil || token.DeviceID != "permanent-device" || token.AccessToken != "" || token.ExpireAt != 0 {
		t.Fatalf("token=%+v err=%v", token, err)
	}
	requireCookieSnapshotCleared(t, store, "acc1")
}

func jsonInt(value int64) string { return strconv.FormatInt(value, 10) }

// TestGetCookieDetailsBadCookie 无权账号 403。
func TestGetCookieDetailsBadCookie(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/cookie/other/details", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权账号应 403，got %d", rec.Code)
	}
}

// TestUpdateCookie 更新 cookie 值。
func TestUpdateCookie(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	h := srv.Router()
	cookie := loginHelper(t, h)
	if err := store.Tokens.Save(ctx, "acc1", "permanent-device", "old-token", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	seedStaleCookieSnapshot(t, store, "acc1")

	body := `{"value":"unb=123; _m_h5_tk=newtoken_2;"}`
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}
	d, err := store.Cookies.GetDetails(ctx, "acc1")
	if err != nil {
		t.Fatalf("GetDetails: %v", err)
	}
	if d.LoginMethod != "" || d.LastLoginAt != 0 {
		t.Fatalf("普通 Cookie 更新不应刷新登录审计字段: method=%q last=%d", d.LoginMethod, d.LastLoginAt)
	}
	token, err := store.Tokens.Get(ctx, "acc1")
	if err != nil || token.DeviceID != "permanent-device" || token.AccessToken != "" || token.ExpireAt != 0 {
		t.Fatalf("token=%+v err=%v", token, err)
	}
	requireCookieSnapshotCleared(t, store, "acc1")
}

func TestUpdateRunningCookieWakesCredentialBlockedAutomationWithoutManager(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	dueAt := time.Now().UTC().Add(time.Hour).Unix()
	if err := store.Automation.DeferTask(ctx, db.DeferredAutomationTask{
		TaskKey: "acc1:credential", CookieID: "acc1", TriggerType: automation.TriggerOrderPaid,
		TaskJSON: `{}`, DueAt: dueAt, ErrorMessage: "FAIL_SYS_SESSION_EXPIRED",
	}); err != nil {
		t.Fatal(err)
	}
	srv.Manager = nil
	srv.updateRunningCookie(ctx, "acc1", "unb=123; _m_h5_tk=fresh_1;")
	var got int64
	if err := store.DB.QueryRowContext(ctx, `SELECT due_at FROM automation_pending_tasks WHERE task_key=?`, "acc1:credential").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Fatalf("Cookie 更新后凭证失败任务 due_at=%d want 0", got)
	}
}

func TestUpdateCookieQRLoginEnablesAccount(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	if err := store.Cookies.SetStatusWithReason(ctx, "acc1", false, "token 失效"); err != nil {
		t.Fatalf("SetStatusWithReason: %v", err)
	}
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"value":"unb=123; _m_h5_tk=qr_2;","login_method":"qr_scan"}`
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !store.Cookies.GetStatus(ctx, "acc1") {
		t.Fatal("扫码登录成功后应重新启用账号")
	}
	var reason string
	if err := store.DB.QueryRowContext(ctx, `SELECT disable_reason FROM cookie_status WHERE cookie_id='acc1'`).Scan(&reason); err != nil {
		t.Fatalf("query disable_reason: %v", err)
	}
	if reason != "" {
		t.Fatalf("扫码登录成功后应清空禁用原因，got %q", reason)
	}
	d, err := store.Cookies.GetDetails(ctx, "acc1")
	if err != nil {
		t.Fatalf("GetDetails: %v", err)
	}
	if d.LoginMethod != "qr_scan" || d.LastLoginAt == 0 {
		t.Fatalf("扫码登录应刷新登录审计字段: %+v", d)
	}
}

func TestSetCookieStatusRecordsManualDisableReason(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/status", strings.NewReader(`{"enabled":false}`))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var enabled int
	var reason string
	if err := store.DB.QueryRow(`SELECT enabled,disable_reason FROM cookie_status WHERE cookie_id='acc1'`).Scan(&enabled, &reason); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 || reason != db.DisableReasonManual {
		t.Fatalf("enabled=%d reason=%q", enabled, reason)
	}
}

func TestSetCookieStatusWaitsForCredentialTransition(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	h := srv.Router()
	cookie := loginHelper(t, h)

	credentialUnlock := store.LockAccountCredentials("acc1")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/status", strings.NewReader(`{"enabled":false}`))
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		done <- rec
	}()
	select {
	case rec := <-done:
		credentialUnlock()
		t.Fatalf("状态更新绕过了账号凭证锁: status=%d body=%s", rec.Code, rec.Body.String())
	case <-time.After(50 * time.Millisecond):
	}
	credentialUnlock()
	select {
	case rec := <-done:
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("释放凭证锁后状态更新未完成")
	}
}

func TestDeleteCookieRechecksOwnershipInsideCredentialLock(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	ctx := context.Background()
	if ok, err := store.Users.Create(ctx, "member-delete", "member-delete@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create replacement owner: ok=%v err=%v", ok, err)
	}
	replacementOwner, err := store.Users.GetByUsername(ctx, "member-delete")
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Router()
	cookie := loginHelper(t, h)

	credentialUnlock := store.LockAccountCredentials("acc1")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodDelete, "/cookies/acc1", nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		done <- rec
	}()
	select {
	case rec := <-done:
		credentialUnlock()
		t.Fatalf("删除绕过了账号凭证锁: status=%d body=%s", rec.Code, rec.Body.String())
	case <-time.After(50 * time.Millisecond):
	}
	if err := store.Cookies.Delete(ctx, "acc1"); err != nil {
		credentialUnlock()
		t.Fatal(err)
	}
	if err := store.Cookies.CreateOwned(ctx, "acc1", "unb=replacement; _m_h5_tk=fresh", replacementOwner.ID); err != nil {
		credentialUnlock()
		t.Fatal(err)
	}
	credentialUnlock()
	rec := <-done
	if rec.Code == http.StatusOK {
		t.Fatalf("旧 owner 的并发请求不得删除新 owner 账号: body=%s", rec.Body.String())
	}
	detail, err := store.Cookies.GetDetails(ctx, "acc1")
	if err != nil || detail.UserID != replacementOwner.ID {
		t.Fatalf("替换后的账号被误删: detail=%+v err=%v", detail, err)
	}
}

// TestUpdateCookieBadJSON 非法 JSON 400。
func TestUpdateCookieBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestUpdateCookieLoginInfo 更新登录信息。
func TestUpdateCookieLoginInfo(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"username":"u1","password":"p1","show_browser":true}`
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/login-info", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	d, err := store.Cookies.GetDetails(ctx, "acc1")
	if err != nil {
		t.Fatalf("GetDetails: %v", err)
	}
	if d.Username != "u1" || d.Password != "p1" || !d.ShowBrowser {
		t.Fatalf("登录信息未正确保存: %+v", d)
	}

	body = `{"username":"u2","login_password":"","show_browser":false}`
	req = httptest.NewRequest(http.MethodPut, "/cookies/acc1/login-info", strings.NewReader(body))
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	d, err = store.Cookies.GetDetails(ctx, "acc1")
	if err != nil {
		t.Fatalf("GetDetails after empty password: %v", err)
	}
	if d.Username != "u2" || d.Password != "p1" || d.ShowBrowser {
		t.Fatalf("空密码更新应保留原密码并更新其他字段: %+v", d)
	}

	body = `{"username":"u2","clear_password":true,"show_browser":false}`
	req = httptest.NewRequest(http.MethodPut, "/cookies/acc1/login-info", strings.NewReader(body))
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear password status=%d body=%s", rec.Code, rec.Body.String())
	}
	d, err = store.Cookies.GetDetails(ctx, "acc1")
	if err != nil {
		t.Fatalf("GetDetails after clear password: %v", err)
	}
	if d.Username != "u2" || d.Password != "" || d.ShowBrowser {
		t.Fatalf("clear_password 应清空密码并保留其他更新: %+v", d)
	}
}

// TestUpdateCookieLoginInfoBadJSON 非法 JSON 400。
func TestUpdateCookieLoginInfoBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/login-info", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetCookieStatus 启停账号。
func TestSetCookieStatus(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	// 先设置账号为停用，便于测试启用路径。
	store.Cookies.SetStatus(ctx, "acc1", false)
	h := srv.Router()
	cookie := loginHelper(t, h)

	// 启用。
	body := `{"enabled":true}`
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/status", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("enable status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !store.Cookies.GetStatus(ctx, "acc1") {
		t.Fatal("应已启用")
	}

	// 停用。
	body2 := `{"enabled":false}`
	req2 := httptest.NewRequest(http.MethodPut, "/cookies/acc1/status", strings.NewReader(body2))
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("disable status=%d", rec2.Code)
	}
	if store.Cookies.GetStatus(ctx, "acc1") {
		t.Fatal("应已停用")
	}
}

// TestSetCookieStatusBadJSON 非法 JSON 400。
func TestSetCookieStatusBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/status", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetCookieAutoConfirmBadJSON 非法 JSON 400。
func TestSetCookieAutoConfirmBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/auto-confirm", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetCookieRemarkBadJSON 非法 JSON 400。
func TestSetCookieRemarkBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/remark", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestDeleteCookie 删除账号。
func TestDeleteCookie(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	store.Cookies.Save(ctx, "acc-del", "unb=1; _m_h5_tk=t_1;", 1)
	store.Cookies.Save(ctx, "acc-keep", "unb=2; _m_h5_tk=t_2;", 1)
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodDelete, "/cookies/acc-del", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("delete status=%d", rec.Code)
	}
	if _, err := store.Cookies.GetDetails(ctx, "acc-del"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("目标账号应被删除，err=%v", err)
	}
	if kept, err := store.Cookies.GetDetails(ctx, "acc-keep"); err != nil || kept.ID != "acc-keep" {
		t.Fatalf("非目标账号不应被删除，kept=%+v err=%v", kept, err)
	}
}

// TestAddCookieBad 缺 id 或 value 400。
func TestAddCookieBad(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"id":"acc2"}`
	req := httptest.NewRequest(http.MethodPost, "/cookies", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 value 应 400，got %d", rec.Code)
	}
}

func TestAddCookieDefaultsManualLoginAudit(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	ctx := context.Background()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"id":"acc-manual","value":"unb=456; _m_h5_tk=manual_1;"}`
	req := httptest.NewRequest(http.MethodPost, "/cookies", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("add status=%d body=%s", rec.Code, rec.Body.String())
	}
	d, err := store.Cookies.GetDetails(ctx, "acc-manual")
	if err != nil {
		t.Fatalf("GetDetails: %v", err)
	}
	if d.LoginMethod != "manual" || d.LastLoginAt == 0 {
		t.Fatalf("手动新增 Cookie 应记录 manual 登录审计字段: %+v", d)
	}
	logs, err := store.LoginLogs.ListByCookie(ctx, "acc-manual", 10)
	if err != nil || len(logs) != 1 || logs[0].Method != "manual" || logs[0].TriggerReason != "手动Cookie录入" {
		t.Fatalf("手动新增 Cookie 应记录登录日志: logs=%#v err=%v", logs, err)
	}
}

// TestAddCookieBadJSON 非法 JSON 400。
func TestAddCookieBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/cookies", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestGetCookieAutoConfirmNotFound 不存在账号 404。
func TestGetCookieAutoConfirmNotFound(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/cookies/no-such/auto-confirm", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在账号应 404，got %d", rec.Code)
	}
}

// TestCachedAccountNickname 备注优先于昵称。
func TestCachedAccountNickname(t *testing.T) {
	cases := []struct {
		remark, nickname, id, want string
	}{
		{"我的备注", "昵称", "acc1", "我的备注"},
		{"", "昵称", "acc1", "昵称"},
		{"", "", "acc1234567890", "账号 acc123"},
	}
	for _, c := range cases {
		got := cachedAccountNickname(&db.CookieDetail{ID: c.id, Nickname: c.nickname, Remark: c.remark})
		if got != c.want {
			t.Errorf("cachedAccountNickname(remark=%q,nick=%q,id=%q)=%q want %q", c.remark, c.nickname, c.id, got, c.want)
		}
	}
}

// TestNormalizeProfileAvatarURL 头像 URL 归一。
func TestNormalizeProfileAvatarURL(t *testing.T) {
	cases := map[string]string{
		"":                                 "",
		"//img.alicdn.com/x.jpg":           "https://img.alicdn.com/x.jpg",
		"http://img.alicdn.com/x.jpg":      "https://img.alicdn.com/x.jpg",
		"https://img.alicdn.com/x.jpg":     "https://img.alicdn.com/x.jpg",
		"  https://img.alicdn.com/x.jpg  ": "https://img.alicdn.com/x.jpg",
	}
	for in, want := range cases {
		if got := normalizeProfileAvatarURL(in); got != want {
			t.Errorf("normalizeProfileAvatarURL(%q)=%q want %q", in, got, want)
		}
	}
}

// TestTruncate truncate 不超长则原样返回。
func TestTruncate(t *testing.T) {
	if got := truncate("abc", 5); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := truncate("abcdef", 3); got != "abc" {
		t.Fatalf("got %q", got)
	}
}
