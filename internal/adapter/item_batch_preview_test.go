package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestItemBatchPreviewPortRejectsMissingStore 验证归属查询在未装配数据库时快速失败。
func TestItemBatchPreviewPortRejectsMissingStore(t *testing.T) {
	// port 是未装配数据库的批量预检适配器。
	port := NewItemBatchPreviewPort(nil)
	// err 表示账号归属查询错误。
	if _, err := port.CookieOwned(context.Background(), 1, "acc1"); err == nil {
		t.Error("CookieOwned() expected missing store error")
	}
	// err 表示卡券归属查询错误。
	if _, err := port.CardOwned(context.Background(), 1, 1); err == nil {
		t.Error("CardOwned() expected missing store error")
	}
}

// TestItemBatchPreviewPortValidatesImageReferences 验证本地图片路径安全规则和普通文件检查。
func TestItemBatchPreviewPortValidatesImageReferences(t *testing.T) {
	// port 是只使用路径校验能力的批量预检适配器。
	port := NewItemBatchPreviewPort(nil)
	// root 是测试上传目录。
	root := t.TempDir()
	// imagePath 是测试图片的绝对路径。
	imagePath := filepath.Join(root, "img", "a.png")
	// err 表示测试图片目录创建错误。
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o750); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	// err 表示测试图片写入错误。
	if err := os.WriteFile(imagePath, []byte("png"), 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	// err 表示有效本地图片校验错误。
	if err := port.ValidateImageReference(root, "img/a.png"); err != nil {
		t.Fatalf("valid image reference error = %v", err)
	}
	// err 表示远程图片校验错误。
	if err := port.ValidateImageReference(root, "https://example.com/a.png"); err != nil {
		t.Fatalf("remote image reference error = %v", err)
	}
	// reference 表示当前待拒绝的图片引用。
	for _, reference := range []string{"../a.png", "/tmp/a.png", "missing.png"} {
		// err 表示不安全或不存在图片的校验结果。
		if err := port.ValidateImageReference(root, reference); err == nil {
			t.Errorf("ValidateImageReference(%q) expected error", reference)
		}
	}
	// err 表示路径穿越校验错误。
	if err := port.ValidateImageReference(root, "../a.png"); !strings.Contains(err.Error(), "图片路径不安全") {
		t.Fatalf("path traversal error = %v", err)
	}
}
