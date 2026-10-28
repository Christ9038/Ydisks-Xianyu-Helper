package automation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"xianyu-go/internal/db"
)

// TestIsPlatformBlockErr 验证风控、会话失效和普通错误的阻断分类，nil 与普通错误不得触发阻断。
func TestIsPlatformBlockErr(t *testing.T) {
	// cases 列出错误样例及其期望的阻断结论。
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"普通错误", errors.New("temporary"), false},
		{"风控", errors.New("FAIL_SYS_USER_VALIDATE::RGV587_ERROR"), true},
	}
	// tc 是当前待验证的错误样例。
	for _, tc := range cases {
		// got 是 isPlatformBlockErr 对当前样例的实际判定。
		if got := isPlatformBlockErr(tc.err); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

// TestRunDeferredTasksStopsAccountAfterRiskVerification 验证同账号首个延迟任务触发风控后，其余任务不再执行，
// 保持 running 等待租约到期，而不是继续请求平台。
func TestRunDeferredTasksStopsAccountAfterRiskVerification(t *testing.T) {
	// store、cleanup 是临时数据库及其释放函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是不带取消的测试上下文。
	ctx := context.Background()
	// admin 是规则的所属用户。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// err 保存规则创建错误。
	if _, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "risk-stop", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "x", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	// orderIDs 是按入队顺序执行的两个同账号订单。
	orderIDs := []string{"risk-1", "risk-2"}
	// orderID 是当前入队订单。
	for _, orderID := range orderIDs {
		// task 是当前订单对应的买家评价延迟任务，delays_elapsed 表示动作延迟已走完。
		task := Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: orderID, ChatID: "chat", BuyerID: "buyer", Raw: map[string]any{"delays_elapsed": true}}
		// raw 是任务的持久化 JSON 快照。
		raw, _ := json.Marshal(task)
		// err 保存延迟任务入队错误。
		if err := store.Automation.DeferTask(ctx, db.DeferredAutomationTask{TaskKey: "cid:buyer_reviewed:" + orderID, CookieID: "cid", TriggerType: TriggerBuyerReviewed, TaskJSON: string(raw), DueAt: 0}); err != nil {
			t.Fatal(err)
		}
	}
	// sender 对每次发送都返回风控错误。
	sender := &testSender{err: errors.New("FAIL_SYS_USER_VALIDATE::RGV587_ERROR")}
	// center 使用上述发送器，使首个任务以风控错误失败。
	center := New(store, testSenderProvider{sender: sender}, nil)
	_ = (&Scheduler{center: center}).runDeferredTasks(ctx)
	// statusOf 读取指定订单延迟任务的当前状态。
	statusOf := func(orderID string) string {
		// status 是延迟任务表中的状态列。
		var status string
		// err 保存状态查询错误。
		if err := store.DB.QueryRowContext(ctx, `SELECT status FROM automation_pending_tasks WHERE task_key=?`, "cid:buyer_reviewed:"+orderID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		return status
	}
	// got 是首个任务执行后的状态：失败后应回到 pending 等待退避重试。
	if got := statusOf("risk-1"); got != "pending" {
		t.Fatalf("首个任务应回到 pending 等待重试, got %s", got)
	}
	// got 是同账号第二个任务的状态：被阻断跳过，仍保持已领取的 running。
	if got := statusOf("risk-2"); got != "running" {
		t.Fatalf("同账号后续任务应被跳过并保持 running, got %s", got)
	}
}

// TestRunDeferredTasksNeverReplaysBargainPending 验证延期队列重放「待刀成」时不调用免拼：
// 任务被结束（不再重试），免拼阶段执行权保持未领取，由人工核对处理。
func TestRunDeferredTasksNeverReplaysBargainPending(t *testing.T) {
	// store、cleanup 是临时数据库及其释放函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是不带取消的测试上下文。
	ctx := context.Background()
	// task 是暂停期间保存的 WebSocket「待刀成」事件快照。
	task := Task{Source: "ws", AccountID: "cid", TriggerType: TriggerBargainPending, OrderID: "bargain-replay-1", ChatID: "chat", BuyerID: "buyer", IsBargain: true}
	// raw 是任务的持久化 JSON 快照。
	raw, _ := json.Marshal(task)
	// err 保存延迟任务入队错误。
	if err := store.Automation.DeferTask(ctx, db.DeferredAutomationTask{TaskKey: "cid:bargain_pending:bargain-replay-1", CookieID: "cid", TriggerType: TriggerBargainPending, TaskJSON: string(raw), DueAt: 0}); err != nil {
		t.Fatal(err)
	}
	// center 不注入任何发送器；若错误地走到免拼，会在领取执行权后留下痕迹。
	center := New(store, nil, nil)
	// err 保存延期任务重放的状态收口错误。
	if err := (&Scheduler{center: center}).runDeferredTasks(ctx); err != nil {
		t.Fatalf("取消重放不应返回错误: %v", err)
	}
	// remaining 是延期队列中剩余的该任务数量。
	var remaining int
	// err 保存延期队列剩余数量的查询错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_pending_tasks WHERE task_key=?`, "cid:bargain_pending:bargain-replay-1").Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("待刀成延期任务应被结束而不是继续重试, 剩余 %d", remaining)
	}
	// claimed 表示此时仍可领取免拼执行权，即重放没有触发免拼。
	claimed, claimErr := store.Automation.ClaimBargainFreeShipping(ctx, "bargain-replay-1", "cid")
	if claimErr != nil || !claimed {
		t.Fatalf("重放不得领取免拼执行权: claimed=%v err=%v", claimed, claimErr)
	}
}

// TestPaidOrderFactsComplete 验证付款订单事实齐全判定：数量必须是正整数，金额与完整规格缺一不可。
func TestPaidOrderFactsComplete(t *testing.T) {
	// full 是事实齐全的付款任务。
	full := Task{TriggerType: TriggerOrderPaid, OrderID: "o1", Quantity: "2", Amount: "9.90", SpecName: "套餐", SpecValue: "30天"}
	// cases 依次修改 full 的一个字段，期望的齐全结论。
	cases := []struct {
		name   string
		mutate func(*Task)
		want   bool
	}{
		{"齐全", func(*Task) {}, true},
		{"数量缺失", func(task *Task) { task.Quantity = "" }, false},
		{"数量非法", func(task *Task) { task.Quantity = "abc" }, false},
		{"数量为零", func(task *Task) { task.Quantity = "0" }, false},
		{"金额缺失", func(task *Task) { task.Amount = " " }, false},
		{"规格名缺失", func(task *Task) { task.SpecName = "" }, false},
		{"规格值缺失", func(task *Task) { task.SpecValue = "" }, false},
	}
	// tc 是当前待验证样例。
	for _, tc := range cases {
		// task 是应用了当前修改的任务副本。
		task := full
		tc.mutate(&task)
		// got 是 paidOrderFactsComplete 对当前样例的实际判定。
		if got := paidOrderFactsComplete(task); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

// TestPrepareTaskSkipsOrderDetailWhenPaidFactsComplete 验证付款任务的本地订单事实齐全时不请求平台订单详情，
// 缺少规格时仍会请求并补齐。
func TestPrepareTaskSkipsOrderDetailWhenPaidFactsComplete(t *testing.T) {
	// store、cleanup 是临时数据库及其释放函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是不带取消的测试上下文。
	ctx := context.Background()
	// calls 统计订单详情查询次数。
	calls := 0
	// center 注入带计数的详情查询器，返回补齐用的规格。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{
		OrderDetailFetcher: testFetcher{detail: &OrderDetail{SpecName: "套餐", SpecValue: "30天", Quantity: "1", Amount: "9.9"}, calls: &calls},
	})
	// paid 构造指定订单号的付款任务。
	paid := func(orderID string) Task {
		return Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: orderID, ItemID: "item-1", BuyerID: "buyer", ChatID: "chat", CookieStr: "unb=1"}
	}
	// 本地订单事实齐全：不得请求平台。
	if err := store.Orders.Upsert(ctx, "facts-full", db.OrderUpsertOpts{CookieID: "cid", ItemID: "item-1", BuyerID: "buyer", ChatID: "chat",
		SpecName: "套餐", SpecValue: "90天", Quantity: "2", Amount: "19.8"}); err != nil {
		t.Fatal(err)
	}
	// prepared、err 保存事实齐全订单的准备结果。
	prepared, err := center.prepareTask(ctx, paid("facts-full"))
	if err != nil || calls != 0 {
		t.Fatalf("事实齐全不应请求订单详情: calls=%d err=%v", calls, err)
	}
	if prepared.SpecValue != "90天" || prepared.Quantity != "2" {
		t.Fatalf("应沿用本地订单事实: %+v", prepared)
	}
	// 本地缺少规格：必须请求并补齐。
	if err := store.Orders.Upsert(ctx, "facts-missing", db.OrderUpsertOpts{CookieID: "cid", ItemID: "item-1", BuyerID: "buyer", ChatID: "chat", Quantity: "1", Amount: "9.9"}); err != nil {
		t.Fatal(err)
	}
	// filled、fillErr 保存缺规格订单的准备结果。
	filled, fillErr := center.prepareTask(ctx, paid("facts-missing"))
	if fillErr != nil || calls != 1 || filled.SpecValue != "30天" {
		t.Fatalf("缺规格应请求并补齐: calls=%d spec=%q err=%v", calls, filled.SpecValue, fillErr)
	}
}

// TestSupplementEventFactsDeterministicOrder 验证报文含多个候选订单号时按字段名字典序取值，重复解析结果一致。
func TestSupplementEventFactsDeterministicOrder(t *testing.T) {
	// raw 在不同字段中携带两个不同的候选订单号；字典序下字段 a 最先，应固定取其订单号。
	raw := map[string]any{
		"b": "https://example.invalid/x?orderId=2222222222",
		"a": "https://example.invalid/x?orderId=1111111111",
		"c": map[string]any{"z": "orderId=3333333333", "y": "orderId=4444444444"},
	}
	// i 是重复次数；随机遍历时 50 次内几乎必然出现不一致。
	for i := 0; i < 50; i++ {
		// got 是本次解析得到的订单号。
		got := fieldsFromRaw(raw).orderID
		if got != "1111111111" {
			t.Fatalf("第 %d 次解析订单号=%q，应固定为字典序最先的 1111111111", i, got)
		}
	}
}
