package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestSecretCodecWithoutConfiguredKeyRemainsUninitialized 验证缺少持久化密钥时编解码器不会生成临时密钥。
func TestSecretCodecWithoutConfiguredKeyRemainsUninitialized(t *testing.T) {
	// codec 保存未配置密钥时的静态编解码器。
	codec := secretCodecFromEnvironment()
	if codec.currentAEAD() != nil {
		t.Fatal("未配置数据密钥时不应生成临时加密实例")
	}
}

// TestReadSensitiveSettingForAccountAuditsWithoutSecret 验证账号运行时读取系统秘密前会审计且审计记录不含秘密值。
func TestReadSensitiveSettingForAccountAuditsWithoutSecret(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "audited-setting-key")
	// store、cleanup 保存审计读取测试使用的数据库及清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存当前敏感设置访问测试上下文。
	ctx := context.Background()
	// createErr 表示创建测试用户时返回的错误。
	if _, createErr := store.Users.Create(ctx, "audit-owner", "audit-owner@example.com", "pw"); createErr != nil {
		t.Fatal(createErr)
	}
	// owner、ownerErr 保存测试账号所有者及查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "audit-owner")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// saveErr 表示创建绑定所有者的测试账号时返回的错误。
	if saveErr := store.Cookies.Save(ctx, "audit-account", "unb=audit", owner.ID); saveErr != nil {
		t.Fatal(saveErr)
	}
	// setErr 表示写入待审计敏感设置时返回的错误。
	if setErr := store.Settings.Set(ctx, "ai_api_key", "audit-secret-value"); setErr != nil {
		t.Fatal(setErr)
	}
	// value、readErr 保存敏感设置明文及受控读取错误。
	value, readErr := store.ReadSensitiveSettingForAccount(ctx, "audit-account", "ai_api_key", "settings.use", "ai_reply")
	if readErr != nil || value != "audit-secret-value" {
		t.Fatalf("读取敏感设置失败: value=%q err=%v", value, readErr)
	}
	// records、listErr 保存按所有者读取的访问审计记录及查询错误。
	records, listErr := store.SecurityAudit.ListByUser(ctx, owner.ID, 10)
	if listErr != nil || len(records) != 1 {
		t.Fatalf("敏感设置访问审计记录异常: records=%+v err=%v", records, listErr)
	}
	if records[0].Action != "settings.use" || records[0].Resource != "ai_reply" || len(records[0].Keys) != 1 || records[0].Keys[0] != "ai_api_key" {
		t.Fatalf("审计上下文异常: %+v", records[0])
	}
	// rawKeys 保存数据库中的审计键 JSON，用于确认秘密值没有进入审计存储。
	var rawKeys string
	// queryErr 表示读取审计键 JSON 时返回的数据库错误。
	if queryErr := store.DB.QueryRowContext(ctx, `SELECT keys_json FROM security_audit_logs WHERE id=?`, records[0].ID).Scan(&rawKeys); queryErr != nil {
		t.Fatal(queryErr)
	}
	if strings.Contains(rawKeys, "audit-secret-value") {
		t.Fatalf("审计记录泄露敏感设置值: %q", rawKeys)
	}
}

// TestReadSensitiveSettingRejectsUnownedOrUnauditedAccess 验证敏感设置读取遇到无所有者、非敏感键或审计故障时拒绝继续。
func TestReadSensitiveSettingRejectsUnownedOrUnauditedAccess(t *testing.T) {
	// store、cleanup 保存拒绝路径测试使用的数据库及清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存当前拒绝路径测试上下文。
	ctx := context.Background()
	// missingErr 表示不存在账号的所有者查询错误。
	_, missingErr := store.ReadSensitiveSettingForAccount(ctx, "missing-account", "ai_api_key", "settings.use", "ai_reply")
	if !errors.Is(missingErr, ErrNotFound) {
		t.Fatalf("不存在账号应返回 ErrNotFound: %v", missingErr)
	}
	// invalidKeyErr 表示非敏感键被错误请求审计读取时的校验错误。
	_, invalidKeyErr := store.ReadSensitiveSetting(ctx, 1, "theme_color", "settings.use", "ai_reply")
	if invalidKeyErr == nil || !strings.Contains(invalidKeyErr.Error(), "敏感设置白名单") {
		t.Fatalf("非敏感键应被拒绝: %v", invalidKeyErr)
	}
	// invalidUserErr 表示缺少有效用户所有者时的校验错误。
	_, invalidUserErr := store.ReadSensitiveSetting(ctx, 0, "ai_api_key", "settings.use", "ai_reply")
	if !errors.Is(invalidUserErr, ErrInvalidUserID) {
		t.Fatalf("无效用户 ID 应被拒绝: %v", invalidUserErr)
	}
	// store.SecurityAudit 置空模拟审计依赖未装配，验证秘密读取不会绕过审计。
	store.SecurityAudit = nil
	// auditMissingErr 表示审计存储缺失时的拒绝错误。
	_, auditMissingErr := store.ReadSensitiveSetting(ctx, 1, "ai_api_key", "settings.use", "ai_reply")
	if auditMissingErr == nil || !strings.Contains(auditMissingErr.Error(), "审计") {
		t.Fatalf("审计存储缺失应拒绝读取: %v", auditMissingErr)
	}
	// store.SecurityAudit 恢复为无数据库连接的实例，模拟审计写入失败。
	store.SecurityAudit = &SecurityAuditLogs{}
	// auditWriteErr 表示审计写入失败时的拒绝错误。
	_, auditWriteErr := store.ReadSensitiveSetting(ctx, 1, "ai_api_key", "settings.use", "ai_reply")
	if auditWriteErr == nil || !strings.Contains(auditWriteErr.Error(), "记录敏感设置访问审计失败") {
		t.Fatalf("审计写入失败应拒绝读取: %v", auditWriteErr)
	}
}

// TestSensitiveRepositoriesEncryptAtRestAndDecryptOnRead 封装TestSensitiveRepositoriesEncryptAtRestAndDecryptOnRead业务协调。
func TestSensitiveRepositoriesEncryptAtRestAndDecryptOnRead(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "unit-test-data-key")
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // ok、err 用于本次流程后续判断的ok、err
	ok, err := store.Users.Create(ctx, "secret-owner", "secret-owner@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, err)
	}
	// owner 用于本次流程后续判断的所有者
	owner, _ := store.Users.GetByUsername(ctx, "secret-owner")
	if // err 用于本次流程后续判断的err
	err := store.Cookies.CreateOwned(ctx, "secret-cookie", "unb=secret; token=plain", owner.ID); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Cookies.UpdateLoginInfo(ctx, "secret-cookie", "login", "password-plain", false); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Tokens.Save(ctx, "secret-cookie", "device-plain", "access-plain", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Settings.Set(ctx, "ai_api_key", "sk-plain"); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Settings.Set(ctx, "captcha.remote_secret_key", "captcha-secret-plain"); err != nil {
		t.Fatal(err)
	}
	// channelID、err 用于本次流程后续判断的渠道ID、err
	channelID, err := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{
		Name: "secret", Type: "webhook", Config: `{"webhook_url":"https://example.test/plain-token"}`, Enabled: true, UserID: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// rawCookie、rawPassword、rawDevice、rawToken、rawSetting、rawCaptchaSecret、rawConfig 用于本次流程后续判断的原始Cookie、rawPassword、rawDevice、rawToken、rawSetting、rawCaptchaSecret、raw配置
	var rawCookie, rawPassword, rawDevice, rawToken, rawSetting, rawCaptchaSecret, rawConfig string
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT value,password FROM cookies WHERE id=?`, "secret-cookie").Scan(&rawCookie, &rawPassword); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT device_id,access_token FROM account_tokens WHERE cookie_id=?`, "secret-cookie").Scan(&rawDevice, &rawToken); err != nil {
		t.Fatal(err)
	}
	// keyCol 用于本次流程后续判断的keyCol
	keyCol := dialectQuote(store.Dialect, "key")
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE `+keyCol+`=?`, "ai_api_key").Scan(&rawSetting); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE `+keyCol+`=?`, "captcha.remote_secret_key").Scan(&rawCaptchaSecret); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT config FROM notification_channels WHERE id=?`, channelID).Scan(&rawConfig); err != nil {
		t.Fatal(err)
	}
	// name、raw 表示当前遍历过程中的name、raw
	for name, raw := range map[string]string{"cookie": rawCookie, "password": rawPassword, "device": rawDevice, "token": rawToken, "setting": rawSetting, "captcha-secret": rawCaptchaSecret, "config": rawConfig} {
		if !strings.HasPrefix(raw, encryptedValuePrefix) || strings.Contains(raw, "plain") {
			t.Fatalf("%s was not encrypted at rest: %q", name, raw)
		}
	}

	// detail、err 用于本次流程后续判断的detail、err
	detail, err := store.Cookies.GetDetails(ctx, "secret-cookie")
	if err != nil || detail.Value != "unb=secret; token=plain" || detail.Password != "password-plain" {
		t.Fatalf("cookie detail=%+v err=%v", detail, err)
	}
	// token、err 用于本次流程后续判断的token、err
	token, err := store.Tokens.Get(ctx, "secret-cookie")
	if err != nil || token.DeviceID != "device-plain" || token.AccessToken != "access-plain" {
		t.Fatalf("token=%+v err=%v", token, err)
	}
	if // setting、err 用于本次流程后续判断的setting、err
	setting, err := store.Settings.Get(ctx, "ai_api_key"); err != nil || setting != "sk-plain" {
		t.Fatalf("setting=%q err=%v", setting, err)
	}
	if // setting、err 用于本次流程后续判断的setting、err
	setting, err := store.Settings.Get(ctx, "captcha.remote_secret_key"); err != nil || setting != "captcha-secret-plain" {
		t.Fatalf("captcha secret=%q err=%v", setting, err)
	}
	// channel、err 用于本次流程后续判断的channel、err
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

// TestSystemSettingsApplyChangesUsesExplicitSecretCommands 验证敏感设置只能通过三态命令更新。
func TestSystemSettingsApplyChangesUsesExplicitSecretCommands(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "apply-change-test-key")
	// store、cleanup 是测试数据库及其清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是当前测试使用的请求上下文。
	ctx := context.Background()
	// err 是初始敏感设置写入错误。
	if err := store.Settings.Set(ctx, "ai_api_key", "before"); err != nil {
		t.Fatal(err)
	}
	// err 是 retain 命令应用错误。
	if err := store.Settings.ApplyChanges(ctx, map[string]string{"theme_color": "blue"}, map[string]SensitiveSettingChange{
		"ai_api_key": {Action: "retain"},
	}); err != nil {
		t.Fatal(err)
	}
	// value、err 是 retain 后的秘密读取结果及错误。
	if value, err := store.Settings.Get(ctx, "ai_api_key"); err != nil || value != "before" {
		t.Fatalf("retain value=%q err=%v", value, err)
	}
	// err 是 replace 命令应用错误。
	if err := store.Settings.ApplyChanges(ctx, nil, map[string]SensitiveSettingChange{
		"ai_api_key": {Action: "replace", Value: "after"},
	}); err != nil {
		t.Fatal(err)
	}
	// value、err 是 replace 后的秘密读取结果及错误。
	if value, err := store.Settings.Get(ctx, "ai_api_key"); err != nil || value != "after" {
		t.Fatalf("replace value=%q err=%v", value, err)
	}
	// err 是 clear 命令应用错误。
	if err := store.Settings.ApplyChanges(ctx, nil, map[string]SensitiveSettingChange{
		"ai_api_key": {Action: "clear"},
	}); err != nil {
		t.Fatal(err)
	}
	// value、err 是 clear 后的秘密读取结果及错误。
	if value, err := store.Settings.Get(ctx, "ai_api_key"); err != nil || value != "" {
		t.Fatalf("clear value=%q err=%v", value, err)
	}
	// err 是普通 values 误带敏感键时返回的拒绝错误。
	if err := store.Settings.ApplyChanges(ctx, map[string]string{"ai_api_key": "forbidden"}, nil); err == nil {
		t.Fatal("普通 values 不应接受敏感设置")
	}
	// err 是 replace 空秘密时返回的校验错误。
	if err := store.Settings.ApplyChanges(ctx, nil, map[string]SensitiveSettingChange{
		"ai_api_key": {Action: "replace", Value: ""},
	}); err == nil {
		t.Fatal("replace 不应接受空秘密")
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

// TestSecretCodecReadsLegacyPlaintextAndRejectsWrongKey 封装TestSecretCodecReadsLegacyPlaintextAndRejectsWrongKey业务协调。
func TestSecretCodecReadsLegacyPlaintextAndRejectsWrongKey(t *testing.T) {
	// codec 用于本次流程后续判断的codec
	codec, _ := newSecretCodec("correct")
	// encrypted、err 用于本次流程后续判断的encrypted、err
	encrypted, err := codec.encrypt("cookie", "owner", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if // plain、err 用于本次流程后续判断的plain、err
	plain, err := codec.decrypt("cookie", "owner", "legacy-plaintext"); err != nil || plain != "legacy-plaintext" {
		t.Fatalf("legacy plain=%q err=%v", plain, err)
	}
	// wrong 用于本次流程后续判断的wrong
	wrong, _ := newSecretCodec("wrong")
	if // err 用于本次流程后续判断的err
	_, err := wrong.decrypt("cookie", "owner", encrypted); err == nil {
		t.Fatal("wrong key must not return ciphertext as plaintext")
	}
	// withoutKey 用于本次流程后续判断的withoutKey
	withoutKey, _ := newSecretCodec("")
	if // err 用于本次流程后续判断的err
	_, err := withoutKey.decrypt("cookie", "owner", encrypted); err == nil {
		t.Fatal("missing key must reject encrypted data")
	}
}

// TestEncryptLegacySecretsUpgradesPlaintextAndValidatesKey 封装TestEncryptLegacySecretsUpgradesPlaintextAndValidatesKey业务协调。
func TestEncryptLegacySecretsUpgradesPlaintextAndValidatesKey(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "migration-key")
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // ok、err 用于本次流程后续判断的ok、err
	ok, err := store.Users.Create(ctx, "legacy-owner", "legacy-owner@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, err)
	}
	// owner 用于本次流程后续判断的所有者
	owner, _ := store.Users.GetByUsername(ctx, "legacy-owner")
	if // err 用于本次流程后续判断的err
	_, err := store.DB.ExecContext(ctx, `INSERT INTO cookies (id,value,user_id,password) VALUES (?,?,?,?)`, "legacy-secret", "legacy-cookie", owner.ID, "legacy-password"); err != nil {
		t.Fatal(err)
	}
	// cardResult、cardInsertErr 保存插入历史明文 API 卡券配置的结果。
	cardResult, cardInsertErr := store.DB.ExecContext(ctx, `INSERT INTO cards (name,type,api_config,user_id) VALUES (?,?,?,?)`, "legacy-api", "api", `{"url":"https://example.com","headers":{"Authorization":"legacy-card-secret"}}`, owner.ID)
	if cardInsertErr != nil {
		t.Fatal(cardInsertErr)
	}
	// cardID 保存需要启动迁移的历史 API 卡券标识。
	cardID, cardIDErr := cardResult.LastInsertId()
	if cardIDErr != nil {
		t.Fatal(cardIDErr)
	}
	if // err 用于本次流程后续判断的err
	err := store.EncryptLegacySecrets(ctx); err != nil {
		t.Fatal(err)
	}
	// raw 用于本次流程后续判断的原始
	var raw string
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT value FROM cookies WHERE id='legacy-secret'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, encryptedValuePrefix) {
		t.Fatalf("legacy value was not upgraded: %q", raw)
	}
	// cardRaw 保存 API 卡券数据库中的原始配置，确认请求模板没有以明文留存。
	var cardRaw string
	// err 表示读取 API 卡券密文时的数据库错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT api_config FROM cards WHERE id=?`, cardID).Scan(&cardRaw); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cardRaw, encryptedValuePrefix) || strings.Contains(cardRaw, "legacy-card-secret") {
		t.Fatalf("legacy API config was not encrypted: %q", cardRaw)
	}
	// card、cardErr 保存专用完整读取路径解密后的 API 配置。
	card, cardErr := store.Cards.Get(ctx, cardID)
	if cardErr != nil || card == nil || !strings.Contains(card.APIConfig, "legacy-card-secret") {
		t.Fatalf("API config decrypt failed card=%+v err=%v", card, cardErr)
	}
	// wrongCodec 用于本次流程后续判断的wrongCodec
	wrongCodec, _ := newSecretCodec("wrong-key")
	// wrongStore 用于本次流程后续判断的wrongStore
	wrongStore := NewStore(store.DB, store.Dialect)
	wrongStore.Cookies.codec = wrongCodec
	if // err 用于本次流程后续判断的err
	err := wrongStore.EncryptLegacySecrets(ctx); err == nil {
		t.Fatal("startup validation must reject the wrong data key")
	}
}
