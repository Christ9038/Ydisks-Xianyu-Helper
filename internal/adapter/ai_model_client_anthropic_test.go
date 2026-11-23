package adapter

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAIModelClientAnthropic 验证 Anthropic 协议的模型目录读取与连接测试：请求路径、密钥头、响应解析与错误分类文案。
func TestAIModelClientAnthropic(t *testing.T) {
	// paths 记录请求路径；status 是消息接口返回的状态码，0 表示成功。
	var paths []string
	// status 是消息接口要返回的状态码，0 表示成功。
	status := 0
	// server 是记录请求路径并按 status 响应的本地 Anthropic 端点。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-Api-Key") != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-a","type":"model","display_name":"A","created_at":"2026-01-01T00:00:00Z"},{"id":"claude-b","type":"model","display_name":"B","created_at":"2026-01-01T00:00:00Z"}],"has_more":false,"first_id":"claude-a","last_id":"claude-b"}`))
			return
		}
		if status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"x","message":"远端正文不应泄漏"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","model":"m","stop_reason":"end_turn","content":[{"type":"text","text":"你好呀"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()
	// client 使用本地测试服务器的 HTTP 客户端；ctx 是共用上下文。
	factory := func(string) (*http.Client, error) { return server.Client(), nil }
	// client 是使用本地受控 HTTP 客户端工厂的模型客户端。
	client := &AIModelClient{newHTTPClient: factory, newTestHTTPClient: factory}
	// ctx 是测试用的后台上下文。
	ctx := context.Background()

	// models 是读取到的模型列表；err 是读取错误，应为空。
	models, err := client.Fetch(ctx, server.URL+"/v1", "secret", "anthropic")
	if err != nil || len(models) != 2 || models[0] != "claude-a" || paths[0] != "/v1/models" {
		t.Fatalf("models=%v paths=%v err=%v", models, paths, err)
	}
	if _, err = client.Fetch(ctx, server.URL, "wrong", "anthropic"); err == nil || !strings.Contains(err.Error(), "HTTP 401") || strings.Contains(err.Error(), "bad key") {
		t.Fatalf("鉴权失败应只含状态码: %v", err)
	}
	// result 是连接测试结果；err 是测试错误，应为空。
	result, err := client.TestConnection(ctx, server.URL, "secret", "claude-a", "anthropic")
	if err != nil || result.Reply != "你好呀" || result.Model != "claude-a" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// code 是模拟上游返回的状态码；want 是期望出现在错误文案中的分类说明。
	for code, want := range map[int]string{404: "模型不存在", 429: "额度或请求频率受限", 500: "HTTP 500", 403: "认证失败"} {
		status = code
		if _, err = client.TestConnection(ctx, server.URL, "secret", "claude-a", "anthropic"); err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "远端正文") {
			t.Fatalf("%d: err=%v want %q", code, err, want)
		}
	}
	if _, err = client.TestConnection(ctx, server.URL, "wrong", "claude-a", "anthropic"); err == nil || !strings.Contains(err.Error(), "认证失败") {
		t.Fatalf("错误密钥: %v", err)
	}
}
