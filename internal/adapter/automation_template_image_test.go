package adapter

import (
	"context"
	"errors"
	"testing"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/automation"
)

// TestTemplateURLImagePreparationFreezesUpload 验证新模板 URL 首次下载上传后，来源失效也不影响同一平台快照直发。
func TestTemplateURLImagePreparationFreezesUpload(t *testing.T) {
	// downloads 用于验证重放不会再次抓取来源。
	downloads := 0
	// sender 记录平台直发收到的地址。
	sender := &automationImageSenderStub{}
	// uploader 以固定尺寸模拟平台上传结果。
	uploader := &automationImageUploaderStub{result: chatapp.ImageUpload{URL: "https://cdn.example/frozen.png", Width: 3, Height: 4}}
	// wrapper 模拟可访问一次的模板图片来源。
	wrapper := automationImageSender{accountID: "account", sender: sender, uploader: uploader,
		// rawURL 只能在首次准备时访问，平台快照不得送回此下载器。
		downloader: func(_ context.Context, rawURL string) ([]byte, string, string, error) {
			downloads++
			if rawURL != "https://origin.example/changed.png" || downloads > 1 {
				return nil, "", "", errors.New("来源已删除")
			}
			return []byte("original"), "image/png", "image.png", nil
		},
	}
	// image、err 保存来源首次上传后的平台引用。
	image, err := wrapper.PrepareImageURL(context.Background(), "https://origin.example/changed.png")
	if err != nil || image.URL != uploader.result.URL || sender.imageCalls != 0 {
		t.Fatalf("模板来源未先准备平台图片: %v", err)
	}
	wrapper.downloader = nil
	wrapper.uploader = nil
	for /* attempt 是首次发送及后续人工补发。 */ attempt := 0; attempt < 2; attempt++ {
		if sendErr := wrapper.SendUploadedImage(context.Background(), "chat", "buyer", image, 0); sendErr != nil { // sendErr 不应依赖来源下载器或上传器。
			t.Fatal(sendErr)
		}
	}
	if downloads != 1 || sender.imageURL != uploader.result.URL || sender.imageCalls != 2 {
		t.Fatal("URL 模板补发重新读取了来源")
	}
}

// TestTemplateImagePreparationGuards 验证 URL 新准备入口的依赖、取消及下载错误均不会发送消息。
func TestTemplateImagePreparationGuards(t *testing.T) {
	for /* name 描述当前需要拒绝的准备或快照状态。 */ _, name := range []string{"uninitialized", "cancelled", "cancelled-after-download", "download-failed", "upload-failed", "invalid-upload"} {
		t.Run(name, func(t *testing.T) {
			// ctx、cancel 为取消分支提供确定性生命周期。
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// sender 记录最终发送次数。
			sender := &automationImageSenderStub{}
			// uploader 默认提供有效结果，只有指定场景破坏该结果。
			uploader := &automationImageUploaderStub{result: chatapp.ImageUpload{URL: "https://cdn.example/a.png", Width: 2, Height: 2}}
			// wrapper 每个场景复用相同准备链路，只替换失败点。
			wrapper := automationImageSender{sender: sender, uploader: uploader,
				// 下载替身不发起网络请求，只在下载失败场景返回错误。
				downloader: func(context.Context, string) ([]byte, string, string, error) {
					if name == "download-failed" {
						return nil, "", "", errors.New("download failed")
					}
					if name == "cancelled-after-download" {
						cancel()
					}
					return []byte("data"), "image/png", "a.png", nil
				},
			}
			switch name {
			case "uninitialized":
				wrapper.uploader = nil
			case "cancelled":
				cancel()
			case "upload-failed":
				uploader.err = errors.New("upload failed")
			case "invalid-upload":
				uploader.result.URL = "file:///private.png"
			}
			if _, err := wrapper.PrepareImageURL(ctx, "https://origin.example/a.png"); !errors.Is(err, automation.ErrMessageNotSent) || sender.imageCalls != 0 { // err 在所有准备错误中必须保留确定未发送类别。
				t.Fatalf("URL 图片准备失败没有被保护: %v", err)
			}
		})
	}
}
