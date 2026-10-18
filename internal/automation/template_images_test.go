package automation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"xianyu-go/internal/db"
	"xianyu-go/internal/deliverytemplate"
)

// TestImageTemplatePreservesOrderAndFrozenPlan 验证图文按冻结顺序发送，图片计为一个单位且不作为文本变量解析。
func TestImageTemplatePreservesOrderAndFrozenPlan(t *testing.T) {
	// events 收集同一发送器的跨类型顺序。
	events := []string{}
	// sender 记录文本与本地图片，无需真实平台。
	sender := &localDeliverySender{testSender: testSender{events: &events}}
	// executor 在无卡密变量时不需要数据库。
	executor := automationActionExecutor{senders: blockingSenderProvider{sender: sender}}
	// action 的旧文本投影故意不完整，验证完整图文计划优先。
	action := db.AutomationAction{Enabled: true, ActionType: ActionSendTemplate, ConfigJSON: "{}",
		TemplateMessages: []string{"旧文本投影不能执行"},
		TemplateMessageItems: []deliverytemplate.Message{
			{Type: "text", Content: "订单 {{order_id}}"},
			{Type: "image", ImagePath: "delivery/code.png"},
			{Type: "text", Content: "完成"},
		},
	}
	// task 携带原始订单及冻结动作计划。
	task := Task{AccountID: "account", ChatID: "chat", BuyerID: "buyer", OrderID: "order", ActionPlan: []db.AutomationAction{action}}
	// result、err 保存整个模板的发送顺序与计数。
	result, err := executor.sendTemplate(context.Background(), task, action)
	if err != nil || result.sent != 3 || result.proof.expectedUnits != 3 || result.proof.preparedUnits != 3 {
		t.Fatalf("图文模板执行计数错误: %v", err)
	}
	if !reflect.DeepEqual(events, []string{"send:订单 order", "image:https://cdn.example/frozen.png", "send:完成"}) {
		t.Fatalf("图文发送顺序错误: %v", events)
	}
	if len(result.proof.messages) != 3 || result.proof.messages[1].Kind != "uploaded_image" || result.proof.tradeText != "订单 order\n完成" {
		t.Fatal("图文消息没有保持快照顺序或混入文本凭证")
	}
	// raw、marshalErr 模拟重启后从原始任务 JSON 恢复图片计划。
	raw, marshalErr := json.Marshal(task)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	// run 只有冻结快照，不通过当前规则或模板反推计划。
	run := &db.AutomationRun{CookieID: "account", OrderID: "order", RawEventJSON: string(raw), DeliveryProof: db.AutomationDeliveryProof{ExpectedUnits: 3, PreparedUnits: 3}}
	if _, planErr := deliveryReplayPlan(run); planErr != nil { // planErr 验证新结构化消息不会被旧文本投影误计数。
		t.Fatal(planErr)
	}
	// 图片不允许冒充空渲染消息跳过。
	run.DeliveryProof.PreparedUnits = 2
	run.DeliveryProof.SkippedTemplateMessages = []db.AutomationDeliverySkip{{ActionIndex: 0, MessageIndex: 1}}
	if _, planErr := deliveryReplayPlan(run); planErr == nil { // planErr 应拒绝伪造的图片跳过证据。
		t.Fatal("图片消息被当作空模板跳过")
	}
}

// TestURLTemplateImageFreezesPlatformProof 验证新 URL 模板与本地图一样冻结上传结果，来源变化不影响快照补发。
func TestURLTemplateImageFreezesPlatformProof(t *testing.T) {
	// sender 记录来源下载和平台直发次数。
	sender := &localDeliverySender{}
	// executor 为纯图片模板注入两阶段图片端口。
	executor := automationActionExecutor{senders: blockingSenderProvider{sender: sender}}
	// task 和 action 不包含文本变量，图片来源不经过模板插值。
	task := Task{AccountID: "account", ChatID: "chat", BuyerID: "buyer"}
	// action 仅有一条 URL 图片消息。
	action := db.AutomationAction{ConfigJSON: "{}", TemplateMessageItems: []deliverytemplate.Message{{Type: "image", ImageURL: "https://origin.example/a.png"}}}
	// result、err 必须保存实际平台引用，而不是可随时改变的来源 URL。
	result, err := executor.sendTemplate(context.Background(), task, action)
	if err != nil || result.sent != 1 || len(sender.urls) != 1 || len(sender.paths) != 0 {
		t.Fatalf("URL 模板图片没有先上传: %v", err)
	}
	assertUploadedDeliveryProof(t, result.proof)
	sender.prepareErr = errors.New("来源已替换或删除")
	if replayErr := executor.sendDeliveryImage(context.Background(), task, result.proof.messages[0], 0); replayErr != nil { // replayErr 不得来自再次访问来源。
		t.Fatal(replayErr)
	}
	if len(sender.urls) != 1 || len(sender.images) != 2 || sender.images[0] != sender.images[1] {
		t.Fatal("新 URL 图片模板重放改变了实际图片")
	}
}

// TestImageTemplateStopsAfterPreparationOrSendFailure 验证图片失败不会继续后续文本，已有消息仍保留在凭证中。
func TestImageTemplateStopsAfterPreparationOrSendFailure(t *testing.T) {
	for /* uncertain 区分准备失败与最终发送结果未知。 */ _, uncertain := range []bool{false, true} {
		// sender 分别在准备或最终发送时失败。
		sender := &localDeliverySender{}
		if uncertain {
			sender.imageErr = errors.New("echo missing")
		} else {
			sender.prepareErr = errors.New("source missing")
		}
		// executor 使用图片替身保留原有的文本发送行为。
		executor := automationActionExecutor{senders: blockingSenderProvider{sender: sender}}
		// action 固定先文本后图片再文本，用于检查部分结果和停止位置。
		action := db.AutomationAction{ConfigJSON: "{}", TemplateMessageItems: []deliverytemplate.Message{{Type: "text", Content: "已发"}, {Type: "image", ImagePath: "code.png"}, {Type: "text", Content: "不能发送"}}}
		// result、err 在失败时必须保留第一条文本和总计划单位数。
		result, err := executor.sendTemplate(context.Background(), Task{AccountID: "account", ChatID: "chat", BuyerID: "buyer"}, action)
		if err == nil || result.sent != 1 || result.proof.expectedUnits != 3 || len(sender.texts) != 1 || sender.texts[0] != "已发" {
			t.Fatalf("图片失败后继续发送或丢失已发消息: %v", err)
		}
		if uncertain {
			assertUploadedDeliveryProof(t, result.reviewProof)
		} else if len(result.reviewProof.messages) != 0 {
			t.Fatal("本地读取失败不应生成未知图片凭证")
		}
	}
}

// TestImageTemplateRejectsInvalidFrozenMessages 验证损坏的冻结图文消息不会被当作文本或静默丢弃正文。
func TestImageTemplateRejectsInvalidFrozenMessages(t *testing.T) {
	// sender 记录任何不应发生的图片或文本发送。
	sender := &localDeliverySender{}
	// executor 不通过数据库修复损坏计划，必须直接拒绝。
	executor := automationActionExecutor{senders: blockingSenderProvider{sender: sender}}
	for /* message 覆盖未知类型、图片带正文、文本带图片三种矛盾快照。 */ _, message := range []deliverytemplate.Message{
		{Type: "invalid", Content: "不发送"},
		{Type: "image", Content: "不允许丢失", ImagePath: "code.png"},
		{Type: "text", Content: "不发送", ImagePath: "code.png"},
	} {
		// result、err 必须在任何素材读取或买家消息之前拒绝。
		result, err := executor.sendTemplate(context.Background(), Task{AccountID: "account", ChatID: "chat", BuyerID: "buyer"}, db.AutomationAction{ConfigJSON: "{}", TemplateMessageItems: []deliverytemplate.Message{message}})
		if !errors.Is(err, ErrMessageNotSent) || result.sent != 0 || len(sender.paths)+len(sender.images)+len(sender.texts) != 0 {
			t.Fatalf("损坏模板产生了发送副作用: %v", err)
		}
	}
}

// TestLocalImageCardUsesUploadedProof 验证数据库中的本地图片库存按购买数量发送，每份凭证只保存平台地址。
func TestLocalImageCardUsesUploadedProof(t *testing.T) {
	// store、cleanup 使用隔离 SQLite 验证路径往返与真实卡密读取。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 约束本地数据库和同步动作生命周期。
	ctx := context.Background()
	// owner、ownerErr 读取创建测试库存所需的本地用户。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// cardID、createErr 保存本地图片库存，不需要在测试机放置实际图片。
	cardID, createErr := store.Cards.Create(ctx, &db.CardFull{Name: "本地图片库存", Type: "image", ImagePath: "cards/code.png", Enabled: true, UserID: owner.ID})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// sender 只模拟沙箱准备，实际路径归属由 adapter 的测试覆盖。
	sender := &localDeliverySender{}
	// executor 通过真实仓储读取图片配置并将发送交给替身。
	executor := automationActionExecutor{store: store, senders: blockingSenderProvider{sender: sender}}
	// result、sendErr 验证购买数量乘发货份数仍按原业务规则计算。
	result, sendErr := executor.sendCardWithProof(ctx, Task{AccountID: "account", ChatID: "chat", BuyerID: "buyer", Quantity: "2"}, db.AutomationAction{CardID: cardID, ConfigJSON: "{}", DeliveryCount: 2})
	if sendErr != nil || result.sent != 4 || result.proof.expectedUnits != 4 || result.proof.preparedUnits != 4 || len(sender.paths) != 4 || len(result.proof.messages) != 4 {
		t.Fatalf("本地图片卡密数量或读取错误: %v", sendErr)
	}
	for /* message 是每份独立图片库存的冻结结果。 */ _, message := range result.proof.messages {
		if message.Kind != "uploaded_image" || message.Content != "https://cdn.example/frozen.png" {
			t.Fatal("本地路径泄漏到发送凭证")
		}
	}
}
