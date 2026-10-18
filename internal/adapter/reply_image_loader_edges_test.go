package adapter

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestReplyImageContextReaderStopsBeforeRead 验证读取中取消不会继续消费底层文件字节。
func TestReplyImageContextReaderStopsBeforeRead(t *testing.T) {
	// ctx 与 cancel 保存请求取消状态。
	ctx, cancel := context.WithCancel(context.Background())
	// source 让测试能够观察取消后剩余字节没有变化。
	source := bytes.NewBufferString("image")
	// reader 在每次读取前检查 ctx，不拥有 source 的生命周期。
	reader := replyImageContextReader{ctx: ctx, reader: source}
	cancel()
	// buffer 是不应被取消读取填充的目标内存。
	buffer := make([]byte, 8)
	// count 与 readErr 保存取消后的读取结果。
	count, readErr := reader.Read(buffer)
	if count != 0 || !errors.Is(readErr, context.Canceled) || source.Len() != 5 {
		t.Fatalf("取消后仍然读取：count=%d err=%v remaining=%d", count, readErr, source.Len())
	}
}

// TestReplyAccountDirectoryScanHonorsContextAndClosedRoot 验证目录匹配在取消或根句柄关闭时安全拒绝。
func TestReplyAccountDirectoryScanHonorsContextAndClosedRoot(t *testing.T) {
	// directory 是当前测试独立的素材根目录。
	directory := t.TempDir()
	// mkdirErr 准备存在的账号目录，避免以目录缺失代替取消断言。
	if mkdirErr := os.Mkdir(filepath.Join(directory, "account-a"), 0700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// root 与 openErr 保存待验证的受限目录句柄。
	root, openErr := os.OpenRoot(directory)
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer root.Close()
	// ctx 与 cancel 构造目录扫描已取消的场景。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if hasExactReplyAccountDirectory(ctx, root, "account-a") {
		t.Fatal("取消后仍继续匹配账号目录")
	}
	root.Close()
	if hasExactReplyAccountDirectory(context.Background(), root, "account-a") {
		t.Fatal("已关闭根目录仍可匹配账号")
	}
}
