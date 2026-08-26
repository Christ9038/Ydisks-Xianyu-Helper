package server

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestParseImportedOrdersCoversMultipartAndRawEntrances 覆盖订单导入的 multipart 与原始请求入口。
func TestParseImportedOrdersCoversMultipartAndRawEntrances(t *testing.T) {
	// body 保存内存中的 multipart 请求体。
	var body bytes.Buffer
	// multipartWriter 负责生成带文件字段的表单边界。
	multipartWriter := multipart.NewWriter(&body)
	// filePart 保存上传文件字段的写入器。
	filePart, createErr := multipartWriter.CreateFormFile("file", "orders.json")
	if createErr != nil {
		t.Fatal(createErr)
	}
	// writeErr 保存上传文件内容写入错误。
	if _, writeErr := filePart.Write([]byte(`[{"order_id":"o1","status":"2"}]`)); writeErr != nil {
		t.Fatal(writeErr)
	}
	// closeErr 保存 multipart 边界关闭错误。
	closeErr := multipartWriter.Close()
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	// multipartRequest 保存带文件的订单导入请求。
	multipartRequest := httptest.NewRequest(http.MethodPost, "/orders/import", &body)
	multipartRequest.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	// multipartRows、multipartErr 保存上传文件解析结果。
	multipartRows, multipartErr := parseImportedOrders(httptest.NewRecorder(), multipartRequest)
	if multipartErr != nil || len(multipartRows) != 1 || multipartRows[0]["order_id"] != "o1" {
		t.Fatalf("multipart rows=%v err=%v", multipartRows, multipartErr)
	}
	// missingFileRequest 保存缺少 file 字段的 multipart 请求。
	missingFileRequest := httptest.NewRequest(http.MethodPost, "/orders/import", strings.NewReader("--bad--"))
	missingFileRequest.Header.Set("Content-Type", "multipart/form-data; boundary=bad")
	// missingFileErr 保存缺少文件字段的解析错误。
	if _, missingFileErr := parseImportedOrders(httptest.NewRecorder(), missingFileRequest); missingFileErr == nil {
		t.Fatal("missing multipart file should fail")
	}
	// rawRequest 保存不带 multipart 的 JSON 请求。
	rawRequest := httptest.NewRequest(http.MethodPost, "/orders/import", strings.NewReader(`{"order_id":"o2"}`))
	// rawRows、rawErr 保存原始 JSON 请求解析结果及错误。
	rawRows, rawErr := parseImportedOrders(httptest.NewRecorder(), rawRequest)
	if rawErr != nil || len(rawRows) != 1 || rawRows[0]["order_id"] != "o2" {
		t.Fatalf("raw rows=%v err=%v", rawRows, rawErr)
	}
}

// TestParseJSONOrderArrayCoversMalformedTrailers 覆盖订单 JSON 数组的边界、元素和尾随内容错误。
func TestParseJSONOrderArrayCoversMalformedTrailers(t *testing.T) {
	// malformedInputs 保存需要返回解析错误的数组样本。
	malformedInputs := []string{"", "{}", "[", "[{", "[] extra", "[{}"}
	// raw 表示当前待解析的 JSON 样本。
	for _, raw := range malformedInputs {
		// parseErr 保存当前 JSON 样本的解析错误。
		if _, parseErr := parseJSONOrderArray([]byte(raw)); parseErr == nil {
			t.Fatalf("malformed JSON %q should fail", raw)
		}
	}
	// rows、parseErr 保存合法空数组的解析结果。
	rows, parseErr := parseJSONOrderArray([]byte("[]"))
	if parseErr != nil || len(rows) != 0 {
		t.Fatalf("empty array rows=%v err=%v", rows, parseErr)
	}
}
