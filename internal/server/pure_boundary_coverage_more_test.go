package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chatapp "xianyu-go/internal/application/chat"
	defaultreplyapp "xianyu-go/internal/application/defaultreply"
)

// TestNormalizePublishHeaderCoversAliases 覆盖发布表格字段的全部业务别名与未知字段回退。
func TestNormalizePublishHeaderCoversAliases(t *testing.T) {
	// cases 保存字段别名及其稳定内部键名。
	cases := []struct {
		// input 表示用户表格中的原始列名。
		input string
		// want 表示导入流程使用的规范字段名。
		want string
	}{
		{"账号", "cookie_id"}, {"标题", "title"}, {"商品描述", "description"}, {"价格", "price"},
		{"原价", "original_price"}, {"库存", "quantity"}, {"邮费模式", "postage_mode"}, {"邮费", "postage"},
		{"商品图片", "images"}, {"类目id", "category_id"}, {"类目名称", "category_name"}, {"频道类目id", "channel_category_id"},
		{"淘宝类目id", "tb_category_id"}, {"付款后自动发货", "paid_delivery_enabled"}, {"付款后发送的卡密", "paid_delivery_contents"},
		{"评价后发送赠品", "review_gift_enabled"}, {"评价赠品内容", "review_gift_contents"}, {"超时未评价时提醒", "review_request_enabled"},
		{"发货几小时后提醒", "review_request_after_hours"}, {"提醒内容", "review_request_message"}, {"最多提醒几次", "review_request_max_attempts"},
		{"求评价延迟秒", "review_request_delay_seconds"}, {"未知列", "未知列"},
	}
	// testCase 表示当前待验证的列名映射样例。
	for _, testCase := range cases {
		// got 表示当前原始列名被规范化后的字段名。
		if got := normalizePublishHeader(testCase.input); got != testCase.want {
			t.Errorf("normalizePublishHeader(%q)=%q want=%q", testCase.input, got, testCase.want)
		}
	}
	// got 表示带大小写与首尾空白的标准英文列名结果。
	if got := normalizePublishHeader("  PRICE "); got != "price" {
		t.Fatalf("header normalization=%q", got)
	}
}

// TestWriteChatMetadataErrorMapsAllCases 覆盖聊天元数据错误到 HTTP envelope 的映射。
func TestWriteChatMetadataErrorMapsAllCases(t *testing.T) {
	// cases 保存应用错误及其 HTTP 状态。
	cases := []struct {
		// operationErr 表示应用层返回的错误。
		operationErr error
		// wantStatus 表示传输层期望返回的 HTTP 状态。
		wantStatus int
	}{
		{chatapp.ErrInvalidInput, http.StatusBadRequest},
		{chatapp.ErrMetadataForbidden, http.StatusForbidden},
		{chatapp.ErrQuickReplyLimitReached, http.StatusConflict},
		{chatapp.ErrQuickReplyNotFound, http.StatusNotFound},
		{errors.New("unexpected"), http.StatusInternalServerError},
	}
	// testCase 表示当前待验证的聊天元数据错误样例。
	for _, testCase := range cases {
		// recorder 捕获错误映射生成的 HTTP 响应。
		recorder := httptest.NewRecorder()
		writeChatMetadataError(recorder, testCase.operationErr)
		if recorder.Code != testCase.wantStatus {
			t.Errorf("error=%v status=%d want=%d", testCase.operationErr, recorder.Code, testCase.wantStatus)
		}
	}
}

// TestWriteDefaultReplyMutationErrorMapsAllCases 覆盖默认回复错误到 HTTP 状态的映射。
func TestWriteDefaultReplyMutationErrorMapsAllCases(t *testing.T) {
	// cases 保存应用错误及其 HTTP 状态。
	cases := []struct {
		// operationErr 表示默认回复应用层返回的错误。
		operationErr error
		// wantStatus 表示传输层期望返回的 HTTP 状态。
		wantStatus int
	}{
		{defaultreplyapp.ErrAccountNotFound, http.StatusNotFound},
		{defaultreplyapp.ErrForbidden, http.StatusForbidden},
		{errors.New("unexpected"), http.StatusInternalServerError},
	}
	// testCase 表示当前待验证的默认回复错误样例。
	for _, testCase := range cases {
		// recorder 捕获错误映射生成的 HTTP 响应。
		recorder := httptest.NewRecorder()
		writeDefaultReplyMutationError(recorder, testCase.operationErr, "fallback")
		if recorder.Code != testCase.wantStatus {
			t.Errorf("error=%v status=%d want=%d", testCase.operationErr, recorder.Code, testCase.wantStatus)
		}
	}
}

// TestDecodeCardAPIConfigAndSettingsValidation 覆盖卡券配置兼容解码和系统设置批量校验。
func TestDecodeCardAPIConfigAndSettingsValidation(t *testing.T) {
	// emptyConfig、emptyErr 保存空配置的兼容结果。
	emptyConfig, emptyErr := decodeCardAPIConfig(nil)
	if emptyErr != nil || emptyConfig != "" {
		t.Fatalf("empty config=%q err=%v", emptyConfig, emptyErr)
	}
	// nullConfig、nullErr 保存 JSON null 配置的兼容结果。
	nullConfig, nullErr := decodeCardAPIConfig(json.RawMessage("null"))
	if nullErr != nil || nullConfig != "" {
		t.Fatalf("null config=%q err=%v", nullConfig, nullErr)
	}
	// legacyConfig、legacyErr 保存历史字符串配置的解码结果。
	legacyConfig, legacyErr := decodeCardAPIConfig(json.RawMessage(`"legacy"`))
	if legacyErr != nil || legacyConfig != "legacy" {
		t.Fatalf("legacy config=%q err=%v", legacyConfig, legacyErr)
	}
	// objectConfig、objectErr 保存新版对象配置的规范 JSON。
	objectConfig, objectErr := decodeCardAPIConfig(json.RawMessage(`{"url":"https://example.invalid","timeout":3}`))
	if objectErr != nil || !strings.Contains(objectConfig, `"url"`) || !strings.Contains(objectConfig, `"timeout"`) {
		t.Fatalf("object config=%q err=%v", objectConfig, objectErr)
	}
	// invalidConfigErr 保存非法配置 JSON 的解码错误。
	_, invalidConfigErr := decodeCardAPIConfig(json.RawMessage(`{"url"`))
	if invalidConfigErr == nil {
		t.Fatal("invalid card config must fail")
	}
	// err 表示有效系统设置样例的校验错误。
	if err := validateSystemSettingValues(map[string]string{"log_level": "debug", "outbound_http_public_only": " TRUE "}); err != nil {
		t.Fatalf("valid settings error=%v", err)
	}
	// err 表示非法日志级别的校验错误。
	if err := validateSystemSettingValues(map[string]string{"log_level": "not-a-level"}); err == nil {
		t.Fatal("invalid log level must fail")
	}
	// err 表示非法公网限制开关的校验错误。
	if err := validateSystemSettingValues(map[string]string{"outbound_http_public_only": "maybe"}); err == nil {
		t.Fatal("invalid public-only flag must fail")
	}
	// actions 保存敏感设置命令及其有效性。
	actions := []struct {
		// action 表示待校验的敏感设置命令。
		action string
		// want 表示命令是否属于支持的三态语义。
		want bool
	}{
		{"retain", true}, {"replace", true}, {"clear", true}, {"delete", false},
	}
	// testCase 表示当前待验证的敏感设置命令样例。
	for _, testCase := range actions {
		// got 表示当前命令是否被识别为有效三态命令。
		if got := validSecretSettingAction(testCase.action); got != testCase.want {
			t.Errorf("action=%q got=%v want=%v", testCase.action, got, testCase.want)
		}
	}
}
