package automation

import "context"

// UploadedImage 保存已上传到平台的图片引用；不包含宿主路径、原始字节或账号凭证。
// 本地素材仅准备一次，发送及人工补发复用同一 URL 和像素尺寸。
type UploadedImage struct {
	// URL 是平台上传完成后的图片地址，不是待下载的来源地址。
	URL string
	// Width 和 Height 是实际图片的像素尺寸。
	Width, Height int
}

// PreparedImageSender 为新图片消息提供准备和平台直发能力；旧 URL 卡密发送器无需实现此接口。
// 准备失败保证尚未发送消息；最终发送沿用结果未知保护，不能自动重试。
type PreparedImageSender interface {
	// PrepareImageURL 用 ctx 下载并上传 rawURL，在新模板发送前冻结平台引用。
	PrepareImageURL(ctx context.Context, rawURL string) (UploadedImage, error)
	// PrepareLocalImage 按发送器绑定的实际账号读取 reference 并上传；ctx 管理读取和上传生命周期。
	PrepareLocalImage(ctx context.Context, reference string) (UploadedImage, error)
	// SendUploadedImage 将 image 直接发给 chatID/toUserID，关联 cardID；不再读取或上传来源。
	SendUploadedImage(ctx context.Context, chatID, toUserID string, image UploadedImage, cardID int64) error
}
