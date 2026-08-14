package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"xianyu-go/internal/db"
)

// TestEnsureAdminIfMissingCreatesOnlyOnce 负责TestEnsureAdminIfMissingCreatesOnlyOnce相关处理。
func TestEnsureAdminIfMissingCreatesOnlyOnce(t *testing.T) {
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// database、dialect、err 保存database、dialect、err，供当前处理流程使用
	database, dialect, err := db.Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	// store 保存store，供当前处理流程使用
	store := db.NewStore(database, dialect)

	// created、err 保存created、err，供当前处理流程使用
	created, err := ensureAdminIfMissing(ctx, store, "admin@example.com", "first-password")
	if err != nil || !created {
		t.Fatalf("first ensure: created=%v err=%v", created, err)
	}
	created, err = ensureAdminIfMissing(ctx, store, "admin@example.com", "second-password")
	if err != nil || created {
		t.Fatalf("second ensure: created=%v err=%v", created, err)
	}
	if // ok、err 保存ok、err，供当前处理流程使用
	_, ok, err := store.Users.VerifyAndUpgrade(ctx, "admin", "first-password"); err != nil || !ok {
		t.Fatalf("original password should remain valid: ok=%v err=%v", ok, err)
	}
	if // ok、err 保存ok、err，供当前处理流程使用
	_, ok, err := store.Users.VerifyAndUpgrade(ctx, "admin", "second-password"); err == nil || ok {
		t.Fatalf("later password must not reset admin: ok=%v err=%v", ok, err)
	}
}

// TestLoadOrCreateDataKeyPersists 负责TestLoadOrCreate数据KeyPersists相关处理。
func TestLoadOrCreateDataKeyPersists(t *testing.T) {
	// path 保存路径，供当前处理流程使用
	path := filepath.Join(t.TempDir(), "data-key")
	// first、err 保存first、err，供当前处理流程使用
	first, err := loadOrCreateDataKey(path)
	if err != nil {
		t.Fatalf("create data key: %v", err)
	}
	if first == "" {
		t.Fatal("created data key is empty")
	}
	// second、err 保存second、err，供当前处理流程使用
	second, err := loadOrCreateDataKey(path)
	if err != nil {
		t.Fatalf("load data key: %v", err)
	}
	if first != second {
		t.Fatalf("data key changed between loads")
	}
	if // raw、err 保存raw、err，供当前处理流程使用
	raw, err := os.ReadFile(path); err != nil || string(raw) == "" {
		t.Fatalf("data key file was not written: err=%v", err)
	}
}

// TestOpenServerLogWriterUsesConfiguredDirectory 负责TestOpenServerLogWriterUsesConfiguredDirectory相关处理。
func TestOpenServerLogWriterUsesConfiguredDirectory(t *testing.T) {
	// logDir 保存logDir，供当前处理流程使用
	logDir := t.TempDir()
	t.Setenv("XIANYU_LOG_DIR", logDir)

	// writer、closeLog、err 保存writer、closeLog、err，供当前处理流程使用
	writer, closeLog, err := openServerLogWriter("")
	if err != nil {
		t.Fatalf("open log writer: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	_, err := io.WriteString(writer, "test log\n"); err != nil {
		t.Fatalf("write log: %v", err)
	}
	closeLog()

	// content、err 保存content、err，供当前处理流程使用
	content, err := os.ReadFile(filepath.Join(logDir, "server.log"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if string(content) != "test log\n" {
		t.Fatalf("unexpected log content: %q", content)
	}
}

// TestResolveDataDirKeepsExplicitDirectory 负责TestResolve数据DirKeepsExplicitDirectory相关处理。
func TestResolveDataDirKeepsExplicitDirectory(t *testing.T) {
	// explicit 保存explicit，供当前处理流程使用
	explicit := filepath.Join(t.TempDir(), "ydisks-data")
	// got、err 保存got、err，供当前处理流程使用
	got, err := resolveDataDir(explicit)
	if err != nil {
		t.Fatalf("resolve explicit data directory: %v", err)
	}
	if got != explicit {
		t.Fatalf("explicit data directory changed: got %q want %q", got, explicit)
	}
}

// TestUserDataDirName 负责Test用户数据Dir名称相关处理。
func TestUserDataDirName(t *testing.T) {
	// base 保存base，供当前处理流程使用
	base := filepath.Join(t.TempDir(), "Application Support")
	// got 保存got，供当前处理流程使用
	got := filepath.Join(base, userDataDirName)
	// want 保存want，供当前处理流程使用
	want := filepath.Join(base, "YdisksXianyuHelper")
	if got != want {
		t.Fatalf("unexpected user data directory: got %q want %q", got, want)
	}
}

// TestResolveDBPathUsesDataDirectoryForDefault 负责TestResolveDB路径Uses数据DirectoryForDefault相关处理。
func TestResolveDBPathUsesDataDirectoryForDefault(t *testing.T) {
	// dataDir 保存数据Dir，供当前处理流程使用
	dataDir := filepath.Join(t.TempDir(), "YdisksXianyuHelper")
	// got 保存got，供当前处理流程使用
	got := resolveDBPath(dataDir, defaultDBPath)
	// want 保存want，供当前处理流程使用
	want := filepath.Join(dataDir, "data", "xianyu_data.db")
	if got != want {
		t.Fatalf("unexpected default database path: got %q want %q", got, want)
	}
}

// TestResolveDBPathPreservesCustomPath 负责TestResolveDB路径PreservesCustom路径相关处理。
func TestResolveDBPathPreservesCustomPath(t *testing.T) {
	// dataDir 保存数据Dir，供当前处理流程使用
	dataDir := filepath.Join(t.TempDir(), "YdisksXianyuHelper")
	// custom 保存custom，供当前处理流程使用
	custom := filepath.Join(t.TempDir(), "custom.db")
	if // got 保存got，供当前处理流程使用
	got := resolveDBPath(dataDir, custom); got != custom {
		t.Fatalf("custom database path changed: got %q want %q", got, custom)
	}
}

// TestPlaywrightRuntimeRootUsesProcessArchitecture 负责TestPlaywrightRuntimeRootUsesProcessArchitecture相关处理。
func TestPlaywrightRuntimeRootUsesProcessArchitecture(t *testing.T) {
	// opts 保存opts，供当前处理流程使用
	opts := serverOptions{playwrightRuntimeRoot: filepath.Join(t.TempDir(), "playwright-runtime")}
	applyPlaywrightRuntimeRoot(&opts)
	// wantRoot 保存wantRoot，供当前处理流程使用
	wantRoot := filepath.Join(opts.playwrightRuntimeRoot, runtime.GOARCH)
	if opts.playwrightDriverDir != filepath.Join(wantRoot, "playwright-driver") {
		t.Fatalf("driver 目录=%q", opts.playwrightDriverDir)
	}
	if opts.playwrightBrowserDir != filepath.Join(wantRoot, "playwright-browsers") {
		t.Fatalf("browser 目录=%q", opts.playwrightBrowserDir)
	}
}
