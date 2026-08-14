package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/xianyu/mtop"
)

var (
	// errCommunicationUnavailable 表示聊天或自动化依赖尚未初始化。
	errCommunicationUnavailable = errors.New("通信服务未启用")
	// errChatOffline 表示目标账号当前没有在线运行实例。
	errChatOffline = errors.New("账号当前离线")
	// errChatSend 表示远端聊天消息发送失败。
	errChatSend = errors.New("聊天消息发送失败")
	// errChatStatusSave 表示远端消息已发送但本地状态保存失败。
	errChatStatusSave = errors.New("聊天发送状态保存失败")
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

// chatTextInput 是发送文字消息的业务输入。
type chatTextInput struct {
	// Session 是聊天会话及买家信息。
	Session db.ChatSession
	// Text 是待发送的文字内容。
	Text string
}

// chatImageInput 是发送图片消息的业务输入。
type chatImageInput struct {
	// Session 是聊天会话及买家信息。
	Session db.ChatSession
	// Filename 是上传图片文件名。
	Filename string
	// ContentType 是图片 MIME 类型。
	ContentType string
	// Data 是图片二进制内容。
	Data []byte
}

// chatMessagePageResult 是聊天历史查询结果，不依赖 HTTP DTO。
type chatMessagePageResult struct {
	// Messages 是本地或远端保存的聊天消息。
	Messages []db.ChatMessage
	// Session 是当前聊天会话摘要。
	Session db.ChatSession
	// HasMore 表示是否还有更早消息。
	HasMore bool
	// NextCursor 是远端历史分页游标。
	NextCursor int64
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

// SendChatText 创建并发送一条文字消息，失败时保留可重试的本地失败状态。
func (svc *communicationService) SendChatText(ctx context.Context, input chatTextInput) (*db.ChatMessage, error) {
	// s 是当前通信应用服务依赖的 Server。
	s := svc.server
	if s.chat == nil || s.Manager == nil {
		return nil, errCommunicationUnavailable
	}
	// sender 保存目标账号的在线发送句柄。
	sender, ok := s.Manager.GetInstance(input.Session.CookieID)
	if !ok || sender == nil {
		return nil, errChatOffline
	}
	// message 和 err 保存待发送消息落库结果。
	message, err := s.chat.CreateOutgoing(ctx, input.Session, input.Text)
	if err != nil {
		return nil, fmt.Errorf("保存待发送消息失败: %w", err)
	}
	// err 表示文字发送错误。
	if err := sender.SendText(engine.WithOutgoingMessageKey(ctx, message.MessageKey), input.Session.ChatID, input.Session.BuyerID, input.Text); err != nil {
		// failed 保存发送失败后的本地消息状态。
		failed, _ := s.chat.SetOutgoingStatus(context.Background(), input.Session.CookieID, message.MessageKey, "failed")
		return failed, fmt.Errorf("%w: %v", errChatSend, err)
	}
	// sent 和 err 保存远端发送成功后的本地状态。
	sent, err := s.chat.SetOutgoingStatus(ctx, input.Session.CookieID, message.MessageKey, "sent")
	if err != nil {
		return sent, fmt.Errorf("%w: %v", errChatStatusSave, err)
	}
	return sent, nil
}

// SendChatImage 上传并发送一条图片消息，失败时保留可重试的本地失败状态。
func (svc *communicationService) SendChatImage(ctx context.Context, input chatImageInput) (*db.ChatMessage, error) {
	// s 是当前通信应用服务依赖的 Server。
	s := svc.server
	if s.chat == nil || s.Manager == nil {
		return nil, errCommunicationUnavailable
	}
	// sender 和 ok 保存目标账号的在线发送句柄及存在性。
	sender, ok := s.Manager.GetInstance(input.Session.CookieID)
	if !ok || sender == nil {
		return nil, errChatOffline
	}
	// cookies 和 err 保存图片上传使用的账号凭证。
	cookies, err := svc.repository.GetCookieValue(ctx, input.Session.CookieID)
	if err != nil {
		return nil, fmt.Errorf("读取账号凭证失败: %w", err)
	}
	// uploader 和 ok 表示图片上传客户端及其能力支持情况。
	uploader, ok := s.mtopClient().(interface {
		UploadChatImage(context.Context, string, string, string, []byte) (*mtop.ChatImageUpload, error)
	})
	if !ok {
		return nil, errCommunicationUnavailable
	}
	// upload 和 err 保存图片上传结果。
	upload, err := uploader.UploadChatImage(ctx, cookies, input.Filename, input.ContentType, input.Data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errChatSend, err)
	}
	if upload.UpdatedCookies != "" && upload.UpdatedCookies != cookies {
		// err 表示保存图片上传后刷新 Cookie 的错误，仅忽略并继续发送。
		_ = svc.repository.UpdateCookieValue(ctx, input.Session.CookieID, upload.UpdatedCookies)
		sender.UpdateCookie(upload.UpdatedCookies)
	}
	// message 和 err 保存图片消息落库结果。
	message, err := s.chat.CreateOutgoingMedia(ctx, input.Session, "image", upload.URL)
	if err != nil {
		return nil, fmt.Errorf("保存待发送图片失败: %w", err)
	}
	// err 表示图片发送错误。
	if err := sender.SendImage(engine.WithOutgoingMessageKey(ctx, message.MessageKey), input.Session.ChatID, input.Session.BuyerID, upload.URL, 0); err != nil {
		// failed 保存图片发送失败后的本地消息状态。
		failed, _ := s.chat.SetOutgoingStatus(context.Background(), input.Session.CookieID, message.MessageKey, "failed")
		return failed, fmt.Errorf("%w: %v", errChatSend, err)
	}
	// sent 和 err 保存图片发送成功后的本地状态。
	sent, err := s.chat.SetOutgoingStatus(ctx, input.Session.CookieID, message.MessageKey, "sent")
	if err != nil {
		return sent, fmt.Errorf("%w: %v", errChatStatusSave, err)
	}
	return sent, nil
}

// MarkChatRead 将指定聊天会话标记为已读。
func (svc *communicationService) MarkChatRead(ctx context.Context, userID int64, accountID, chatID string) error {
	return svc.repository.MarkChatRead(ctx, userID, accountID, chatID)
}

// ListStoredChatMessages 查询本地聊天历史并返回会话摘要。
func (svc *communicationService) ListStoredChatMessages(ctx context.Context, userID int64, accountID, chatID string, beforeID int64, limit int) (chatMessagePageResult, error) {
	// messages 和 err 保存本地聊天历史查询结果。
	messages, err := svc.repository.ListChatMessages(ctx, userID, accountID, chatID, beforeID, limit)
	if err != nil {
		return chatMessagePageResult{}, err
	}
	// session 保存当前聊天会话摘要。
	var session db.ChatSession
	// sessions 和 sessionErr 保存当前用户的聊天会话列表及查询错误。
	if sessions, sessionErr := svc.repository.ListChatSessions(ctx, userID, accountID, 500); sessionErr == nil {
		// candidate 是当前遍历到的会话摘要。
		for _, candidate := range sessions {
			if candidate.ChatID == chatID {
				session = svc.server.resolveSelectedChatIdentity(ctx, accountID, candidate)
				break
			}
		}
	}
	return chatMessagePageResult{Messages: messages, Session: session, HasMore: len(messages) == limit}, nil
}
