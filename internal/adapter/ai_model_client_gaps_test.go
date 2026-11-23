package adapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAIModelClientFetchErrorPaths 覆盖模型目录读取的参数校验、客户端工厂失败、HTTP 失败和空目录路径。
func TestAIModelClientFetchErrorPaths(t *testing.T) {
	// ctx 是所有调用共用的后台上下文。
	ctx := context.Background()
	// okFactory 返回不做地址限制的普通客户端，模拟已通过端点策略。
	okFactory := func(string) (*http.Client, error) { return &http.Client{}, nil }
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := (&AIModelClient{newHTTPClient: okFactory}).Fetch(ctx, "  /", "k", "openai"); err == nil {
		t.Fatal("空地址必须拒绝")
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := (&AIModelClient{}).Fetch(ctx, "http://x", "k", "openai"); err == nil {
		t.Fatal("未装配客户端工厂必须拒绝")
	}
	// nilClient 是未初始化的空指针客户端。
	var nilClient *AIModelClient
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := nilClient.Fetch(ctx, "http://x", "k", "openai"); err == nil {
		t.Fatal("空客户端必须拒绝")
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := (&AIModelClient{newHTTPClient: okFactory}).Fetch(ctx, "http://x\x7f", "k", "openai"); err == nil {
		t.Fatal("非法控制字符地址应在构造请求时失败")
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := (&AIModelClient{newHTTPClient: func(string) (*http.Client, error) { return nil, errors.New("blocked") }}).Fetch(ctx, "http://x", "", "openai"); err == nil || !strings.Contains(err.Error(), "地址无效") {
		t.Fatalf("工厂失败应包装为地址无效: %v", err)
	}
	// responder 决定本地端点本次返回的状态码与正文。
	var status = http.StatusOK
	// body 是本地端点返回的响应正文。
	var body = `{"data":[]}`
	// server 是按 status/body 回复的本地模型目录端点。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("空密钥不应发送 Authorization")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	// client 是指向本地端点的测试客户端。
	client := &AIModelClient{newHTTPClient: okFactory}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := client.Fetch(ctx, server.URL, "", "openai"); err == nil || !strings.Contains(err.Error(), "模型列表为空") {
		t.Fatalf("空目录: %v", err)
	}
	status, body = http.StatusBadGateway, "bad"
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := client.Fetch(ctx, server.URL, "", "openai"); err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("HTTP 失败: %v", err)
	}
	status, body = http.StatusOK, "{"
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := client.Fetch(ctx, server.URL, "", "openai"); err == nil {
		t.Fatal("非法 JSON 应失败")
	}
	status, body = http.StatusOK, strings.Repeat("x", MaxAIModelsResponseBytes+1)
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := client.Fetch(ctx, server.URL, "", "openai"); err == nil {
		t.Fatal("超大响应应失败")
	}
	// cancelled 是已取消上下文，模拟传输层失败。
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := client.Fetch(cancelled, server.URL, "", "openai"); err == nil || !strings.Contains(err.Error(), "读取模型失败") {
		t.Fatalf("传输失败: %v", err)
	}
}

// TestAIModelClientTestConnectionGuards 覆盖连接测试的参数校验、工厂回退与传输错误分类。
func TestAIModelClientTestConnectionGuards(t *testing.T) {
	// ctx 是所有调用共用的后台上下文。
	ctx := context.Background()
	// okFactory 返回普通客户端。
	okFactory := func(string) (*http.Client, error) { return &http.Client{}, nil }
	// replyServer 返回合法的 chat completion 响应。
	replyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}]}`))
	}))
	defer replyServer.Close()
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := (&AIModelClient{newTestHTTPClient: okFactory}).TestConnection(ctx, " ", "k", "m", "openai"); err == nil {
		t.Fatal("空地址必须拒绝")
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := (&AIModelClient{newTestHTTPClient: okFactory}).TestConnection(ctx, "http://x", "k", " ", "openai"); err == nil {
		t.Fatal("空模型必须拒绝")
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := (&AIModelClient{}).TestConnection(ctx, "http://x", "k", "m", "openai"); err == nil {
		t.Fatal("未装配必须拒绝")
	}
	// nilClient 是未初始化的空指针客户端。
	var nilClient *AIModelClient
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := nilClient.TestConnection(ctx, "http://x", "k", "m", "openai"); err == nil {
		t.Fatal("空客户端必须拒绝")
	}
	// 仅装配常规工厂时应回退使用它。
	result, err := (&AIModelClient{newHTTPClient: okFactory}).TestConnection(ctx, replyServer.URL, "", "m", "openai")
	if err != nil || result.Reply != "hi" {
		t.Fatalf("回退工厂: %+v %v", result, err)
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := (&AIModelClient{newTestHTTPClient: func(string) (*http.Client, error) { return nil, errors.New("blocked") }}).TestConnection(ctx, "http://x", "k", "m", "openai"); err == nil || !strings.Contains(err.Error(), "地址无效") {
		t.Fatalf("工厂失败: %v", err)
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := (&AIModelClient{newTestHTTPClient: okFactory}).TestConnection(ctx, "http://x\x7f", "k", "m", "openai"); err == nil {
		t.Fatal("非法地址应在构造请求时失败")
	}
	// cancelled 是已取消上下文。
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := (&AIModelClient{newTestHTTPClient: okFactory}).TestConnection(cancelled, replyServer.URL, "k", "m", "openai"); err == nil || !strings.Contains(err.Error(), "连接失败") {
		t.Fatalf("取消: %v", err)
	}
	// expired 是已超过截止时间的上下文，应归类为连接超时。
	expired, cancelExpired := context.WithTimeout(ctx, 0)
	defer cancelExpired()
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := (&AIModelClient{newTestHTTPClient: okFactory}).TestConnection(expired, replyServer.URL, "k", "m", "openai"); err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("超时: %v", err)
	}
	// bigServer 返回超大成功响应，覆盖读取失败分支。
	bigServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", MaxAIModelsResponseBytes+1)))
	}))
	defer bigServer.Close()
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := (&AIModelClient{newTestHTTPClient: okFactory}).TestConnection(ctx, bigServer.URL, "k", "m", "openai"); err == nil || !strings.Contains(err.Error(), "读取 AI 服务响应失败") {
		t.Fatalf("超大: %v", err)
	}
	// badServer 返回无法解析的成功响应。
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("nope")) }))
	defer badServer.Close()
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := (&AIModelClient{newTestHTTPClient: okFactory}).TestConnection(ctx, badServer.URL, "k", "m", "openai"); err == nil {
		t.Fatal("无法解析应失败")
	}
}

// TestExtractChatReplyShapes 覆盖回复提取的全部兼容形状。
func TestExtractChatReplyShapes(t *testing.T) {
	// cases 是响应正文到期望文本（或错误）的映射用例。
	cases := []struct {
		/* 用例名 */ name string
		/* 响应正文 */ raw string
		/* 期望文本 */ want string
		/* 是否期望错误 */ wantErr bool
	}{
		{"非法", "x", "", true},
		{"无候选", `{"choices":[]}`, "", true},
		{"文本块数组", `{"choices":[{"message":{"content":[{"text":" a "},{"text":""},{"text":"b"}]}}]}`, "a\nb", false},
		{"旧式text", `{"choices":[{"text":" old "}]}`, "old", false},
		{"仅推理", `{"choices":[{"message":{"reasoning_content":"think"}}]}`, "模型已返回推理结果", false},
		{"无可显示", `{"choices":[{"message":{"content":42}}]}`, "模型已成功响应（未返回可显示文本）", false},
	}
	// tc 当前表驱动用例，包含输入与期望输出。
	for _, tc := range cases {
		// got、err 是提取结果与错误。
		got, err := extractChatReply([]byte(tc.raw))
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Fatalf("%s: got=%q err=%v", tc.name, got, err)
		}
	}
}
