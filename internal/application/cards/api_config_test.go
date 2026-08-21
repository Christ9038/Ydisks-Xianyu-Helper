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
