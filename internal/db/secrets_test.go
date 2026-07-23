package db

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSensitiveRepositoriesEncryptAtRestAndDecryptOnRead(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "unit-test-data-key")
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if ok, err := store.Users.Create(ctx, "secret-owner", "secret-owner@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, err)
	}
	owner, _ := store.Users.GetByUsername(ctx, "secret-owner")
	if err := store.Cookies.CreateOwned(ctx, "secret-cookie", "unb=secret; token=plain", owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Cookies.UpdateLoginInfo(ctx, "secret-cookie", "login", "password-plain", false); err != nil {
		t.Fatal(err)
	}
	if err := store.Tokens.Save(ctx, "secret-cookie", "device-plain", "access-plain", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if err := store.Settings.Set(ctx, "ai_api_key", "sk-plain"); err != nil {
		t.Fatal(err)
	}
	if err := store.Settings.Set(ctx, "captcha.remote_secret_key", "captcha-secret-plain"); err != nil {
		t.Fatal(err)
	}
	channelID, err := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{
		Name: "secret", Type: "webhook", Config: `{"webhook_url":"https://example.test/plain-token"}`, Enabled: true, UserID: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	var rawCookie, rawPassword, rawDevice, rawToken, rawSetting, rawCaptchaSecret, rawConfig string
	if err := store.DB.QueryRowContext(ctx, `SELECT value,password FROM cookies WHERE id=?`, "secret-cookie").Scan(&rawCookie, &rawPassword); err != nil {
		t.Fatal(err)
	}
	if err := store.DB.QueryRowContext(ctx, `SELECT device_id,access_token FROM account_tokens WHERE cookie_id=?`, "secret-cookie").Scan(&rawDevice, &rawToken); err != nil {
		t.Fatal(err)
	}
	keyCol := dialectQuote(store.Dialect, "key")
	if err := store.DB.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE `+keyCol+`=?`, "ai_api_key").Scan(&rawSetting); err != nil {
		t.Fatal(err)
	}
	if err := store.DB.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE `+keyCol+`=?`, "captcha.remote_secret_key").Scan(&rawCaptchaSecret); err != nil {
		t.Fatal(err)
	}
	if err := store.DB.QueryRowContext(ctx, `SELECT config FROM notification_channels WHERE id=?`, channelID).Scan(&rawConfig); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{"cookie": rawCookie, "password": rawPassword, "device": rawDevice, "token": rawToken, "setting": rawSetting, "captcha-secret": rawCaptchaSecret, "config": rawConfig} {
		if !strings.HasPrefix(raw, encryptedValuePrefix) || strings.Contains(raw, "plain") {
			t.Fatalf("%s was not encrypted at rest: %q", name, raw)
		}
	}

	detail, err := store.Cookies.GetDetails(ctx, "secret-cookie")
	if err != nil || detail.Value != "unb=secret; token=plain" || detail.Password != "password-plain" {
		t.Fatalf("cookie detail=%+v err=%v", detail, err)
	}
	token, err := store.Tokens.Get(ctx, "secret-cookie")
	if err != nil || token.DeviceID != "device-plain" || token.AccessToken != "access-plain" {
		t.Fatalf("token=%+v err=%v", token, err)
	}
	if setting, err := store.Settings.Get(ctx, "ai_api_key"); err != nil || setting != "sk-plain" {
		t.Fatalf("setting=%q err=%v", setting, err)
	}
	if setting, err := store.Settings.Get(ctx, "captcha.remote_secret_key"); err != nil || setting != "captcha-secret-plain" {
		t.Fatalf("captcha secret=%q err=%v", setting, err)
	}
	channel, err := store.Notifications.GetChannel(ctx, channelID)
	if err != nil || channel == nil || !strings.Contains(channel.Config, "plain-token") {
		t.Fatalf("channel=%+v err=%v", channel, err)
	}
}

func TestSecretCodecReadsLegacyPlaintextAndRejectsWrongKey(t *testing.T) {
	codec, _ := newSecretCodec("correct")
	encrypted, err := codec.encrypt("cookie", "owner", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := codec.decrypt("cookie", "owner", "legacy-plaintext"); err != nil || plain != "legacy-plaintext" {
		t.Fatalf("legacy plain=%q err=%v", plain, err)
	}
	wrong, _ := newSecretCodec("wrong")
	if _, err := wrong.decrypt("cookie", "owner", encrypted); err == nil {
		t.Fatal("wrong key must not return ciphertext as plaintext")
	}
	withoutKey, _ := newSecretCodec("")
	if _, err := withoutKey.decrypt("cookie", "owner", encrypted); err == nil {
		t.Fatal("missing key must reject encrypted data")
	}
}

func TestEncryptLegacySecretsUpgradesPlaintextAndValidatesKey(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "migration-key")
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if ok, err := store.Users.Create(ctx, "legacy-owner", "legacy-owner@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, err)
	}
	owner, _ := store.Users.GetByUsername(ctx, "legacy-owner")
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO cookies (id,value,user_id,password) VALUES (?,?,?,?)`, "legacy-secret", "legacy-cookie", owner.ID, "legacy-password"); err != nil {
		t.Fatal(err)
	}
	if err := store.EncryptLegacySecrets(ctx); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := store.DB.QueryRowContext(ctx, `SELECT value FROM cookies WHERE id='legacy-secret'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, encryptedValuePrefix) {
		t.Fatalf("legacy value was not upgraded: %q", raw)
	}
	wrongCodec, _ := newSecretCodec("wrong-key")
	wrongStore := NewStore(store.DB, store.Dialect)
	wrongStore.Cookies.codec = wrongCodec
	if err := wrongStore.EncryptLegacySecrets(ctx); err == nil {
		t.Fatal("startup validation must reject the wrong data key")
	}
}
