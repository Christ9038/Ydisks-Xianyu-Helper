package db

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	encryptedValuePrefix = "enc:v1:"
	cookieMetadataScope  = "cookie-metadata"
)

// secretCodec 对数据库敏感字段做 AES-256-GCM 信封加密。未配置密钥时保持
// 明文兼容；已加密数据若缺少/使用错误密钥会明确报错，绝不把密文当凭证使用。
type secretCodec struct{ aead cipher.AEAD }

func secretCodecFromEnvironment() *secretCodec {
	codec, _ := newSecretCodec(strings.TrimSpace(os.Getenv("XIANYU_DATA_KEY")))
	return codec
}

func newSecretCodec(key string) (*secretCodec, error) {
	codec := &secretCodec{}
	if key == "" {
		return codec, nil
	}
	digest := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(digest[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	codec.aead = aead
	return codec, nil
}

func (c *secretCodec) encrypt(scope, owner, value string) (string, error) {
	if value == "" {
		return value, nil
	}
	if strings.HasPrefix(value, encryptedValuePrefix) {
		if _, err := c.decrypt(scope, owner, value); err != nil {
			return "", err
		}
		return value, nil
	}
	if c == nil || c.aead == nil {
		return value, nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(value), []byte(scope+"\x00"+owner))
	return encryptedValuePrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// EncryptLegacySecrets 将启用 XIANYU_DATA_KEY 前写入的明文敏感字段原地升级。
// 已加密行同时用于校验当前密钥，错误密钥会在启动业务 worker 前失败。
func (s *Store) EncryptLegacySecrets(ctx context.Context) error {
	if s == nil || s.DB == nil {
		return nil
	}
	codec := s.Cookies.codec
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	type cookieSecret struct{ id, value, password, metadata string }
	rows, err := tx.QueryContext(ctx, `SELECT id,value,COALESCE(password,''),COALESCE(metadata_json,'') FROM cookies`)
	if err != nil {
		return err
	}
	var cookies []cookieSecret
	for rows.Next() {
		var row cookieSecret
		if err := rows.Scan(&row.id, &row.value, &row.password, &row.metadata); err != nil {
			rows.Close()
			return err
		}
		cookies = append(cookies, row)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, row := range cookies {
		value, err := codec.encrypt("cookie", row.id, row.value)
		if err != nil {
			return fmt.Errorf("校验账号 %s Cookie: %w", row.id, err)
		}
		password, err := codec.encrypt("login-password", row.id, row.password)
		if err != nil {
			return fmt.Errorf("校验账号 %s 登录密码: %w", row.id, err)
		}
		metadata, err := codec.encrypt(cookieMetadataScope, row.id, row.metadata)
		if err != nil {
			return fmt.Errorf("校验账号 %s Cookie metadata: %w", row.id, err)
		}
		if value != row.value || password != row.password || metadata != row.metadata {
			if _, err := tx.ExecContext(ctx, `UPDATE cookies SET value=?,password=?,metadata_json=? WHERE id=?`, value, password, metadata, row.id); err != nil {
				return err
			}
		}
	}

	type tokenSecret struct{ cookieID, deviceID, accessToken string }
	rows, err = tx.QueryContext(ctx, `SELECT cookie_id,device_id,access_token FROM account_tokens`)
	if err != nil {
		return err
	}
	var tokens []tokenSecret
	for rows.Next() {
		var row tokenSecret
		if err := rows.Scan(&row.cookieID, &row.deviceID, &row.accessToken); err != nil {
			rows.Close()
			return err
		}
		tokens = append(tokens, row)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, row := range tokens {
		deviceID, err := codec.encrypt("device-id", row.cookieID, row.deviceID)
		if err != nil {
			return err
		}
		accessToken, err := codec.encrypt("access-token", row.cookieID, row.accessToken)
		if err != nil {
			return err
		}
		if deviceID != row.deviceID || accessToken != row.accessToken {
			if _, err := tx.ExecContext(ctx, `UPDATE account_tokens SET device_id=?,access_token=? WHERE cookie_id=?`, deviceID, accessToken, row.cookieID); err != nil {
				return err
			}
		}
	}

	keyCol := dialectQuote(s.Dialect, "key")
	rows, err = tx.QueryContext(ctx, `SELECT `+keyCol+`,value FROM system_settings`)
	if err != nil {
		return err
	}
	settings := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			rows.Close()
			return err
		}
		if isSensitiveSettingKey(key) {
			settings[key] = value
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for key, value := range settings {
		encrypted, err := codec.encrypt("system-setting", key, value)
		if err != nil {
			return err
		}
		if encrypted != value {
			if _, err := tx.ExecContext(ctx, `UPDATE system_settings SET value=? WHERE `+keyCol+`=?`, encrypted, key); err != nil {
				return err
			}
		}
	}

	type channelSecret struct {
		id, userID int64
		config     string
	}
	rows, err = tx.QueryContext(ctx, `SELECT id,COALESCE(user_id,1),config FROM notification_channels`)
	if err != nil {
		return err
	}
	var channels []channelSecret
	for rows.Next() {
		var row channelSecret
		if err := rows.Scan(&row.id, &row.userID, &row.config); err != nil {
			rows.Close()
			return err
		}
		channels = append(channels, row)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, row := range channels {
		encrypted, err := codec.encrypt("notification-config", fmt.Sprint(row.userID), row.config)
		if err != nil {
			return err
		}
		if encrypted != row.config {
			if _, err := tx.ExecContext(ctx, `UPDATE notification_channels SET config=? WHERE id=?`, encrypted, row.id); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func (c *secretCodec) decrypt(scope, owner, value string) (string, error) {
	if !strings.HasPrefix(value, encryptedValuePrefix) {
		return value, nil
	}
	if c == nil || c.aead == nil {
		return "", errors.New("数据库包含加密凭证，但 XIANYU_DATA_KEY 未配置")
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, encryptedValuePrefix))
	if err != nil || len(raw) < c.aead.NonceSize() {
		return "", fmt.Errorf("敏感字段密文格式无效")
	}
	nonce := raw[:c.aead.NonceSize()]
	plain, err := c.aead.Open(nil, nonce, raw[c.aead.NonceSize():], []byte(scope+"\x00"+owner))
	if err != nil {
		return "", errors.New("敏感字段解密失败，请检查 XIANYU_DATA_KEY")
	}
	return string(plain), nil
}

func isSensitiveSettingKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "ai_api_key", "smtp_password", "qq_reply_secret_key", "captcha.remote_secret_key":
		return true
	default:
		return false
	}
}
