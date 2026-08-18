package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// cachedAccountNickname 保留测试对账号展示名兼容规则的独立验证，不参与生产 HTTP 路径。
func cachedAccountNickname(d *db.CookieDetail) string {
	if strings.TrimSpace(d.Remark) != "" {
		return strings.TrimSpace(d.Remark)
	}
	if strings.TrimSpace(d.Nickname) != "" {
		return strings.TrimSpace(d.Nickname)
	}
	return "账号 " + truncate(d.ID, 6)
}

// seedStaleCookieSnapshot 封装seedStale登录凭证Snapshot业务协调。
func seedStaleCookieSnapshot(t *testing.T, store *db.Store, cookieID string) {
	t.Helper()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// detail、err 用于本次流程后续判断的detail、err
	detail, err := store.Cookies.GetDetails(ctx, cookieID)
	if err != nil {
		t.Fatalf("GetDetails before seeding snapshot: %v", err)
	}
	// metadata 用于本次流程后续判断的metadata
	metadata := cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, []cookierefresh.BrowserCookie{{
		Name: "stale_snapshot", Value: "old", Domain: ".goofish.com", Path: "/",
	}})
	if // err 用于本次流程后续判断的err
	err := store.Cookies.UpdateRenewalCookie(ctx, cookieID, detail.Value, metadata, time.Now().Unix()); err != nil {
		t.Fatalf("seed stale snapshot: %v", err)
	}
}

// requireCookieSnapshotCleared 封装require登录凭证SnapshotCleared业务协调。
func requireCookieSnapshotCleared(t *testing.T, store *db.Store, cookieID string) {
	t.Helper()
	// detail、err 用于本次流程后续判断的detail、err
	detail, err := store.Cookies.GetDetails(context.Background(), cookieID)
	if err != nil {
		t.Fatalf("GetDetails after cookie overwrite: %v", err)
	}
	if // snapshot、ok 用于本次流程后续判断的snapshot、ok
	snapshot, ok := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); ok {
		t.Fatalf("扁平 Cookie 覆盖后必须清除旧快照: %+v", snapshot)
	}
}

// TestLongLoginSettingsProxyAndPersistCookieSnapshot 封装TestLong登录设置ProxyAndPersist登录凭证Snapshot业务协调。
func TestLongLoginSettingsProxyAndPersistCookieSnapshot(t *testing.T) {
	// passport 用于本次流程后续判断的passport
	passport := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fromSite") != "77" || r.URL.Query().Get("appName") != "xianyu" || r.URL.Query().Get("bizEntrance") != "web" {
			t.Fatalf("query=%s", r.URL.RawQuery)
		}
		// requestCookies 用于本次流程后续判断的请求Cookies
		requestCookies := r.Header.Get("Cookie")
		if !strings.Contains(requestCookies, "passport_only=allowed") || strings.Contains(requestCookies, "www_only=blocked") {
			http.Error(w, "Cookie 未按 passport 域作用域发送: "+requestCookies, http.StatusBadRequest)
			return
		}
		if strings.Contains(r.URL.Path, "set") {
			if // err 用于本次流程后续判断的err
			err := r.ParseForm(); err != nil || r.Form.Get("status") != "0" {
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

	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.CookieRenew = xrenew.Service{
		HTTPClient:            passport.Client(),
		QueryLoginSettingsURL: passport.URL + "/queryLoginSettings.do",
		SetLoginSettingsURL:   passport.URL + "/setLoginSettings.do",
	}
	// detail、err 用于本次流程后续判断的detail、err
	detail, err := store.Cookies.GetDetails(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := cookierefresh.SnapshotFromCookieString(detail.Value, ".goofish.com")
	snapshot = append(snapshot,
		cookierefresh.BrowserCookie{Name: "passport_only", Value: "allowed", Domain: ".goofish.com", Path: "/", Secure: true},
		cookierefresh.BrowserCookie{Name: "www_only", Value: "blocked", Domain: "www.goofish.com", Path: "/", Secure: true},
	)
	// metadata 用于本次流程后续判断的metadata
	metadata := cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, snapshot)
	if // err 用于本次流程后续判断的err
	err := store.Cookies.UpdateRenewalCookie(context.Background(), "acc1", detail.Value, metadata, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// session 用于本次流程后续判断的会话
	session := loginHelper(t, h)

	// request 表示当前遍历过程中的请求
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/cookies/acc1/long-login", nil),
		httptest.NewRequest(http.MethodPut, "/cookies/acc1/long-login", strings.NewReader(`{"enabled":true}`)),
	} {
		request.AddCookie(session)
		// recorder 用于本次流程后续判断的recorder
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", request.Method, recorder.Code, recorder.Body.String())
		}
		// result 用于本次流程后续判断的结果
		var result xrenew.LongLoginSettings
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || !result.CanOpenLongLogin || !result.Enabled {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}

	detail, err = store.Cookies.GetDetails(context.Background(), "acc1")
	if err != nil || !strings.Contains(detail.Value, "havana_lgc_exp=4102444800000") {
		t.Fatalf("cookie detail=%+v err=%v", detail, err)
	}
	snapshot = cookierefresh.SnapshotFromMetadata(detail.MetadataJSON)
	// longLoginCookie 用于本次流程后续判断的long登录登录凭证
	var longLoginCookie *cookierefresh.BrowserCookie
	// i 表示当前遍历过程中的i
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

// TestLongLoginFailureStillPersistsResponseCookiesWithoutInventingSnapshot 封装TestLong登录FailureStillPersists响应CookiesWithoutInventingSnapshot业务协调。
func TestLongLoginFailureStillPersistsResponseCookiesWithoutInventingSnapshot(t *testing.T) {
	// passport 用于本次流程后续判断的passport
	passport := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "rotated=fresh; Domain=.goofish.com; Path=/; Secure; HttpOnly")
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer passport.Close()

	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.CookieRenew = xrenew.Service{
		HTTPClient:            passport.Client(),
		QueryLoginSettingsURL: passport.URL + "/queryLoginSettings.do",
	}
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// session 用于本次流程后续判断的会话
	session := loginHelper(t, h)
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/cookies/acc1/long-login", nil)
	req.AddCookie(session)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// detail、err 用于本次流程后续判断的detail、err
	detail, err := store.Cookies.GetDetails(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail.Value, "rotated=fresh") {
		t.Fatalf("失败响应头的 Cookie 未持久化: %q", detail.Value)
	}
	if // complete 用于本次流程后续判断的complete
	_, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); complete {
		t.Fatal("历史扁平 Cookie 不得因长登录响应伪造成完整浏览器 Jar")
	}
}

// TestLongLoginAuthoritativeSnapshotCanBeDeletedToEmpty 封装TestLong登录AuthoritativeSnapshotCanBeDeletedToEmpty业务协调。
func TestLongLoginAuthoritativeSnapshotCanBeDeletedToEmpty(t *testing.T) {
	// passport 用于本次流程后续判断的passport
	passport := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "only_cookie=; Domain=.goofish.com; Path=/; Max-Age=0; Secure")
		_, _ = w.Write([]byte(`{"content":{"data":{"returnValue":{"canOpenLongLogin":true,"hasLongTokenLogin":true}}}}`))
	}))
	defer passport.Close()

	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// detail、err 用于本次流程后续判断的detail、err
	detail, err := store.Cookies.GetDetails(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	// metadata 用于本次流程后续判断的metadata
	metadata := cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, []cookierefresh.BrowserCookie{{
		Name: "only_cookie", Value: "old", Domain: ".goofish.com", Path: "/", Secure: true,
	}})
	if // err 用于本次流程后续判断的err
	err := store.Cookies.UpdateRenewalCookie(context.Background(), "acc1", "only_cookie=old", metadata, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	srv.CookieRenew = xrenew.Service{
		HTTPClient:            passport.Client(),
		QueryLoginSettingsURL: passport.URL + "/queryLoginSettings.do",
	}
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// session 用于本次流程后续判断的会话
	session := loginHelper(t, h)
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/cookies/acc1/long-login", nil)
	req.AddCookie(session)
	// rec 用于本次流程后续判断的rec
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
	// snapshot、complete 用于本次流程后续判断的snapshot、complete
	snapshot, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	if !complete || len(snapshot) != 0 {
		t.Fatalf("应保留权威空 Jar，complete=%v snapshot=%+v", complete, snapshot)
	}
}

// TestListCookies 列表 cookie_id。
func TestListCookies(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/cookies", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// ids 用于本次流程后续判断的ids
	var ids []string
	json.Unmarshal(rec.Body.Bytes(), &ids)
	if len(ids) != 1 || ids[0] != "acc1" {
		t.Fatalf("cookies 列表异常: %+v", ids)
	}
}

// TestRefreshCookieProfile 主动刷新账号资料。
func TestRefreshCookieProfile(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/cookies/acc1/refresh-profile", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 用于本次流程后续判断的响应
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
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/cookies/other/refresh-profile", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权账号应 403，got %d", rec.Code)
	}
}

// TestGetCookieDetails 单账号详情。
func TestGetCookieDetails(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/cookie/acc1/details", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// d 用于本次流程后续判断的d
	var d map[string]any
	json.Unmarshal(rec.Body.Bytes(), &d)
	if d["id"] != "acc1" || d["has_cookie"] != true {
		t.Fatalf("详情异常: %+v", d)
	}
}

// TestListCookieDetailsIncludesShowBrowser 封装TestList登录凭证DetailsIncludesShow浏览器业务协调。
func TestListCookieDetailsIncludesShowBrowser(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	err := store.Cookies.UpdateLoginInfo(ctx, "acc1", "login-user", "secret", true); err != nil {
		t.Fatalf("UpdateLoginInfo: %v", err)
	}
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/cookies/details", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// details 用于本次流程后续判断的details
	var details []map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(rec.Body.Bytes(), &details); err != nil {
		t.Fatalf("decode details: %v", err)
	}
	if len(details) != 1 || details[0]["show_browser"] != true {
		t.Fatalf("账号列表应返回 show_browser=true: %+v", details)
	}
	if // ok 用于本次流程后续判断的ok
	_, ok := details[0]["login_password"]; ok {
		t.Fatalf("账号列表不应返回登录密码: %+v", details[0])
	}
}

// TestUpdateCookieSettingsAtomically 封装TestUpdate登录凭证设置Atomically业务协调。
func TestUpdateCookieSettingsAtomically(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	// channelID、err 用于本次流程后续判断的渠道ID、err
	channelID, err := store.Notifications.CreateChannel(context.Background(), &db.NotificationChannelRow{
		Name: "owned", Type: "webhook", Config: `{}`, Enabled: true, UserID: admin.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	// body 用于本次流程后续判断的请求体
	body := `{"remark":"atomic","auto_confirm":false,"pause_duration":3,"username":"login-user","show_browser":true,"channel_ids":[` + jsonInt(channelID) + `]}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/settings", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// detail 用于本次流程后续判断的detail
	detail, _ := store.Cookies.GetDetails(context.Background(), "acc1")
	// bindings 用于本次流程后续判断的bindings
	bindings, _ := store.Notifications.AccountBindings(context.Background(), "acc1")
	if detail.Remark != "atomic" || detail.AutoConfirm || detail.PauseDuration != 3 || detail.Username != "login-user" || !detail.ShowBrowser {
		t.Fatalf("detail=%+v", detail)
	}
	if len(bindings) != 1 || bindings[0] != channelID {
		t.Fatalf("bindings=%v", bindings)
	}
}

// TestUpdateCookieSettingsClearsTokenButKeepsDeviceID 封装TestUpdate登录凭证设置Clears令牌ButKeepsDeviceID业务协调。
func TestUpdateCookieSettingsClearsTokenButKeepsDeviceID(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	err := store.Tokens.Save(ctx, "acc1", "permanent-device", "old-token", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	seedStaleCookieSnapshot(t, store, "acc1")
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/settings", strings.NewReader(`{"cookie":"unb=123; _m_h5_tk=new_1;"}`))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// token、err 用于本次流程后续判断的token、err
	token, err := store.Tokens.Get(ctx, "acc1")
	if err != nil || token.DeviceID != "permanent-device" || token.AccessToken != "" || token.ExpireAt != 0 {
		t.Fatalf("token=%+v err=%v", token, err)
	}
	requireCookieSnapshotCleared(t, store, "acc1")
}

// jsonInt 封装jsonInt业务协调。
func jsonInt(value int64) string { return strconv.FormatInt(value, 10) }

// TestGetCookieDetailsBadCookie 无权账号 403。
func TestGetCookieDetailsBadCookie(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/cookie/other/details", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权账号应 403，got %d", rec.Code)
	}
}

// TestUpdateCookie 更新 cookie 值。
func TestUpdateCookie(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)
	if // err 用于本次流程后续判断的err
	err := store.Tokens.Save(ctx, "acc1", "permanent-device", "old-token", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	seedStaleCookieSnapshot(t, store, "acc1")

	// body 用于本次流程后续判断的请求体
	body := `{"value":"unb=123; _m_h5_tk=newtoken_2;"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}
	// d、err 用于本次流程后续判断的d、err
	d, err := store.Cookies.GetDetails(ctx, "acc1")
	if err != nil {
		t.Fatalf("GetDetails: %v", err)
	}
	if d.LoginMethod != "" || d.LastLoginAt != 0 {
		t.Fatalf("普通 Cookie 更新不应刷新登录审计字段: method=%q last=%d", d.LoginMethod, d.LastLoginAt)
	}
	// token、err 用于本次流程后续判断的token、err
	token, err := store.Tokens.Get(ctx, "acc1")
	if err != nil || token.DeviceID != "permanent-device" || token.AccessToken != "" || token.ExpireAt != 0 {
		t.Fatalf("token=%+v err=%v", token, err)
	}
	requireCookieSnapshotCleared(t, store, "acc1")
}

// TestUpdateCookieRejectsStaleRevision 验证客户端携带过期凭证版本时不会覆盖较新的 Cookie。
func TestUpdateCookieRejectsStaleRevision(t *testing.T) {
	// srv、store、cleanup 保存测试服务器、数据库和资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存本次测试使用的数据库上下文。
	ctx := context.Background()
	// baselineValue 保存建立可比较版本所需的旧 Cookie 值。
	baselineValue := "unb=baseline; _m_h5_tk=baseline_token;"
	// baselineErr 表示写入可比较版本基线时的数据库错误。
	if baselineErr := store.Cookies.UpdateRenewalCookie(ctx, "acc1", baselineValue, `{}`, 100); baselineErr != nil {
		t.Fatalf("写入基线 Cookie 失败: %v", baselineErr)
	}
	// initialErr 保存读取初始账号详情时的错误。
	initial, initialErr := store.Cookies.GetDetails(ctx, "acc1")
	if initialErr != nil {
		t.Fatalf("读取初始账号详情失败: %v", initialErr)
	}
	// newerValue 是并发请求已经写入的新 Cookie 值。
	newerValue := "unb=latest; _m_h5_tk=latest_token;"
	// updateErr 表示写入并发新 Cookie 时的数据库错误。
	if updateErr := store.Cookies.UpdateRenewalCookie(ctx, "acc1", newerValue, initial.MetadataJSON, 200); updateErr != nil {
		t.Fatalf("写入并发新 Cookie 失败: %v", updateErr)
	}
	// h 保存 HTTP 路由；session 保存当前用户的会话 Cookie。
	h := srv.Router()
	// session 保存当前用户登录后的 HttpOnly 会话 Cookie。
	session := loginHelper(t, h)
	// body 保存携带旧版本的更新请求体。
	body := fmt.Sprintf(`{"value":"unb=stale; _m_h5_tk=stale_token;","last_refresh_at":%d}`, initial.LastRefreshAt)
	// req、rec 保存 HTTP 请求与响应记录器。
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1", strings.NewReader(body))
	req.AddCookie(session)
	// rec 记录过期版本请求的 HTTP 响应，供状态码与错误契约断言。
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("过期版本应返回 409，got %d body=%s", rec.Code, rec.Body.String())
	}
	// latest、latestErr 保存冲突后数据库中的凭证值和读取错误。
	latest, latestErr := store.Cookies.GetDetails(ctx, "acc1")
	if latestErr != nil {
		t.Fatalf("读取冲突后账号详情失败: %v", latestErr)
	}
	if latest.Value != newerValue {
		t.Fatalf("过期版本覆盖了新 Cookie: %q", latest.Value)
	}
}

// TestUpdateRunningCookieWakesCredentialBlockedAutomationWithoutManager 封装TestUpdateRunning登录凭证WakesCredentialBlocked自动化WithoutManager业务协调。
func TestUpdateRunningCookieWakesCredentialBlockedAutomationWithoutManager(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// dueAt 用于本次流程后续判断的dueAt
	dueAt := time.Now().UTC().Add(time.Hour).Unix()
	if // err 用于本次流程后续判断的err
	err := store.Automation.DeferTask(ctx, db.DeferredAutomationTask{
		TaskKey: "acc1:credential", CookieID: "acc1", TriggerType: automation.TriggerOrderPaid,
		TaskJSON: `{}`, DueAt: dueAt, ErrorMessage: "FAIL_SYS_SESSION_EXPIRED",
	}); err != nil {
		t.Fatal(err)
	}
	srv.Manager = nil
	srv.updateRunningCookie(ctx, "acc1", "unb=123; _m_h5_tk=fresh_1;")
	// got 用于本次流程后续判断的got
	var got int64
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT due_at FROM automation_pending_tasks WHERE task_key=?`, "acc1:credential").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Fatalf("Cookie 更新后凭证失败任务 due_at=%d want 0", got)
	}
}

// TestUpdateCookieQRLoginEnablesAccount 封装TestUpdate登录凭证QR登录Enables账号业务协调。
func TestUpdateCookieQRLoginEnablesAccount(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	err := store.Cookies.SetStatusWithReason(ctx, "acc1", false, "token 失效"); err != nil {
		t.Fatalf("SetStatusWithReason: %v", err)
	}
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"value":"unb=123; _m_h5_tk=qr_2;","login_method":"qr_scan"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !store.Cookies.GetStatus(ctx, "acc1") {
		t.Fatal("扫码登录成功后应重新启用账号")
	}
	// reason 用于本次流程后续判断的原因
	var reason string
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT disable_reason FROM cookie_status WHERE cookie_id='acc1'`).Scan(&reason); err != nil {
		t.Fatalf("query disable_reason: %v", err)
	}
	if reason != "" {
		t.Fatalf("扫码登录成功后应清空禁用原因，got %q", reason)
	}
	// d、err 用于本次流程后续判断的d、err
	d, err := store.Cookies.GetDetails(ctx, "acc1")
	if err != nil {
		t.Fatalf("GetDetails: %v", err)
	}
	if d.LoginMethod != "qr_scan" || d.LastLoginAt == 0 {
		t.Fatalf("扫码登录应刷新登录审计字段: %+v", d)
	}
}

// TestSetCookieStatusRecordsManualDisableReason 封装TestSet登录凭证状态RecordsManualDisable原因业务协调。
func TestSetCookieStatusRecordsManualDisableReason(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/status", strings.NewReader(`{"enabled":false}`))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// enabled 用于本次流程后续判断的启用状态
	var enabled int
	// reason 用于本次流程后续判断的原因
	var reason string
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRow(`SELECT enabled,disable_reason FROM cookie_status WHERE cookie_id='acc1'`).Scan(&enabled, &reason); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 || reason != db.DisableReasonManual {
		t.Fatalf("enabled=%d reason=%q", enabled, reason)
	}
}

// TestSetCookieStatusWaitsForCredentialTransition 封装TestSet登录凭证状态WaitsForCredentialTransition业务协调。
func TestSetCookieStatusWaitsForCredentialTransition(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// credentialUnlock 用于本次流程后续判断的credentialUnlock
	credentialUnlock := store.LockAccountCredentials("acc1")
	// done 用于本次流程后续判断的done
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		// req 用于本次流程后续判断的req
		req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/status", strings.NewReader(`{"enabled":false}`))
		req.AddCookie(cookie)
		// rec 用于本次流程后续判断的rec
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		done <- rec
	}()
	select {
	case // rec 用于本次流程后续判断的rec
	rec := <-done:
		credentialUnlock()
		t.Fatalf("状态更新绕过了账号凭证锁: status=%d body=%s", rec.Code, rec.Body.String())
	case <-time.After(50 * time.Millisecond):
	}
	credentialUnlock()
	select {
	case // rec 用于本次流程后续判断的rec
	rec := <-done:
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("释放凭证锁后状态更新未完成")
	}
}

// TestDeleteCookieRechecksOwnershipInsideCredentialLock 封装TestDelete登录凭证RechecksOwnershipInsideCredential锁业务协调。
func TestDeleteCookieRechecksOwnershipInsideCredentialLock(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // ok、err 用于本次流程后续判断的ok、err
	ok, err := store.Users.Create(ctx, "member-delete", "member-delete@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create replacement owner: ok=%v err=%v", ok, err)
	}
	// replacementOwner、err 用于本次流程后续判断的replacementOwner、err
	replacementOwner, err := store.Users.GetByUsername(ctx, "member-delete")
	if err != nil {
		t.Fatal(err)
	}
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// credentialUnlock 用于本次流程后续判断的credentialUnlock
	credentialUnlock := store.LockAccountCredentials("acc1")
	// done 用于本次流程后续判断的done
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		// req 用于本次流程后续判断的req
		req := httptest.NewRequest(http.MethodDelete, "/cookies/acc1", nil)
		req.AddCookie(cookie)
		// rec 用于本次流程后续判断的rec
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		done <- rec
	}()
	select {
	case // rec 用于本次流程后续判断的rec
	rec := <-done:
		credentialUnlock()
		t.Fatalf("删除绕过了账号凭证锁: status=%d body=%s", rec.Code, rec.Body.String())
	case <-time.After(50 * time.Millisecond):
	}
	if // err 用于本次流程后续判断的err
	err := store.Cookies.Delete(ctx, "acc1"); err != nil {
		credentialUnlock()
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Cookies.CreateOwned(ctx, "acc1", "unb=replacement; _m_h5_tk=fresh", replacementOwner.ID); err != nil {
		credentialUnlock()
		t.Fatal(err)
	}
	credentialUnlock()
	// rec 用于本次流程后续判断的rec
	rec := <-done
	if rec.Code == http.StatusOK {
		t.Fatalf("旧 owner 的并发请求不得删除新 owner 账号: body=%s", rec.Body.String())
	}
	// detail、err 用于本次流程后续判断的detail、err
	detail, err := store.Cookies.GetDetails(ctx, "acc1")
	if err != nil || detail.UserID != replacementOwner.ID {
		t.Fatalf("替换后的账号被误删: detail=%+v err=%v", detail, err)
	}
}

// TestUpdateCookieBadJSON 非法 JSON 400。
func TestUpdateCookieBadJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestUpdateCookieLoginInfo 更新登录信息。
func TestUpdateCookieLoginInfo(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"username":"u1","password":"p1","show_browser":true}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/login-info", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// d、err 用于本次流程后续判断的d、err
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
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/login-info", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetCookieStatus 启停账号。
func TestSetCookieStatus(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// 先设置账号为停用，便于测试启用路径。
	store.Cookies.SetStatus(ctx, "acc1", false)
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// 启用。
	body := `{"enabled":true}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/status", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
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
	// req2 用于本次流程后续判断的req2
	req2 := httptest.NewRequest(http.MethodPut, "/cookies/acc1/status", strings.NewReader(body2))
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
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
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/status", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetCookieAutoConfirmBadJSON 非法 JSON 400。
func TestSetCookieAutoConfirmBadJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/auto-confirm", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetCookieRemarkBadJSON 非法 JSON 400。
func TestSetCookieRemarkBadJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/cookies/acc1/remark", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestDeleteCookie 删除账号。
func TestDeleteCookie(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	store.Cookies.Save(ctx, "acc-del", "unb=1; _m_h5_tk=t_1;", 1)
	store.Cookies.Save(ctx, "acc-keep", "unb=2; _m_h5_tk=t_2;", 1)
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodDelete, "/cookies/acc-del", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("delete status=%d", rec.Code)
	}
	// 删除后的运行时停止必须登记到 Server 生命周期，等待其完成后再验证清理结果。
	srv.WaitForBackground()
	if // err 用于本次流程后续判断的err
	_, err := store.Cookies.GetDetails(ctx, "acc-del"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("目标账号应被删除，err=%v", err)
	}
	if // kept、err 用于本次流程后续判断的kept、err
	kept, err := store.Cookies.GetDetails(ctx, "acc-keep"); err != nil || kept.ID != "acc-keep" {
		t.Fatalf("非目标账号不应被删除，kept=%+v err=%v", kept, err)
	}
}

// TestAddCookieBad 缺 id 或 value 400。
func TestAddCookieBad(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"id":"acc2"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/cookies", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 value 应 400，got %d", rec.Code)
	}
}

// TestAddCookieDefaultsManualLoginAudit 封装TestAdd登录凭证DefaultsManual登录Audit业务协调。
func TestAddCookieDefaultsManualLoginAudit(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"id":"acc-manual","value":"unb=456; _m_h5_tk=manual_1;"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/cookies", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("add status=%d body=%s", rec.Code, rec.Body.String())
	}
	// d、err 用于本次流程后续判断的d、err
	d, err := store.Cookies.GetDetails(ctx, "acc-manual")
	if err != nil {
		t.Fatalf("GetDetails: %v", err)
	}
	if d.LoginMethod != "manual" || d.LastLoginAt == 0 {
		t.Fatalf("手动新增 Cookie 应记录 manual 登录审计字段: %+v", d)
	}
	// logs、err 用于本次流程后续判断的logs、err
	logs, err := store.LoginLogs.ListByCookie(ctx, "acc-manual", 10)
	if err != nil || len(logs) != 1 || logs[0].Method != "manual" || logs[0].TriggerReason != "手动Cookie录入" {
		t.Fatalf("手动新增 Cookie 应记录登录日志: logs=%#v err=%v", logs, err)
	}
}

// TestAddCookieBadJSON 非法 JSON 400。
func TestAddCookieBadJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/cookies", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestGetCookieAutoConfirmNotFound 不存在账号 404。
func TestGetCookieAutoConfirmNotFound(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/cookies/no-such/auto-confirm", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在账号应 404，got %d", rec.Code)
	}
}

// TestCachedAccountNickname 备注优先于昵称。
func TestCachedAccountNickname(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := []struct {
		remark, nickname, id, want string
	}{
		{"我的备注", "昵称", "acc1", "我的备注"},
		{"", "昵称", "acc1", "昵称"},
		{"", "", "acc1234567890", "账号 acc123"},
	}
	// c 表示当前遍历过程中的c
	for _, c := range cases {
		// got 用于本次流程后续判断的got
		got := cachedAccountNickname(&db.CookieDetail{ID: c.id, Nickname: c.nickname, Remark: c.remark})
		if got != c.want {
			t.Errorf("cachedAccountNickname(remark=%q,nick=%q,id=%q)=%q want %q", c.remark, c.nickname, c.id, got, c.want)
		}
		// summaryGot 保存非敏感应用摘要生成的展示名，验证摘要边界与兼容模型保持同样的日志语义。
		summaryGot := cachedAccountSummaryNickname(accountapp.AccountSummary{ID: c.id, Nickname: c.nickname, Remark: c.remark})
		if summaryGot != c.want {
			t.Errorf("cachedAccountSummaryNickname(remark=%q,nick=%q,id=%q)=%q want %q", c.remark, c.nickname, c.id, summaryGot, c.want)
		}
	}
}

// TestNormalizeProfileAvatarURL 头像 URL 归一。
func TestNormalizeProfileAvatarURL(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := map[string]string{
		"":                                 "",
		"//img.alicdn.com/x.jpg":           "https://img.alicdn.com/x.jpg",
		"http://img.alicdn.com/x.jpg":      "https://img.alicdn.com/x.jpg",
		"https://img.alicdn.com/x.jpg":     "https://img.alicdn.com/x.jpg",
		"  https://img.alicdn.com/x.jpg  ": "https://img.alicdn.com/x.jpg",
	}
	// in、want 表示当前遍历过程中的in、want
	for in, want := range cases {
		if // got 用于本次流程后续判断的got
		got := normalizeProfileAvatarURL(in); got != want {
			t.Errorf("normalizeProfileAvatarURL(%q)=%q want %q", in, got, want)
		}
	}
}

// TestTruncate truncate 不超长则原样返回。
func TestTruncate(t *testing.T) {
	if // got 用于本次流程后续判断的got
	got := truncate("abc", 5); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if // got 用于本次流程后续判断的got
	got := truncate("abcdef", 3); got != "abc" {
		t.Fatalf("got %q", got)
	}
}
