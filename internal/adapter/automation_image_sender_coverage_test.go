package adapter

import (
	"context"
	"errors"
	"testing"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/automation"
)

// coverageAutomationSender 是自动化图片发送测试使用的内存发送器。
type coverageAutomationSender struct {
	// text 保存最近一次文本发送参数，确认包装器没有改写文本。
	text string
	// imageURL 保存最近一次图片地址，确认上传结果被传给 WebSocket。
	imageURL string
	// width、height 保存图片尺寸透传结果。
	width, height int
	// cookie 保存运行时收到的 Cookie 更新。
	cookie string
	// sendErr 控制发送器返回的模拟错误。
	sendErr error
}

// SendText 记录自动化文本发送参数。

// SendText 记录自动化文本发送参数并返回模拟错误。
func (s *coverageAutomationSender) SendText(_ context.Context, _ string, _ string, text string) error {
	s.text = text
	return s.sendErr
}

// SendImage 记录自动化图片发送参数并返回模拟错误。
func (s *coverageAutomationSender) SendImage(_ context.Context, _ string, _ string, imageURL string, _ int64, width, height int) error {
	s.imageURL, s.width, s.height = imageURL, width, height
	return s.sendErr
}

// UpdateCookie 记录发送器收到的运行时凭证更新。
func (s *coverageAutomationSender) UpdateCookie(cookie string) {
	s.cookie = cookie
}

// coverageAutomationUploader 是自动化图片上传测试使用的内存上传器。
type coverageAutomationUploader struct {
	// upload 保存模拟平台上传结果。
	upload chatapp.ImageUpload
	// uploadErr 控制上传器返回的模拟错误。
	uploadErr error
}

// UploadChatImage 返回预设的应用层图片上传结果。
func (u coverageAutomationUploader) UploadChatImage(context.Context, string, string, string, []byte) (chatapp.ImageUpload, error) {
	return u.upload, u.uploadErr
}

// TestAutomationImageSenderMapsFailureAndSuccessPaths 验证图片自动化包装器的确定性错误和成功路径。
func TestAutomationImageSenderMapsFailureAndSuccessPaths(t *testing.T) {
	// ctx 是本测试图片发送共用的上下文。
	ctx := context.Background()
	// sender 保存记录图片转发结果的内存发送器。
	sender := &coverageAutomationSender{}
	// sendErr 保存发送器未初始化时的稳定错误。
	sendErr := (automationImageSender{}).SendText(ctx, "chat", "buyer", "你好")
	if !errors.Is(sendErr, automation.ErrMessageNotSent) {
		t.Fatalf("空文本发送错误=%v", sendErr)
	}
	// downloaderErr 保存远程图片读取失败时的包装错误。
	downloaderErr := automationImageSender{sender: sender, uploader: coverageAutomationUploader{}, downloader: func(context.Context, string) ([]byte, string, string, error) {
		return nil, "", "", errors.New("download failed")
	}}.SendImage(ctx, "chat", "buyer", "https://image.invalid/a.jpg", 1, 0, 0)
	if !errors.Is(downloaderErr, automation.ErrMessageNotSent) {
		t.Fatalf("下载失败错误=%v", downloaderErr)
	}
	// uploadErr 保存平台图片上传失败时的包装错误。
	uploadErr := automationImageSender{sender: sender, uploader: coverageAutomationUploader{uploadErr: errors.New("upload failed")}, downloader: func(context.Context, string) ([]byte, string, string, error) {
		return []byte("image"), "image/jpeg", "a.jpg", nil
	}}.SendImage(ctx, "chat", "buyer", "https://image.invalid/a.jpg", 1, 0, 0)
	if !errors.Is(uploadErr, automation.ErrMessageNotSent) {
		t.Fatalf("上传失败错误=%v", uploadErr)
	}
	// emptyUploadErr 保存平台上传成功但没有返回地址时的稳定错误。
	emptyUploadErr := automationImageSender{sender: sender, uploader: coverageAutomationUploader{}, downloader: func(context.Context, string) ([]byte, string, string, error) {
		return []byte("image"), "image/jpeg", "a.jpg", nil
	}}.SendImage(ctx, "chat", "buyer", "https://image.invalid/a.jpg", 1, 0, 0)
	if !errors.Is(emptyUploadErr, automation.ErrMessageNotSent) {
		t.Fatalf("空图片地址错误=%v", emptyUploadErr)
	}
	// successSender 保存成功路径使用的图片发送器。
	successSender := &coverageAutomationSender{}
	// sentErr 保存图片上传和 WebSocket 转发的最终结果。
	sentErr := automationImageSender{accountID: "cid", sender: successSender, uploader: coverageAutomationUploader{upload: chatapp.ImageUpload{URL: "https://cdn.invalid/a.jpg", Width: 640, Height: 480}}, downloader: func(context.Context, string) ([]byte, string, string, error) {
		return []byte("image"), "image/jpeg", "a.jpg", nil
	}}.SendImage(ctx, "chat", "buyer", "https://image.invalid/a.jpg", 1, 0, 0)
	if sentErr != nil || successSender.imageURL != "https://cdn.invalid/a.jpg" || successSender.width != 640 || successSender.height != 480 {
		t.Fatalf("图片成功转发异常 sender=%+v err=%v", successSender, sentErr)
	}
	// readySender 保存没有可选就绪接口的发送器，兼容包装器应默认视为就绪。
	readySender := automationImageSender{sender: successSender}
	if !readySender.AutomationReady() {
		t.Fatal("未实现就绪接口的发送器不应被阻止")
	}
	// nilReady 保存没有底层发送器的包装器。
	nilReady := automationImageSender{}
	if nilReady.AutomationReady() {
		t.Fatal("空发送器不应报告已就绪")
	}
	// wrappedSender 保存通过自动化包装器执行 Cookie 更新的发送器。
	wrappedSender := automationImageSender{sender: successSender}
	wrappedSender.UpdateCookie("updated-cookie")
	if successSender.cookie != "updated-cookie" {
		t.Fatal("Cookie 更新未透传")
	}
}

// TestAutomationImageSenderProviderRejectsIncompleteState 验证图片发送器来源在依赖不完整时返回不可发送状态。
func TestAutomationImageSenderProviderRejectsIncompleteState(t *testing.T) {
	// provider 保存零值图片发送器来源。
	provider := automationImageSenderProvider{}
	// sender、ok 保存依赖缺失时的发送器查找结果。
	sender, ok := provider.Sender("cid")
	if sender != nil || ok {
		t.Fatalf("不完整图片发送器来源返回 sender=%v ok=%v", sender, ok)
	}
}
