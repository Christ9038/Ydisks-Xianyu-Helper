package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// manualRequestRecorder 是实现“请求人工客服”通知端口的测试处理器，记录收到的通知并可返回预设错误。
type manualRequestRecorder struct {
	recordingHandler
	// requestMu 保护 requests。
	requestMu sync.Mutex
	// requests 保存收到的通知摘要。
	requests []ChatManualRequest
	// err 是通知端口返回的预设错误。
	err error
}

// RequestManualReply 记录一次人工客服通知请求。
func (h *manualRequestRecorder) RequestManualReply(_ context.Context, request ChatManualRequest) error {
	h.requestMu.Lock()
	defer h.requestMu.Unlock()
	h.requests = append(h.requests, request)
	return h.err
}

// count 返回已收到的通知次数。
func (h *manualRequestRecorder) count() int {
	h.requestMu.Lock()
	defer h.requestMu.Unlock()
	return len(h.requests)
}

// hookAIReplier 是在生成回复期间执行钩子的 AI 替身，用于模拟“AI 生成期间人工已回复”。
type hookAIReplier struct {
	// result 是预设的 AI 回复。
	result *ReplyResult
	// during 在返回前执行，可为空。
	during func()
	// called 统计调用次数。
	called int
}

// Reply 执行钩子并返回预设回复。
func (f *hookAIReplier) Reply(_ context.Context, _ ChatMessage) (*ReplyResult, error) {
	f.called++
	if f.during != nil {
		f.during()
	}
	return f.result, nil
}

// manualTakeoverFixture 是回复链人工介入门禁测试的共用装配。
type manualTakeoverFixture struct {
	// store 是隔离 SQLite 仓储。
	store *db.Store
	// service 是被测回复服务。
	service *ReplyService
	// tracker 是注入回复服务的人工介入跟踪器。
	tracker *manualTakeoverTracker
	// clock 是跟踪器的模拟时钟。
	clock *fakeClock
	// delivery 记录实际投递的回复。
	delivery *recordingReplyDelivery
	// api 是外部 Agent 替身。
	api *fakeAPIReplier
	// ai 是 AI 替身。
	ai *hookAIReplier
	// handler 是通知端口替身。
	handler *manualRequestRecorder
}

// newManualTakeoverFixture 创建配置了指定暂停分钟数与范围的回复服务装配。
func newManualTakeoverFixture(t *testing.T, minutes int, scope string) *manualTakeoverFixture {
	t.Helper()
	// store、cleanup 是隔离数据库及其关闭函数。
	store, cleanup := newReplyStore(t)
	t.Cleanup(cleanup)
	// admin 是夹具账号的所有者。
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	// err 保存 UpdateSettings 调用的返回错误，非空即说明该步骤未按预期完成。
	if _, err := store.Cookies.UpdateSettings(context.Background(), "cid", db.AccountSettingsUpdate{UserID: admin.ID, ManualTakeoverMinutes: &minutes, ManualTakeoverScope: &scope}); err != nil {
		t.Fatalf("保存人工介入配置: %v", err)
	}
	// fixture 汇总全部替身。
	fixture := &manualTakeoverFixture{store: store, delivery: &recordingReplyDelivery{result: ReplySendResult{TextSent: true}}, api: &fakeAPIReplier{}, ai: &hookAIReplier{result: &ReplyResult{Text: "AI 回复"}}, handler: &manualRequestRecorder{}}
	fixture.tracker, fixture.clock = newTestManualTakeover()
	fixture.service = newReplyService("cid", store, fixture.delivery, fixture.api, fixture.ai, nil)
	fixture.service.manualTakeover = fixture.tracker
	fixture.service.manualRequests = func() Handler { return fixture.handler }
	return fixture
}

// sentTexts 返回已投递回复的文本列表。
func (f *manualTakeoverFixture) sentTexts() []string {
	// texts 收集投递文本。
	texts := make([]string, 0, len(f.delivery.messages))
	// message 是单条已投递回复。
	for _, message := range f.delivery.messages {
		texts = append(texts, message.Text)
	}
	return texts
}

// TestReplyManualTakeoverAIScopeSkipsGenerativeOnly 验证 scope=ai 时暂停窗口内跳过 Agent 与 AI，关键词与默认回复照常；窗口到期后 AI 恢复。
func TestReplyManualTakeoverAIScopeSkipsGenerativeOnly(t *testing.T) {
	// fixture 配置 10 分钟、只暂停 AI。
	fixture := newManualTakeoverFixture(t, 10, db.ManualTakeoverScopeAI)
	// ctx 是测试上下文。
	ctx := context.Background()
	// err 保存 AddRow 调用的返回错误，非空即说明该步骤未按预期完成。
	if _, err := fixture.store.Keywords.AddRow(ctx, db.KeywordRow{CookieID: "cid", Expressions: []string{"发货"}, Reply: "关键词回复"}); err != nil {
		t.Fatal(err)
	}
	// err 保存 Upsert 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := fixture.store.DefaultReps.Upsert(ctx, "cid", db.DefaultReply{Enabled: true, ReplyContent: "默认回复"}); err != nil {
		t.Fatal(err)
	}
	fixture.tracker.markManual("chat-1", fixture.clock.now())

	// err 保存 Handle 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := fixture.service.Handle(ctx, chatMsg("什么时候发货", "", "chat-1")); err != nil {
		t.Fatal(err)
	}
	// err 保存 Handle 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := fixture.service.Handle(ctx, chatMsg("便宜点", "", "chat-1")); err != nil {
		t.Fatal(err)
	}
	if fixture.api.called != 0 || fixture.ai.called != 0 {
		t.Fatalf("暂停窗口内不应调用 Agent/AI: api=%d ai=%d", fixture.api.called, fixture.ai.called)
	}
	// got 是实际保留的条目数量，用于断言容量上限。
	if got := fixture.sentTexts(); len(got) != 2 || got[0] != "关键词回复" || got[1] != "默认回复" {
		t.Fatalf("关键词与默认回复应照常发送: %#v", got)
	}
	// 其他会话不受暂停影响。
	if err := fixture.service.Handle(ctx, chatMsg("便宜点", "", "chat-2")); err != nil {
		t.Fatal(err)
	}
	if fixture.ai.called != 1 {
		t.Fatalf("其他会话应正常调用 AI: %d", fixture.ai.called)
	}
	fixture.clock.advance(10 * time.Minute)
	// err 保存 Handle 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := fixture.service.Handle(ctx, chatMsg("便宜点", "", "chat-1")); err != nil {
		t.Fatal(err)
	}
	if fixture.ai.called != 2 {
		t.Fatalf("窗口到期后应恢复 AI: %d", fixture.ai.called)
	}
}

// TestReplyManualTakeoverAllScopeSkipsEverything 验证 scope=all 时暂停窗口内不发送任何自动回复，包括关键词与默认回复。
func TestReplyManualTakeoverAllScopeSkipsEverything(t *testing.T) {
	// fixture 配置 10 分钟、暂停全部。
	fixture := newManualTakeoverFixture(t, 10, db.ManualTakeoverScopeAll)
	// ctx 是测试上下文。
	ctx := context.Background()
	// err 保存 AddRow 调用的返回错误，非空即说明该步骤未按预期完成。
	if _, err := fixture.store.Keywords.AddRow(ctx, db.KeywordRow{CookieID: "cid", Expressions: []string{"发货"}, Reply: "关键词回复"}); err != nil {
		t.Fatal(err)
	}
	// err 保存 Upsert 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := fixture.store.DefaultReps.Upsert(ctx, "cid", db.DefaultReply{Enabled: true, ReplyContent: "默认回复"}); err != nil {
		t.Fatal(err)
	}
	fixture.tracker.markManual("chat-1", fixture.clock.now())
	// err 保存 Handle 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := fixture.service.Handle(ctx, chatMsg("什么时候发货", "", "chat-1")); err != nil {
		t.Fatal(err)
	}
	if len(fixture.delivery.messages) != 0 || fixture.ai.called != 0 || fixture.api.called != 0 {
		t.Fatalf("全部暂停时不应发送任何回复: %#v", fixture.sentTexts())
	}
}

// TestReplyManualTakeoverDisabledOrUnreadable 验证分钟数为 0 时功能关闭，以及账号配置读取失败时降级为不暂停。
func TestReplyManualTakeoverDisabledOrUnreadable(t *testing.T) {
	// fixture 配置 0 分钟（关闭）。
	fixture := newManualTakeoverFixture(t, 0, db.ManualTakeoverScopeAll)
	// ctx 是测试上下文。
	ctx := context.Background()
	fixture.tracker.markManual("chat-1", fixture.clock.now())
	// err 保存 Handle 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := fixture.service.Handle(ctx, chatMsg("便宜点", "", "chat-1")); err != nil {
		t.Fatal(err)
	}
	if fixture.ai.called != 1 || len(fixture.delivery.messages) != 1 {
		t.Fatalf("功能关闭时 AI 应照常回复: ai=%d sent=%d", fixture.ai.called, len(fixture.delivery.messages))
	}
	// 账号不存在时读取配置失败，必须降级为不暂停而不是阻断回复。
	fixture.service.cookieID = "missing"
	// err 保存 Handle 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := fixture.service.Handle(ctx, chatMsg("便宜点", "", "chat-1")); err != nil {
		t.Fatal(err)
	}
	if fixture.ai.called != 2 {
		t.Fatalf("配置读取失败时应降级为不暂停: %d", fixture.ai.called)
	}
}

// TestReplyManualTakeoverRechecksBeforeSendingAI 验证 AI 生成期间人工介入时，发送前的二次检查会放弃这条 AI 回复。
func TestReplyManualTakeoverRechecksBeforeSendingAI(t *testing.T) {
	// fixture 配置 10 分钟、只暂停 AI。
	fixture := newManualTakeoverFixture(t, 10, db.ManualTakeoverScopeAI)
	fixture.ai.during = func() { fixture.tracker.markManual("chat-1", fixture.clock.now()) }
	// err 保存 Handle 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := fixture.service.Handle(context.Background(), chatMsg("便宜点", "", "chat-1")); err != nil {
		t.Fatal(err)
	}
	if fixture.ai.called != 1 || len(fixture.delivery.messages) != 0 {
		t.Fatalf("AI 生成期间人工介入后不应发送: ai=%d sent=%#v", fixture.ai.called, fixture.sentTexts())
	}
}

// TestReplyKeywordNotifyManual 验证“通知人工”规则命中后照常回复、发送通知并让会话进入暂停窗口；去重窗口内不重复通知。
func TestReplyKeywordNotifyManual(t *testing.T) {
	// fixture 配置 10 分钟、只暂停 AI。
	fixture := newManualTakeoverFixture(t, 10, db.ManualTakeoverScopeAI)
	// ctx 是测试上下文。
	ctx := context.Background()
	// err 保存 AddRow 调用的返回错误，非空即说明该步骤未按预期完成。
	if _, err := fixture.store.Keywords.AddRow(ctx, db.KeywordRow{CookieID: "cid", Expressions: []string{"人工"}, Reply: "已为您转接人工", NotifyManual: true}); err != nil {
		t.Fatal(err)
	}
	// message 是买家要求人工的消息。
	message := chatMsg("我要人工客服", "item-9", "chat-1")
	// err 保存 Handle 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := fixture.service.Handle(ctx, message); err != nil {
		t.Fatal(err)
	}
	// got 是实际保留的条目数量，用于断言容量上限。
	if got := fixture.sentTexts(); len(got) != 1 || got[0] != "已为您转接人工" {
		t.Fatalf("规则回复应照常发送: %#v", got)
	}
	if fixture.handler.count() != 1 {
		t.Fatalf("应发送一次人工客服通知: %d", fixture.handler.count())
	}
	// request 是收到的通知摘要。
	request := fixture.handler.requests[0]
	if request.AccountID != "cid" || request.ChatID != "chat-1" || request.BuyerName != "买家" || request.ItemID != "item-9" || request.Text != "我要人工客服" {
		t.Fatalf("通知摘要字段错误: %#v", request)
	}
	if !fixture.tracker.isPaused("chat-1", 10*time.Minute) {
		t.Fatal("命中通知人工后会话应进入暂停窗口")
	}
	// 随后的普通消息不再由 AI 回复。
	if err := fixture.service.Handle(ctx, chatMsg("在吗", "", "chat-1")); err != nil {
		t.Fatal(err)
	}
	if fixture.ai.called != 0 {
		t.Fatalf("转人工后 AI 应暂停: %d", fixture.ai.called)
	}
	// 去重窗口内再次命中只回复不重复通知。
	if err := fixture.service.Handle(ctx, message); err != nil {
		t.Fatal(err)
	}
	if fixture.handler.count() != 1 {
		t.Fatalf("去重窗口内不应重复通知: %d", fixture.handler.count())
	}
}

// TestReplyKeywordNotifyManualEdgeCases 覆盖空回复规则仍通知、全部暂停时仍通知、通知端口失败或缺失时不影响回复。
func TestReplyKeywordNotifyManualEdgeCases(t *testing.T) {
	// fixture 配置 10 分钟、暂停全部。
	fixture := newManualTakeoverFixture(t, 10, db.ManualTakeoverScopeAll)
	// ctx 是测试上下文。
	ctx := context.Background()
	// 历史数据中的空回复规则：只通知、不发送。
	if err := fixture.store.Keywords.ReplaceForCookie(ctx, "cid", []db.KeywordRow{{Keyword: "人工", Reply: "", NotifyManual: true}}); err != nil {
		t.Fatal(err)
	}
	// err 保存 Handle 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := fixture.service.Handle(ctx, chatMsg("人工", "", "chat-1")); err != nil {
		t.Fatal(err)
	}
	if fixture.handler.count() != 1 || len(fixture.delivery.messages) != 0 {
		t.Fatalf("空回复规则应只通知不发送: notify=%d sent=%d", fixture.handler.count(), len(fixture.delivery.messages))
	}
	// 会话已在全部暂停窗口内，去重窗口过后买家再次要求人工仍需通知。
	fixture.clock.advance(manualNotifyDedupWindow - time.Minute)
	fixture.tracker.markManual("chat-1", fixture.clock.now())
	fixture.clock.advance(time.Minute)
	// err 保存 Handle 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := fixture.service.Handle(ctx, chatMsg("人工", "", "chat-1")); err != nil {
		t.Fatal(err)
	}
	if fixture.handler.count() != 2 || len(fixture.delivery.messages) != 0 {
		t.Fatalf("全部暂停期间仍应通知人工: notify=%d sent=%d", fixture.handler.count(), len(fixture.delivery.messages))
	}
	// 通知端口返回错误只记录日志。
	fixture.handler.err = errors.New("通知渠道不可用")
	// err 保存 Handle 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := fixture.service.Handle(ctx, chatMsg("人工", "", "chat-2")); err != nil {
		t.Fatalf("通知失败不应影响回复链: %v", err)
	}
	// 处理器未实现通知端口或未装配时不 panic。
	fixture.service.manualRequests = func() Handler { return &recordingHandler{} }
	fixture.service.Handle(ctx, chatMsg("人工", "", "chat-3"))
	fixture.service.manualRequests = nil
	fixture.service.Handle(ctx, chatMsg("人工", "", "chat-4"))
	// 缺少会话 ID 的消息不领取通知。
	fixture.service.requestManualReply(ctx, ChatMessage{})
}
