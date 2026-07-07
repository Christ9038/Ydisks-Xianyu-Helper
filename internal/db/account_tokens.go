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
	return tk, nil
}

// Save upsert 缓存的 device_id + accessToken + expire_at。
func (t *AccountTokens) Save(ctx context.Context, cookieID, deviceID, accessToken string, expireAt int64) error {
	_, err := t.DB.ExecContext(ctx,
		`INSERT INTO account_tokens (cookie_id, device_id, access_token, expire_at, updated_at)
		 VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`+
			dialectUpsert(t.Dialect, []string{"cookie_id"}, map[string]string{
				"device_id":    "excluded.device_id",
				"access_token": "excluded.access_token",
				"expire_at":    "excluded.expire_at",
				"updated_at":   "CURRENT_TIMESTAMP",
			}),
		cookieID, deviceID, accessToken, expireAt)
	if err != nil {
		return fmt.Errorf("保存 account_tokens: %w", err)
	}
	return nil
}

// Clear 删除账号的 token 缓存（session 失效 / 短连接可疑失效时调用）。
func (t *AccountTokens) Clear(ctx context.Context, cookieID string) error {
	_, err := t.DB.ExecContext(ctx, `DELETE FROM account_tokens WHERE cookie_id=?`, cookieID)
	return err
}
