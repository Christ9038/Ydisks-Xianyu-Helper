package db

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestSensitiveRepositoriesEncryptAtRestAndDecryptOnRead 负责TestSensitiveRepositoriesEncryptAtRestAndDecryptOnRead相关处理。
func TestSensitiveRepositoriesEncryptAtRestAndDecryptOnRead(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "unit-test-data-key")
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	if // ok、err 保存ok、err，供当前处理流程使用
	ok, err := store.Users.Create(ctx, "secret-owner", "secret-owner@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, err)
	}
	// owner 保存所有者，供当前处理流程使用
	owner, _ := store.Users.GetByUsername(ctx, "secret-owner")
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.CreateOwned(ctx, "secret-cookie", "unb=secret; token=plain", owner.ID); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.UpdateLoginInfo(ctx, "secret-cookie", "login", "password-plain", false); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Tokens.Save(ctx, "secret-cookie", "device-plain", "access-plain", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Settings.Set(ctx, "ai_api_key", "sk-plain"); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Settings.Set(ctx, "captcha.remote_secret_key", "captcha-secret-plain"); err != nil {
		t.Fatal(err)
	}
	// channelID、err 保存渠道ID、err，供当前处理流程使用
	channelID, err := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{
		Name: "secret", Type: "webhook", Config: `{"webhook_url":"https://example.test/plain-token"}`, Enabled: true, UserID: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// rawCookie、rawPassword、rawDevice、rawToken、rawSetting、rawCaptchaSecret、rawConfig 保存原始Cookie、rawPassword、rawDevice、rawToken、rawSetting、rawCaptchaSecret、raw配置，供当前处理流程使用
	var rawCookie, rawPassword, rawDevice, rawToken, rawSetting, rawCaptchaSecret, rawConfig string
	if // err 保存err，供当前处理流程使用
	err := store.DB.QueryRowContext(ctx, `SELECT value,password FROM cookies WHERE id=?`, "secret-cookie").Scan(&rawCookie, &rawPassword); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.DB.QueryRowContext(ctx, `SELECT device_id,access_token FROM account_tokens WHERE cookie_id=?`, "secret-cookie").Scan(&rawDevice, &rawToken); err != nil {
		t.Fatal(err)
	}
	// keyCol 保存keyCol，供当前处理流程使用
	keyCol := dialectQuote(store.Dialect, "key")
	if // err 保存err，供当前处理流程使用
	err := store.DB.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE `+keyCol+`=?`, "ai_api_key").Scan(&rawSetting); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.DB.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE `+keyCol+`=?`, "captcha.remote_secret_key").Scan(&rawCaptchaSecret); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.DB.QueryRowContext(ctx, `SELECT config FROM notification_channels WHERE id=?`, channelID).Scan(&rawConfig); err != nil {
		t.Fatal(err)
	}
	// name、raw 表示当前遍历过程中的name、raw
	for name, raw := range map[string]string{"cookie": rawCookie, "password": rawPassword, "device": rawDevice, "token": rawToken, "setting": rawSetting, "captcha-secret": rawCaptchaSecret, "config": rawConfig} {
		if !strings.HasPrefix(raw, encryptedValuePrefix) || strings.Contains(raw, "plain") {
			t.Fatalf("%s was not encrypted at rest: %q", name, raw)
		}
	}

	// detail、err 保存detail、err，供当前处理流程使用
	detail, err := store.Cookies.GetDetails(ctx, "secret-cookie")
	if err != nil || detail.Value != "unb=secret; token=plain" || detail.Password != "password-plain" {
		t.Fatalf("cookie detail=%+v err=%v", detail, err)
	}
	// token、err 保存token、err，供当前处理流程使用
	token, err := store.Tokens.Get(ctx, "secret-cookie")
	if err != nil || token.DeviceID != "device-plain" || token.AccessToken != "access-plain" {
		t.Fatalf("token=%+v err=%v", token, err)
	}
	if // setting、err 保存setting、err，供当前处理流程使用
	setting, err := store.Settings.Get(ctx, "ai_api_key"); err != nil || setting != "sk-plain" {
		t.Fatalf("setting=%q err=%v", setting, err)
	}
	if // setting、err 保存setting、err，供当前处理流程使用
	setting, err := store.Settings.Get(ctx, "captcha.remote_secret_key"); err != nil || setting != "captcha-secret-plain" {
		t.Fatalf("captcha secret=%q err=%v", setting, err)
	}
	// channel、err 保存channel、err，供当前处理流程使用
	channel, err := store.Notifications.GetChannel(ctx, channelID)
	if err != nil || channel == nil || !strings.Contains(channel.Config, "plain-token") {
		t.Fatalf("channel=%+v err=%v", channel, err)
	}
}

// TestSystemSettingsRedactedNeverReturnsSensitivePlaintext 验证管理端设置视图只返回敏感配置状态。
func TestSystemSettingsRedactedNeverReturnsSensitivePlaintext(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "redacted-test-key")
	// store、cleanup 是测试数据库及其清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是当前测试使用的请求上下文。
	ctx := context.Background()
	// err 是批量保存设置时返回的错误。
	if err := store.Settings.SetMany(ctx, map[string]string{
		"theme_color":               "blue",
		"ai_api_key":                "sk-never-return",
		"smtp_password":             "smtp-never-return",
		"captcha.remote_secret_key": "captcha-never-return",
	}); err != nil {
		t.Fatal(err)
	}

	// redacted、err 是脱敏设置视图及其读取错误。
	redacted, err := store.Settings.Redacted(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if redacted["theme_color"] != "blue" {
		t.Fatalf("普通配置未返回: %#v", redacted)
	}
	for _, secret := range []string{"ai_api_key", "smtp_password", "captcha.remote_secret_key"} { // secret 是待验证的敏感配置键。
		// ok 表示脱敏响应是否意外包含敏感键。
		if _, ok := redacted[secret]; ok {
			t.Fatalf("敏感配置明文不应返回: key=%s value=%q", secret, redacted[secret])
		}
		if redacted[secret+"_configured"] != "true" {
			t.Fatalf("敏感配置状态缺失: key=%s response=%#v", secret, redacted)
		}
	}
}

// TestSensitiveSettingEmptyValueClearsSecret 验证敏感设置显式提交空值时会删除密文。
func TestSensitiveSettingEmptyValueClearsSecret(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "clear-test-key")
	// store、cleanup 是测试数据库及其清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是当前测试使用的请求上下文。
	ctx := context.Background()
	// err 是首次写入敏感设置时返回的错误。
	if err := store.Settings.Set(ctx, "ai_api_key", "sk-to-clear"); err != nil {
		t.Fatal(err)
	}
	// err 是显式清除敏感设置时返回的错误。
	if err := store.Settings.Set(ctx, "ai_api_key", ""); err != nil {
		t.Fatal(err)
	}
	// value、err 是读取已清除敏感设置的结果。
	value, err := store.Settings.Get(ctx, "ai_api_key")
	if err != nil {
		t.Fatal(err)
	}
	if value != "" {
		t.Fatalf("敏感设置未清除: %q", value)
	}
	// redacted、err 是清除后的脱敏视图及其读取错误。
	redacted, err := store.Settings.Redacted(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if redacted["ai_api_key_configured"] != "" {
		t.Fatalf("清除后仍报告已配置: %#v", redacted)
	}
}

// TestSecretCodecReadsLegacyPlaintextAndRejectsWrongKey 负责TestSecretCodecReadsLegacyPlaintextAndRejectsWrongKey相关处理。
func TestSecretCodecReadsLegacyPlaintextAndRejectsWrongKey(t *testing.T) {
	// codec 保存codec，供当前处理流程使用
	codec, _ := newSecretCodec("correct")
	// encrypted、err 保存encrypted、err，供当前处理流程使用
	encrypted, err := codec.encrypt("cookie", "owner", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if // plain、err 保存plain、err，供当前处理流程使用
	plain, err := codec.decrypt("cookie", "owner", "legacy-plaintext"); err != nil || plain != "legacy-plaintext" {
		t.Fatalf("legacy plain=%q err=%v", plain, err)
	}
	// wrong 保存wrong，供当前处理流程使用
	wrong, _ := newSecretCodec("wrong")
	if // err 保存err，供当前处理流程使用
	_, err := wrong.decrypt("cookie", "owner", encrypted); err == nil {
		t.Fatal("wrong key must not return ciphertext as plaintext")
	}
	// withoutKey 保存withoutKey，供当前处理流程使用
	withoutKey, _ := newSecretCodec("")
	if // err 保存err，供当前处理流程使用
	_, err := withoutKey.decrypt("cookie", "owner", encrypted); err == nil {
		t.Fatal("missing key must reject encrypted data")
	}
}

// TestEncryptLegacySecretsUpgradesPlaintextAndValidatesKey 负责TestEncryptLegacySecretsUpgradesPlaintextAndValidatesKey相关处理。
func TestEncryptLegacySecretsUpgradesPlaintextAndValidatesKey(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "migration-key")
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	if // ok、err 保存ok、err，供当前处理流程使用
	ok, err := store.Users.Create(ctx, "legacy-owner", "legacy-owner@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, err)
	}
	// owner 保存所有者，供当前处理流程使用
	owner, _ := store.Users.GetByUsername(ctx, "legacy-owner")
	if // err 保存err，供当前处理流程使用
	_, err := store.DB.ExecContext(ctx, `INSERT INTO cookies (id,value,user_id,password) VALUES (?,?,?,?)`, "legacy-secret", "legacy-cookie", owner.ID, "legacy-password"); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.EncryptLegacySecrets(ctx); err != nil {
		t.Fatal(err)
	}
	// raw 保存原始，供当前处理流程使用
	var raw string
	if // err 保存err，供当前处理流程使用
	err := store.DB.QueryRowContext(ctx, `SELECT value FROM cookies WHERE id='legacy-secret'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, encryptedValuePrefix) {
		t.Fatalf("legacy value was not upgraded: %q", raw)
	}
	// wrongCodec 保存wrongCodec，供当前处理流程使用
	wrongCodec, _ := newSecretCodec("wrong-key")
	// wrongStore 保存wrongStore，供当前处理流程使用
	wrongStore := NewStore(store.DB, store.Dialect)
	wrongStore.Cookies.codec = wrongCodec
	if // err 保存err，供当前处理流程使用
	err := wrongStore.EncryptLegacySecrets(ctx); err == nil {
		t.Fatal("startup validation must reject the wrong data key")
	}
}
