package automation

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/db"
)

// nilAutomationContext 返回用于覆盖自动化发送兼容 nil Context 分支的空上下文接口。
func nilAutomationContext() context.Context { return nil }

// TestActionExecutorHelperBranches 验证自动化动作执行器的金额、规格、数量和错误分类辅助逻辑。
func TestActionExecutorHelperBranches(t *testing.T) {
	// cents、centsErr 保存合法目标价格折算后的分值和错误。
	if cents, centsErr := adjustPriceCentsFromConfig(`{"target_price":"12.3"}`); centsErr != nil || cents != 1230 {
		t.Fatalf("金额解析结果=%d err=%v", cents, centsErr)
	}
	// invalidConfigErr 保存 JSON 格式错误的改价配置解析错误。
	if _, invalidConfigErr := adjustPriceCentsFromConfig("bad-json"); invalidConfigErr == nil {
		t.Fatal("错误改价配置应失败")
	}
	// invalidPriceErr 保存零金额配置触发的业务校验错误。
	if _, invalidPriceErr := adjustPriceCentsFromConfig(`{"target_price":"0"}`); invalidPriceErr == nil {
		t.Fatal("零金额改价配置应失败")
	}
	// invalidFractionErr 保存小数部分含非数字字符时的金额解析错误。
	if _, invalidFractionErr := parseYuanToCents("1.a"); invalidFractionErr == nil {
		t.Fatal("非法小数部分改价配置应失败")
	}
	if parsePositiveInt("") != 0 || parsePositiveInt("-1") != 0 || parsePositiveInt("bad") != 0 || parsePositiveInt("2") != 2 {
		t.Fatal("数量解析分支异常")
	}
	// quantityTask 保存有效订单数量，用于验证发货数量乘法。
	quantityTask := Task{Quantity: "2"}
	if deliverySendCount(quantityTask, db.AutomationAction{DeliveryCount: 3}) != 6 || deliverySendCount(Task{Quantity: "bad"}, db.AutomationAction{}) != 1 {
		t.Fatal("发货数量计算异常")
	}
	if appendTradeText("", " next ") != "next" || appendTradeText("first", "") != "first" || appendTradeText("first", "second") != "first\nsecond" {
		t.Fatal("发货文本拼接异常")
	}
	// matchingTask 保存规格匹配测试使用的订单事实。
	matchingTask := Task{SpecName: "套餐", SpecValue: "标准"}
	if actionMatchesOrderSpec(matchingTask, db.AutomationAction{ConfigJSON: "{}"}) || !actionMatchesOrderSpec(matchingTask, db.AutomationAction{ConfigJSON: `{"spec_name":"套餐","spec_value":"标准"}`}) || actionMatchesOrderSpec(matchingTask, db.AutomationAction{ConfigJSON: `{"spec_name":"套餐","spec_value":"高级"}`}) || actionMatchesOrderSpec(matchingTask, db.AutomationAction{ConfigJSON: "bad-json"}) {
		t.Fatal("动作规格匹配异常")
	}
	// multiSpecCases 覆盖多维 SKU 的完整匹配、维度顺序、部分匹配和空规格拒绝。
	multiSpecCases := []struct {
		name   string
		task   Task
		config string
		want   bool
	}{
		{name: "exact combination", task: Task{SpecName: "颜色；尺码", SpecValue: "红色；M"}, config: `{"spec_name":"颜色；尺码","spec_value":"红色；M"}`, want: true},
		{name: "different second dimension", task: Task{SpecName: "颜色；尺码", SpecValue: "红色；M"}, config: `{"spec_name":"颜色；尺码","spec_value":"红色；L"}`},
		{name: "different dimension order", task: Task{SpecName: "颜色；尺码", SpecValue: "红色；M"}, config: `{"spec_name":"尺码；颜色","spec_value":"M；红色"}`},
		{name: "only first dimension is not wildcard", task: Task{SpecName: "颜色；尺码", SpecValue: "红色；M"}, config: `{"spec_name":"颜色","spec_value":"红色"}`},
		{name: "empty filter rejected", task: Task{SpecName: "颜色；尺码", SpecValue: "红色；M"}, config: `{}`},
	}
	for /* tc 表示当前多 SKU 动作匹配测试场景。 */ _, tc := range multiSpecCases {
		t.Run(tc.name, func(t *testing.T) {
			// got 表示当前组合规格是否被动作配置精确匹配。
			if got := actionMatchesOrderSpec(tc.task, db.AutomationAction{ConfigJSON: tc.config}); got != tc.want {
				t.Fatalf("multi SKU match=%v want %v", got, tc.want)
			}
		})
	}
	// notSentErr 保存确定未发送错误，用于验证分类函数保持原错误。
	notSentErr := errors.New("not sent")
	if classifyMessageSendError(nil) != nil || classifyMessageSendError(notSentErr) == nil {
		t.Fatal("消息发送错误分类异常")
	}
	if !errors.Is(classifyMessageSendError(ErrMessageNotSent), ErrMessageNotSent) {
		t.Fatal("确定未发送标记未保留")
	}
	// unknownErr 保存可能已经抵达平台的未知发送错误。
	unknownErr := errors.New("unknown send result")
	// classifiedErr 保存未知发送错误的统一包装结果。
	classifiedErr := classifyMessageSendError(unknownErr)
	// uncertain 保存结果未知错误包装，验证可被 errors.As 识别。
	var uncertain *uncertainActionError
	if !errors.As(classifiedErr, &uncertain) || !errors.Is(classifiedErr, unknownErr) {
		t.Fatal("未知发送错误未标记为结果未知")
	}
}

// TestActionExecutorMessageSendBranches 验证文字和图片发送的成功、未装配及发送器错误分支。
func TestActionExecutorMessageSendBranches(t *testing.T) {
	// task 保存文字和图片发送共用的非敏感聊天身份。
	task := Task{AccountID: "account", ChatID: "chat", BuyerID: "buyer"}
	// successSender 保存记录成功消息的发送器替身。
	successSender := &testSender{}
	// successExecutor 保存完整装配发送器的动作执行器。
	successExecutor := automationActionExecutor{senders: testSenderProvider{sender: successSender}}
	// sendErr 保存文字发送成功路径的结果错误。
	if sendErr := successExecutor.sendText(nilAutomationContext(), task, "hello"); sendErr != nil {
		t.Fatalf("文字发送失败: %v", sendErr)
	}
	// imageErr 保存图片发送成功路径的结果错误。
	if imageErr := successExecutor.sendImage(nilAutomationContext(), task, "https://example.invalid/image.png", 3); imageErr != nil {
		t.Fatalf("图片发送失败: %v", imageErr)
	}
	// unavailableExecutor 保存未装配发送器的动作执行器。
	unavailableExecutor := automationActionExecutor{}
	// unavailableTextErr 保存未装配发送器的文字发送错误。
	if unavailableTextErr := unavailableExecutor.sendText(nilAutomationContext(), task, "hello"); !errors.Is(unavailableTextErr, ErrMessageNotSent) {
		t.Fatalf("未装配文字发送器错误=%v", unavailableTextErr)
	}
	// senderErr 是发送器需要返回的稳定错误。
	senderErr := errors.New("sender failed")
	// errorExecutor 保存发送器错误场景的动作执行器。
	errorExecutor := automationActionExecutor{senders: testSenderProvider{sender: &testSender{err: senderErr}}}
	// actualSenderErr 保存文字发送错误透传结果。
	if actualSenderErr := errorExecutor.sendText(nilAutomationContext(), task, "hello"); !errors.Is(actualSenderErr, senderErr) {
		t.Fatalf("文字发送错误未透传: %v", actualSenderErr)
	}
}
