package db

import (
	"context"
	"database/sql"
	"errors"
)

// LibraryImage 对应 library_images，只保存图片元数据；图片字节位于上传根目录的 library 子目录。
type LibraryImage struct {
	// ID 是图片主键。
	ID int64
	// UserID 是图片所属用户。
	UserID int64
	// Filename 是用户上传时的原始文件名，仅用于展示。
	Filename string
	// StorageKey 是服务端生成的相对存储路径，格式为 <user_id>/<sha256>.<ext>。
	StorageKey string
	// ContentType 是经内容嗅探确认的图片 MIME 类型。
	ContentType string
	// SizeBytes 是图片字节数。
	SizeBytes int64
	// SHA256 是图片内容摘要，同一用户内用于去重。
	SHA256 string
	// CreatedAt 是入库时间文本，沿用数据库时间格式。
	CreatedAt string
}

// LibraryImages 是图片库元数据的数据库访问入口。
type LibraryImages struct {
	// DB 是共享数据库连接池。
	DB *sql.DB
	// Dialect 是当前数据库方言，用于获取自增主键。
	Dialect Dialect
}

// libraryImageColumns 是读取图片元数据时统一使用的列清单。
const libraryImageColumns = `id,user_id,filename,storage_key,content_type,size_bytes,sha256,created_at`

// scanLibraryImage 把一行查询结果读入图片元数据模型。
func scanLibraryImage(scanner interface{ Scan(...any) error }) (LibraryImage, error) {
	// image 保存当前扫描出的图片元数据。
	var image LibraryImage
	// err 保存扫描错误。
	err := scanner.Scan(&image.ID, &image.UserID, &image.Filename, &image.StorageKey, &image.ContentType, &image.SizeBytes, &image.SHA256, &image.CreatedAt)
	return image, err
}

// List 返回用户的全部图片，按上传时间倒序。
func (l *LibraryImages) List(ctx context.Context, userID int64) ([]LibraryImage, error) {
	// rows、err 保存图片列表查询结果。
	rows, err := l.DB.QueryContext(ctx, `SELECT `+libraryImageColumns+` FROM library_images WHERE user_id=? ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// images 保存读取到的图片元数据。
	images := []LibraryImage{}
	for rows.Next() {
		// image、scanErr 保存当前行及其扫描错误。
		image, scanErr := scanLibraryImage(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		images = append(images, image)
	}
	return images, rows.Err()
}

// Get 按用户隔离读取单张图片；不存在或不属于该用户时返回 ErrNotFound。
func (l *LibraryImages) Get(ctx context.Context, userID, id int64) (*LibraryImage, error) {
	// image、err 保存查询结果。
	image, err := scanLibraryImage(l.DB.QueryRowContext(ctx, `SELECT `+libraryImageColumns+` FROM library_images WHERE id=? AND user_id=?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &image, nil
}

// GetMany 按用户隔离读取一组图片，返回的集合只包含属于该用户的图片。
func (l *LibraryImages) GetMany(ctx context.Context, userID int64, ids []int64) ([]LibraryImage, error) {
	// images 保存按请求顺序读取的图片。
	images := make([]LibraryImage, 0, len(ids))
	// id 是当前待读取的图片主键。
	for _, id := range ids {
		// image、err 保存单张图片的读取结果。
		image, err := l.Get(ctx, userID, id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		images = append(images, *image)
	}
	return images, nil
}

// FindBySHA256 读取用户名下相同内容摘要的图片；不存在时返回 ErrNotFound。
func (l *LibraryImages) FindBySHA256(ctx context.Context, userID int64, sha256 string) (*LibraryImage, error) {
	// image、err 保存按摘要查询的结果。
	image, err := scanLibraryImage(l.DB.QueryRowContext(ctx, `SELECT `+libraryImageColumns+` FROM library_images WHERE user_id=? AND sha256=?`, userID, sha256))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &image, nil
}

// Insert 写入图片元数据并返回带主键的记录；并发上传同内容时返回已存在的记录与 created=false。
func (l *LibraryImages) Insert(ctx context.Context, image LibraryImage) (LibraryImage, bool, error) {
	// id、err 保存新记录主键及写入错误。
	id, err := insertReturningID(ctx, l.DB, l.Dialect,
		`INSERT INTO library_images (user_id, filename, storage_key, content_type, size_bytes, sha256) VALUES (?,?,?,?,?,?)`,
		image.UserID, image.Filename, image.StorageKey, image.ContentType, image.SizeBytes, image.SHA256)
	if err != nil {
		// existing、findErr 保存并发写入场景下已存在的同摘要记录。
		existing, findErr := l.FindBySHA256(ctx, image.UserID, image.SHA256)
		if findErr != nil {
			return LibraryImage{}, false, err
		}
		return *existing, false, nil
	}
	// created、getErr 保存刚写入记录的完整内容（含数据库生成的时间）。
	created, getErr := l.Get(ctx, image.UserID, id)
	if getErr != nil {
		return LibraryImage{}, false, getErr
	}
	return *created, true, nil
}

// Delete 删除用户名下的图片元数据；记录不存在返回 ErrNotFound。
func (l *LibraryImages) Delete(ctx context.Context, userID, id int64) error {
	// res、err 保存删除结果。
	res, err := l.DB.ExecContext(ctx, `DELETE FROM library_images WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	// affected 保存受影响行数，用于识别不存在的图片。
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}
