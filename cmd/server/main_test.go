package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"xianyu-go/internal/db"
)

func TestEnsureAdminIfMissingCreatesOnlyOnce(t *testing.T) {
	ctx := context.Background()
	database, dialect, err := db.Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store := db.NewStore(database, dialect)

	created, err := ensureAdminIfMissing(ctx, store, "admin@example.com", "first-password")
	if err != nil || !created {
		t.Fatalf("first ensure: created=%v err=%v", created, err)
	}
	created, err = ensureAdminIfMissing(ctx, store, "admin@example.com", "second-password")
	if err != nil || created {
		t.Fatalf("second ensure: created=%v err=%v", created, err)
	}
	if _, ok, err := store.Users.VerifyAndUpgrade(ctx, "admin", "first-password"); err != nil || !ok {
		t.Fatalf("original password should remain valid: ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.Users.VerifyAndUpgrade(ctx, "admin", "second-password"); err == nil || ok {
		t.Fatalf("later password must not reset admin: ok=%v err=%v", ok, err)
	}
}

func TestLoadOrCreateDataKeyPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data-key")
	first, err := loadOrCreateDataKey(path)
	if err != nil {
		t.Fatalf("create data key: %v", err)
	}
	if first == "" {
		t.Fatal("created data key is empty")
	}
	second, err := loadOrCreateDataKey(path)
	if err != nil {
		t.Fatalf("load data key: %v", err)
	}
	if first != second {
		t.Fatalf("data key changed between loads")
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) == "" {
		t.Fatalf("data key file was not written: err=%v", err)
	}
}
