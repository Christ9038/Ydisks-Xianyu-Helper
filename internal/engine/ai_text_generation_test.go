package engine

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// TestGenerateGlobalAIText 覆盖：缺存储、未配置 Key、成功、上游错误、空内容、地址被拦截。
func TestGenerateGlobalAIText(t *testing.T) {
	// ctx 是测试上下文。
	ctx := context.Background()
	// err 是缺少存储时的错误，应非空。
	if _, err := GenerateGlobalAIText(ctx, nil, "cid", "s", "u", 100); err == nil {
		t.Fatal("缺少存储应报错")
	}

	// store 是临时测试库。
	store, cleanup := newAIStore(t)
	defer cleanup()
	// err 是未配置 Key 时的错误，应为 ErrAINotConfigured。
	if _, err := GenerateGlobalAIText(ctx, store, "cid", "s", "u", 100); !errors.Is(err, ErrAINotConfigured) {
		t.Fatalf("未配置 Key 应返回 ErrAINotConfigured: %v", err)
	}

	// ok 是返回正常文案的模拟端点。
	ok := mockOpenAIServer(t, 0, "  东西很好，发货也快  ")
	// err 是写入测试 API Key 的错误。
	if err := store.Settings.Set(ctx, "ai_api_key", "sk-test"); err != nil {
		t.Fatal(err)
	}
	// err 是写入测试端点地址的错误。
	if err := store.Settings.Set(ctx, "ai_api_url", ok.URL); err != nil {
		t.Fatal(err)
	}
	// got 是成功路径返回的文案；err 是生成错误。
	if got, err := GenerateGlobalAIText(ctx, store, "cid", "系统", "商品", 100); err != nil || got != "东西很好，发货也快" {
		t.Fatalf("成功路径: %q %v", got, err)
	}

	// failing 与 empty 分别模拟上游 500 和空内容。
	failing := mockOpenAIServer(t, http.StatusInternalServerError, "")
	// err 是切换到失败端点的写入错误。
	if err := store.Settings.Set(ctx, "ai_api_url", failing.URL); err != nil {
		t.Fatal(err)
	}
	// err 是上游 500 时的生成错误，应包装为调用失败。
	if _, err := GenerateGlobalAIText(ctx, store, "cid", "s", "u", 100); err == nil || !strings.Contains(err.Error(), "AI 调用失败") {
		t.Fatalf("上游错误应包装: %v", err)
	}
	// empty 是返回空白内容的模拟端点。
	empty := mockOpenAIServer(t, 0, "   ")
	// err 是切换到空内容端点的写入错误。
	if err := store.Settings.Set(ctx, "ai_api_url", empty.URL); err != nil {
		t.Fatal(err)
	}
	// err 是空内容时的生成错误，应非空。
	if _, err := GenerateGlobalAIText(ctx, store, "cid", "s", "u", 100); err == nil {
		t.Fatal("空内容应报错")
	}

	// 出站校验拒绝地址时应在发起请求前失败。
	original := newAIHTTPClient
	t.Cleanup(func() { newAIHTTPClient = original })
	newAIHTTPClient = func(string) (*http.Client, error) { return nil, errors.New("blocked") }
	// err 是出站校验拒绝地址时的生成错误。
	if _, err := GenerateGlobalAIText(ctx, store, "cid", "s", "u", 100); err == nil || !strings.Contains(err.Error(), "地址无效") {
		t.Fatalf("被拦截的地址应报错: %v", err)
	}
}
