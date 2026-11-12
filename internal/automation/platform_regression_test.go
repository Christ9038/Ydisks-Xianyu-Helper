package automation

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/mtop/mtopsim"
)

// 本文件收录由平台模拟器发现并已修复的缺陷的回归用例。每个用例的注释先描述缺陷发生时的错误行为，
// 再给出必须保持的正确行为；用例全部经真实 MTOP 客户端驱动，断言失败说明对应缺陷复发。

// TestPlatformRegressionNumericOrderIDLosesPrecision 回归：交易卡片内嵌 JSON 以数字形式携带 19 位订单号时，
// 事件解析经 float64 中转，订单号末几位被舍入成另一个合法的纯数字订单号并被采用。
// 正确行为：原样保留订单号，或在无法保证精度时不采用该值。
func TestPlatformRegressionNumericOrderIDLosesPrecision(t *testing.T) {
	// orderID 是 19 位订单号，超过 float64 可精确表示的 2^53。
	const orderID = "4567890123456789012"
	// raw 是付款系统消息，订单号只出现在卡片内嵌 JSON 的数字字段中。
	raw := map[string]any{"1": map[string]any{
		"2": "62904549781@goofish", "7": "1",
		"10": map[string]any{"reminderContent": "[我已付款，等待你发货]", "extJson": `{"contentType":"6"}`},
		"6":  map[string]any{"3": map[string]any{"5": `{"dxCard":{"item":{"main":{"args":{"bizOrderId":` + orderID + `}}}}}`}},
	}}
	// task 是从该消息识别出的付款任务。
	task := ExtractTaskFromWS(platformAccountID, "", raw)
	if task == nil {
		t.Fatal("付款消息应被识别")
	}
	if task.OrderID != orderID && task.OrderID != "" {
		t.Fatalf("订单号被舍入为另一个订单号：期望 %s（或放弃采用），实际 %s", orderID, task.OrderID)
	}
}

// TestPlatformRegressionAlreadyDeliveredIsNotIdempotent 回归：订单在平台上已发货时，平台对确认发货返回不带 FAIL_BIZ 前缀的
// “ORDER_ALREADY_DELIVERY::已发货成功，请刷新页面~”。自动化中心本应把它当作幂等成功（consignAlreadyDelivered），
// 但真实客户端先把这个未知前缀的返回码归类为系统错误并返回 error，执行器因此走“结果未知”分支转人工核对，幂等分支永远到不了。
// 典型场景：确认发货已被平台执行但响应丢失，随后人工“完整发货”或待发货兜底补做确认发货。
// 正确行为：补做的确认发货成功返回，本地订单标记为已发货。
func TestPlatformRegressionAlreadyDeliveredIsNotIdempotent(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// itemID 是单规格商品；orderID 是买家已付款的订单。
	itemID := flow.addItem("视频会员月卡", false)
	// orderID 是买家已付款、等待发货的平台订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})
	// task 是补做确认发货的任务。
	task := Task{AccountID: platformAccountID, OrderID: orderID, ItemID: itemID, BuyerID: platformBuyerID, ForceConfirmShipment: true}
	// 第一次确认发货：平台已执行，但响应在网络上丢失。
	flow.gateway.InjectFault(mtopsim.APIConsign, mtopsim.Fault{Err: errors.New("connection reset by peer"), AfterEffect: true})
	if lostErr := flow.center.confirmShipment(flow.ctx, task); lostErr == nil { // lostErr 是响应丢失时的结果，必须是错误。
		t.Fatal("响应丢失时确认发货应返回结果未知错误")
	}
	if flow.gateway.Order(orderID).Status != mtopsim.OrderShipped {
		t.Fatal("预置条件：平台订单应已发货")
	}

	// retryErr 是补做确认发货的结果：平台回答“已发货”，应视为成功。
	retryErr := flow.center.confirmShipment(flow.ctx, task)
	if retryErr != nil {
		t.Fatalf("平台已发货应按幂等成功处理，实际返回: %v", retryErr)
	}
	if localOrder := flow.localOrder(orderID); localOrder.OrderStatus != "shipped" || !localOrder.SystemShipped { // localOrder 是本地订单事实。
		t.Fatalf("本地订单应同步为已发货: %+v", localOrder)
	}
}

// TestPlatformRegressionNumericStatusHidesCompletedOrderFromAutoRate 回归：订单详情接口返回数字状态码（4 表示交易成功），
// 自动化准备阶段把它原样写入订单表，覆盖确认收货消息写入的规范值 completed；
// 而自动评价只查询 order_status='completed'，该订单从此不再被评价。
// 正确行为：写入前归一化状态，或查询时兼容数字码，订单仍在待评价列表中。
func TestPlatformRegressionNumericStatusHidesCompletedOrderFromAutoRate(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// itemID 是单规格商品；orderID 是已发货并确认收货的订单。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 STATUS-1")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// orderID 是买家已付款、等待发货的平台订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})
	flow.mustDeliver(flow.gateway.PaidEvent(orderID), TriggerOrderPaid)
	flow.gateway.ConfirmReceipt(orderID)
	flow.mustDeliver(flow.gateway.CompletedEvent(orderID), TriggerOrderCompleted)
	// 平台在交易完成后重复推送了一次付款消息；处理它会重新查询订单详情，平台返回状态码 4。
	// handleErr 是重复付款消息的处理错误。
	if _, handleErr := flow.deliver(flow.gateway.PaidEvent(orderID)); handleErr != nil {
		t.Fatalf("重复付款消息处理失败: %v", handleErr)
	}

	if status := flow.localOrder(orderID).OrderStatus; status != "completed" { // status 是本地保存的订单状态。
		t.Errorf("本地订单状态应保持规范值 completed，实际被覆盖为 %q", status)
	}
	// due、dueErr 是自动评价的待评价订单列表。
	due, dueErr := flow.store.AccountTasks.DueAutoRateOrderIDs(flow.ctx, platformAccountID, 200)
	if dueErr != nil || len(due) != 1 || due[0] != orderID {
		t.Fatalf("已确认收货的订单应在待评价列表中，实际 %v err=%v", due, dueErr)
	}
}

// TestPlatformRegressionDuplicatePaidEventRefetchesOrderDetail 回归：无规格订单的“付款事实齐全”判断要求规格名和规格值非空，
// 因此永远不齐全；而订单详情补齐发生在“订单已发货/运行已存在”的判断之前，
// 导致平台每重复推送一次付款消息，就多请求一次订单详情接口。
// 正确行为：已发货订单的重复付款消息不产生任何平台请求。
func TestPlatformRegressionDuplicatePaidEventRefetchesOrderDetail(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// itemID 是单规格商品；orderID 是买家已付款的订单。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 DUP-1")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// orderID 是买家已付款、等待发货的平台订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})
	flow.mustDeliver(flow.gateway.PaidEvent(orderID), TriggerOrderPaid)
	flow.gateway.ResetCalls()

	// replay 是重复推送的序号。
	for replay := 0; replay < 3; replay++ {
		// handleErr 是重复消息的处理错误。
		if _, handleErr := flow.deliver(flow.gateway.PaidEvent(orderID)); handleErr != nil {
			t.Fatalf("重复付款消息处理失败: %v", handleErr)
		}
	}
	if calls := flow.gateway.CallCount(); calls != 0 { // calls 是重复消息引发的平台请求数。
		t.Fatalf("已发货订单的 3 条重复付款消息引发了 %d 次平台请求：%v", calls, flow.gateway.CallSequence())
	}
}

// TestPlatformRegressionAutoRateKeepsProbingUnderRiskControl 回归：自动评价批次遇风控后会终止，但失败的订单被设置了 10 分钟后重试，
// 下一轮调度扫描（默认每分钟一次）会跳过它并对下一笔待评价订单发起请求，再次撞上风控。
// 账号处于风控期间，每轮扫描都会向平台发一次评价请求，直到待评价订单耗尽，之后还会按重试计划继续重放。
// 正确行为：账号遇风控后，在人工处理之前后续扫描不再向平台发评价请求。
func TestPlatformRegressionAutoRateKeepsProbingUnderRiskControl(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// itemID 是单规格商品。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 RATE-RISK")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// settingsErr 是自动评价配置写入错误。
	if settingsErr := flow.store.AccountTasks.Upsert(flow.ctx, db.AccountTaskSettings{CookieID: platformAccountID, AutoRateEnabled: true, RateContent: "感谢支持"}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// index 是待评价订单的序号，共 4 笔已确认收货的订单。
	for index := 0; index < 4; index++ {
		// orderID 是已付款、已发货并确认收货的订单。
		orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})
		flow.mustDeliver(flow.gateway.PaidEvent(orderID), TriggerOrderPaid)
		flow.gateway.ConfirmReceipt(orderID)
		flow.mustDeliver(flow.gateway.CompletedEvent(orderID), TriggerOrderCompleted)
	}
	flow.gateway.SetRiskControl(platformSellerID, true)
	flow.gateway.ResetCalls()

	// scan 是连续的调度扫描轮次；生产环境中相邻两轮间隔一分钟。
	for scan := 0; scan < 4; scan++ {
		flow.center.scanAccountTasks(flow.ctx)
	}
	if calls := flow.gateway.CallCount(mtopsim.APIRateCreate); calls > 1 { // calls 是风控期间发出的评价请求数。
		t.Fatalf("风控期间 4 轮扫描共发出 %d 次评价请求，遇风控后不应继续请求", calls)
	}
}

// TestPlatformRegressionPolishRetriesImmediatelyAfterRiskControl 回归：每日擦亮批次中途遇风控时，运行被记为失败且重试时间为 0。
// 领取条件是“重试时间 <= 当前时间”，因此 0 表示立即可重试：下一轮调度扫描会重新拉取商品列表并从头擦亮，
// 直到用完 6 次尝试。风控期间这等于每分钟重放一次整个批次。
// 正确行为：遇风控终止的擦亮批次不得被后续扫描自动重放。
func TestPlatformRegressionPolishRetriesImmediatelyAfterRiskControl(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// index 是批量添加商品的序号，共 4 件。
	for index := 0; index < 4; index++ {
		flow.addItem("商品"+string(rune('A'+index)), false)
	}
	// settingsErr 是每日擦亮配置写入错误；擦亮时间设为 00:00，保证任意时刻都已到点。
	if settingsErr := flow.store.AccountTasks.Upsert(flow.ctx, db.AccountTaskSettings{CookieID: platformAccountID, AutoPolishEnabled: true, PolishTime: "00:00"}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// 第一轮扫描：第 2 件商品擦亮时遇风控，批次终止。
	flow.gateway.InjectFault(mtopsim.APIPolish, mtopsim.Fault{Ret: []string{mtopsim.RetSuccess}}, mtopsim.Fault{Ret: []string{mtopsim.RetRiskControl}})
	flow.center.scanAccountTasks(flow.ctx)
	if polish := flow.gateway.CallCount(mtopsim.APIPolish); polish != 2 { // polish 是第一轮发出的擦亮请求数。
		t.Fatalf("第一轮应在第 2 次擦亮遇风控后终止，实际请求 %d 次", polish)
	}
	flow.gateway.SetRiskControl(platformSellerID, true)
	flow.gateway.ResetCalls()

	// 第二轮扫描紧随其后：账号仍处于风控。
	flow.center.scanAccountTasks(flow.ctx)
	if calls := flow.gateway.CallCount(); calls != 0 { // calls 是第二轮扫描发出的平台请求数。
		t.Fatalf("遇风控终止的擦亮批次在下一轮扫描被立即重放，发出 %d 次平台请求：%v", calls, flow.gateway.CallSequence())
	}
}

// TestPlatformRegressionClientClassifiesAlreadyDeliveredAsSystemError 在协议层单独回归“已发货”返回码的分类问题：
// 平台已明确告知订单已发货，客户端却返回系统错误，调用方无法与“结果未知”区分。
// 正确行为：与其他明确业务结果一样返回 ok=false、err=nil，并带上平台返回码。
func TestPlatformRegressionClientClassifiesAlreadyDeliveredAsSystemError(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// itemID 是单规格商品；orderID 是买家已付款的订单。
	itemID := flow.addItem("视频会员月卡", false)
	// orderID 是买家已付款、等待发货的平台订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})
	// client、cookie 是连接模拟器的真实客户端和卖家当前 Cookie。
	client, cookie := flow.gateway.Client(), flow.gateway.Cookie(platformSellerID)
	// ok、firstErr 是第一次确认发货的结果。
	if ok, _, _, firstErr := client.ConsignContext(flow.ctx, cookie, orderID); firstErr != nil || !ok {
		t.Fatalf("第一次确认发货应成功: ok=%v err=%v", ok, firstErr)
	}
	// ok、ret、secondErr 是对已发货订单再次确认发货的结果。
	ok, ret, _, secondErr := client.ConsignContext(flow.ctx, cookie, orderID)
	if secondErr != nil {
		// kind 是客户端对该返回码的分类。
		kind, _ := mtop.MTopErrorKindOf(secondErr)
		t.Fatalf("已发货应作为明确业务结果返回，实际 err=%v（分类 %s）", secondErr, kind)
	}
	if ok || len(ret) != 1 || ret[0] != mtopsim.RetAlreadyDelivered {
		t.Fatalf("应返回 ok=false 和平台返回码: ok=%v ret=%v", ok, ret)
	}
}

// TestPlatformRegressionDuplicatePaidEventWithoutAutoConsign 回归：关闭“自动确认发货”时订单发卡后仍停在待发货，
// 重复付款消息不会被“订单已发货”门禁拦截；必须由“同一规则同一事件已成功”的本地运行记录在补全订单事实之前拦截。
// 正确行为：重复付款消息不产生任何平台请求，也不重复发卡。
func TestPlatformRegressionDuplicatePaidEventWithoutAutoConsign(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// autoConsign 是关闭后的“自动确认发货”开关值。
	autoConsign := false
	flow.setSettings(db.AccountSettingsUpdate{AutoConsign: &autoConsign})
	// itemID 是单规格商品。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 DUP-2")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// orderID 是买家已付款、等待发货的平台订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})
	flow.mustDeliver(flow.gateway.PaidEvent(orderID), TriggerOrderPaid)
	if status := flow.localOrder(orderID).OrderStatus; !db.IsPendingShipStatus(status) { // status 是发卡后本地订单状态。
		t.Fatalf("预置条件：关闭自动确认发货后订单应仍为待发货，实际 %q", status)
	}
	// sentBefore 是首次发货后买家已收到的消息数。
	sentBefore := len(flow.sender.texts())
	flow.gateway.ResetCalls()

	// replay 是重复推送的序号。
	for replay := 0; replay < 3; replay++ {
		// handleErr 是重复消息的处理错误。
		if _, handleErr := flow.deliver(flow.gateway.PaidEvent(orderID)); handleErr != nil {
			t.Fatalf("重复付款消息处理失败: %v", handleErr)
		}
	}
	if calls := flow.gateway.CallCount(); calls != 0 { // calls 是重复消息引发的平台请求数。
		t.Fatalf("已发卡订单的重复付款消息引发了 %d 次平台请求：%v", calls, flow.gateway.CallSequence())
	}
	if sentAfter := len(flow.sender.texts()); sentAfter != sentBefore { // sentAfter 是重复消息处理后买家收到的消息数。
		t.Fatalf("重复付款消息不得重复发卡：之前 %d 条，之后 %d 条", sentBefore, sentAfter)
	}
}

// TestPlatformRegressionPaidEventForLocallyShippedOrderSkipsDetail 回归：订单在本地已是已发货（例如人工发货，没有任何自动化运行记录），
// 迟到的付款消息必然被付款门禁跳过，却仍会先请求一次平台订单详情。
// 正确行为：本地订单已越过待发货阶段时，新到的付款消息不请求平台。
func TestPlatformRegressionPaidEventForLocallyShippedOrderSkipsDetail(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// itemID 是单规格商品。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 LATE-1")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// orderID 是买家已付款的平台订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})
	// upsertErr 是把本地订单预置为“已发货”的写入错误，模拟卖家已在别处完成发货。
	if upsertErr := flow.store.Orders.Upsert(flow.ctx, orderID, db.OrderUpsertOpts{CookieID: platformAccountID, ItemID: itemID, BuyerID: platformBuyerID, OrderStatus: "shipped"}); upsertErr != nil {
		t.Fatal(upsertErr)
	}
	flow.gateway.ResetCalls()
	// handleErr 是迟到付款消息的处理错误。
	if _, handleErr := flow.deliver(flow.gateway.PaidEvent(orderID)); handleErr != nil {
		t.Fatalf("迟到付款消息处理失败: %v", handleErr)
	}
	if calls := flow.gateway.CallCount(); calls != 0 { // calls 是迟到消息引发的平台请求数。
		t.Fatalf("本地已发货订单的付款消息引发了 %d 次平台请求：%v", calls, flow.gateway.CallSequence())
	}
	if sent := len(flow.sender.texts()); sent != 0 { // sent 是买家收到的消息数，已发货订单不得再发卡。
		t.Fatalf("本地已发货订单不得再发卡，实际发送 %d 条", sent)
	}
}

// TestPlatformRegressionRiskBlockLifecycle 验证账号级风控阻断的解除条件：凭证未变化或未超过最短保持时长时保持阻断；
// 两者同时满足才自动解除；用户手动执行任务视为确认恢复，立即解除，若平台仍要求验证则重新阻断。
func TestPlatformRegressionRiskBlockLifecycle(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// coordinator 指向自动化中心内的账号任务协调器；必须取地址，协调器持有的阻断表不可复制。
	coordinator := &flow.center.taskRunner
	flow.gateway.SetRiskControl(platformSellerID, true)
	// riskErr 是平台风控验证错误，由真实客户端对模拟器的风控响应分类得到。
	_, riskErr := flow.gateway.Client().FetchAllItems(flow.ctx, flow.gateway.Cookie(platformSellerID), 20, 1)
	if !mtop.IsRiskVerificationErr(riskErr) {
		t.Fatalf("预置条件：应得到风控错误，实际 %v", riskErr)
	}
	// blocked 读取账号当前是否被风控阻断，读取失败时终止用例。
	blocked := func() bool {
		t.Helper()
		// isBlocked、blockErr 是阻断状态及读取凭证指纹的错误。
		isBlocked, blockErr := coordinator.accountTaskRiskBlocked(flow.ctx, platformAccountID)
		if blockErr != nil {
			t.Fatal(blockErr)
		}
		return isBlocked
	}
	// rewind 把阻断时间拨回 elapsed 之前，用于在不等待真实时间的情况下验证最短保持时长。
	rewind := func(elapsed time.Duration) {
		t.Helper()
		// stored、found 是当前登记的阻断事实。
		stored, found := coordinator.riskBlocked.Load(platformAccountID)
		if !found {
			t.Fatal("账号应已登记风控阻断")
		}
		// block 是待改写阻断时间的事实副本。
		block := stored.(accountTaskRiskBlock)
		block.blockedAt = time.Now().Add(-elapsed)
		coordinator.riskBlocked.Store(platformAccountID, block)
	}

	coordinator.noteAccountTaskRisk(flow.ctx, platformAccountID, errors.New("普通业务失败"))
	if blocked() {
		t.Fatal("非风控错误不得登记阻断")
	}
	coordinator.noteAccountTaskRisk(flow.ctx, platformAccountID, riskErr)
	if !blocked() {
		t.Fatal("风控错误应登记阻断")
	}
	rewind(2 * accountTaskRiskMinHold)
	if !blocked() {
		t.Fatal("凭证未变化时，超过最短保持时长也不得自动解除")
	}
	// renewed 是用户通过验证后重新登录得到的新 Cookie。
	renewed := flow.gateway.Relogin(platformSellerID)
	// saveErr 是保存新 Cookie 的错误。
	if saveErr := flow.store.Cookies.Save(flow.ctx, platformAccountID, renewed, flow.adminID); saveErr != nil {
		t.Fatal(saveErr)
	}
	rewind(accountTaskRiskMinHold / 2)
	if !blocked() {
		t.Fatal("凭证已变化但未超过最短保持时长时不得解除")
	}
	rewind(2 * accountTaskRiskMinHold)
	if blocked() {
		t.Fatal("凭证已变化且超过最短保持时长后应解除阻断")
	}

	// 手动执行：平台仍处于风控时重新登记阻断；风控解除后手动执行清除阻断。
	// settingsErr 是每日擦亮配置写入错误。
	if settingsErr := flow.store.AccountTasks.Upsert(flow.ctx, db.AccountTaskSettings{CookieID: platformAccountID, AutoPolishEnabled: true, PolishTime: "00:00"}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	flow.addItem("商品甲", false)
	// manualErr 是风控期间手动擦亮的结果，必须把风控上报给用户。
	if _, manualErr := flow.center.RunAccountTask(flow.ctx, platformAccountID, TaskAutoPolish); !mtop.IsRiskVerificationErr(manualErr) {
		t.Fatalf("风控期间手动执行应上报风控，实际 %v", manualErr)
	}
	if !blocked() {
		t.Fatal("手动执行再次命中风控后应重新阻断")
	}
	flow.gateway.SetRiskControl(platformSellerID, false)
	flow.gateway.ResetCalls()
	flow.center.scanAccountTasks(flow.ctx)
	if calls := flow.gateway.CallCount(); calls != 0 { // calls 是阻断期间调度扫描发出的平台请求数。
		t.Fatalf("阻断期间调度扫描不得请求平台，实际 %d 次", calls)
	}
	// summary、manualErr 是风控解除后手动擦亮的结果。
	summary, manualErr := flow.center.RunAccountTask(flow.ctx, platformAccountID, TaskAutoPolish)
	if manualErr != nil || summary.Success != 1 {
		t.Fatalf("风控解除后手动擦亮应成功: %+v err=%v", summary, manualErr)
	}
	if blocked() {
		t.Fatal("手动执行成功后应解除阻断")
	}
}

// TestPlatformRegressionPolishAbortSchedulesRetryLater 回归：擦亮批次中途因风控中止时，运行记录的重试时间曾被写成 0（立即可重领）。
// 正确行为：中止的运行记录带有晚于当前时间的重试时间，即使进程重启清空了内存阻断也不会被下一轮扫描立即重放。
func TestPlatformRegressionPolishAbortSchedulesRetryLater(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	flow.addItem("商品甲", false)
	flow.addItem("商品乙", false)
	flow.gateway.InjectFault(mtopsim.APIPolish, mtopsim.Fault{Ret: []string{mtopsim.RetRiskControl}})
	// taskErr 是擦亮任务的执行结果。
	if _, taskErr := flow.center.RunAccountTask(flow.ctx, platformAccountID, TaskAutoPolish); !mtop.IsRiskVerificationErr(taskErr) {
		t.Fatalf("应上报风控: %v", taskErr)
	}
	// runs、runsErr 是该账号最近的任务运行记录。
	runs, runsErr := flow.store.AccountTasks.RecentRuns(flow.ctx, platformAccountID, 5)
	if runsErr != nil || len(runs) != 1 {
		t.Fatalf("应只有一条擦亮运行记录: %v err=%v", runs, runsErr)
	}
	if run := runs[0]; run.Status != "failed" || run.NextRetryAt <= time.Now().UTC().Unix() { // run 是被风控中止的擦亮运行。
		t.Fatalf("中止的擦亮运行应为 failed 且重试时间晚于当前时间: %+v", run)
	}
}

// TestPlatformRegressionLegacyNumericCompletedStatusIsRateable 回归：历史版本已把订单详情的数字状态码 4 写进订单表，
// 这些存量订单同样必须进入待评价列表，而不是只靠新写入路径的归一化。
func TestPlatformRegressionLegacyNumericCompletedStatusIsRateable(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// itemID 是单规格商品。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 LEGACY-1")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// orderID 是已发货并确认收货的订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})
	flow.mustDeliver(flow.gateway.PaidEvent(orderID), TriggerOrderPaid)
	flow.gateway.ConfirmReceipt(orderID)
	flow.mustDeliver(flow.gateway.CompletedEvent(orderID), TriggerOrderCompleted)
	flow.exec(`UPDATE orders SET order_status='4' WHERE order_id=?`, orderID)

	// due、dueErr 是自动评价的待评价订单列表。
	due, dueErr := flow.store.AccountTasks.DueAutoRateOrderIDs(flow.ctx, platformAccountID, 200)
	if dueErr != nil || len(due) != 1 || due[0] != orderID {
		t.Fatalf("存量数字状态码的已完成订单应在待评价列表中，实际 %v err=%v", due, dueErr)
	}
}

// TestPlatformRegressionRiskBlockWithoutReadableCredential 验证凭证指纹不可读时的保守行为：
// 登记阻断时读不到指纹则只能由手动执行解除；判断阻断时读不到指纹则保持阻断并把错误交给调用方。
func TestPlatformRegressionRiskBlockWithoutReadableCredential(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// coordinator 指向自动化中心内的账号任务协调器。
	coordinator := &flow.center.taskRunner
	flow.gateway.SetRiskControl(platformSellerID, true)
	// riskErr 是平台风控验证错误。
	_, riskErr := flow.gateway.Client().FetchAllItems(flow.ctx, flow.gateway.Cookie(platformSellerID), 20, 1)
	coordinator.noteAccountTaskRisk(flow.ctx, platformAccountID, riskErr)
	// stored 是登记的阻断事实；把阻断时间拨回到最短保持时长之前，使判断必须读取当前凭证指纹。
	stored, _ := coordinator.riskBlocked.Load(platformAccountID)
	// block 是待改写阻断时间的事实副本。
	block := stored.(accountTaskRiskBlock)
	block.blockedAt = time.Now().Add(-2 * accountTaskRiskMinHold)
	coordinator.riskBlocked.Store(platformAccountID, block)
	// closeErr 是关闭底层数据库的错误；关闭后凭证指纹无法读取。
	if closeErr := flow.store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// stillBlocked、readErr 是凭证不可读时的阻断判断结果。
	stillBlocked, readErr := coordinator.accountTaskRiskBlocked(flow.ctx, platformAccountID)
	if !stillBlocked || readErr == nil {
		t.Fatalf("凭证不可读时应保持阻断并返回错误: blocked=%v err=%v", stillBlocked, readErr)
	}

	coordinator.riskBlocked.Delete(platformAccountID)
	coordinator.noteAccountTaskRisk(flow.ctx, platformAccountID, riskErr)
	// unreadable 是凭证不可读时登记的阻断事实，指纹必须为空。
	unreadable, found := coordinator.riskBlocked.Load(platformAccountID)
	if !found || unreadable.(accountTaskRiskBlock).fingerprint != "" {
		t.Fatalf("凭证不可读时应登记空指纹阻断: %+v found=%v", unreadable, found)
	}
	// emptyBlock 是空指纹阻断的事实副本，拨回阻断时间验证它不随时间解除。
	emptyBlock := unreadable.(accountTaskRiskBlock)
	emptyBlock.blockedAt = time.Now().Add(-2 * accountTaskRiskMinHold)
	coordinator.riskBlocked.Store(platformAccountID, emptyBlock)
	if heldBlocked, heldErr := coordinator.accountTaskRiskBlocked(flow.ctx, platformAccountID); !heldBlocked || heldErr != nil { // heldBlocked、heldErr 是空指纹阻断的判断结果。
		t.Fatalf("空指纹阻断只能由手动执行解除: blocked=%v err=%v", heldBlocked, heldErr)
	}
}

// TestEventJSONNumbersKeepOrderIDPrecision 验证事件解析对数字订单号的处理：内嵌 JSON 的数字保留十进制原文；
// 上游已解码成 float64 且超过 2^53 的数值已丢失精度，不得被采用；非单个完整 JSON 值的文本视为解析失败。
func TestEventJSONNumbersKeepOrderIDPrecision(t *testing.T) {
	// orderID 是 19 位订单号，超过 float64 可精确表示的范围。
	const orderID = "4567890123456789012"
	// decoded、ok 是内嵌 JSON 的解码结果。
	decoded, ok := decodeEventJSONPreservingNumbers(`{"bizOrderId":` + orderID + `}`)
	if !ok || strAny(decoded.(map[string]any)["bizOrderId"]) != orderID {
		t.Fatalf("数字订单号应原样保留: %v ok=%v", decoded, ok)
	}
	// text 是不构成单个完整 JSON 值的输入。
	for _, text := range []string{`{"a":1} trailing`, `{"a":`, `{"a":1}{"b":2}`} {
		if _, valid := decodeEventJSONPreservingNumbers(text); valid { // valid 表示该文本是否被接受。
			t.Fatalf("%q 不是单个完整 JSON 值，应解析失败", text)
		}
	}

	// rounded 是上游把订单号解码为 float64 后的值，末几位已被舍入。
	rounded := float64(4567890123456789012)
	// fields 接收补齐的交易事实。
	fields := &rawFields{}
	supplementEventFactByKey(fields, "bizOrderId", rounded)
	if fields.orderID != "" {
		t.Fatalf("已丢失精度的浮点订单号不得被采用，实际 %q", fields.orderID)
	}
	supplementEventFactByKey(fields, "bizOrderId", float64(1234567890123))
	if fields.orderID != "1234567890123" {
		t.Fatalf("可精确表示的数字订单号应被采用，实际 %q", fields.orderID)
	}
	if !impreciseEventNumber(-rounded) || impreciseEventNumber(orderID) || impreciseEventNumber(json.Number(orderID)) {
		t.Fatal("只有超出精确范围的 float64 才算丢失精度")
	}
}

// TestOrderRoleFromTaskNamePrefersExplicitRecipientMarker 验证任务名的接收方判断：
// “_卖家”“_买家”明确标注优先于正文字样，两种标注并存时以靠后的为准；没有标注时沿用按字样判断的兼容行为。
func TestOrderRoleFromTaskNamePrefersExplicitRecipientMarker(t *testing.T) {
	// cases 把平台任务名映射到期望的接收方角色。
	cases := map[string]string{
		"付款完成待发货_卖家-正向升级": "seller",
		"已付款_买家":          "buyer",
		"买家已付款_卖家":        "seller",
		"卖家已发货_买家":        "buyer",
		"通知_买家_卖家":        "seller",
		"通知_卖家_买家":        "buyer",
		"买家付款提醒":          "buyer",
		"卖家发货提醒":          "seller",
		"交易提醒":            "",
	}
	// taskName、want 是当前任务名及其期望角色。
	for taskName, want := range cases {
		if got := orderRoleFromTaskName(taskName); got != want { // got 是实际判定的角色。
			t.Errorf("orderRoleFromTaskName(%q)=%q，期望 %q", taskName, got, want)
		}
	}
}

// TestPlatformRegressionPaidEventSettledDecision 验证“付款事件已收口”的判定边界：只有同一账号同一订单、
// 且运行处于不可重领终态（success、needs_review）时才跳过；仍可能被重领的状态、他人账号的同号事件、
// 续跑任务和非付款事件都必须交回原有流程。
func TestPlatformRegressionPaidEventSettledDecision(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// itemID 是单规格商品。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 SETTLED-1")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// orderID 是已完成自动发货的订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})
	flow.mustDeliver(flow.gateway.PaidEvent(orderID), TriggerOrderPaid)
	// run、runErr 是该订单的付款发货运行，用于取得规则主键。
	run, runErr := flow.store.Automation.GetLatestOrderDeliveryRun(flow.ctx, platformAccountID, orderID)
	if runErr != nil {
		t.Fatal(runErr)
	}
	// rule 是只带主键的规则，判定只依赖规则主键。
	rule := db.AutomationRule{ID: run.RuleID}
	// task 是同一账号同一订单的新付款事件。
	task := Task{AccountID: platformAccountID, OrderID: orderID, TriggerType: TriggerOrderPaid}
	// settled 返回 candidate 是否被判定为已收口。
	settled := func(candidate Task) bool {
		return flow.center.runs.paidEventAlreadySettled(flow.ctx, candidate, rule)
	}
	if !settled(task) {
		t.Fatal("已成功的付款运行应判定为已收口")
	}
	// foreign 是其他账号携带同一订单号的事件，不得借用本账号的运行结论。
	foreign := task
	foreign.AccountID = "other-account"
	// resumed 是携带运行 ID 的续跑任务。
	resumed := task
	resumed.Raw = map[string]any{"automation_run_id": run.ID}
	// completed 是同一订单的非付款事件。
	completed := task
	completed.TriggerType = TriggerOrderCompleted
	// unknownOrder 是没有任何运行记录的订单。
	unknownOrder := task
	unknownOrder.OrderID = "4567890123499999999"
	// noOrder 是缺少订单号的事件。
	noOrder := task
	noOrder.OrderID = ""
	// name、candidate 是不得被判定为已收口的事件及其说明。
	for name, candidate := range map[string]Task{"他人账号": foreign, "续跑任务": resumed, "非付款事件": completed, "无运行记录": unknownOrder, "缺少订单号": noOrder} {
		if settled(candidate) {
			t.Errorf("%s不应判定为已收口", name)
		}
	}
	// status、want 是运行状态及其期望判定：仍可能被重领的状态必须返回 false。
	for status, want := range map[string]bool{"needs_review": true, "failed": false, "running": false, "canceled": false} {
		flow.exec(`UPDATE automation_runs SET status=? WHERE id=?`, status, run.ID)
		if got := settled(task); got != want { // got 是该状态下的实际判定。
			t.Errorf("运行状态 %s 的判定为 %v，期望 %v", status, got, want)
		}
	}
}

// TestPlatformRegressionRiskInRateSkipsPolishInSameScan 验证同一轮扫描内自动评价命中风控后，
// 不再为该账号拉取商品列表或擦亮：一轮扫描在风控下最多只有那一次评价请求。
func TestPlatformRegressionRiskInRateSkipsPolishInSameScan(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// itemID 是单规格商品。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 SCAN-1")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// settingsErr 是同时开启自动评价与每日擦亮的配置写入错误。
	if settingsErr := flow.store.AccountTasks.Upsert(flow.ctx, db.AccountTaskSettings{CookieID: platformAccountID, AutoRateEnabled: true, RateContent: "感谢支持", AutoPolishEnabled: true, PolishTime: "00:00"}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// orderID 是已确认收货、等待评价的订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})
	flow.mustDeliver(flow.gateway.PaidEvent(orderID), TriggerOrderPaid)
	flow.gateway.ConfirmReceipt(orderID)
	flow.mustDeliver(flow.gateway.CompletedEvent(orderID), TriggerOrderCompleted)
	flow.gateway.SetRiskControl(platformSellerID, true)
	flow.gateway.ResetCalls()

	flow.center.scanAccountTasks(flow.ctx)
	if sequence := flow.gateway.CallSequence(); len(sequence) != 1 || sequence[0] != mtopsim.APIRateCreate { // sequence 是本轮扫描发出的全部平台请求。
		t.Fatalf("风控下一轮扫描只应有一次评价请求，实际 %v", sequence)
	}
}

// riskFingerprintFailingRepository 在真实账号任务仓储之上让凭证指纹读取失败，用于验证风控状态无法判断时的扫描行为。
type riskFingerprintFailingRepository struct {
	// AccountTaskRepository 是被包装的真实仓储，其余方法原样透传。
	AccountTaskRepository
}

// GetCookieRuntimeData 始终返回读取失败，模拟凭证存储暂时不可用。
func (riskFingerprintFailingRepository) GetCookieRuntimeData(context.Context, string) (db.CookieRuntimeData, error) {
	return db.CookieRuntimeData{}, errors.New("凭证存储暂时不可用")
}

// TestPlatformRegressionScanSkipsAccountWhenRiskStateUnreadable 验证扫描时无法判断风控阻断是否可以解除（凭证指纹读取失败）时，
// 该账号本轮被跳过且阻断保留，不向平台发出任何请求。
func TestPlatformRegressionScanSkipsAccountWhenRiskStateUnreadable(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	flow.addItem("商品甲", false)
	// settingsErr 是每日擦亮配置写入错误。
	if settingsErr := flow.store.AccountTasks.Upsert(flow.ctx, db.AccountTaskSettings{CookieID: platformAccountID, AutoPolishEnabled: true, PolishTime: "00:00"}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// client 是连接模拟器的真实客户端。
	client := flow.gateway.Client()
	// coordinator 使用凭证指纹读取必然失败的仓储，其余依赖与生产装配一致。
	coordinator := &accountTaskCoordinator{
		repository: riskFingerprintFailingRepository{AccountTaskRepository: newStoreAccountTaskRepository(flow.store)},
		client:     func() AccountTaskClient { return client },
		recoverer:  func() CredentialRecoverer { return nil },
		logger:     slog.Default(),
	}
	coordinator.riskBlocked.Store(platformAccountID, accountTaskRiskBlock{fingerprint: "旧凭证指纹", blockedAt: time.Now().Add(-2 * accountTaskRiskMinHold)})
	flow.gateway.ResetCalls()

	coordinator.scanAccountTasks(flow.ctx)
	if calls := flow.gateway.CallCount(); calls != 0 { // calls 是本轮扫描发出的平台请求数。
		t.Fatalf("风控状态无法判断时不得请求平台，实际 %d 次：%v", calls, flow.gateway.CallSequence())
	}
	if _, kept := coordinator.riskBlocked.Load(platformAccountID); !kept { // kept 表示阻断是否仍然保留。
		t.Fatal("风控状态无法判断时必须保留阻断")
	}
}
