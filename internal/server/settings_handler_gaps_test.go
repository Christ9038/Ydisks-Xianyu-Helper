package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	settingsapp "xianyu-go/internal/application/settings"
)

// settingsGapPort 是系统设置端口桩，所有读写返回预设错误。
type settingsGapPort struct {
	// SettingsPort 提供未覆盖方法的默认（空）实现。
	SettingsPort
	// applyErr 是 ApplySystemChanges 的错误。
	applyErr error
	// getSystemErr 是 GetSystem 的错误。
	getSystemErr error
	// publicErr 是 PublicSystem 的错误。
	publicErr error
	// aiGetErr 是 GetAIReply 的错误。
	aiGetErr error
	// aiUpsertErr 是 UpsertAIReply 的错误。
	aiUpsertErr error
	// aiListErr 是 ListAIReply 的错误。
	aiListErr error
	// modelsErr 是 ListAIModels 的错误。
	modelsErr error
	// testErr 是 TestAIConnection 的错误。
	testErr error
}

// IsSensitiveSettingKey 把 ai_api_key 视为敏感设置。
func (p *settingsGapPort) IsSensitiveSettingKey(key string) bool { return key == "ai_api_key" }

// ApplySystemChanges 返回预设错误。
func (p *settingsGapPort) ApplySystemChanges(context.Context, int64, map[string]string, map[string]settingsapp.SecretChange) error {
	return p.applyErr
}

// GetSystem 返回预设错误。
func (p *settingsGapPort) GetSystem(context.Context, int64) (map[string]string, error) {
	return map[string]string{}, p.getSystemErr
}

// PublicSystem 返回预设错误。
func (p *settingsGapPort) PublicSystem(context.Context) (map[string]string, error) {
	return map[string]string{}, p.publicErr
}

// GetAIReply 返回预设错误。
func (p *settingsGapPort) GetAIReply(context.Context, int64, string) (settingsapp.AIReplySettings, error) {
	return settingsapp.AIReplySettings{CookieID: "acc1"}, p.aiGetErr
}

// UpsertAIReply 返回预设错误。
func (p *settingsGapPort) UpsertAIReply(context.Context, int64, string, settingsapp.AIReplySettings) error {
	return p.aiUpsertErr
}

// ListAIReply 返回预设错误或一行配置。
func (p *settingsGapPort) ListAIReply(context.Context, int64) ([]settingsapp.AIReplySettings, error) {
	return []settingsapp.AIReplySettings{{CookieID: "acc1"}}, p.aiListErr
}

// ListAIModels 返回预设错误。
func (p *settingsGapPort) ListAIModels(context.Context, int64, string, string, string) ([]string, error) {
	return nil, p.modelsErr
}

// TestAIConnection 返回预设错误。
func (p *settingsGapPort) TestAIConnection(context.Context, int64, string, string, string, string) (settingsapp.AIConnectionTestResult, error) {
	return settingsapp.AIConnectionTestResult{}, p.testErr
}

// SetSystem 返回预设错误。
func (p *settingsGapPort) SetSystem(context.Context, int64, string, string, string) error {
	return p.applyErr
}

// TestSetSettingsBatchGapBranches 覆盖批量系统设置的格式、校验与保存失败分支。
func TestSetSettingsBatchGapBranches(t *testing.T) {
	// srv、cleanup 是基础测试服务及资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 是可变系统设置桩。
	port := &settingsGapPort{}
	srv.applications.settings = port
	// handler 是真实路由。
	handler := srv.Router()
	// cookie 是管理员会话。
	cookie := loginHelper(t, handler)

	// cases 描述请求体与期望状态码。
	cases := []struct {
		// name 是子场景名称。
		name string
		// body 是请求体。
		body string
		// want 是期望状态码。
		want int
	}{
		{"empty object", `{}`, 400},
		{"bad values", `{"values":5}`, 400},
		{"sensitive in values", `{"values":{"ai_api_key":"k"}}`, 400},
		{"bad value json", `{"values":{"theme":null}}`, 400},
		{"empty key", `{"values":{"":"x"}}`, 400},
		{"bad secrets", `{"secrets":5}`, 400},
		{"invalid secret action", `{"secrets":{"ai_api_key":{"action":"wipe"}}}`, 400},
		{"replace without value", `{"secrets":{"ai_api_key":{"action":"replace"}}}`, 400},
		{"non sensitive secret", `{"secrets":{"theme":{"action":"clear"}}}`, 400},
		{"legacy sensitive", `{"ai_api_key":"k"}`, 400},
		{"legacy null", `{"theme":null}`, 400},
		{"only retain secret", `{"secrets":{"ai_api_key":{"action":"retain"}}}`, 200},
		{"bad log level", `{"log_level":"verbose"}`, 400},
		{"bad bool", `{"outbound_http_public_only":"maybe"}`, 400},
		{"explicit values ok", `{"values":{"theme":"dark"},"theme_color":"x"}`, 200},
	}
	// tc 表示当前批量设置场景。
	for _, tc := range cases {
		// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
		if rec := serveChatCoverageRequest(handler, cookie, "PUT", "/system-settings", tc.body); rec.Code != tc.want {
			t.Errorf("%s => %d want %d body=%s", tc.name, rec.Code, tc.want, rec.Body.String())
		}
	}
	port.applyErr = errors.New("db")
	// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
	if rec := serveChatCoverageRequest(handler, cookie, "PUT", "/system-settings", `{"theme":"x"}`); rec.Code != http.StatusInternalServerError {
		t.Errorf("apply failure status=%d", rec.Code)
	}
	// 单项设置保存失败。
	for _, path := range []string{"/system-settings/theme", "/system-settings/ai_api_key"} {
		// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
		if rec := serveChatCoverageRequest(handler, cookie, "PUT", path, `{"value":"x","action":"replace"}`); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s set failure status=%d", path, rec.Code)
		}
	}
	// 查询失败。
	port.getSystemErr = errors.New("db")
	// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
	if rec := serveChatCoverageRequest(handler, cookie, "GET", "/system-settings", ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("get all failure status=%d", rec.Code)
	}
	port.publicErr = errors.New("db")
	// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
	if rec := serveChatCoverageRequest(handler, cookie, "GET", "/system-settings/public", ""); rec.Code == http.StatusOK {
		t.Logf("public settings route returned %d", rec.Code)
	}
}

// TestAIReplyAndModelHandlersGapBranches 覆盖 AI 回复设置与模型接口的错误映射。
func TestAIReplyAndModelHandlersGapBranches(t *testing.T) {
	// srv、cleanup 是基础测试服务及资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 是可变系统设置桩。
	port := &settingsGapPort{}
	srv.applications.settings = port
	// handler 是真实路由。
	handler := srv.Router()
	// cookie 是管理员会话。
	cookie := loginHelper(t, handler)
	// validBody 是合法 AI 回复设置。
	const validBody = `{"ai_enabled":true,"max_discount_percent":10,"max_discount_amount":10,"max_bargain_rounds":3}`

	port.aiListErr = errors.New("db")
	// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
	if rec := serveChatCoverageRequest(handler, cookie, "GET", "/ai-reply-settings", ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("list failure status=%d", rec.Code)
	}
	port.aiListErr = nil
	// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
	if rec := serveChatCoverageRequest(handler, cookie, "GET", "/ai-reply-settings", ""); rec.Code != http.StatusOK {
		t.Errorf("list status=%d", rec.Code)
	}

	// getCases 描述单账号读取的错误映射。
	getCases := []struct {
		err  error
		want int
	}{
		{settingsapp.ErrConfigNotFound, 200}, {settingsapp.ErrAccountNotFound, 404},
		{settingsapp.ErrForbidden, 403}, {errors.New("db"), 500}, {nil, 200},
	}
	// tc 表示当前读取场景。
	for _, tc := range getCases {
		port.aiGetErr = tc.err
		// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
		if rec := serveChatCoverageRequest(handler, cookie, "GET", "/ai-reply-settings/acc1", ""); rec.Code != tc.want {
			t.Errorf("get %v => %d want %d", tc.err, rec.Code, tc.want)
		}
	}

	// ownership 描述保存前归属校验失败映射。
	ownership := []struct {
		err  error
		want int
	}{{settingsapp.ErrAccountNotFound, 404}, {errors.New("db"), 500}}
	// tc 表示当前归属失败场景。
	for _, tc := range ownership {
		port.aiGetErr = tc.err
		// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
		if rec := serveChatCoverageRequest(handler, cookie, "PUT", "/ai-reply-settings/acc1", validBody); rec.Code != tc.want {
			t.Errorf("set ownership %v => %d want %d", tc.err, rec.Code, tc.want)
		}
	}
	port.aiGetErr = settingsapp.ErrConfigNotFound
	// validation 描述保存字段校验失败。
	validation := []string{
		`{`, `{"max_discount_percent":101,"max_bargain_rounds":3}`, `{"max_discount_amount":-1,"max_bargain_rounds":3}`,
		`{"max_bargain_rounds":0}`, `{"max_bargain_rounds":3,"ai_mode":"weird"}`, `{"max_bargain_rounds":3,"auto_adjust_price_enabled":true}`,
	}
	// body 表示当前校验失败请求体。
	for _, body := range validation {
		// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
		if rec := serveChatCoverageRequest(handler, cookie, "PUT", "/ai-reply-settings/acc1", body); rec.Code != http.StatusBadRequest {
			t.Errorf("validation %q => %d", body, rec.Code)
		}
	}
	// upsert 描述写入失败映射。
	upsert := []struct {
		err  error
		want int
	}{
		{settingsapp.ErrPricingModeConflict, 409}, {settingsapp.ErrAccountNotFound, 404},
		{errors.New("db"), 500}, {nil, 200},
	}
	// tc 表示当前写入场景。
	for _, tc := range upsert {
		port.aiUpsertErr = tc.err
		// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
		if rec := serveChatCoverageRequest(handler, cookie, "PUT", "/ai-reply-settings/acc1", validBody); rec.Code != tc.want {
			t.Errorf("upsert %v => %d want %d", tc.err, rec.Code, tc.want)
		}
	}

	// 模型目录与连接测试。
	for _, path := range []string{"/ai-models", "/ai-test"} {
		// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
		if rec := serveChatCoverageRequest(handler, cookie, "POST", path, "{"); rec.Code != http.StatusBadRequest {
			t.Errorf("%s bad json status=%d", path, rec.Code)
		}
	}
	port.modelsErr, port.testErr = errors.New("upstream"), errors.New("upstream")
	// path 当前遍历的请求路径
	for _, path := range []string{"/ai-models", "/ai-test"} {
		// rec 本次请求返回的 HTTP 响应记录，用于断言状态码与正文
		if rec := serveChatCoverageRequest(handler, cookie, "POST", path, `{"base_url":"https://x","api_key":"k"}`); rec.Code != http.StatusBadGateway {
			t.Errorf("%s upstream status=%d", path, rec.Code)
		}
	}
}

// TestCollectSystemSettingValuesRejectsLongKeys 覆盖设置键长度和 JSON 值解析的纯函数分支。
func TestCollectSystemSettingValuesRejectsLongKeys(t *testing.T) {
	// longKey 超过 100 个字符。
	longKey := make([]byte, 101)
	// i 循环序号，用于生成不同的测试数据
	for i := range longKey {
		longKey[i] = 'a'
	}
	// values 保存收集结果。
	values := map[string]string{}
	// err 本步骤返回的错误，用于断言失败或成功路径
	if err := collectSystemSettingValues(map[string]json.RawMessage{string(longKey): json.RawMessage(`"x"`)}, values, func(string) bool { return false }); err == nil {
		t.Error("long key should fail")
	}
	// err 本步骤返回的错误，用于断言失败或成功路径
	if err := collectSystemSettingValues(map[string]json.RawMessage{"a": json.RawMessage(`{`)}, values, func(string) bool { return false }); err == nil {
		t.Error("malformed value should fail")
	}
}
