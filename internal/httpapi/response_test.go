package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCodeForStatusCoversHTTPContract 验证所有标准 HTTP 状态码都映射到稳定错误码，未知状态降级为内部错误。
func TestCodeForStatusCoversHTTPContract(t *testing.T) {
	// cases 保存 HTTP 状态码与统一错误码的契约映射。
	cases := []struct {
		// status 是待转换的 HTTP 状态码。
		status int
		// want 是稳定机器错误码。
		want string
	}{
		{http.StatusBadRequest, CodeBadRequest}, {http.StatusUnauthorized, CodeUnauthorized},
		{http.StatusForbidden, CodeForbidden}, {http.StatusNotFound, CodeNotFound},
		{http.StatusConflict, CodeConflict}, {http.StatusTooManyRequests, CodeTooManyRequests},
		{http.StatusNotImplemented, CodeNotImplemented}, {http.StatusBadGateway, CodeBadGateway},
		{http.StatusServiceUnavailable, CodeServiceUnavailable}, {http.StatusInternalServerError, CodeInternalError},
	}
	// testCase 表示当前待验证的 HTTP 状态码映射样例。
	for _, testCase := range cases {
		// got 是当前 HTTP 状态码对应的统一错误码。
		got := CodeForStatus(testCase.status)
		if got != testCase.want {
			t.Fatalf("status=%d code=%q want=%q", testCase.status, got, testCase.want)
		}
	}
}

// TestWriteErrorUsesStatusDefault 验证未提供错误码时响应会根据状态码填充默认码。
func TestWriteErrorUsesStatusDefault(t *testing.T) {
	// recorder 是捕获统一错误响应的测试记录器。
	recorder := httptest.NewRecorder()
	WriteError(recorder, http.StatusNotFound, "", "资源不存在", "")
	// response 是反序列化后的统一错误 DTO。
	var response ErrorResponse
	// decodeErr 保存统一错误响应反序列化错误。
	if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &response); decodeErr != nil {
		t.Fatalf("decode response: %v", decodeErr)
	}
	if response.Code != CodeNotFound || response.Message != "资源不存在" || response.RequestID != "" {
		t.Fatalf("response=%+v", response)
	}
}

// TestWriteErrorDetails 验证统一错误 DTO 可携带恢复所需的结构化详情。
func TestWriteErrorDetails(t *testing.T) {
	// recorder 是捕获错误响应的测试记录器。
	recorder := httptest.NewRecorder()
	// details 是远端操作完成后供客户端核对的商品信息。
	details := map[string]any{"item_id": "remote-item", "item_url": "https://example/item/remote-item"}
	WriteErrorDetails(recorder, http.StatusInternalServerError, "remote_published_local_save_failed", "本地保存失败", "req-1", details)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	// response 是反序列化后的统一错误 DTO。
	var response ErrorResponse
	// decodeErr 记录当前操作失败原因响应 JSON 反序列化失败的原因。
	if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &response); decodeErr != nil {
		t.Fatalf("decode error response: %v", decodeErr)
	}
	if response.Code != "remote_published_local_save_failed" || response.Message != "本地保存失败" || response.RequestID != "req-1" {
		t.Fatalf("response=%+v", response)
	}
	if response.Details["item_id"] != "remote-item" || response.Details["item_url"] != "https://example/item/remote-item" {
		t.Fatalf("details=%+v", response.Details)
	}
}
