package db

import (
	"context"
	"encoding/json"
	"testing"
)

// TestCardSummaryHelpers 覆盖卡券 API 摘要的标量解析、超时兼容和模板校验分支。
func TestCardSummaryHelpers(t *testing.T) {
	// cases 保存不同公开配置输入及其摘要预期。
	cases := []struct {
		name  string
		raw   string
		ready bool
	}{
		{name: "valid", raw: `{"url":"https://example.test/api","method":"post","timeout_seconds":5,"retry_enabled":true,"headers":{"x":"{idempotency_key}"}}`, ready: true},
		{name: "legacy-timeout", raw: `{"url":"http://example.test","timeout":"7"}`, ready: true},
		{name: "invalid-json", raw: "{", ready: false},
		{name: "invalid-url", raw: `{"url":"file:///tmp/a"}`, ready: false},
		{name: "invalid-method", raw: `{"url":"https://example.test","method":"PUT"}`, ready: false},
		{name: "invalid-timeout", raw: `{"url":"https://example.test","timeout_seconds":61}`, ready: false},
		{name: "retry-without-key", raw: `{"url":"https://example.test","retry_enabled":true}`, ready: false},
	}
	// item 表示当前待验证的 API 摘要样例。
	for _, item := range cases {
		// summary 保存当前配置的非敏感摘要。
		summary := summarizeCardAPIConfig("api", item.raw)
		if summary == nil || summary.Ready != item.ready {
			t.Fatalf("%s summary=%+v want ready=%v", item.name, summary, item.ready)
		}
	}
	// nonAPI 验证非 API 卡券不产生 API 摘要。
	nonAPI := summarizeCardAPIConfig("text", `{}`)
	if nonAPI != nil {
		t.Fatalf("non-api summary=%+v", nonAPI)
	}
	// timeoutDefault、timeoutInvalid 验证新旧字段和非法文本的解析结果。
	timeoutDefault := parseSummaryTimeout(map[string]json.RawMessage{})
	// timeoutInvalid 保存非法超时文本的解析结果。
	timeoutInvalid := parseSummaryTimeout(map[string]json.RawMessage{"timeout_seconds": json.RawMessage(`"bad"`)})
	if timeoutDefault != 10 || timeoutInvalid != 0 {
		t.Fatalf("timeouts=%d/%d", timeoutDefault, timeoutInvalid)
	}
	// scalarNumber、scalarInvalid 验证摘要标量的数字与非法 JSON 解析。
	scalarNumber := summaryRawString(json.RawMessage(`12`))
	// scalarInvalid 保存非法 JSON 标量的空结果。
	scalarInvalid := summaryRawString(json.RawMessage(`{`))
	if scalarNumber != "12" || scalarInvalid != "" {
		t.Fatalf("scalars=%q/%q", scalarNumber, scalarInvalid)
	}
	// configured 验证空对象、空字符串和有效模板的配置判定。
	if templateConfigured(json.RawMessage(`{}`)) || templateConfigured(json.RawMessage(`""`)) || !templateConfigured(json.RawMessage(`{"x":1}`)) {
		t.Fatal("templateConfigured result incorrect")
	}
	// summary 是公开字段校验使用的有效摘要。
	summary := CardAPIConfigSummary{URL: "https://example.test", Method: "GET", TimeoutSeconds: 5, RetryEnabled: true}
	// missingKey 验证重试配置缺少幂等占位符时被拒绝。
	missingKey := validateSummaryAPIConfig(summary, map[string]any{"headers": map[string]any{"x": "no-key"}})
	if missingKey == nil {
		t.Fatal("retry without idempotency key should fail")
	}
	// nestedKey 验证数组和对象嵌套中的占位符可被找到。
	if !summaryTemplateContains([]any{map[string]any{"x": "{idempotency_key}"}}, "{idempotency_key}") || summaryTemplateContains(42, "x") {
		t.Fatal("nested template matching incorrect")
	}
}

// TestCardSummaryRepositoryPaths 验证卡券完整读取和脱敏摘要读取不会混淆敏感 API 模板。
func TestCardSummaryRepositoryPaths(t *testing.T) {
	// store、cleanup 提供迁移后的 SQLite 测试数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// userID 保存卡券所属用户主键。
	userID, _ := seedAccount(t, store)
	// apiCard 保存带请求模板的 API 卡券。
	apiCard := &CardFull{Name: "API卡", Type: "api", APIConfig: `{"url":"https://example.test","method":"GET","timeout_seconds":5}`, Enabled: true, UserID: userID}
	// apiID、createErr 保存 API 卡券创建结果。
	apiID, createErr := store.Cards.Create(ctx, apiCard)
	if createErr != nil || apiID == 0 {
		t.Fatalf("apiID=%d err=%v", apiID, createErr)
	}
	// summary、summaryErr 保存脱敏摘要读取结果。
	summary, summaryErr := store.Cards.GetSummary(ctx, apiID)
	if summaryErr != nil || summary == nil || summary.APIConfig != "" || summary.APIConfigSummary == nil || !summary.APIConfigSummary.Ready {
		t.Fatalf("summary=%+v err=%v", summary, summaryErr)
	}
	// full、fullErr 保存自动发货专用的完整配置读取结果。
	full, fullErr := store.Cards.GetForDelivery(ctx, apiID)
	if fullErr != nil || full == nil || full.APIConfig == "" {
		t.Fatalf("full=%+v err=%v", full, fullErr)
	}
	// all、allErr 保存用户卡券脱敏列表。
	all, allErr := store.Cards.AllForUserSummary(ctx, userID)
	if allErr != nil || len(all) != 1 || all[0].APIConfig != "" || all[0].APIConfigSummary == nil {
		t.Fatalf("all=%+v err=%v", all, allErr)
	}
	// missing、missingErr 验证不存在卡券的统一错误。
	missing, missingErr := store.Cards.GetSummary(ctx, 999999)
	if missing != nil || missingErr == nil {
		t.Fatalf("missing=%+v err=%v", missing, missingErr)
	}
}
