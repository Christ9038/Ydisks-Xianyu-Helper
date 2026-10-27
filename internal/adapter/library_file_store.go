package adapter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	libraryapp "xianyu-go/internal/application/library"
)

// libraryDirName 是上传根目录下存放资源库图片的子目录；与 publish_batches 同级，批次过期清理不会触及它。
const libraryDirName = "library"

// publishBatchesDirName 是上传根目录下存放批量铺货批次目录的子目录，与批量铺货处理器约定一致。
const publishBatchesDirName = "publish_batches"

// libraryStorageKeyPattern 约束服务端生成的存储键：<用户ID>/<64位十六进制摘要>.<小写字母扩展名>。
var libraryStorageKeyPattern = regexp.MustCompile(`^[0-9]+/[0-9a-f]{64}\.[a-z]{3,4}$`)

// libraryBatchIDPattern 约束批次标识，防止拼出越界目录。
var libraryBatchIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// libraryCopyNamePattern 约束批次目录内的快照文件名，只允许单层普通文件名。
var libraryCopyNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

// LibraryFileStore 把资源库图片字节保存在 <上传根目录>/library 下，并负责批次图片快照复制。
// 全部读写通过 os.Root 完成，符号链接和 .. 都无法逃出对应目录；目录在首次写入时创建，构造本身不做 I/O。
// 它没有可变状态，可被多个 goroutine 并发使用。
type LibraryFileStore struct {
	// uploadRoot 是上传根目录，来自 uploadroot.Dir，可为相对路径。
	uploadRoot string
}

// NewLibraryFileStore 创建以 uploadRoot 为上传根目录的文件存储。
func NewLibraryFileStore(uploadRoot string) (*LibraryFileStore, error) {
	if strings.TrimSpace(uploadRoot) == "" {
		return nil, errors.New("上传根目录不能为空")
	}
	return &LibraryFileStore{uploadRoot: uploadRoot}, nil
}

// libraryRoot 打开（必要时创建）资源库根目录。
func (s *LibraryFileStore) libraryRoot() (*os.Root, error) {
	// dir 是资源库目录路径。
	dir := filepath.Join(s.uploadRoot, libraryDirName)
	// err 保存 MkdirAll 调用的失败原因。
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("创建资源库目录失败: %w", err)
	}
	return os.OpenRoot(dir)
}

// Save 原子写入图片字节：先写同目录临时文件再重命名，避免读取到半截文件。
func (s *LibraryFileStore) Save(_ context.Context, key string, data []byte) error {
	if !libraryStorageKeyPattern.MatchString(key) {
		return errors.New("图片存储键无效")
	}
	// root、err 保存资源库根目录句柄。
	root, err := s.libraryRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	// mkdirErr 保存 MkdirAll 调用的失败原因。
	if mkdirErr := root.MkdirAll(path.Dir(key), 0o750); mkdirErr != nil {
		return fmt.Errorf("创建用户图片目录失败: %w", mkdirErr)
	}
	// temp 是同目录临时文件名；同一内容的并发写入互相覆盖是无害的。
	temp := key + ".tmp"
	// writeErr 保存 WriteFile 调用的失败原因。
	if writeErr := root.WriteFile(temp, data, 0o600); writeErr != nil {
		return fmt.Errorf("写入图片失败: %w", writeErr)
	}
	// renameErr 保存 Rename 调用的失败原因。
	if renameErr := root.Rename(temp, key); renameErr != nil {
		_ = root.Remove(temp)
		return fmt.Errorf("保存图片失败: %w", renameErr)
	}
	return nil
}

// Read 读取图片字节。
func (s *LibraryFileStore) Read(_ context.Context, key string) ([]byte, error) {
	if !libraryStorageKeyPattern.MatchString(key) {
		return nil, errors.New("图片存储键无效")
	}
	// root、err 保存资源库根目录句柄。
	root, err := s.libraryRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(key)
}

// Remove 删除图片字节，文件不存在视为成功。
func (s *LibraryFileStore) Remove(_ context.Context, key string) error {
	if !libraryStorageKeyPattern.MatchString(key) {
		return errors.New("图片存储键无效")
	}
	// root、err 保存资源库根目录句柄。
	root, err := s.libraryRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	// removeErr 保存 Remove 调用的失败原因。
	if removeErr := root.Remove(key); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return removeErr
	}
	return nil
}

// Prepare 创建批次专属上传目录 <上传根目录>/publish_batches/<批次ID> 并返回其路径，路径形式与批量铺货处理器一致。
func (s *LibraryFileStore) Prepare(_ context.Context, batchID string) (string, error) {
	if !libraryBatchIDPattern.MatchString(batchID) {
		return "", errors.New("批次标识无效")
	}
	// dir 是批次上传目录。
	dir := filepath.Join(s.uploadRoot, publishBatchesDirName, batchID)
	// err 保存 MkdirAll 调用的失败原因。
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("创建上传目录失败: %w", err)
	}
	return dir, nil
}

// CopyImage 把资源库图片复制为批次目录下的快照文件。
func (s *LibraryFileStore) CopyImage(ctx context.Context, uploadDir, key, name string) error {
	if !libraryCopyNamePattern.MatchString(name) || name == "." || name == ".." {
		return errors.New("快照文件名无效")
	}
	// err 保存 checkBatchDir 调用的失败原因。
	if err := s.checkBatchDir(uploadDir); err != nil {
		return err
	}
	// data、err 保存资源库图片字节。
	data, err := s.Read(ctx, key)
	if err != nil {
		return fmt.Errorf("读取资源库图片失败: %w", err)
	}
	// batch、openErr 保存批次目录句柄。
	batch, openErr := os.OpenRoot(uploadDir)
	if openErr != nil {
		return fmt.Errorf("打开上传目录失败: %w", openErr)
	}
	defer batch.Close()
	return batch.WriteFile(name, data, 0o600)
}

// Discard 删除预检失败的批次目录；只允许删除 publish_batches 下的子目录。
func (s *LibraryFileStore) Discard(_ context.Context, uploadDir string) error {
	// err 保存 checkBatchDir 调用的失败原因。
	if err := s.checkBatchDir(uploadDir); err != nil {
		return err
	}
	// root、err 保存上传根目录句柄。
	root, err := os.OpenRoot(s.uploadRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	// rel 是批次目录相对上传根目录的路径。
	rel, _ := filepath.Rel(s.uploadRoot, uploadDir)
	return root.RemoveAll(rel)
}

// checkBatchDir 确认目录位于 <上传根目录>/publish_batches 之下，避免调用方传入任意路径。
func (s *LibraryFileStore) checkBatchDir(uploadDir string) error {
	// rel、err 是批次目录相对批次总目录的路径。
	rel, err := filepath.Rel(filepath.Join(s.uploadRoot, publishBatchesDirName), uploadDir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return errors.New("批次上传目录无效")
	}
	return nil
}

// LibraryImageInspector 通过内容嗅探确认字节是 PNG、JPEG、GIF 或 WebP 图片，不信任文件名和客户端类型。
type LibraryImageInspector struct{}

// Inspect 返回 MIME 类型与不带点的扩展名。
func (LibraryImageInspector) Inspect(data []byte) (string, string, error) {
	// contentType 是 net/http 嗅探出的类型。
	switch contentType := http.DetectContentType(data); contentType {
	case "image/png":
		return contentType, "png", nil
	case "image/jpeg":
		return contentType, "jpg", nil
	case "image/gif":
		return contentType, "gif", nil
	case "image/webp":
		return contentType, "webp", nil
	default:
		return "", "", errors.New("仅支持 PNG、JPEG、GIF、WebP 图片")
	}
}

// 编译期确认文件存储同时满足图片字节端口和批次快照端口。
var (
	_ libraryapp.ImageBlobStore       = (*LibraryFileStore)(nil)
	_ libraryapp.PublishSnapshotStore = (*LibraryFileStore)(nil)
	_ libraryapp.ImageInspector       = LibraryImageInspector{}
)
