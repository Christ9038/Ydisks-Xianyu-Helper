package engine

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

// TestReplyServiceDeliversLibraryImageForKeywordDefaultAndGreeting 验证三类回复都把图片库图片主键交给统一投递端口，且不带图片地址。
func TestReplyServiceDeliversLibraryImageForKeywordDefaultAndGreeting(t *testing.T) {
	// store、cleanup 是迁移后的隔离测试数据库聚合和资源释放函数。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是本用例共用的请求上下文。
	ctx := context.Background()
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if _, err := store.Keywords.AddRow(ctx, db.KeywordRow{CookieID: "cid", Expressions: []string{"看图"}, Type: "image", LibraryImageID: 21}); err != nil {
		t.Fatal(err)
	}
	// delivery 记录发送内容；图片已确认送达。
	delivery := &recordingReplyDelivery{result: ReplySendResult{ImageSent: true}}
	// service 是被测的回复服务。
	service := NewReplyService("cid", store, delivery, nil, nil, nil)
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if err := service.Handle(ctx, chatMsg("看图", "item1", "chat1")); err != nil {
		t.Fatal(err)
	}
	if len(delivery.messages) != 1 || delivery.messages[0].LibraryImageID != 21 || delivery.messages[0].ImageURL != "" {
		t.Fatalf("关键词图片库回复异常: %+v", delivery.messages)
	}

	// 默认回复：只含图片库图片也是有效回复，不会被当成空回复跳过。
	if err := store.DefaultReps.Upsert(ctx, "cid", db.DefaultReply{Enabled: true, ReplyLibraryImageID: 22}); err != nil {
		t.Fatal(err)
	}
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if err := service.Handle(ctx, chatMsg("没有匹配的话", "item1", "chat2")); err != nil {
		t.Fatal(err)
	}
	// last 是默认回复投递的消息。
	last := delivery.messages[len(delivery.messages)-1]
	if last.LibraryImageID != 22 || last.Text != "" {
		t.Fatalf("默认图片库回复异常: %+v", last)
	}

	// 商品首次回复：图片库图片与文字一起交给投递端口。
	if _, err := store.ItemGreetings.Create(ctx, db.ItemGreetingRule{CookieID: "cid", Name: "g", Enabled: true, ReplyContent: "欢迎", ReplyLibraryImageID: 23, ItemIDs: []string{"item9"}}); err != nil {
		t.Fatal(err)
	}
	// before 记录首次回复前的消息数。
	before := len(delivery.messages)
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if err := service.Handle(ctx, chatMsg("没有匹配的话", "item9", "chat3")); err != nil {
		t.Fatal(err)
	}
	// greeting 是商品首次回复投递的消息。
	greeting := delivery.messages[before]
	if greeting.LibraryImageID != 23 || greeting.Text != "欢迎" {
		t.Fatalf("首次回复图片库图片异常: %+v", greeting)
	}
}

// TestReplyOnceLibraryImageIsNotResentAfterImageConfirmed 验证 reply_once 的图片库图片分段确认后，重试只补发尚未成功的文字。
func TestReplyOnceLibraryImageIsNotResentAfterImageConfirmed(t *testing.T) {
	// store、cleanup 是迁移后的隔离测试数据库聚合和资源释放函数。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是本用例共用的请求上下文。
	ctx := context.Background()
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if err := store.DefaultReps.Upsert(ctx, "cid", db.DefaultReply{Enabled: true, ReplyContent: "文字", ReplyLibraryImageID: 31, ReplyOnce: true}); err != nil {
		t.Fatal(err)
	}
	// delivery 第一次只确认图片、文字未送达并报错；第二次确认文字。
	delivery := &partialThenOKDelivery{}
	// service 是被测的回复服务。
	service := NewReplyService("cid", store, delivery, nil, nil, nil)
	_ = service.Handle(ctx, chatMsg("hi", "item1", "chat1"))
	_ = service.Handle(ctx, chatMsg("hi", "item1", "chat1"))
	if len(delivery.messages) != 2 {
		t.Fatalf("应发送两次: %+v", delivery.messages)
	}
	if delivery.messages[0].LibraryImageID != 31 || delivery.messages[1].LibraryImageID != 0 || delivery.messages[1].Text != "文字" {
		t.Fatalf("图片确认后不应重发图片库图片: %+v", delivery.messages)
	}
}

// partialThenOKDelivery 第一次只确认图片并返回错误，第二次确认文字，用于验证分段重试。
type partialThenOKDelivery struct {
	// messages 保存每次投递收到的完整回复。
	messages []ReplyMessage
}

// SendReply 按调用次数返回分段确认结果。
func (d *partialThenOKDelivery) SendReply(_ context.Context, message ReplyMessage) (ReplySendResult, error) {
	d.messages = append(d.messages, message)
	if len(d.messages) == 1 {
		return ReplySendResult{ImageSent: true}, context.DeadlineExceeded
	}
	return ReplySendResult{TextSent: true}, nil
}
