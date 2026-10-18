package adapter

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// replyPNG 为本地文件测试生成一张有效的小图片，避免依赖网络或仓库素材。
func replyPNG(t *testing.T) []byte {
	t.Helper()
	// encoded 保存 PNG 编码后的内存字节。
	var encoded bytes.Buffer
	// encodeErr 是测试夹具编码错误。
	if encodeErr := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 3))); encodeErr != nil {
		t.Fatal(encodeErr)
	}
	return encoded.Bytes()
}

// TestLocalReplyImageLoaderReadsFreshAccountFile 验证账号目录隔离、内存读取及替换文件后不使用旧缓存。
func TestLocalReplyImageLoaderReadsFreshAccountFile(t *testing.T) {
	// root 是仅供本测试使用的回复图片根目录。
	root := t.TempDir()
	// directory 保存账号内部的嵌套图片目录。
	directory := filepath.Join(root, "account-a", "商品")
	// mkdirErr 是准备测试目录的错误。
	if mkdirErr := os.MkdirAll(directory, 0700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// expected 保存待加载的真实 PNG 内容。
	expected := replyPNG(t)
	// filename 是账号下的测试文件，不包含真实用户数据。
	filename := filepath.Join(directory, "回复.png")
	// writeErr 是写入初始测试图片的错误。
	if writeErr := os.WriteFile(filename, expected, 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// loader 固定素材根目录，不依赖工作目录或网络。
	loader := newLocalReplyImageLoader(root)
	// data、mediaType、name 和 loadErr 保存读取出的图片字节与上传元数据。
	data, mediaType, name, loadErr := loader(context.Background(), "account-a", "商品/回复.png")
	if loadErr != nil || !bytes.Equal(data, expected) || mediaType != "image/png" || name != "回复.png" {
		t.Fatalf("本地图片读取失败：type=%q name=%q err=%v", mediaType, name, loadErr)
	}
	// otherErr 验证另一个账号不能复用相同相对路径读取前一账号素材。
	if _, _, _, otherErr := loader(context.Background(), "account-b", "商品/回复.png"); otherErr == nil {
		t.Fatal("未隔离账号图片目录")
	}
	// aliasErr 验证不区分大小写的文件系统也不能把另一个账号别名映射到当前目录。
	if _, _, _, aliasErr := loader(context.Background(), "ACCOUNT-A", "商品/回复.png"); aliasErr == nil {
		t.Fatal("账号大小写别名不应读取其他账号目录")
	}
	// replaceErr 将文件替换成非图片，以证明后续调用会重新读取和校验。
	if replaceErr := os.WriteFile(filename, []byte("not an image"), 0600); replaceErr != nil {
		t.Fatal(replaceErr)
	}
	// reloadErr 不应因命中旧缓存而伪造成功。
	if _, _, _, reloadErr := loader(context.Background(), "account-a", "商品/回复.png"); reloadErr == nil {
		t.Fatal("替换后的非法内容仍使用旧图片")
	}
}

// TestLocalReplyImageLoaderRejectsUnsafeInputs 验证路径、大小、内容、目录和取消边界均在上传前拒绝。
func TestLocalReplyImageLoaderRejectsUnsafeInputs(t *testing.T) {
	// root 和 directory 保存隔离测试根目录与账号目录。
	root := t.TempDir()
	// directory 是有效账号的素材目录。
	directory := filepath.Join(root, "account-a")
	// mkdirErr 是目录夹具准备错误。
	if mkdirErr := os.Mkdir(directory, 0700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// fixtures 包含空文件、伪装文件、截断 PNG、过大文件及合法图片。
	fixtures := map[string][]byte{"empty.png": {}, "fake.png": []byte("secret text"), "broken.png": []byte("\x89PNG\r\n\x1a\n"), "large.png": make([]byte, (10<<20)+1), "valid.png": replyPNG(t)}
	// name 和 data 是当前写入的测试文件及其内容。
	for name, data := range fixtures {
		// writeErr 是当前夹具的写入错误。
		if writeErr := os.WriteFile(filepath.Join(directory, name), data, 0600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	// loader 仅允许读取当前测试根目录。
	loader := newLocalReplyImageLoader(root)
	// reference 是必须拒绝的无效引用或文件。
	for _, reference := range []string{"", "../valid.png", "/valid.png", "C:/valid.png", `..\valid.png`, "a/../../valid.png", "empty.png", "fake.png", "broken.png", "large.png", ".", "missing.png"} {
		// loadErr 必须只返回稳定诊断，不能泄露宿主绝对路径。
		if _, _, _, loadErr := loader(context.Background(), "account-a", reference); loadErr == nil || strings.Contains(loadErr.Error(), root) {
			t.Fatalf("引用 %q 未被安全拒绝：%v", reference, loadErr)
		}
	}
	// accountID 是必须拒绝的跨账号目录输入。
	for _, accountID := range []string{"", ".", "..", "../account-a", "account-a/child", `account-a\child`, "C:"} {
		// accountErr 保存非法账号路径的拒绝结果。
		if _, _, _, accountErr := loader(context.Background(), accountID, "valid.png"); accountErr == nil {
			t.Fatalf("账号目录 %q 未被拒绝", accountID)
		}
	}
	// ctx 和 cancel 构造已经取消的请求。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// cancelErr 应保留 context.Canceled，且不会读取图片。
	if _, _, _, cancelErr := loader(ctx, "account-a", "valid.png"); !errors.Is(cancelErr, context.Canceled) {
		t.Fatalf("取消未传播：%v", cancelErr)
	}
}

// TestLocalReplyImageLoaderRejectsEscapingSymlinks 验证链接不能越过账号根目录；Windows 无链接权限时明确跳过。
func TestLocalReplyImageLoaderRejectsEscapingSymlinks(t *testing.T) {
	// root 和 outside 保存回复根目录及其外部目标。
	root, outside := t.TempDir(), t.TempDir()
	// mkdirErr 创建正常账号目录。
	if mkdirErr := os.Mkdir(filepath.Join(root, "account-a"), 0700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// writeErr 创建根目录之外的有效图片。
	if writeErr := os.WriteFile(filepath.Join(outside, "outside.png"), replyPNG(t), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// symlinkErr 表示当前系统是否允许创建测试符号链接。
	if symlinkErr := os.Symlink(outside, filepath.Join(root, "account-a", "escape")); symlinkErr != nil {
		t.Skipf("当前环境不能创建符号链接：%v", symlinkErr)
	}
	// loader 在账号沙箱内拒绝链接指向的外部图片。
	loader := newLocalReplyImageLoader(root)
	// loadErr 保存越界链接的拒绝结果。
	if _, _, _, loadErr := loader(context.Background(), "account-a", "escape/outside.png"); loadErr == nil {
		t.Fatal("越界符号链接不应可读")
	}
	// insideErr 为账号自身建立可读图片，确保账号链接测试不是因文件缺失而通过。
	if insideErr := os.WriteFile(filepath.Join(root, "account-a", "valid.png"), replyPNG(t), 0600); insideErr != nil {
		t.Fatal(insideErr)
	}
	// accountLinkErr 创建账号目录本身的跳转链接。
	if accountLinkErr := os.Symlink(filepath.Join(root, "account-a"), filepath.Join(root, "account-b")); accountLinkErr != nil {
		t.Fatal(accountLinkErr)
	}
	// accountErr 应拒绝把其他账号目录链接作为当前账号根目录。
	if _, _, _, accountErr := loader(context.Background(), "account-b", "valid.png"); accountErr == nil {
		t.Fatal("账号目录链接不应可用")
	}
}
