package adapter

import (
	"context"
	"errors"
	"testing"

	chatapp "xianyu-go/internal/application/chat"
)

// chatConversationRefreshFake 提供联系人分页测试结果，隔离订单同步与平台运行时。
type chatConversationRefreshFake struct {
	// pages 保存按调用顺序返回的联系人分页。
	pages []chatapp.ConversationPage
	// refreshErr 保存联系人刷新失败原因。
	refreshErr error
	// cursors 保存每次联系人刷新收到的平台游标。
	cursors []int64
}

// RefreshConversations 返回预置联系人分页并记录请求游标。
func (f *chatConversationRefreshFake) RefreshConversations(_ context.Context, _ string, cursor int64, _ int) (chatapp.ConversationPage, error) {
	f.cursors = append(f.cursors, cursor)
	if f.refreshErr != nil {
		return chatapp.ConversationPage{}, f.refreshErr
	}
	// pageIndex 保存本次请求对应的预置分页下标。
	pageIndex := len(f.cursors) - 1
	if pageIndex >= len(f.pages) {
		return chatapp.ConversationPage{}, nil
	}
	return f.pages[pageIndex], nil
}

// RefreshHistory 满足聊天刷新 Port 的历史刷新能力，但本测试只关注联系人分页。
func (f *chatConversationRefreshFake) RefreshHistory(context.Context, string, string, int64, int, chatapp.Session) (chatapp.HistoryPage, error) {
	return chatapp.HistoryPage{}, nil
}

// TestNewChatConversationRefreshCallbackWalksPages 验证订单触发的联系人刷新会完整推进分页游标。
func TestNewChatConversationRefreshCallbackWalksPages(t *testing.T) {
	// provider 保存两页联系人结果，第二页表示刷新完成。
	provider := &chatConversationRefreshFake{pages: []chatapp.ConversationPage{{HasMore: true, NextCursor: 10}, {HasMore: false}}}
	// refresh 保存按需联系人刷新回调。
	refresh := NewChatConversationRefreshCallback(provider)
	// refreshErr 保存完整联系人刷新结果。
	refreshErr := refresh(context.Background(), "cookie-1")
	if refreshErr != nil || len(provider.cursors) != 2 || provider.cursors[0] != 0 || provider.cursors[1] != 10 {
		t.Fatalf("联系人分页刷新异常: cursors=%v err=%v", provider.cursors, refreshErr)
	}
}

// TestNewChatConversationRefreshCallbackRejectsStalledCursor 验证平台游标不推进时立即终止重试。
func TestNewChatConversationRefreshCallbackRejectsStalledCursor(t *testing.T) {
	// provider 保存游标不推进的异常联系人分页。
	provider := &chatConversationRefreshFake{pages: []chatapp.ConversationPage{{HasMore: true, NextCursor: 0}}}
	// refresh 保存按需联系人刷新回调。
	refresh := NewChatConversationRefreshCallback(provider)
	// refreshErr 保存游标异常结果。
	refreshErr := refresh(context.Background(), "cookie-1")
	if refreshErr == nil || refreshErr.Error() != "聊天联系人刷新游标未推进" {
		t.Fatalf("应拒绝停滞游标: err=%v", refreshErr)
	}
}

// TestNewChatConversationRefreshCallbackPropagatesProviderError 验证联系人刷新 Port 错误原样返回。
func TestNewChatConversationRefreshCallbackPropagatesProviderError(t *testing.T) {
	// providerErr 保存底层联系人刷新错误。
	providerErr := errors.New("联系人刷新失败")
	// provider 保存返回底层错误的联系人刷新 Port。
	provider := &chatConversationRefreshFake{refreshErr: providerErr}
	// refresh 保存按需联系人刷新回调。
	refresh := NewChatConversationRefreshCallback(provider)
	// refreshErr 保存回调返回的底层错误。
	refreshErr := refresh(context.Background(), "cookie-1")
	if !errors.Is(refreshErr, providerErr) {
		t.Fatalf("联系人刷新错误未透传: got=%v want=%v", refreshErr, providerErr)
	}
}

// TestNewChatConversationRefreshCallbackAllowsMissingProvider 验证未装配聊天刷新 Port 时返回可选空能力。
func TestNewChatConversationRefreshCallbackAllowsMissingProvider(t *testing.T) {
	// refresh 保存未装配联系人刷新 Port 时的回调结果。
	refresh := NewChatConversationRefreshCallback(nil)
	if refresh != nil {
		t.Fatal("未装配联系人刷新 Port 应返回 nil")
	}
}

// TestChatRefreshCursorHistory 验证联系人递减游标、停滞、多页循环和页数预算。
func TestChatRefreshCursorHistory(t *testing.T) {
	// scenario 是当前平台游标序列，wantPages 防止把提前退出误报为完成。
	for _, scenario := range []struct {
		// name 标识当前分页场景。
		name string
		// pages 是平台按调用顺序返回的受控分页。
		pages []chatapp.ConversationPage
		// wantError 表示当前序列是否无法完整结束。
		wantError bool
		// wantPages 是应执行的平台请求次数。
		wantPages int
	}{
		{"descending", []chatapp.ConversationPage{{HasMore: true, NextCursor: 100}, {HasMore: true, NextCursor: 50}, {}}, false, 3},
		{"stalled", []chatapp.ConversationPage{{HasMore: true, NextCursor: 100}, {HasMore: true, NextCursor: 100}}, true, 2},
		{"cycle", []chatapp.ConversationPage{{HasMore: true, NextCursor: 100}, {HasMore: true, NextCursor: 50}, {HasMore: true, NextCursor: 100}}, true, 3},
		{"negative", []chatapp.ConversationPage{{HasMore: true, NextCursor: -1}}, true, 1},
	} {
		// t 提供当前游标场景的断言上下文。
		t.Run(scenario.name, func(t *testing.T) {
			// provider 返回当前虚构分页，不进行外部 I/O。
			provider := &chatConversationRefreshFake{pages: scenario.pages}
			// err 保存完整联系人刷新结果。
			err := NewChatConversationRefreshCallback(provider)(context.Background(), "account")
			if (err != nil) != scenario.wantError || len(provider.cursors) != scenario.wantPages {
				t.Fatalf("分页结果错误: pages=%d err=%v", len(provider.cursors), err)
			}
		})
	}
	// pages 构造持续向历史推进但始终不结束的响应，验证页数上限。
	pages := make([]chatapp.ConversationPage, chatRefreshMaxPages)
	// index 是当前页序号，游标始终为正且不重复。
	for index := range pages {
		pages[index] = chatapp.ConversationPage{HasMore: true, NextCursor: int64(chatRefreshMaxPages - index)}
	}
	// provider 提供超出安全预算的联系人列表。
	provider := &chatConversationRefreshFake{pages: pages}
	// err 应表明仍有后续页时刷新已达到安全预算。
	if err := NewChatConversationRefreshCallback(provider)(context.Background(), "account"); err == nil || len(provider.cursors) != chatRefreshMaxPages {
		t.Fatal("未按页数预算停止")
	}
}
