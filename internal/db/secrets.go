package db

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// encryptedValuePrefix 用于本次流程后续判断的encrypted值Prefix
const (
	encryptedValuePrefix = "enc:v1:"
	cookieMetadataScope  = "cookie-metadata"
)

// secretCodec 对数据库敏感字段做 AES-256-GCM 信封加密。未配置密钥时保持
// 明文兼容；已加密数据若缺少/使用错误密钥会明确报错，绝不把密文当凭证使用。
// secretCodec 用于本次流程后续判断的secretCodec
type secretCodec struct{ aead cipher.AEAD }

// secretCodecFromEnvironment 封装secretCodecFromEnvironment业务协调。
func secretCodecFromEnvironment() *secretCodec {
	// codec 用于本次流程后续判断的codec
	codec, _ := newSecretCodec(strings.TrimSpace(os.Getenv("XIANYU_DATA_KEY")))
	return codec
}

// newSecretCodec 封装newSecretCodec业务协调。
func newSecretCodec(key string) (*secretCodec, error) {
	// codec 用于本次流程后续判断的codec
	codec := &secretCodec{}
	if key == "" {
		return codec, nil
	}
	// digest 用于本次流程后续判断的digest
	digest := sha256.Sum256([]byte(key))
	// block、err 用于本次流程后续判断的block、err
	block, err := aes.NewCipher(digest[:])
	if err != nil {
		return nil, err
	}
	// aead、err 用于本次流程后续判断的aead、err
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	codec.aead = aead
	return codec, nil
}

// encrypt 封装encrypt业务协调。
func (c *secretCodec) encrypt(scope, owner, value string) (string, error) {
	if value == "" {
		return value, nil
	}
	if strings.HasPrefix(value, encryptedValuePrefix) {
		if // err 用于本次流程后续判断的err
		_, err := c.decrypt(scope, owner, value); err != nil {
			return "", err
		}
		return value, nil
	}
	if c == nil || c.aead == nil {
		return value, nil
	}
	// nonce 用于本次流程后续判断的nonce
	nonce := make([]byte, c.aead.NonceSize())
	if // err 用于本次流程后续判断的err
	_, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	// sealed 用于本次流程后续判断的sealed
	sealed := c.aead.Seal(nonce, nonce, []byte(value), []byte(scope+"\x00"+owner))
	return encryptedValuePrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// EncryptLegacySecrets 将启用 XIANYU_DATA_KEY 前写入的明文敏感字段原地升级。
// 已加密行同时用于校验当前密钥，错误密钥会在启动业务 worker 前失败。
// EncryptLegacySecrets 封装EncryptLegacySecrets业务协调。
func (s *Store) EncryptLegacySecrets(ctx context.Context) error {
	// codec、tx、err 是本次迁移使用的加密器、唯一事务边界及其打开错误。
	codec, tx, err := s.beginSecretMigration(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return nil
	}
	defer tx.Rollback()
	// cookieSecret 用于本次流程后续判断的登录凭证Secret
	type cookieSecret struct{ id, value, password, metadata string }
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := tx.QueryContext(ctx, `SELECT id,value,COALESCE(password,''),COALESCE(metadata_json,'') FROM cookies`)
	if err != nil {
		return err
	}
	// cookies 用于本次流程后续判断的cookies
	var cookies []cookieSecret
	for rows.Next() {
		// row 用于本次流程后续判断的row
		var row cookieSecret
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&row.id, &row.value, &row.password, &row.metadata); err != nil {
			rows.Close()
			return err
		}
		cookies = append(cookies, row)
	}
	if // err 用于本次流程后续判断的err
	err := rows.Close(); err != nil {
		return err
	}
	// row 表示当前遍历过程中的row
	for _, row := range cookies {
		// value、err 用于本次流程后续判断的value、err
		value, err := codec.encrypt("cookie", row.id, row.value)
		if err != nil {
			return fmt.Errorf("校验账号 %s Cookie: %w", row.id, err)
		}
		// password、err 用于本次流程后续判断的password、err
		password, err := codec.encrypt("login-password", row.id, row.password)
		if err != nil {
			return fmt.Errorf("校验账号 %s 登录密码: %w", row.id, err)
		}
		// metadata、err 用于本次流程后续判断的metadata、err
		metadata, err := codec.encrypt(cookieMetadataScope, row.id, row.metadata)
		if err != nil {
			return fmt.Errorf("校验账号 %s Cookie metadata: %w", row.id, err)
		}
		if value != row.value || password != row.password || metadata != row.metadata {
			if // err 用于本次流程后续判断的err
			_, err := tx.ExecContext(ctx, `UPDATE cookies SET value=?,password=?,metadata_json=? WHERE id=?`, value, password, metadata, row.id); err != nil {
				return err
			}
		}
	}
	// tokenSecret 用于本次流程后续判断的令牌Secret
	type tokenSecret struct{ cookieID, deviceID, accessToken string }
	rows, err = tx.QueryContext(ctx, `SELECT cookie_id,device_id,access_token FROM account_tokens`)
	if err != nil {
		return err
	}
	// tokens 用于本次流程后续判断的tokens
	var tokens []tokenSecret
	for rows.Next() {
		// row 用于本次流程后续判断的row
		var row tokenSecret
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&row.cookieID, &row.deviceID, &row.accessToken); err != nil {
			rows.Close()
			return err
		}
		tokens = append(tokens, row)
	}
	if // err 用于本次流程后续判断的err
	err := rows.Close(); err != nil {
		return err
	}
	// row 表示当前遍历过程中的row
	for _, row := range tokens {
		// deviceID、err 用于本次流程后续判断的deviceID、err
		deviceID, err := codec.encrypt("device-id", row.cookieID, row.deviceID)
		if err != nil {
			return err
		}
		// accessToken、err 用于本次流程后续判断的accessToken、err
		accessToken, err := codec.encrypt("access-token", row.cookieID, row.accessToken)
		if err != nil {
			return err
		}
		if deviceID != row.deviceID || accessToken != row.accessToken {
			if // err 用于本次流程后续判断的err
			_, err := tx.ExecContext(ctx, `UPDATE account_tokens SET device_id=?,access_token=? WHERE cookie_id=?`, deviceID, accessToken, row.cookieID); err != nil {
				return err
			}
		}
	}

	// keyCol 用于本次流程后续判断的keyCol
	keyCol := dialectQuote(s.Dialect, "key")
	rows, err = tx.QueryContext(ctx, `SELECT `+keyCol+`,value FROM system_settings`)
	if err != nil {
		return err
	}
	// settings 用于本次流程后续判断的设置
	settings := make(map[string]string)
	for rows.Next() {
		// key、value 用于本次流程后续判断的key、value
		var key, value string
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&key, &value); err != nil {
			rows.Close()
			return err
		}
		if isSensitiveSettingKey(key) {
			settings[key] = value
		}
	}
	if // err 用于本次流程后续判断的err
	err := rows.Close(); err != nil {
		return err
	}
	// key、value 表示当前遍历过程中的key、value
	for key, value := range settings {
		// encrypted、err 用于本次流程后续判断的encrypted、err
		encrypted, err := codec.encrypt("system-setting", key, value)
		if err != nil {
			return err
		}
		if encrypted != value {
			if // err 用于本次流程后续判断的err
			_, err := tx.ExecContext(ctx, `UPDATE system_settings SET value=? WHERE `+keyCol+`=?`, encrypted, key); err != nil {
				return err
			}
		}
	}

	// channelSecret 用于本次流程后续判断的渠道Secret
	type channelSecret struct {
		id, userID int64
		config     string
	}
	rows, err = tx.QueryContext(ctx, `SELECT id,COALESCE(user_id,1),config FROM notification_channels`)
	if err != nil {
		return err
	}
	// channels 用于本次流程后续判断的渠道列表
	var channels []channelSecret
	for rows.Next() {
		// row 用于本次流程后续判断的row
		var row channelSecret
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&row.id, &row.userID, &row.config); err != nil {
			rows.Close()
			return err
		}
		channels = append(channels, row)
	}
	if // err 用于本次流程后续判断的err
	err := rows.Close(); err != nil {
		return err
	}
	// row 表示当前遍历过程中的row
	for _, row := range channels {
		// encrypted、err 用于本次流程后续判断的encrypted、err
		encrypted, err := codec.encrypt("notification-config", fmt.Sprint(row.userID), row.config)
		if err != nil {
			return err
		}
		if encrypted != row.config {
			if // err 用于本次流程后续判断的err
			_, err := tx.ExecContext(ctx, `UPDATE notification_channels SET config=? WHERE id=?`, encrypted, row.id); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// beginSecretMigration 在不暴露部分可见状态前创建敏感字段升级所需的单一数据库事务。
func (s *Store) beginSecretMigration(ctx context.Context) (*secretCodec, *sql.Tx, error) {
	if s == nil || s.DB == nil {
		return nil, nil, nil
	}
	// codec 是 Store 已配置的数据密钥编解码器；tx 是覆盖所有旧凭证升级的原子事务。
	codec := s.Cookies.codec
	// tx 是覆盖全部旧秘密字段的原子事务；err 保存事务创建失败原因。
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	return codec, tx, nil
}

// decrypt 封装decrypt业务协调。
func (c *secretCodec) decrypt(scope, owner, value string) (string, error) {
	if !strings.HasPrefix(value, encryptedValuePrefix) {
		return value, nil
	}
	if c == nil || c.aead == nil {
		return "", errors.New("数据库包含加密凭证，但 XIANYU_DATA_KEY 未配置")
	}
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, encryptedValuePrefix))
	if err != nil || len(raw) < c.aead.NonceSize() {
		return "", fmt.Errorf("敏感字段密文格式无效")
	}
	// nonce 用于本次流程后续判断的nonce
	nonce := raw[:c.aead.NonceSize()]
	// plain、err 用于本次流程后续判断的plain、err
	plain, err := c.aead.Open(nil, nonce, raw[c.aead.NonceSize():], []byte(scope+"\x00"+owner))
	if err != nil {
		return "", errors.New("敏感字段解密失败，请检查 XIANYU_DATA_KEY")
	}
	return string(plain), nil
}

// isSensitiveSettingKey 封装isSensitive设置Key业务协调。
func isSensitiveSettingKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "ai_api_key", "smtp_password", "qq_reply_secret_key", "captcha.remote_secret_key":
		return true
	default:
		return false
	}
}

// IsSensitiveSettingKey 判断系统设置键是否属于必须隔离处理的敏感配置。
func IsSensitiveSettingKey(key string) bool {
	return isSensitiveSettingKey(key)
}

// SensitiveSettingKeys 返回全部敏感系统设置键名，调用方可用于访问审计且不包含秘密值。
func SensitiveSettingKeys() []string {
	return []string{"ai_api_key", "smtp_password", "qq_reply_secret_key", "captcha.remote_secret_key"}
}
