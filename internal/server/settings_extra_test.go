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
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// 注入一个本地 HTTP server 作为 ai_api_url。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen-plus"},{"id":"qwen-max"}]}`))
	}))
	defer ts.Close()

	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"base_url":"` + ts.URL + `","api_key":"sk-test"}`
	req := httptest.NewRequest(http.MethodPost, "/ai-models", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	models, _ := res["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("应2个模型，got %+v", res)
	}
}

func TestReadOpenAIModelsBodyRejectsOversizedResponse(t *testing.T) {
	_, err := readOpenAIModelsBody(strings.NewReader(strings.Repeat("x", maxOpenAIModelsResponseBytes+1)))
	if err == nil {
		t.Fatal("oversized models response should fail")
	}
}

// TestListAIModelsBadJSON 非法 JSON 400。
func TestListAIModelsBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/ai-models", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestListAIModelsEmptyBaseURL 空地址使用默认并失败（默认阿里云地址不可达或返回非 2xx）。
func TestListAIModelsEmptyBaseURL(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// 注入一个返回错误状态码的本地 server。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	// 设置系统设置 ai_api_url 指向该 server。
	srv.Store.Settings.Set(context.Background(), "ai_api_url", ts.URL)

	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/ai-models", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("模型拉取失败应 502，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestSetSettingBadJSON 非法 JSON 400。
func TestSetSettingBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/system-settings/theme_color", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

func TestSetLogLevelValidatesAndAppliesRuntimeLevel(t *testing.T) {
	defer logging.Level.Set(slog.LevelInfo)

	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	badReq := httptest.NewRequest(http.MethodPut, "/system-settings/log_level", strings.NewReader(`{"value":"verbose"}`))
	badReq.AddCookie(cookie)
	badRec := httptest.NewRecorder()
	h.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid log level should be 400, got %d body=%s", badRec.Code, badRec.Body.String())
	}

	goodReq := httptest.NewRequest(http.MethodPut, "/system-settings/log_level", strings.NewReader(`{"value":"debug"}`))
	goodReq.AddCookie(cookie)
	goodRec := httptest.NewRecorder()
	h.ServeHTTP(goodRec, goodReq)
	if goodRec.Code != http.StatusOK {
		t.Fatalf("valid log level should be 200, got %d body=%s", goodRec.Code, goodRec.Body.String())
	}
	if got := logging.Level.Level(); got != slog.LevelDebug {
		t.Fatalf("runtime log level=%v want debug", got)
	}
	saved, err := srv.Store.Settings.Get(context.Background(), "log_level")
	if err != nil || saved != "debug" {
		t.Fatalf("saved log_level=%q err=%v", saved, err)
	}
}

func TestSystemSettingsRequireAdmin(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	if _, err := srv.Store.Users.Create(context.Background(), "user-settings", "user-settings@example.com", "pw"); err != nil {
		t.Fatalf("create user: %v", err)
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"user-settings","password":"pw"}`))
	loginRec := httptest.NewRecorder()
	h.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK || len(loginRec.Result().Cookies()) == 0 {
		t.Fatalf("login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	cookie := loginRec.Result().Cookies()[0]

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/system-settings", ""},
		{http.MethodPut, "/system-settings/theme_color", `{"value":"red"}`},
		{http.MethodPost, "/ai-models", `{"base_url":"http://127.0.0.1","api_key":"sk"}`},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s should be 403, got %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

// TestListUserSettings 用户设置增删查。
func TestListUserSettings(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	// 设。
	body := `{"value":"dark"}`
	req := httptest.NewRequest(http.MethodPut, "/user-settings/theme", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("set status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 查全部。
	req2 := httptest.NewRequest(http.MethodGet, "/user-settings", nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("list status=%d", rec2.Code)
	}
	var m map[string]string
	json.Unmarshal(rec2.Body.Bytes(), &m)
	if m["theme"] != "dark" {
		t.Fatalf("设置未生效: %+v", m)
	}

	// 查单。
	req3 := httptest.NewRequest(http.MethodGet, "/user-settings/theme", nil)
	req3.AddCookie(cookie)
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	var one map[string]any
	json.Unmarshal(rec3.Body.Bytes(), &one)
	if one["value"] != "dark" {
		t.Fatalf("查单异常: %+v", one)
	}

	// 查不存在的 key。
	req4 := httptest.NewRequest(http.MethodGet, "/user-settings/no-such-key", nil)
	req4.AddCookie(cookie)
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	var miss map[string]any
	json.Unmarshal(rec4.Body.Bytes(), &miss)
	if miss["value"] != "" {
		t.Fatalf("不存在 key 应返回空: %+v", miss)
	}
}

// TestSetUserSettingBadJSON 非法 JSON 400。
func TestSetUserSettingBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/user-settings/theme", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestSetAIReplyBadJSON 非法 JSON 400。
func TestSetAIReplyBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/ai-reply-settings/acc1", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

func TestGetAIReplyMissingAccountIsNotFound(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/ai-reply-settings/no-such", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在账号应 404，got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetAIReplyExistingAccountWithoutConfigReturnsDefault(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/ai-reply-settings/acc1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["ai_enabled"] != false || res["max_discount_percent"] != float64(10) {
		t.Fatalf("默认值异常: %+v", res)
	}
}

func TestAIReplySettingsAreUserScoped(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := store.Users.Create(ctx, "user2", "u2@e.com", "pw"); err != nil {
		t.Fatalf("create user2: %v", err)
	}
	user2, err := store.Users.GetByUsername(ctx, "user2")
	if err != nil {
		t.Fatalf("get user2: %v", err)
	}
	if err := store.Cookies.Save(ctx, "other-acc", "unb=456; _m_h5_tk=tk2_1;", user2.ID); err != nil {
		t.Fatalf("save other cookie: %v", err)
	}
	if _, err := store.DB.ExecContext(ctx,
		`INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('other-acc', 1, 'secret')`); err != nil {
		t.Fatalf("insert ai settings: %v", err)
	}

	h := srv.Router()
	cookie := loginHelper(t, h)

	listReq := httptest.NewRequest(http.MethodGet, "/ai-reply-settings", nil)
	listReq.AddCookie(cookie)
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var listed map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if _, ok := listed["other-acc"]; ok {
		t.Fatalf("list leaked other user's AI settings: %+v", listed)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/ai-reply-settings/other-acc", nil)
	getReq.AddCookie(cookie)
	getRec := httptest.NewRecorder()
	h.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusForbidden {
		t.Fatalf("cross-user get should be 403, got %d body=%s", getRec.Code, getRec.Body.String())
	}

	setReq := httptest.NewRequest(http.MethodPut, "/ai-reply-settings/other-acc", strings.NewReader(
		`{"ai_enabled":true,"max_discount_percent":20,"max_discount_amount":200,"max_bargain_rounds":5,"custom_prompts":"override"}`,
	))
	setReq.AddCookie(cookie)
	setRec := httptest.NewRecorder()
	h.ServeHTTP(setRec, setReq)
	if setRec.Code != http.StatusForbidden {
		t.Fatalf("cross-user set should be 403, got %d body=%s", setRec.Code, setRec.Body.String())
	}
}

// TestFetchOpenAIModelsEmptyBaseURL 空地址错误。
func TestFetchOpenAIModelsEmptyBaseURL(t *testing.T) {
	_, err := fetchOpenAIModels(context.Background(), "", "")
	if err == nil {
		t.Fatal("空地址应报错")
	}
}
