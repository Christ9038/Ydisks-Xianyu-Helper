package browser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePersistentUserDataDirConvertsRelativePathAndCreatesDirectory(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	target := filepath.Join(t.TempDir(), "nested", "profile")
	relative, err := filepath.Rel(cwd, target)
	if err != nil {
		t.Fatalf("Rel: %v", err)
	}

	got, err := resolvePersistentUserDataDir(relative)
	if err != nil {
		t.Fatalf("resolvePersistentUserDataDir: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("返回路径必须是绝对路径，got %q", got)
	}
	if got != filepath.Clean(target) {
		t.Fatalf("got %q, want %q", got, filepath.Clean(target))
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%q 应为目录", got)
	}
}

func TestResolvePersistentUserDataDirPreservesAbsolutePath(t *testing.T) {
	target := filepath.Join(t.TempDir(), "profile")
	got, err := resolvePersistentUserDataDir("  " + target + "  ")
	if err != nil {
		t.Fatalf("resolvePersistentUserDataDir: %v", err)
	}
	if got != target {
		t.Fatalf("got %q, want %q", got, target)
	}
}

func TestResolvePersistentUserDataDirRejectsEmptyPath(t *testing.T) {
	_, err := resolvePersistentUserDataDir("   ")
	if err == nil || !strings.Contains(err.Error(), "不能为空") {
		t.Fatalf("空路径应返回明确错误，got %v", err)
	}
}

func TestResolvePersistentUserDataDirReportsCreateFailure(t *testing.T) {
	parentFile := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parentFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := resolvePersistentUserDataDir(filepath.Join(parentFile, "profile"))
	if err == nil || !strings.Contains(err.Error(), "创建") {
		t.Fatalf("目录创建失败应返回明确错误，got %v", err)
	}
}
