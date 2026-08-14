package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
	// decodeErr 表示错误响应 JSON 反序列化失败的原因。
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
