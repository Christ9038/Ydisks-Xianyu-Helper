package automation

import (
	"context"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestPaidDeliveryUsesNormalConsignForBargainReadyOrder 验证开启免拼后的两段 WebSocket 严格按免拼、发卡、普通确认发货顺序执行。
func TestPaidDeliveryUsesNormalConsignForBargainReadyOrder(t *testing.T) {
	// store、cleanup 保存付款自动发货主链路使用的测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是创建订单、规则和执行自动化任务共用的上下文。
	ctx := context.Background()
	// admin、adminErr 保存规则归属管理员及读取错误。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// enabled 是同时打开付款自动发货和订单状态确认开关的设置值。
	enabled := true
	// _, settingsErr 保存账号自动化开关写入结果。
	_, settingsErr := store.Cookies.UpdateSettings(ctx, "cid", db.AccountSettingsUpdate{UserID: admin.ID, AutoConfirm: &enabled, AutoConsign: &enabled, AutoBargain: &enabled})
	if settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// bargain 表示订单列表同步已确认当前订单属于砍价活动。
	bargain := true
	// orderErr 保存砍价订单事实写入错误。
	orderErr := store.Orders.Upsert(ctx, "paid-bargain-order", db.OrderUpsertOpts{CookieID: "cid", ItemID: "10001", BuyerID: "20002", ChatID: "chat-1", OrderStatus: "pending_ship", IsBargain: &bargain})
	if orderErr != nil {
		t.Fatal(orderErr)
	}
	// cardID、cardErr 保存满足付款自动发货完整性规则的一次性数据卡密组及创建错误。
	cardID, cardErr := store.Cards.Create(ctx, &db.CardFull{Name: "砍价免拼测试卡", Type: "data", DataContent: "BARGAIN-CODE", Enabled: true, UserID: admin.ID})
	if cardErr != nil {
		t.Fatal(cardErr)
	}
	// _, ruleErr 保存先发货内容后确认平台状态的付款规则创建结果。
	_, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: admin.ID, CookieID: "cid", ItemID: "10001", Name: "砍价免拼", TriggerType: TriggerOrderPaid, Enabled: true, Actions: []db.AutomationActionInput{{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, ConfigJSON: `{}`, Enabled: true, SortOrder: 1}, {ActionType: ActionConfirmShipment, Enabled: true, SortOrder: 2}}})
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// client 是断言最终阶段使用普通确认发货端点的 MTOP 内存替身。
	client := &fakeMTop{consignOk: true, consignRet: []string{"SUCCESS::调用成功"}, freeShippingOK: true, freeShippingRet: []string{"SUCCESS::调用成功"}}
	// sender 是接收测试卡密内容的在线消息发送替身。
	sender := &testSender{}
	// center 是注入免拼替身与在线发送器的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{MTop: client})
	// pendingErr 保存第一段“待刀成”WebSocket 的免拼执行结果。
	pendingErr := center.HandleTask(ctx, Task{Source: "ws", AccountID: "cid", OrderRole: OrderRoleSeller, TriggerType: TriggerBargainPending, OrderID: "paid-bargain-order", ItemID: "10001", BuyerID: "20002", ChatID: "chat-1", IsBargain: true})
	if pendingErr != nil {
		t.Fatalf("砍价待刀成免拼失败: %v", pendingErr)
	}
	if client.freeShippingCalls != 1 || client.consignCalls != 0 || len(sender.texts) != 0 {
		t.Fatalf("最终待发货消息到达前发生发卡或确认抢跑: free=%d consign=%d messages=%+v", client.freeShippingCalls, client.consignCalls, sender.texts)
	}
	// handleErr 保存付款自动发货执行结果。
	handleErr := center.HandleTask(ctx, Task{Source: "ws", AccountID: "cid", OrderRole: OrderRoleSeller, TriggerType: TriggerOrderPaid, OrderID: "paid-bargain-order", ItemID: "10001", BuyerID: "20002", ChatID: "chat-1", IsBargain: true})
	if handleErr != nil {
		t.Fatalf("砍价付款自动发货失败: %v", handleErr)
	}
	if len(sender.texts) != 1 || client.freeShippingCalls != 1 || client.consignCalls != 1 {
		t.Fatalf("成功小刀后发卡和普通确认发货顺序异常: messages=%+v free=%d consign=%d", sender.texts, client.freeShippingCalls, client.consignCalls)
	}
}

// TestConfirmShipmentUsesNormalConsignForBargainOrder 验证砍价订单的最终确认发货始终使用普通确认发货接口。
func TestConfirmShipmentUsesNormalConsignForBargainOrder(t *testing.T) {
	// store、cleanup 保存自动化确认发货所需的本地数据库及资源释放函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是本测试所有数据库与自动化操作共用的非取消上下文。
	ctx := context.Background()
	// client 是记录普通确认与免拼确认调用的 MTOP 内存替身。
	client := &fakeMTop{consignOk: true, consignRet: []string{"SUCCESS::调用成功"}}
	// center 是使用替身 MTOP 客户端的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: client})
	// task 是已确认砍价活动且由人工请求立即确认的平台订单。
	task := Task{AccountID: "cid", OrderID: "bargain-order", ItemID: "10001", BuyerID: "20002", IsBargain: true, ForceConfirmShipment: true}
	// confirmErr 保存普通确认发货和本地状态收口过程中的错误。
	confirmErr := center.confirmShipment(ctx, task)
	if confirmErr != nil {
		t.Fatalf("砍价订单普通确认发货失败: %v", confirmErr)
	}
	if client.freeShippingCalls != 0 || client.consignCalls != 1 || client.consignOrderIn != task.OrderID {
		t.Fatalf("砍价订单调用路径错误: free=%d consign=%d order=%q", client.freeShippingCalls, client.consignCalls, client.consignOrderIn)
	}
	// order、orderErr 保存免拼成功后读取到的本地订单发货事实。
	order, orderErr := store.Orders.Get(ctx, task.OrderID)
	if orderErr != nil || order.OrderStatus != "shipped" || !order.SystemShipped {
		t.Fatalf("普通确认成功后的订单状态错误: order=%+v err=%v", order, orderErr)
	}
}

// TestBargainPendingRejectsIncompletePlatformIdentifiers 验证免拼阶段缺少平台标识时在网络调用前安全停止。
func TestBargainPendingRejectsIncompletePlatformIdentifiers(t *testing.T) {
	// store、cleanup 保存本测试使用的自动化存储及清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// client 是若被错误调用即可暴露问题的 MTOP 内存替身。
	client := &fakeMTop{freeShippingOK: true}
	// center 是注入上述替身的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: client})
	// freeShipErr 保存缺少买家标识时的免拼阶段预检拒绝结果。
	freeShipErr := center.actions.freeShipBargain(context.Background(), Task{AccountID: "cid", OrderID: "bargain-incomplete", ItemID: "10001", IsBargain: true})
	if freeShipErr == nil || !strings.Contains(freeShipErr.Error(), "免拼发货缺少订单ID、商品ID或买家ID") || client.freeShippingCalls != 0 || client.consignCalls != 0 {
		t.Fatalf("不完整砍价订单未在网络前拒绝: err=%v free=%d consign=%d", freeShipErr, client.freeShippingCalls, client.consignCalls)
	}
}

// TestBargainFreeShippingRecoversExpiredSessionOnce 验证免拼收到明确 Session 失效后只恢复一次凭证，
// 并使用恢复后的 Cookie 重试一次；这类确认未执行的失败不得被误标为结果未知。
func TestBargainFreeShippingRecoversExpiredSessionOnce(t *testing.T) {
	// store、cleanup 保存免拼会话恢复测试的数据库和关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 保存本测试的无取消上下文。
	ctx := context.Background()
	// client 依次模拟明确 Session 失效与凭证恢复后的免拼成功。
	client := &fakeMTop{freeShippingResults: []fakeFreeShippingResult{
		{ret: []string{"FAIL_SYS_SESSION_EXPIRED::Session过期"}},
		{ok: true, ret: []string{"SUCCESS::调用成功"}},
	}}
	// recoverer 会在恢复回调中为测试账号写入新的有效 Cookie。
	recoverer := &fakeCredentialRecoverer{store: store}
	// center 保存注入免拼客户端和凭证恢复器的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: client, OrderDetailFetcher: recoverer})
	// freeShipErr 保存恢复并重试后的免拼执行结果。
	freeShipErr := center.actions.freeShipBargain(ctx, Task{AccountID: "cid", OrderID: "bargain-session", ItemID: "item", BuyerID: "buyer", IsBargain: true})
	if freeShipErr != nil {
		t.Fatalf("免拼 Session 恢复后应成功: %v", freeShipErr)
	}
	if recoverer.calls != 1 || client.freeShippingCalls != 2 {
		t.Fatalf("recover calls=%d free shipping calls=%d，期望 1/2", recoverer.calls, client.freeShippingCalls)
	}
	if len(client.freeShippingCookies) != 2 || !strings.Contains(client.freeShippingCookies[1], "fresh_1") {
		t.Fatalf("恢复后的免拼未使用新凭证: %v", client.freeShippingCookies)
	}
}

// TestBargainPendingRunsOnlyIndependentFreeShipping 验证“待刀成”只读取自动免拼开关并调用免拼，不发卡也不确认发货。
func TestBargainPendingRunsOnlyIndependentFreeShipping(t *testing.T) {
	// store、cleanup 保存隔离的自动化数据库及资源释放函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 保存测试共用的无取消上下文。
	ctx := context.Background()
	// admin、adminErr 保存账号所属管理员和读取错误。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// enabled 是本测试显式开启的独立自动免拼开关。
	enabled := true
	// settingsErr 保存独立开关写入错误。
	if _, settingsErr := store.Cookies.UpdateSettings(ctx, "cid", db.AccountSettingsUpdate{UserID: admin.ID, AutoBargain: &enabled}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// client 是记录阶段接口选择的 MTOP 内存替身。
	client := &fakeMTop{freeShippingOK: true, freeShippingRet: []string{"SUCCESS::调用成功"}}
	// sender 是若被错误使用即可暴露抢跑的在线消息替身。
	sender := &testSender{}
	// center 是待验证的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{MTop: client})
	// handleErr 保存待刀成 WS 任务的执行错误。
	handleErr := center.HandleTask(ctx, Task{Source: "ws", AccountID: "cid", OrderRole: OrderRoleSeller, TriggerType: TriggerBargainPending, OrderID: "bargain-pending", ItemID: "10001", BuyerID: "20002", ChatID: "chat"})
	if handleErr != nil {
		t.Fatalf("待刀成免拼失败: %v", handleErr)
	}
	if client.freeShippingCalls != 1 || client.consignCalls != 0 || len(sender.texts) != 0 {
		t.Fatalf("待刀成阶段发生发卡或确认发货抢跑: free=%d consign=%d messages=%+v", client.freeShippingCalls, client.consignCalls, sender.texts)
	}
}

// TestBargainPendingRequiresWebSocketAndIndependentSwitch 验证二人小刀普通发货不会因待刀成消息或计划任务误触发免拼。
func TestBargainPendingRequiresWebSocketAndIndependentSwitch(t *testing.T) {
	// store、cleanup 保存默认关闭自动免拼的测试数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// client 是若边界失效就会记录免拼调用的 MTOP 替身。
	client := &fakeMTop{freeShippingOK: true, freeShippingRet: []string{"SUCCESS::调用成功"}}
	// center 是使用默认关闭自动免拼账号设置的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: client})
	// wsTask 是关闭自动免拼时收到的真实待刀成 WebSocket 阶段。
	wsTask := Task{Source: "ws", AccountID: "cid", OrderRole: OrderRoleSeller, TriggerType: TriggerBargainPending, OrderID: "bargain-normal", ItemID: "item", BuyerID: "buyer", IsBargain: true}
	if /* handleErr 保存关闭免拼时处理待刀成消息的错误。 */ handleErr := center.HandleTask(context.Background(), wsTask); handleErr != nil {
		t.Fatalf("关闭免拼时处理待刀成消息失败: %v", handleErr)
	}
	// schedulerTask 模拟兜底任务错误构造免拼阶段，中心必须在平台调用前拒绝。
	schedulerTask := wsTask
	schedulerTask.Source = "scheduler"
	if /* handleErr 保存拒绝计划任务免拼阶段时的处理错误。 */ handleErr := center.HandleTask(context.Background(), schedulerTask); handleErr != nil {
		t.Fatalf("拒绝计划任务免拼阶段不应报错: %v", handleErr)
	}
	if client.freeShippingCalls != 0 || client.consignCalls != 0 {
		t.Fatalf("二人小刀普通发货或兜底任务误触发免拼: free=%d consign=%d", client.freeShippingCalls, client.consignCalls)
	}
}

// TestMergeOrderIntoTaskCarriesBargainFlag 验证 WS、调度和延迟恢复共用的订单事实补全不会丢失免拼发货标记。
func TestMergeOrderIntoTaskCarriesBargainFlag(t *testing.T) {
	// order 是订单同步已明确标记为砍价活动的本地订单事实。
	order := seedAutomationOrder("merge-bargain", true)
	// merged 是使用本地订单补全后的自动化任务快照。
	merged := mergeOrderIntoTask(Task{}, order)
	if !merged.IsBargain {
		t.Fatal("订单免拼标记未进入自动化任务")
	}
}

// seedAutomationOrder 构造仅供任务事实合并测试使用的非敏感订单模型；bargain 控制平台同步标记。
func seedAutomationOrder(orderID string, bargain bool) *db.Order {
	// bargainValue 把布尔测试入参转换为数据库订单模型使用的整数标记。
	bargainValue := 0
	if bargain {
		bargainValue = 1
	}
	return &db.Order{OrderID: orderID, IsBargain: bargainValue}
}

// raceFreeShipMTop 在免拼请求进行期间模拟凭证续期并发写入，随后返回平台成功与带变更的响应 Cookie，
// 复现“免拼已成功但旧响应 Cookie 因并发更新被跳过写回”的窗口。
type raceFreeShipMTop struct {
	// fakeMTop 提供其余 MTOP 能力的内存替身。
	*fakeMTop
	// store 用于在免拼调用期间写入并发更新的新凭证。
	store *db.Store
	// latestCookie 是并发写入的新 Cookie，旧响应不得覆盖它。
	latestCookie string
}

// FreeShippingContext 先写入新凭证再返回平台免拼成功，使响应 Cookie 写回时指纹已变化。
func (r *raceFreeShipMTop) FreeShippingContext(_ context.Context, cookieStr, _, _, _ string) (bool, []string, string, error) {
	// err 保存并发凭证写入错误。
	if err := r.store.Cookies.UpdateRenewalCookie(context.Background(), "cid", r.latestCookie, "", 1700000000); err != nil {
		return false, nil, "", err
	}
	return true, []string{"SUCCESS::调用成功"}, cookieStr + " stale=1;", nil
}

// TestBargainPendingCredentialConflictAfterFreeShippingSucceeds 验证平台免拼已成功时，响应 Cookie 因凭证并发更新被跳过写回
// 不再被判为结果未知：阶段记为 succeeded、不进入 needs_review，新凭证不被旧响应覆盖，兜底候选资格保留。
func TestBargainPendingCredentialConflictAfterFreeShippingSucceeds(t *testing.T) {
	// store、cleanup 保存隔离的自动化数据库及资源释放函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 保存测试共用的无取消上下文。
	ctx := context.Background()
	// admin、adminErr 保存账号所属管理员和读取错误。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// enabled 是显式开启的独立自动免拼开关。
	enabled := true
	// settingsErr 保存自动免拼开关写入错误。
	if _, settingsErr := store.Cookies.UpdateSettings(ctx, "cid", db.AccountSettingsUpdate{UserID: admin.ID, AutoBargain: &enabled}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// client 在免拼调用期间并发写入新凭证。
	client := &raceFreeShipMTop{fakeMTop: &fakeMTop{}, store: store, latestCookie: "unb=123; _m_h5_tk=tk_latest;"}
	// center 是注入上述替身的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: client})
	// handleErr 保存待刀成 WS 任务的执行错误。
	handleErr := center.HandleTask(ctx, Task{Source: "ws", AccountID: "cid", OrderRole: OrderRoleSeller, TriggerType: TriggerBargainPending, OrderID: "bargain-cred-race", ItemID: "10001", BuyerID: "20002", ChatID: "chat", IsBargain: true})
	if handleErr != nil {
		t.Fatalf("免拼已成功时凭证并发冲突不应使阶段失败: %v", handleErr)
	}
	// stageStatus 保存免拼阶段表终态。
	var stageStatus string
	// scanErr 保存阶段状态读取错误。
	if scanErr := store.DB.QueryRowContext(ctx, `SELECT status FROM bargain_free_shipping_stages WHERE order_id=? AND cookie_id=?`, "bargain-cred-race", "cid").Scan(&stageStatus); scanErr != nil {
		t.Fatal(scanErr)
	}
	if stageStatus != "succeeded" {
		t.Fatalf("阶段状态 = %q，期望 succeeded（不得进入 needs_review）", stageStatus)
	}
	// detail、detailErr 保存最终凭证，断言旧响应未覆盖并发写入的新凭证。
	detail, detailErr := store.Cookies.GetDetails(ctx, "cid")
	if detailErr != nil {
		t.Fatal(detailErr)
	}
	if detail.Value != client.latestCookie {
		t.Fatalf("旧响应覆盖了并发写入的新凭证: %q", detail.Value)
	}
	// runIssues、taskIssues 保存人工处理面板数据，免拼成功后不应出现待人工项。
	runIssues, taskIssues, issuesErr := store.Automation.ListIssues(ctx, admin.ID)
	if issuesErr != nil || len(runIssues) != 0 || len(taskIssues) != 0 {
		t.Fatalf("不应出现待人工项: runs=%d tasks=%d err=%v", len(runIssues), len(taskIssues), issuesErr)
	}
}
