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

func TestRunCreatesAdminInTemporaryDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "init-admin.db")
	var out bytes.Buffer
	err := run(context.Background(), dbPath, bufio.NewReader(strings.NewReader("admin@example.com\nsecret\nsecret\n")), &out)
	if err != nil {
		t.Fatalf("run create: %v", err)
	}
	if !strings.Contains(out.String(), "初始化完成") {
		t.Fatalf("missing create confirmation: %s", out.String())
	}
	d, dialect, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	admin, err := db.NewStore(d, dialect).Users.GetAdmin(context.Background())
	if err != nil || admin == nil || admin.Email != "admin@example.com" {
		t.Fatalf("admin=%+v err=%v", admin, err)
	}
}

func TestRunExistingAdminCanSkipReset(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "init-admin.db")
	input := "admin@example.com\nsecret\nsecret\n"
	if err := run(context.Background(), dbPath, bufio.NewReader(strings.NewReader(input)), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(context.Background(), dbPath, bufio.NewReader(strings.NewReader("n\n")), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "跳过初始化") {
		t.Fatalf("missing skip confirmation: %s", out.String())
	}
}

func TestRunExistingAdminResetsPasswordAfterMismatchRetry(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "init-admin.db")
	if err := run(context.Background(), dbPath, bufio.NewReader(strings.NewReader("admin@example.com\nold\nold\n")), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	input := "y\nnew\nnot-the-same\nnew\nnew\n"
	if err := run(context.Background(), dbPath, bufio.NewReader(strings.NewReader(input)), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	d, dialect, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store := db.NewStore(d, dialect)
	if _, ok, _ := store.Users.VerifyAndUpgrade(context.Background(), "admin", "old"); ok {
		t.Fatal("old password should be invalid")
	}
	if _, ok, _ := store.Users.VerifyAndUpgrade(context.Background(), "admin", "new"); !ok {
		t.Fatal("new password should be valid")
	}
}

func TestRunRejectsEmptyEmail(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "init-admin.db")
	err := run(context.Background(), dbPath, bufio.NewReader(strings.NewReader("\n")), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "邮箱不能为空") {
		t.Fatalf("err=%v, want empty email error", err)
	}
}

func TestMainCLIEntrypointUsesDatabaseFlag(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "main.db")
	stdin, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
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
