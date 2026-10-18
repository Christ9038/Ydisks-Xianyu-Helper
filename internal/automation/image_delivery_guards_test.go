package automation

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/db"
)

// fixedImageProvider 为离线和空发送器场景提供明确可用性，不创建账号运行时。
type fixedImageProvider struct {
	// sender 是本场景绑定的发送器，允许为 nil 以验证初始化保护。
	sender MessageSender
	// online 表示来源是否声明该账号在线。
	online bool
}

// Sender 返回夹具绑定的可用性与发送器，不执行账号查询。
func (p fixedImageProvider) Sender(string) (MessageSender, bool) { return p.sender, p.online }

// TestPreparedImageSenderRejectsMissingContext 验证缺少发送身份或账号能力时不会开始读取/上传。
func TestPreparedImageSenderRejectsMissingContext(t *testing.T) {
	// task 提供合法身份，以便每个场景仅破坏一个依赖。
	task := Task{AccountID: "account", ChatID: "chat", BuyerID: "buyer"}
	// sender 记录所有可能的准备调用。
	sender := &localDeliverySender{}
	for /* scenario 描述缺少身份、发送器或在线状态的场景。 */ _, scenario := range []string{"no-chat", "no-buyer", "no-provider", "offline", "nil-sender"} {
		t.Run(scenario, func(t *testing.T) {
			// input 是当前子测试专用身份副本。
			input := task
			// executor 默认在线，各场景只替换对应依赖。
			executor := automationActionExecutor{senders: fixedImageProvider{sender: sender, online: true}}
			switch scenario {
			case "no-chat":
				input.ChatID = ""
			case "no-buyer":
				input.BuyerID = ""
			case "no-provider":
				executor.senders = nil
			case "offline":
				executor.senders = fixedImageProvider{sender: sender}
			case "nil-sender":
				executor.senders = fixedImageProvider{online: true}
			}
			if _, err := executor.deliverImage(context.Background(), input, "", "code.png", 0); !errors.Is(err, ErrMessageNotSent) || len(sender.paths) != 0 { // err 必须在来源读取之前返回。
				t.Fatalf("缺少发送上下文仍准备图片: %v", err)
			}
		})
	}
}

// TestLegacyImageProofStillUsesURLSender 验证历史 image 快照无需新增准备能力，继续使用原 URL 通道。
func TestLegacyImageProofStillUsesURLSender(t *testing.T) {
	// sender 只有旧消息接口，不提供新的图片准备能力。
	sender := &testSender{}
	// executor 不要求历史快照升级为平台直发结构。
	executor := automationActionExecutor{senders: testSenderProvider{sender: sender}}
	if err := executor.sendDeliveryImage(context.Background(), Task{AccountID: "account", ChatID: "chat", BuyerID: "buyer"}, db.AutomationDeliveryMessage{Kind: "image", Content: "https://origin.example/old.png"}, 0); err != nil { // err 不应要求历史图片快照具备新增的准备能力。
		t.Fatalf("历史图片快照通道不兼容: %v", err)
	}
}
