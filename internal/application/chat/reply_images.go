package chat

import (
	"context"
	"fmt"
	"strings"
)

// LocalImageLoader 把 accountID 账号素材目录内的 reference 相对路径读入内存。
// 返回图片字节、实际媒体类型、上传文件名与错误；实现负责沙箱、大小限制及取消，不长期缓存文件内容。
type LocalImageLoader func(ctx context.Context, accountID, reference string) (data []byte, contentType, filename string, err error)

// NewWithReplyImageSources 在首次暴露服务前固定两种图片来源；旧构造入口保持仅支持 URL 的兼容行为。
// repository、outgoing、senders、uploader、subscription、refresh 复用现有聊天端口，downloader 负责 URL，localLoader 负责账号本地图，identity 为可选身份解析端口。
func NewWithReplyImageSources(repository Repository, outgoing OutgoingRepository, senders SenderProvider, uploader ImageUploader, subscription SubscriptionProvider, refresh RefreshProvider, downloader ImageURLDownloader, localLoader LocalImageLoader, identity ...IdentityResolver) *Service {
	// service 尚未交给调用者，构造期写入端口不会与运行中请求并发。
	service := NewWithSendingSubscriptionAndRefreshAndDownloader(repository, outgoing, senders, uploader, subscription, refresh, downloader, identity...)
	service.localImageLoader = localLoader
	return service
}

// sendReplyImage 根据规范化的 input 选择加载来源，随后进入同一 SendImage 上传与消息确认链路。
// 本地加载发生在任何平台发送之前；失败返回确定性错误，不把路径当作网络 URL 重试。
func (s *Service) sendReplyImage(ctx context.Context, input ReplyInput) (*Message, error) {
	if input.ImagePath == "" {
		return s.SendImageURL(ctx, ImageURLInput{Session: input.Session, ImageURL: input.ImageURL})
	}
	if s.localImageLoader == nil || s.uploader == nil {
		return nil, ErrUnavailable
	}
	// data、contentType、filename 和 loadErr 是当前请求专用的图片内容与读取结果。
	data, contentType, filename, loadErr := s.localImageLoader(ctx, input.Session.AccountID, input.ImagePath)
	if loadErr != nil {
		return nil, fmt.Errorf("%w: %w", ErrSend, loadErr)
	}
	if len(data) == 0 || strings.TrimSpace(contentType) == "" {
		return nil, ErrSendInvalidInput
	}
	return s.SendImage(ctx, ImageInput{Session: input.Session, Filename: filename, ContentType: contentType, Data: data})
}
