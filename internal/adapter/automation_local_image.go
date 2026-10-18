package adapter

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/replyimage"
)

// PrepareLocalImage 用 ctx 约束读取和上传，reference 始终在发送器绑定的实际账号沙箱内解释。
// 返回已上传的平台引用供发送和加密快照复用；任何错误都发生在消息发送之前。
func (s automationImageSender) PrepareLocalImage(ctx context.Context, reference string) (automation.UploadedImage, error) {
	if s.sender == nil || s.localLoader == nil || s.uploader == nil {
		return automation.UploadedImage{}, fmt.Errorf("%w: 本地图片发送器未初始化", automation.ErrMessageNotSent)
	}
	if err := ctx.Err(); err != nil { // err 是调用方取消原因；取消时不访问本地文件。
		return automation.UploadedImage{}, fmt.Errorf("%w: %w", automation.ErrMessageNotSent, err)
	}
	if reference == "" {
		return automation.UploadedImage{}, fmt.Errorf("%w: 本地图片路径为空", automation.ErrMessageNotSent)
	}
	if err := replyimage.ValidatePath(reference); err != nil { // err 拒绝非法来源引用，不能把本地路径交给 URL 下载器。
		return automation.UploadedImage{}, fmt.Errorf("%w: %w", automation.ErrMessageNotSent, err)
	}
	// data、contentType、filename、loadErr 仅在本次准备中持有图片字节及元数据，读取后不缓存原文件。
	data, contentType, filename, loadErr := s.localLoader(ctx, s.accountID, reference)
	if loadErr != nil {
		return automation.UploadedImage{}, fmt.Errorf("%w: 读取本地发货图片失败: %w", automation.ErrMessageNotSent, loadErr)
	}
	if err := ctx.Err(); err != nil { // err 阻止已取消的读取继续上传。
		return automation.UploadedImage{}, fmt.Errorf("%w: %w", automation.ErrMessageNotSent, err)
	}
	return s.uploadPreparedImage(ctx, filename, contentType, data)
}

// PrepareImageURL 为新模板下载 rawURL 并上传，在发送前冻结平台地址，避免补发时来源变化。
// ctx 约束下载和上传生命周期；旧 URL 卡密仍使用原 SendImage，不改变其历史快照格式。
func (s automationImageSender) PrepareImageURL(ctx context.Context, rawURL string) (automation.UploadedImage, error) {
	if s.sender == nil || s.downloader == nil || s.uploader == nil {
		return automation.UploadedImage{}, fmt.Errorf("%w: 图片发送器未初始化", automation.ErrMessageNotSent)
	}
	if err := ctx.Err(); err != nil { // err 阻止已取消的模板继续下载图片。
		return automation.UploadedImage{}, fmt.Errorf("%w: %w", automation.ErrMessageNotSent, err)
	}
	// data、contentType、filename、downloadErr 保留一次性下载结果，不把来源地址写进新模板快照。
	data, contentType, filename, downloadErr := s.downloader(ctx, rawURL)
	if downloadErr != nil {
		return automation.UploadedImage{}, fmt.Errorf("%w: 下载模板图片失败: %w", automation.ErrMessageNotSent, downloadErr)
	}
	return s.uploadPreparedImage(ctx, filename, contentType, data)
}

// uploadPreparedImage 用 ctx 将 filename/contentType 描述的内存 data 上传为实际账号的平台图片。
// 两种新来源共用该准备阶段，失败保证尚未发送消息，返回值仅包含快照需要的地址和尺寸。
func (s automationImageSender) uploadPreparedImage(ctx context.Context, filename, contentType string, data []byte) (automation.UploadedImage, error) {
	if err := ctx.Err(); err != nil { // err 避免已取消的下载结果继续上传。
		return automation.UploadedImage{}, fmt.Errorf("%w: %w", automation.ErrMessageNotSent, err)
	}
	// uploaded、uploadErr 保存平台图片上传结果，实际发货账号不能由素材配置覆盖。
	uploaded, uploadErr := s.uploader.UploadChatImage(ctx, s.accountID, filename, contentType, data)
	if uploadErr != nil {
		return automation.UploadedImage{}, fmt.Errorf("%w: 上传发货图片失败: %w", automation.ErrMessageNotSent, uploadErr)
	}
	// image 只保存平台元数据，不保存可能随后替换或删除的来源引用。
	image := automation.UploadedImage{URL: uploaded.URL, Width: uploaded.Width, Height: uploaded.Height}
	if !validUploadedAutomationImage(image) {
		return automation.UploadedImage{}, fmt.Errorf("%w: 图片上传未返回有效地址或尺寸", automation.ErrMessageNotSent)
	}
	return image, nil
}

// SendUploadedImage 用 ctx 约束向 chatID/toUserID 的最终发送，cardID 保留卡密关联。
// image 来自已上传的加密快照，不进行本地读取、来源下载或二次上传；发送错误保留原不确定语义。
func (s automationImageSender) SendUploadedImage(ctx context.Context, chatID, toUserID string, image automation.UploadedImage, cardID int64) error {
	if s.sender == nil || !validUploadedAutomationImage(image) {
		return fmt.Errorf("%w: 已上传图片快照无效或发送器未初始化", automation.ErrMessageNotSent)
	}
	if err := ctx.Err(); err != nil { // err 在进入 WebSocket 之前保留确定未发送的取消类别。
		return fmt.Errorf("%w: %w", automation.ErrMessageNotSent, err)
	}
	return s.sender.SendImage(engine.WithOutgoingEchoConfirmation(ctx), chatID, toUserID, image.URL, cardID, image.Width, image.Height)
}

// validUploadedAutomationImage 检查 image 有可发送的平台 HTTP(S) 地址和正像素尺寸，拒绝损坏的准备结果或快照。
func validUploadedAutomationImage(image automation.UploadedImage) bool {
	// parsed、err 只解析平台引用，不发起请求或记录图片地址。
	parsed, err := url.Parse(image.URL)
	return err == nil && strings.TrimSpace(image.URL) == image.URL && parsed.Hostname() != "" && parsed.User == nil &&
		(parsed.Scheme == "http" || parsed.Scheme == "https") && image.Width > 0 && image.Height > 0
}

// 编译期检查图片包装器提供本地素材准备与平台快照直发能力。
var _ automation.PreparedImageSender = automationImageSender{}
