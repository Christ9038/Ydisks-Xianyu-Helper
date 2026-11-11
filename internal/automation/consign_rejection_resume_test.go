package automation

import (
	"context"
	"errors"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// consignRejectionFixture 是“付款发卡 + 确认发货”端到端夹具，返回上下文、数据库、发送替身、平台替身、中心与订单号。
func consignRejectionFixture(t *testing.T, platform *fakeMTop) (context.Context, *db.Store, *testSender, *Center, string) {
	t.Helper()
	// store、cleanup 是隔离数据库及关闭函数。
	store, cleanup := newAutomationTestStore(t)
	t.Cleanup(cleanup)
	// ctx 是本夹具共用的数据库上下文。
	ctx := context.Background()
	// admin、adminErr 保存规则所属管理员。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// enabled 同时打开自动发货与自动确认发货。
	enabled := true
	// settingsErr 保存账号开关写入错误。
	if _, settingsErr := store.Cookies.UpdateSettings(ctx, "cid", db.AccountSettingsUpdate{UserID: admin.ID, AutoConfirm: &enabled, AutoConsign: &enabled}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// orderID 是本夹具的待发货订单号。
	orderID := "sensitive-order"
	// orderErr 保存订单事实写入错误。
	if orderErr := store.Orders.Upsert(ctx, orderID, db.OrderUpsertOpts{CookieID: "cid", ItemID: "10001", BuyerID: "20002", ChatID: "chat-1", OrderStatus: "pending_ship", Quantity: "1"}); orderErr != nil {
		t.Fatal(orderErr)
	}
	// cardID、cardErr 保存一次性数据卡密组。
	cardID, cardErr := store.Cards.Create(ctx, &db.CardFull{Name: "敏感词测试卡", Type: "data", DataContent: "网盘链接 SECRET-1", Enabled: true, UserID: admin.ID})
	if cardErr != nil {
		t.Fatal(cardErr)
	}
	// ruleErr 保存先发卡后确认发货的付款规则写入错误。
	if _, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: admin.ID, CookieID: "cid", ItemID: "10001", Name: "敏感词拒绝", TriggerType: TriggerOrderPaid, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, ConfigJSON: `{}`, Enabled: true, SortOrder: 1}, {ActionType: ActionConfirmShipment, Enabled: true, SortOrder: 2}}}); ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// sender 记录发给买家的消息。
	sender := &testSender{}
	// center 是注入平台替身和在线发送器的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{MTop: platform})
	return ctx, store, sender, center, orderID
}

// consignRunState 读取订单付款运行的状态、动作占用标记和已发送数量。
func consignRunState(t *testing.T, store *db.Store, orderID string) (status string, actionStarted, sent int) {
	t.Helper()
	// scanErr 保存运行状态读取错误。
	if scanErr := store.DB.QueryRowContext(context.Background(),
		`SELECT status,action_started,sent_count FROM automation_runs WHERE order_id=? AND trigger_type='order_paid' ORDER BY id DESC LIMIT 1`, orderID).Scan(&status, &actionStarted, &sent); scanErr != nil {
		t.Fatal(scanErr)
	}
	return status, actionStarted, sent
}

// newResumeScheduler 构造只驱动待发货续跑扫描的调度器，冷却与结算窗口关闭以便单测立即执行。
func newResumeScheduler(center *Center) *Scheduler {
	return &Scheduler{center: center, pendingShipCooldown: map[string]time.Time{}, pendingShipSettleWindow: -1}
}

// TestConsignSensitiveWordRejectionResumesOnlyConfirmShipment 验证 #113 端到端链路：
// 发卡成功后确认发货被平台以敏感词明确拒绝 → 运行转人工核对但清除动作占用 → 用户改为固定文字后，
// 待发货续跑只补做确认发货、携带固定文字，且不重发卡密。
func TestConsignSensitiveWordRejectionResumesOnlyConfirmShipment(t *testing.T) {
	// platform 第一次以敏感词拒绝确认发货（真实客户端对该返回码给出 ok=false、err=nil），之后成功。
	platform := &fakeMTop{consignResults: []fakeConsignResult{{ret: []string{"HIT_SENSITIVE_WORD::命中敏感字符"}}, {ok: true, ret: []string{"SUCCESS::调用成功"}}}}
	// ctx、store、sender、center、orderID 来自端到端夹具。
	ctx, store, sender, center, orderID := consignRejectionFixture(t, platform)

	// handleErr 是付款自动发货结果；确认发货被拒必须报错，不能伪装成功。
	handleErr := center.HandleTask(ctx, Task{Source: "ws", AccountID: "cid", OrderRole: OrderRoleSeller, TriggerType: TriggerOrderPaid, OrderID: orderID, ItemID: "10001", BuyerID: "20002", ChatID: "chat-1"})
	if handleErr == nil {
		t.Fatal("确认发货被拒绝时付款运行应返回错误")
	}
	if len(sender.texts) != 1 || platform.consignCalls != 1 || platform.consignTradeTextIn != "网盘链接 SECRET-1" {
		t.Fatalf("首轮应发卡一次并携带卡密确认一次: texts=%v calls=%d text=%q", sender.texts, platform.consignCalls, platform.consignTradeTextIn)
	}
	// status、actionStarted、sent 是首轮结束后的运行状态。
	status, actionStarted, sent := consignRunState(t, store, orderID)
	if status != "needs_review" || actionStarted != 0 || sent != 1 {
		t.Fatalf("明确拒绝应转人工核对并清除动作占用: status=%s action_started=%d sent=%d", status, actionStarted, sent)
	}

	// admin、adminErr 保存写入凭证设置所需的用户。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// mode、text 是用户改用的固定文字凭证。
	mode, text := db.ConsignProofModeFixed, "已通过聊天发送"
	// updateErr 保存凭证设置写入错误。
	if _, updateErr := store.Cookies.UpdateSettings(ctx, "cid", db.AccountSettingsUpdate{UserID: admin.ID, ConsignProofMode: &mode, ConsignProofText: &text}); updateErr != nil {
		t.Fatal(updateErr)
	}

	newResumeScheduler(center).scanPendingShipResumes(ctx)
	if platform.consignCalls != 2 || platform.consignTradeTextIn != text {
		t.Fatalf("续跑应只补确认发货并携带固定文字: calls=%d text=%q", platform.consignCalls, platform.consignTradeTextIn)
	}
	if len(sender.texts) != 1 {
		t.Fatalf("续跑不得重发卡密: %v", sender.texts)
	}
	// order、orderErr 验证补确认成功后本地订单已发货。
	order, orderErr := store.Orders.Get(ctx, orderID)
	if orderErr != nil || order.OrderStatus != "shipped" {
		t.Fatalf("补确认后订单应为已发货: order=%+v err=%v", order, orderErr)
	}
	// status 是补确认后的运行状态。
	if status, _, _ := consignRunState(t, store, orderID); status != "success" {
		t.Fatalf("补确认后运行应成功: status=%s", status)
	}
}

// TestConsignUncertainResultStaysOutOfResumeScan 验证确认发货结果真正不确定（传输错误）时保留动作占用，
// 续跑扫描既不重开也不调用平台，只能人工核对（#113 第二处：不再被每轮重复选中）。
func TestConsignUncertainResultStaysOutOfResumeScan(t *testing.T) {
	// platform 以传输错误模拟请求可能已到达平台但响应丢失。
	platform := &fakeMTop{consignErr: errors.New("connection reset by peer")}
	// ctx、store、center、orderID 来自端到端夹具。
	ctx, store, _, center, orderID := consignRejectionFixture(t, platform)

	// handleErr 是付款自动发货结果；结果不确定必须报错。
	if handleErr := center.HandleTask(ctx, Task{Source: "ws", AccountID: "cid", OrderRole: OrderRoleSeller, TriggerType: TriggerOrderPaid, OrderID: orderID, ItemID: "10001", BuyerID: "20002", ChatID: "chat-1"}); handleErr == nil {
		t.Fatal("确认发货结果不确定时付款运行应返回错误")
	}
	// status、actionStarted 是首轮结束后的运行状态。
	status, actionStarted, _ := consignRunState(t, store, orderID)
	if status != "needs_review" || actionStarted != 1 {
		t.Fatalf("结果不确定应转人工核对并保留动作占用: status=%s action_started=%d", status, actionStarted)
	}
	// candidates、candidateErr 验证该运行不再出现在续跑候选中。
	candidates, candidateErr := store.Automation.PendingShipResumableRunsAfter(ctx, "", 5, 200)
	if candidateErr != nil || len(candidates) != 0 {
		t.Fatalf("动作占用中的运行不应成为续跑候选: %+v err=%v", candidates, candidateErr)
	}
	newResumeScheduler(center).scanPendingShipResumes(ctx)
	if platform.consignCalls != 1 {
		t.Fatalf("续跑扫描不得再次调用确认发货: calls=%d", platform.consignCalls)
	}
}
