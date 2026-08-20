package server

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// buildMultipartFileRequest 构造指定文件字节数的真实 multipart 请求，用于验证文件上限与请求包装开销分离。
func buildMultipartFileRequest(t *testing.T, fieldName, filename string, size int64) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	// body 保存 multipart writer 编码出的完整请求字节。
	var body bytes.Buffer
	// writer 负责生成真实 boundary、文件头和结束分隔符。
	writer := multipart.NewWriter(&body)
	// fileWriter、createErr 保存目标文件字段写入器及其创建错误。
	fileWriter, createErr := writer.CreateFormFile(fieldName, filename)
	if createErr != nil {
		t.Fatalf("create multipart file: %v", createErr)
	}
	// content 保存精确指定大小的文件内容；重复字节避免测试数据额外引入编码差异。
	content := bytes.Repeat([]byte{'x'}, int(size))
	// writeErr 保存向 multipart 文件字段写入精确大小测试字节时的错误。
	if _, writeErr := fileWriter.Write(content); writeErr != nil {
		t.Fatalf("write multipart file: %v", writeErr)
	}
	// closeErr 保存写出 multipart 结束 boundary 时的错误。
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("close multipart file: %v", closeErr)
	}
	// request 是带完整 Content-Type boundary 的 HTTP 上传请求。
	request := httptest.NewRequest(http.MethodPost, "/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	// recorder 保存共享 multipart 解析器分类错误时的响应。
	recorder := httptest.NewRecorder()
	return request, recorder
}

// TestMultipartFileLimitsLeaveMetadataSpace 验证恰好达到聊天 10 MiB、订单 32 MiB 文件上限时仍可通过 multipart 解析，超过文件上限则由后续文件读取拒绝。
func TestMultipartFileLimitsLeaveMetadataSpace(t *testing.T) {
	// chatExactRequest、chatExactRecorder 保存恰好 10 MiB 聊天图片请求及其解析响应。
	chatExactRequest, chatExactRecorder := buildMultipartFileRequest(t, "image", "chat.png", maxChatImageBytes)
	if !parseMultipartRequest(chatExactRecorder, chatExactRequest, maxChatImageMultipartBytes, maxChatImageMultipartBytes, "图片上传内容不能超过 11 MiB") {
		t.Fatalf("exact 10 MiB chat image rejected: %s", chatExactRecorder.Body.String())
	}
	// chatExactFile、chatExactHeader、chatExactErr 保存表单中精确上限图片及其读取错误。
	chatExactFile, chatExactHeader, chatExactErr := chatExactRequest.FormFile("image")
	if chatExactErr != nil {
		t.Fatalf("read exact chat image form field: %v", chatExactErr)
	}
	defer chatExactFile.Close()
	// chatExactData、chatExactReadErr 保存带一字节探测读取后的图片内容及读取错误。
	chatExactData, chatExactReadErr := io.ReadAll(io.LimitReader(chatExactFile, maxChatImageBytes+1))
	if chatExactReadErr != nil || chatExactHeader.Filename != "chat.png" || len(chatExactData) != int(maxChatImageBytes) {
		t.Fatalf("exact chat image filename=%q bytes=%d err=%v", chatExactHeader.Filename, len(chatExactData), chatExactReadErr)
	}

	// chatOverRequest、chatOverRecorder 保存超出单图片上限一字节但仍小于总 multipart 上限的请求。
	chatOverRequest, chatOverRecorder := buildMultipartFileRequest(t, "image", "chat-over.png", maxChatImageBytes+1)
	if !parseMultipartRequest(chatOverRecorder, chatOverRequest, maxChatImageMultipartBytes, maxChatImageMultipartBytes, "图片上传内容不能超过 11 MiB") {
		t.Fatalf("chat file above 10 MiB should reach file validation: %s", chatOverRecorder.Body.String())
	}
	// chatOverFile、chatOverErr 保存超限图片字段及其读取错误。
	chatOverFile, _, chatOverErr := chatOverRequest.FormFile("image")
	if chatOverErr != nil {
		t.Fatalf("read oversized chat image form field: %v", chatOverErr)
	}
	defer chatOverFile.Close()
	// chatOverData、chatOverReadErr 保存含探测字节的超限图片内容及读取错误。
	chatOverData, chatOverReadErr := io.ReadAll(io.LimitReader(chatOverFile, maxChatImageBytes+1))
	if chatOverReadErr != nil || len(chatOverData) <= int(maxChatImageBytes) {
		t.Fatalf("chat file limit not detectable: bytes=%d err=%v", len(chatOverData), chatOverReadErr)
	}

	// orderExactRequest、orderExactRecorder 保存恰好 32 MiB 订单文件请求及其解析响应。
	orderExactRequest, orderExactRecorder := buildMultipartFileRequest(t, "file", "orders.csv", maxOrderImportBytes)
	if !parseMultipartRequest(orderExactRecorder, orderExactRequest, maxOrderImportMultipartBytes, maxOrderImportBytes, "上传内容不能超过 33 MiB") {
		t.Fatalf("exact 32 MiB order file rejected: %s", orderExactRecorder.Body.String())
	}
	// orderExactFile、orderExactErr 保存订单文件字段及其读取错误。
	orderExactFile, _, orderExactErr := orderExactRequest.FormFile("file")
	if orderExactErr != nil {
		t.Fatalf("read exact order file form field: %v", orderExactErr)
	}
	defer orderExactFile.Close()
	// orderExactData、orderExactReadErr 保存带一字节探测读取后的订单文件内容及读取错误。
	orderExactData, orderExactReadErr := io.ReadAll(io.LimitReader(orderExactFile, maxOrderImportBytes+1))
	if orderExactReadErr != nil || len(orderExactData) != int(maxOrderImportBytes) {
		t.Fatalf("exact order file bytes=%d err=%v", len(orderExactData), orderExactReadErr)
	}

	// orderOverRequest、orderOverRecorder 保存超出订单文件上限一字节但仍可由 multipart 总请求限制接收的请求。
	orderOverRequest, orderOverRecorder := buildMultipartFileRequest(t, "file", "orders-over.csv", maxOrderImportBytes+1)
	if !parseMultipartRequest(orderOverRecorder, orderOverRequest, maxOrderImportMultipartBytes, maxOrderImportBytes, "上传内容不能超过 33 MiB") {
		t.Fatalf("order file above 32 MiB should reach file validation: %s", orderOverRecorder.Body.String())
	}
	// orderOverFile、orderOverErr 保存超限订单文件字段及其读取错误。
	orderOverFile, _, orderOverErr := orderOverRequest.FormFile("file")
	if orderOverErr != nil {
		t.Fatalf("read oversized order file form field: %v", orderOverErr)
	}
	defer orderOverFile.Close()
	// orderOverData、orderOverReadErr 保存含探测字节的超限订单文件内容及读取错误。
	orderOverData, orderOverReadErr := io.ReadAll(io.LimitReader(orderOverFile, maxOrderImportBytes+1))
	if orderOverReadErr != nil || len(orderOverData) <= int(maxOrderImportBytes) {
		t.Fatalf("order file limit not detectable: bytes=%d err=%v", len(orderOverData), orderOverReadErr)
	}
}

// TestMultipartParseErrorsRemainClassified 验证格式错误、boundary 损坏和总请求超限返回不同错误，避免统一提示掩盖根因。
func TestMultipartParseErrorsRemainClassified(t *testing.T) {
	// wrongMediaRequest、wrongMediaRecorder 保存非 multipart 上传请求及其分类错误响应。
	wrongMediaRequest := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(`{}`))
	wrongMediaRequest.Header.Set("Content-Type", "application/json")
	// wrongMediaRecorder 保存非 multipart 请求被拒绝后的统一错误响应。
	wrongMediaRecorder := httptest.NewRecorder()
	if parseMultipartRequest(wrongMediaRecorder, wrongMediaRequest, maxChatImageMultipartBytes, maxChatImageMultipartBytes, "图片上传内容不能超过 11 MiB") {
		t.Fatal("non-multipart request unexpectedly parsed")
	}
	if !strings.Contains(wrongMediaRecorder.Body.String(), "请求格式错误") {
		t.Fatalf("wrong media response=%s", wrongMediaRecorder.Body.String())
	}

	// brokenRequest、brokenRecorder 保存声明错误 boundary 的 multipart 请求及其分类响应。
	brokenRequest := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("--different\r\n"))
	brokenRequest.Header.Set("Content-Type", "multipart/form-data; boundary=expected")
	// brokenRecorder 保存 boundary 与请求体不一致时的统一错误响应。
	brokenRecorder := httptest.NewRecorder()
	if parseMultipartRequest(brokenRecorder, brokenRequest, maxChatImageMultipartBytes, maxChatImageMultipartBytes, "图片上传内容不能超过 11 MiB") {
		t.Fatal("broken boundary request unexpectedly parsed")
	}
	if !strings.Contains(brokenRecorder.Body.String(), "上传表单损坏") {
		t.Fatalf("broken boundary response=%s", brokenRecorder.Body.String())
	}

	// oversizedRequest、oversizedRecorder 保存有效 multipart 但总体超过聊天上传总配额的请求及其分类响应。
	oversizedRequest, oversizedRecorder := buildMultipartFileRequest(t, "image", "too-large.png", maxChatImageMultipartBytes)
	if parseMultipartRequest(oversizedRecorder, oversizedRequest, maxChatImageMultipartBytes, maxChatImageMultipartBytes, "图片上传内容不能超过 11 MiB") {
		t.Fatal("oversized multipart request unexpectedly parsed")
	}
	if !strings.Contains(oversizedRecorder.Body.String(), "图片上传内容不能超过 11 MiB") {
		t.Fatalf("oversized response=%s", oversizedRecorder.Body.String())
	}
}
