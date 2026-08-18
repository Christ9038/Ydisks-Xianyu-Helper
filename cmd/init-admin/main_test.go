package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestRunCreatesAdminInTemporaryDatabase 封装Test运行CreatesAdminInTemporaryDatabase业务协调。
func TestRunCreatesAdminInTemporaryDatabase(t *testing.T) {
	// dbPath 用于本次流程后续判断的db路径
	dbPath := filepath.Join(t.TempDir(), "init-admin.db")
	// out 用于本次流程后续判断的out
	var out bytes.Buffer
	// err 用于本次流程后续判断的err
	err := run(context.Background(), dbPath, bufio.NewReader(strings.NewReader("admin@example.com\nsecret\nsecret\n")), &out)
	if err != nil {
		t.Fatalf("run create: %v", err)
	}
	if !strings.Contains(out.String(), "初始化完成") {
		t.Fatalf("missing create confirmation: %s", out.String())
	}
	// d、dialect、err 用于本次流程后续判断的d、dialect、err
	d, dialect, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	// admin、err 用于本次流程后续判断的admin、err
	admin, err := db.NewStore(d, dialect).Users.GetAdmin(context.Background())
	if err != nil || admin == nil || admin.Email != "admin@example.com" {
		t.Fatalf("admin=%+v err=%v", admin, err)
	}
}

// TestRunExistingAdminCanSkipReset 封装Test运行ExistingAdminCanSkipReset业务协调。
func TestRunExistingAdminCanSkipReset(t *testing.T) {
	// dbPath 用于本次流程后续判断的db路径
	dbPath := filepath.Join(t.TempDir(), "init-admin.db")
	// input 用于本次流程后续判断的input
	input := "admin@example.com\nsecret\nsecret\n"
	if // err 用于本次流程后续判断的err
	err := run(context.Background(), dbPath, bufio.NewReader(strings.NewReader(input)), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	// out 用于本次流程后续判断的out
	var out bytes.Buffer
	if // err 用于本次流程后续判断的err
	err := run(context.Background(), dbPath, bufio.NewReader(strings.NewReader("n\n")), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "跳过初始化") {
		t.Fatalf("missing skip confirmation: %s", out.String())
	}
}

// TestRunExistingAdminResetsPasswordAfterMismatchRetry 封装Test运行ExistingAdminResets密码AfterMismatch重试业务协调。
func TestRunExistingAdminResetsPasswordAfterMismatchRetry(t *testing.T) {
	// dbPath 用于本次流程后续判断的db路径
	dbPath := filepath.Join(t.TempDir(), "init-admin.db")
	if // err 用于本次流程后续判断的err
	err := run(context.Background(), dbPath, bufio.NewReader(strings.NewReader("admin@example.com\nold\nold\n")), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	// input 用于本次流程后续判断的input
	input := "y\nnew\nnot-the-same\nnew\nnew\n"
	if // err 用于本次流程后续判断的err
	err := run(context.Background(), dbPath, bufio.NewReader(strings.NewReader(input)), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	// d、dialect、err 用于本次流程后续判断的d、dialect、err
	d, dialect, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	// store 用于本次流程后续判断的store
	store := db.NewStore(d, dialect)
	if // ok 用于本次流程后续判断的ok
	_, ok, _ := store.Users.VerifyAndUpgrade(context.Background(), "admin", "old"); ok {
		t.Fatal("old password should be invalid")
	}
	if // ok 用于本次流程后续判断的ok
	_, ok, _ := store.Users.VerifyAndUpgrade(context.Background(), "admin", "new"); !ok {
		t.Fatal("new password should be valid")
	}
}

// TestRunRejectsEmptyEmail 封装Test运行RejectsEmpty邮箱业务协调。
func TestRunRejectsEmptyEmail(t *testing.T) {
	// dbPath 用于本次流程后续判断的db路径
	dbPath := filepath.Join(t.TempDir(), "init-admin.db")
	// err 用于本次流程后续判断的err
	err := run(context.Background(), dbPath, bufio.NewReader(strings.NewReader("\n")), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "邮箱不能为空") {
		t.Fatalf("err=%v, want empty email error", err)
	}
}

// TestMainCLIEntrypointUsesDatabaseFlag 封装TestMainCLIEntrypointUsesDatabaseFlag业务协调。
func TestMainCLIEntrypointUsesDatabaseFlag(t *testing.T) {
	// dbPath 用于本次流程后续判断的db路径
	dbPath := filepath.Join(t.TempDir(), "main.db")
	// stdin、writer、err 用于本次流程后续判断的stdin、writer、err
	stdin, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	// oldArgs、oldStdin、oldEnv、oldCommandLine 用于本次流程后续判断的oldArgs、oldStdin、oldEnv、oldCommandLine
	oldArgs, oldStdin, oldEnv, oldCommandLine := os.Args, os.Stdin, os.Getenv("DATABASE_URL"), flag.CommandLine
	defer func() {
		os.Args, os.Stdin, flag.CommandLine = oldArgs, oldStdin, oldCommandLine
		_ = os.Setenv("DATABASE_URL", oldEnv)
		_ = stdin.Close()
	}()
	os.Args = []string{"init-admin", "-db", dbPath}
	os.Stdin = stdin
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
	_ = os.Unsetenv("DATABASE_URL")
	go func() {
		_, _ = writer.Write([]byte("admin@example.com\nmain-secret\nmain-secret\n"))
		_ = writer.Close()
	}()
	main()
}
