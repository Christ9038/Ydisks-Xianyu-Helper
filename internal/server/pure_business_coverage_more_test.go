package server

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

// TestOrderImportPureHelpersCoverRequestAndValueBranches 验证订单导入 raw 请求、字段转换和多种标量值分支。
func TestOrderImportPureHelpersCoverRequestAndValueBranches(t *testing.T) {
	// request 保存 JSON raw body 订单导入请求。
	request := httptest.NewRequest("POST", "/orders/import", bytes.NewBufferString(`[{"订单号":"order-1","数量":2,"状态":true}]`))
	// response 保存导入解析所需的 HTTP 响应写入器。
	response := httptest.NewRecorder()
	// rows、parseErr 保存 raw 请求解析出的订单行及错误。
	rows, parseErr := parseImportedOrders(response, request)
	if parseErr != nil || len(rows) != 1 || rows[0]["order_id"] != "order-1" {
		t.Fatalf("raw 订单解析结果=%v err=%v", rows, parseErr)
	}
	// invalidRequest 保存无法按 JSON 或表格解析的 raw 请求。
	invalidRequest := httptest.NewRequest("POST", "/orders/import", bytes.NewBufferString("\x00"))
	// invalidResponse 保存错误请求的响应写入器。
	invalidResponse := httptest.NewRecorder()
	// invalidErr 保存非法 raw 导入的解析错误。
	if _, invalidErr := parseImportedOrders(invalidResponse, invalidRequest); invalidErr == nil {
		t.Fatal("非法 raw 导入应返回错误")
	}
	if stringFromAny(nil) != "" || stringFromAny("text") != "text" || stringFromAny(float64(1.25)) != "1.25" || stringFromAny(3) != "3" || stringFromAny(int64(4)) != "4" || stringFromAny(true) != "true" {
		t.Fatal("标量字段转换异常")
	}
	// orderFields 保存按标准字段名读取订单导入值的字段集合。
	orderFields := map[string]any{"order_id": " ", "amount": 12.5, "status": false}
	if firstImportString(orderFields, "order_id", "amount") != "12.5" || firstImportString(orderFields, "status") != "false" || firstImportString(orderFields, "missing") != "" {
		t.Fatal("订单首个非空字段读取异常")
	}
}

// TestPublishImageAndSheetHelpersCoverPathAndHeaderBranches 验证铺货图片路径、文件名和表头归一化边界。
func TestPublishImageAndSheetHelpersCoverPathAndHeaderBranches(t *testing.T) {
	if pathBaseFromURL("https://example.test/image.png?x=1") != "image.png" || pathBaseFromURL("https://example.test/") == "" || pathBaseFromURL("") == "" || pathBaseFromURL("?") == "" {
		t.Fatal("图片 URL 文件名提取异常")
	}
	if !isHTTPURL(" HTTPS://example.test/image.png ") || isHTTPURL("ftp://example.test/image.png") {
		t.Fatal("图片 URL 协议识别异常")
	}
	// rel、relErr 保存安全图片路径的归一化结果和错误。
	if rel, relErr := safeZipPath("folder/image.png"); relErr != nil || rel != "folder/image.png" {
		t.Fatalf("安全图片路径=%q err=%v", rel, relErr)
	}
	// unsafeErr 保存路径逃逸校验错误。
	if _, unsafeErr := safeZipPath("../secret.txt"); unsafeErr == nil {
		t.Fatal("路径逃逸应被拒绝")
	}
	// split 保存混合分隔符图片引用的拆分结果。
	if split := splitImageRefs("a.png； b.png\nc.png;;"); len(split) != 3 || split[1] != "b.png" {
		t.Fatalf("图片引用拆分=%v", split)
	}
	if normalizePublishHeader("商品标题") != "title" || normalizePublishHeader("review_request_delay_seconds") != "review_request_delay_seconds" || normalizePublishHeader("未知字段") != "未知字段" {
		t.Fatal("铺货表头归一化异常")
	}
	// publishMap 保存铺货表格行映射结果和是否存在有效字段。
	publishMap, nonEmpty := publishRowToMap([]string{"title", "price"}, []string{" 标题 ", ""})
	if !nonEmpty || publishMap["title"] != "标题" || publishMap["price"] != "" {
		t.Fatalf("铺货行映射=%v nonEmpty=%v", publishMap, nonEmpty)
	}
}
