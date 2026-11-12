package automation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/mtop/mtopsim"
)

// 本文件是基于平台模拟器的自动化全流程测试：
// 平台推进订单阶段并推送 WebSocket 系统消息 → ExtractTaskFromWS 识别 → 自动化中心匹配规则 →
// 真实 mtop.ClientImpl 向模拟器发出签名请求 → 断言平台侧订单事实、本地订单事实和请求次数。
// 与使用 fakeMTop 的单元测试相比，这里的确认发货、免拼、订单详情都经过真实的协议编码、错误分类和平台状态机。

// 平台流程测试使用的账号标识：本地账号 cid 对应平台卖家 platformSellerID。
const (
	// platformAccountID 是本地账号主键，与 newAutomationTestStore 创建的账号一致。
	platformAccountID = "cid"
	// platformSellerID 是平台侧卖家账号标识。
	platformSellerID = "2200000000001"
	// platformBuyerID 是平台侧买家账号标识。
	platformBuyerID = "3300000000002"
)

// platformMessage 记录自动化中心通过在线连接发给买家的一条消息，以及发送瞬间平台已收到的确认发货请求数。
type platformMessage struct {
	// chatID 是消息发往的会话。
	chatID string
	// toUserID 是消息接收方的平台账号标识。
	toUserID string
	// text 是消息正文；图片消息保存图片地址。
	text string
	// consignCallsBefore 是发送该消息时平台已收到的确认发货请求数，用于证明“先发卡后确认发货”。
	consignCallsBefore int
}

// platformSender 是平台流程测试使用的在线发送器替身：只记录消息，不建立 WebSocket 连接。
type platformSender struct {
	// gateway 用于在发送瞬间读取平台请求计数。
	gateway *mtopsim.Gateway
	// messages 按发送顺序保存全部消息。
	messages []platformMessage
	// cookieUpdates 保存自动化中心同步给在线连接的新 Cookie。
	cookieUpdates []string
}

// SendText 记录一条文本消息及发送瞬间的确认发货请求数。
func (s *platformSender) SendText(_ context.Context, chatID, toUserID, text string) error {
	s.messages = append(s.messages, platformMessage{chatID: chatID, toUserID: toUserID, text: text, consignCallsBefore: s.gateway.CallCount(mtopsim.APIConsign)})
	return nil
}

// SendImage 记录一条图片消息；流程测试只使用文本卡密，保留该方法以满足发送器接口。
func (s *platformSender) SendImage(_ context.Context, chatID, toUserID, imageURL string, _ int64, _, _ int) error {
	s.messages = append(s.messages, platformMessage{chatID: chatID, toUserID: toUserID, text: imageURL, consignCallsBefore: s.gateway.CallCount(mtopsim.APIConsign)})
	return nil
}

// UpdateCookie 记录自动化中心在平台轮换凭证后同步给在线连接的 Cookie。
func (s *platformSender) UpdateCookie(cookieStr string) {
	s.cookieUpdates = append(s.cookieUpdates, cookieStr)
}

// texts 返回已发送消息的正文列表。
func (s *platformSender) texts() []string {
	// texts 按发送顺序保存消息正文。
	texts := make([]string, 0, len(s.messages))
	// message 是已发送的一条消息。
	for _, message := range s.messages {
		texts = append(texts, message.text)
	}
	return texts
}

// platformSenderProvider 让全部账号共用同一个发送器替身。
type platformSenderProvider struct {
	// sender 是被共用的发送器替身。
	sender *platformSender
}

// Sender 返回共用的发送器替身，表示账号在线。
func (p platformSenderProvider) Sender(string) (MessageSender, bool) { return p.sender, true }

// platformOrderDetailFetcher 用真实 MTOP 客户端向模拟器查询订单详情，等价于生产 adapter 的订单详情查询器。
type platformOrderDetailFetcher struct {
	// client 是连接模拟器的真实 MTOP 客户端。
	client *mtop.ClientImpl
	// store 提供账号当前 Cookie，与生产实现一样以存储中的凭证为准。
	store *db.Store
}

// FetchOrderDetail 读取账号当前 Cookie 并查询平台订单详情，把协议结果转换为自动化中心需要的订单事实。
func (f platformOrderDetailFetcher) FetchOrderDetail(ctx context.Context, cookieID, orderID, _, _, _ string) (*OrderDetail, error) {
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
	return &OrderDetail{Quantity: detail.Quantity, SpecName: detail.SpecName, SpecValue: detail.SpecValue, Amount: detail.Amount, OrderStatus: detail.OrderStatus}, nil
}

// platformFlow 把平台模拟器、真实 MTOP 客户端、SQLite 存储和自动化中心装配成一套可端到端驱动的环境。
type platformFlow struct {
	// t 是当前用例，辅助方法失败时直接终止用例。
	t *testing.T
	// ctx 是全部调用共用的上下文。
	ctx context.Context
	// gateway 是平台模拟器。
	gateway *mtopsim.Gateway
	// store 是隔离的 SQLite 存储。
	store *db.Store
	// center 是被测自动化中心。
	center *Center
	// sender 是在线发送器替身。
	sender *platformSender
	// adminID 是本地账号所属管理员主键。
	adminID int64
}

// newPlatformFlow 创建平台流程环境：本地账号保存模拟器签发的 Cookie，自动化中心的全部平台请求都发往模拟器。
func newPlatformFlow(t *testing.T) *platformFlow {
	t.Helper()
	// store、cleanup 是隔离存储及其关闭函数；夹具默认开启自动确认发货。
	store, cleanup := newAutomationTestStore(t)
	t.Cleanup(cleanup)
	// ctx 是全部调用共用的上下文。
	ctx := context.Background()
	// admin、adminErr 是本地账号所属管理员。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// gateway 是每个用例独立的平台状态。
	gateway := mtopsim.New()
	// saveErr 是把模拟器签发的 Cookie 保存为本地账号凭证的错误。
	if saveErr := store.Cookies.Save(ctx, platformAccountID, gateway.AddAccount(platformSellerID, "测试卖家"), admin.ID); saveErr != nil {
		t.Fatal(saveErr)
	}
	// client 是连接模拟器的真实 MTOP 客户端，确认发货、免拼、改价、评价和擦亮共用它。
	client := gateway.Client()
	// sender 是在线发送器替身。
	sender := &platformSender{gateway: gateway}
	// center 是注入了真实协议客户端的自动化中心。
	center := NewWithDependencies(store, platformSenderProvider{sender: sender}, nil, CenterDependencies{
		MTop: client, AccountTaskClient: client, OrderDetailFetcher: platformOrderDetailFetcher{client: client, store: store},
	})
	return &platformFlow{t: t, ctx: ctx, gateway: gateway, store: store, center: center, sender: sender, adminID: admin.ID}
}

// exec 执行一条夹具 SQL，失败时终止用例。
func (f *platformFlow) exec(query string, args ...any) {
	f.t.Helper()
	// execErr 是夹具 SQL 的执行错误。
	if _, execErr := f.store.DB.ExecContext(f.ctx, query, args...); execErr != nil {
		f.t.Fatalf("夹具 SQL 失败: %v", execErr)
	}
}

// addItem 在平台和本地商品库同时创建一件在售商品，返回平台商品 ID。
func (f *platformFlow) addItem(title string, multiSpec bool) string {
	f.t.Helper()
	// itemID 是平台分配的商品 ID。
	itemID := f.gateway.AddItem(platformSellerID, title, "9.90", multiSpec)
	// multiSpecFlag 是本地商品库的多规格标记。
	multiSpecFlag := 0
	if multiSpec {
		multiSpecFlag = 1
	}
	f.exec(`INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES (?,?,?,?)`, platformAccountID, itemID, title, multiSpecFlag)
	return itemID
}

// addTextCard 创建固定文本卡密：每次发货都发送同一段内容。
func (f *platformFlow) addTextCard(cardID int64, content string) {
	f.t.Helper()
	f.exec(`INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES (?,?,'text',?,1,?)`, cardID, "文本卡密", content, f.adminID)
}

// addDataCard 创建库存卡密：每行是一份库存，发出后即被消耗。
func (f *platformFlow) addDataCard(cardID int64, lines ...string) {
	f.t.Helper()
	f.exec(`INSERT INTO cards (id,name,type,data_content,enabled,user_id) VALUES (?,?,'data',?,1,?)`, cardID, "库存卡密", strings.Join(lines, "\n"), f.adminID)
}

// addRule 为商品创建一条启用的自动化规则。
func (f *platformFlow) addRule(itemID, triggerType string, actions ...db.AutomationActionInput) {
	f.t.Helper()
	// index 是动作在规则中的位置，用于补全执行顺序。
	for index := range actions {
		actions[index].Enabled = true
		actions[index].SortOrder = index + 1
	}
	// ruleErr 是规则创建错误。
	if _, ruleErr := f.store.Automation.Create(f.ctx, db.AutomationRuleInput{
		UserID: f.adminID, CookieID: platformAccountID, ItemID: itemID, Name: "平台流程规则", TriggerType: triggerType,
		Enabled: true, Priority: 100, Actions: actions,
	}); ruleErr != nil {
		f.t.Fatalf("创建规则失败: %v", ruleErr)
	}
}

// setSettings 修改本地账号的自动化开关。
func (f *platformFlow) setSettings(update db.AccountSettingsUpdate) {
	f.t.Helper()
	update.UserID = f.adminID
	// settingsErr 是账号设置写入错误。
	if _, settingsErr := f.store.Cookies.UpdateSettings(f.ctx, platformAccountID, update); settingsErr != nil {
		f.t.Fatalf("修改账号设置失败: %v", settingsErr)
	}
}

// placePaidOrder 模拟买家拍下并付款，返回平台订单号。
func (f *platformFlow) placePaidOrder(input mtopsim.OrderInput) string {
	f.t.Helper()
	input.BuyerID, input.BuyerNick = platformBuyerID, "测试买家"
	if input.Amount == "" {
		input.Amount = "9.90"
	}
	// orderID 是平台分配的订单号。
	orderID := f.gateway.PlaceOrder(input)
	f.gateway.PayOrder(orderID)
	return orderID
}

// deliver 把一条平台 WebSocket 系统消息交给事件识别和自动化中心处理，等价于 engine 收到系统消息后的分流。
// 返回识别出的触发类型（未识别时为空）和自动化中心的处理错误。
func (f *platformFlow) deliver(raw map[string]any) (string, error) {
	f.t.Helper()
	// cookie、cookieErr 是账号当前 Cookie；engine 在分流时同样携带当前凭证。
	cookie, cookieErr := f.store.Cookies.GetValue(f.ctx, platformAccountID)
	if cookieErr != nil {
		f.t.Fatal(cookieErr)
	}
	// task 是从系统消息识别出的自动化任务；无法识别或属于买家副本时为 nil。
	task := ExtractTaskFromWS(platformAccountID, cookie, raw)
	if task == nil {
		return "", nil
	}
	return task.TriggerType, f.center.HandleTask(f.ctx, *task)
}

// mustDeliver 处理一条必须被识别为 wantTrigger 且处理成功的系统消息。
func (f *platformFlow) mustDeliver(raw map[string]any, wantTrigger string) {
	f.t.Helper()
	// trigger、handleErr 是识别出的触发类型和处理错误。
	trigger, handleErr := f.deliver(raw)
	if trigger != wantTrigger || handleErr != nil {
		f.t.Fatalf("系统消息应识别为 %s 并处理成功，实际 trigger=%q err=%v", wantTrigger, trigger, handleErr)
	}
}

// localOrder 返回本地订单事实；订单不存在时终止用例。
func (f *platformFlow) localOrder(orderID string) *db.Order {
	f.t.Helper()
	// order、orderErr 是本地订单及读取错误。
	order, orderErr := f.store.Orders.Get(f.ctx, orderID)
	if orderErr != nil {
		f.t.Fatalf("读取本地订单 %s 失败: %v", orderID, orderErr)
	}
	return order
}

// TestPlatformFlowRegularDelivery 覆盖分支一“常规发货”：付款 WebSocket → 获取订单金额数量 → 发卡 → 确认发货，
// 并验证重复推送不会重复发卡或重复确认发货。
func TestPlatformFlowRegularDelivery(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// itemID 是单规格商品；orderID 是买家已付款的订单。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 REGULAR-1")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// orderID 是买家已付款、等待发货的平台订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})

	flow.mustDeliver(flow.gateway.PaidEvent(orderID), TriggerOrderPaid)

	// platformOrder 是平台侧订单事实。
	platformOrder := flow.gateway.Order(orderID)
	if platformOrder.Status != mtopsim.OrderShipped || platformOrder.ConsignCount != 1 {
		t.Fatalf("平台订单应已确认发货一次: %+v", platformOrder)
	}
	if len(flow.sender.messages) != 1 {
		t.Fatalf("应只发送一条卡密消息: %+v", flow.sender.messages)
	}
	// message 是发给买家的卡密消息。
	message := flow.sender.messages[0]
	if message.text != "卡号 REGULAR-1" || message.chatID != platformOrder.ChatID || message.toUserID != platformBuyerID || message.consignCallsBefore != 0 {
		t.Fatalf("卡密必须在确认发货之前发到买家会话: %+v", message)
	}
	// want 是期望的平台请求序列：一次订单详情补齐金额数量，一次确认发货。
	want := mtopsim.APIOrderDetail + "," + mtopsim.APIConsign
	if sequence := strings.Join(flow.gateway.CallSequence(), ","); sequence != want { // sequence 是平台实际收到的请求序列。
		t.Fatalf("平台请求序列应为 %s，实际 %s", want, sequence)
	}
	// localOrder 是本地订单事实，必须与平台一致。
	localOrder := flow.localOrder(orderID)
	if localOrder.OrderStatus != "shipped" || !localOrder.SystemShipped || localOrder.Quantity != "1" || localOrder.Amount != "9.90" ||
		localOrder.ItemID != itemID || localOrder.BuyerID != platformBuyerID || localOrder.CookieID != platformAccountID {
		t.Fatalf("本地订单事实: %+v", localOrder)
	}

	// 平台重复推送同一付款消息：不得再次发卡，也不得再次确认发货。
	// replay 是重复推送的序号。
	for replay := 0; replay < 2; replay++ {
		// handleErr 是重复消息的处理错误。
		if _, handleErr := flow.deliver(flow.gateway.PaidEvent(orderID)); handleErr != nil {
			t.Fatalf("重复付款消息处理失败: %v", handleErr)
		}
	}
	if len(flow.sender.messages) != 1 || flow.gateway.CallCount(mtopsim.APIConsign) != 1 || flow.gateway.Order(orderID).ConsignCount != 1 {
		t.Fatalf("重复付款消息不得重复发卡或确认发货: messages=%d calls=%v", len(flow.sender.messages), flow.gateway.CallSequence())
	}
}

// TestPlatformFlowAutoConsignDisabled 验证账号关闭“自动确认发货”时只发卡，不调用确认发货接口，平台订单保持待发货。
func TestPlatformFlowAutoConsignDisabled(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// consignOff 是关闭自动确认发货的开关值。
	consignOff := false
	flow.setSettings(db.AccountSettingsUpdate{AutoConsign: &consignOff})
	// itemID 是单规格商品；orderID 是买家已付款的订单。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 NOCONSIGN-1")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// orderID 是买家已付款、等待发货的平台订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})

	flow.mustDeliver(flow.gateway.PaidEvent(orderID), TriggerOrderPaid)

	if len(flow.sender.messages) != 1 || flow.gateway.CallCount(mtopsim.APIConsign) != 0 || flow.gateway.Order(orderID).Status != mtopsim.OrderPendingShip {
		t.Fatalf("关闭自动确认发货时只应发卡: messages=%v calls=%v", flow.sender.texts(), flow.gateway.CallSequence())
	}
}

// TestPlatformFlowAutoDeliveryDisabled 验证账号关闭“自动发货”总开关时付款消息不发卡、不请求平台。
func TestPlatformFlowAutoDeliveryDisabled(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// deliveryOff 是关闭自动发货总开关的值。
	deliveryOff := false
	flow.setSettings(db.AccountSettingsUpdate{AutoConfirm: &deliveryOff})
	// itemID 是单规格商品；orderID 是买家已付款的订单。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 OFF-1")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// orderID 是买家已付款、等待发货的平台订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})

	flow.mustDeliver(flow.gateway.PaidEvent(orderID), TriggerOrderPaid)

	if len(flow.sender.messages) != 0 || flow.gateway.CallCount() != 0 {
		t.Fatalf("关闭自动发货后不得发卡或请求平台: messages=%v calls=%v", flow.sender.texts(), flow.gateway.CallSequence())
	}
}

// TestPlatformFlowMultiSpecDelivery 覆盖分支二“多规格发货”：只执行完整匹配订单 SKU 组合的发卡动作，按购买件数发放库存；
// 规格组合不匹配任何动作的订单既不发卡也不确认发货。
func TestPlatformFlowMultiSpecDelivery(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// itemID 是带“颜色 × 时长”两个规格维度的商品。
	itemID := flow.addItem("视频会员", true)
	flow.addDataCard(41, "RED-MONTH-1", "RED-MONTH-2", "RED-MONTH-3")
	flow.addDataCard(42, "BLUE-YEAR-1", "BLUE-YEAR-2")
	flow.addRule(itemID, TriggerOrderPaid,
		db.AutomationActionInput{ActionType: ActionSendCard, CardID: 41, DeliveryCount: 1, ConfigJSON: `{"spec_name":"颜色；时长","spec_value":"红色；月卡"}`},
		db.AutomationActionInput{ActionType: ActionSendCard, CardID: 42, DeliveryCount: 1, ConfigJSON: `{"spec_name":"颜色；时长","spec_value":"蓝色；年卡"}`},
	)
	// matchedOrder 购买两件“红色；月卡”。
	matchedOrder := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID, Quantity: 2, Amount: "19.80",
		Specs: []mtopsim.Spec{{Name: "颜色", Value: "红色"}, {Name: "时长", Value: "月卡"}}})

	flow.mustDeliver(flow.gateway.PaidEvent(matchedOrder), TriggerOrderPaid)

	// delivered 是发给买家的全部内容。
	delivered := strings.Join(flow.sender.texts(), "\n")
	if !strings.Contains(delivered, "RED-MONTH-1") || !strings.Contains(delivered, "RED-MONTH-2") ||
		strings.Contains(delivered, "RED-MONTH-3") || strings.Contains(delivered, "BLUE") {
		t.Fatalf("应只发放两份红色月卡库存，实际发送 %q", delivered)
	}
	if flow.gateway.Order(matchedOrder).Status != mtopsim.OrderShipped {
		t.Fatalf("匹配规格的订单应已确认发货")
	}
	// localOrder 是本地订单事实，规格必须是完整组合。
	localOrder := flow.localOrder(matchedOrder)
	if localOrder.SpecName != "颜色；时长" || localOrder.SpecValue != "红色；月卡" || localOrder.Quantity != "2" {
		t.Fatalf("本地订单规格事实: %+v", localOrder)
	}

	// unmatchedOrder 的规格组合“红色；年卡”没有对应动作：只匹配部分规格不得发卡。
	unmatchedOrder := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID,
		Specs: []mtopsim.Spec{{Name: "颜色", Value: "红色"}, {Name: "时长", Value: "年卡"}}})
	// sentBefore 是处理不匹配订单之前已发送的消息数。
	sentBefore := len(flow.sender.messages)
	flow.gateway.ResetCalls()
	// handleErr 是不匹配订单的处理错误；无论是否报错都不得产生发货副作用。
	_, handleErr := flow.deliver(flow.gateway.PaidEvent(unmatchedOrder))
	if len(flow.sender.messages) != sentBefore || flow.gateway.CallCount(mtopsim.APIConsign) != 0 || flow.gateway.Order(unmatchedOrder).Status != mtopsim.OrderPendingShip {
		t.Fatalf("规格不完整匹配时不得发卡或确认发货: err=%v messages=%v calls=%v", handleErr, flow.sender.texts(), flow.gateway.CallSequence())
	}
}

// TestPlatformFlowBargainWithoutAutoFreeShipping 覆盖分支三“二人小刀普通发货”：未开启自动免拼时不处理“待刀成”阶段，
// 收到“我已成功小刀，待发货”后才发卡并确认发货。
func TestPlatformFlowBargainWithoutAutoFreeShipping(t *testing.T) {
	// flow 是独立的平台流程环境；自动免拼默认关闭。
	flow := newPlatformFlow(t)
	// itemID 是单规格商品；orderID 是已付款但未刀成的砍价订单。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 BARGAIN-1")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// orderID 是买家已付款但尚未刀成的砍价订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID, Bargain: true})

	flow.mustDeliver(flow.gateway.BargainPendingEvent(orderID), TriggerBargainPending)
	if len(flow.sender.messages) != 0 || flow.gateway.CallCount() != 0 {
		t.Fatalf("未开启自动免拼时待刀成阶段不得发卡或请求平台: messages=%v calls=%v", flow.sender.texts(), flow.gateway.CallSequence())
	}

	// 第二位买家参与，订单自然刀成，平台推送第二阶段卡片。
	flow.gateway.FormBargain(orderID)
	flow.mustDeliver(flow.gateway.BargainReadyEvent(orderID), TriggerOrderPaid)

	// platformOrder 是平台侧订单事实：从未免拼，已确认发货一次。
	platformOrder := flow.gateway.Order(orderID)
	if platformOrder.FreeShipped || platformOrder.Status != mtopsim.OrderShipped || platformOrder.ConsignCount != 1 {
		t.Fatalf("自然刀成后应按常规顺序发货: %+v", platformOrder)
	}
	if len(flow.sender.messages) != 1 || flow.sender.messages[0].consignCallsBefore != 0 || flow.gateway.CallCount(mtopsim.APIFreeShipping) != 0 {
		t.Fatalf("应先发卡后确认发货且不调用免拼: messages=%+v calls=%v", flow.sender.messages, flow.gateway.CallSequence())
	}
}

// TestPlatformFlowBargainWithAutoFreeShipping 覆盖分支四“二人小刀免拼发货”：“待刀成”只执行一次免拼且绝不发卡；
// 必须等到“我已成功小刀，待发货”才发卡，发卡成功后才确认发货；确认发货走普通接口而不是免拼接口。
func TestPlatformFlowBargainWithAutoFreeShipping(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// bargainOn 是开启自动免拼的开关值。
	bargainOn := true
	flow.setSettings(db.AccountSettingsUpdate{AutoBargain: &bargainOn})
	// itemID 是单规格商品；orderID 是已付款但未刀成的砍价订单。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 FREESHIP-1")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// orderID 是买家已付款但尚未刀成的砍价订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID, Bargain: true})

	// 平台重复推送“待刀成”卡片：免拼只能执行一次。
	// replay 是重复推送的序号。
	for replay := 0; replay < 3; replay++ {
		// trigger、handleErr 是识别结果和处理错误。
		if trigger, handleErr := flow.deliver(flow.gateway.BargainPendingEvent(orderID)); trigger != TriggerBargainPending || handleErr != nil {
			t.Fatalf("待刀成消息处理失败: trigger=%q err=%v", trigger, handleErr)
		}
	}
	// afterPending 是免拼阶段结束后的平台订单事实：已刀成、仍待发货。
	afterPending := flow.gateway.Order(orderID)
	if !afterPending.FreeShipped || !afterPending.BargainFormed || afterPending.Status != mtopsim.OrderPendingShip {
		t.Fatalf("待刀成阶段只应完成免拼: %+v", afterPending)
	}
	if sequence := strings.Join(flow.gateway.CallSequence(), ","); sequence != mtopsim.APIFreeShipping { // sequence 是平台实际收到的请求序列。
		t.Fatalf("待刀成阶段只应请求一次免拼接口，实际 %s", sequence)
	}
	if len(flow.sender.messages) != 0 {
		t.Fatalf("待刀成阶段严禁发卡: %v", flow.sender.texts())
	}

	flow.gateway.ResetCalls()
	flow.mustDeliver(flow.gateway.BargainReadyEvent(orderID), TriggerOrderPaid)

	// shipped 是最终平台订单事实。
	shipped := flow.gateway.Order(orderID)
	if shipped.Status != mtopsim.OrderShipped || shipped.ConsignCount != 1 {
		t.Fatalf("成功小刀后应确认发货一次: %+v", shipped)
	}
	if len(flow.sender.messages) != 1 || flow.sender.messages[0].text != "卡号 FREESHIP-1" || flow.sender.messages[0].consignCallsBefore != 0 {
		t.Fatalf("发卡必须先于确认发货: %+v", flow.sender.messages)
	}
	if flow.gateway.CallCount(mtopsim.APIFreeShipping) != 0 || flow.gateway.CallCount(mtopsim.APIConsign) != 1 {
		t.Fatalf("确认发货必须走普通接口且不得再次免拼: %v", flow.gateway.CallSequence())
	}
	if localOrder := flow.localOrder(orderID); localOrder.OrderStatus != "shipped" || localOrder.IsBargain == 0 { // localOrder 是本地订单事实。
		t.Fatalf("本地订单应记录为已发货的砍价订单: %+v", localOrder)
	}
}

// TestPlatformFlowBargainReadyBeforeFormedIsRejectedByPlatform 验证平台是确认发货顺序的最终裁决者：
// 即使错误地收到“待发货”卡片，只要订单在平台上尚未刀成，确认发货就会被平台拒绝，订单保持待发货。
func TestPlatformFlowBargainReadyBeforeFormedIsRejectedByPlatform(t *testing.T) {
	// flow 是独立的平台流程环境；自动免拼默认关闭，订单保持未刀成。
	flow := newPlatformFlow(t)
	// itemID 是单规格商品；orderID 是已付款但未刀成的砍价订单。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 EARLY-1")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// orderID 是买家已付款但尚未刀成的砍价订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID, Bargain: true})

	// handleErr 是处理结果：确认发货被平台明确拒绝，必须作为失败上报而不是静默成功。
	_, handleErr := flow.deliver(flow.gateway.BargainReadyEvent(orderID))
	if handleErr == nil {
		t.Fatalf("平台拒绝确认发货时必须返回错误")
	}
	if flow.gateway.Order(orderID).Status != mtopsim.OrderPendingShip || flow.localOrder(orderID).OrderStatus == "shipped" {
		t.Fatalf("平台拒绝后平台与本地订单都不得变为已发货")
	}
}

// TestPlatformFlowBuyerCopyIsIgnored 验证同一笔交易推送给买家账号的副本不会触发卖家自动化。
func TestPlatformFlowBuyerCopyIsIgnored(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// itemID 是单规格商品；orderID 是买家已付款的订单。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 BUYER-1")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// orderID 是买家已付款、等待发货的平台订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})

	// trigger、handleErr 是买家副本的识别结果和处理错误。
	trigger, handleErr := flow.deliver(flow.gateway.PaidEventBuyerCopy(orderID))
	if trigger != "" || handleErr != nil || len(flow.sender.messages) != 0 || flow.gateway.CallCount() != 0 {
		t.Fatalf("买家副本不得进入自动化: trigger=%q err=%v messages=%v calls=%v", trigger, handleErr, flow.sender.texts(), flow.gateway.CallSequence())
	}
}

// TestPlatformFlowTokenExpiryDuringDelivery 验证发货过程中签名 Token 过期时自动恢复：只发一次卡、只确认发货一次，
// 平台轮换后的 Cookie 被写回本地账号并同步给在线连接。
func TestPlatformFlowTokenExpiryDuringDelivery(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// itemID 是单规格商品；orderID 是买家已付款的订单。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 TOKEN-1")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// orderID 是买家已付款、等待发货的平台订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})
	// staleCookie 是 Token 过期前保存在本地的 Cookie。
	staleCookie, _ := flow.store.Cookies.GetValue(flow.ctx, platformAccountID)
	flow.gateway.ExpireToken(platformSellerID)

	flow.mustDeliver(flow.gateway.PaidEvent(orderID), TriggerOrderPaid)

	if len(flow.sender.messages) != 1 || flow.gateway.Order(orderID).ConsignCount != 1 {
		t.Fatalf("Token 过期恢复后应恰好发卡一次、确认发货一次: messages=%v order=%+v", flow.sender.texts(), flow.gateway.Order(orderID))
	}
	// savedCookie 是流程结束后本地保存的 Cookie，必须带有平台新签发的 Token。
	savedCookie, _ := flow.store.Cookies.GetValue(flow.ctx, platformAccountID)
	if !mtop.MTopTokenCookieChanged(staleCookie, savedCookie) {
		t.Fatalf("平台轮换的 Token 必须写回本地账号")
	}
	// 写回的凭证必须可直接用于下一次平台请求，不再触发 Token 过期。
	flow.gateway.ResetCalls()
	// detailErr 是使用写回凭证查询订单详情的错误。
	if _, detailErr := flow.gateway.Client().FetchOrderDetail(flow.ctx, savedCookie, orderID); detailErr != nil || flow.gateway.CallCount() != 1 {
		t.Fatalf("写回的凭证应一次请求成功: err=%v calls=%v", detailErr, flow.gateway.CallSequence())
	}
}

// TestPlatformFlowRiskControlStopsDelivery 验证风控期间付款事件不会发卡，也不会反复请求平台。
func TestPlatformFlowRiskControlStopsDelivery(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// itemID 是单规格商品；orderID 是买家已付款的订单。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 RISK-1")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// orderID 是买家已付款、等待发货的平台订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})
	flow.gateway.SetRiskControl(platformSellerID, true)

	// handleErr 是风控期间付款事件的处理结果；事件可以被延期，但不得发卡。
	_, handleErr := flow.deliver(flow.gateway.PaidEvent(orderID))
	if len(flow.sender.messages) != 0 || flow.gateway.Order(orderID).Status != mtopsim.OrderPendingShip {
		t.Fatalf("风控期间不得发卡或确认发货: err=%v messages=%v", handleErr, flow.sender.texts())
	}
	if calls := flow.gateway.CallCount(); calls != 1 { // calls 是风控期间实际发出的平台请求数。
		t.Fatalf("风控期间一次付款事件最多请求平台一次，实际 %d 次：%v", calls, flow.gateway.CallSequence())
	}
}

// TestPlatformFlowSensitiveWordConsign 回归 issue 113：发货内容命中平台敏感词时，确认发货是平台明确拒绝而不是结果未知：
// 运行转人工核对、卡密不重发、确认发货不在同一次运行内重试；改用“不携带凭证”设置后确认发货可以成功。
func TestPlatformFlowSensitiveWordConsign(t *testing.T) {
	t.Run("默认携带卡密全文被平台拒绝", func(t *testing.T) {
		// flow 是独立的平台流程环境。
		flow := newPlatformFlow(t)
		// itemID 是单规格商品；orderID 是买家已付款的订单。
		itemID := flow.addItem("视频会员月卡", false)
		flow.addTextCard(31, "卡号 违禁-1")
		flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
		// orderID 是买家已付款、等待发货的平台订单号。
		orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})

		// handleErr 是处理结果：买家已收到卡密而平台未发货，必须转人工核对。
		_, handleErr := flow.deliver(flow.gateway.PaidEvent(orderID))
		if !errors.Is(handleErr, errAutomationNeedsReview) || !strings.Contains(handleErr.Error(), "HIT_SENSITIVE_WORD") {
			t.Fatalf("敏感词拒绝应转人工核对并保留平台原因: %v", handleErr)
		}
		if len(flow.sender.messages) != 1 || flow.gateway.CallCount(mtopsim.APIConsign) != 1 || flow.gateway.Order(orderID).Status != mtopsim.OrderPendingShip {
			t.Fatalf("卡密只发一次、确认发货只请求一次、平台订单保持待发货: messages=%v calls=%v", flow.sender.texts(), flow.gateway.CallSequence())
		}
		if flow.localOrder(orderID).OrderStatus == "shipped" {
			t.Fatalf("平台拒绝后本地订单不得标记为已发货")
		}
	})
	t.Run("不携带凭证时确认发货成功", func(t *testing.T) {
		// flow 是独立的平台流程环境。
		flow := newPlatformFlow(t)
		// proofNone 是“确认发货不携带凭证”的设置值。
		proofNone := db.ConsignProofModeNone
		flow.setSettings(db.AccountSettingsUpdate{ConsignProofMode: &proofNone})
		// itemID 是单规格商品；orderID 是买家已付款的订单。
		itemID := flow.addItem("视频会员月卡", false)
		flow.addTextCard(31, "卡号 违禁-1")
		flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
		// orderID 是买家已付款、等待发货的平台订单号。
		orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})

		flow.mustDeliver(flow.gateway.PaidEvent(orderID), TriggerOrderPaid)

		// platformOrder 是平台侧订单事实：已发货且凭证为空，买家仍收到完整卡密。
		platformOrder := flow.gateway.Order(orderID)
		if platformOrder.Status != mtopsim.OrderShipped || platformOrder.ConsignText != "" || len(flow.sender.messages) != 1 || flow.sender.messages[0].text != "卡号 违禁-1" {
			t.Fatalf("不携带凭证时应正常发货: order=%+v messages=%v", platformOrder, flow.sender.texts())
		}
	})
}

// TestPlatformFlowAdjustPriceOnOrderCreated 验证“拍下后自动改价”：待付款订单被改为目标价格，平台暂时不可改价时有限重试。
func TestPlatformFlowAdjustPriceOnOrderCreated(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// previousGap 保存生产重试间隔；测试缩短间隔以免真实等待，结束时恢复。
	previousGap := adjustPriceTransientRetryGap
	adjustPriceTransientRetryGap = 0
	t.Cleanup(func() { adjustPriceTransientRetryGap = previousGap })
	// itemID 是单规格商品；orderID 是买家拍下但未付款的订单。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addRule(itemID, TriggerOrderCreated, db.AutomationActionInput{ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"6.66"}`})
	// orderID 是买家已拍下、尚未付款的平台订单号。
	orderID := flow.gateway.PlaceOrder(mtopsim.OrderInput{ItemID: itemID, BuyerID: platformBuyerID, BuyerNick: "测试买家", Amount: "9.90"})
	flow.gateway.SetAdjustBusy(orderID, 2)

	flow.mustDeliver(flow.gateway.CreatedEvent(orderID), TriggerOrderCreated)

	if amount := flow.gateway.Order(orderID).Amount; amount != "6.66" { // amount 是改价后的平台成交金额。
		t.Fatalf("订单应被改价为 6.66，实际 %s；calls=%v", amount, flow.gateway.CallSequence())
	}
	if attempts := flow.gateway.CallCount(mtopsim.APIAdjustPrice); attempts != 3 { // attempts 是改价接口实际请求次数。
		t.Fatalf("两次暂时不可改价后应共请求 3 次，实际 %d 次", attempts)
	}
}

// TestPlatformFlowReviewGift 验证“买家评价后赠送”：收到买家评价系统消息后发送赠品，不请求任何平台接口。
func TestPlatformFlowReviewGift(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// itemID 是单规格商品；orderID 是已完成交易并被买家评价的订单。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 PAID-1")
	flow.addTextCard(32, "赠品 GIFT-1")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	flow.addRule(itemID, TriggerBuyerReviewed, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 32, DeliveryCount: 1})
	// orderID 是买家已付款、等待发货的平台订单号。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})
	flow.mustDeliver(flow.gateway.PaidEvent(orderID), TriggerOrderPaid)
	flow.gateway.ConfirmReceipt(orderID)
	flow.mustDeliver(flow.gateway.CompletedEvent(orderID), TriggerOrderCompleted)
	if completed := flow.localOrder(orderID); completed.OrderStatus != "completed" || completed.CompletedAt == "" { // completed 是确认收货后的本地订单事实。
		t.Fatalf("确认收货消息应推进本地订单完成事实: %+v", completed)
	}

	flow.gateway.BuyerRate(orderID)
	flow.gateway.ResetCalls()
	flow.mustDeliver(flow.gateway.BuyerReviewedEvent(orderID), TriggerBuyerReviewed)
	// 重复推送评价消息不得重复赠送。
	flow.mustDeliver(flow.gateway.BuyerReviewedEvent(orderID), TriggerBuyerReviewed)

	// texts 是发给买家的全部消息：付款卡密一条，赠品一条。
	texts := flow.sender.texts()
	if len(texts) != 2 || texts[1] != "赠品 GIFT-1" || flow.gateway.CallCount() != 0 {
		t.Fatalf("评价赠品应只发送一次且不请求平台: texts=%v calls=%v", texts, flow.gateway.CallSequence())
	}
}

// TestPlatformFlowAutoRate 验证自动评价账号任务：只评价本地已记录确认收货的订单，平台侧评价内容与配置一致，重复执行不再请求平台。
func TestPlatformFlowAutoRate(t *testing.T) {
	// flow 是独立的平台流程环境。
	flow := newPlatformFlow(t)
	// itemID 是单规格商品。
	itemID := flow.addItem("视频会员月卡", false)
	flow.addTextCard(31, "卡号 RATE-1")
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	// settingsErr 是自动评价配置写入错误。
	if settingsErr := flow.store.AccountTasks.Upsert(flow.ctx, db.AccountTaskSettings{CookieID: platformAccountID, AutoRateEnabled: true, RateContent: "感谢支持"}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// completedOrder 已确认收货；shippedOrder 只发货未确认收货，不应被评价。
	completedOrder := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})
	// shippedOrder 是只发货、买家尚未确认收货的订单号，不应被评价。
	shippedOrder := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})
	flow.mustDeliver(flow.gateway.PaidEvent(completedOrder), TriggerOrderPaid)
	flow.mustDeliver(flow.gateway.PaidEvent(shippedOrder), TriggerOrderPaid)
	flow.gateway.ConfirmReceipt(completedOrder)
	flow.mustDeliver(flow.gateway.CompletedEvent(completedOrder), TriggerOrderCompleted)
	flow.gateway.ResetCalls()

	// summary、taskErr 是自动评价任务的执行结果。
	summary, taskErr := flow.center.RunAccountTask(flow.ctx, platformAccountID, TaskAutoRate)
	if taskErr != nil || summary.Found != 1 || summary.Success != 1 {
		t.Fatalf("自动评价: %+v err=%v", summary, taskErr)
	}
	// rated 是平台侧被评价订单的事实。
	rated := flow.gateway.Order(completedOrder)
	if !rated.SellerRated || rated.SellerFeedback != "感谢支持" || flow.gateway.Order(shippedOrder).SellerRated {
		t.Fatalf("只应评价已确认收货的订单: %+v", rated)
	}
	if sequence := strings.Join(flow.gateway.CallSequence(), ","); sequence != mtopsim.APIRateCreate { // sequence 是平台实际收到的请求序列。
		t.Fatalf("一笔待评价订单只应请求一次评价接口，实际 %s", sequence)
	}
	flow.gateway.ResetCalls()
	if summary, taskErr = flow.center.RunAccountTask(flow.ctx, platformAccountID, TaskAutoRate); taskErr != nil || summary.Found != 0 || flow.gateway.CallCount() != 0 {
		t.Fatalf("已评价订单不得再次请求平台: %+v err=%v calls=%v", summary, taskErr, flow.gateway.CallSequence())
	}
}

// TestPlatformFlowAutoPolish 验证每日擦亮账号任务：先分页取得在售商品，再逐件擦亮；遇到风控立即终止本批次，不再请求其余商品。
func TestPlatformFlowAutoPolish(t *testing.T) {
	t.Run("全部擦亮", func(t *testing.T) {
		// flow 是独立的平台流程环境。
		flow := newPlatformFlow(t)
		// itemIDs 是三件在售商品。
		itemIDs := []string{flow.addItem("商品甲", false), flow.addItem("商品乙", false), flow.addItem("商品丙", false)}
		// summary、taskErr 是擦亮任务的执行结果。
		summary, taskErr := flow.center.RunAccountTask(flow.ctx, platformAccountID, TaskAutoPolish)
		if taskErr != nil || summary.Found != 3 || summary.Success != 3 {
			t.Fatalf("擦亮: %+v err=%v", summary, taskErr)
		}
		// itemID 是被擦亮的商品。
		for _, itemID := range itemIDs {
			if item, _ := flow.gateway.Item(itemID); item.PolishedOn == "" { // item 是平台侧商品事实。
				t.Fatalf("商品 %s 应已擦亮", itemID)
			}
		}
		if list, polish := flow.gateway.CallCount(mtopsim.APIItemList), flow.gateway.CallCount(mtopsim.APIPolish); list != 1 || polish != 3 { // list、polish 是商品列表和擦亮接口的请求数。
			t.Fatalf("应请求商品列表 1 次、擦亮 3 次，实际 %d、%d", list, polish)
		}
	})
	t.Run("批次中途遇风控立即终止", func(t *testing.T) {
		// flow 是独立的平台流程环境。
		flow := newPlatformFlow(t)
		// index 是批量添加商品的序号，共 5 件。
		for index := 0; index < 5; index++ {
			flow.addItem("商品"+string(rune('A'+index)), false)
		}
		// 第 2 次擦亮请求返回风控。
		flow.gateway.InjectFault(mtopsim.APIPolish, mtopsim.Fault{Ret: []string{mtopsim.RetSuccess}}, mtopsim.Fault{Ret: []string{mtopsim.RetRiskControl}})
		// taskErr 是擦亮任务的执行结果，必须把风控上报给调用方。
		_, taskErr := flow.center.RunAccountTask(flow.ctx, platformAccountID, TaskAutoPolish)
		if !mtop.IsRiskVerificationErr(taskErr) {
			t.Fatalf("应上报风控: %v", taskErr)
		}
		if polish, backup := flow.gateway.CallCount(mtopsim.APIPolish), flow.gateway.CallCount(mtopsim.APIPolishBackup); polish != 2 || backup != 0 { // polish、backup 是主接口和备用接口的请求数。
			t.Fatalf("遇风控后不得继续请求其余商品或备用接口: 主接口 %d 次，备用接口 %d 次", polish, backup)
		}
	})
}
