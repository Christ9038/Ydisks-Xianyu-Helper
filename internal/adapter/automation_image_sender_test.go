package adapter

import (
	"context"
	"errors"
	"testing"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/automation"
)

// automationImageSenderStub 记录包装器最终交给账号运行时的发送参数。
type automationImageSenderStub struct {
	// imageURL 保存最终写入 WebSocket 图片消息的平台地址。
	imageURL string
	// cardID 保存关联卡密组标识，确保包装不丢失自动化上下文。
	cardID int64
	// width 和 height 保存闲鱼上传返回的图片尺寸。
	width, height int
	// imageCalls 记录最终 WebSocket 图片发送次数。
	imageCalls int
}

// SendText 满足自动化发送契约；本测试只验证图片链路。
func (s *automationImageSenderStub) SendText(context.Context, string, string, string) error {
	return nil
}

// SendImage 记录平台图片地址和尺寸，模拟账号运行时的 WebSocket 发送。
func (s *automationImageSenderStub) SendImage(_ context.Context, _, _, imageURL string, cardID int64, width, height int) error {
	s.imageURL, s.cardID, s.width, s.height = imageURL, cardID, width, height
	s.imageCalls++
	return nil
}

// UpdateCookie 满足自动化发送契约；凭证写回由图片上传适配器自身负责。
func (s *automationImageSenderStub) UpdateCookie(string) {}

// automationImageUploaderStub 记录上传输入并返回可控的平台图片结果。
type automationImageUploaderStub struct {
	// accountID 保存上传时实际使用的发货账号。
	accountID string
	// filename 和 contentType 保存上传文件元数据。
	filename, contentType string
	// data 保存上传收到的图片字节。
	data []byte
	// result 和 err 分别控制上传成功结果与失败原因。
	result chatapp.ImageUpload
	err    error
}

// UploadChatImage 记录临时下载的图片并返回预设的闲鱼上传结果。
func (u *automationImageUploaderStub) UploadChatImage(_ context.Context, accountID, filename, contentType string, data []byte) (chatapp.ImageUpload, error) {
	u.accountID, u.filename, u.contentType, u.data = accountID, filename, contentType, data
	return u.result, u.err
}

// TestAutomationImageSenderUploadsURLContentBeforeSending 验证图片卡密 URL 先以内存形式下载并上传，再使用上传地址发送。
func TestAutomationImageSenderUploadsURLContentBeforeSending(t *testing.T) {
	// sourceData 是模拟从卡密 URL 下载到的图片字节。
	sourceData := []byte("image-content")
	// sender 是记录最终 WebSocket 调用的账号运行时替身。
	sender := &automationImageSenderStub{}
	// uploader 返回闲鱼上传后的地址和实际图片尺寸。
	uploader := &automationImageUploaderStub{result: chatapp.ImageUpload{URL: "https://cdn.goofish.example/card.png", Width: 1280, Height: 720}}
	// imageSender 是待验证的自动化图片发送包装器。
	imageSender := automationImageSender{
		accountID: "account-1", sender: sender, uploader: uploader,
		downloader: func(_ context.Context, rawURL string) ([]byte, string, string, error) {
			if rawURL != "https://origin.example/card.png" {
				return nil, "", "", errors.New("unexpected source URL")
			}
			return sourceData, "image/png", "card.png", nil
		},
	}
	// err 保存完整下载、上传和发送流程的结果。
	err := imageSender.SendImage(context.Background(), "chat-1", "buyer-1", "https://origin.example/card.png", 42, 0, 0)
	if err != nil {
		t.Fatalf("SendImage: %v", err)
	}
	if uploader.accountID != "account-1" || uploader.filename != "card.png" || uploader.contentType != "image/png" || string(uploader.data) != string(sourceData) {
		t.Fatalf("上传参数错误: account=%q filename=%q type=%q data=%q", uploader.accountID, uploader.filename, uploader.contentType, uploader.data)
	}
	if sender.imageCalls != 1 || sender.imageURL != uploader.result.URL || sender.cardID != 42 || sender.width != 1280 || sender.height != 720 {
		t.Fatalf("发送参数错误: calls=%d url=%q card=%d size=%dx%d", sender.imageCalls, sender.imageURL, sender.cardID, sender.width, sender.height)
	}
}

// TestAutomationImageSenderFailsBeforeWebSocketForPreparationErrors 验证下载、上传和取消失败都不会写入 WebSocket，并保持可安全重试的错误语义。
func TestAutomationImageSenderFailsBeforeWebSocketForPreparationErrors(t *testing.T) {
	// cases 覆盖图片发送前的确定性准备失败场景。
	cases := []struct {
		// name 是子测试名称。
		name string
		// downloader 控制远程图片读取结果。
		downloader automationImageDownloader
		// uploaderError 控制闲鱼图片上传失败。
		uploaderError error
	}{
		{
			name: "download-failure",
			downloader: func(context.Context, string) ([]byte, string, string, error) {
				return nil, "", "", errors.New("source unavailable")
			},
		},
		{
			name: "upload-failure",
			downloader: func(context.Context, string) ([]byte, string, string, error) {
				return []byte("image"), "image/png", "card.png", nil
			},
			uploaderError: errors.New("upload rejected"),
		},
		{
			name: "cancelled-download",
			downloader: func(ctx context.Context, _ string) ([]byte, string, string, error) {
				return nil, "", "", ctx.Err()
			},
		},
	}
	// testCase 表示当前准备失败场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// sender 是确保准备失败时不会调用 WebSocket 的替身。
			sender := &automationImageSenderStub{}
			// uploader 是返回当前场景上传结果的替身。
			uploader := &automationImageUploaderStub{err: testCase.uploaderError}
			// imageSender 是当前失败场景使用的图片发送包装器。
			imageSender := automationImageSender{accountID: "account-1", sender: sender, uploader: uploader, downloader: testCase.downloader}
			// ctx、cancel 为取消场景提供已经结束的上下文。
			ctx, cancel := context.WithCancel(context.Background())
			if testCase.name == "cancelled-download" {
				cancel()
			} else {
				defer cancel()
			}
			// err 保存准备阶段失败返回的自动化错误。
			err := imageSender.SendImage(ctx, "chat-1", "buyer-1", "https://origin.example/card.png", 1, 0, 0)
			if !errors.Is(err, automation.ErrMessageNotSent) || sender.imageCalls != 0 {
				t.Fatalf("准备失败必须安全重试且不得发送: calls=%d err=%v", sender.imageCalls, err)
			}
		})
	}
}

// TestDownloadAutomationImageRejectsNonHTTPURL 验证图片卡密下载入口拒绝非 HTTP(S) 地址，不创建本地或网络副作用。
func TestDownloadAutomationImageRejectsNonHTTPURL(t *testing.T) {
	// rawURL 表示当前非法图片来源地址。
	for _, rawURL := range []string{"", "file:///tmp/card.png", "ftp://example.com/card.png", "https://user:pass@example.com/card.png"} {
		// _, _, _, err 丢弃非法 URL 的空结果，只验证校验错误。
		if _, _, _, err := downloadAutomationImage(context.Background(), rawURL); err == nil {
			t.Fatalf("非法图片 URL 未被拒绝: %q", rawURL)
		}
	}
}
