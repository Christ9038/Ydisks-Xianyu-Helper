package automation

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/db"
)

// offlineSenderProvider 模拟账号当前没有在线消息发送器的运行状态。
type offlineSenderProvider struct{}

// Sender 返回明确的离线结论，供发送错误分类测试使用。
func (offlineSenderProvider) Sender(string) (MessageSender, bool) {
	return nil, false
}

// TestActionExecutorCardContentAndImageBranches 覆盖卡券类型转换和图片发送错误分类路径。
func TestActionExecutorCardContentAndImageBranches(t *testing.T) {
	// executor 保存不依赖外部平台的动作执行器。
	executor := automationActionExecutor{}
	// cardCases 保存支持与拒绝的卡券类型输入。
	cardCases := []struct {
		name      string
		card      *db.CardFull
		wantText  string
		wantImage string
		wantError bool
	}{
		{name: "text", card: &db.CardFull{Type: "text", TextContent: "文本"}, wantText: "文本"},
		{name: "image", card: &db.CardFull{Type: "image", ImageURL: "https://example.invalid/image.png"}, wantImage: "https://example.invalid/image.png"},
		{name: "empty text", card: &db.CardFull{Type: "text"}, wantError: true},
		{name: "empty image", card: &db.CardFull{Type: "image"}, wantError: true},
		{name: "data", card: &db.CardFull{Type: "data", DataContent: "A"}, wantError: true},
		{name: "api", card: &db.CardFull{Type: "api"}, wantError: true},
		{name: "unknown", card: &db.CardFull{Type: "other"}, wantError: true},
	}
	// cardCase 表示当前待判断的卡券类型场景。
	for _, cardCase := range cardCases {
		// text、image、contentErr 保存当前卡券转换结果和错误。
		text, image, contentErr := executor.cardContent(context.Background(), cardCase.card)
		if cardCase.wantError && contentErr == nil {
			t.Fatalf("%s should fail", cardCase.name)
		}
		if !cardCase.wantError && (contentErr != nil || text != cardCase.wantText || image != cardCase.wantImage) {
			t.Fatalf("%s content=(%q,%q) err=%v", cardCase.name, text, image, contentErr)
		}
	}
	// invalidTask 保存缺少聊天身份的图片动作输入。
	invalidTask := Task{AccountID: "cid"}
	// invalidErr 保存缺少聊天身份时的确定未发送错误。
	invalidErr := executor.sendImage(context.Background(), invalidTask, "https://example.invalid/image.png", 1)
	if !errors.Is(invalidErr, ErrMessageNotSent) {
		t.Fatalf("invalid image task error=%v", invalidErr)
	}
	// offlineExecutor 保存账号离线时的动作执行器。
	offlineExecutor := automationActionExecutor{senders: offlineSenderProvider{}}
	// offlineErr 保存账号离线时的确定未发送错误。
	offlineErr := offlineExecutor.sendImage(context.Background(), Task{AccountID: "cid", ChatID: "chat", BuyerID: "buyer"}, "https://example.invalid/image.png", 1)
	if !errors.Is(offlineErr, ErrMessageNotSent) {
		t.Fatalf("offline image error=%v", offlineErr)
	}
}

// TestAdjustPriceTransientBusyClassifierCoversStableMessages 验证改价暂时性繁忙错误只匹配明确的平台提示。
func TestAdjustPriceTransientBusyClassifierCoversStableMessages(t *testing.T) {
	// cases 保存错误文本及其是否允许重试的分类结果。
	cases := []struct {
		// name 是当前错误分类场景名称。
		name string
		// err 是待分类的基础错误。
		err error
		// want 表示是否属于可重试的暂时性繁忙。
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "known-code", err: errors.New("CANNOT_MODIFY_FEE"), want: true},
		{name: "retry-later", err: errors.New("请稍后重试"), want: true},
		{name: "try-again", err: errors.New("请稍后再试"), want: true},
		{name: "terminal", err: errors.New("订单已关闭"), want: false},
	}
	// testCase 表示当前待验证的改价错误分类场景。
	for _, testCase := range cases {
		// got 保存当前错误文本的暂时性繁忙分类结果。
		if got := isAdjustPriceTransientBusy(testCase.err); got != testCase.want {
			t.Errorf("%s classified=%v want=%v", testCase.name, got, testCase.want)
		}
	}
}
