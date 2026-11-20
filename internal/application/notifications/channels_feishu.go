package notifications

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// FeishuBindingStatusWaiting 表示等待用户在飞书侧完成扫码确认。
	FeishuBindingStatusWaiting = "waiting"
	// FeishuBindingStatusSuccess 表示扫码成功且通知渠道已保存。
	FeishuBindingStatusSuccess = "success"
	// FeishuBindingStatusExpired 表示飞书设备码或本地会话已过期。
	FeishuBindingStatusExpired = "expired"
	// FeishuBindingStatusError 表示授权被拒绝或绑定过程中出现可展示的失败。
	FeishuBindingStatusError = "error"
	// feishuAppChannelType 是扫码绑定创建或更新的通知渠道类型。
	feishuAppChannelType = "feishu_app"
	// feishuAppChannelName 是新建飞书应用渠道的默认名称，不包含 open_id 或任何凭据。
	feishuAppChannelName = "飞书应用"
	// feishuAppDefaultEventTypes 是新建渠道默认订阅的事件类型 JSON。
	feishuAppDefaultEventTypes = `["chat_message_received"]`
	// feishuAppDefaultExpiresIn 是注册结果缺少有效期时的本地会话兜底有效期。
	feishuAppDefaultExpiresIn = 5 * time.Minute
	// feishuAppDefaultPollInterval 是注册结果缺少间隔时的最小轮询间隔。
	feishuAppDefaultPollInterval = 5 * time.Second
	// feishuAppMaxPollInterval 是 slow_down 后允许的最大轮询间隔，避免会话在过期前无限拉长。
	feishuAppMaxPollInterval = 60 * time.Second
)

var (
	// ErrFeishuUnavailable 表示应用服务未注入飞书扫码注册端口。
	ErrFeishuUnavailable = errors.New("飞书应用绑定未启用")
	// ErrFeishuSessionNotFound 表示扫码会话不存在、已过期或不属于当前用户。
	ErrFeishuSessionNotFound = errors.New("飞书绑定会话不存在或已过期")
)

// FeishuPollStatus 是应用层消费的飞书设备码轮询分类，与平台协议解耦。
type FeishuPollStatus string

const (
	// FeishuPollPending 表示用户尚未完成扫码确认。
	FeishuPollPending FeishuPollStatus = "pending"
	// FeishuPollSlowDown 表示平台要求降低轮询频率。
	FeishuPollSlowDown FeishuPollStatus = "slow_down"
	// FeishuPollSuccess 表示授权成功且结果携带应用凭据。
	FeishuPollSuccess FeishuPollStatus = "success"
	// FeishuPollDenied 表示用户拒绝授权。
	FeishuPollDenied FeishuPollStatus = "denied"
	// FeishuPollExpired 表示飞书设备码已过期。
	FeishuPollExpired FeishuPollStatus = "expired"
	// FeishuPollError 表示平台返回非法或无法归类的授权结果。
	FeishuPollError FeishuPollStatus = "error"
)

// FeishuDeviceCode 是应用层定义的扫码注册结果，不携带平台 HTTP 类型。
type FeishuDeviceCode struct {
	// DeviceCode 是平台颁发的短期设备码，只保存到进程内会话，禁止日志输出。
	DeviceCode string
	// QRCodeURL 是可直接编码为二维码的完整扫码地址。
	QRCodeURL string
	// ExpiresIn 是设备码有效期；零值由应用服务使用默认值。
	ExpiresIn time.Duration
	// Interval 是平台建议的轮询间隔；零值由应用服务使用默认值。
	Interval time.Duration
}

// FeishuPollRequest 是一次轮询所需的站点域名与设备码。
type FeishuPollRequest struct {
	// Domain 是设备码所属站点域名，空值由注册适配器按飞书处理。
	Domain string
	// DeviceCode 是扫码注册返回的设备码。
	DeviceCode string
}

// FeishuPollResult 是应用层消费的轮询结果；敏感字段只允许进入加密渠道配置。
type FeishuPollResult struct {
	// Status 是协议分类后的轮询状态。
	Status FeishuPollStatus
	// Domain 是轮询后生效的站点域名，用于保存渠道配置和后续轮询。
	Domain string
	// AppID 是扫码创建的应用标识，仅成功状态非空。
	AppID string
	// AppSecret 是扫码创建的应用密钥，仅成功状态非空；禁止日志或响应输出。
	AppSecret string
	// OpenID 是授权用户 open_id，仅成功状态非空。
	OpenID string
	// Message 是可展示给用户的稳定中文说明。
	Message string
	// Interval 是 slow_down 要求的新轮询间隔。
	Interval time.Duration
}

// FeishuRegistrar 定义通知应用层执行飞书扫码注册所需的最小协议端口。
// 实现方负责平台 HTTP 与凭据解析，应用层只消费 plain 类型并进行会话与渠道编排。
type FeishuRegistrar interface {
	// BeginRegistration 发起设备码注册并返回扫码地址与轮询参数。
	BeginRegistration(ctx context.Context) (FeishuDeviceCode, error)
	// PollRegistration 执行一次设备码轮询并返回分类结果。
	PollRegistration(ctx context.Context, request FeishuPollRequest) (FeishuPollResult, error)
}

// FeishuBindingOptions 是一次飞书扫码绑定需要保存的渠道行为选项，使用具名字段避免多个裸布尔参数混淆。
type FeishuBindingOptions struct {
	// BindAllAccounts 表示绑定成功后是否把渠道绑定到当前用户全部账号。
	BindAllAccounts bool
	// NotifyWarning 表示是否推送警告类通知。
	NotifyWarning bool
	// NotifyInfo 表示是否推送普通提示类通知。
	NotifyInfo bool
}

// FeishuChannelCategories 是飞书应用渠道的警告与普通提示推送开关。
type FeishuChannelCategories struct {
	// Warning 表示是否推送警告类通知。
	Warning bool
	// Info 表示是否推送普通提示类通知。
	Info bool
}

// FeishuBindingStart 是开始扫码绑定后返回给 transport 的非敏感结果。
type FeishuBindingStart struct {
	// SessionID 是进程内扫码会话标识。
	SessionID string
	// QRCodeURL 是可直接展示的扫码地址。
	QRCodeURL string
	// ExpiresIn 是会话剩余有效秒数，供前端安排提示或轮询。
	ExpiresIn int
}

// FeishuBindingStatus 是扫码绑定状态的非敏感查询结果。
type FeishuBindingStatus struct {
	// Status 是 waiting、success、expired 或 error。
	Status string
	// Message 是可展示的中文说明，不包含平台原始响应或凭据。
	Message string
	// ChannelID 是成功时已创建或更新的通知渠道标识；其他状态为零值。
	ChannelID int64
}

// feishuBindingSession 是单个扫码绑定的进程内状态；所有字段由 ChannelService.feishuMu 保护。
type feishuBindingSession struct {
	// ID 是会话标识。
	ID string
	// UserID 是发起绑定的用户，查询时再次校验归属。
	UserID int64
	// DeviceCode 是平台设备码，禁止日志输出。
	DeviceCode string
	// Domain 是当前轮询使用的站点域名，识别到 Lark 后保持为 lark。
	Domain string
	// ExpiresAt 是本地会话停止接受轮询的时间。
	ExpiresAt time.Time
	// Interval 是当前轮询间隔。
	Interval time.Duration
	// NextPollAt 是允许下一次平台轮询的最早时间。
	NextPollAt time.Time
	// BindAll 表示成功后是否把渠道绑定到当前用户全部账号。
	BindAll bool
	// NotifyWarning 表示该渠道是否推送警告类通知。
	NotifyWarning bool
	// NotifyInfo 表示该渠道是否推送普通提示类通知。
	NotifyInfo bool
	// Status 是当前绑定状态。
	Status string
	// Message 是可展示的稳定说明。
	Message string
	// ChannelID 是成功后的渠道标识。
	ChannelID int64
	// Polling 表示已有请求正在执行平台轮询，避免并发重复调用。
	Polling bool
}

// feishuAppChannelConfig 是保存到加密渠道 Config 的飞书应用字段；禁止序列化到 HTTP 响应。
type feishuAppChannelConfig struct {
	// AppID 是飞书应用标识。
	AppID string `json:"app_id"`
	// AppSecret 是飞书应用密钥，只允许存在于加密渠道配置。
	AppSecret string `json:"app_secret"`
	// OpenID 是授权用户 open_id。
	OpenID string `json:"open_id"`
	// Domain 是开放平台站点域名，取值为 feishu 或 lark。
	Domain string `json:"domain"`
	// NotifyWarning 表示是否推送警告类通知。
	NotifyWarning bool `json:"notify_warning"`
	// NotifyInfo 表示是否推送普通提示类通知。
	NotifyInfo bool `json:"notify_info"`
}

// StartFeishuAppBinding 发起飞书扫码绑定并登记进程内会话。
// userID 是发起用户；options 同时携带账号绑定策略和两个分类推送开关。返回结果只包含 session_id、扫码地址和秒数。
func (s *ChannelService) StartFeishuAppBinding(ctx context.Context, userID int64, options FeishuBindingOptions) (FeishuBindingStart, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return FeishuBindingStart{}, ErrChannelInvalidInput
	}
	if s.registrar == nil {
		return FeishuBindingStart{}, ErrFeishuUnavailable
	}
	// deviceCode、beginErr 保存注册结果及协议错误。
	deviceCode, beginErr := s.registrar.BeginRegistration(ctx)
	if beginErr != nil {
		return FeishuBindingStart{}, beginErr
	}
	// qrURL 是去除空白后的扫码地址；空值说明注册结果不可展示。
	qrURL := strings.TrimSpace(deviceCode.QRCodeURL)
	if strings.TrimSpace(deviceCode.DeviceCode) == "" || qrURL == "" {
		return FeishuBindingStart{}, fmt.Errorf("飞书扫码注册结果不完整")
	}
	// sessionID、identifierErr 保存随机会话标识及生成错误。
	sessionID, identifierErr := s.generateSessionID()
	if identifierErr != nil {
		return FeishuBindingStart{}, identifierErr
	}
	// now 是会话登记时间。
	now := s.currentTime()
	// expiresIn 是经过兜底后的设备码有效期。
	expiresIn := deviceCode.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = feishuAppDefaultExpiresIn
	}
	// interval 是经过最小值和上限处理的轮询间隔。
	interval := normalizePollInterval(deviceCode.Interval)
	// session 是即将登记的扫码会话；其状态和字段只在 feishuMu 锁内修改。
	session := &feishuBindingSession{
		ID:            sessionID,
		UserID:        userID,
		DeviceCode:    deviceCode.DeviceCode,
		Domain:        "feishu",
		ExpiresAt:     now.Add(expiresIn),
		Interval:      interval,
		NextPollAt:    now.Add(interval),
		BindAll:       options.BindAllAccounts,
		NotifyWarning: options.NotifyWarning,
		NotifyInfo:    options.NotifyInfo,
		Status:        FeishuBindingStatusWaiting,
		Message:       "等待飞书扫码确认",
	}
	s.feishuMu.Lock()
	if s.feishuSessions == nil {
		s.feishuSessions = make(map[string]*feishuBindingSession)
	}
	s.cleanupExpiredFeishuSessions(now)
	s.feishuSessions[sessionID] = session
	s.feishuMu.Unlock()
	return FeishuBindingStart{SessionID: sessionID, QRCodeURL: qrURL, ExpiresIn: int(expiresIn.Seconds())}, nil
}

// GetFeishuAppBindingStatus 查询扫码绑定状态；到达轮询时间且状态仍为 waiting 时会执行一次平台轮询。
// userID 用于会话归属隔离，sessionID 由开始绑定接口返回。不存在或本地已过期的会话返回 ErrFeishuSessionNotFound。
func (s *ChannelService) GetFeishuAppBindingStatus(ctx context.Context, userID int64, sessionID string) (FeishuBindingStatus, error) {
	if s == nil || s.repository == nil || userID <= 0 || strings.TrimSpace(sessionID) == "" {
		return FeishuBindingStatus{}, ErrChannelInvalidInput
	}
	if s.registrar == nil {
		return FeishuBindingStatus{}, ErrFeishuUnavailable
	}
	// now 是本次状态查询时间。
	now := s.currentTime()
	s.feishuMu.Lock()
	s.cleanupExpiredFeishuSessions(now)
	// session 是当前用户可见的扫码会话；不存在或归属不符都按同一错误处理，避免泄漏其他用户会话。
	session := s.feishuSessions[sessionID]
	if session == nil || session.UserID != userID {
		s.feishuMu.Unlock()
		return FeishuBindingStatus{}, ErrFeishuSessionNotFound
	}
	if session.Status != FeishuBindingStatusWaiting {
		// status 是当前会话终态快照；复制后立即释放锁。
		status := feishuBindingStatusFromSession(session)
		s.feishuMu.Unlock()
		return status, nil
	}
	if session.Polling {
		// 轮询中的会话由持有轮询权的请求完成状态收口，不能被并发查询提前删除。
		status := feishuBindingStatusFromSession(session)
		s.feishuMu.Unlock()
		return status, nil
	}
	if !now.Before(session.ExpiresAt) {
		delete(s.feishuSessions, sessionID)
		s.feishuMu.Unlock()
		return FeishuBindingStatus{}, ErrFeishuSessionNotFound
	}
	if now.Before(session.NextPollAt) {
		// status 是当前等待状态快照；尚未到达下一次轮询时间时不发起平台请求。
		status := feishuBindingStatusFromSession(session)
		s.feishuMu.Unlock()
		return status, nil
	}
	// pollRequest 是本轮平台轮询所需的不可变请求快照。
	pollRequest := FeishuPollRequest{Domain: session.Domain, DeviceCode: session.DeviceCode}
	// bindingOptions 是创建或更新渠道时使用的账号绑定与分类推送开关快照。
	bindingOptions := FeishuBindingOptions{BindAllAccounts: session.BindAll, NotifyWarning: session.NotifyWarning, NotifyInfo: session.NotifyInfo}
	// currentInterval 是当前会话轮询间隔的锁内快照。
	currentInterval := normalizePollInterval(session.Interval)
	// currentDomain 是当前会话站点域名的锁内快照。
	currentDomain := session.Domain
	session.Polling = true
	s.feishuMu.Unlock()
	// result、pollErr 保存平台轮询分类结果或协议错误。
	result, pollErr := s.registrar.PollRegistration(ctx, pollRequest)
	if pollErr != nil {
		return s.finishFeishuPollFailure(sessionID, "飞书授权状态查询失败，请重新发起绑定"), nil
	}
	// interval 是下一轮等待间隔，默认沿用锁内读取的当前会话间隔。
	interval := currentInterval
	if result.Interval > 0 {
		interval = normalizePollInterval(result.Interval)
	}
	switch result.Status {
	case FeishuPollPending:
		// nextDomain 是轮询后应继续使用的站点域名；Lark 识别后不会回退。
		nextDomain := mergePollDomain(currentDomain, result.Domain)
		return s.finishFeishuWaiting(sessionID, nextDomain, interval, "等待飞书扫码确认"), nil
	case FeishuPollSlowDown:
		// slowedInterval 是平台要求后的新间隔；平台未给值时按当前间隔翻倍并限制上限。
		slowedInterval := interval
		if result.Interval <= 0 {
			slowedInterval = normalizePollInterval(interval * 2)
		}
		return s.finishFeishuWaiting(sessionID, mergePollDomain(currentDomain, result.Domain), slowedInterval, "飞书要求降低轮询频率"), nil
	case FeishuPollDenied:
		return s.finishFeishuTerminal(sessionID, FeishuBindingStatusError, "用户已拒绝飞书应用授权"), nil
	case FeishuPollExpired:
		return s.finishFeishuTerminal(sessionID, FeishuBindingStatusExpired, "飞书扫码授权已过期，请重新扫码"), nil
	case FeishuPollError:
		// message 是平台适配器提供的稳定中文说明；为空时使用通用失败文案。
		message := strings.TrimSpace(result.Message)
		if message == "" {
			message = "飞书授权失败，请重新扫码"
		}
		return s.finishFeishuTerminal(sessionID, FeishuBindingStatusError, message), nil
	case FeishuPollSuccess:
		// channelID、bindErr 保存渠道保存和账号绑定结果；bindingOptions 已在锁内复制。
		channelID, bindErr := s.persistFeishuAppChannel(ctx, userID, result, bindingOptions)
		if bindErr != nil {
			return s.finishFeishuPollFailure(sessionID, "飞书授权成功，但保存通知渠道失败，请重试"), nil
		}
		s.feishuMu.Lock()
		// current 是保存完成时仍存在的同一会话；会话被清理时不覆盖其他操作。
		current := s.feishuSessions[sessionID]
		if current == nil || current.UserID != userID {
			s.feishuMu.Unlock()
			return FeishuBindingStatus{}, ErrFeishuSessionNotFound
		}
		current.Polling = false
		current.Status = FeishuBindingStatusSuccess
		current.Message = "飞书应用绑定成功"
		current.ChannelID = channelID
		s.feishuMu.Unlock()
		return FeishuBindingStatus{Status: FeishuBindingStatusSuccess, Message: "飞书应用绑定成功", ChannelID: channelID}, nil
	default:
		return s.finishFeishuTerminal(sessionID, FeishuBindingStatusError, "飞书授权结果无法识别，请重新扫码"), nil
	}
}

// persistFeishuAppChannel 创建或更新当前用户的 feishu_app 渠道，并按 options 绑定全部账号。
// result 携带平台凭据，只允许写入加密渠道 Config；返回值是渠道标识，错误不应包含凭据。
func (s *ChannelService) persistFeishuAppChannel(ctx context.Context, userID int64, result FeishuPollResult, options FeishuBindingOptions) (int64, error) {
	// configBytes、configErr 保存加密前渠道配置 JSON 及序列化错误。
	configBytes, configErr := json.Marshal(feishuAppChannelConfig{
		AppID:         result.AppID,
		AppSecret:     result.AppSecret,
		OpenID:        result.OpenID,
		Domain:        normalizeFeishuDomain(result.Domain),
		NotifyWarning: options.NotifyWarning,
		NotifyInfo:    options.NotifyInfo,
	})
	if configErr != nil {
		return 0, fmt.Errorf("编码飞书应用渠道配置失败")
	}
	// channels、listErr 保存当前用户渠道摘要及查询错误；摘要不包含任何配置。
	channels, listErr := s.repository.ListChannels(ctx, userID)
	if listErr != nil {
		return 0, listErr
	}
	// existingID 是已有 feishu_app 渠道标识；0 表示需要新建。
	var existingID int64
	// channel 是当前遍历到的非敏感渠道摘要。
	for _, channel := range channels {
		if channel.Type == feishuAppChannelType {
			existingID = channel.ID
			break
		}
	}
	// channelID 是本次保存后的渠道标识。
	var channelID int64
	if existingID > 0 {
		// record、recordErr 保存已有渠道完整记录及读取错误。
		record, recordErr := s.repository.GetChannelForUpdate(ctx, existingID, userID)
		if recordErr != nil {
			return 0, recordErr
		}
		if record != nil {
			record.Config = string(configBytes)
			record.Enabled = true
			record.Type = feishuAppChannelType
			if strings.TrimSpace(record.EventTypes) == "" {
				record.EventTypes = feishuAppDefaultEventTypes
			}
			// updateErr 是已有渠道更新写入错误；nil 表示更新成功。
			if updateErr := s.repository.UpdateChannel(ctx, userID, *record); updateErr != nil {
				return 0, updateErr
			}
			channelID = record.ID
		}
	}
	if channelID == 0 {
		// createdID、createErr 保存新渠道创建结果及错误。
		createdID, createErr := s.repository.CreateChannel(ctx, userID, ChannelInput{
			Name:       feishuAppChannelName,
			Type:       feishuAppChannelType,
			Config:     string(configBytes),
			EventTypes: feishuAppDefaultEventTypes,
			Enabled:    true,
		})
		if createErr != nil {
			return 0, createErr
		}
		channelID = createdID
	}
	if !options.BindAllAccounts {
		return channelID, nil
	}
	// accountIDs、accountsErr 保存当前用户账号标识及查询错误；不读取 Cookie。
	accountIDs, accountsErr := s.repository.ListOwnedAccountIDs(ctx, userID)
	if accountsErr != nil {
		return channelID, accountsErr
	}
	// accountID 是当前待绑定渠道的账号标识。
	for _, accountID := range accountIDs {
		if strings.TrimSpace(accountID) == "" {
			continue
		}
		// bindingErr 保存当前账号单条绑定写入错误；SetSingleBinding 只增改本渠道，保留其他绑定。
		if bindingErr := s.repository.SetSingleBinding(ctx, accountID, channelID, true); bindingErr != nil {
			return channelID, bindingErr
		}
	}
	return channelID, nil
}

// finishFeishuWaiting 在锁内更新等待状态的下一次轮询时间并返回状态快照。
func (s *ChannelService) finishFeishuWaiting(sessionID, domain string, interval time.Duration, message string) FeishuBindingStatus {
	// now 是状态更新时间；nextPollAt 是基于新间隔计算的最早轮询时间。
	now := s.currentTime()
	// nextPollAt 是基于新间隔计算的最早轮询时间。
	nextPollAt := now.Add(interval)
	s.feishuMu.Lock()
	defer s.feishuMu.Unlock()
	// session 是仍存在的扫码会话；已被清理时返回等待快照而不重建会话。
	session := s.feishuSessions[sessionID]
	if session == nil {
		return FeishuBindingStatus{Status: FeishuBindingStatusWaiting, Message: message}
	}
	session.Polling = false
	session.Domain = domain
	session.Interval = interval
	session.NextPollAt = nextPollAt
	session.Status = FeishuBindingStatusWaiting
	session.Message = message
	return feishuBindingStatusFromSession(session)
}

// finishFeishuTerminal 在锁内写入终态并返回状态快照；终态会话保留到会话过期后由清理逻辑删除。
func (s *ChannelService) finishFeishuTerminal(sessionID, status, message string) FeishuBindingStatus {
	s.feishuMu.Lock()
	defer s.feishuMu.Unlock()
	// session 是仍存在的扫码会话；已清理时直接返回调用方给定的终态。
	session := s.feishuSessions[sessionID]
	if session == nil {
		return FeishuBindingStatus{Status: status, Message: message}
	}
	session.Polling = false
	session.Status = status
	session.Message = message
	return feishuBindingStatusFromSession(session)
}

// finishFeishuPollFailure 在锁内写入可展示的失败状态；协议错误本身不返回给 transport，避免泄漏内部细节。
func (s *ChannelService) finishFeishuPollFailure(sessionID, message string) FeishuBindingStatus {
	return s.finishFeishuTerminal(sessionID, FeishuBindingStatusError, message)
}

// cleanupExpiredFeishuSessions 删除本地已过期且未在轮询的会话；调用方必须持有 feishuMu。
func (s *ChannelService) cleanupExpiredFeishuSessions(now time.Time) {
	// sessionID 是当前遍历到的会话标识；session 是当前会话状态。
	for sessionID, session := range s.feishuSessions {
		if session == nil || (!session.Polling && !now.Before(session.ExpiresAt)) {
			delete(s.feishuSessions, sessionID)
		}
	}
}

// generateSessionID 调用当前会话标识生成器并校验结果非空；调用时不得持有 feishuMu。
func (s *ChannelService) generateSessionID() (string, error) {
	s.feishuMu.Lock()
	// generator 是构造期注入或默认的会话标识生成函数。
	generator := s.newSessionID
	s.feishuMu.Unlock()
	if generator == nil {
		generator = newFeishuSessionID
	}
	// sessionID、identifierErr 保存生成结果及错误。
	sessionID, identifierErr := generator()
	if identifierErr != nil {
		return "", fmt.Errorf("生成飞书绑定会话失败")
	}
	if strings.TrimSpace(sessionID) == "" {
		return "", fmt.Errorf("生成飞书绑定会话失败")
	}
	return sessionID, nil
}

// currentTime 返回应用服务使用的当前时间；零值服务或测试未注入时退回 time.Now。
func (s *ChannelService) currentTime() time.Time {
	s.feishuMu.Lock()
	// clock 是构造期注入或默认的时间函数。
	clock := s.now
	s.feishuMu.Unlock()
	if clock == nil {
		return time.Now()
	}
	return clock()
}

// newFeishuSessionID 使用 crypto/rand 生成不可预测的十六进制会话标识。
func newFeishuSessionID() (string, error) {
	// randomBytes 是加密随机源生成的 16 字节会话熵。
	var randomBytes [16]byte
	// readErr 是加密随机源读取失败原因；nil 表示已取得完整会话熵。
	if _, readErr := rand.Read(randomBytes[:]); readErr != nil {
		return "", readErr
	}
	return hex.EncodeToString(randomBytes[:]), nil
}

// normalizePollInterval 将轮询间隔限制在协议最小值与上限之间，零值使用默认间隔。
func normalizePollInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return feishuAppDefaultPollInterval
	}
	if interval < feishuAppDefaultPollInterval {
		return feishuAppDefaultPollInterval
	}
	if interval > feishuAppMaxPollInterval {
		return feishuAppMaxPollInterval
	}
	return interval
}

// normalizeFeishuDomain 将渠道配置域名归一为 feishu 或 lark；未知值按飞书中国站处理。
func normalizeFeishuDomain(domain string) string {
	if strings.EqualFold(strings.TrimSpace(domain), "lark") {
		return "lark"
	}
	return "feishu"
}

// feishuBindingStatusFromSession 在锁内复制会话状态，避免把可变指针暴露给 transport。
func feishuBindingStatusFromSession(session *feishuBindingSession) FeishuBindingStatus {
	if session == nil {
		return FeishuBindingStatus{Status: FeishuBindingStatusError, Message: "飞书绑定会话状态不可用"}
	}
	return FeishuBindingStatus{Status: session.Status, Message: session.Message, ChannelID: session.ChannelID}
}

// GetFeishuChannelCategories 读取飞书应用渠道的警告与普通提示开关。
// 只允许 type=feishu_app 的渠道；配置中的开关缺失时按 true 处理，错误不包含配置内容。
func (s *ChannelService) GetFeishuChannelCategories(ctx context.Context, userID, channelID int64) (FeishuChannelCategories, error) {
	if s == nil || s.repository == nil || userID <= 0 || channelID <= 0 {
		return FeishuChannelCategories{}, ErrChannelInvalidInput
	}
	// record、lookupErr 保存归属校验后的渠道完整记录及读取错误；Config 只在本方法内解析。
	record, lookupErr := s.repository.GetChannelForUpdate(ctx, channelID, userID)
	if lookupErr != nil {
		return FeishuChannelCategories{}, lookupErr
	}
	if record == nil {
		return FeishuChannelCategories{}, ErrChannelNotFound
	}
	if record.Type != feishuAppChannelType {
		return FeishuChannelCategories{}, ErrChannelInvalidInput
	}
	return parseFeishuChannelCategories(record.Config)
}

// SetFeishuChannelCategories 更新飞书应用渠道的警告与普通提示开关，并保留其余配置字段。
// 仅允许 type=feishu_app；写入失败或配置无效时返回不包含配置值的错误。
func (s *ChannelService) SetFeishuChannelCategories(ctx context.Context, userID, channelID int64, categories FeishuChannelCategories) error {
	if s == nil || s.repository == nil || userID <= 0 || channelID <= 0 {
		return ErrChannelInvalidInput
	}
	// record、lookupErr 保存归属校验后的渠道完整记录及读取错误；Config 只在本方法内合并。
	record, lookupErr := s.repository.GetChannelForUpdate(ctx, channelID, userID)
	if lookupErr != nil {
		return lookupErr
	}
	if record == nil {
		return ErrChannelNotFound
	}
	if record.Type != feishuAppChannelType {
		return ErrChannelInvalidInput
	}
	// updatedConfig、updateErr 保存只修改两个开关后的完整配置 JSON 及合并错误。
	updatedConfig, updateErr := updateFeishuChannelCategories(record.Config, categories)
	if updateErr != nil {
		return updateErr
	}
	record.Config = updatedConfig
	record.UserID = userID
	return s.repository.UpdateChannel(ctx, userID, *record)
}

// parseFeishuChannelCategories 解析渠道配置中的两个分类开关；字段缺失或为 null 时按 true 处理。
func parseFeishuChannelCategories(config string) (FeishuChannelCategories, error) {
	// fields 保存配置对象的原始 JSON 字段；未知字段只用于保留，不进入响应。
	var fields map[string]json.RawMessage
	// unmarshalErr 是配置对象解析失败原因；nil 表示配置是 JSON 对象。
	if unmarshalErr := json.Unmarshal([]byte(config), &fields); unmarshalErr != nil || fields == nil {
		return FeishuChannelCategories{}, fmt.Errorf("飞书渠道配置无法解析")
	}
	// warning、warningErr 保存警告类开关及解析错误。
	warning, warningErr := parseFeishuCategorySwitch(fields, "notify_warning")
	if warningErr != nil {
		return FeishuChannelCategories{}, warningErr
	}
	// info、infoErr 保存普通提示类开关及解析错误。
	info, infoErr := parseFeishuCategorySwitch(fields, "notify_info")
	if infoErr != nil {
		return FeishuChannelCategories{}, infoErr
	}
	return FeishuChannelCategories{Warning: warning, Info: info}, nil
}

// parseFeishuCategorySwitch 读取单个布尔开关；缺失、空值或 null 按 true 处理，类型错误返回稳定错误。
func parseFeishuCategorySwitch(fields map[string]json.RawMessage, key string) (bool, error) {
	// raw、exists 保存配置字段原始值和是否存在。
	raw, exists := fields[key]
	if !exists || len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return true, nil
	}
	// enabled 保存解析后的布尔开关值。
	var enabled bool
	// unmarshalErr 是布尔开关解析失败原因；nil 表示字段类型合法。
	if unmarshalErr := json.Unmarshal(raw, &enabled); unmarshalErr != nil {
		return false, fmt.Errorf("飞书渠道分类开关无效")
	}
	return enabled, nil
}

// updateFeishuChannelCategories 在原配置对象上只覆盖两个开关，保留 app_secret、open_id、domain 和未知字段。
func updateFeishuChannelCategories(config string, categories FeishuChannelCategories) (string, error) {
	// fields 保存配置对象的原始 JSON 字段，确保未修改字段按原值写回。
	var fields map[string]json.RawMessage
	// unmarshalErr 是配置对象解析失败原因；nil 表示配置是 JSON 对象。
	if unmarshalErr := json.Unmarshal([]byte(config), &fields); unmarshalErr != nil || fields == nil {
		return "", fmt.Errorf("飞书渠道配置无法解析")
	}
	// warningRaw、warningErr 保存警告类开关的 JSON 布尔值及序列化错误。
	warningRaw, warningErr := json.Marshal(categories.Warning)
	if warningErr != nil {
		return "", fmt.Errorf("编码飞书渠道分类开关失败")
	}
	// infoRaw、infoErr 保存普通提示类开关的 JSON 布尔值及序列化错误。
	infoRaw, infoErr := json.Marshal(categories.Info)
	if infoErr != nil {
		return "", fmt.Errorf("编码飞书渠道分类开关失败")
	}
	fields["notify_warning"] = warningRaw
	fields["notify_info"] = infoRaw
	// encoded、encodeErr 保存合并后的完整配置 JSON 与序列化错误；内容只用于持久化。
	encoded, encodeErr := json.Marshal(fields)
	if encodeErr != nil {
		return "", fmt.Errorf("编码飞书渠道配置失败")
	}
	return string(encoded), nil
}

// mergePollDomain 合并锁内复制的当前域名与轮询结果域名，保证 Lark 识别后不回退到飞书。
func mergePollDomain(currentDomain, nextDomain string) string {
	// current 是归一后的当前站点域名。
	current := normalizeFeishuDomain(currentDomain)
	if current == "lark" || strings.EqualFold(strings.TrimSpace(nextDomain), "lark") {
		return "lark"
	}
	return normalizeFeishuDomain(nextDomain)
}
