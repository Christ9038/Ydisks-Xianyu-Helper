package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/logging"
)

// TestListAIModels 通过 mock OpenAI 端点返回模型列表。
func TestListAIModels(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// 注入一个本地 HTTP server 作为 ai_api_url。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen-plus"},{"id":"qwen-max"}]}`))
	}))
	defer ts.Close()

	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"base_url":"` + ts.URL + `","api_key":"sk-test"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/ai-models", strings.NewReader(body))
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
	// models 用于本次流程后续判断的模型列表
	models, _ := res["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("应2个模型，got %+v", res)
	}
	// admin、adminErr 保存当前登录管理员，用于验证 AI 模型请求已记录敏感密钥使用审计。
	admin, adminErr := store.Users.GetByUsername(context.Background(), "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// auditRecords、auditErr 保存 AI 模型请求产生的审计记录及查询错误。
	auditRecords, auditErr := store.SecurityAudit.ListByUser(context.Background(), admin.ID, 10)
	if auditErr != nil || len(auditRecords) != 1 {
		t.Fatalf("AI 模型请求审计记录异常: records=%+v err=%v", auditRecords, auditErr)
	}
	if auditRecords[0].Action != "settings.use" || auditRecords[0].Resource != "ai_models" || len(auditRecords[0].Keys) != 1 || auditRecords[0].Keys[0] != "ai_api_key" {
		t.Fatalf("AI 模型请求审计上下文异常: %+v", auditRecords[0])
	}
}

// TestReadOpenAIModelsBodyRejectsOversizedResponse 封装TestReadOpenAI模型列表请求体RejectsOversized响应业务协调。
func TestReadOpenAIModelsBodyRejectsOversizedResponse(t *testing.T) {
	// err 用于本次流程后续判断的err
	_, err := readOpenAIModelsBody(strings.NewReader(strings.Repeat("x", maxOpenAIModelsResponseBytes+1)))
	if err == nil {
		t.Fatal("oversized models response should fail")
	}
}

// TestListAIModelsBadJSON 非法 JSON 400。
func TestListAIModelsBadJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/ai-models", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestListAIModelsEmptyBaseURL 空地址使用默认并失败（默认阿里云地址不可达或返回非 2xx）。
func TestListAIModelsEmptyBaseURL(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// 注入一个返回错误状态码的本地 server。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	// 设置系统设置 ai_api_url 指向该 server。
	store.Settings.Set(context.Background(), "ai_api_url", ts.URL)

	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/ai-models", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("模型拉取失败应 502，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestSetSettingBadJSON 非法 JSON 400。
func TestSetSettingBadJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/system-settings/theme_color", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetLogLevelValidatesAndAppliesRuntimeLevel 封装TestSetLogLevelValidatesAndAppliesRuntimeLevel业务协调。
func TestSetLogLevelValidatesAndAppliesRuntimeLevel(t *testing.T) {
	defer logging.Level.Set(slog.LevelInfo)

	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// badReq 用于本次流程后续判断的badReq
	badReq := httptest.NewRequest(http.MethodPut, "/system-settings/log_level", strings.NewReader(`{"value":"verbose"}`))
	badReq.AddCookie(cookie)
	// badRec 用于本次流程后续判断的badRec
	badRec := httptest.NewRecorder()
	h.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid log level should be 400, got %d body=%s", badRec.Code, badRec.Body.String())
	}

	// goodReq 用于本次流程后续判断的goodReq
	goodReq := httptest.NewRequest(http.MethodPut, "/system-settings/log_level", strings.NewReader(`{"value":"debug"}`))
	goodReq.AddCookie(cookie)
	// goodRec 用于本次流程后续判断的goodRec
	goodRec := httptest.NewRecorder()
	h.ServeHTTP(goodRec, goodReq)
	if goodRec.Code != http.StatusOK {
		t.Fatalf("valid log level should be 200, got %d body=%s", goodRec.Code, goodRec.Body.String())
	}
	if // got 用于本次流程后续判断的got
	got := logging.Level.Level(); got != slog.LevelDebug {
		t.Fatalf("runtime log level=%v want debug", got)
	}
	// saved、err 用于本次流程后续判断的saved、err
	saved, err := store.Settings.Get(context.Background(), "log_level")
	if err != nil || saved != "debug" {
		t.Fatalf("saved log_level=%q err=%v", saved, err)
	}
}

// TestSystemSettingsRequireAdmin 封装Test系统设置RequireAdmin业务协调。
func TestSystemSettingsRequireAdmin(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	if // err 用于本次流程后续判断的err
	_, err := store.Users.Create(context.Background(), "user-settings", "user-settings@example.com", "pw"); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// loginReq 用于本次流程后续判断的登录Req
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"user-settings","password":"pw"}`))
	// loginRec 用于本次流程后续判断的登录Rec
	loginRec := httptest.NewRecorder()
	h.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK || len(loginRec.Result().Cookies()) == 0 {
		t.Fatalf("login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginRec.Result().Cookies()[0]

	// cases 用于本次流程后续判断的cases
	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/system-settings", ""},
		{http.MethodPut, "/system-settings/theme_color", `{"value":"red"}`},
		{http.MethodPost, "/ai-models", `{"base_url":"http://127.0.0.1","api_key":"sk"}`},
	}
	// tc 表示当前遍历过程中的tc
	for _, tc := range cases {
		// req 用于本次流程后续判断的req
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.AddCookie(cookie)
		// rec 用于本次流程后续判断的rec
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s should be 403, got %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

// TestBulkSystemSettingsAreAtomic 封装TestBulk系统设置AreAtomic业务协调。
func TestBulkSystemSettingsAreAtomic(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// badReq 用于本次流程后续判断的badReq
	badReq := httptest.NewRequest(http.MethodPut, "/system-settings", strings.NewReader(`{"theme_color":"red","log_level":"verbose"}`))
	badReq.AddCookie(cookie)
	// badRec 用于本次流程后续判断的badRec
	badRec := httptest.NewRecorder()
	h.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", badRec.Code, badRec.Body.String())
	}
	if // value 用于本次流程后续判断的值
	value, _ := store.Settings.Get(context.Background(), "theme_color"); value == "red" {
		t.Fatal("invalid bulk request partially saved theme_color")
	}

	// goodReq 用于本次流程后续判断的goodReq
	goodReq := httptest.NewRequest(http.MethodPut, "/system-settings", strings.NewReader(`{"theme_color":"blue","renewal_log_retention_days":15}`))
	goodReq.AddCookie(cookie)
	// goodRec 用于本次流程后续判断的goodRec
	goodRec := httptest.NewRecorder()
	h.ServeHTTP(goodRec, goodReq)
	if goodRec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", goodRec.Code, goodRec.Body.String())
	}
	if // value 用于本次流程后续判断的值
	value, _ := store.Settings.Get(context.Background(), "theme_color"); value != "blue" {
		t.Fatalf("theme_color=%q", value)
	}
	if // value 用于本次流程后续判断的值
	value, _ := store.Settings.Get(context.Background(), "renewal_log_retention_days"); value != "15" {
		t.Fatalf("retention=%q", value)
	}
}

// TestListUserSettings 用户设置增删查。
func TestListUserSettings(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// 设。
	body := `{"value":"dark"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/user-settings/theme", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("set status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 查全部。
	req2 := httptest.NewRequest(http.MethodGet, "/user-settings", nil)
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("list status=%d", rec2.Code)
	}
	// m 用于本次流程后续判断的m
	var m map[string]string
	json.Unmarshal(rec2.Body.Bytes(), &m)
	if m["theme"] != "dark" {
		t.Fatalf("设置未生效: %+v", m)
	}

	// 查单。
	req3 := httptest.NewRequest(http.MethodGet, "/user-settings/theme", nil)
	req3.AddCookie(cookie)
	// rec3 用于本次流程后续判断的rec3
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	// one 用于本次流程后续判断的one
	var one map[string]any
	json.Unmarshal(rec3.Body.Bytes(), &one)
	if one["value"] != "dark" {
		t.Fatalf("查单异常: %+v", one)
	}

	// 查不存在的 key。
	req4 := httptest.NewRequest(http.MethodGet, "/user-settings/no-such-key", nil)
	req4.AddCookie(cookie)
	// rec4 用于本次流程后续判断的rec4
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	// miss 用于本次流程后续判断的miss
	var miss map[string]any
	json.Unmarshal(rec4.Body.Bytes(), &miss)
	if miss["value"] != "" {
		t.Fatalf("不存在 key 应返回空: %+v", miss)
	}
}

// TestSetUserSettingBadJSON 非法 JSON 400。
func TestSetUserSettingBadJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/user-settings/theme", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetAIReplyBadJSON 非法 JSON 400。
func TestSetAIReplyBadJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/ai-reply-settings/acc1", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestGetAIReplyMissingAccountIsNotFound 封装TestGetAI回复Missing账号IsNotFound业务协调。
func TestGetAIReplyMissingAccountIsNotFound(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/ai-reply-settings/no-such", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在账号应 404，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestGetAIReplyExistingAccountWithoutConfigReturnsDefault 封装TestGetAI回复Existing账号Without配置ReturnsDefault业务协调。
func TestGetAIReplyExistingAccountWithoutConfigReturnsDefault(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/ai-reply-settings/acc1", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 用于本次流程后续判断的响应
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["ai_enabled"] != false || res["max_discount_percent"] != float64(10) {
		t.Fatalf("默认值异常: %+v", res)
	}
}

// TestAIReplySettingsAreUserScoped 封装TestAI回复设置Are用户Scoped业务协调。
func TestAIReplySettingsAreUserScoped(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	_, err := store.Users.Create(ctx, "user2", "u2@e.com", "pw"); err != nil {
		t.Fatalf("create user2: %v", err)
	}
	// user2、err 用于本次流程后续判断的user2、err
	user2, err := store.Users.GetByUsername(ctx, "user2")
	if err != nil {
		t.Fatalf("get user2: %v", err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Cookies.Save(ctx, "other-acc", "unb=456; _m_h5_tk=tk2_1;", user2.ID); err != nil {
		t.Fatalf("save other cookie: %v", err)
	}
	if // err 用于本次流程后续判断的err
	_, err := store.DB.ExecContext(ctx,
		`INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('other-acc', 1, 'secret')`); err != nil {
		t.Fatalf("insert ai settings: %v", err)
	}

	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// listReq 用于本次流程后续判断的listReq
	listReq := httptest.NewRequest(http.MethodGet, "/ai-reply-settings", nil)
	listReq.AddCookie(cookie)
	// listRec 用于本次流程后续判断的listRec
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	// listed 用于本次流程后续判断的listed
	var listed map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if // ok 用于本次流程后续判断的ok
	_, ok := listed["other-acc"]; ok {
		t.Fatalf("list leaked other user's AI settings: %+v", listed)
	}

	// getReq 用于本次流程后续判断的getReq
	getReq := httptest.NewRequest(http.MethodGet, "/ai-reply-settings/other-acc", nil)
	getReq.AddCookie(cookie)
	// getRec 用于本次流程后续判断的getRec
	getRec := httptest.NewRecorder()
	h.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusForbidden {
		t.Fatalf("cross-user get should be 403, got %d body=%s", getRec.Code, getRec.Body.String())
	}

	// setReq 用于本次流程后续判断的setReq
	setReq := httptest.NewRequest(http.MethodPut, "/ai-reply-settings/other-acc", strings.NewReader(
		`{"ai_enabled":true,"max_discount_percent":20,"max_discount_amount":200,"max_bargain_rounds":5,"custom_prompts":"override"}`,
	))
	setReq.AddCookie(cookie)
	// setRec 用于本次流程后续判断的setRec
	setRec := httptest.NewRecorder()
	h.ServeHTTP(setRec, setReq)
	if setRec.Code != http.StatusForbidden {
		t.Fatalf("cross-user set should be 403, got %d body=%s", setRec.Code, setRec.Body.String())
	}
}

// TestFetchOpenAIModelsEmptyBaseURL 空地址错误。
func TestFetchOpenAIModelsEmptyBaseURL(t *testing.T) {
	// err 用于本次流程后续判断的err
	_, err := fetchOpenAIModels(context.Background(), "", "")
	if err == nil {
		t.Fatal("空地址应报错")
	}
}

// TestSystemSettingsEndpointRedactsSensitiveValues 验证管理员设置接口不会返回敏感配置明文。
func TestSystemSettingsEndpointRedactsSensitiveValues(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "server-redacted-test-key")
	// srv、store、cleanup 是测试服务、数据库及其清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 是当前测试使用的请求上下文。
	ctx := context.Background()
	// err 是写入脱敏测试秘密时返回的错误。
	if err := store.Settings.SetMany(ctx, map[string]string{
		"ai_api_key":                "sk-server-secret",
		"smtp_password":             "smtp-server-secret",
		"captcha.remote_secret_key": "captcha-server-secret",
	}); err != nil {
		t.Fatal(err)
	}
	// h 是当前测试服务的 HTTP 路由器。
	h := srv.Router()
	// cookie 是管理员登录后得到的会话 Cookie。
	cookie := loginHelper(t, h)
	// req 是读取管理员系统设置的 HTTP 请求。
	req := httptest.NewRequest(http.MethodGet, "/system-settings", nil)
	req.AddCookie(cookie)
	// rec 是捕获设置响应的 HTTP 记录器。
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings status=%d body=%s", rec.Code, rec.Body.String())
	}
	// response 是管理员系统设置的脱敏响应。
	var response map[string]string
	// err 是解析设置响应时返回的 JSON 错误。
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"sk-server-secret", "smtp-server-secret", "captcha-server-secret"} { // secret 是不应出现在响应中的敏感明文。
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("settings response leaked secret %q: %s", secret, rec.Body.String())
		}
	}
	for _, key := range []string{"ai_api_key", "smtp_password", "captcha.remote_secret_key"} { // key 是待验证的敏感配置键。
		// ok 表示脱敏响应是否意外包含敏感键。
		if _, ok := response[key]; ok {
			t.Fatalf("settings response contains sensitive key %q: %#v", key, response)
		}
		if response[key+"_configured"] != "true" {
			t.Fatalf("settings response misses configured marker %q: %#v", key, response)
		}
	}
	// admin、adminErr 保存管理员用户及其查询错误。
	admin, adminErr := store.Users.GetAdmin(ctx)
	if adminErr != nil || admin == nil {
		t.Fatalf("读取管理员失败: admin=%+v err=%v", admin, adminErr)
	}
	// auditRecords、auditErr 保存管理员读取敏感配置产生的审计记录。
	auditRecords, auditErr := store.SecurityAudit.ListByUser(ctx, admin.ID, 10)
	if auditErr != nil || len(auditRecords) == 0 || auditRecords[0].Action != "settings.read" {
		t.Fatalf("敏感设置读取未生成审计记录: records=%+v err=%v", auditRecords, auditErr)
	}
}

// TestSensitiveSettingsAccessRequiresAuditStorage 验证审计存储不可用时敏感读取会拒绝继续。
func TestSensitiveSettingsAccessRequiresAuditStorage(t *testing.T) {
	// srv、store、cleanup 是测试服务、数据库及其清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 是当前测试服务的 HTTP 路由器。
	h := srv.Router()
	// cookie 是管理员登录后得到的会话 Cookie。
	cookie := loginHelper(t, h)
	store.SecurityAudit = nil
	// req 是在审计存储不可用时读取系统设置的请求。
	req := httptest.NewRequest(http.MethodGet, "/system-settings", nil)
	req.AddCookie(cookie)
	// rec 是捕获拒绝响应的 HTTP 记录器。
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("审计存储不可用时应拒绝敏感读取，status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestSystemSettingsEndpointUsesExplicitSecretCommands 验证 HTTP 设置更新不会把秘密混入普通字段。
func TestSystemSettingsEndpointUsesExplicitSecretCommands(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "server-secret-command-key")
	// srv、store、cleanup 是测试服务、数据库及其清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 是当前测试使用的请求上下文。
	ctx := context.Background()
	// err 是初始敏感设置写入错误。
	if err := store.Settings.Set(ctx, "ai_api_key", "before"); err != nil {
		t.Fatal(err)
	}
	// h 是当前测试服务的 HTTP 路由器。
	h := srv.Router()
	// cookie 是管理员登录后得到的会话 Cookie。
	cookie := loginHelper(t, h)
	// replaceReq 是提交敏感替换命令的请求。
	replaceReq := httptest.NewRequest(http.MethodPut, "/system-settings", strings.NewReader(`{"values":{"theme_color":"blue"},"secrets":{"ai_api_key":{"action":"replace","value":"after"}}}`))
	replaceReq.AddCookie(cookie)
	// replaceRec 是替换命令的 HTTP 响应记录器。
	replaceRec := httptest.NewRecorder()
	h.ServeHTTP(replaceRec, replaceReq)
	if replaceRec.Code != http.StatusOK {
		t.Fatalf("replace status=%d body=%s", replaceRec.Code, replaceRec.Body.String())
	}
	// value、err 是替换后的秘密读取结果及错误。
	if value, err := store.Settings.Get(ctx, "ai_api_key"); err != nil || value != "after" {
		t.Fatalf("replace value=%q err=%v", value, err)
	}
	// clearReq 是提交敏感清除命令的请求。
	clearReq := httptest.NewRequest(http.MethodPut, "/system-settings", strings.NewReader(`{"secrets":{"ai_api_key":{"action":"clear"}}}`))
	clearReq.AddCookie(cookie)
	// clearRec 是清除命令的 HTTP 响应记录器。
	clearRec := httptest.NewRecorder()
	h.ServeHTTP(clearRec, clearReq)
	if clearRec.Code != http.StatusOK {
		t.Fatalf("clear status=%d body=%s", clearRec.Code, clearRec.Body.String())
	}
	// value、err 是清除后的秘密读取结果及错误。
	if value, err := store.Settings.Get(ctx, "ai_api_key"); err != nil || value != "" {
		t.Fatalf("clear value=%q err=%v", value, err)
	}
	// admin、adminErr 保存管理员用户及其查询错误。
	admin, adminErr := store.Users.GetAdmin(ctx)
	if adminErr != nil || admin == nil {
		t.Fatalf("读取管理员失败: admin=%+v err=%v", admin, adminErr)
	}
	// auditRecords、auditErr 保存敏感设置替换和清除产生的审计记录。
	auditRecords, auditErr := store.SecurityAudit.ListByUser(ctx, admin.ID, 10)
	if auditErr != nil || len(auditRecords) < 2 {
		t.Fatalf("敏感设置写入未生成足够审计记录: records=%+v err=%v", auditRecords, auditErr)
	}
	// forbiddenReq 是尝试把敏感值放进普通 values 的请求。
	forbiddenReq := httptest.NewRequest(http.MethodPut, "/system-settings", strings.NewReader(`{"values":{"ai_api_key":"leak"}}`))
	forbiddenReq.AddCookie(cookie)
	// forbiddenRec 是拒绝普通敏感字段的 HTTP 响应记录器。
	forbiddenRec := httptest.NewRecorder()
	h.ServeHTTP(forbiddenRec, forbiddenReq)
	if forbiddenRec.Code != http.StatusBadRequest {
		t.Fatalf("forbidden status=%d body=%s", forbiddenRec.Code, forbiddenRec.Body.String())
	}
	// legacyForbiddenReq 是旧版顶层敏感字段请求，必须同样被拒绝。
	legacyForbiddenReq := httptest.NewRequest(http.MethodPut, "/system-settings", strings.NewReader(`{"ai_api_key":"legacy-leak"}`))
	legacyForbiddenReq.AddCookie(cookie)
	// legacyForbiddenRec 是旧版顶层敏感字段的 HTTP 响应记录器。
	legacyForbiddenRec := httptest.NewRecorder()
	h.ServeHTTP(legacyForbiddenRec, legacyForbiddenReq)
	if legacyForbiddenRec.Code != http.StatusBadRequest {
		t.Fatalf("legacy forbidden status=%d body=%s", legacyForbiddenRec.Code, legacyForbiddenRec.Body.String())
	}
}
