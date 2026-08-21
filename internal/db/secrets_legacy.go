package db

import (
	"context"
	"database/sql"
	"fmt"
)

// legacyCookieSecret 保存历史账号凭证和登录元数据，迁移时只在事务内部使用。
type legacyCookieSecret struct {
	// id 是账号稳定标识。
	id string
	// value 是闲鱼 Cookie 或其密文。
	value string
	// password 是密码登录秘密或其密文。
	password string
	// metadata 是账号登录元数据或其密文。
	metadata string
}

// migrateLegacyCookies 加密并校验历史账号 Cookie、密码和登录元数据。
func migrateLegacyCookies(ctx context.Context, tx *sql.Tx, codec *secretCodec) error {
	// rows、err 保存账号秘密查询游标及查询错误。
	rows, err := tx.QueryContext(ctx, `SELECT id,value,COALESCE(password,''),COALESCE(metadata_json,'') FROM cookies`)
	if err != nil {
		return err
	}
	defer rows.Close()
	// cookies 保存待迁移的账号秘密。
	var cookies []legacyCookieSecret
	for rows.Next() {
		// row 保存当前账号的全部敏感字段。
		var row legacyCookieSecret
		// err 表示账号秘密扫描错误。
		if err := rows.Scan(&row.id, &row.value, &row.password, &row.metadata); err != nil {
			return err
		}
		cookies = append(cookies, row)
	}
	// err 表示账号秘密查询游标遍历错误。
	if err := rows.Err(); err != nil {
		return err
	}
	// row 表示当前需要加密或校验的账号秘密。
	for _, row := range cookies {
		// value、err 保存 Cookie 密文及加密错误。
		value, err := codec.encrypt("cookie", row.id, row.value)
		if err != nil {
			return fmt.Errorf("校验账号 %s Cookie: %w", row.id, err)
		}
		// password、err 保存登录密码密文及加密错误。
		password, err := codec.encrypt("login-password", row.id, row.password)
		if err != nil {
			return fmt.Errorf("校验账号 %s 登录密码: %w", row.id, err)
		}
		// metadata、err 保存登录元数据密文及加密错误。
		metadata, err := codec.encrypt(cookieMetadataScope, row.id, row.metadata)
		if err != nil {
			return fmt.Errorf("校验账号 %s Cookie metadata: %w", row.id, err)
		}
		if value != row.value || password != row.password || metadata != row.metadata {
			// err 表示回写账号敏感字段时的数据库错误。
			if _, err := tx.ExecContext(ctx, `UPDATE cookies SET value=?,password=?,metadata_json=? WHERE id=?`, value, password, metadata, row.id); err != nil {
				return err
			}
		}
	}
	return nil
}

// legacyTokenSecret 保存历史账号令牌，迁移时只在事务内部使用。
type legacyTokenSecret struct {
	// cookieID 是令牌所属账号标识。
	cookieID string
	// deviceID 是设备标识或其密文。
	deviceID string
	// accessToken 是访问令牌或其密文。
	accessToken string
}

// migrateLegacyTokens 加密并校验历史账号设备标识和访问令牌。
func migrateLegacyTokens(ctx context.Context, tx *sql.Tx, codec *secretCodec) error {
	// rows、err 保存令牌查询游标及查询错误。
	rows, err := tx.QueryContext(ctx, `SELECT cookie_id,device_id,access_token FROM account_tokens`)
	if err != nil {
		return err
	}
	defer rows.Close()
	// tokens 保存待迁移的账号令牌。
	var tokens []legacyTokenSecret
	for rows.Next() {
		// row 保存当前账号令牌。
		var row legacyTokenSecret
		// err 表示账号令牌扫描错误。
		if err := rows.Scan(&row.cookieID, &row.deviceID, &row.accessToken); err != nil {
			return err
		}
		tokens = append(tokens, row)
	}
	// err 表示账号令牌查询游标遍历错误。
	if err := rows.Err(); err != nil {
		return err
	}
	// row 表示当前需要加密或校验的账号令牌。
	for _, row := range tokens {
		// deviceID、err 保存设备标识密文及加密错误。
		deviceID, err := codec.encrypt("device-id", row.cookieID, row.deviceID)
		if err != nil {
			return err
		}
		// accessToken、err 保存访问令牌密文及加密错误。
		accessToken, err := codec.encrypt("access-token", row.cookieID, row.accessToken)
		if err != nil {
			return err
		}
		if deviceID != row.deviceID || accessToken != row.accessToken {
			// err 表示回写账号令牌时的数据库错误。
			if _, err := tx.ExecContext(ctx, `UPDATE account_tokens SET device_id=?,access_token=? WHERE cookie_id=?`, deviceID, accessToken, row.cookieID); err != nil {
				return err
			}
		}
	}
	return nil
}

// migrateLegacySettings 加密并校验历史敏感系统设置。
func migrateLegacySettings(ctx context.Context, tx *sql.Tx, codec *secretCodec, dialect Dialect) error {
	// keyCol 是当前数据库方言的设置键列引用。
	keyCol := dialectQuote(dialect, "key")
	// rows、err 保存系统设置查询游标及查询错误。
	rows, err := tx.QueryContext(ctx, `SELECT `+keyCol+`,value FROM system_settings`)
	if err != nil {
		return err
	}
	defer rows.Close()
	// settings 保存需要迁移的敏感设置。
	settings := make(map[string]string)
	for rows.Next() {
		// key、value 保存当前系统设置键和值。
		var key, value string
		// err 表示系统设置扫描错误。
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		if isSensitiveSettingKey(key) {
			settings[key] = value
		}
	}
	// err 表示系统设置查询游标遍历错误。
	if err := rows.Err(); err != nil {
		return err
	}
	// key、value 表示当前需要迁移的敏感设置。
	for key, value := range settings {
		// encrypted、err 保存设置密文及加密错误。
		encrypted, err := codec.encrypt("system-setting", key, value)
		if err != nil {
			return err
		}
		if encrypted != value {
			// err 表示回写系统设置时的数据库错误。
			if _, err := tx.ExecContext(ctx, `UPDATE system_settings SET value=? WHERE `+keyCol+`=?`, encrypted, key); err != nil {
				return err
			}
		}
	}
	return nil
}

// legacyNotificationChannel 保存历史通知渠道的所属用户和完整配置。
type legacyNotificationChannel struct {
	// id 是渠道数据库主键。
	id int64
	// userID 是渠道所属用户。
	userID int64
	// config 是渠道配置或其密文。
	config string
}

// migrateLegacyNotificationChannels 加密并校验历史通知渠道配置。
func migrateLegacyNotificationChannels(ctx context.Context, tx *sql.Tx, codec *secretCodec) error {
	// rows、err 保存通知渠道查询游标及查询错误。
	rows, err := tx.QueryContext(ctx, `SELECT id,COALESCE(user_id,1),config FROM notification_channels`)
	if err != nil {
		return err
	}
	defer rows.Close()
	// channels 保存待迁移的通知渠道。
	var channels []legacyNotificationChannel
	for rows.Next() {
		// row 保存当前通知渠道的敏感字段。
		var row legacyNotificationChannel
		// err 表示通知渠道扫描错误。
		if err := rows.Scan(&row.id, &row.userID, &row.config); err != nil {
			return err
		}
		channels = append(channels, row)
	}
	// err 表示通知渠道查询游标遍历错误。
	if err := rows.Err(); err != nil {
		return err
	}
	// row 表示当前需要加密或校验的通知渠道。
	for _, row := range channels {
		// encrypted、err 保存渠道密文及加密错误。
		encrypted, err := codec.encrypt("notification-config", fmt.Sprint(row.userID), row.config)
		if err != nil {
			return err
		}
		if encrypted != row.config {
			// err 表示回写通知渠道配置时的数据库错误。
			if _, err := tx.ExecContext(ctx, `UPDATE notification_channels SET config=? WHERE id=?`, encrypted, row.id); err != nil {
				return err
			}
		}
	}
	return nil
}
