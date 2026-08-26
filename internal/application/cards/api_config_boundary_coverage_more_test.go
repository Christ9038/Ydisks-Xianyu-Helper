package cards

import "testing"

// TestAPIConfigParseAndSummaryPropagateNormalizationErrors 验证解析和摘要入口都不会吞掉归一化阶段错误。
func TestAPIConfigParseAndSummaryPropagateNormalizationErrors(t *testing.T) {
	// parseErr 保存非法 JSON 在完整解析入口返回的错误。
	_, parseErr := ParseAPIConfig("{")
	if parseErr == nil {
		t.Fatal("非法 JSON 应从完整解析入口返回错误")
	}
	// summary 保存非法 JSON 在摘要入口生成的脱敏错误状态。
	summary := SummarizeAPIConfig("{")
	if summary.Ready || summary.ValidationError == "" {
		t.Fatalf("非法 JSON 摘要异常: %+v", summary)
	}
	// replaceErr 保存 replace 三态缺少新模板时的解析错误。
	_, replaceErr := ParseAPIConfig(`{"url":"https://example.test","headers_action":"replace"}`)
	if replaceErr == nil {
		t.Fatal("replace 缺少 headers 模板应返回错误")
	}
	// retainSummary 保存无旧模板时 retain 三态的合法摘要结果。
	retainSummary := SummarizeAPIConfig(`{"url":"https://example.test","headers_action":"retain","params_action":"retain"}`)
	if !retainSummary.Ready {
		t.Fatalf("无旧模板 retain 摘要应可用: %+v", retainSummary)
	}
}
