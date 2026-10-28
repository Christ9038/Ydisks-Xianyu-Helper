package automation

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// auditTimeline 读取管理员名下全部审计事件，按写入顺序返回，供断言行为时间线。
func auditTimeline(t *testing.T, store *db.Store) []db.AutomationAuditEvent {
	t.Helper()
	// admin 是夹具创建的管理员，审计事件按其隔离。
	admin, userErr := store.Users.GetByUsername(context.Background(), "admin")
	if userErr != nil {
		t.Fatal(userErr)
	}
	// page、listErr 保存按正序读取的审计事件。
	page, listErr := store.Automation.ListAuditEvents(context.Background(), db.AutomationAuditQuery{UserID: admin.ID, Ascending: true, Limit: 200})
	if listErr != nil {
		t.Fatal(listErr)
	}
	return page.Events
}

// auditKinds 把时间线压缩成「事件类型:状态」序列，便于整体断言顺序。
func auditKinds(events []db.AutomationAuditEvent) []string {
	// kinds 保存压缩后的序列。
	kinds := make([]string, 0, len(events))
	// event 是当前遍历的事件。
	for _, event := range events {
		kinds = append(kinds, event.EventType+":"+event.Status)
	}
	return kinds
}

// TestAuditRecordsPaidDeliveryTimelineWithoutLeakingCardContent 验证付款发货留下完整时间线，摘要含卡密组名称但绝不含卡密正文。
func TestAuditRecordsPaidDeliveryTimelineWithoutLeakingCardContent(t *testing.T) {
	// store、cleanup 是隔离的测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是测试根上下文。
	ctx := context.Background()
	// admin 是规则所有者。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('cid','item-1','会员')`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES (31,'默认库存','text','SECRET-CARD-1',1,?)`, admin.ID)
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "item-1", Name: "付款后自动发货", TriggerType: TriggerOrderPaid,
		Enabled: true, Priority: 100,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1, Enabled: true, SortOrder: 1},
			{ActionType: ActionConfirmShipment, Enabled: true, SortOrder: 2},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// server 模拟确认发货接口。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"]}`)
	}))
	defer server.Close()
	// center 使用本地 MTOP 与订单详情桩构造。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{
		MTop:               &mtop.ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/"},
		OrderDetailFetcher: testFetcher{detail: &OrderDetail{Quantity: "1", Amount: "9.9"}},
	})
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", OrderRole: OrderRoleSeller, CookieStr: "unb=123; _m_h5_tk=tk_1;", TriggerType: TriggerOrderPaid,
		ChatID: "chat-1", OrderID: "order-audit", ItemID: "item-1", BuyerID: "buyer-1", Raw: map[string]any{"message_id": "m1"},
	}); err != nil {
		t.Fatal(err)
	}

	// events 是完整审计时间线。
	events := auditTimeline(t, store)
	// want 是期望的行为顺序：开始、发卡、确认发货、终态成功。
	want := []string{"run_started:running", "action_succeeded:success", "action_succeeded:success", "run_finished:success"}
	// got 保存被测调用的实际返回值，用于与期望值比较。
	if got := auditKinds(events); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("时间线=%v want %v", got, want)
	}
	if events[1].ActionType != ActionSendCard || !strings.Contains(events[1].Summary, "默认库存") {
		t.Fatalf("发卡摘要应包含卡密组名称: %+v", events[1])
	}
	if events[2].ActionType != ActionConfirmShipment {
		t.Fatalf("第二个动作应为确认发货: %+v", events[2])
	}
	// event 是当前遍历的事件。
	for _, event := range events {
		if strings.Contains(event.Summary+event.Reason, "SECRET-CARD-1") {
			t.Fatalf("审计事件泄露卡密正文: %+v", event)
		}
		if event.RunID == 0 || event.OrderID != "order-audit" || event.Source != "ws" {
			t.Fatalf("事件缺少运行/订单/来源: %+v", event)
		}
	}
}

// TestAuditRecordsActionFailureAndRetrySuccess 验证确定未发送的失败留下 action_failed 与 failed 终态，重试成功后追加成功终态。
func TestAuditRecordsActionFailureAndRetrySuccess(t *testing.T) {
	// store、cleanup 是隔离的测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是测试根上下文。
	ctx := context.Background()
	// admin 是规则所有者。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "gift", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "gift", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	// sender 先返回确定未发送错误，随后恢复。
	sender := &testSender{err: fmt.Errorf("%w: websocket reconnecting", ErrMessageNotSent)}
	// center 是被测自动化中心。
	center := New(store, testSenderProvider{sender: sender}, nil)
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "o-fail", ChatID: "chat", BuyerID: "buyer"}); err == nil {
		t.Fatal("首次发送应失败")
	}
	// firstPass 是首次失败后的时间线。
	firstPass := auditKinds(auditTimeline(t, store))
	// want 是期望的事件序列，用于与实际序列整体比较。
	if want := "run_started:running,action_failed:failed,run_finished:failed"; strings.Join(firstPass, ",") != want {
		t.Fatalf("失败时间线=%v want %s", firstPass, want)
	}
	// events 保存读取到的审计事件，用于断言内容与顺序。
	if events := auditTimeline(t, store); !strings.Contains(events[1].Reason, "websocket reconnecting") || events[2].Reason == "" {
		t.Fatalf("失败事件应携带原因: %+v", events)
	}

	sender.err = nil
	_, _ = store.DB.ExecContext(ctx, `UPDATE automation_runs SET next_retry_at=0 WHERE order_id='o-fail'`)
	NewScheduler(center).runRecoveryTasks(ctx)
	// all 是重试后的完整时间线，末尾必须出现成功终态。
	all := auditKinds(auditTimeline(t, store))
	// last 是时间线最后一个事件的类型与状态，用于断言重试后的终态。
	if last := all[len(all)-1]; last != "run_finished:success" {
		t.Fatalf("重试成功后末尾应为成功终态: %v", all)
	}
}

// TestAuditRecordsSkipQueueAndDelayedAction 验证账号暂停排队、账号停用跳过、无匹配规则跳过和动作延迟排队。
func TestAuditRecordsSkipQueueAndDelayedAction(t *testing.T) {
	// store、cleanup 是隔离的测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是测试根上下文。
	ctx := context.Background()
	// admin 是规则所有者。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "delayed", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "later", DelaySeconds: 30, Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	// center 是被测自动化中心。
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)

	// 延迟动作：写入延迟队列并留下 queued 事件。
	if err := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "o-delay", ChatID: "chat", BuyerID: "buyer"}); err != nil {
		t.Fatal(err)
	}
	// 账号暂停：事件持久化排队。
	if _, err := store.Cookies.SetPause(ctx, "cid", 10); err != nil {
		t.Fatal(err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "o-pause", ChatID: "chat2", BuyerID: "buyer"}); err != nil {
		t.Fatal(err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := store.Cookies.SetPause(ctx, "cid", 0); err != nil {
		t.Fatal(err)
	}
	// 没有匹配规则：订单创建触发器未配置规则。
	if err := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerOrderCreated, OrderID: "o-norule", ChatID: "chat3", BuyerID: "buyer"}); err != nil {
		t.Fatal(err)
	}
	// 账号停用：只记录事实不执行。
	if err := store.Cookies.SetStatus(ctx, "cid", false); err != nil {
		t.Fatal(err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := center.HandleTask(ctx, Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "o-off", ChatID: "chat4", BuyerID: "buyer"}); err != nil {
		t.Fatal(err)
	}

	// byOrder 把时间线按订单号归类，断言每个场景的事件。
	byOrder := map[string][]string{}
	// reasons 保存每个订单最后一条事件的原因文本。
	reasons := map[string]string{}
	// event 是当前遍历的事件。
	for _, event := range auditTimeline(t, store) {
		byOrder[event.OrderID] = append(byOrder[event.OrderID], event.EventType+":"+event.Status)
		reasons[event.OrderID] = event.Reason
	}
	// got 保存被测调用的实际返回值，用于与期望值比较。
	if got := strings.Join(byOrder["o-delay"], ","); got != "run_started:running,queued:queued" {
		t.Fatalf("延迟动作时间线=%s", got)
	}
	if !strings.Contains(reasons["o-delay"], "30") || !strings.Contains(reasons["o-delay"], "发送文本消息") {
		t.Fatalf("延迟原因应说明秒数和动作: %q", reasons["o-delay"])
	}
	// got 保存被测调用的实际返回值，用于与期望值比较。
	if got := strings.Join(byOrder["o-pause"], ","); got != "queued:queued" {
		t.Fatalf("暂停时间线=%s", got)
	}
	// got 保存被测调用的实际返回值，用于与期望值比较。
	if got := strings.Join(byOrder["o-norule"], ","); got != "skipped:skipped" {
		t.Fatalf("无规则时间线=%s", got)
	}
	// got 保存被测调用的实际返回值，用于与期望值比较。
	if got := strings.Join(byOrder["o-off"], ","); got != "skipped:skipped" || reasons["o-off"] != "账号已停用" {
		t.Fatalf("停用时间线=%s reason=%q", got, reasons["o-off"])
	}
}

// TestAuditRecordsPaidDeliverySkippedWhenAutoDeliveryOff 验证账号关闭自动发货时付款事件只留下跳过记录。
func TestAuditRecordsPaidDeliverySkippedWhenAutoDeliveryOff(t *testing.T) {
	// store、cleanup 是隔离的测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是测试根上下文。
	ctx := context.Background()
	// admin 是账号所有者。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// off 把自动发货总开关设为关闭。
	off := false
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := store.Cookies.UpdateSettings(ctx, "cid", db.AccountSettingsUpdate{UserID: admin.ID, AutoConfirm: &off}); err != nil {
		t.Fatal(err)
	}
	// center 是被测自动化中心。
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := center.HandleTask(ctx, Task{Source: "ws", AccountID: "cid", OrderRole: OrderRoleSeller, TriggerType: TriggerOrderPaid, OrderID: "o-autooff", ChatID: "chat", ItemID: "item-1", BuyerID: "buyer"}); err != nil {
		t.Fatal(err)
	}
	// events 是仅含跳过记录的时间线。
	events := auditTimeline(t, store)
	if len(events) != 1 || events[0].Status != db.AuditStatusSkipped || events[0].Reason != "账号未开启自动发货" {
		t.Fatalf("events=%+v", events)
	}
}

// TestAuditRecordsBargainFreeShipResult 验证免拼开关关闭时留下跳过记录，开启后记录免拼结果。
func TestAuditRecordsBargainFreeShipResult(t *testing.T) {
	// store、cleanup 是隔离的测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是测试根上下文。
	ctx := context.Background()
	// center 是被测自动化中心。
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	// task 是砍价待刀成阶段的 WebSocket 任务。
	task := Task{Source: "ws", AccountID: "cid", OrderRole: OrderRoleSeller, TriggerType: TriggerBargainPending, OrderID: "o-bargain", ChatID: "chat", ItemID: "item-1", BuyerID: "buyer"}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := center.handleBargainPending(ctx, task); err != nil {
		t.Fatal(err)
	}
	// events 保存读取到的审计事件，用于断言内容与顺序。
	if events := auditTimeline(t, store); len(events) != 1 || events[0].Status != db.AuditStatusSkipped {
		t.Fatalf("开关关闭应只记录跳过: %+v", events)
	}
	// recordBargainResult 直接验证三种阶段结果到审计状态的映射。
	center.recordBargainResult(ctx, task, "succeeded", nil)
	center.recordBargainResult(ctx, task, "failed", fmt.Errorf("平台拒绝"))
	center.recordBargainResult(ctx, task, "needs_review", fmt.Errorf("结果未知"))
	// events 是追加后的完整时间线。
	events := auditTimeline(t, store)
	// got 保存被测调用的实际返回值，用于与期望值比较。
	if got := strings.Join(auditKinds(events), ","); got != "skipped:skipped,bargain_free_ship:success,bargain_free_ship:failed,bargain_free_ship:needs_review" {
		t.Fatalf("免拼时间线=%s", got)
	}
	if events[2].Reason != "平台拒绝" {
		t.Fatalf("失败原因缺失: %+v", events[2])
	}
}

// TestAuditRecorderNeverFailsAutomation 验证记录器在零值、缺少账号和账号不存在时都不 panic、不上抛错误。
func TestAuditRecorderNeverFailsAutomation(t *testing.T) {
	// store、cleanup 是隔离的测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是测试根上下文。
	ctx := context.Background()
	// 零值记录器与缺少账号都是空操作。
	auditRecorder{}.skipped(ctx, Task{AccountID: "cid"}, "x")
	newAuditRecorder(store, nil).skipped(ctx, Task{}, "x")
	// 账号不存在时写入失败只记日志，logger 为空时同样安全。
	newAuditRecorder(store, nil).skipped(ctx, Task{AccountID: "missing"}, "x")
	newAuditRecorder(store, slog.Default()).skipped(ctx, Task{AccountID: "missing"}, "x")
	// 已取消的调用方上下文不影响审计写入。
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	newAuditRecorder(store, nil).skipped(canceled, Task{AccountID: "cid", Source: "ws"}, "取消后仍写入")
	// events 是取消上下文下写入的事件。
	events := auditTimeline(t, store)
	if len(events) != 1 || events[0].Reason != "取消后仍写入" {
		t.Fatalf("events=%+v", events)
	}
	// 延期重放的来源统一标记为 recovery。
	newAuditRecorder(store, nil).skipped(ctx, Task{AccountID: "cid", Source: "ws", Raw: map[string]any{"automation_deferred_replay": true}}, "重放")
	if events = auditTimeline(t, store); events[1].Source != "recovery" {
		t.Fatalf("重放来源=%q", events[1].Source)
	}
}

// TestAuditSummaryAndTruncation 验证动作摘要只使用名称与数量，文本按字符截断。
func TestAuditSummaryAndTruncation(t *testing.T) {
	// cases 描述各动作类型的期望摘要。
	cases := []struct {
		name   string
		action db.AutomationAction
		result actionExecutionResult
		want   string
	}{
		{"发卡取实际数量", db.AutomationAction{ActionType: ActionSendCard, CardName: "组A", DeliveryCount: 3}, actionExecutionResult{sent: 2}, "发送卡密「组A」×2"},
		{"发卡回退配置数量", db.AutomationAction{ActionType: ActionSendCard, CardName: "组A", DeliveryCount: 3}, actionExecutionResult{}, "发送卡密「组A」×3"},
		{"发卡最少一张", db.AutomationAction{ActionType: ActionSendCard, CardName: "组A"}, actionExecutionResult{}, "发送卡密「组A」×1"},
		{"模板", db.AutomationAction{ActionType: ActionSendTemplate, DeliveryTemplateName: "T", TemplateMessages: []string{"a", "b"}}, actionExecutionResult{}, "发送发货模板「T」（2 条消息）"},
		{"文本不含正文", db.AutomationAction{ActionType: ActionSendText, MessageTemplate: "秘密正文"}, actionExecutionResult{}, "发送文本消息"},
		{"确认发货", db.AutomationAction{ActionType: ActionConfirmShipment}, actionExecutionResult{}, "确认发货"},
		{"改价", db.AutomationAction{ActionType: ActionAdjustPrice}, actionExecutionResult{}, "修改订单价格"},
		{"未知类型", db.AutomationAction{ActionType: "custom"}, actionExecutionResult{}, "custom"},
	}
	// tc 是当前遍历的表驱动用例，包含输入与期望结果。
	for _, tc := range cases {
		// got 保存被测调用的实际返回值，用于与期望值比较。
		if got := summarizeAction(tc.action, tc.result); got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
	// got 保存被测调用的实际返回值，用于与期望值比较。
	if got := truncateAuditText("  短文本  ", 10); got != "短文本" {
		t.Fatalf("got %q", got)
	}
	// got 保存被测调用的实际返回值，用于与期望值比较。
	if got := truncateAuditText("一二三四五六", 3); got != "一二三…" {
		t.Fatalf("got %q", got)
	}
}

// TestSchedulerPrunesExpiredAuditEventsDaily 验证调度器清理超过保留期的审计事件，且一天内不重复清理。
func TestSchedulerPrunesExpiredAuditEventsDaily(t *testing.T) {
	// store、cleanup 是隔离的测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是测试根上下文。
	ctx := context.Background()
	// now 是写入测试事件使用的当前时间。
	now := time.Now().UTC()
	// at 是当前遍历的事件发生时刻（Unix 秒）。
	for _, at := range []time.Time{now.Add(-auditRetention - time.Hour), now.Add(-time.Hour)} {
		// err 保存被测调用返回的错误，用于断言成功或期望的失败。
		if err := store.Automation.AppendAuditEvent(ctx, db.AutomationAuditEvent{CookieID: "cid", EventType: "skipped", Status: db.AuditStatusSkipped, CreatedAt: at.Unix()}); err != nil {
			t.Fatal(err)
		}
	}
	// scheduler 是被测调度器。
	scheduler := NewScheduler(New(store, testSenderProvider{sender: &testSender{}}, nil))
	scheduler.pruneAuditEvents(ctx)
	// events 保存读取到的审计事件，用于断言内容与顺序。
	if events := auditTimeline(t, store); len(events) != 1 {
		t.Fatalf("应只剩未过期事件: %+v", events)
	}
	// 一天内再次调用不应再清理：补写一条过期事件后仍保留。
	if err := store.Automation.AppendAuditEvent(ctx, db.AutomationAuditEvent{CookieID: "cid", EventType: "skipped", Status: db.AuditStatusSkipped, CreatedAt: now.Add(-auditRetention - time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}
	scheduler.pruneAuditEvents(ctx)
	// events 保存读取到的审计事件，用于断言内容与顺序。
	if events := auditTimeline(t, store); len(events) != 2 {
		t.Fatalf("同一天内不应重复清理: %+v", events)
	}
}
