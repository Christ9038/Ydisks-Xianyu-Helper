//go:build unix

package adapter

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestLocalReplyImageOpenDoesNotBlockOnFIFO 验证检查后的图片若被替换为 FIFO，实际打开也使用非阻塞标志。
func TestLocalReplyImageOpenDoesNotBlockOnFIFO(t *testing.T) {
	if localReplyImageOpenFlags&syscall.O_NONBLOCK == 0 {
		t.Fatal("Unix 本地图片必须使用非阻塞打开标志")
	}
	// directory 是当前测试独有的文件根目录。
	directory := t.TempDir()
	// fifoErr 创建没有写入端的命名管道，复现可能阻塞的目标类型。
	if fifoErr := syscall.Mkfifo(filepath.Join(directory, "reply.png"), 0600); fifoErr != nil {
		t.Fatal(fifoErr)
	}
	// root 与 rootErr 保存受限目录句柄。
	root, rootErr := os.OpenRoot(directory)
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	defer root.Close()
	// file 与 openErr 使用生产标志直接打开管道；无写入端也必须立即返回。
	file, openErr := root.OpenFile("reply.png", localReplyImageOpenFlags, 0)
	if openErr != nil {
		t.Fatal(openErr)
	}
	file.Close()
	// readErr 验证完整读取函数拒绝特殊文件而不把它当图片读取。
	if _, _, _, readErr := readLocalReplyImage(context.Background(), root, "reply.png"); readErr == nil {
		t.Fatal("FIFO 不应进入图片读取")
	}
}
