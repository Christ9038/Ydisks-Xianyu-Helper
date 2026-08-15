package server

import (
	"context"
	"errors"
	"strings"

	"xianyu-go/internal/account"
	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/automation"
	domainchat "xianyu-go/internal/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/xianyu/mtop"
)

var (
	// errCommunicationUnavailable 表示聊天或自动化依赖尚未初始化。
	errCommunicationUnavailable = errors.New("通信服务未启用")
)

// communicationService 是聊天、账号任务和通知应用服务。
type communicationService struct {
	// server 提供数据库、聊天事件中心、账号运行时和通知器依赖。
	server *Server
	// repository 提供账号任务、通知绑定和聊天历史的最小持久化能力。
	repository communicationRepository
}

// accountTaskUpdateInput 是账号任务设置更新的业务输入。
type accountTaskUpdateInput struct {
	// CookieID 是账号标识。
	CookieID string
	// Settings 是待保存的任务设置。
	Settings db.AccountTaskSettings
}

// notificationBindingRow 是通知绑定列表的一行业务数据。
type notificationBindingRow struct {
	// ID 是绑定记录标识。
	ID int64
	// CookieID 是绑定账号标识。
	CookieID string
	// ChannelID 是通知渠道标识。
	ChannelID int64
	// ChannelName 是通知渠道名称。
	ChannelName string
	// Enabled 表示绑定是否启用。
	Enabled bool
}

// communicationApplication 返回当前 Server 绑定的通信应用服务。
func (s *Server) communicationApplication() *communicationService {
	return s.applicationServiceSet().communication
}

// GetAccountTaskSettings 读取指定账号的任务设置。
func (svc *communicationService) GetAccountTaskSettings(ctx context.Context, cookieID string) (db.AccountTaskSettings, error) {
	return svc.repository.GetAccountTaskSettings(ctx, cookieID)
}

// UpdateAccountTaskSettings 校验并保存账号任务设置，然后返回数据库中的最终值。
func (svc *communicationService) UpdateAccountTaskSettings(ctx context.Context, input accountTaskUpdateInput) (db.AccountTaskSettings, error) {
	// settings 是规范化后的账号任务设置。
	settings := input.Settings
	settings.CookieID = input.CookieID
	settings.RateContent = strings.TrimSpace(settings.RateContent)
	if settings.AutoRateEnabled && settings.RateContent == "" {
		return db.AccountTaskSettings{}, errors.New("启用自动评价时评价内容不能为空")
	}
	if len([]rune(settings.RateContent)) > 500 {
		return db.AccountTaskSettings{}, errors.New("评价内容不能超过 500 个字符")
	}
	if !accountTaskTimePattern.MatchString(settings.PolishTime) {
		return db.AccountTaskSettings{}, errors.New("擦亮时间格式必须为 HH:mm")
	}
	// err 表示任务设置持久化错误。
	if err := svc.repository.UpsertAccountTaskSettings(ctx, settings); err != nil {
		return db.AccountTaskSettings{}, err
	}
	return svc.repository.GetAccountTaskSettings(ctx, input.CookieID)
}

// ListAccountTaskRuns 查询账号最近的任务执行记录。
func (svc *communicationService) ListAccountTaskRuns(ctx context.Context, cookieID string, limit int) ([]db.AccountTaskRun, error) {
	return svc.repository.ListAccountTaskRuns(ctx, cookieID, limit)
}

// RunAccountTask 执行一次账号自动化任务并返回执行摘要。
func (svc *communicationService) RunAccountTask(ctx context.Context, cookieID, taskType string) (automation.AccountTaskSummary, error) {
	if svc.server.automation == nil {
		return automation.AccountTaskSummary{}, errCommunicationUnavailable
	}
	return svc.server.automation.RunAccountTask(ctx, cookieID, taskType)
}

// ListNotificationChannels 查询用户拥有的通知渠道。
func (svc *communicationService) ListNotificationChannels(ctx context.Context, userID int64) ([]db.NotificationChannelRow, error) {
	return svc.repository.ListNotificationChannels(ctx, userID)
}

// CreateNotificationChannel 创建用户通知渠道。
func (svc *communicationService) CreateNotificationChannel(ctx context.Context, row db.NotificationChannelRow) (int64, error) {
	return svc.repository.CreateNotificationChannel(ctx, &row)
}

// UpdateNotificationChannel 更新用户拥有的通知渠道。
func (svc *communicationService) UpdateNotificationChannel(ctx context.Context, row db.NotificationChannelRow, userID int64) error {
	return svc.repository.UpdateNotificationChannel(ctx, &row, userID)
}

// GetNotificationChannel 查询用户拥有的单个通知渠道。
func (svc *communicationService) GetNotificationChannel(ctx context.Context, channelID, userID int64) (*db.NotificationChannelRow, error) {
	return svc.repository.GetNotificationChannel(ctx, channelID, userID)
}

// DeleteNotificationChannel 删除用户拥有的通知渠道。
func (svc *communicationService) DeleteNotificationChannel(ctx context.Context, channelID, userID int64) error {
	return svc.repository.DeleteNotificationChannel(ctx, channelID, userID)
}

// TestNotificationChannel 向用户拥有的通知渠道发送测试消息。
func (svc *communicationService) TestNotificationChannel(ctx context.Context, channelID, userID int64, body string) error {
	if svc.server.notifier == nil {
		return errCommunicationUnavailable
	}
	// channel 和 err 保存通知渠道查询结果。
	channel, err := svc.repository.GetNotificationChannelConfig(ctx, channelID)
	if err != nil {
		return err
	}
	if channel == nil {
		return db.ErrForbidden
	}
	// row 和 err 保存带用户归属的渠道查询结果。
	row, err := svc.repository.GetNotificationChannel(ctx, channelID, userID)
	if err != nil {
		return err
	}
	if row == nil {
		return db.ErrForbidden
	}
	return svc.server.notifier.SendToChannel(channelID, body)
}

// ListNotificationBindings 查询用户账号的通知绑定。
func (svc *communicationService) ListNotificationBindings(ctx context.Context, userID int64) (map[string][]notificationBindingRow, error) {
	// rows 和 err 保存绑定查询结果集。
	rows, err := svc.repository.ListNotificationBindings(ctx, userID)
	if err != nil {
		return nil, err
	}
	// result 是按账号分组的通知绑定列表。
	result := make(map[string][]notificationBindingRow)
	// row 表示当前遍历过程中的row
	for _, row := range rows {
		// binding 保存当前扫描到的通知绑定记录。
		binding := notificationBindingRow{ID: row.ID, CookieID: row.CookieID, ChannelID: row.ChannelID, ChannelName: row.ChannelName, Enabled: row.Enabled}
		result[binding.CookieID] = append(result[binding.CookieID], binding)
	}
	return result, nil
}

// SetNotificationBindings 覆盖保存账号的通知渠道绑定。
func (svc *communicationService) SetNotificationBindings(ctx context.Context, cookieID string, channelIDs []int64) error {
	return svc.repository.SetNotificationBindings(ctx, cookieID, channelIDs)
}

// GetNotificationBindingIDs 查询账号当前启用的通知渠道标识。
func (svc *communicationService) GetNotificationBindingIDs(ctx context.Context, cookieID string) ([]int64, error) {
	return svc.repository.GetNotificationBindingIDs(ctx, cookieID)
}

// SetSingleNotificationBinding 更新单个账号通知渠道的启用状态。
func (svc *communicationService) SetSingleNotificationBinding(ctx context.Context, cookieID string, channelID int64, enabled bool) error {
	if !enabled {
		return svc.repository.SetSingleNotificationBinding(ctx, cookieID, channelID, false)
	}
	return svc.repository.SetSingleNotificationBinding(ctx, cookieID, channelID, true)
}

// DeleteNotificationBinding 删除用户账号下的一条通知绑定。
func (svc *communicationService) DeleteNotificationBinding(ctx context.Context, userID, bindingID int64) error {
	return svc.repository.DeleteNotificationBinding(ctx, userID, bindingID)
}

// DeleteAccountNotificationBindings 删除用户账号的全部通知绑定。
func (svc *communicationService) DeleteAccountNotificationBindings(ctx context.Context, userID int64, cookieID string) error {
	return svc.repository.DeleteAccountNotificationBindings(ctx, userID, cookieID)
}

// MarkChatRead 将指定聊天会话标记为已读。
func (svc *communicationService) MarkChatRead(ctx context.Context, userID int64, accountID, chatID string) error {
	return svc.repository.MarkChatRead(ctx, userID, accountID, chatID)
}

// storeChatOutgoingRepository 将领域聊天服务适配为实时发送应用端口。
type storeChatOutgoingRepository struct {
	// service 保存聊天消息幂等写入和实时事件发布能力。
	service *domainchat.Service
}

// CreateOutgoing 创建文字消息并转换为应用层非敏感模型。
func (r storeChatOutgoingRepository) CreateOutgoing(ctx context.Context, session chatapp.Session, text string) (chatapp.Message, error) {
	if r.service == nil {
		return chatapp.Message{}, chatapp.ErrUnavailable
	}
	// message、err 保存领域服务返回的消息及写入错误。
	message, err := r.service.CreateOutgoing(ctx, dbChatSessionFromApplication(session), text)
	return applicationChatMessage(message), err
}

// CreateOutgoingMedia 创建媒体消息并转换为应用层非敏感模型。
func (r storeChatOutgoingRepository) CreateOutgoingMedia(ctx context.Context, session chatapp.Session, messageType, content string) (chatapp.Message, error) {
	if r.service == nil {
		return chatapp.Message{}, chatapp.ErrUnavailable
	}
	// message、err 保存领域服务返回的媒体消息及写入错误。
	message, err := r.service.CreateOutgoingMedia(ctx, dbChatSessionFromApplication(session), messageType, content)
	return applicationChatMessage(message), err
}

// SetOutgoingStatus 更新外发消息状态并转换为应用层非敏感模型。
func (r storeChatOutgoingRepository) SetOutgoingStatus(ctx context.Context, accountID, key, status string) (chatapp.Message, error) {
	if r.service == nil {
		return chatapp.Message{}, chatapp.ErrUnavailable
	}
	// message、err 保存领域服务返回的状态消息及更新错误。
	message, err := r.service.SetOutgoingStatus(ctx, accountID, key, status)
	return applicationChatMessage(message), err
}

// managerChatSenderProvider 将账号管理器适配为聊天应用的在线发送端口。
type managerChatSenderProvider struct {
	// manager 保存当前进程内的账号运行时管理器。
	manager *account.Manager
}

// Sender 返回指定账号的在线发送能力。
func (p managerChatSenderProvider) Sender(accountID string) (chatapp.Sender, bool) {
	if p.manager == nil {
		return nil, false
	}
	// sender、ok 保存账号管理器返回的运行时发送器及存在性。
	sender, ok := p.manager.GetInstance(accountID)
	if !ok || sender == nil {
		return nil, false
	}
	return managerChatSender{sender: sender}, true
}

// managerChatSender 将自动化消息发送接口收敛为应用聊天端口，并注入幂等键上下文。
type managerChatSender struct {
	// sender 保存账号运行时提供的最小消息发送能力。
	sender automation.MessageSender
}

// SendText 发送文字并将应用层幂等键传递给运行时旁路观察器。
func (s managerChatSender) SendText(ctx context.Context, chatID, toUserID, text, messageKey string) error {
	return s.sender.SendText(engine.WithOutgoingMessageKey(ctx, messageKey), chatID, toUserID, text)
}

// SendImage 发送图片并将应用层幂等键传递给运行时接口。
func (s managerChatSender) SendImage(ctx context.Context, chatID, toUserID, imageURL string, cardID int64, messageKey string) error {
	return s.sender.SendImage(engine.WithOutgoingMessageKey(ctx, messageKey), chatID, toUserID, imageURL, cardID)
}

// storeChatCredentialRepository 将账号 Cookie 仓储适配为图片发送端口。
type storeChatCredentialRepository struct {
	// store 保存数据库聚合入口，仅在凭证适配器内读取和更新 Cookie。
	store *db.Store
}

// GetCookieValue 读取图片上传所需的账号凭证；调用方不得记录返回值。
func (r storeChatCredentialRepository) GetCookieValue(ctx context.Context, accountID string) (string, error) {
	if r.store == nil || r.store.Cookies == nil {
		return "", chatapp.ErrUnavailable
	}
	return r.store.Cookies.GetValue(ctx, accountID)
}

// UpdateCookieValue 保存图片上传后平台刷新的账号凭证。
func (r storeChatCredentialRepository) UpdateCookieValue(ctx context.Context, accountID, cookieValue string) error {
	if r.store == nil || r.store.Cookies == nil {
		return chatapp.ErrUnavailable
	}
	return r.store.Cookies.UpdateValueExisting(ctx, accountID, cookieValue)
}

// serverChatImageUploader 将 Server 当前 MTOP 客户端适配为图片上传端口。
type serverChatImageUploader struct {
	// server 提供可动态替换的 MTOP 客户端，便于测试注入平台替身。
	server *Server
	// credentials 负责在平台刷新后持久化明文凭证，但不向应用层返回。
	credentials storeChatCredentialRepository
	// manager 负责将刷新后的凭证同步到在线运行时。
	manager *account.Manager
}

// UploadChatImage 在适配器内部读取和刷新凭证，只向应用层返回图片地址。
func (u serverChatImageUploader) UploadChatImage(ctx context.Context, accountID, filename, contentType string, data []byte) (chatapp.ImageUpload, error) {
	if u.server == nil {
		return chatapp.ImageUpload{}, chatapp.ErrUnavailable
	}
	// cookieValue 和 err 保存平台调用所需的短暂明文凭证及读取错误，不得离开适配器。
	cookieValue, err := u.credentials.GetCookieValue(ctx, accountID)
	if err != nil {
		return chatapp.ImageUpload{}, err
	}
	// uploader、ok 保存 MTOP 图片上传能力及接口支持情况。
	uploader, ok := u.server.mtopClient().(interface {
		UploadChatImage(context.Context, string, string, string, []byte) (*mtop.ChatImageUpload, error)
	})
	if !ok {
		return chatapp.ImageUpload{}, chatapp.ErrUnavailable
	}
	// upload、err 保存图片平台返回结果及调用错误。
	upload, err := uploader.UploadChatImage(ctx, cookieValue, filename, contentType, data)
	if err != nil {
		return chatapp.ImageUpload{}, err
	}
	if upload == nil {
		return chatapp.ImageUpload{}, chatapp.ErrSend
	}
	if upload.UpdatedCookies != "" && upload.UpdatedCookies != cookieValue {
		// persistErr 保存刷新凭证的持久化错误；该错误必须反馈给调用方，避免静默丢失会话状态。
		if persistErr := u.credentials.UpdateCookieValue(ctx, accountID, upload.UpdatedCookies); persistErr != nil {
			return chatapp.ImageUpload{}, persistErr
		}
		if u.manager != nil {
			// sender、senderOK 保存刷新凭证同步到运行时的结果。
			sender, senderOK := u.manager.GetInstance(accountID)
			if senderOK && sender != nil {
				sender.UpdateCookie(upload.UpdatedCookies)
			}
		}
	}
	return chatapp.ImageUpload{URL: upload.URL}, nil
}

// newChatSendingApplication 创建实时聊天发送应用服务及其 Server 适配器。
func newChatSendingApplication(server *Server) *chatapp.Service {
	// domainService 保存聊天领域服务；空值表示发送能力未装配。
	var domainService *domainchat.Service
	if server != nil {
		domainService = server.chat
	}
	// store 保存聊天适配器可用的数据库入口；空 Server 仍返回可安全调用的不可用服务。
	var store *db.Store
	// manager 保存账号运行时管理器；空值表示无法发送平台消息。
	var manager *account.Manager
	if server != nil {
		store, manager = server.Store, server.Manager
	}
	return chatapp.NewWithSending(
		newStoreChatApplicationRepository(store),
		storeChatOutgoingRepository{service: domainService},
		managerChatSenderProvider{manager: manager},
		serverChatImageUploader{server: server, credentials: storeChatCredentialRepository{store: store}, manager: manager},
	)
}

// applicationChatMessage 将数据库消息转换为应用层模型；空指针保持零值。
func applicationChatMessage(message *db.ChatMessage) chatapp.Message {
	if message == nil {
		return chatapp.Message{}
	}
	return chatapp.Message{
		ID: message.ID, AccountID: message.CookieID, ChatID: message.ChatID, MessageKey: message.MessageKey,
		Direction: message.Direction, SenderID: message.SenderID, SenderName: message.SenderName,
		MessageType: message.MessageType, Content: message.Content, Status: message.Status, SentAt: message.SentAt,
	}
}

// 确保各 Server 适配器覆盖聊天应用服务定义的最小端口。
var (
	_ chatapp.OutgoingRepository = storeChatOutgoingRepository{}
	_ chatapp.SenderProvider     = managerChatSenderProvider{}
	_ chatapp.Sender             = managerChatSender{}
	_ chatapp.ImageUploader      = serverChatImageUploader{}
)
