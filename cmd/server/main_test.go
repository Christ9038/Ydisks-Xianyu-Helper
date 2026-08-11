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

func TestResolveDataDirKeepsExplicitDirectory(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "ydisks-data")
	got, err := resolveDataDir(explicit)
	if err != nil {
		t.Fatalf("resolve explicit data directory: %v", err)
	}
	if got != explicit {
		t.Fatalf("explicit data directory changed: got %q want %q", got, explicit)
	}
}

func TestUserDataDirName(t *testing.T) {
	base := filepath.Join(t.TempDir(), "Application Support")
	got := filepath.Join(base, userDataDirName)
	want := filepath.Join(base, "YdisksXianyuHelper")
	if got != want {
		t.Fatalf("unexpected user data directory: got %q want %q", got, want)
	}
}

func TestResolveDBPathUsesDataDirectoryForDefault(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "YdisksXianyuHelper")
	got := resolveDBPath(dataDir, defaultDBPath)
	want := filepath.Join(dataDir, "data", "xianyu_data.db")
	if got != want {
		t.Fatalf("unexpected default database path: got %q want %q", got, want)
	}
}

func TestResolveDBPathPreservesCustomPath(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "YdisksXianyuHelper")
	custom := filepath.Join(t.TempDir(), "custom.db")
	if got := resolveDBPath(dataDir, custom); got != custom {
		t.Fatalf("custom database path changed: got %q want %q", got, custom)
	}
}
