package engine

import (
	"context"
	"strings"
	"testing"

	"xianyu-go/internal/agentclient"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/mtop/mtopsim"
)

// 本文件是订单阶段功能基于平台模拟器的端到端测试：
// 平台模拟器推进订单并推送 WebSocket 系统消息 → 自动化中心用真实 MTOP 客户端发卡、免拼、确认发货并落库订单事实 →
// AI 回复器与外部 Agent 回复器读取本地订单，断言每个交易阶段提供给模型和 Agent 的订单阶段，
// 同时断言生成回复本身不会向平台发出任何 MTOP 请求。

// 订单阶段端到端测试使用的平台账号标识。
const (
	// orderFlowSellerID 是平台侧卖家账号标识，对应本地账号 cid。
	orderFlowSellerID = "2200000000001"
	// orderFlowBuyerID 是平台侧买家账号标识。
	orderFlowBuyerID = "3300000000002"
)

// orderFlowSender 是自动化中心使用的在线发送器替身：只记录发给买家的消息正文。
type orderFlowSender struct {
	// texts 按发送顺序保存发给买家的消息正文或图片地址。
	texts []string
}

// SendText 记录一条文本消息。
func (s *orderFlowSender) SendText(_ context.Context, _, _, text string) error {
	s.texts = append(s.texts, text)
	return nil
}

// SendImage 记录一条图片消息的地址；本测试只使用文本卡密。
func (s *orderFlowSender) SendImage(_ context.Context, _, _, imageURL string, _ int64, _, _ int) error {
	s.texts = append(s.texts, imageURL)
	return nil
}

// UpdateCookie 忽略平台轮换后的凭证同步；本测试不建立在线连接。
func (s *orderFlowSender) UpdateCookie(string) {}

// orderFlowSenders 让账号始终在线并共用同一个发送器替身。
type orderFlowSenders struct {
	// sender 是被共用的发送器替身。
	sender *orderFlowSender
}

// Sender 返回共用的发送器替身，表示账号在线。
func (p orderFlowSenders) Sender(string) (automation.MessageSender, bool) { return p.sender, true }

// orderFlowDetailFetcher 用真实 MTOP 客户端向模拟器查询订单详情，等价于生产适配器的订单详情查询器。
type orderFlowDetailFetcher struct {
	// client 是连接模拟器的真实 MTOP 客户端。
	client *mtop.ClientImpl
	// store 提供账号当前 Cookie。
	store *db.Store
}

// FetchOrderDetail 读取账号当前 Cookie 并查询平台订单详情。
func (f orderFlowDetailFetcher) FetchOrderDetail(ctx context.Context, cookieID, orderID, _, _, _ string) (*automation.OrderDetail, error) {
	// cookie、cookieErr 是账号当前 Cookie 及读取错误。
	cookie, cookieErr := f.store.Cookies.GetValue(ctx, cookieID)
	if cookieErr != nil {
		return nil, cookieErr
	}
	// detail、detailErr 是平台订单详情及查询错误。
	detail, detailErr := f.client.FetchOrderDetail(ctx, cookie, orderID)
	if detailErr != nil {
		return nil, detailErr
	}
	return &automation.OrderDetail{Quantity: detail.Quantity, SpecName: detail.SpecName, SpecValue: detail.SpecValue, Amount: detail.Amount, OrderStatus: detail.OrderStatus}, nil
}

// orderFlow 把平台模拟器、自动化中心、AI 回复器和 Agent 回复器装配成一套可端到端驱动的环境。
type orderFlow struct {
	// t 是当前用例，辅助方法失败时直接终止用例。
	t *testing.T
	// ctx 是全部调用共用的上下文。
	ctx context.Context
	// gateway 是平台模拟器。
	gateway *mtopsim.Gateway
	// store 是隔离的 SQLite 存储。
	store *db.Store
	// center 是使用真实 MTOP 客户端的自动化中心。
	center *automation.Center
	// sender 记录自动化中心发给买家的卡密消息。
	sender *orderFlowSender
	// adminID 是本地账号所属管理员主键。
	adminID int64
	// ai 是开启订单阶段开关的 AI 回复器；system 读取模型最近一次收到的系统提示词。
	ai     *AIReplierImpl
	system func() string
	// agentFixture 提供开启订单阶段开关的 Agent 回复器及其收到的请求。
	agentFixture *agentFixture
}

// newOrderFlow 创建端到端环境：本地账号保存模拟器签发的 Cookie，AI 与 Agent 均开启订单阶段开关。
func newOrderFlow(t *testing.T) *orderFlow {
	t.Helper()
	t.Setenv("XIANYU_DATA_KEY", "order-flow-test-key")
	// agent 是始终接手并回复一句话的 Agent 夹具，同时提供隔离存储。
	agent := newAgentFixture(t, scriptedResponse{body: `{"replies":[{"text":"收到"}]}`})
	// ctx 是全部调用共用的上下文。
	ctx := context.Background()
	// store 是 Agent 夹具创建的隔离存储，已含管理员与账号 cid。
	store := agent.store
	// admin、adminErr 是本地账号所属管理员及读取错误。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// gateway 是每个用例独立的平台状态。
	gateway := mtopsim.New()
	// saveErr 是把模拟器签发的 Cookie 保存为本地账号凭证的错误。
	if saveErr := store.Cookies.Save(ctx, "cid", gateway.AddAccount(orderFlowSellerID, "测试卖家"), admin.ID); saveErr != nil {
		t.Fatal(saveErr)
	}
	// consign 是开启自动确认发货的开关值。
	consign := true
	// settingsErr 是开启自动确认发货的写入错误。
	if _, settingsErr := store.Cookies.UpdateSettings(ctx, "cid", db.AccountSettingsUpdate{UserID: admin.ID, AutoConsign: &consign}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// client 是连接模拟器的真实 MTOP 客户端。
	client := gateway.Client()
	// sender 是在线发送器替身。
	sender := &orderFlowSender{}
	// center 是注入了真实协议客户端的自动化中心。
	center := automation.NewWithDependencies(store, orderFlowSenders{sender: sender}, nil, automation.CenterDependencies{
		MTop: client, AccountTaskClient: client, OrderDetailFetcher: orderFlowDetailFetcher{client: client, store: store},
	})
	// server、system 是本地模型服务及其系统提示词读取函数。
	server, system := capturingOpenAIServer(t, "好的")
	// aiErr 是保存开启订单阶段的全场景客服设置的错误。
	if aiErr := store.AIReply.UpsertSettings(ctx, "cid", db.AIReplySettings{AIEnabled: true, MaxDiscountPercent: 10, MaxDiscountAmount: 20,
		MaxBargainRounds: 3, AIMode: "full_service", OrderContextEnabled: true}); aiErr != nil {
		t.Fatal(aiErr)
	}
	// key、value 表示一项全局模型配置。
	for key, value := range map[string]string{"ai_api_key": "sk-test", "ai_api_url": server.URL} {
		// setErr 是全局模型配置写入错误。
		if setErr := store.Settings.Set(ctx, key, value); setErr != nil {
			t.Fatal(setErr)
		}
	}
	// current、currentErr 是 Agent 夹具已保存的配置摘要及读取错误，用于在保留地址的前提下开启订单阶段。
	current, currentErr := store.AgentReply.Get(ctx, "cid")
	if currentErr != nil {
		t.Fatal(currentErr)
	}
	// agentErr 是开启 Agent 订单阶段开关的写入错误。
	if agentErr := store.AgentReply.Upsert(ctx, "cid", db.AgentReplyWrite{Enabled: true, EndpointURL: current.EndpointURL,
		RequestTimeoutSeconds: current.RequestTimeoutSeconds, MaxSessionSeconds: current.MaxSessionSeconds, OrderContextEnabled: true}); agentErr != nil {
		t.Fatal(agentErr)
	}
	return &orderFlow{t: t, ctx: ctx, gateway: gateway, store: store, center: center, sender: sender, adminID: admin.ID,
		ai: NewAIReplier("cid", store, nil), system: system, agentFixture: agent}
}

// addItem 在平台和本地商品库同时创建一件在售商品，并为它配置“付款后发送文本卡密”的规则，返回平台商品 ID。
func (f *orderFlow) addItem(title string, cardID int64, cardText string) string {
	f.t.Helper()
	// itemID 是平台分配的商品 ID。
	itemID := f.gateway.AddItem(orderFlowSellerID, title, "9.90", false)
	// statement 表示一条夹具 SQL 及其参数。
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO item_info (cookie_id,item_id,item_title,item_price,item_description) VALUES (?,?,?,?,?)`, []any{"cid", itemID, title, "9.90", "虚拟商品"}},
		{`INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES (?,?,'text',?,1,?)`, []any{cardID, "文本卡密", cardText, f.adminID}},
	} {
		// execErr 是夹具 SQL 的执行错误。
		if _, execErr := f.store.DB.ExecContext(f.ctx, statement.query, statement.args...); execErr != nil {
			f.t.Fatalf("夹具 SQL 失败: %v", execErr)
		}
	}
	// ruleErr 是付款后发卡规则的创建错误。
	if _, ruleErr := f.store.Automation.Create(f.ctx, db.AutomationRuleInput{
		UserID: f.adminID, CookieID: "cid", ItemID: itemID, Name: "付款发卡", TriggerType: automation.TriggerOrderPaid, Enabled: true, Priority: 100,
		Actions: []db.AutomationActionInput{{ActionType: automation.ActionSendCard, CardID: cardID, DeliveryCount: 1, Enabled: true, SortOrder: 1}},
	}); ruleErr != nil {
		f.t.Fatalf("创建规则失败: %v", ruleErr)
	}
	return itemID
}

// deliver 把一条平台 WebSocket 系统消息交给事件识别和自动化中心处理，必须被识别为 wantTrigger 且处理成功。
func (f *orderFlow) deliver(raw map[string]any, wantTrigger string) {
	f.t.Helper()
	// cookie、cookieErr 是账号当前 Cookie 及读取错误。
	cookie, cookieErr := f.store.Cookies.GetValue(f.ctx, "cid")
	if cookieErr != nil {
		f.t.Fatal(cookieErr)
	}
	// task 是从系统消息识别出的自动化任务。
	task := automation.ExtractTaskFromWS("cid", cookie, raw)
	if task == nil || task.TriggerType != wantTrigger {
		f.t.Fatalf("系统消息应识别为 %s: %+v", wantTrigger, task)
	}
	// handleErr 是自动化中心的处理错误。
	if handleErr := f.center.HandleTask(f.ctx, *task); handleErr != nil {
		f.t.Fatalf("处理 %s 失败: %v", wantTrigger, handleErr)
	}
}

// expectStage 让买家在指定会话就指定商品发一条普通咨询，断言 AI 提示词与 Agent 请求中的订单阶段，
// 并断言两次回复都没有向平台发出任何 MTOP 请求。wantStages 为空表示已确认没有订单。
func (f *orderFlow) expectStage(step, chatID, itemID, wantLabel string, wantStages ...string) {
	f.t.Helper()
	// before 是生成回复前平台已收到的请求总数。
	before := f.gateway.CallCount()
	// message 是买家就该商品发来的普通咨询。
	message := ChatMessage{AccountID: "cid", ChatID: chatID, SenderUserID: orderFlowBuyerID, SenderName: "测试买家", Text: "教程怎么用", ItemID: itemID}
	// result、err 是 AI 回复结果及错误。
	result, err := f.ai.Reply(f.ctx, message)
	if err != nil || result == nil || result.Text != "好的" {
		f.t.Fatalf("%s: AI 回复异常: %+v %v", step, result, err)
	}
	// prompt 是模型本次收到的系统提示词。
	prompt := f.system()
	if !strings.Contains(prompt, wantLabel) {
		f.t.Fatalf("%s: 提示词应包含 %q:\n%s", step, wantLabel, prompt)
	}
	// seenBefore 是本次转发前 Agent 已收到的请求数。
	seenBefore := len(f.agentFixture.agent.seen())
	// agentResult、agentErr 是 Agent 回复器的接手结果及错误。
	agentResult, agentErr := f.agentFixture.replier.Reply(f.ctx, message)
	if agentErr != nil || agentResult == nil || !agentResult.Skip {
		f.t.Fatalf("%s: Agent 回复异常: %+v %v", step, agentResult, agentErr)
	}
	// requests 是 Agent 累计收到的请求；本次只应新增一个 message 事件。
	requests := f.agentFixture.agent.seen()
	if len(requests) != seenBefore+1 || requests[seenBefore].OrderContext == nil {
		f.t.Fatalf("%s: Agent 应收到携带订单阶段的 message 事件: %+v", step, requests[seenBefore:])
	}
	// got 保存 Agent 实际收到的订单阶段列表。
	got := make([]string, 0, len(requests[seenBefore].OrderContext.Orders))
	// order 表示 Agent 收到的一笔订单阶段。
	for _, order := range requests[seenBefore].OrderContext.Orders {
		got = append(got, order.Stage)
	}
	if strings.Join(got, ",") != strings.Join(wantStages, ",") {
		f.t.Fatalf("%s: Agent 订单阶段应为 %v，实际 %v", step, wantStages, got)
	}
	if after := f.gateway.CallCount(); after != before { // after 是生成回复后平台收到的请求总数。
		f.t.Fatalf("%s: 生成回复不得请求平台，新增 %d 次: %v", step, after-before, f.gateway.CallSequence())
	}
}

// TestOrderContextPlatformRegularLifecycle 沿常规订单的完整生命周期验证订单阶段：
// 未下单 → 拍下未付款 → 付款后自动发卡并确认发货 → 买家确认收货，且其他商品的咨询不受该订单影响。
func TestOrderContextPlatformRegularLifecycle(t *testing.T) {
	// flow 是独立的端到端环境。
	flow := newOrderFlow(t)
	// itemID 是买家实际购买的商品。
	itemID := flow.addItem("视频会员月卡", 31, "卡号 REGULAR-1")
	// otherItemID 是买家只咨询未购买的另一件商品，用于证明订单阶段不跨商品串单。
	otherItemID := flow.addItem("音乐会员月卡", 32, "卡号 OTHER-1")

	flow.expectStage("下单前", "chat-before-order", itemID, "未查到该买家在本商品下的订单（尚未下单）。")

	// orderID 是买家拍下但尚未付款的平台订单号。
	orderID := flow.gateway.PlaceOrder(mtopsim.OrderInput{ItemID: itemID, BuyerID: orderFlowBuyerID, BuyerNick: "测试买家", Amount: "9.90"})
	// chatID 是模拟平台为该订单分配的买卖会话，后续咨询都发生在这个会话里。
	chatID := flow.gateway.Order(orderID).ChatID
	flow.deliver(flow.gateway.CreatedEvent(orderID), automation.TriggerOrderCreated)
	flow.expectStage("拍下未付款", chatID, itemID, "- 订单阶段：已拍下，尚未确认付款", agentclient.OrderStageCreated)
	if len(flow.sender.texts) != 0 {
		t.Fatalf("拍下未付款阶段不得发卡: %v", flow.sender.texts)
	}

	flow.gateway.PayOrder(orderID)
	flow.deliver(flow.gateway.PaidEvent(orderID), automation.TriggerOrderPaid)
	// shipped 是平台侧订单事实：付款后已发卡并确认发货一次。
	shipped := flow.gateway.Order(orderID)
	if shipped.Status != mtopsim.OrderShipped || shipped.ConsignCount != 1 || len(flow.sender.texts) != 1 || flow.sender.texts[0] != "卡号 REGULAR-1" {
		t.Fatalf("付款后应发卡并确认发货一次: order=%+v texts=%v", shipped, flow.sender.texts)
	}
	flow.expectStage("发货后", chatID, itemID, "- 订单阶段：已发货，待买家确认收货", agentclient.OrderStageShipped)
	// prompt 是发货后的系统提示词，不得包含订单号、金额或卡密内容。
	prompt := flow.system()
	// secret 表示不得进入提示词的订单敏感内容。
	for _, secret := range []string{orderID, "REGULAR-1"} {
		if strings.Contains(prompt, secret) {
			t.Fatalf("提示词不得包含 %q:\n%s", secret, prompt)
		}
	}

	flow.expectStage("其他商品的咨询", chatID, otherItemID, "未查到该买家在本商品下的订单（尚未下单）。")

	flow.gateway.ConfirmReceipt(orderID)
	flow.deliver(flow.gateway.CompletedEvent(orderID), automation.TriggerOrderCompleted)
	flow.expectStage("确认收货后", chatID, itemID, "- 订单阶段：交易成功（已完成）", agentclient.OrderStageCompleted)

	// 买家再次拍下同一商品：最新订单排在最前，历史订单仍然列出。
	secondOrderID := flow.gateway.PlaceOrder(mtopsim.OrderInput{ItemID: itemID, BuyerID: orderFlowBuyerID, BuyerNick: "测试买家", Amount: "9.90"})
	flow.deliver(flow.gateway.CreatedEvent(secondOrderID), automation.TriggerOrderCreated)
	flow.expectStage("复购拍下", chatID, itemID, "- 订单阶段：已拍下，尚未确认付款", agentclient.OrderStageCreated, agentclient.OrderStageCompleted)
}

// TestOrderContextPlatformPaidWithoutAutoConsign 验证关闭自动确认发货时，付款发卡后的订单阶段为“已付款待发货”或“已发货”之一的确定结果，
// 且平台订单保持待发货、未调用确认发货接口。
func TestOrderContextPlatformPaidWithoutAutoConsign(t *testing.T) {
	// flow 是独立的端到端环境。
	flow := newOrderFlow(t)
	// consignOff 是关闭自动确认发货的开关值。
	consignOff := false
	// settingsErr 是关闭自动确认发货的写入错误。
	if _, settingsErr := flow.store.Cookies.UpdateSettings(flow.ctx, "cid", db.AccountSettingsUpdate{UserID: flow.adminID, AutoConsign: &consignOff}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// itemID 是买家购买的商品。
	itemID := flow.addItem("视频会员月卡", 31, "卡号 NOCONSIGN-1")
	// orderID 是买家已付款的平台订单号。
	orderID := flow.gateway.PlaceOrder(mtopsim.OrderInput{ItemID: itemID, BuyerID: orderFlowBuyerID, BuyerNick: "测试买家", Amount: "9.90"})
	flow.gateway.PayOrder(orderID)
	flow.deliver(flow.gateway.PaidEvent(orderID), automation.TriggerOrderPaid)
	if order := flow.gateway.Order(orderID); order.Status != mtopsim.OrderPendingShip || flow.gateway.CallCount(mtopsim.APIConsign) != 0 || len(flow.sender.texts) != 1 { // order 是平台侧订单事实。
		t.Fatalf("关闭自动确认发货时应只发卡: order=%+v texts=%v calls=%v", order, flow.sender.texts, flow.gateway.CallSequence())
	}
	flow.expectStage("已付款已发卡未确认发货", flow.gateway.Order(orderID).ChatID, itemID, "- 订单阶段：已付款，待发货", agentclient.OrderStagePaid)
}

// TestOrderContextPlatformBargainLifecycle 验证二人小刀订单：待刀成阶段保守地显示为“尚未确认付款”，
// 成功小刀并发货后显示为“已发货”；读取订单阶段不改变免拼与发货顺序。
func TestOrderContextPlatformBargainLifecycle(t *testing.T) {
	// flow 是独立的端到端环境。
	flow := newOrderFlow(t)
	// bargainOn 是开启自动免拼的开关值。
	bargainOn := true
	// settingsErr 是开启自动免拼的写入错误。
	if _, settingsErr := flow.store.Cookies.UpdateSettings(flow.ctx, "cid", db.AccountSettingsUpdate{UserID: flow.adminID, AutoBargain: &bargainOn}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// itemID 是买家购买的商品。
	itemID := flow.addItem("视频会员月卡", 31, "卡号 BARGAIN-1")
	// orderID 是买家已付款但尚未刀成的砍价订单号。
	orderID := flow.gateway.PlaceOrder(mtopsim.OrderInput{ItemID: itemID, BuyerID: orderFlowBuyerID, BuyerNick: "测试买家", Amount: "9.90", Bargain: true})
	flow.gateway.PayOrder(orderID)
	// chatID 是模拟平台为该砍价订单分配的买卖会话。
	chatID := flow.gateway.Order(orderID).ChatID

	flow.deliver(flow.gateway.BargainPendingEvent(orderID), automation.TriggerBargainPending)
	// pending 是待刀成阶段结束后的平台订单事实：只完成免拼，尚未发货。
	pending := flow.gateway.Order(orderID)
	if !pending.FreeShipped || pending.Status != mtopsim.OrderPendingShip || len(flow.sender.texts) != 0 {
		t.Fatalf("待刀成阶段只应完成免拼且不得发卡: order=%+v texts=%v", pending, flow.sender.texts)
	}
	flow.expectStage("待刀成", chatID, itemID, "- 订单阶段：已拍下，尚未确认付款", agentclient.OrderStageCreated)
	if len(flow.sender.texts) != 0 || flow.gateway.CallCount(mtopsim.APIConsign) != 0 || flow.gateway.CallCount(mtopsim.APIFreeShipping) != 1 {
		t.Fatalf("读取订单阶段不得触发发卡、确认发货或再次免拼: texts=%v calls=%v", flow.sender.texts, flow.gateway.CallSequence())
	}

	flow.deliver(flow.gateway.BargainReadyEvent(orderID), automation.TriggerOrderPaid)
	// shipped 是成功小刀后的平台订单事实。
	shipped := flow.gateway.Order(orderID)
	if shipped.Status != mtopsim.OrderShipped || shipped.ConsignCount != 1 || len(flow.sender.texts) != 1 {
		t.Fatalf("成功小刀后应发卡并确认发货一次: order=%+v texts=%v", shipped, flow.sender.texts)
	}
	flow.expectStage("成功小刀并发货", chatID, itemID, "- 订单阶段：已发货，待买家确认收货", agentclient.OrderStageShipped)
}
