package settings

import (
	"context"
	"errors"
	"testing"
)

// TestTestAIConnectionFailureBranches 覆盖连接测试在身份、客户端、设置读取、密钥读取与审计各阶段失败时的返回。
func TestTestAIConnectionFailureBranches(t *testing.T) {
	// ctx 是测试上下文。
	ctx := context.Background()
	// boom 是存储层固定失败。
	boom := errors.New("storage down")
	// invalidService 的用户身份非法。
	service := NewService(&settingsRepositoryFake{}, &modelClientFake{})
	// err 保存下方被测调用返回的错误，用于断言该路径按预期成功或失败。
	if _, err := service.TestAIConnection(ctx, 0, "", "", "", ""); !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("invalid user: %v", err)
	}
	// noClient 没有装配模型客户端。
	noClient := NewService(&settingsRepositoryFake{}, nil)
	// err 保存下方被测调用返回的错误，用于断言该路径按预期成功或失败。
	if _, err := noClient.TestAIConnection(ctx, 7, "", "", "", ""); err == nil {
		t.Fatal("缺少模型客户端应失败")
	}
	// urlFail 读取已保存地址失败。
	urlFail := NewService(&settingsRepositoryFake{systemErr: boom}, &modelClientFake{})
	// err 保存下方被测调用返回的错误，用于断言该路径按预期成功或失败。
	if _, err := urlFail.TestAIConnection(ctx, 7, "", "k", "m", ""); !errors.Is(err, boom) {
		t.Fatalf("url lookup: %v", err)
	}
	// secretFail 读取受控 API Key 失败。
	secretFail := NewService(&settingsRepositoryFake{sensitiveKeys: []string{"ai_api_key"}, readSecretErr: boom}, &modelClientFake{})
	// err 保存下方被测调用返回的错误，用于断言该路径按预期成功或失败。
	if _, err := secretFail.TestAIConnection(ctx, 7, "https://x.test/v1", "", "m", ""); !errors.Is(err, boom) {
		t.Fatalf("secret read: %v", err)
	}
	// auditFail 临时 API Key 使用审计失败，必须阻止出站。
	client := &modelClientFake{}
	// auditFail 是下方语句引入的测试值，承载被测调用的结果或夹具，仅在当前测试内使用。
	auditFail := NewService(&settingsRepositoryFake{sensitiveKeys: []string{"ai_api_key"}, auditErr: boom}, client)
	// err 保存下方被测调用返回的错误，用于断言该路径按预期成功或失败。
	if _, err := auditFail.TestAIConnection(ctx, 7, "https://x.test/v1", "temp-key", "m", ""); !errors.Is(err, boom) || client.calls != 0 {
		t.Fatalf("audit: %v calls=%d", err, client.calls)
	}
	// modelFail 读取默认模型失败。
	modelFail := NewService(&settingsRepositoryFake{systemErr: boom, sensitiveKeys: []string{"ai_api_key"}}, &modelClientFake{})
	// err 保存下方被测调用返回的错误，用于断言该路径按预期成功或失败。
	if _, err := modelFail.TestAIConnection(ctx, 7, "https://x.test/v1", "temp-key", "", ""); !errors.Is(err, boom) {
		t.Fatalf("model lookup: %v", err)
	}
}

// TestTestAIConnectionDefaultsEndpoint 验证地址未保存时回退到内置默认兼容端点。
func TestTestAIConnectionDefaultsEndpoint(t *testing.T) {
	// client 记录最终使用的端点。
	client := &modelClientFake{}
	// service 的系统设置为空，密钥与模型由调用方显式提供。
	service := NewService(&settingsRepositoryFake{systemValues: map[string]string{}, sensitiveKeys: []string{"ai_api_key"}}, client)
	// err 保存下方被测调用返回的错误，用于断言该路径按预期成功或失败。
	if _, err := service.TestAIConnection(context.Background(), 7, "", "temp-key", "m", ""); err != nil {
		t.Fatal(err)
	}
	if client.testBaseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("endpoint=%q", client.testBaseURL)
	}
}
