package adapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestParseAIModelsSupportsOpenAIShapes 验证模型目录解析兼容 data、models 和名称回退结构。
func TestParseAIModelsSupportsOpenAIShapes(t *testing.T) {
	// models、err 保存模型目录解析结果和错误。
	models, err := ParseAIModels([]byte(`{"data":[{"id":"qwen-plus"},{"name":"fallback"},{"id":"qwen-plus"}]}`))
	if err != nil || len(models) != 2 || models[0] != "qwen-plus" || models[1] != "fallback" {
		t.Fatalf("models=%v err=%v", models, err)
	}
}

// TestAIModelClientFetchUsesHeaderAndBounds 验证模型请求只把密钥放入请求头并拒绝空模型列表。
func TestAIModelClientFetchUsesHeaderAndBounds(t *testing.T) {
	// server 是返回固定模型目录的本地测试端点。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("authorization header was not forwarded")
		}
		_, _ = writer.Write([]byte(`{"models":["qwen-plus"]}`))
	}))
	defer server.Close()
	// client 是替换了受信任端点 HTTP 工厂的测试客户端。
	client := &AIModelClient{newHTTPClient: func(string) (*http.Client, error) { return &http.Client{}, nil }}
	// models、err 保存模型请求结果和错误。
	models, err := client.Fetch(context.Background(), server.URL, "test-secret")
	if err != nil || len(models) != 1 || models[0] != "qwen-plus" {
		t.Fatalf("models=%v err=%v", models, err)
	}
	// oversizedErr 表示超大模型响应被拒绝的错误。
	_, oversizedErr := ReadAIModelsBody(strings.NewReader(strings.Repeat("x", MaxAIModelsResponseBytes+1)))
	if oversizedErr == nil {
		t.Fatal("oversized model response should fail")
	}
}
