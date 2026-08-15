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
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// 注入一个本地 HTTP server 作为 ai_api_url。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen-plus"},{"id":"qwen-max"}]}`))
	}))
	defer ts.Close()

	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"base_url":"` + ts.URL + `","api_key":"sk-test"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/ai-models", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 保存响应，供当前处理流程使用
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	// models 保存模型列表，供当前处理流程使用
	models, _ := res["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("应2个模型，got %+v", res)
	}
}

// TestReadOpenAIModelsBodyRejectsOversizedResponse 负责TestReadOpenAI模型列表请求体RejectsOversized响应相关处理。
func TestReadOpenAIModelsBodyRejectsOversizedResponse(t *testing.T) {
	// err 保存err，供当前处理流程使用
	_, err := readOpenAIModelsBody(strings.NewReader(strings.Repeat("x", maxOpenAIModelsResponseBytes+1)))
	if err == nil {
		t.Fatal("oversized models response should fail")
	}
}

// TestListAIModelsBadJSON 非法 JSON 400。
func TestListAIModelsBadJSON(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/ai-models", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestListAIModelsEmptyBaseURL 空地址使用默认并失败（默认阿里云地址不可达或返回非 2xx）。
func TestListAIModelsEmptyBaseURL(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// 注入一个返回错误状态码的本地 server。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	// 设置系统设置 ai_api_url 指向该 server。
	srv.Store.Settings.Set(context.Background(), "ai_api_url", ts.URL)

	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/ai-models", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("模型拉取失败应 502，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestSetSettingBadJSON 非法 JSON 400。
func TestSetSettingBadJSON(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/system-settings/theme_color", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetLogLevelValidatesAndAppliesRuntimeLevel 负责TestSetLogLevelValidatesAndAppliesRuntimeLevel相关处理。
func TestSetLogLevelValidatesAndAppliesRuntimeLevel(t *testing.T) {
	defer logging.Level.Set(slog.LevelInfo)

	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// badReq 保存badReq，供当前处理流程使用
	badReq := httptest.NewRequest(http.MethodPut, "/system-settings/log_level", strings.NewReader(`{"value":"verbose"}`))
	badReq.AddCookie(cookie)
	// badRec 保存badRec，供当前处理流程使用
	badRec := httptest.NewRecorder()
	h.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid log level should be 400, got %d body=%s", badRec.Code, badRec.Body.String())
	}

	// goodReq 保存goodReq，供当前处理流程使用
	goodReq := httptest.NewRequest(http.MethodPut, "/system-settings/log_level", strings.NewReader(`{"value":"debug"}`))
	goodReq.AddCookie(cookie)
	// goodRec 保存goodRec，供当前处理流程使用
	goodRec := httptest.NewRecorder()
	h.ServeHTTP(goodRec, goodReq)
	if goodRec.Code != http.StatusOK {
		t.Fatalf("valid log level should be 200, got %d body=%s", goodRec.Code, goodRec.Body.String())
	}
	if // got 保存got，供当前处理流程使用
	got := logging.Level.Level(); got != slog.LevelDebug {
		t.Fatalf("runtime log level=%v want debug", got)
	}
	// saved、err 保存saved、err，供当前处理流程使用
	saved, err := srv.Store.Settings.Get(context.Background(), "log_level")
	if err != nil || saved != "debug" {
		t.Fatalf("saved log_level=%q err=%v", saved, err)
	}
}

// TestSystemSettingsRequireAdmin 负责Test系统设置RequireAdmin相关处理。
func TestSystemSettingsRequireAdmin(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	if // err 保存err，供当前处理流程使用
	_, err := srv.Store.Users.Create(context.Background(), "user-settings", "user-settings@example.com", "pw"); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// loginReq 保存登录Req，供当前处理流程使用
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"user-settings","password":"pw"}`))
	// loginRec 保存登录Rec，供当前处理流程使用
	loginRec := httptest.NewRecorder()
	h.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK || len(loginRec.Result().Cookies()) == 0 {
		t.Fatalf("login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginRec.Result().Cookies()[0]

	// cases 保存cases，供当前处理流程使用
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
		// req 保存req，供当前处理流程使用
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.AddCookie(cookie)
		// rec 保存rec，供当前处理流程使用
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s should be 403, got %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

// TestBulkSystemSettingsAreAtomic 负责TestBulk系统设置AreAtomic相关处理。
func TestBulkSystemSettingsAreAtomic(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// badReq 保存badReq，供当前处理流程使用
	badReq := httptest.NewRequest(http.MethodPut, "/system-settings", strings.NewReader(`{"theme_color":"red","log_level":"verbose"}`))
	badReq.AddCookie(cookie)
	// badRec 保存badRec，供当前处理流程使用
	badRec := httptest.NewRecorder()
	h.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", badRec.Code, badRec.Body.String())
	}
	if // value 保存值，供当前处理流程使用
	value, _ := srv.Store.Settings.Get(context.Background(), "theme_color"); value == "red" {
		t.Fatal("invalid bulk request partially saved theme_color")
	}

	// goodReq 保存goodReq，供当前处理流程使用
	goodReq := httptest.NewRequest(http.MethodPut, "/system-settings", strings.NewReader(`{"theme_color":"blue","renewal_log_retention_days":15}`))
	goodReq.AddCookie(cookie)
	// goodRec 保存goodRec，供当前处理流程使用
	goodRec := httptest.NewRecorder()
	h.ServeHTTP(goodRec, goodReq)
	if goodRec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", goodRec.Code, goodRec.Body.String())
	}
	if // value 保存值，供当前处理流程使用
	value, _ := srv.Store.Settings.Get(context.Background(), "theme_color"); value != "blue" {
		t.Fatalf("theme_color=%q", value)
	}
	if // value 保存值，供当前处理流程使用
	value, _ := srv.Store.Settings.Get(context.Background(), "renewal_log_retention_days"); value != "15" {
		t.Fatalf("retention=%q", value)
	}
}

// TestListUserSettings 用户设置增删查。
func TestListUserSettings(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// 设。
	body := `{"value":"dark"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/user-settings/theme", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("set status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 查全部。
	req2 := httptest.NewRequest(http.MethodGet, "/user-settings", nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("list status=%d", rec2.Code)
	}
	// m 保存m，供当前处理流程使用
	var m map[string]string
	json.Unmarshal(rec2.Body.Bytes(), &m)
	if m["theme"] != "dark" {
		t.Fatalf("设置未生效: %+v", m)
	}

	// 查单。
	req3 := httptest.NewRequest(http.MethodGet, "/user-settings/theme", nil)
	req3.AddCookie(cookie)
	// rec3 保存rec3，供当前处理流程使用
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	// one 保存one，供当前处理流程使用
	var one map[string]any
	json.Unmarshal(rec3.Body.Bytes(), &one)
	if one["value"] != "dark" {
		t.Fatalf("查单异常: %+v", one)
	}

	// 查不存在的 key。
	req4 := httptest.NewRequest(http.MethodGet, "/user-settings/no-such-key", nil)
	req4.AddCookie(cookie)
	// rec4 保存rec4，供当前处理流程使用
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	// miss 保存miss，供当前处理流程使用
	var miss map[string]any
	json.Unmarshal(rec4.Body.Bytes(), &miss)
	if miss["value"] != "" {
		t.Fatalf("不存在 key 应返回空: %+v", miss)
	}
}

// TestSetUserSettingBadJSON 非法 JSON 400。
func TestSetUserSettingBadJSON(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/user-settings/theme", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetAIReplyBadJSON 非法 JSON 400。
func TestSetAIReplyBadJSON(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/ai-reply-settings/acc1", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestGetAIReplyMissingAccountIsNotFound 负责TestGetAI回复Missing账号IsNotFound相关处理。
func TestGetAIReplyMissingAccountIsNotFound(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/ai-reply-settings/no-such", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在账号应 404，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestGetAIReplyExistingAccountWithoutConfigReturnsDefault 负责TestGetAI回复Existing账号Without配置ReturnsDefault相关处理。
func TestGetAIReplyExistingAccountWithoutConfigReturnsDefault(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/ai-reply-settings/acc1", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 保存响应，供当前处理流程使用
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["ai_enabled"] != false || res["max_discount_percent"] != float64(10) {
		t.Fatalf("默认值异常: %+v", res)
	}
}

// TestAIReplySettingsAreUserScoped 负责TestAI回复设置Are用户Scoped相关处理。
func TestAIReplySettingsAreUserScoped(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	if // err 保存err，供当前处理流程使用
	_, err := store.Users.Create(ctx, "user2", "u2@e.com", "pw"); err != nil {
		t.Fatalf("create user2: %v", err)
	}
	// user2、err 保存user2、err，供当前处理流程使用
	user2, err := store.Users.GetByUsername(ctx, "user2")
	if err != nil {
		t.Fatalf("get user2: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.Save(ctx, "other-acc", "unb=456; _m_h5_tk=tk2_1;", user2.ID); err != nil {
		t.Fatalf("save other cookie: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	_, err := store.DB.ExecContext(ctx,
		`INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('other-acc', 1, 'secret')`); err != nil {
		t.Fatalf("insert ai settings: %v", err)
	}

	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// listReq 保存listReq，供当前处理流程使用
	listReq := httptest.NewRequest(http.MethodGet, "/ai-reply-settings", nil)
	listReq.AddCookie(cookie)
	// listRec 保存listRec，供当前处理流程使用
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	// listed 保存listed，供当前处理流程使用
	var listed map[string]any
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if // ok 保存ok，供当前处理流程使用
	_, ok := listed["other-acc"]; ok {
		t.Fatalf("list leaked other user's AI settings: %+v", listed)
	}

	// getReq 保存getReq，供当前处理流程使用
	getReq := httptest.NewRequest(http.MethodGet, "/ai-reply-settings/other-acc", nil)
	getReq.AddCookie(cookie)
	// getRec 保存getRec，供当前处理流程使用
	getRec := httptest.NewRecorder()
	h.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusForbidden {
		t.Fatalf("cross-user get should be 403, got %d body=%s", getRec.Code, getRec.Body.String())
	}

	// setReq 保存setReq，供当前处理流程使用
	setReq := httptest.NewRequest(http.MethodPut, "/ai-reply-settings/other-acc", strings.NewReader(
		`{"ai_enabled":true,"max_discount_percent":20,"max_discount_amount":200,"max_bargain_rounds":5,"custom_prompts":"override"}`,
	))
	setReq.AddCookie(cookie)
	// setRec 保存setRec，供当前处理流程使用
	setRec := httptest.NewRecorder()
	h.ServeHTTP(setRec, setReq)
	if setRec.Code != http.StatusForbidden {
		t.Fatalf("cross-user set should be 403, got %d body=%s", setRec.Code, setRec.Body.String())
	}
}

// TestFetchOpenAIModelsEmptyBaseURL 空地址错误。
func TestFetchOpenAIModelsEmptyBaseURL(t *testing.T) {
	// err 保存err，供当前处理流程使用
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
}
