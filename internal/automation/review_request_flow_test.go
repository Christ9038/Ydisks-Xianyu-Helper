package automation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/llmclient"
	"xianyu-go/internal/llmclient/llmsim"
	"xianyu-go/internal/xianyu/mtop/mtopsim"
)

// 本文件是“求评价三段消息（引导语 → 好评文案 → 复制提示）”的完整功能测试，覆盖整条真实链路：
//
//	平台模拟器（mtopsim）推进订单阶段并推送 WebSocket 系统消息
//	→ 自动化中心付款发货（真实 MTOP 客户端签名请求 → 确认发货）
//	→ 调度器扫描到期订单 → 规则预解析好评文案（模板池 / 经真实 llmclient 调用的 LLM 网关模拟器）
//	→ 在线发送器按顺序收到三条消息 → 订单求评价计数落库。
//
// 求评价本身只走 WebSocket 发消息、不访问 MTOP；因此每个用例都断言“扫描期间平台收到的 MTOP 请求数不变”，
// 守护 CLAUDE.md 的 MTOP 低频约束，并验证风控状态下求评价不会触碰平台接口。

const (
	// flowLLMKey 是 LLM 网关模拟器接受的测试密钥。
	flowLLMKey = "sk-flow-test"
	// flowLLMModel 是请求中声明的测试模型名。
	flowLLMModel = "flow-model"
	// flowItemTitle 是用例商品标题，也是唯一允许发给 AI 的商品信息。
	flowItemTitle = "视频会员月卡"
	// flowLeadText 是引导语文案。
	flowLeadText = "亲，商品满意的话麻烦给个评价～"
	// flowTailText 是复制提示文案。
	flowTailText = "好评模板我帮你生成好啦，直接复制上面的内容就行～"
	// flowCardText 是付款发货发出的卡密文本，用来证明它不会泄露给 AI。
	flowCardText = "卡号 SECRET-CARD-777"
)

// llmGatewayGenerator 通过真实 llmclient 调用 LLM 网关模拟器的好评文案生成器，行为对应生产 adapter 的实现：
// 只把商品标题和风格要求发给模型。真实 adapter 生成器的配置读取在 adapter 包另有测试。
type llmGatewayGenerator struct {
	// store 用于读取商品标题。
	store *db.Store
	// baseURL 是 LLM 网关模拟器地址。
	baseURL string
	// protocol 是 openai 或 anthropic。
	protocol string
}

// Generate 向 LLM 网关请求一条好评文案；拒绝回答视为失败。
func (g llmGatewayGenerator) Generate(ctx context.Context, prompt ReviewCopyPrompt) (string, error) {
	// title 是商品标题，读取失败时退化为通用说法。
	title := "这件商品"
	// item 是本地商品记录；itemErr 是读取失败原因，失败时退化为通用说法。
	if item, itemErr := g.store.Items.Get(ctx, prompt.AccountID, prompt.ItemID); itemErr == nil && item != nil && item.ItemTitle != "" {
		title = item.ItemTitle
	}
	// result 是模型响应；err 是调用错误。
	result, err := llmclient.Complete(ctx, llmclient.Endpoint{Protocol: g.protocol, BaseURL: g.baseURL, APIKey: flowLLMKey, Model: flowLLMModel, HTTPClient: http.DefaultClient},
		llmclient.Request{System: "风格=" + prompt.Style + "；补充=" + prompt.Extra, UserText: "商品：《" + title + "》", MaxTokens: 400})
	if err != nil {
		return "", err
	}
	if result.Refused {
		return "", errors.New("模型拒绝生成")
	}
	return result.Text, nil
}

// reviewFlow 在平台流程环境上叠加 LLM 网关模拟器与调度器。
type reviewFlow struct {
	// platformFlow 提供平台模拟器、存储、自动化中心和在线发送器。
	*platformFlow
	// llm 是 LLM 网关模拟器。
	llm *llmsim.Gateway
	// scheduler 是被测调度器，扫描到期的求评价订单和延迟任务。
	scheduler *Scheduler
}

// newReviewFlow 创建带 LLM 网关的求评价流程环境，protocol 指定 AI 协议。
func newReviewFlow(t *testing.T, protocol string) *reviewFlow {
	t.Helper()
	// llm 是本用例独立的 LLM 网关。
	llm := llmsim.New(t)
	// base 是装配了 AI 生成器的平台流程环境。
	base := newPlatformFlow(t, func(dependencies *CenterDependencies, store *db.Store) {
		dependencies.ReviewCopyGenerator = llmGatewayGenerator{store: store, baseURL: llm.URL(), protocol: protocol}
	})
	return &reviewFlow{platformFlow: base, llm: llm, scheduler: NewScheduler(base.center)}
}

// addReviewRule 为商品创建启用的求评价规则。
func (f *reviewFlow) addReviewRule(itemID, configJSON string, actions ...db.AutomationActionInput) {
	f.t.Helper()
	// index 是动作位置，用于补全顺序与启用标记。
	for index := range actions {
		actions[index].Enabled = true
		actions[index].SortOrder = index + 1
	}
	// ruleErr 是求评价规则创建错误。
	if _, ruleErr := f.store.Automation.Create(f.ctx, db.AutomationRuleInput{
		UserID: f.adminID, CookieID: platformAccountID, ItemID: itemID, Name: "求评价流程规则", TriggerType: TriggerReviewMissingTimeout,
		Enabled: true, Priority: 100, ConfigJSON: configJSON, Actions: actions,
	}); ruleErr != nil {
		f.t.Fatalf("创建求评价规则失败: %v", ruleErr)
	}
}

// threeActions 返回“引导语 + 好评文案 + 复制提示”三个动作；copyConfig 是好评文案动作的 config_json。
func threeActions(copyConfig string) []db.AutomationActionInput {
	return []db.AutomationActionInput{
		{ActionType: ActionSendText, MessageTemplate: flowLeadText},
		{ActionType: ActionSendReviewCopy, ConfigJSON: copyConfig},
		{ActionType: ActionSendText, MessageTemplate: flowTailText},
	}
}

// deliverAndShip 让买家下单付款，经付款 WebSocket 触发自动发货并由平台确认发货，返回订单号。
func (f *reviewFlow) deliverAndShip(itemID string) string {
	f.t.Helper()
	// orderID 是买家已付款的订单。
	orderID := f.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})
	f.mustDeliver(f.gateway.PaidEvent(orderID), TriggerOrderPaid)
	if status := f.gateway.Order(orderID).Status; status != mtopsim.OrderShipped { // status 是平台侧订单状态。
		f.t.Fatalf("付款发货后平台订单应已发货，实际 %v", status)
	}
	return orderID
}

// backdate 把订单的时间列回拨 hours 小时，模拟等待期已过。
func (f *reviewFlow) backdate(orderID, column string, hours int) {
	f.t.Helper()
	// stamp 是回拨后的 UTC 时间文本。
	stamp := time.Now().UTC().Add(-time.Duration(hours) * time.Hour).Format("2006-01-02 15:04:05")
	f.exec("UPDATE orders SET "+column+"=? WHERE order_id=?", stamp, orderID)
}

// scan 触发一轮调度器扫描。
func (f *reviewFlow) scan() { f.scheduler.scan(f.ctx) }

// mtopCalls 返回平台模拟器累计收到的 MTOP 请求数。
func (f *reviewFlow) mtopCalls() int { return len(f.gateway.CallSequence()) }

// resetMessages 清空已记录的发送消息，使后续断言只关注求评价阶段。
func (f *reviewFlow) resetMessages() { f.sender.messages = nil }

// runStatus 返回订单求评价运行的状态，没有运行时返回空串。
func (f *reviewFlow) runStatus(orderID string) string {
	f.t.Helper()
	// status 是该订单最近一次求评价运行的状态。
	var status string
	_ = f.store.DB.QueryRowContext(f.ctx, `SELECT status FROM automation_runs WHERE order_id=? AND trigger_type=? ORDER BY id DESC LIMIT 1`, orderID, TriggerReviewMissingTimeout).Scan(&status)
	return status
}

// wantTexts 断言已发送消息正文与期望完全一致（含顺序）。
func (f *reviewFlow) wantTexts(want ...string) {
	f.t.Helper()
	if got := f.sender.texts(); strings.Join(got, "\n") != strings.Join(want, "\n") { // got 是实际发送的消息。
		f.t.Fatalf("消息不符:\n got=%q\nwant=%q", got, want)
	}
}

// reviewCount 返回本地订单已发送的求评价次数。
func (f *reviewFlow) reviewCount(orderID string) int { return f.localOrder(orderID).ReviewRequestCount }

// setupShippedOrder 创建商品、付款发货规则和求评价规则，让一笔订单完成发货并回拨发货时间，返回订单号。
func (f *reviewFlow) setupShippedOrder(configJSON string, actions []db.AutomationActionInput) string {
	f.t.Helper()
	// itemID 是商品；规则对该商品生效。
	itemID := f.addItem(flowItemTitle, false)
	f.addTextCard(31, flowCardText)
	f.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	f.addReviewRule(itemID, configJSON, actions...)
	// orderID 是已发货订单。
	orderID := f.deliverAndShip(itemID)
	f.backdate(orderID, "shipped_at", 2)
	f.resetMessages()
	return orderID
}

// TestReviewFlowThreeMessagesFullLifecycle 覆盖主流程：发货后到期 → 依次发出引导语、AI 好评文案、复制提示；
// 到期前不发；重复扫描不重发；第二次求评重新生成文案；达到上限后停止；扫描期间不产生任何 MTOP 请求；
// 发给 AI 的内容只含商品标题，不含买家、订单号和卡密。
func TestReviewFlowThreeMessagesFullLifecycle(t *testing.T) {
	// flow 是带 LLM 网关的求评价环境；两次生成分别返回不同文案。
	flow := newReviewFlow(t, llmclient.ProtocolOpenAI)
	flow.llm.Enqueue(llmsim.Step{Text: "“质量不错，发货很快，推荐购买！”"}, llmsim.Step{Text: "第二次：客服耐心，体验很好"})
	// orderID 是已发货且等待期已过的订单；最多求评两次、间隔 1 小时。
	orderID := flow.setupShippedOrder(`{"after_shipped_hours":1,"repeat_interval_hours":1,"max_attempts":2}`,
		threeActions(`{"source":"ai","ai_style":"warm","ai_min_chars":10,"ai_max_chars":40,"ai_prompt":"提到发货速度"}`))
	// mtopBefore 是扫描前平台已收到的 MTOP 请求数。
	mtopBefore := flow.mtopCalls()

	// 等待期未到：改回刚发货，不应发送，也不应调用 AI。
	flow.backdate(orderID, "shipped_at", 0)
	flow.scan()
	if len(flow.sender.messages) != 0 || flow.llm.Calls() != 0 {
		t.Fatalf("等待期未到不应发送也不应调用 AI: messages=%v llm=%d", flow.sender.texts(), flow.llm.Calls())
	}

	// 等待期已过：三条消息依次发出，AI 文案已清理引号。
	flow.backdate(orderID, "shipped_at", 2)
	flow.scan()
	flow.wantTexts(flowLeadText, "质量不错，发货很快，推荐购买！", flowTailText)
	if flow.reviewCount(orderID) != 1 || flow.runStatus(orderID) != "success" {
		t.Fatalf("首次求评后应计数 1 且运行成功: count=%d status=%s", flow.reviewCount(orderID), flow.runStatus(orderID))
	}
	// 三条消息都发往该订单的买家会话。
	for /* message 是发给买家的一条消息。 */ _, message := range flow.sender.messages {
		if message.chatID != flow.gateway.Order(orderID).ChatID || message.toUserID != platformBuyerID {
			t.Fatalf("消息应发往订单会话与买家: %+v", message)
		}
	}

	// 发给 AI 的请求：一次、带密钥与模型、只含商品标题，不含买家、订单号、卡密。
	requests := flow.llm.Requests()
	if len(requests) != 1 {
		t.Fatalf("首次求评只应调用一次 AI: %d", len(requests))
	}
	// request 是首次发给 AI 的请求。
	request := requests[0]
	if request.Authorization != "Bearer "+flowLLMKey || request.Model != flowLLMModel || !strings.Contains(request.UserText(), flowItemTitle) ||
		!strings.Contains(request.System, "warm") || !strings.Contains(request.System, "提到发货速度") {
		t.Fatalf("AI 请求内容不符: %+v", request)
	}
	for /* secret 是不允许出现在 AI 请求中的敏感信息。 */ _, secret := range []string{platformBuyerID, "测试买家", orderID, "SECRET-CARD", flowCardText} {
		if strings.Contains(request.Body, secret) {
			t.Fatalf("AI 请求不得包含 %q: %s", secret, request.Body)
		}
	}

	// 立即再扫：重复间隔未到，不得重发。
	flow.resetMessages()
	flow.scan()
	if len(flow.sender.messages) != 0 || flow.llm.Calls() != 1 {
		t.Fatalf("间隔未到不应重发: %v llm=%d", flow.sender.texts(), flow.llm.Calls())
	}

	// 间隔已过：第二次求评，重新生成新文案。
	flow.backdate(orderID, "last_review_request_at", 2)
	flow.scan()
	flow.wantTexts(flowLeadText, "第二次：客服耐心，体验很好", flowTailText)
	if flow.reviewCount(orderID) != 2 || flow.llm.Calls() != 2 {
		t.Fatalf("第二次求评后应计数 2 且共调用 AI 两次: count=%d llm=%d", flow.reviewCount(orderID), flow.llm.Calls())
	}

	// 达到最多次数：不再发送，也不再调用 AI。
	flow.resetMessages()
	flow.backdate(orderID, "last_review_request_at", 2)
	flow.scan()
	if len(flow.sender.messages) != 0 || flow.llm.Calls() != 2 {
		t.Fatalf("达到 max_attempts 后应停止: %v llm=%d", flow.sender.texts(), flow.llm.Calls())
	}
	// 求评价全程只走 WebSocket：平台没有收到任何新的 MTOP 请求。
	if flow.mtopCalls() != mtopBefore {
		t.Fatalf("求评价不得产生 MTOP 请求: before=%d after=%d %v", mtopBefore, flow.mtopCalls(), flow.gateway.CallSequence())
	}
}

// TestReviewFlowCompletedBasis 覆盖“确认收货后计时”：只有收到确认收货系统消息并写入 completed_at 后才求评价，
// 与发货时间无关；按发货计时的规则在同一订单上则已到期。
func TestReviewFlowCompletedBasis(t *testing.T) {
	// flow 是求评价环境；好评文案来自固定模板池，不需要 AI。
	flow := newReviewFlow(t, llmclient.ProtocolOpenAI)
	// orderID 是已发货订单；规则按确认收货计时。
	orderID := flow.setupShippedOrder(`{"basis":"completed","after_shipped_hours":1,"max_attempts":1}`,
		threeActions(`{"source":"pool","pool":["收到货了，很满意"],"random":true}`))
	// 已发货但买家尚未确认收货：即使发货已超过等待期也不求评价。
	flow.scan()
	if len(flow.sender.messages) != 0 {
		t.Fatalf("尚未确认收货不应求评价: %v", flow.sender.texts())
	}

	// 买家确认收货：平台推进订单并推送系统消息，本地写入 completed_at。
	flow.gateway.ConfirmReceipt(orderID)
	flow.mustDeliver(flow.gateway.CompletedEvent(orderID), TriggerOrderCompleted)
	if flow.localOrder(orderID).CompletedAt == "" {
		t.Fatal("确认收货系统消息应写入 completed_at")
	}
	// mtopBefore 是确认收货系统消息处理完成后的 MTOP 请求数，其后的求评价扫描不得再增加。
	mtopBefore := flow.mtopCalls()

	// 刚确认收货：等待期未到。
	flow.scan()
	if len(flow.sender.messages) != 0 {
		t.Fatalf("刚确认收货不应求评价: %v", flow.sender.texts())
	}
	// 确认收货已超过等待期：发送三条消息，AI 没有被调用。
	flow.backdate(orderID, "completed_at", 2)
	flow.scan()
	flow.wantTexts(flowLeadText, "收到货了，很满意", flowTailText)
	if flow.llm.Calls() != 0 || flow.reviewCount(orderID) != 1 {
		t.Fatalf("模板池来源不应调用 AI: llm=%d count=%d", flow.llm.Calls(), flow.reviewCount(orderID))
	}
	if flow.mtopCalls() != mtopBefore {
		t.Fatalf("求评价不得产生 MTOP 请求: %v", flow.gateway.CallSequence())
	}
}

// TestReviewFlowShippedBasisIgnoresCompletedOnlyOrders 验证按发货计时的规则不会处理“已确认收货但并非系统发货”的订单，
// 守护为支持确认收货计时而放宽的订单查询没有改变历史规则的行为。
func TestReviewFlowShippedBasisIgnoresCompletedOnlyOrders(t *testing.T) {
	// flow 是求评价环境，规则按发货计时。
	flow := newReviewFlow(t, llmclient.ProtocolOpenAI)
	// itemID 是商品；规则只含一条引导语（历史单文本规则形态）。
	itemID := flow.addItem(flowItemTitle, false)
	flow.addReviewRule(itemID, `{"after_shipped_hours":1,"max_attempts":1}`, db.AutomationActionInput{ActionType: ActionSendText, MessageTemplate: flowLeadText})
	// orderID 是买家在别处发货、本系统没有发货记录但已确认收货的订单。
	orderID := flow.placePaidOrder(mtopsim.OrderInput{ItemID: itemID})
	flow.exec(`INSERT INTO orders (order_id,cookie_id,item_id,buyer_id,chat_id,system_shipped,completed_at) VALUES (?,?,?,?,?,0,?)`,
		"manual-"+orderID, platformAccountID, itemID, platformBuyerID, "chat-manual", time.Now().UTC().Add(-48*time.Hour).Format("2006-01-02 15:04:05"))
	flow.scan()
	if len(flow.sender.messages) != 0 {
		t.Fatalf("按发货计时的规则不应处理非系统发货订单: %v", flow.sender.texts())
	}
}

// TestReviewFlowLLMOutageThenRecovery 覆盖 AI 故障：第一条消息发出之前文案生成失败，买家不会收到半套消息；
// 运行进入失败重试；网关恢复后重试成功，且整套消息只发送一次。
func TestReviewFlowLLMOutageThenRecovery(t *testing.T) {
	// flow 是求评价环境；网关先持续返回 503。
	flow := newReviewFlow(t, llmclient.ProtocolOpenAI)
	flow.llm.SetFallback(llmsim.Step{Status: http.StatusServiceUnavailable})
	// orderID 是已发货订单；好评文案只来自 AI，无模板池兜底。
	orderID := flow.setupShippedOrder(`{"after_shipped_hours":1,"max_attempts":1}`, threeActions(`{"source":"ai"}`))
	// mtopBefore 是扫描前的 MTOP 请求数。
	mtopBefore := flow.mtopCalls()

	flow.scan()
	if len(flow.sender.messages) != 0 {
		t.Fatalf("AI 故障时买家不得收到任何消息（含引导语）: %v", flow.sender.texts())
	}
	if flow.reviewCount(orderID) != 0 || flow.runStatus(orderID) != "failed" {
		t.Fatalf("应记为失败待重试且不计求评次数: count=%d status=%s", flow.reviewCount(orderID), flow.runStatus(orderID))
	}
	// attemptsDuringOutage 是故障期间的 AI 调用次数。
	attemptsDuringOutage := flow.llm.Calls()
	if attemptsDuringOutage != 1 {
		t.Fatalf("单次扫描只应调用一次 AI: %d", attemptsDuringOutage)
	}

	// 网关恢复，重试窗口到达：恢复任务重新执行，整套消息发出一次。
	flow.llm.SetFallback(llmsim.Step{Text: "恢复后的好评文案"})
	flow.exec(`UPDATE automation_runs SET next_retry_at=0 WHERE order_id=? AND trigger_type=?`, orderID, TriggerReviewMissingTimeout)
	flow.scan()
	flow.wantTexts(flowLeadText, "恢复后的好评文案", flowTailText)
	if flow.reviewCount(orderID) != 1 || flow.runStatus(orderID) != "success" {
		t.Fatalf("恢复后应成功并计数 1: count=%d status=%s", flow.reviewCount(orderID), flow.runStatus(orderID))
	}
	if flow.llm.Calls() != attemptsDuringOutage+1 {
		t.Fatalf("恢复只应再调用一次 AI: %d", flow.llm.Calls())
	}
	if flow.mtopCalls() != mtopBefore {
		t.Fatalf("故障与恢复期间不得产生 MTOP 请求: %v", flow.gateway.CallSequence())
	}
	// 再扫一轮不会重复发送。
	flow.resetMessages()
	flow.scan()
	if len(flow.sender.messages) != 0 {
		t.Fatalf("成功后不应重复发送: %v", flow.sender.texts())
	}
}

// TestReviewFlowLLMBadAnswersSendNothing 覆盖 AI 返回拒绝回答、空内容、仅引号内容，以及 Anthropic 协议的同类故障：
// 全部按生成失败处理，买家不收到任何消息。
func TestReviewFlowLLMBadAnswersSendNothing(t *testing.T) {
	// cases 是各类异常响应及其协议。
	cases := []struct {
		name     string
		protocol string
		step     llmsim.Step
	}{
		{"OpenAI空内容", llmclient.ProtocolOpenAI, llmsim.Step{Text: "   "}},
		{"OpenAI仅引号", llmclient.ProtocolOpenAI, llmsim.Step{Text: "“”"}},
		{"OpenAI限流", llmclient.ProtocolOpenAI, llmsim.Step{Status: http.StatusTooManyRequests}},
		{"Anthropic拒绝回答", llmclient.ProtocolAnthropic, llmsim.Step{Refuse: true}},
		{"Anthropic空内容", llmclient.ProtocolAnthropic, llmsim.Step{Text: ""}},
		{"Anthropic服务错误", llmclient.ProtocolAnthropic, llmsim.Step{Status: http.StatusInternalServerError}},
	}
	for /* tc 是一个异常响应场景。 */ _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// flow 是该协议的求评价环境，网关总是返回异常响应。
			flow := newReviewFlow(t, tc.protocol)
			flow.llm.SetFallback(tc.step)
			// orderID 是已发货订单。
			orderID := flow.setupShippedOrder(`{"after_shipped_hours":1,"max_attempts":1}`, threeActions(`{"source":"ai"}`))
			flow.scan()
			if len(flow.sender.messages) != 0 || flow.reviewCount(orderID) != 0 {
				t.Fatalf("生成失败不得发送任何消息: %v count=%d", flow.sender.texts(), flow.reviewCount(orderID))
			}
		})
	}
}

// TestReviewFlowAnthropicProtocol 验证 Anthropic 协议同样走通完整链路，并使用 x-api-key 鉴权。
func TestReviewFlowAnthropicProtocol(t *testing.T) {
	// flow 是使用 Anthropic 协议的求评价环境。
	flow := newReviewFlow(t, llmclient.ProtocolAnthropic)
	flow.llm.Enqueue(llmsim.Step{Text: "Anthropic 生成的好评"})
	// orderID 是已发货订单。
	orderID := flow.setupShippedOrder(`{"after_shipped_hours":1,"max_attempts":1}`, threeActions(`{"source":"ai"}`))
	flow.scan()
	flow.wantTexts(flowLeadText, "Anthropic 生成的好评", flowTailText)
	// requests 是网关记录的请求。
	requests := flow.llm.Requests()
	if len(requests) != 1 || requests[0].Protocol != "anthropic" || requests[0].APIKey != flowLLMKey || flow.reviewCount(orderID) != 1 {
		t.Fatalf("Anthropic 请求不符: %+v count=%d", requests, flow.reviewCount(orderID))
	}
}

// TestReviewFlowMixedFallsBackToPool 验证混合模式 AI 命中但网关故障时回退到模板池，买家仍收到完整三条。
func TestReviewFlowMixedFallsBackToPool(t *testing.T) {
	// flow 是求评价环境；AI 比例 100% 且网关故障。
	flow := newReviewFlow(t, llmclient.ProtocolOpenAI)
	flow.llm.SetFallback(llmsim.Step{Status: http.StatusBadGateway})
	// orderID 是已发货订单。
	orderID := flow.setupShippedOrder(`{"after_shipped_hours":1,"max_attempts":1}`,
		threeActions(`{"source":"mixed","ai_ratio":100,"pool":["兜底好评"],"random":true}`))
	flow.scan()
	flow.wantTexts(flowLeadText, "兜底好评", flowTailText)
	if flow.llm.Calls() != 1 || flow.reviewCount(orderID) != 1 {
		t.Fatalf("应先尝试 AI 一次再回退: llm=%d count=%d", flow.llm.Calls(), flow.reviewCount(orderID))
	}
}

// TestReviewFlowMixedRatioZeroNeverCallsAI 验证混合模式比例为 0 时只用模板池，不访问 AI 网关。
func TestReviewFlowMixedRatioZeroNeverCallsAI(t *testing.T) {
	// flow 是求评价环境。
	flow := newReviewFlow(t, llmclient.ProtocolOpenAI)
	flow.setupShippedOrder(`{"after_shipped_hours":1,"max_attempts":1}`,
		threeActions(`{"source":"mixed","ai_ratio":0,"pool":["只用模板"],"random":true}`))
	flow.scan()
	flow.wantTexts(flowLeadText, "只用模板", flowTailText)
	if flow.llm.Calls() != 0 {
		t.Fatalf("比例为 0 不应调用 AI: %d", flow.llm.Calls())
	}
}

// TestReviewFlowPoolRotatesInOrderAcrossOrders 验证关闭随机后，模板池按该规则的运行顺序循环：三笔订单依次得到 A、B、A。
func TestReviewFlowPoolRotatesInOrderAcrossOrders(t *testing.T) {
	// flow 是求评价环境；一条账号级规则覆盖三笔订单。
	flow := newReviewFlow(t, llmclient.ProtocolOpenAI)
	// itemID 是商品。
	itemID := flow.addItem(flowItemTitle, false)
	flow.addTextCard(31, flowCardText)
	flow.addRule(itemID, TriggerOrderPaid, db.AutomationActionInput{ActionType: ActionSendCard, CardID: 31, DeliveryCount: 1})
	flow.addReviewRule(itemID, `{"after_shipped_hours":1,"max_attempts":1}`, threeActions(`{"source":"pool","pool":["模板A","模板B"],"random":false}`)...)
	// orders 是三笔已发货且到期的订单。
	var orders []string
	for range 3 {
		// orderID 是新订单；每笔订单发货卡密为同一固定文本卡。
		orderID := flow.deliverAndShip(itemID)
		flow.backdate(orderID, "shipped_at", 2)
		orders = append(orders, orderID)
	}
	flow.resetMessages()
	flow.scan()
	// copies 是三笔订单收到的好评文案（每笔订单的第二条消息），按订单号升序处理。
	var copies []string
	// index 是消息在发送序列中的位置，每个订单三条，第二条（下标取 3 的余数为 1）是好评文案；message 是一条已发送消息。
	for index, message := range flow.sender.messages {
		if index%3 == 1 {
			copies = append(copies, message.text)
		}
	}
	if len(flow.sender.messages) != 9 || strings.Join(copies, ",") != "模板A,模板B,模板A" {
		t.Fatalf("应按顺序循环 A,B,A: %v", flow.sender.texts())
	}
	for /* orderID 是本用例的订单。 */ _, orderID := range orders {
		if flow.reviewCount(orderID) != 1 {
			t.Fatalf("每笔订单应计数 1: %s", orderID)
		}
	}
}

// TestReviewFlowDelayedMessagesReuseSingleCopy 覆盖消息间隔：好评文案和复制提示带固定延迟与随机上浮，
// 通过持久化的延迟任务逐条发出；延迟落在 [最小, 最大] 区间内；整个过程只调用一次 AI（文案固定在任务快照里）。
func TestReviewFlowDelayedMessagesReuseSingleCopy(t *testing.T) {
	// flow 是求评价环境。
	flow := newReviewFlow(t, llmclient.ProtocolOpenAI)
	flow.llm.Enqueue(llmsim.Step{Text: "延迟发送的好评"})
	// actions 是三段消息：第二、三条各延迟 5 秒并随机上浮 0 到 4 秒。
	actions := []db.AutomationActionInput{
		{ActionType: ActionSendText, MessageTemplate: flowLeadText},
		{ActionType: ActionSendReviewCopy, DelaySeconds: 5, ConfigJSON: `{"source":"ai","delay_jitter_seconds":4}`},
		{ActionType: ActionSendText, MessageTemplate: flowTailText, DelaySeconds: 5, ConfigJSON: `{"delay_jitter_seconds":4}`},
	}
	// orderID 是已发货订单。
	orderID := flow.setupShippedOrder(`{"after_shipped_hours":1,"max_attempts":1}`, actions)
	// started 是扫描开始时间，用于检查延迟任务到期时间。
	started := time.Now().Unix()

	// 第一轮扫描：只发出引导语，好评文案已确定（AI 已调用一次），后续消息进入延迟任务。
	flow.scan()
	flow.wantTexts(flowLeadText)
	if flow.llm.Calls() != 1 {
		t.Fatalf("发出第一条消息前就应确定文案: llm=%d", flow.llm.Calls())
	}
	// dueAt 是延迟任务的到期时间，应在固定延迟加随机上浮的区间内。
	var dueAt int64
	if err := flow.store.DB.QueryRowContext(flow.ctx, `SELECT due_at FROM automation_pending_tasks WHERE status='pending'`).Scan(&dueAt); err != nil { // err 是读取延迟任务的错误。
		t.Fatalf("应存在一个待执行的延迟任务: %v", err)
	}
	if delay := dueAt - started; delay < 5 || delay > 10 { // delay 是延迟任务相对扫描开始的等待秒数。
		t.Fatalf("延迟应在 5 到 9 秒（含执行抖动）区间内: %d", delay)
	}
	// 延迟未到：再次领取不应发送。
	flow.scheduler.scanDeferredTasks(flow.ctx)
	flow.wantTexts(flowLeadText)

	// makeDue 把全部待执行延迟任务设为立即到期。
	makeDue := func() { flow.exec(`UPDATE automation_pending_tasks SET due_at=1 WHERE status='pending'`) }
	makeDue()
	flow.scheduler.scanDeferredTasks(flow.ctx)
	flow.wantTexts(flowLeadText, "延迟发送的好评")
	makeDue()
	flow.scheduler.scanDeferredTasks(flow.ctx)
	flow.wantTexts(flowLeadText, "延迟发送的好评", flowTailText)

	if flow.llm.Calls() != 1 {
		t.Fatalf("延迟恢复期间不得重新生成文案: llm=%d", flow.llm.Calls())
	}
	if flow.reviewCount(orderID) != 1 || flow.runStatus(orderID) != "success" {
		t.Fatalf("全部发出后应计数 1 且成功: count=%d status=%s", flow.reviewCount(orderID), flow.runStatus(orderID))
	}
}

// TestReviewFlowLegacySingleTextRule 验证历史单文本规则（没有好评文案动作）行为不变：只发一条，不调用 AI。
func TestReviewFlowLegacySingleTextRule(t *testing.T) {
	// flow 是求评价环境。
	flow := newReviewFlow(t, llmclient.ProtocolOpenAI)
	// orderID 是已发货订单。
	orderID := flow.setupShippedOrder(`{"after_shipped_hours":1,"max_attempts":1}`,
		[]db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: flowLeadText}})
	flow.scan()
	flow.wantTexts(flowLeadText)
	if flow.llm.Calls() != 0 || flow.reviewCount(orderID) != 1 {
		t.Fatalf("历史规则不应调用 AI: llm=%d count=%d", flow.llm.Calls(), flow.reviewCount(orderID))
	}
}

// TestReviewFlowBuyerReviewedStopsRequests 验证买家评价后（平台推送评价系统消息）不再求评价。
func TestReviewFlowBuyerReviewedStopsRequests(t *testing.T) {
	// flow 是求评价环境。
	flow := newReviewFlow(t, llmclient.ProtocolOpenAI)
	flow.llm.SetFallback(llmsim.Step{Text: "不该被使用"})
	// orderID 是已发货订单。
	orderID := flow.setupShippedOrder(`{"after_shipped_hours":1,"max_attempts":3}`, threeActions(`{"source":"ai"}`))
	flow.gateway.ConfirmReceipt(orderID)
	flow.gateway.BuyerRate(orderID)
	flow.mustDeliver(flow.gateway.BuyerReviewedEvent(orderID), TriggerBuyerReviewed)
	flow.resetMessages()
	flow.scan()
	if len(flow.sender.messages) != 0 || flow.llm.Calls() != 0 {
		t.Fatalf("买家已评价后不应求评价也不应调用 AI: %v llm=%d", flow.sender.texts(), flow.llm.Calls())
	}
}

// TestReviewFlowUnaffectedByPlatformRiskControl 验证平台对账号触发风控后求评价仍只走 WebSocket：
// 不触碰任何 MTOP 接口（避免延长风控），三条消息照常发出。
func TestReviewFlowUnaffectedByPlatformRiskControl(t *testing.T) {
	// flow 是求评价环境。
	flow := newReviewFlow(t, llmclient.ProtocolOpenAI)
	// orderID 是已发货订单；好评文案来自模板池。
	orderID := flow.setupShippedOrder(`{"after_shipped_hours":1,"max_attempts":1}`, threeActions(`{"source":"pool","pool":["风控期间的好评"]}`))
	flow.gateway.SetRiskControl(platformSellerID, true)
	// mtopBefore 是风控开启后的 MTOP 请求数。
	mtopBefore := flow.mtopCalls()
	flow.scan()
	flow.wantTexts(flowLeadText, "风控期间的好评", flowTailText)
	if flow.mtopCalls() != mtopBefore || flow.reviewCount(orderID) != 1 {
		t.Fatalf("风控期间求评价不得请求 MTOP: %v count=%d", flow.gateway.CallSequence(), flow.reviewCount(orderID))
	}
}

// TestReviewFlowRuleSnapshotRoundTrip 验证规则经数据库往返后动作顺序、类型和配置保持不变，
// 并且调度器读取的是持久化后的规则（防止序列化遗漏 send_review_copy 的配置）。
func TestReviewFlowRuleSnapshotRoundTrip(t *testing.T) {
	// flow 是求评价环境。
	flow := newReviewFlow(t, llmclient.ProtocolOpenAI)
	// itemID 是商品。
	itemID := flow.addItem(flowItemTitle, false)
	flow.addReviewRule(itemID, `{"basis":"completed","after_shipped_hours":3}`, threeActions(`{"source":"pool","pool":["往返"],"random":false}`)...)
	// rules 是按触发类型读取到的规则。
	rules, err := flow.store.Automation.Match(flow.ctx, platformAccountID, itemID, TriggerReviewMissingTimeout)
	if err != nil || len(rules) != 1 {
		t.Fatalf("应读到一条规则: %v %d", err, len(rules))
	}
	// kinds 是规则动作类型序列。
	var kinds []string
	for /* action 是规则的一个动作。 */ _, action := range rules[0].Actions {
		kinds = append(kinds, action.ActionType)
	}
	if strings.Join(kinds, ",") != "send_text,send_review_copy,send_text" {
		t.Fatalf("动作顺序不符: %v", kinds)
	}
	// config 是好评文案动作配置，应保持原样。
	var config map[string]any
	if json.Unmarshal([]byte(rules[0].Actions[1].ConfigJSON), &config) != nil || config["source"] != "pool" || config["random"] != false {
		t.Fatalf("好评文案配置丢失: %s", rules[0].Actions[1].ConfigJSON)
	}
	if parseReviewRuleConfig(rules[0].ConfigJSON).Basis != reviewBasisCompleted {
		t.Fatalf("规则 basis 丢失: %s", rules[0].ConfigJSON)
	}
}
