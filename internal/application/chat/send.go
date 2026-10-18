package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	// ErrUnavailable 表示聊天发送所需的持久化、运行时或图片上传端口未装配。
	ErrUnavailable = errors.New("聊天发送服务未启用")
	// ErrOffline 表示目标账号没有可用的在线发送实例。
	ErrOffline = errors.New("账号当前离线")
	// ErrSend 表示平台发送动作失败，消息状态已尽力标记为失败。
	ErrSend = errors.New("聊天消息发送失败")
	// ErrSendUncertain 表示请求可能已经到达平台，调用方不得自动重试。
	ErrSendUncertain = errors.New("聊天消息发送结果待确认")
	// ErrStatusSave 表示平台动作已成功，但本地发送状态没有保存成功。
	ErrStatusSave = errors.New("聊天发送状态保存失败")
	// ErrSendInvalidInput 表示发送用例缺少会话标识或消息内容不符合限制。
	ErrSendInvalidInput = errors.New("聊天发送参数无效")
)

const (
	// outgoingStatusSaveTimeout 限制平台动作完成后的本地状态收口时间，避免客户端断开后无限阻塞发送流程。
	outgoingStatusSaveTimeout = 5 * time.Second
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
	// Width 保存平台识别出的图片宽度，单位为像素；非正值表示使用协议默认尺寸。
	Width int
	// Height 保存平台识别出的图片高度，单位为像素；非正值表示使用协议默认尺寸。
	Height int
}

// ImageURLDownloader 定义把公网图片 URL 转换为消息页面图片输入的能力。
// 返回值依次为图片字节、媒体类型和上传文件名；下载实现不得持久化图片或泄露凭证。
type ImageURLDownloader func(ctx context.Context, imageURL string) (data []byte, contentType string, filename string, err error)

// ReplyInput 是一次完整聊天回复的应用层输入，图片先于文字发送。
type ReplyInput struct {
	// Session 保存已完成账号归属校验的会话摘要，不包含登录凭证。
	Session Session
	// Text 保存可选的文字回复内容。
	Text string
	// ImageURL 保存可选的来源图片地址；发送时会先下载并复用 SendImage 上传链路。
	ImageURL string
	// ImagePath 保存账号素材目录内的相对图片路径；与 ImageURL 互斥，仅用于默认回复。
	ImagePath string
}

// ReplyResult 是完整回复的分段投递结果，用于调用方恢复一次性回复状态。
type ReplyResult struct {
	// Image 保存图片分段的本地消息；图片未请求或尚未创建时为空。
	Image *Message
	// Text 保存文字分段的本地消息；文字未请求或尚未创建时为空。
	Text *Message
	// ImageSent 表示图片已由平台确认，包含本地状态收口失败但远端已确认的情况。
	ImageSent bool
	// TextSent 表示文字已由平台确认，包含本地状态收口失败但远端已确认的情况。
	TextSent bool
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
	SendImage(ctx context.Context, chatID, toUserID, imageURL string, cardID int64, width, height int, messageKey string) error
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
func NewWithSending(repository Repository, outgoing OutgoingRepository, senders SenderProvider, uploader ImageUploader, identity ...IdentityResolver) *Service {
	return NewWithSendingAndDownloader(repository, outgoing, senders, uploader, nil, identity...)
}

// NewWithSendingAndDownloader 创建支持 URL 图片回复的聊天应用服务，下载能力由适配器构造期注入。
func NewWithSendingAndDownloader(repository Repository, outgoing OutgoingRepository, senders SenderProvider, uploader ImageUploader, downloader ImageURLDownloader, identity ...IdentityResolver) *Service {
	// service 保存聊天历史、发送和平台身份能力的统一应用服务。
	service := &Service{
		repository:        repository,
		outgoing:          outgoing,
		senders:           senders,
		uploader:          uploader,
		imageDownloader:   downloader,
		sessionOperations: newSessionOperationGate(),
	}
	if len(identity) > 0 {
		service.identityResolver = identity[0]
	}
	return service
}

// NewWithSendingAndSubscription 创建同时支持发送和实时订阅的聊天应用服务。
func NewWithSendingAndSubscription(repository Repository, outgoing OutgoingRepository, senders SenderProvider, uploader ImageUploader, subscription SubscriptionProvider, identity ...IdentityResolver) *Service {
	return NewWithSendingAndSubscriptionAndDownloader(repository, outgoing, senders, uploader, subscription, nil, identity...)
}

// NewWithSendingAndSubscriptionAndDownloader 创建支持实时订阅和 URL 图片下载端口的聊天应用服务。
func NewWithSendingAndSubscriptionAndDownloader(repository Repository, outgoing OutgoingRepository, senders SenderProvider, uploader ImageUploader, subscription SubscriptionProvider, downloader ImageURLDownloader, identity ...IdentityResolver) *Service {
	// service 保存聊天历史、发送、实时订阅和图片下载能力的统一应用服务。
	service := NewWithSendingAndDownloader(repository, outgoing, senders, uploader, downloader, identity...)
	service.subscription = subscription
	return service
}

// NewWithSendingSubscriptionAndRefresh 创建同时支持发送、订阅和平台刷新的聊天应用服务。
func NewWithSendingSubscriptionAndRefresh(repository Repository, outgoing OutgoingRepository, senders SenderProvider, uploader ImageUploader, subscription SubscriptionProvider, refresh RefreshProvider, identity ...IdentityResolver) *Service {
	return NewWithSendingSubscriptionAndRefreshAndDownloader(repository, outgoing, senders, uploader, subscription, refresh, nil, identity...)
}

// NewWithSendingSubscriptionAndRefreshAndDownloader 创建生产聊天应用服务并固定 URL 图片下载端口。
func NewWithSendingSubscriptionAndRefreshAndDownloader(repository Repository, outgoing OutgoingRepository, senders SenderProvider, uploader ImageUploader, subscription SubscriptionProvider, refresh RefreshProvider, downloader ImageURLDownloader, identity ...IdentityResolver) *Service {
	// service 保存聊天用例所需的持久化、平台刷新、发送、订阅和图片下载端口。
	service := NewWithSendingAndSubscriptionAndDownloader(repository, outgoing, senders, uploader, subscription, downloader, identity...)
	service.refresh = refresh
	return service
}

// SendingAvailable 报告文字/媒体消息所需的应用端口是否已完成装配。
// 该查询只反映依赖生命周期，不触碰账号凭证或外部平台。
func (s *Service) SendingAvailable() bool {
	return s != nil && s.outgoing != nil && s.senders != nil
}

// ImageUploadAvailable 报告图片上传所需的应用端口是否已完成装配。
func (s *Service) ImageUploadAvailable() bool {
	return s != nil && s.uploader != nil
}

// SendReply 按消息页面的统一发送规则投递 input，图片先发送、文字随后发送。
// URL 和本地图片均复用 SendImage 的上传、尺寸与状态收口；图片失败时不继续发送文字。
func (s *Service) SendReply(ctx context.Context, input ReplyInput) (*ReplyResult, error) {
	// reply 和 normalizeErr 保存规范化后的完整回复及输入校验结果。
	reply, normalizeErr := normalizeReplyInput(input)
	if normalizeErr != nil {
		return nil, normalizeErr
	}
	if s == nil || s.outgoing == nil || s.senders == nil {
		return nil, ErrUnavailable
	}
	// result 保存图片和文字两个分段的本地消息及平台确认状态。
	result := &ReplyResult{}
	if reply.ImageURL != "" || reply.ImagePath != "" {
		// imageMessage 和 imageErr 保存统一图片发送入口的结果及错误。
		imageMessage, imageErr := s.sendReplyImage(ctx, reply)
		result.Image = imageMessage
		result.ImageSent = replyPartDelivered(imageMessage, imageErr)
		if imageErr != nil {
			return result, imageErr
		}
	}
	if reply.Text != "" {
		// textMessage 和 textErr 保存统一文字发送入口的结果及错误。
		textMessage, textErr := s.SendText(ctx, OutgoingInput{Session: reply.Session, Text: reply.Text})
		result.Text = textMessage
		result.TextSent = replyPartDelivered(textMessage, textErr)
		if textErr != nil {
			return result, textErr
		}
	}
	return result, nil
}

// SendImageURL 下载并通过消息页面的 SendImage 入口发送 URL 图片。
// 调用方不需要自行读取图片尺寸，上传适配器会从实际图片内容和平台响应中得到真实宽高。
func (s *Service) SendImageURL(ctx context.Context, input ImageURLInput) (*Message, error) {
	// session、imageURL 保存规范化后的图片会话和来源地址。
	session := input.Session
	session.AccountID = strings.TrimSpace(session.AccountID)
	session.ChatID = strings.TrimSpace(session.ChatID)
	session.PeerUserID = strings.TrimSpace(session.PeerUserID)
	// imageURL 保存去除空白后的来源图片地址。
	imageURL := strings.TrimSpace(input.ImageURL)
	if session.AccountID == "" || session.ChatID == "" || session.PeerUserID == "" || imageURL == "" {
		return nil, ErrSendInvalidInput
	}
	if s == nil || s.outgoing == nil || s.senders == nil || s.uploader == nil || s.imageDownloader == nil {
		return nil, ErrUnavailable
	}
	// imageInput 和 downloadErr 保存 URL 图片转换后的消息页面输入及下载错误。
	imageInput, downloadErr := s.downloadImageInput(ctx, session, imageURL)
	if downloadErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrSend, downloadErr)
	}
	return s.SendImage(ctx, imageInput)
}

// ImageURLInput 是通过统一消息页面入口发送远程图片的应用层输入。
type ImageURLInput struct {
	// Session 保存目标会话摘要，不包含账号凭证。
	Session Session
	// ImageURL 保存待下载的公网图片地址。
	ImageURL string
}

// downloadImageInput 把 URL 图片下载结果转换成现有图片发送用例的输入。
func (s *Service) downloadImageInput(ctx context.Context, session Session, imageURL string) (ImageInput, error) {
	if s == nil || s.imageDownloader == nil {
		return ImageInput{}, ErrUnavailable
	}
	// data、contentType、filename 和 downloadErr 保存图片下载结果及错误。
	data, contentType, filename, downloadErr := s.imageDownloader(ctx, imageURL)
	if downloadErr != nil {
		return ImageInput{}, downloadErr
	}
	if len(data) == 0 || strings.TrimSpace(contentType) == "" {
		return ImageInput{}, ErrSendInvalidInput
	}
	return ImageInput{Session: session, Filename: filename, ContentType: contentType, Data: data}, nil
}

// replyPartDelivered 判断一个回复分段是否已经由平台确认；状态收口失败不应触发远端重发。
func replyPartDelivered(message *Message, sendErr error) bool {
	return message != nil && (sendErr == nil || errors.Is(sendErr, ErrStatusSave))
}

// Subscribe 订阅当前用户有权接收的实时聊天事件；取消函数可安全重复调用。
func (s *Service) Subscribe(ctx context.Context, userID int64) (<-chan Event, func(), error) {
	if s == nil || s.subscription == nil || userID <= 0 {
		return nil, nil, ErrSubscriptionUnavailable
	}
	// events、cancel、err 保存订阅事件流、幂等清理函数和底层错误。
	events, cancel, err := s.subscription.Subscribe(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	// once 保证应用层向 HTTP 暴露的清理函数可安全重复调用。
	var once sync.Once
	return events, func() { once.Do(cancel) }, nil
}

// RefreshConversations 刷新并保存指定账号的联系人页；原始平台数据不会离开应用端口。
func (s *Service) RefreshConversations(ctx context.Context, accountID string, cursor int64, limit int) (ConversationPage, error) {
	accountID = strings.TrimSpace(accountID)
	if s == nil || s.refresh == nil || accountID == "" || limit <= 0 {
		return ConversationPage{}, ErrRefreshUnavailable
	}
	return s.refresh.RefreshConversations(ctx, accountID, cursor, limit)
}

// RefreshHistory 刷新并保存指定会话的消息页；session 只包含非敏感展示字段。
func (s *Service) RefreshHistory(ctx context.Context, accountID, chatID string, cursor int64, limit int, session Session) (HistoryPage, error) {
	accountID = strings.TrimSpace(accountID)
	chatID = strings.TrimSpace(chatID)
	if s == nil || s.refresh == nil || accountID == "" || chatID == "" || limit <= 0 {
		return HistoryPage{}, ErrRefreshUnavailable
	}
	return s.refresh.RefreshHistory(ctx, accountID, chatID, cursor, limit, session)
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
	// unlockOperation 阻止同一会话的本地删除在平台发送和状态收口之间穿插执行。
	unlockOperation := s.sessionOperations.lock(session.AccountID, session.ChatID)
	defer unlockOperation()
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
	if sendErr := sender.SendText(ctx, session.ChatID, session.PeerUserID, text, message.MessageKey); sendErr != nil {
		if errors.Is(sendErr, ErrSendUncertain) {
			// statusCtx 和 statusCancel 为未知结果状态收口提供独立五秒窗口。
			statusCtx, statusCancel := outgoingStatusContext(ctx)
			// uncertain 保存本地未知结果状态写入结果。
			uncertain, _ := s.outgoing.SetOutgoingStatus(statusCtx, session.AccountID, message.MessageKey, "uncertain")
			statusCancel()
			return messagePointer(uncertain, message), fmt.Errorf("%w: %v", ErrSendUncertain, sendErr)
		}
		// failed 保存平台发送失败后的本地状态；状态保存失败不覆盖原始发送错误。
		statusCtx, statusCancel := outgoingStatusContext(ctx)
		// failed 保存平台发送失败后写入的最新消息状态，写入失败时仍保留原始发送错误。
		failed, _ := s.outgoing.SetOutgoingStatus(statusCtx, session.AccountID, message.MessageKey, "failed")
		statusCancel()
		return messagePointer(failed, message), fmt.Errorf("%w: %v", ErrSend, sendErr)
	}
	// sent 和 err 保存平台发送成功后的本地状态及状态持久化错误。
	statusCtx, statusCancel := outgoingStatusContext(ctx)
	// sent 和 err 保存平台已确认投递后的本地 sent 状态与可能的状态收口错误。
	sent, err := s.outgoing.SetOutgoingStatus(statusCtx, session.AccountID, message.MessageKey, "sent")
	statusCancel()
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
	// unlockOperation 覆盖上传、平台发送和状态收口，使随后到达的删除能够清空本次完整操作。
	unlockOperation := s.sessionOperations.lock(session.AccountID, session.ChatID)
	defer unlockOperation()
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
	if sendErr := sender.SendImage(ctx, session.ChatID, session.PeerUserID, upload.URL, 0, upload.Width, upload.Height, message.MessageKey); sendErr != nil {
		if errors.Is(sendErr, ErrSendUncertain) {
			// statusCtx 和 statusCancel 为图片未知结果状态收口提供独立窗口。
			statusCtx, statusCancel := outgoingStatusContext(ctx)
			// uncertain 保存图片消息未知结果状态写入结果。
			uncertain, _ := s.outgoing.SetOutgoingStatus(statusCtx, session.AccountID, message.MessageKey, "uncertain")
			statusCancel()
			return messagePointer(uncertain, message), fmt.Errorf("%w: %v", ErrSendUncertain, sendErr)
		}
		// failed 保存图片发送失败后的本地状态。
		statusCtx, statusCancel := outgoingStatusContext(ctx)
		// failed 保存平台图片发送失败后写入的最新消息状态，写入失败时仍保留原始发送错误。
		failed, _ := s.outgoing.SetOutgoingStatus(statusCtx, session.AccountID, message.MessageKey, "failed")
		statusCancel()
		return messagePointer(failed, message), fmt.Errorf("%w: %v", ErrSend, sendErr)
	}
	// sent 和 err 保存图片发送成功后的本地状态及状态持久化错误。
	statusCtx, statusCancel := outgoingStatusContext(ctx)
	// sent 和 err 保存平台图片已确认投递后的本地 sent 状态与可能的状态收口错误。
	sent, err := s.outgoing.SetOutgoingStatus(statusCtx, session.AccountID, message.MessageKey, "sent")
	statusCancel()
	if err != nil {
		return messagePointer(sent, message), fmt.Errorf("%w: %v", ErrStatusSave, err)
	}
	return messagePointer(sent, message), nil
}

// normalizeOutgoingInput 校验发送会话并返回去除首尾空白的文字内容。
func normalizeOutgoingInput(session Session, text string) (Session, string, error) {
	session.AccountID = strings.TrimSpace(session.AccountID)
	session.ChatID = strings.TrimSpace(session.ChatID)
	session.PeerUserID = strings.TrimSpace(session.PeerUserID)
	text = strings.TrimSpace(text)
	if session.AccountID == "" || session.ChatID == "" || session.PeerUserID == "" || text == "" || len([]rune(text)) > 2000 {
		return Session{}, "", ErrSendInvalidInput
	}
	return session, text, nil
}

// normalizeReplyInput 规范化 input 的会话与内容，返回至少包含一个分段且图片来源不冲突的回复。
func normalizeReplyInput(input ReplyInput) (ReplyInput, error) {
	input.Session.AccountID = strings.TrimSpace(input.Session.AccountID)
	input.Session.ChatID = strings.TrimSpace(input.Session.ChatID)
	input.Session.PeerUserID = strings.TrimSpace(input.Session.PeerUserID)
	input.Text = strings.TrimSpace(input.Text)
	input.ImageURL = strings.TrimSpace(input.ImageURL)
	input.ImagePath = strings.TrimSpace(input.ImagePath)
	if input.Session.AccountID == "" || input.Session.ChatID == "" || input.Session.PeerUserID == "" ||
		(input.Text == "" && input.ImageURL == "" && input.ImagePath == "") || len([]rune(input.Text)) > 2000 ||
		(input.ImageURL != "" && input.ImagePath != "") {
		return ReplyInput{}, ErrSendInvalidInput
	}
	return input, nil
}

// messagePointer 在状态更新返回空值时回退到已创建消息，确保错误响应仍能携带幂等键。
func messagePointer(message Message, fallback Message) *Message {
	if message.MessageKey == "" {
		message = fallback
	}
	return &message
}

// outgoingStatusContext 为远端副作用后的本地状态补偿创建有界上下文；它保留请求中的追踪值但忽略客户端取消。
func outgoingStatusContext(parent context.Context) (context.Context, context.CancelFunc) {
	// base 保存去除取消语义后的父上下文；nil Context 退化为后台上下文以保持补偿路径可用。
	base := context.Background()
	if parent != nil {
		base = context.WithoutCancel(parent)
	}
	return context.WithTimeout(base, outgoingStatusSaveTimeout)
}
