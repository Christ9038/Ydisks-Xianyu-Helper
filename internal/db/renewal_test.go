package db

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestUpdateRenewalCookieEncryptsCookieAndMetadataAtRest(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "renewal-encryption-key")
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if ok, err := store.Users.Create(ctx, "renewal-owner", "renewal-owner@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, err)
	}
	owner, _ := store.Users.GetByUsername(ctx, "renewal-owner")
	if err := store.Cookies.CreateOwned(ctx, "renewal-cookie", "old=1", owner.ID); err != nil {
		t.Fatal(err)
	}
	metadata := `{"cookies_refresh_snapshot":[{"name":"token","value":"metadata-secret","domain":".goofish.com","path":"/"}],"other":true}`
	if err := store.Cookies.UpdateRenewalCookie(ctx, "renewal-cookie", "token=cookie-secret", metadata, 12345); err != nil {
		t.Fatal(err)
	}

	var rawCookie, rawMetadata string
	if err := store.DB.QueryRowContext(ctx, `SELECT value,metadata_json FROM cookies WHERE id=?`, "renewal-cookie").Scan(&rawCookie, &rawMetadata); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{"cookie": rawCookie, "metadata": rawMetadata} {
		if !strings.HasPrefix(raw, encryptedValuePrefix) || strings.Contains(raw, "secret") {
			t.Fatalf("%s was not encrypted at rest: %q", name, raw)
		}
	}
	detail, err := store.Cookies.GetDetails(ctx, "renewal-cookie")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Value != "token=cookie-secret" || detail.MetadataJSON != metadata || detail.LastRefreshAt != 12345 {
		t.Fatalf("detail=%+v", detail)
	}
	accounts, err := store.Cookies.ActiveRenewalAccounts(ctx)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("accounts=%+v err=%v", accounts, err)
	}
	if accounts[0].Value != detail.Value || accounts[0].MetadataJSON != metadata {
		t.Fatalf("renewal account not decrypted: %+v", accounts[0])
	}
}

func TestUpdateRenewalCookieRejectsMissingAccount(t *testing.T) {
	store, cleanup := newTestDB(t)
	defer cleanup()
	err := store.Cookies.UpdateRenewalCookie(context.Background(), "missing", "a=1", `{}`, 1)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

func TestFlatCookieUpdateClearsSnapshotAndPreservesOtherMetadata(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "flat-update-metadata-key")
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if ok, err := store.Users.Create(ctx, "flat-owner", "flat-owner@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, err)
	}
	owner, _ := store.Users.GetByUsername(ctx, "flat-owner")
	if err := store.Cookies.CreateOwned(ctx, "flat-cookie", "sid=old", owner.ID); err != nil {
		t.Fatal(err)
	}
	metadata := `{"cookies_refresh_snapshot":[{"name":"sid","value":"old","domain":".goofish.com","path":"/"}],"other":true}`
	if err := store.Cookies.UpdateRenewalCookie(ctx, "flat-cookie", "sid=old", metadata, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.Cookies.UpdateValueExisting(ctx, "flat-cookie", "sid=fresh"); err != nil {
		t.Fatal(err)
	}
	detail, err := store.Cookies.GetDetails(ctx, "flat-cookie")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Value != "sid=fresh" || strings.Contains(detail.MetadataJSON, "cookies_refresh_snapshot") || !strings.Contains(detail.MetadataJSON, `"other":true`) {
		t.Fatalf("detail=%+v", detail)
	}
	var rawMetadata string
	if err := store.DB.QueryRowContext(ctx, `SELECT metadata_json FROM cookies WHERE id=?`, "flat-cookie").Scan(&rawMetadata); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rawMetadata, encryptedValuePrefix) || strings.Contains(rawMetadata, "other") {
		t.Fatalf("metadata not encrypted at rest: %q", rawMetadata)
	}
}

func TestEncryptLegacySecretsMigratesCookieMetadata(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "metadata-migration-key")
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if ok, err := store.Users.Create(ctx, "metadata-owner", "metadata-owner@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, err)
	}
	owner, _ := store.Users.GetByUsername(ctx, "metadata-owner")
	metadata := `{"cookies_refresh_snapshot":[{"name":"sid","value":"legacy-secret"}]}`
	if _, err := store.DB.ExecContext(ctx,
		`INSERT INTO cookies (id,value,user_id,metadata_json) VALUES (?,?,?,?)`,
		"legacy-metadata", "legacy-cookie", owner.ID, metadata); err != nil {
		t.Fatal(err)
	}
	if err := store.EncryptLegacySecrets(ctx); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := store.DB.QueryRowContext(ctx, `SELECT metadata_json FROM cookies WHERE id=?`, "legacy-metadata").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, encryptedValuePrefix) || strings.Contains(raw, "legacy-secret") {
		t.Fatalf("legacy metadata was not encrypted: %q", raw)
	}
	detail, err := store.Cookies.GetDetails(ctx, "legacy-metadata")
	if err != nil || detail.MetadataJSON != metadata {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
}
