package cards

import "testing"

// TestAPIConfigSummaryCoversValidationAndDecodeFailures 覆盖 API 配置摘要的校验失败和执行模型解码失败。
func TestAPIConfigSummaryCoversValidationAndDecodeFailures(t *testing.T) {
	// invalidSummary 保存缺少合法 URL 的脱敏摘要。
	invalidSummary := SummarizeAPIConfig(`{"method":"GET"}`)
	if invalidSummary.Ready || invalidSummary.ValidationError == "" {
		t.Fatalf("缺少 URL 的摘要异常: %+v", invalidSummary)
	}
	// methodSummary 保存不支持请求方法的脱敏摘要。
	methodSummary := SummarizeAPIConfig(`{"url":"https://example.test","method":"PATCH"}`)
	if methodSummary.Ready || methodSummary.ValidationError == "" {
		t.Fatalf("不支持方法的摘要异常: %+v", methodSummary)
	}
	// headersErr 保存头模板类型无法解码为对象的解析错误。
	_, headersErr := ParseAPIConfig(`{"url":"https://example.test","headers":123}`)
	if headersErr == nil {
		t.Fatal("数字头模板未被拒绝")
	}
	// paramsErr 保存参数模板类型无法解码为对象的解析错误。
	_, paramsErr := ParseAPIConfig(`{"url":"https://example.test","params":123}`)
	if paramsErr == nil {
		t.Fatal("数字参数模板未被拒绝")
	}
}
