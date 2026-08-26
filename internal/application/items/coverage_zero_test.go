package items

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestItemsRemainingSmallBranches 验证批量预检、自动化收口、XML 文本和发布错误的剩余小分支。
func TestItemsRemainingSmallBranches(t *testing.T) {
	// previewService 是绑定账号归属替身的批量预检服务。
	previewService, previewErr := NewBatchPreviewService(batchPreviewOwnershipFake{cookieOwned: "account-1"}, batchPreviewImageFake{})
	if previewErr != nil {
		t.Fatal(previewErr)
	}
	// owned、ownedErr 保存账号归属检查结果。
	owned, ownedErr := previewService.CookieOwned(context.Background(), 7, " account-1 ")
	if ownedErr != nil || !owned {
		t.Fatalf("账号归属检查异常: owned=%v err=%v", owned, ownedErr)
	}
	// invalidPreviewService 验证空服务的安全错误分支。
	var invalidPreviewService *BatchPreviewService
	// invalidErr 保存空预检服务返回的初始化错误。
	if _, invalidErr := invalidPreviewService.CookieOwned(context.Background(), 7, "account-1"); invalidErr == nil {
		t.Fatal("空预检服务应返回错误")
	}
	// localService 是只执行自动化规则收口的本地服务。
	localService, localErr := NewBatchLocalPublishService(&batchCompletionRepositoryFake{}, &batchPublishedItemRepositoryFake{}, &batchPublishRuleRepositoryFake{})
	if localErr != nil {
		t.Fatal(localErr)
	}
	// ensureErr 保存空自动化配置的幂等收口结果。
	ensureErr := localService.EnsureAutomationRules(context.Background(), 7, BatchRow{AutomationJSON: `{}`}, &BatchPublishResult{ItemID: "item-1"})
	if ensureErr != nil {
		t.Fatalf("空自动化配置收口失败: %v", ensureErr)
	}
	// token 保存批次租约令牌生成结果。
	token := randomBatchToken()
	if len(token) == 0 {
		t.Fatal("批次租约令牌不能为空")
	}
	// got 保存 XML 字符数据拼接后的文本。
	if got := xmlCharData(`<t>前</t><t>后&amp;符号</t>`); got != "前后&符号" {
		t.Fatalf("XML 字符数据拼接错误: %q", got)
	}
	// cause 是发布错误包装使用的基础错误。
	cause := errors.New("发布失败")
	// publishErr 保存带基础错误链的发布错误。
	publishErr := &PublishError{Code: PublishErrorUnknown, Err: cause}
	if !errors.Is(publishErr, cause) || publishErr.Unwrap() != cause {
		t.Fatal("发布错误基础错误链未保留")
	}
	// nilPublishErr 验证空发布错误指针的安全文本和错误链。
	var nilPublishErr *PublishError
	if nilPublishErr.Error() != string(PublishErrorUnknown) || nilPublishErr.Unwrap() != nil {
		t.Fatal("空发布错误语义异常")
	}
	if !strings.Contains(publishErr.Error(), string(PublishErrorUnknown)) {
		t.Fatal("发布错误默认分类文本异常")
	}
}

// TestBatchPreviewFieldNormalization 覆盖批量表格表头、字段值和邮费模式归一化。
func TestBatchPreviewFieldNormalization(t *testing.T) {
	// headers 保存中英文混合表头，验证别名映射为稳定字段名。
	headers := normalizeHeaders([]string{" 账号 ID ", "商品名称", "价格", "未知字段"})
	if headers[0] != "cookie_id" || headers[1] != "title" || headers[2] != "price" || headers[3] != "未知字段" {
		t.Fatalf("headers=%v", headers)
	}
	// aliases 保存所有主要表头别名，确保导入模板字段映射稳定。
	aliases := map[string]string{
		"账号": "cookie_id", "商品详情": "description", "原价": "original_price", "库存": "quantity", "邮费模式": "postage_mode", "邮费": "postage",
		"图片": "images", "类目ID": "category_id", "类目": "category_name", "频道类目ID": "channel_category_id", "淘宝类目ID": "tb_category_id",
		"付款后自动发货": "paid_delivery_enabled", "付款后发送的卡密": "paid_delivery_contents", "评价后发送赠品": "review_gift_enabled", "评价后发送的卡密": "review_gift_contents",
		"超时未评价时提醒": "review_request_enabled", "发货几小时后提醒": "review_request_after_hours", "提醒内容": "review_request_message", "最多提醒几次": "review_request_max_attempts", "求评价延迟秒": "review_request_delay_seconds",
	}
	// alias、want 表示当前遍历中的表头别名及稳定字段名。
	for alias, want := range aliases {
		// got 保存当前别名归一化后的字段名。
		if got := normalizeHeader(alias); got != want {
			t.Errorf("normalizeHeader(%q)=%q want %q", alias, got, want)
		}
	}
	// fields、nonEmpty 保存一行表格映射结果及是否存在有效内容。
	fields, nonEmpty := rowMap(headers, []string{"cid", "商品", "12.5", ""})
	if !nonEmpty || fields["cookie_id"] != "cid" || fields["price"] != "12.5" {
		t.Fatalf("fields=%v nonEmpty=%v", fields, nonEmpty)
	}
	// emptyFields、emptyNonEmpty 验证空行不会被识别为有效数据。
	emptyFields, emptyNonEmpty := rowMap([]string{"title", "price"}, []string{" ", ""})
	if emptyNonEmpty || emptyFields["title"] != "" {
		t.Fatalf("empty fields=%v nonEmpty=%v", emptyFields, emptyNonEmpty)
	}
	// first 保存按候选字段优先级提取的文本。
	first := firstString(map[string]any{"a": " ", "b": 12.5}, "a", "b")
	if first != "12.5" {
		t.Fatalf("first=%q", first)
	}
	// sharedCell、inlineCell、plainCell 保存 XLSX 不同单元格编码形式。
	sharedCell := xlsxCellValue(xlsxCell{Type: "s", Value: "1"}, []string{"zero", "shared"})
	// inlineCell 保存 XLSX 内联字符串单元格的裁剪结果。
	inlineCell := xlsxCellValue(xlsxCell{Type: "inlineStr", InlineStr: " inline "}, nil)
	// plainCell 保存 XLSX 普通单元格的裁剪结果。
	plainCell := xlsxCellValue(xlsxCell{Value: " plain "}, nil)
	if sharedCell != "shared" || inlineCell != "inline" || plainCell != "plain" {
		t.Fatalf("xlsx cells=%q/%q/%q", sharedCell, inlineCell, plainCell)
	}
	// values 保存常见表格类型到文本的转换结果。
	values := []struct {
		input any
		want  string
	}{{nil, ""}, {"text", "text"}, {float64(1.2), "1.2"}, {float32(1.5), "1.5"}, {int(2), "2"}, {int64(3), "3"}, {true, "true"}}
	// item 表示当前待验证的表格值样例。
	for _, item := range values {
		// got 保存当前表格值转换后的文本。
		if got := stringValue(item.input); got != item.want {
			t.Errorf("stringValue(%v)=%q want %q", item.input, got, item.want)
		}
	}
	// postageCases 保存平台邮费模式的用户输入映射。
	postageCases := map[string]string{"包邮": "free", "free_shipping": "free", "固定邮费": "fixed", "一口价邮费": "fixed", "custom": "custom"}
	// raw、want 表示当前遍历中的邮费模式及预期值。
	for raw, want := range postageCases {
		// got 保存当前邮费模式归一化后的平台值。
		if got := normalizePostageMode(raw); got != want {
			t.Errorf("normalizePostageMode(%q)=%q want %q", raw, got, want)
		}
	}
}
