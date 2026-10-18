package adapter

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/automation"
)

// TestUploadedImageRejectsInvalidReplay 验证损坏快照、未初始化和取消在最终发送前被拒绝。
func TestUploadedImageRejectsInvalidReplay(t *testing.T) {
	for /* scenario 描述当前直发入口失败。 */ _, scenario := range []string{"no-sender", "invalid-image", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			// ctx、cancel 模拟由运行所有者取消的人工补发。
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// sender 记录不得发生的 WebSocket 写入。
			sender := &automationImageSenderStub{}
			// wrapper 不需要文件读取和上传依赖即可验证快照直发。
			wrapper := automationImageSender{sender: sender}
			// image 是有效的已上传图片元数据基础夹具。
			image := automation.UploadedImage{URL: "https://cdn.example/image.png", Width: 10, Height: 20}
			switch scenario {
			case "no-sender":
				wrapper.sender = nil
			case "invalid-image":
				image.Height = 0
			case "cancelled":
				cancel()
			}
			if err := wrapper.SendUploadedImage(ctx, "chat", "buyer", image, 0); !errors.Is(err, automation.ErrMessageNotSent) || sender.imageCalls != 0 { // err 必须说明尚未产生买家消息。
				t.Fatalf("非法或取消快照仍然发送: %v", err)
			}
		})
	}
}
