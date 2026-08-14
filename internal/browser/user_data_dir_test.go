package browser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolvePersistentUserDataDirConvertsRelativePathAndCreatesDirectory 负责TestResolvePersistent用户数据DirConvertsRelative路径AndCreatesDirectory相关处理。
func TestResolvePersistentUserDataDirConvertsRelativePathAndCreatesDirectory(t *testing.T) {
	// cwd、err 保存cwd、err，供当前处理流程使用
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	// target 保存target，供当前处理流程使用
	target := filepath.Join(t.TempDir(), "nested", "profile")
	// relative、err 保存relative、err，供当前处理流程使用
	relative, err := filepath.Rel(cwd, target)
	if err != nil {
		t.Fatalf("Rel: %v", err)
	}

	// got、err 保存got、err，供当前处理流程使用
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
	// info、err 保存info、err，供当前处理流程使用
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%q 应为目录", got)
	}
}

// TestResolvePersistentUserDataDirPreservesAbsolutePath 负责TestResolvePersistent用户数据DirPreservesAbsolute路径相关处理。
func TestResolvePersistentUserDataDirPreservesAbsolutePath(t *testing.T) {
	// target 保存target，供当前处理流程使用
	target := filepath.Join(t.TempDir(), "profile")
	// got、err 保存got、err，供当前处理流程使用
	got, err := resolvePersistentUserDataDir("  " + target + "  ")
	if err != nil {
		t.Fatalf("resolvePersistentUserDataDir: %v", err)
	}
	if got != target {
		t.Fatalf("got %q, want %q", got, target)
	}
}

// TestResolvePersistentUserDataDirRejectsEmptyPath 负责TestResolvePersistent用户数据DirRejectsEmpty路径相关处理。
func TestResolvePersistentUserDataDirRejectsEmptyPath(t *testing.T) {
	// err 保存err，供当前处理流程使用
	_, err := resolvePersistentUserDataDir("   ")
	if err == nil || !strings.Contains(err.Error(), "不能为空") {
		t.Fatalf("空路径应返回明确错误，got %v", err)
	}
}

// TestResolvePersistentUserDataDirReportsCreateFailure 负责TestResolvePersistent用户数据DirReportsCreateFailure相关处理。
func TestResolvePersistentUserDataDirReportsCreateFailure(t *testing.T) {
	// parentFile 保存parent文件，供当前处理流程使用
	parentFile := filepath.Join(t.TempDir(), "file")
	if // err 保存err，供当前处理流程使用
	err := os.WriteFile(parentFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// err 保存err，供当前处理流程使用
	_, err := resolvePersistentUserDataDir(filepath.Join(parentFile, "profile"))
	if err == nil || !strings.Contains(err.Error(), "创建") {
		t.Fatalf("目录创建失败应返回明确错误，got %v", err)
	}
}
