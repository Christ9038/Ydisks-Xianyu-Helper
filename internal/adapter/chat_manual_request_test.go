package adapter

import (
	"context"
	"strings"
	"testing"

	"xianyu-go/internal/engine"
)

// TestRequestManualReplyForwardsChatManualEvent 验证“请求人工客服”通知以独立类别转发给通知器，正文包含定位信息且截断超长买家原话。
func TestRequestManualReplyForwardsChatManualEvent(t *testing.T) {
	// notifier 记录转发的账号事件。
	notifier := &fakeNotifier{}
	// adapter 是注入通知器的适配器。
	adapter := New(nil, nil, nil)
	adapter.notifier = notifier
	// longText 是超过截断上限的买家原话。
	longText := strings.Repeat("字", manualRequestTextLimit+5)
	// err 保存 RequestManualReply 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := adapter.RequestManualReply(context.Background(), engine.ChatManualRequest{AccountID: "acc1", ChatID: "chat-1", BuyerName: "小明", ItemID: "item-9", Text: longText}); err != nil {
		t.Fatalf("RequestManualReply: %v", err)
	}
	if len(notifier.events) != 1 {
		t.Fatalf("应转发一条事件: %#v", notifier.events)
	}
	// event 是转发的事件。
	event := notifier.events[0]
	if event.cookieID != "acc1" || event.eventType != engine.EventChatManualRequested || event.level != engine.AlertLevelWarn || event.title != "买家请求人工客服" {
		t.Fatalf("事件字段错误: %#v", event)
	}
	// want 是通知正文必须包含的定位片段。
	for _, want := range []string{"小明 请求人工客服", "会话: chat-1", "商品: item-9", "…"} {
		if !strings.Contains(event.body, want) {
			t.Fatalf("正文缺少 %q: %s", want, event.body)
		}
	}
	if strings.Contains(event.body, longText) {
		t.Fatal("超长买家原话应被截断")
	}
	// 通知器未注入时只记录日志，不 panic。
	adapter.notifier = nil
	// err 保存 RequestManualReply 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := adapter.RequestManualReply(context.Background(), engine.ChatManualRequest{AccountID: "acc1"}); err != nil {
		t.Fatalf("未注入通知器时不应返回错误: %v", err)
	}
}

// TestFormatManualRequestBodyFallbacks 验证买家称呼回退顺序与空字段省略。
func TestFormatManualRequestBodyFallbacks(t *testing.T) {
	// body 是按回退规则格式化后的通知正文。
	if body := formatManualRequestBody(engine.ChatManualRequest{BuyerID: "buyer-1"}); body != "buyer-1 请求人工客服，请尽快在会话中回复。" {
		t.Fatalf("应回退买家 ID: %q", body)
	}
	// body 是按回退规则格式化后的通知正文。
	if body := formatManualRequestBody(engine.ChatManualRequest{}); !strings.HasPrefix(body, "买家 请求人工客服") || strings.Contains(body, "会话:") {
		t.Fatalf("空字段应省略: %q", body)
	}
	// got 是非正上限下的截断结果，应保持原文。
	if got := truncateRunes("短文本", 0); got != "短文本" {
		t.Fatalf("非正上限不截断: %q", got)
	}
}

// adapterImplementsManualRequest 编译期确认生产适配器实现了引擎的人工客服通知端口。
var _ interface {
	RequestManualReply(context.Context, engine.ChatManualRequest) error
} = (*Adapter)(nil)
