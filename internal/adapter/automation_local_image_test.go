package adapter

import (
	"context"
	"errors"
	"testing"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/automation"
)

// TestAutomationLocalImagePreparationAndReplay 验证本地图按实际账号加载，补发仅使用上传结果，不再读取来源。
func TestAutomationLocalImagePreparationAndReplay(t *testing.T) {
	// sender 记录最终消息；uploader 返回该账号已上传图片的稳定引用。
	sender := &automationImageSenderStub{}
	// uploader 不进行真实平台调用，只返回图片元数据。
	uploader := &automationImageUploaderStub{result: chatapp.ImageUpload{URL: "https://cdn.example/uploaded.png", Width: 32, Height: 24}}
	// loads 用于确认准备与重复发送之间没有再次读取本地文件。
	loads := 0
	// wrapper 把账号和独立的本地图片加载器固定在同一发送上下文。
	wrapper := automationImageSender{accountID: "actual-account", sender: sender, uploader: uploader,
		// ctx 跟随调用取消，accountID 和 reference 必须保持账号隔离及相对路径语义。
		localLoader: func(ctx context.Context, accountID, reference string) ([]byte, string, string, error) {
			loads++
			if accountID != "actual-account" || reference != "delivery/code.png" || ctx.Err() != nil {
				t.Fatal("本地图片没有使用实际发货账号和相对路径")
			}
			return []byte("local-image"), "image/png", "code.png", nil
		},
		// 远程下载器不应该收到任何本地路径或已上传图片。
		downloader: func(context.Context, string) ([]byte, string, string, error) {
			t.Fatal("本地准备和快照重发不得下载 URL")
			return nil, "", "", nil
		},
	}
	// image、prepareErr 保存上传完成的图片和发送前错误；准备操作本身不得发送消息。
	image, prepareErr := wrapper.PrepareLocalImage(context.Background(), "delivery/code.png")
	if prepareErr != nil || sender.imageCalls != 0 || loads != 1 || uploader.accountID != "actual-account" || string(uploader.data) != "local-image" {
		t.Fatalf("本地准备失败或提前发送: %v", prepareErr)
	}
	// 使原文件不可用，验证后续重复发送只依赖冻结的平台引用。
	wrapper.localLoader = nil
	for /* attempt 是使用同一图片快照的发送轮次。 */ attempt := 0; attempt < 2; attempt++ {
		if err := wrapper.SendUploadedImage(context.Background(), "chat", "buyer", image, 17); err != nil { // err 是当前快照发送错误。
			t.Fatal(err)
		}
	}
	if sender.imageCalls != 2 || loads != 1 || sender.width != 32 || sender.height != 24 || sender.cardID != 17 || sender.imageURL != image.URL {
		t.Fatal("图片快照重发未保留地址、尺寸或卡密上下文")
	}
}

// TestAutomationLocalImagePreparationFailures 验证本地读取、上传、取消及初始化错误均为确定未发送。
func TestAutomationLocalImagePreparationFailures(t *testing.T) {
	// scenarios 覆盖所有不应进入最终消息发送器的准备失败。
	scenarios := []string{"loader-missing", "load-failed", "uploader-missing", "upload-failed", "empty-url", "cancelled", "invalid-path", "empty-path", "invalid-size", "cancelled-after-read"}
	for /* name 描述当前准备阶段失败。 */ _, name := range scenarios {
		t.Run(name, func(t *testing.T) {
			// ctx、cancel 控制当前准备阶段的生命周期。
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// sender 记录是否错误地进入了最终发送阶段。
			sender := &automationImageSenderStub{}
			// uploader 为成功基础场景返回有效的图片元数据。
			uploader := &automationImageUploaderStub{result: chatapp.ImageUpload{URL: "https://cdn.example/image.png", Width: 2, Height: 3}}
			// wrapper 在各场景中只破坏一个准备依赖。
			wrapper := automationImageSender{accountID: "account", sender: sender, uploader: uploader,
				// 读取替身用固定字节避免访问文件系统。
				localLoader: func(context.Context, string, string) ([]byte, string, string, error) {
					if name == "load-failed" {
						return nil, "", "", errors.New("missing")
					}
					if name == "cancelled-after-read" {
						cancel()
					}
					return []byte("image"), "image/png", "image.png", nil
				},
			}
			// reference 保持默认合法路径，仅在穿越场景替换。
			reference := "image.png"
			switch name {
			case "loader-missing":
				wrapper.localLoader = nil
			case "uploader-missing":
				wrapper.uploader = nil
			case "upload-failed":
				uploader.err = errors.New("upload failed")
			case "empty-url":
				uploader.result.URL = ""
			case "cancelled":
				cancel()
			case "empty-path":
				reference = ""
			case "invalid-path":
				reference = "../other/image.png"
			case "invalid-size":
				uploader.result.Width = 0
			}
			// image、err 验证失败没有产生可供补发的半成品。
			image, err := wrapper.PrepareLocalImage(ctx, reference)
			if !errors.Is(err, automation.ErrMessageNotSent) || sender.imageCalls != 0 || image.URL != "" {
				t.Fatalf("准备失败没有保持确定未发送: %v", err)
			}
		})
	}
}
