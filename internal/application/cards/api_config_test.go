package cards

import (
	"encoding/json"
	"testing"
)

// TestParseAPIConfigCompatibilityAndRetryGuard 验证历史字段归一、合法路径和幂等重试前置条件。
func TestParseAPIConfigCompatibilityAndRetryGuard(t *testing.T) {
	// legacy 保存历史项目使用的 timeout、headers 和 params 字符串字段。
	legacy := `{"url":"https://example.com/card","method":"get","timeout":"12","headers":"{\"Authorization\":\"Bearer secret\"}","params":"{\"order\":\"{order_id}\"}"}`
	// config、err 保存历史配置归一结果及错误。
	config, err := normalizeAPIConfig(legacy, "")
	if err != nil || config == "" {
		t.Fatalf("历史 API 配置应可归一 config=%q err=%v", config, err)
	}
	// document、parseErr 保存归一配置的执行模型。
	document, parseErr := ParseAPIConfig(config)
	if parseErr != nil || document.Method != "GET" || document.Timeout != 12 || document.Params["order"] != "{order_id}" {
		t.Fatalf("历史 API 配置归一错误 document=%+v err=%v", document, parseErr)
	}
	// _, retryErr 验证缺少稳定幂等键时拒绝开启重试。
	if _, retryErr := normalizeAPIConfig(`{"url":"https://example.com","retry_enabled":true,"params":{"order_id":"{order_id}"}}`, ""); retryErr == nil {
		t.Fatal("启用重试但没有幂等键时必须拒绝保存")
	}
	// summary 保存脱敏摘要，确认模板秘密不会进入查询模型。
	summary := SummarizeAPIConfig(config)
	if !summary.Ready || !summary.HeadersConfigured || !summary.ParamsConfigured || summary.ValidationError != "" {
		t.Fatalf("API 摘要错误: %+v", summary)
	}
}

// TestNormalizeAPIConfigThreeStateAndNullGuard 验证敏感模板三态更新及 null 配置不会触发 panic。
func TestNormalizeAPIConfigThreeStateAndNullGuard(t *testing.T) {
	// existing 是包含秘密模板的旧配置，仅用于验证保留和清除语义。
	existing := `{"url":"https://example.com","headers":{"Authorization":"secret"},"params":{"token":"value"}}`
	// retained、retainErr 保存 retain 操作的归一结果及错误。
	retained, retainErr := normalizeAPIConfig(`{"url":"https://example.com","headers_action":"retain","params_action":"retain"}`, existing)
	if retainErr != nil || retained == "" {
		t.Fatalf("retain 应保留旧模板 config=%q err=%v", retained, retainErr)
	}
	// cleared、clearErr 保存 clear 操作的归一结果及错误。
	cleared, clearErr := normalizeAPIConfig(`{"url":"https://example.com","headers_action":"clear","params_action":"clear"}`, existing)
	if clearErr != nil || !containsJSONEmptyObject(cleared, "headers") || !containsJSONEmptyObject(cleared, "params") {
		t.Fatalf("clear 应清空旧模板 config=%q err=%v", cleared, clearErr)
	}
	// replaceErr 保存缺少替换模板时的校验错误。
	if _, replaceErr := normalizeAPIConfig(`{"url":"https://example.com","headers_action":"replace"}`, existing); replaceErr == nil {
		t.Fatal("replace 未提供模板时必须拒绝而不是默默保留")
	}
	// nullErr 保存 null 配置的校验错误。
	if _, nullErr := normalizeAPIConfig("null", existing); nullErr == nil {
		t.Fatal("null API 配置必须返回校验错误")
	}
}

// containsJSONEmptyObject 判断规范 JSON 中指定模板是否为显式空对象。
func containsJSONEmptyObject(raw, key string) bool {
	// fields 保存规范 JSON 对象，测试只检查模板是否为空而不输出秘密值。
	var fields map[string]any
	// err 表示测试 JSON 解析错误。
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return false
	}
	// value、ok 保存指定字段的对象值及类型判断。
	value, ok := fields[key].(map[string]any)
	return ok && len(value) == 0
}

// TestAPIConfigRejectsInvalidAndCoversCompatibilityHelpers 验证 API 配置解析错误、默认值和递归占位符边界。
func TestAPIConfigRejectsInvalidAndCoversCompatibilityHelpers(t *testing.T) {
	if // err 是空 API 配置的缺失错误。
	_, err := ParseAPIConfig(""); err == nil {
		t.Fatal("空 API 配置应被拒绝")
	}
	if // err 是非法 JSON API 配置的解析错误。
	_, err := ParseAPIConfig("{"); err == nil {
		t.Fatal("非法 JSON 应被拒绝")
	}
	if // err 是非法 API 地址的校验错误。
	_, err := ParseAPIConfig(`{"url":"ftp://example.test"}`); err == nil {
		t.Fatal("非法协议地址应被拒绝")
	}
	if // err 是带用户凭据 API 地址的校验错误。
	_, err := ParseAPIConfig(`{"url":"https://user:pass@example.test"}`); err == nil {
		t.Fatal("带用户凭据地址应被拒绝")
	}
	if // err 是不支持请求方法的校验错误。
	_, err := ParseAPIConfig(`{"url":"https://example.test","method":"PUT"}`); err == nil {
		t.Fatal("不支持的方法应被拒绝")
	}
	if // err 是超时越界的校验错误。
	_, err := ParseAPIConfig(`{"url":"https://example.test","timeout_seconds":61}`); err == nil {
		t.Fatal("越界超时应被拒绝")
	}
	if // err 是启用重试但没有幂等键的校验错误。
	_, err := ParseAPIConfig(`{"url":"https://example.test","retry_enabled":true,"headers":{"x":"none"}}`); err == nil {
		t.Fatal("缺少幂等键应被拒绝")
	}
	// summary 是非法配置的脱敏校验摘要。
	summary := SummarizeAPIConfig("{")
	if summary.ValidationError == "" || summary.Ready {
		t.Fatalf("非法配置摘要异常: %+v", summary)
	}
	// fallback 是使用 existing 配置补齐空更新的规范 JSON。
	fallback, fallbackErr := normalizeAPIConfig("", `{"url":"https://example.test"}`)
	if fallbackErr != nil || fallback == "" {
		t.Fatalf("空更新未保留旧配置: config=%q err=%v", fallback, fallbackErr)
	}
	if // err 是空字符串模板字段的兼容解析结果。
	_, err := normalizeAPIConfig(`{"url":"https://example.test","headers":"","params":""}`, ""); err != nil {
		t.Fatalf("空模板字段不应失败: %v", err)
	}
	if // err 是非法旧模板字符串的解析错误。
	_, err := normalizeAPIConfig(`{"url":"https://example.test","headers":"not-json"}`, ""); err == nil {
		t.Fatal("非法旧模板字符串应被拒绝")
	}
	if // err 是未知模板三态命令的校验错误。
	_, err := normalizeAPIConfig(`{"url":"https://example.test","headers_action":"unknown"}`, ""); err == nil {
		t.Fatal("未知模板操作应被拒绝")
	}
	if // err 是参数模板未知三态命令的校验错误。
	_, err := normalizeAPIConfig(`{"url":"https://example.test","params_action":"unknown"}`, ""); err == nil {
		t.Fatal("参数模板未知操作应被拒绝")
	}
	if // err 是未提交请求头模板时保留已有敏感模板的结果。
	_, err := normalizeAPIConfig(`{"url":"https://example.test"}`, `{"url":"https://example.test","headers":{"x":"old"}}`); err != nil {
		t.Fatalf("未提交模板保留失败: %v", err)
	}
	if // err 是没有旧值时 retain 模板的空对象回退结果。
	_, err := normalizeAPIConfig(`{"url":"https://example.test","headers_action":"retain","params_action":"retain"}`, ""); err != nil {
		t.Fatalf("无旧值 retain 不应失败: %v", err)
	}
	if !containsAPIPlaceholder([]any{"prefix", map[string]any{"id": "{idempotency_key}"}}, "{idempotency_key}") {
		t.Fatal("数组嵌套占位符未识别")
	}
	// emptyFields 是模板字段为空时解码器应跳过的字段集合。
	emptyFields := map[string]json.RawMessage{"headers": nil, "params": nil}
	// emptyDocument、emptyErr 保存空字段解码结果。
	var emptyDocument APIConfig
	// emptyErr 保存空模板字段解码返回的错误。
	emptyErr := decodeAPIConfig(emptyFields, &emptyDocument)
	if emptyErr != nil {
		t.Fatalf("空模板字段解码失败: %v", emptyErr)
	}
	// invalidFields 是直接调用低层解码器时的非法 RawMessage 字段集合。
	invalidFields := map[string]json.RawMessage{"headers": json.RawMessage("{")}
	// invalidDocument 保存非法 RawMessage 解码目标。
	var invalidDocument APIConfig
	if // err 是低层配置编码失败错误。
	err := decodeAPIConfig(invalidFields, &invalidDocument); err == nil {
		t.Fatal("非法 RawMessage 应触发低层解码错误")
	}
	if // err 是摘要低层解码失败错误。
	_, err := decodeAPIConfigForSummary(invalidFields); err == nil {
		t.Fatal("非法摘要 RawMessage 应触发低层解码错误")
	}
	if containsAPIPlaceholder(map[string]any{"id": "none"}, "{idempotency_key}") || containsAPIPlaceholder(123, "{idempotency_key}") {
		t.Fatal("不存在的占位符被错误识别")
	}
	if // raw 是空 JSON 字段的空字符串结果。
	raw := rawString(nil); raw != "" {
		t.Fatalf("空 JSON 字段=%q", raw)
	}
	if // raw 是数字 JSON 字段的兼容字符串结果。
	raw := rawString(json.RawMessage("12")); raw != "12" {
		t.Fatalf("数字 JSON 字段=%q", raw)
	}
	if // raw 是非法 JSON 字段的空结果。
	raw := rawString(json.RawMessage("{")); raw != "" {
		t.Fatalf("非法 JSON 字段=%q", raw)
	}
	if // value 是非法兼容超时的稳定默认值。
	value := parseIntDefault("bad", 7); value != 7 {
		t.Fatalf("非法超时默认值=%d", value)
	}
}
