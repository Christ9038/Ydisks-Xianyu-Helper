package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// AccountToken 持久化最近一次页面运行实例的 device_id 和 token 请求元数据。
// accessToken 不用于后续 WebSocket 注册；官方消息页在每次 loginV2/reConnect
// 前都会重新获取 token，页面重载后也会生成新的 device_id。
// AccountToken 保存账号令牌，供当前处理流程使用
type AccountToken struct {
	CookieID          string
	DeviceID          string
	AccessToken       string
	ExpireAt          int64 // unix 秒，0 表示无有效 token
	CookieFingerprint string
}

// AccountTokens 读写 account_tokens 表。
type AccountTokens struct {
	DB      *sql.DB
	Dialect Dialect
	codec   *secretCodec
}

// Get 取账号缓存的 device_id + accessToken。无记录返回 ErrNotFound。
func (t *AccountTokens) Get(ctx context.Context, cookieID string) (AccountToken, error) {
	// tk 保存tk，供当前处理流程使用
	var tk AccountToken
	tk.CookieID = cookieID
	// err 保存err，供当前处理流程使用
	err := t.DB.QueryRowContext(ctx,
		`SELECT device_id, access_token, expire_at, cookie_fingerprint FROM account_tokens WHERE cookie_id=?`,
		cookieID).Scan(&tk.DeviceID, &tk.AccessToken, &tk.ExpireAt, &tk.CookieFingerprint)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AccountToken{}, ErrNotFound
		}
		return AccountToken{}, err
	}
	tk.DeviceID, err = t.codec.decrypt("device-id", cookieID, tk.DeviceID)
	if err != nil {
		return AccountToken{}, err
	}
	tk.AccessToken, err = t.codec.decrypt("access-token", cookieID, tk.AccessToken)
	if err != nil {
		return AccountToken{}, err
	}
	return tk, nil
}

// Save upsert 缓存的 device_id + accessToken + expire_at。
func (t *AccountTokens) Save(ctx context.Context, cookieID, deviceID, accessToken string, expireAt int64) error {
	return t.SaveBound(ctx, cookieID, deviceID, accessToken, expireAt, "")
}

// SaveBound persists an access token together with the page-runtime device ID
// and canonical Cookie state from which it was issued.
// SaveBound 保存Bound。
func (t *AccountTokens) SaveBound(ctx context.Context, cookieID, deviceID, accessToken string, expireAt int64, cookieFingerprint string) error {
	// encryptedDeviceID、err 保存encryptedDeviceID、err，供当前处理流程使用
	encryptedDeviceID, err := t.codec.encrypt("device-id", cookieID, deviceID)
	if err != nil {
		return err
	}
	// encryptedToken、err 保存encryptedToken、err，供当前处理流程使用
	encryptedToken, err := t.codec.encrypt("access-token", cookieID, accessToken)
	if err != nil {
		return err
	}
	_, err = t.DB.ExecContext(ctx,
		`INSERT INTO account_tokens (cookie_id, device_id, access_token, expire_at, cookie_fingerprint, updated_at)
		 VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`+
			dialectUpsert(t.Dialect, []string{"cookie_id"}, map[string]string{
				"device_id":          "excluded.device_id",
				"access_token":       "excluded.access_token",
				"expire_at":          "excluded.expire_at",
				"cookie_fingerprint": "excluded.cookie_fingerprint",
				"updated_at":         "CURRENT_TIMESTAMP",
			}),
		cookieID, encryptedDeviceID, encryptedToken, expireAt, cookieFingerprint)
	if err != nil {
		return fmt.Errorf("保存 account_tokens: %w", err)
	}
	return nil
}

// GetOrCreateDeviceID returns the permanent device ID for an account. The
// candidate is persisted only when the account has no identity yet.
// GetOrCreateDeviceID 读取OrCreateDeviceID。
func (t *AccountTokens) GetOrCreateDeviceID(ctx context.Context, cookieID, candidate string) (string, error) {
	if candidate == "" {
		return "", fmt.Errorf("device_id 不能为空")
	}
	// encryptedCandidate、err 保存encryptedCandidate、err，供当前处理流程使用
	encryptedCandidate, err := t.codec.encrypt("device-id", cookieID, candidate)
	if err != nil {
		return "", err
	}
	// Insert-once makes concurrent account starts converge on the same identity.
	// A normal upsert can let two starters each observe a different winning ID.
	if // err 保存err，供当前处理流程使用
	_, err := t.DB.ExecContext(ctx,
		dialectInsertIgnorePrefix(t.Dialect)+` INTO account_tokens (cookie_id, device_id, access_token, expire_at, updated_at)
		 VALUES (?, ?, '', 0, CURRENT_TIMESTAMP)`+dialectInsertIgnore(t.Dialect, []string{"cookie_id"}),
		cookieID, encryptedCandidate); err != nil {
		return "", fmt.Errorf("创建 account_tokens device_id: %w", err)
	}
	// Upgrade the only legacy state that had no identity. The conditional update
	// is also first-writer-wins under concurrent starts.
	if // err 保存err，供当前处理流程使用
	_, err := t.DB.ExecContext(ctx,
		`UPDATE account_tokens SET device_id=?, updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND device_id=''`,
		encryptedCandidate, cookieID); err != nil {
		return "", fmt.Errorf("补全 account_tokens device_id: %w", err)
	}
	// tk、err 保存tk、err，供当前处理流程使用
	tk, err := t.Get(ctx, cookieID)
	if err != nil {
		return "", err
	}
	return tk.DeviceID, nil
}

// Clear clears only the expiring access token. The permanent device_id row is
// retained across session expiry, login refresh, risk recovery and restarts.
// Clear 负责Clear相关处理。
func (t *AccountTokens) Clear(ctx context.Context, cookieID string) error {
	// encryptedToken、err 保存encryptedToken、err，供当前处理流程使用
	encryptedToken, err := t.codec.encrypt("access-token", cookieID, "")
	if err != nil {
		return err
	}
	_, err = t.DB.ExecContext(ctx,
		`UPDATE account_tokens SET access_token=?, expire_at=0, cookie_fingerprint='', updated_at=CURRENT_TIMESTAMP WHERE cookie_id=?`,
		encryptedToken, cookieID)
	return err
}
