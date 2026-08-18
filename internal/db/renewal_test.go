package db

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestUpdateRenewalCookieEncryptsCookieAndMetadataAtRest 封装TestUpdateRenewal登录凭证Encrypts登录凭证AndMetadataAtRest业务协调。
func TestUpdateRenewalCookieEncryptsCookieAndMetadataAtRest(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "renewal-encryption-key")
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // ok、err 用于本次流程后续判断的ok、err
	ok, err := store.Users.Create(ctx, "renewal-owner", "renewal-owner@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, err)
	}
	// owner 用于本次流程后续判断的所有者
	owner, _ := store.Users.GetByUsername(ctx, "renewal-owner")
	if // err 用于本次流程后续判断的err
	err := store.Cookies.CreateOwned(ctx, "renewal-cookie", "old=1", owner.ID); err != nil {
		t.Fatal(err)
	}
	// metadata 用于本次流程后续判断的metadata
	metadata := `{"cookies_refresh_snapshot":[{"name":"token","value":"metadata-secret","domain":".goofish.com","path":"/"}],"other":true}`
	if // err 用于本次流程后续判断的err
	err := store.Cookies.UpdateRenewalCookie(ctx, "renewal-cookie", "token=cookie-secret", metadata, 12345); err != nil {
		t.Fatal(err)
	}

	// rawCookie、rawMetadata 用于本次流程后续判断的原始Cookie、rawMetadata
	var rawCookie, rawMetadata string
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT value,metadata_json FROM cookies WHERE id=?`, "renewal-cookie").Scan(&rawCookie, &rawMetadata); err != nil {
		t.Fatal(err)
	}
	// name、raw 表示当前遍历过程中的name、raw
	for name, raw := range map[string]string{"cookie": rawCookie, "metadata": rawMetadata} {
		if !strings.HasPrefix(raw, encryptedValuePrefix) || strings.Contains(raw, "secret") {
			t.Fatalf("%s was not encrypted at rest: %q", name, raw)
		}
	}
	// detail、err 用于本次流程后续判断的detail、err
	detail, err := store.Cookies.GetDetails(ctx, "renewal-cookie")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Value != "token=cookie-secret" || detail.MetadataJSON != metadata || detail.LastRefreshAt != 12345 {
		t.Fatalf("detail=%+v", detail)
	}
	// accounts、err 用于本次流程后续判断的accounts、err
	accounts, err := store.Cookies.ActiveRenewalRuntimeAccounts(ctx)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("accounts=%+v err=%v", accounts, err)
	}
	if accounts[0].Value != detail.Value || accounts[0].MetadataJSON != metadata {
		t.Fatalf("renewal account not decrypted: %+v", accounts[0])
	}
}

// TestUpdateRenewalCookieRejectsMissingAccount 封装TestUpdateRenewal登录凭证RejectsMissing账号业务协调。
func TestUpdateRenewalCookieRejectsMissingAccount(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestDB(t)
	defer cleanup()
	// err 用于本次流程后续判断的err
	err := store.Cookies.UpdateRenewalCookie(context.Background(), "missing", "a=1", `{}`, 1)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

// TestFlatCookieUpdateClearsSnapshotAndPreservesOtherMetadata 封装TestFlat登录凭证UpdateClearsSnapshotAndPreservesOtherMetadata业务协调。
func TestFlatCookieUpdateClearsSnapshotAndPreservesOtherMetadata(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "flat-update-metadata-key")
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // ok、err 用于本次流程后续判断的ok、err
	ok, err := store.Users.Create(ctx, "flat-owner", "flat-owner@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, err)
	}
	// owner 用于本次流程后续判断的所有者
	owner, _ := store.Users.GetByUsername(ctx, "flat-owner")
	if // err 用于本次流程后续判断的err
	err := store.Cookies.CreateOwned(ctx, "flat-cookie", "sid=old", owner.ID); err != nil {
		t.Fatal(err)
	}
	// metadata 用于本次流程后续判断的metadata
	metadata := `{"cookies_refresh_snapshot":[{"name":"sid","value":"old","domain":".goofish.com","path":"/"}],"other":true}`
	if // err 用于本次流程后续判断的err
	err := store.Cookies.UpdateRenewalCookie(ctx, "flat-cookie", "sid=old", metadata, 1); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Cookies.UpdateValueExisting(ctx, "flat-cookie", "sid=fresh"); err != nil {
		t.Fatal(err)
	}
	// detail、err 用于本次流程后续判断的detail、err
	detail, err := store.Cookies.GetDetails(ctx, "flat-cookie")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Value != "sid=fresh" || strings.Contains(detail.MetadataJSON, "cookies_refresh_snapshot") || !strings.Contains(detail.MetadataJSON, `"other":true`) {
		t.Fatalf("detail=%+v", detail)
	}
	// rawMetadata 用于本次流程后续判断的原始Metadata
	var rawMetadata string
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT metadata_json FROM cookies WHERE id=?`, "flat-cookie").Scan(&rawMetadata); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rawMetadata, encryptedValuePrefix) || strings.Contains(rawMetadata, "other") {
		t.Fatalf("metadata not encrypted at rest: %q", rawMetadata)
	}
}

// TestEncryptLegacySecretsMigratesCookieMetadata 封装TestEncryptLegacySecretsMigrates登录凭证Metadata业务协调。
func TestEncryptLegacySecretsMigratesCookieMetadata(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "metadata-migration-key")
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // ok、err 用于本次流程后续判断的ok、err
	ok, err := store.Users.Create(ctx, "metadata-owner", "metadata-owner@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, err)
	}
	// owner 用于本次流程后续判断的所有者
	owner, _ := store.Users.GetByUsername(ctx, "metadata-owner")
	// metadata 用于本次流程后续判断的metadata
	metadata := `{"cookies_refresh_snapshot":[{"name":"sid","value":"legacy-secret"}]}`
	if // err 用于本次流程后续判断的err
	_, err := store.DB.ExecContext(ctx,
		`INSERT INTO cookies (id,value,user_id,metadata_json) VALUES (?,?,?,?)`,
		"legacy-metadata", "legacy-cookie", owner.ID, metadata); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.EncryptLegacySecrets(ctx); err != nil {
		t.Fatal(err)
	}
	// raw 用于本次流程后续判断的原始
	var raw string
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT metadata_json FROM cookies WHERE id=?`, "legacy-metadata").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, encryptedValuePrefix) || strings.Contains(raw, "legacy-secret") {
		t.Fatalf("legacy metadata was not encrypted: %q", raw)
	}
	// detail、err 用于本次流程后续判断的detail、err
	detail, err := store.Cookies.GetDetails(ctx, "legacy-metadata")
	if err != nil || detail.MetadataJSON != metadata {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
}

// TestRenewalRuntimeQueriesExcludeLoginSecrets 验证续期窄查询只解密 Cookie 与 metadata，不读取登录密码并过滤禁用账号。
func TestRenewalRuntimeQueriesExcludeLoginSecrets(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "renewal-runtime-query-key")
	// store 是当前测试使用的 SQLite repository 聚合器。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是测试数据库操作共用的上下文。
	ctx := context.Background()
	// createErr 表示创建测试用户失败的原因。
	if ok, createErr := store.Users.Create(ctx, "runtime-renewal-owner", "runtime-renewal-owner@example.com", "pw"); createErr != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, createErr)
	}
	// owner 是测试账号的所有者。
	owner, ownerErr := store.Users.GetByUsername(ctx, "runtime-renewal-owner")
	if ownerErr != nil {
		t.Fatalf("get owner: %v", ownerErr)
	}
	// activeErr 表示创建启用账号失败的原因。
	if activeErr := store.Cookies.CreateOwned(ctx, "runtime-renewal-active", "sid=active", owner.ID); activeErr != nil {
		t.Fatalf("create active account: %v", activeErr)
	}
	// metadata 是续期运行所需的合法 Cookie 快照元数据。
	metadata := `{"other":"runtime-metadata"}`
	// updateErr 表示写入启用账号 Cookie 和 metadata 失败的原因。
	if updateErr := store.Cookies.UpdateRenewalCookie(ctx, "runtime-renewal-active", "sid=active", metadata, 1); updateErr != nil {
		t.Fatalf("update active account: %v", updateErr)
	}
	// disabledErr 表示创建禁用账号失败的原因。
	if disabledErr := store.Cookies.CreateOwned(ctx, "runtime-renewal-disabled", "sid=disabled", owner.ID); disabledErr != nil {
		t.Fatalf("create disabled account: %v", disabledErr)
	}
	// statusErr 表示禁用测试账号失败的原因。
	if statusErr := store.Cookies.SetStatus(ctx, "runtime-renewal-disabled", false); statusErr != nil {
		t.Fatalf("disable account: %v", statusErr)
	}
	// corruptErr 表示写入故意损坏的登录字段失败的原因；窄查询不应读取这些字段。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET username=?,password=? WHERE id=?`,
		"runtime-user", "not-a-password-ciphertext", "runtime-renewal-active"); corruptErr != nil {
		t.Fatalf("corrupt login fields: %v", corruptErr)
	}
	// accounts 是过滤禁用账号后得到的续期窄模型列表。
	accounts, listErr := store.Cookies.ActiveRenewalRuntimeAccounts(ctx)
	if listErr != nil {
		t.Fatalf("ActiveRenewalRuntimeAccounts: %v", listErr)
	}
	if len(accounts) != 1 || accounts[0].ID != "runtime-renewal-active" || accounts[0].Value != "sid=active" || accounts[0].MetadataJSON != metadata {
		t.Fatalf("runtime accounts=%+v", accounts)
	}
	// account 是按账号 ID 原子重读到的启用续期窄模型。
	account, getErr := store.Cookies.GetRenewalRuntimeAccount(ctx, "runtime-renewal-active")
	if getErr != nil || !account.Enabled || account.Value != "sid=active" || account.MetadataJSON != metadata {
		t.Fatalf("GetRenewalRuntimeAccount account=%+v err=%v", account, getErr)
	}
}
