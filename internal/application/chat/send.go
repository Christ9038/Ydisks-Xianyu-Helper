package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrUnavailable 表示聊天发送所需的持久化、运行时或图片上传端口未装配。
	ErrUnavailable = errors.New("聊天发送服务未启用")
	// ErrOffline 表示目标账号没有可用的在线发送实例。
	ErrOffline = errors.New("账号当前离线")
	// ErrSend 表示平台发送动作失败，消息状态已尽力标记为失败。
	ErrSend = errors.New("聊天消息发送失败")
	// ErrStatusSave 表示平台动作已成功，但本地发送状态没有保存成功。
	ErrStatusSave = errors.New("聊天发送状态保存失败")
	// ErrSendInvalidInput 表示发送用例缺少会话标识或消息内容不符合限制。
	ErrSendInvalidInput = errors.New("聊天发送参数无效")
)

// OutgoingInput 是发送文字消息的应用层输入，不携带 HTTP 或数据库模型。
type OutgoingInput struct {
	// Session 保存已完成账号归属校验的会话摘要，不包含登录凭证。
	Session Session
	// Text 保存待发送的文字内容，应用层会去除首尾空白并限制长度。
	Text string
}

// ImageInput 是发送图片消息的应用层输入，Data 只在当前请求生命周期内使用。
type ImageInput struct {
	// Session 保存已完成账号归属校验的会话摘要，不包含登录凭证。
	Session Session
	// Filename 保存上传文件名，供平台接口识别图片来源。
	Filename string
	// ContentType 保存 HTTP 已校验的图片 MIME 类型。
	ContentType string
	// Data 保存图片二进制内容，调用完成后不由服务长期持有。
	Data []byte
}

// ImageUpload 是图片平台适配器返回的非敏感结果。
type ImageUpload struct {
	// URL 保存可供聊天发送的图片地址。
	URL string
}

// OutgoingRepository 定义发送用例需要的本地消息写入能力。
type OutgoingRepository interface {
	// CreateOutgoing 创建状态为 sending 的文字消息并返回幂等键。
	CreateOutgoing(ctx context.Context, session Session, text string) (Message, error)
	// CreateOutgoingMedia 创建状态为 sending 的媒体消息并返回幂等键。
	CreateOutgoingMedia(ctx context.Context, session Session, messageType, content string) (Message, error)
	// SetOutgoingStatus 更新外发消息状态并返回最新消息。
	SetOutgoingStatus(ctx context.Context, accountID, key, status string) (Message, error)
}

// Sender 定义单个在线账号的最小聊天发送能力。
type Sender interface {
	// SendText 发送文本；messageKey 用于将平台旁路事件与待发送消息关联。
	SendText(ctx context.Context, chatID, toUserID, text, messageKey string) error
	// SendImage 发送图片；messageKey 用于将平台旁路事件与待发送消息关联。
	SendImage(ctx context.Context, chatID, toUserID, imageURL string, cardID int64, messageKey string) error
}

// SenderProvider 按账号标识解析当前在线发送实例。
type SenderProvider interface {
	// Sender 返回指定账号的发送能力；不存在时返回 false。
	Sender(accountID string) (Sender, bool)
}

// ImageUploader 定义图片上传所需的平台能力。
type ImageUploader interface {
	// UploadChatImage 按账号标识上传图片；凭证读取与刷新由平台适配器内部完成。
	UploadChatImage(ctx context.Context, accountID, filename, contentType string, data []byte) (ImageUpload, error)
}

// NewWithSending 创建同时支持历史查询和实时发送的聊天应用服务。
func NewWithSending(repository Repository, outgoing OutgoingRepository, senders SenderProvider, uploader ImageUploader) *Service {
	return &Service{
		repository: repository,
		outgoing:   outgoing,
		senders:    senders,
		uploader:   uploader,
	}
}

// SendText 创建并发送一条文字消息，失败时尽力保留本地 failed 状态。
func (s *Service) SendText(ctx context.Context, input OutgoingInput) (*Message, error) {
	// session 和 text 保存规范化后的会话及消息内容。
	session, text, err := normalizeOutgoingInput(input.Session, input.Text)
	if err != nil {
		return nil, err
	}
	if s == nil || s.outgoing == nil || s.senders == nil {
		return nil, ErrUnavailable
	}
	// sender 和 ok 保存目标账号的在线发送句柄及存在性。
	sender, ok := s.senders.Sender(session.AccountID)
	if !ok || sender == nil {
		return nil, ErrOffline
	}
	// message 和 err 保存本地待发送消息及持久化错误。
	message, err := s.outgoing.CreateOutgoing(ctx, session, text)
	if err != nil {
		return nil, fmt.Errorf("保存待发送消息失败: %w", err)
	}
	// sendErr 表示平台文字发送失败；失败分支会补写本地 failed 状态。
	if sendErr := sender.SendText(ctx, session.ChatID, session.BuyerID, text, message.MessageKey); sendErr != nil {
		// failed 保存平台发送失败后的本地状态；状态保存失败不覆盖原始发送错误。
		failed, _ := s.outgoing.SetOutgoingStatus(context.Background(), session.AccountID, message.MessageKey, "failed")
		return messagePointer(failed, message), fmt.Errorf("%w: %v", ErrSend, sendErr)
	}
	// sent 和 err 保存平台发送成功后的本地状态及状态持久化错误。
	sent, err := s.outgoing.SetOutgoingStatus(ctx, session.AccountID, message.MessageKey, "sent")
	if err != nil {
		return messagePointer(sent, message), fmt.Errorf("%w: %v", ErrStatusSave, err)
	}
	return messagePointer(sent, message), nil
}

// SendImage 上传并发送一条图片消息，失败时尽力保留本地 failed 状态。
func (s *Service) SendImage(ctx context.Context, input ImageInput) (*Message, error) {
	// session 保存规范化后的会话摘要。
	session, _, err := normalizeOutgoingInput(input.Session, "图片")
	if err != nil {
		return nil, err
	}
	if s == nil || s.outgoing == nil || s.senders == nil || s.uploader == nil {
		return nil, ErrUnavailable
	}
	if len(input.Data) == 0 {
		return nil, ErrSendInvalidInput
	}
	// sender 和 ok 保存目标账号的在线发送句柄及存在性。
	sender, ok := s.senders.Sender(session.AccountID)
	if !ok || sender == nil {
		return nil, ErrOffline
	}
	// upload 和 err 保存图片上传结果及平台错误。
	upload, err := s.uploader.UploadChatImage(ctx, session.AccountID, input.Filename, input.ContentType, input.Data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSend, err)
	}
	if strings.TrimSpace(upload.URL) == "" {
		return nil, fmt.Errorf("%w: 图片上传未返回地址", ErrSend)
	}
	// message 和 err 保存图片待发送消息及本地写入错误。
	message, err := s.outgoing.CreateOutgoingMedia(ctx, session, "image", upload.URL)
	if err != nil {
		return nil, fmt.Errorf("保存待发送图片失败: %w", err)
	}
	// sendErr 表示平台图片发送失败；失败分支会补写本地 failed 状态。
	if sendErr := sender.SendImage(ctx, session.ChatID, session.BuyerID, upload.URL, 0, message.MessageKey); sendErr != nil {
		// failed 保存图片发送失败后的本地状态。
		failed, _ := s.outgoing.SetOutgoingStatus(context.Background(), session.AccountID, message.MessageKey, "failed")
		return messagePointer(failed, message), fmt.Errorf("%w: %v", ErrSend, sendErr)
	}
	// sent 和 err 保存图片发送成功后的本地状态及状态持久化错误。
	sent, err := s.outgoing.SetOutgoingStatus(ctx, session.AccountID, message.MessageKey, "sent")
	if err != nil {
		return messagePointer(sent, message), fmt.Errorf("%w: %v", ErrStatusSave, err)
	}
	return messagePointer(sent, message), nil
}

// normalizeOutgoingInput 校验发送会话并返回去除首尾空白的文字内容。
func normalizeOutgoingInput(session Session, text string) (Session, string, error) {
	session.AccountID = strings.TrimSpace(session.AccountID)
	session.ChatID = strings.TrimSpace(session.ChatID)
	session.BuyerID = strings.TrimSpace(session.BuyerID)
	text = strings.TrimSpace(text)
	if session.AccountID == "" || session.ChatID == "" || session.BuyerID == "" || text == "" || len([]rune(text)) > 2000 {
		return Session{}, "", ErrSendInvalidInput
	}
	return session, text, nil
}

// messagePointer 在状态更新返回空值时回退到已创建消息，确保错误响应仍能携带幂等键。
func messagePointer(message Message, fallback Message) *Message {
	if message.MessageKey == "" {
		message = fallback
	}
	return &message
}
