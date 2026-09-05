package notify

import (
	"context"
	"errors"
	"time"

	"xianyu-go/internal/db"
)

// Repository 定义通知器读取渠道、系统 SMTP 配置并处理持久化 outbox 所需的最小能力。
type Repository interface {
	// AccountChannels 返回账号当前绑定的通知渠道。
	AccountChannels(ctx context.Context, cookieID string) ([]db.NotificationChannel, error)
	// EnqueueOutbox 将待发送通知追加到持久化 outbox。
	EnqueueOutbox(ctx context.Context, messages []db.NotificationOutboxInput) error
	// ClaimOutbox 抢占一批可发送的 outbox 消息。
	ClaimOutbox(ctx context.Context, workerToken string, now time.Time, limit int) ([]db.NotificationOutboxMessage, error)
	// GetChannel 查询通知渠道的完整配置。
	GetChannel(ctx context.Context, channelID int64) (*db.NotificationChannel, error)
	// CompleteOutbox 确认当前 worker 已完成 outbox 消息。
	CompleteOutbox(ctx context.Context, messageID int64, workerToken string) (bool, error)
	// MarkOutboxUncertain 隔离外部发送成功但本地确认失败的 outbox 消息。
	MarkOutboxUncertain(ctx context.Context, messageID int64, workerToken, lastError string) (bool, error)
	// RetryOutbox 写入 outbox 消息的重试状态。
	RetryOutbox(ctx context.Context, messageID int64, workerToken, lastError string, nextAttemptAt int64, permanent bool) (bool, error)
	// GetSetting 读取系统设置中的字符串值。
	GetSetting(ctx context.Context, key string) (string, error)
}

// userScopedSettings 是通知器按渠道所有者读取敏感系统设置的可选能力。
type userScopedSettings interface {
	// GetSettingForUser 读取指定用户可用的系统设置，并为敏感键写入访问审计。
	GetSettingForUser(ctx context.Context, userID int64, key string) (string, error)
}

// storeRepository 将完整 Store 适配为通知器窄 repository。
type storeRepository struct {
	// store 保存数据库聚合入口，仅在适配器内部使用。
	store *db.Store
	// cookieID 保存当前通知器绑定的账号标识，用于为系统敏感设置访问解析所有者并写入审计。
	cookieID string
}

// AccountChannels 委托账号通知渠道查询。
func (r storeRepository) AccountChannels(ctx context.Context, cookieID string) ([]db.NotificationChannel, error) {
	return r.store.Notifications.AccountChannels(ctx, cookieID)
}

// EnqueueOutbox 委托通知 outbox 写入。
func (r storeRepository) EnqueueOutbox(ctx context.Context, messages []db.NotificationOutboxInput) error {
	return r.store.Notifications.EnqueueOutbox(ctx, messages)
}

// ClaimOutbox 委托通知 outbox 抢占。
func (r storeRepository) ClaimOutbox(ctx context.Context, workerToken string, now time.Time, limit int) ([]db.NotificationOutboxMessage, error) {
	return r.store.Notifications.ClaimOutbox(ctx, workerToken, now, limit)
}

// GetChannel 委托通知渠道查询。
func (r storeRepository) GetChannel(ctx context.Context, channelID int64) (*db.NotificationChannel, error) {
	return r.store.Notifications.GetChannel(ctx, channelID)
}

// CompleteOutbox 委托通知 outbox 完成确认。
func (r storeRepository) CompleteOutbox(ctx context.Context, messageID int64, workerToken string) (bool, error) {
	return r.store.Notifications.CompleteOutbox(ctx, messageID, workerToken)
}

// MarkOutboxUncertain 委托通知 outbox 不确定状态隔离。
func (r storeRepository) MarkOutboxUncertain(ctx context.Context, messageID int64, workerToken, lastError string) (bool, error) {
	return r.store.Notifications.MarkOutboxUncertain(ctx, messageID, workerToken, lastError)
}

// RetryOutbox 委托通知 outbox 重试状态更新。
func (r storeRepository) RetryOutbox(ctx context.Context, messageID int64, workerToken, lastError string, nextAttemptAt int64, permanent bool) (bool, error) {
	return r.store.Notifications.RetryOutbox(ctx, messageID, workerToken, lastError, nextAttemptAt, permanent)
}

// GetSetting 委托系统设置读取。
func (r storeRepository) GetSetting(ctx context.Context, key string) (string, error) {
	if r.store.Settings == nil {
		return "", errors.New("系统设置 repository 未初始化")
	}
	if db.IsSensitiveSettingKey(key) {
		return r.store.ReadSensitiveSettingForAccount(ctx, r.cookieID, key, "settings.use", "notifications")
	}
	return r.store.Settings.Get(ctx, key)
}

// GetSettingForUser 按通知渠道所属用户读取系统设置，避免全局通知器丢失敏感设置所有者。
func (r storeRepository) GetSettingForUser(ctx context.Context, userID int64, key string) (string, error) {
	if r.store.Settings == nil {
		return "", errors.New("系统设置 repository 未初始化")
	}
	if db.IsSensitiveSettingKey(key) {
		return r.store.ReadSensitiveSetting(ctx, userID, key, "settings.use", "notifications")
	}
	return r.store.Settings.Get(ctx, key)
}

// newStoreRepository 从完整 Store 构造通知器使用的窄 repository。
func newStoreRepository(cookieID string, store *db.Store) Repository {
	if store == nil || store.Notifications == nil {
		return nil
	}
	return storeRepository{store: store, cookieID: cookieID}
}
