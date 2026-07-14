package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// AccountToken 持久化的账号登录凭证缓存（device_id + accessToken）。
//
// 设计动机（参考 xianyu-auto-reply 的 xy_token_cache）：
//   - device_id 跨进程重启复用，避免每次重启换新设备 ID 触发阿里端设备绑定/风控；
//   - accessToken 缓存后，短重启可复用、瞬时 mtop 失败可回退到上次有效 token，不掉线。
type AccountToken struct {
	CookieID    string
	DeviceID    string
	AccessToken string
	ExpireAt    int64 // unix 秒，0 表示无有效 token
}

// AccountTokens 读写 account_tokens 表。
type AccountTokens struct {
	DB      *sql.DB
	Dialect Dialect
	codec   *secretCodec
}

// Get 取账号缓存的 device_id + accessToken。无记录返回 ErrNotFound。
func (t *AccountTokens) Get(ctx context.Context, cookieID string) (AccountToken, error) {
	var tk AccountToken
	tk.CookieID = cookieID
	err := t.DB.QueryRowContext(ctx,
		`SELECT device_id, access_token, expire_at FROM account_tokens WHERE cookie_id=?`,
		cookieID).Scan(&tk.DeviceID, &tk.AccessToken, &tk.ExpireAt)
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
	// device_id is an account identity, not part of the expiring token cache.
	// Once stored, token refreshes must never replace it.
	if existing, err := t.Get(ctx, cookieID); err == nil && existing.DeviceID != "" {
		deviceID = existing.DeviceID
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	encryptedDeviceID, err := t.codec.encrypt("device-id", cookieID, deviceID)
	if err != nil {
		return err
	}
	encryptedToken, err := t.codec.encrypt("access-token", cookieID, accessToken)
	if err != nil {
		return err
	}
	_, err = t.DB.ExecContext(ctx,
		`INSERT INTO account_tokens (cookie_id, device_id, access_token, expire_at, updated_at)
		 VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`+
			dialectUpsert(t.Dialect, []string{"cookie_id"}, map[string]string{
				"device_id":    "excluded.device_id",
				"access_token": "excluded.access_token",
				"expire_at":    "excluded.expire_at",
				"updated_at":   "CURRENT_TIMESTAMP",
			}),
		cookieID, encryptedDeviceID, encryptedToken, expireAt)
	if err != nil {
		return fmt.Errorf("保存 account_tokens: %w", err)
	}
	return nil
}

// GetOrCreateDeviceID returns the permanent device ID for an account. The
// candidate is persisted only when the account has no identity yet.
func (t *AccountTokens) GetOrCreateDeviceID(ctx context.Context, cookieID, candidate string) (string, error) {
	if candidate == "" {
		return "", fmt.Errorf("device_id 不能为空")
	}
	encryptedCandidate, err := t.codec.encrypt("device-id", cookieID, candidate)
	if err != nil {
		return "", err
	}
	// Insert-once makes concurrent account starts converge on the same identity.
	// A normal upsert can let two starters each observe a different winning ID.
	if _, err := t.DB.ExecContext(ctx,
		dialectInsertIgnorePrefix(t.Dialect)+` INTO account_tokens (cookie_id, device_id, access_token, expire_at, updated_at)
		 VALUES (?, ?, '', 0, CURRENT_TIMESTAMP)`+dialectInsertIgnore(t.Dialect, []string{"cookie_id"}),
		cookieID, encryptedCandidate); err != nil {
		return "", fmt.Errorf("创建 account_tokens device_id: %w", err)
	}
	// Upgrade the only legacy state that had no identity. The conditional update
	// is also first-writer-wins under concurrent starts.
	if _, err := t.DB.ExecContext(ctx,
		`UPDATE account_tokens SET device_id=?, updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND device_id=''`,
		encryptedCandidate, cookieID); err != nil {
		return "", fmt.Errorf("补全 account_tokens device_id: %w", err)
	}
	tk, err := t.Get(ctx, cookieID)
	if err != nil {
		return "", err
	}
	return tk.DeviceID, nil
}

// Clear clears only the expiring access token. The permanent device_id row is
// retained across session expiry, login refresh, risk recovery and restarts.
func (t *AccountTokens) Clear(ctx context.Context, cookieID string) error {
	encryptedToken, err := t.codec.encrypt("access-token", cookieID, "")
	if err != nil {
		return err
	}
	_, err = t.DB.ExecContext(ctx,
		`UPDATE account_tokens SET access_token=?, expire_at=0, updated_at=CURRENT_TIMESTAMP WHERE cookie_id=?`,
		encryptedToken, cookieID)
	return err
}
