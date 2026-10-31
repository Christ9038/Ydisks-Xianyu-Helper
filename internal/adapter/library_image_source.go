package adapter

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"xianyu-go/internal/db"
	"xianyu-go/internal/uploadroot"
)

// ErrLibraryImageUnavailable 表示图片库图片不存在、不属于账号所有者，或字节无法读取。
// 调用方应把它视为确定未发送：此时尚未向平台发起任何请求。
var ErrLibraryImageUnavailable = errors.New("图片库图片不可用")

// LibraryImageSource 按“发送账号的所有者”从图片库读取图片字节，供聊天回复与自动化发货共用。
// 它只读数据库元数据和本地文件，不触碰账号凭证；没有可变状态，可被多个 goroutine 并发使用。
type LibraryImageSource struct {
	// store 提供账号所有者和图片元数据查询。
	store *db.Store
	// files 提供图片字节读取。
	files *LibraryFileStore
}

// NewLibraryImageSource 创建图片库读取来源；上传根目录沿用资源库的 uploadroot 配置。
func NewLibraryImageSource(store *db.Store) (*LibraryImageSource, error) {
	// files、filesErr 保存沿用资源库上传根目录构造的文件存储及其构造错误。
	files, filesErr := NewLibraryFileStore(uploadroot.Dir())
	if filesErr != nil {
		return nil, fmt.Errorf("构造图片库读取来源失败: %w", filesErr)
	}
	return &LibraryImageSource{store: store, files: files}, nil
}

// ReadLibraryImage 读取 accountID 所属用户图片库中的一张图片。
// 返回值与 chat.ImageURLDownloader 同形（字节、媒体类型、文件名），下游继续走统一的上传与真实宽高链路。
func (s *LibraryImageSource) ReadLibraryImage(ctx context.Context, accountID string, imageID int64) ([]byte, string, string, error) {
	if s == nil || s.store == nil || s.store.Cookies == nil || s.store.LibraryImages == nil || s.files == nil {
		return nil, "", "", fmt.Errorf("%w: 图片库读取来源未装配", ErrLibraryImageUnavailable)
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" || imageID <= 0 {
		return nil, "", "", fmt.Errorf("%w: 账号或图片标识无效", ErrLibraryImageUnavailable)
	}
	// ownerID 是发送账号的所有者；图片库按用户隔离，账号只能发送自己所有者的图片。
	ownerID, ownerErr := s.store.Cookies.GetOwnerID(ctx, accountID)
	if ownerErr != nil {
		return nil, "", "", fmt.Errorf("%w: 无法确认账号所有者", ErrLibraryImageUnavailable)
	}
	// image 保存按 (用户, 图片) 过滤后的元数据，他人图片与不存在的图片都表现为未找到。
	image, imageErr := s.store.LibraryImages.Get(ctx, ownerID, imageID)
	if imageErr != nil || image == nil {
		return nil, "", "", fmt.Errorf("%w: 图片不存在或已被删除", ErrLibraryImageUnavailable)
	}
	// data 保存图片字节；读取失败不暴露本地路径。
	data, readErr := s.files.Read(ctx, image.StorageKey)
	if readErr != nil || len(data) == 0 {
		return nil, "", "", fmt.Errorf("%w: 图片文件无法读取", ErrLibraryImageUnavailable)
	}
	// filename 优先使用上传名；缺失时回退到存储键的文件名，保证平台上传有扩展名。
	filename := strings.TrimSpace(image.Filename)
	if filename == "" {
		filename = filepath.Base(image.StorageKey)
	}
	return data, image.ContentType, filename, nil
}
