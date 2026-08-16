package server

import (
	"context"
	"testing"

	automationapp "xianyu-go/internal/application/automation"
)

// TestAccountTaskApplicationUpdatesSettings 验证账号任务设置由独立应用服务校验并持久化。
func TestAccountTaskApplicationUpdatesSettings(t *testing.T) {
	// srv、store 和 cleanup 构造并释放测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// settings 和 err 保存应用服务返回的最终任务设置。
	settings, err := srv.accountTaskApplication().UpdateSettings(context.Background(), automationapp.AccountTaskSettings{
		CookieID: "acc1", AutoRateEnabled: true, RateContent: "  好评  ", AutoPolishEnabled: false, PolishTime: "03:00",
	})
	if err != nil {
		t.Fatalf("UpdateAccountTaskSettings error: %v", err)
	}
	if settings.CookieID != "acc1" || settings.RateContent != "好评" || !settings.AutoRateEnabled {
		t.Fatalf("unexpected task settings: %+v", settings)
	}
}

// TestAccountTaskApplicationRejectsInvalidSettings 验证账号任务业务校验不依赖 HTTP 请求。
func TestAccountTaskApplicationRejectsInvalidSettings(t *testing.T) {
	// srv、store 和 cleanup 构造并释放测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// err 保存无效任务设置的校验错误。
	_, err := srv.accountTaskApplication().UpdateSettings(context.Background(), automationapp.AccountTaskSettings{
		CookieID: "acc1", AutoRateEnabled: true, PolishTime: "03:00",
	})
	if err == nil {
		t.Fatal("启用自动评价但没有内容应该失败")
	}
}

// TestChatApplicationListsStoredMessages 验证聊天历史应用服务返回稳定分页结果。
func TestChatApplicationListsStoredMessages(t *testing.T) {
	// srv、store 和 cleanup 构造并释放测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// page 和 err 保存聊天历史查询结果。
	page, err := srv.chatApplication().ListStoredMessages(context.Background(), 1, "acc1", "chat-missing", 0, 20)
	if err != nil {
		t.Fatalf("ListStoredMessages error: %v", err)
	}
	if len(page.Messages) != 0 || page.Session.ChatID != "" {
		t.Fatalf("unexpected empty chat page: %+v", page)
	}
}
