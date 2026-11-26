package adapter

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/llmclient"
	"xianyu-go/internal/llmclient/llmsim"
)

// 本文件用 LLM 网关模拟器验证“生产装配的好评文案生成器”：全局 AI 设置（地址、密钥、模型、协议）→ engine 配置读取 →
// 真实 llmclient 请求 → 网关；再经自动化中心完整执行一次求评价运行，证明 adapter 装配与自动化链路连通。

// reviewSender 是只记录消息的在线发送器替身。
type reviewSender struct {
	// mu 保护 sent：调度 goroutine 写入，测试 goroutine 读取。
	mu sync.Mutex
	// sent 按发送顺序保存消息正文。
	sent []string
}

// texts 返回已发送消息正文的副本。
func (s *reviewSender) texts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.sent...)
}

// SendText 记录一条文本消息。
func (s *reviewSender) SendText(_ context.Context, _, _, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, text)
	return nil
}

// SendImage 满足发送器接口；求评价不发送图片。
func (s *reviewSender) SendImage(context.Context, string, string, string, int64, int, int) error {
	return nil
}

// UpdateCookie 满足发送器接口。
func (s *reviewSender) UpdateCookie(string) {}

// reviewSenderProvider 让全部账号共用同一个发送器。
type reviewSenderProvider struct {
	// sender 是被共用的发送器替身。
	sender *reviewSender
}

// Sender 返回共用发送器，表示账号在线。
func (p reviewSenderProvider) Sender(string) (automation.MessageSender, bool) { return p.sender, true }

// configureAI 把全局 AI 设置指向 LLM 网关模拟器。
func configureAI(t *testing.T, store *db.Store, baseURL, protocol, apiKey string) {
	t.Helper()
	// ctx 是测试上下文。
	ctx := context.Background()
	for /* key 与 value 是一项全局 AI 设置。 */ key, value := range map[string]string{"ai_api_key": apiKey, "ai_api_url": baseURL, "ai_model": "review-model", "ai_api_protocol": protocol} {
		if err := store.Settings.Set(ctx, key, value); err != nil { // err 是设置写入错误。
			t.Fatal(err)
		}
	}
}

// seedItem 写入账号 cid 的一件商品，供生成器读取标题。
func seedItem(t *testing.T, store *db.Store, itemID, title string) {
	t.Helper()
	if _, err := store.DB.ExecContext(context.Background(), `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('cid',?,?)`, itemID, title); err != nil { // err 是商品写入错误。
		t.Fatal(err)
	}
}

// TestReviewCopyGeneratorAgainstLLMGateway 覆盖两种协议：配置读取、鉴权头、模型名、只含商品标题与风格要求的提示词，
// 以及未配置密钥、网关故障、拒绝回答时的错误。
func TestReviewCopyGeneratorAgainstLLMGateway(t *testing.T) {
	// ctx 是测试上下文。
	ctx := context.Background()
	for /* protocol 是被测协议。 */ _, protocol := range []string{llmclient.ProtocolOpenAI, llmclient.ProtocolAnthropic} {
		t.Run(protocol, func(t *testing.T) {
			// store、cleanup 是带 cid 账号的隔离数据库。
			store, cleanup := newAdapterTestStore(t)
			defer cleanup()
			// gateway 是 LLM 网关模拟器。
			gateway := llmsim.New(t)
			seedItem(t, store, "item-1", "视频会员月卡")
			// generator 是生产装配的生成器。
			generator := newReviewCopyGenerator(store)
			// prompt 是一次生成请求，附带风格、字数和附加要求。
			prompt := automation.ReviewCopyPrompt{AccountID: "cid", ItemID: "item-1", Style: "warm", MinChars: 10, MaxChars: 30, Extra: "提到包装"}

			// 未配置密钥：不发出请求。
			if _, err := generator.Generate(ctx, prompt); err == nil || gateway.Calls() != 0 { // err 是未配置时的错误。
				t.Fatalf("未配置密钥应失败且不请求网关: %v calls=%d", err, gateway.Calls())
			}

			configureAI(t, store, gateway.URL(), protocol, "sk-review")
			gateway.Enqueue(llmsim.Step{Text: "  包装完好，发货很快  "})
			// text 是生成器返回的去除空白后的文案；err 是生成错误。
			text, err := generator.Generate(ctx, prompt)
			if err != nil || text != "包装完好，发货很快" {
				t.Fatalf("应返回去除空白的文案: %q %v", text, err)
			}
			// request 是网关收到的请求。
			request := gateway.Requests()[0]
			if request.Model != "review-model" || request.Protocol != protocol || request.MaxTokens != reviewCopyMaxTokens {
				t.Fatalf("请求参数不符: %+v", request)
			}
			if protocol == llmclient.ProtocolOpenAI && request.Authorization != "Bearer sk-review" || protocol == llmclient.ProtocolAnthropic && request.APIKey != "sk-review" {
				t.Fatalf("鉴权头不符: %+v", request)
			}
			if !strings.Contains(request.UserText(), "视频会员月卡") || !strings.Contains(request.System, "热情洋溢") ||
				!strings.Contains(request.System, "10 到 30") || !strings.Contains(request.System, "提到包装") {
				t.Fatalf("提示词应含商品标题、风格、字数和附加要求: system=%q user=%q", request.System, request.UserText())
			}

			// 商品缺失时退化为通用说法而不是失败。
			gateway.Enqueue(llmsim.Step{Text: "好评"})
			if _, err = generator.Generate(ctx, automation.ReviewCopyPrompt{AccountID: "cid", ItemID: "missing"}); err != nil {
				t.Fatalf("商品缺失不应阻断生成: %v", err)
			}
			if last := gateway.Requests()[1]; !strings.Contains(last.UserText(), "这件商品") { // last 是商品缺失时的请求。
				t.Fatalf("商品缺失应使用通用说法: %q", last.UserText())
			}

			// 网关故障与拒绝回答都返回错误，不返回部分文本。
			gateway.Enqueue(llmsim.Step{Status: 500}, llmsim.Step{Refuse: true}, llmsim.Step{Text: " "})
			for /* name 是失败场景名称。 */ _, name := range []string{"500", "拒绝回答", "空内容"} {
				if got, genErr := generator.Generate(ctx, prompt); genErr == nil || got != "" { // got 与 genErr 是失败场景的结果。
					t.Fatalf("%s 应失败且不返回文本: %q %v", name, got, genErr)
				}
			}
		})
	}
}

// TestReviewRequestRunThroughProductionWiring 通过生产装配的生成器，让自动化中心完整执行一次求评价运行：
// 引导语 → AI 文案 → 复制提示依次发出，AI 网关只被调用一次且只收到商品标题。
func TestReviewRequestRunThroughProductionWiring(t *testing.T) {
	// ctx 是测试上下文。
	ctx := context.Background()
	// store、cleanup 是带 cid 账号的隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// admin 是账号所有者。
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// gateway 是 LLM 网关模拟器，全局 AI 设置指向它。
	gateway := llmsim.New(t)
	gateway.Enqueue(llmsim.Step{Text: "物流很快，客服耐心"})
	configureAI(t, store, gateway.URL(), llmclient.ProtocolOpenAI, "sk-wiring")
	seedItem(t, store, "item-wiring", "机械键盘")
	if _, err = store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "item-wiring", Name: "求评价", TriggerType: automation.TriggerReviewMissingTimeout, Enabled: true, Priority: 100,
		ConfigJSON: `{"after_shipped_hours":1,"max_attempts":1}`,
		Actions: []db.AutomationActionInput{
			{ActionType: automation.ActionSendText, MessageTemplate: "麻烦评价一下～", Enabled: true, SortOrder: 1},
			{ActionType: automation.ActionSendReviewCopy, ConfigJSON: `{"source":"ai","ai_max_chars":30}`, Enabled: true, SortOrder: 2},
			{ActionType: automation.ActionSendText, MessageTemplate: "直接复制上面的就行", Enabled: true, SortOrder: 3},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// shipped 是回拨两小时的发货时间。
	shipped := time.Now().UTC().Add(-2 * time.Hour).Format("2006-01-02 15:04:05")
	if _, err = store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,cookie_id,item_id,buyer_id,chat_id,system_shipped,shipped_at) VALUES ('order-wiring','cid','item-wiring','buyer-9','chat-9',1,?)`, shipped); err != nil {
		t.Fatal(err)
	}
	// sender 记录发出的消息。
	sender := &reviewSender{}
	// center 是注入生产生成器的自动化中心。
	center := automation.NewWithDependencies(store, reviewSenderProvider{sender: sender}, nil, automation.CenterDependencies{ReviewCopyGenerator: newReviewCopyGenerator(store)})
	// scheduler 扫描到期订单。
	scheduler := automation.NewScheduler(center)
	// runCtx 限制调度循环的运行时间；Run 启动即扫描一次。
	runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	go scheduler.Run(runCtx)
	// deadline 是等待消息发出的截止时间。
	deadline := time.Now().Add(2 * time.Second)
	for len(sender.texts()) < 3 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	scheduler.Wait()

	if strings.Join(sender.texts(), "|") != "麻烦评价一下～|物流很快，客服耐心|直接复制上面的就行" {
		t.Fatalf("三条消息应按顺序发出: %q", sender.texts())
	}
	// requests 是网关收到的请求。
	requests := gateway.Requests()
	if len(requests) != 1 || !strings.Contains(requests[0].UserText(), "机械键盘") ||
		strings.Contains(requests[0].Body, "buyer-9") || strings.Contains(requests[0].Body, "order-wiring") {
		t.Fatalf("AI 只应被调用一次且不含买家与订单信息: %+v", requests)
	}
}
