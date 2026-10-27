package engine

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/db"
)

// seedGreeting 为测试账号创建覆盖 item1 的商品首次回复规则。
func seedGreeting(t *testing.T, store *db.Store, enabled bool, content string) {
	t.Helper()
	// err 保存 Create 的返回错误，非空时按该分支处理。
	if _, err := store.ItemGreetings.Create(context.Background(), db.ItemGreetingRule{CookieID: "cid", Name: "g", Enabled: enabled, ReplyContent: content, ItemIDs: []string{"item1"}}); err != nil {
		t.Fatal(err)
	}
}

// sentTexts 提取投递端口收到的全部文字，便于断言顺序。
func sentTexts(delivery *recordingReplyDelivery) []string {
	// texts 保存按发送顺序排列的文字。
	texts := make([]string, 0, len(delivery.messages))
	// message 是投递端口记录的一条回复消息。
	for _, message := range delivery.messages {
		texts = append(texts, message.Text)
	}
	return texts
}

// TestItemGreetingSentOncePerChatBeforeNormalReply 首条消息先发首次回复再走关键词；同会话第二条只走关键词。
func TestItemGreetingSentOncePerChatBeforeNormalReply(t *testing.T) {
	// store、cleanup 是迁移后的隔离测试数据库聚合和资源释放函数。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是本用例共用的请求上下文。
	ctx := context.Background()
	seedGreeting(t, store, true, "欢迎咨询 {item_id}")
	store.DB.ExecContext(ctx, `INSERT INTO keywords (cookie_id,keyword,reply,type) VALUES ('cid','在吗','在的','text')`)
	// delivery 记录发送顺序；result 表示文字已确认。
	delivery := &recordingReplyDelivery{result: ReplySendResult{TextSent: true}}
	// service 是被测的服务实例，绑定本用例的测试依赖。
	service := NewReplyService("cid", store, delivery, nil, nil, nil)

	// err 保存 Handle 的返回错误，非空时按该分支处理。
	if err := service.Handle(ctx, chatMsg("在吗", "item1", "chat1")); err != nil {
		t.Fatal(err)
	}
	// err 保存 Handle 的返回错误，非空时按该分支处理。
	if err := service.Handle(ctx, chatMsg("在吗", "item1", "chat1")); err != nil {
		t.Fatal(err)
	}
	// want 是期望的发送序列：首次回复、关键词、关键词。
	got := sentTexts(delivery)
	if len(got) != 3 || got[0] != "欢迎咨询 item1" || got[1] != "在的" || got[2] != "在的" {
		t.Fatalf("发送序列异常: %v", got)
	}
	// 另一个会话再次触发。
	if err := service.Handle(ctx, chatMsg("在吗", "item1", "chat2")); err != nil {
		t.Fatal(err)
	}
	// got 保存 sentTexts 的返回值。
	if got := sentTexts(delivery); len(got) != 5 || got[3] != "欢迎咨询 item1" {
		t.Fatalf("新会话应再次触发首次回复: %v", got)
	}
}

// TestItemGreetingSkippedWhenNotApplicable 停用规则、其他商品、无 ItemID 或无 ChatID 都不触发。
func TestItemGreetingSkippedWhenNotApplicable(t *testing.T) {
	// store、cleanup 是迁移后的隔离测试数据库聚合和资源释放函数。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是本用例共用的请求上下文。
	ctx := context.Background()
	seedGreeting(t, store, false, "不应发送")
	// delivery 是记录或模拟消息投递结果的端口替身。
	delivery := &recordingReplyDelivery{result: ReplySendResult{TextSent: true}}
	// service 是被测的服务实例，绑定本用例的测试依赖。
	service := NewReplyService("cid", store, delivery, nil, nil, nil)
	// m 是当前待处理的买家消息。
	for _, m := range []ChatMessage{chatMsg("hi", "item1", "c"), chatMsg("hi", "other", "c"), chatMsg("hi", "", "c"), chatMsg("hi", "item1", "")} {
		// err 保存 Handle 的返回错误，非空时按该分支处理。
		if err := service.Handle(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	if len(delivery.messages) != 0 {
		t.Fatalf("不应发送: %v", sentTexts(delivery))
	}
	// 启用后无 ChatID 仍不触发（无法去重）。
	store.DB.ExecContext(ctx, `UPDATE item_greeting_rules SET enabled=1`)
	// err 保存 Handle 的返回错误，非空时按该分支处理。
	if err := service.Handle(ctx, chatMsg("hi", "item1", "")); err != nil || len(delivery.messages) != 0 {
		t.Fatalf("无 ChatID 不应触发 err=%v msgs=%d", err, len(delivery.messages))
	}
}

// TestItemGreetingFailureDoesNotBlockNormalReply 首次回复确定失败后关键词回复照常发送，且下次仍可重试。
func TestItemGreetingFailureDoesNotBlockNormalReply(t *testing.T) {
	// store、cleanup 是迁移后的隔离测试数据库聚合和资源释放函数。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是本用例共用的请求上下文。
	ctx := context.Background()
	seedGreeting(t, store, true, "欢迎")
	store.DB.ExecContext(ctx, `INSERT INTO keywords (cookie_id,keyword,reply,type) VALUES ('cid','在吗','在的','text')`)
	// failing 让第一次投递失败（首次回复），之后成功。
	delivery := &failFirstDelivery{}
	// service 是被测的服务实例，绑定本用例的测试依赖。
	service := NewReplyService("cid", store, delivery, nil, nil, nil)
	// err 保存 Handle 的返回错误，非空时按该分支处理。
	if err := service.Handle(ctx, chatMsg("在吗", "item1", "chat1")); err != nil {
		t.Fatalf("普通回复应成功: %v", err)
	}
	if len(delivery.texts) != 2 || delivery.texts[1] != "在的" {
		t.Fatalf("关键词回复应继续发送: %v", delivery.texts)
	}
	// 确定失败记录为 failed，下一条消息可重试首次回复。
	if err := service.Handle(ctx, chatMsg("在吗", "item1", "chat1")); err != nil {
		t.Fatal(err)
	}
	if len(delivery.texts) != 4 || delivery.texts[2] != "欢迎" {
		t.Fatalf("失败后应可重试首次回复: %v", delivery.texts)
	}
}

// failFirstDelivery 第一次发送返回确定失败，其余成功，并记录每次尝试的文字。
type failFirstDelivery struct {
	// texts 保存每次发送尝试的文字。
	texts []string
}

// SendReply 第一次尝试返回错误，之后确认文字送达。
func (d *failFirstDelivery) SendReply(_ context.Context, message ReplyMessage) (ReplySendResult, error) {
	d.texts = append(d.texts, message.Text)
	if len(d.texts) == 1 {
		return ReplySendResult{}, errors.New("发送失败")
	}
	return ReplySendResult{TextSent: true}, nil
}

// TestItemGreetingUncertainResultNeverResent 结果不确定时隔离记录，后续消息不再重发首次回复。
func TestItemGreetingUncertainResultNeverResent(t *testing.T) {
	// store、cleanup 是迁移后的隔离测试数据库聚合和资源释放函数。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是本用例共用的请求上下文。
	ctx := context.Background()
	seedGreeting(t, store, true, "欢迎")
	// delivery 是记录或模拟消息投递结果的端口替身。
	delivery := &recordingReplyDelivery{result: ReplySendResult{Uncertain: true}, err: errors.New("超时")}
	// service 是被测的服务实例，绑定本用例的测试依赖。
	service := NewReplyService("cid", store, delivery, nil, nil, nil)
	// i 是当前循环序号。
	for i := 0; i < 2; i++ {
		// err 保存 Handle 的返回错误，非空时按该分支处理。
		if err := service.Handle(ctx, chatMsg("hi", "item1", "chat1")); err != nil {
			t.Fatal(err)
		}
	}
	if len(delivery.messages) != 1 {
		t.Fatalf("不确定结果不得重发: %d", len(delivery.messages))
	}
}

// TestDefaultReplyOnceYieldsToItemGreeting 已触发商品首次回复的会话不再发送账号级仅一次默认回复；非仅一次的默认回复不受影响。
func TestDefaultReplyOnceYieldsToItemGreeting(t *testing.T) {
	// store、cleanup 是迁移后的隔离测试数据库聚合和资源释放函数。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是本用例共用的请求上下文。
	ctx := context.Background()
	seedGreeting(t, store, true, "商品欢迎")
	store.DB.ExecContext(ctx, `INSERT INTO default_replies (cookie_id,enabled,reply_content,reply_once) VALUES ('cid',1,'账号默认',1)`)
	// delivery 是记录或模拟消息投递结果的端口替身。
	delivery := &recordingReplyDelivery{result: ReplySendResult{TextSent: true}}
	// service 是被测的服务实例，绑定本用例的测试依赖。
	service := NewReplyService("cid", store, delivery, nil, nil, nil)
	// err 保存 Handle 的返回错误，非空时按该分支处理。
	if err := service.Handle(ctx, chatMsg("随便", "item1", "chat1")); err != nil {
		t.Fatal(err)
	}
	// got 保存 sentTexts 的返回值。
	if got := sentTexts(delivery); len(got) != 1 || got[0] != "商品欢迎" {
		t.Fatalf("商品级应覆盖账号级: %v", got)
	}
	// 其他商品的会话没有首次回复记录，账号默认回复照常发送。
	if err := service.Handle(ctx, chatMsg("随便", "other", "chat2")); err != nil {
		t.Fatal(err)
	}
	// got 保存 sentTexts 的返回值。
	if got := sentTexts(delivery); len(got) != 2 || got[1] != "账号默认" {
		t.Fatalf("无首次回复记录时默认回复应发送: %v", got)
	}
	// 非仅一次的默认回复在已触发首次回复的会话中仍然发送。
	store.DB.ExecContext(ctx, `UPDATE default_replies SET reply_once=0`)
	// err 保存 Handle 的返回错误，非空时按该分支处理。
	if err := service.Handle(ctx, chatMsg("再问", "item1", "chat1")); err != nil {
		t.Fatal(err)
	}
	// got 保存 sentTexts 的返回值。
	if got := sentTexts(delivery); len(got) != 3 || got[2] != "账号默认" {
		t.Fatalf("非仅一次默认回复不应被覆盖: %v", got)
	}
}
