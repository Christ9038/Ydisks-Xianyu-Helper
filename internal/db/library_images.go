package db

import (
	"context"
	"database/sql"
	"errors"
	"xianyu-go/internal/deliverytemplate"
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

// LibraryImageUsage 描述一处引用图片库图片的配置，用于删除图片前的引用检查。
type LibraryImageUsage struct {
	// ImageID 是被引用的图片主键。
	ImageID int64
	// Label 是面向用户的引用位置说明，例如“关键词「你好」”，不包含卡密、Cookie 或消息正文。
	Label string
}

// Usages 返回用户名下回复、卡密、快捷回复和发货模板对图片库图片的全部引用。
// 引用完整性靠本检查保证（这些表没有指向 library_images 的外键），读取失败必须向上返回而不是当作“无引用”。
func (l *LibraryImages) Usages(ctx context.Context, userID int64) ([]LibraryImageUsage, error) {
	// sources 保存各引用来源的查询和标签前缀；每条查询都返回 (image_id, name) 两列。
	sources := []struct {
		// label 是引用位置的中文前缀。
		label string
		// query 是按用户过滤并只返回已引用行的查询。
		query string
	}{
		{"关键词", `SELECT k.library_image_id, COALESCE(k.keyword,'') FROM keywords k JOIN cookies c ON c.id=k.cookie_id WHERE c.user_id=? AND k.library_image_id>0`},
		{"账号默认回复", `SELECT d.library_image_id, d.cookie_id FROM default_replies d JOIN cookies c ON c.id=d.cookie_id WHERE c.user_id=? AND d.library_image_id>0`},
		{"商品首次回复", `SELECT r.library_image_id, COALESCE(r.name,'') FROM item_greeting_rules r JOIN cookies c ON c.id=r.cookie_id WHERE c.user_id=? AND r.library_image_id>0`},
		{"图片卡密", `SELECT cd.library_image_id, COALESCE(cd.name,'') FROM cards cd WHERE cd.user_id=? AND cd.library_image_id>0`},
		{"快捷回复（账号", `SELECT q.library_image_id, q.cookie_id FROM chat_quick_replies q JOIN cookies c ON c.id=q.cookie_id WHERE c.user_id=? AND q.library_image_id>0`},
	}
	// usages 保存全部引用。
	var usages []LibraryImageUsage
	// source 是当前查询的引用来源。
	for _, source := range sources {
		// rows、err 保存当前来源的查询结果。
		rows, err := l.DB.QueryContext(ctx, source.query, userID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			// usage、name 保存当前行的引用与名称。
			var usage LibraryImageUsage
			// name 是被引用位置的名称（规则、卡密或模板名），用于拼接引用标签。
			var name string
			// scanErr 保存扫描引用行的失败原因，非空时终止查询。
			if scanErr := rows.Scan(&usage.ImageID, &name); scanErr != nil {
				rows.Close()
				return nil, scanErr
			}
			usage.Label = usageLabel(source.label, name)
			usages = append(usages, usage)
		}
		// iterErr 保存逐行读取的迭代错误。
		iterErr := rows.Err()
		rows.Close()
		if iterErr != nil {
			return nil, iterErr
		}
	}
	// templateRows、templateErr 保存未删除发货模板中的图片消息；图片令牌在 content 中，需在 Go 里解析主键。
	templateRows, templateErr := l.DB.QueryContext(ctx,
		`SELECT t.name, m.content FROM delivery_template_messages m JOIN delivery_templates t ON t.id=m.template_id
		 WHERE t.user_id=? AND t.deleted_at IS NULL AND m.content LIKE '{{library_image:%'`, userID)
	if templateErr != nil {
		return nil, templateErr
	}
	defer templateRows.Close()
	for templateRows.Next() {
		// templateName、content 保存模板名称和图片消息令牌。
		var templateName, content string
		// scanErr 保存扫描引用行的失败原因，非空时终止查询。
		if scanErr := templateRows.Scan(&templateName, &content); scanErr != nil {
			return nil, scanErr
		}
		// imageID 是从模板消息中解析出的图片库主键；ok 表示该消息是否为合法的图片库令牌。
		if imageID, ok := deliverytemplate.ImageID(content); ok {
			usages = append(usages, LibraryImageUsage{ImageID: imageID, Label: usageLabel("发货模板", templateName)})
		}
	}
	return usages, templateRows.Err()
}

// usageLabel 组合引用位置说明；名称为空时只保留前缀。
func usageLabel(prefix, name string) string {
	if name == "" {
		return prefix
	}
	if prefix == "快捷回复（账号" {
		return prefix + " " + name + "）"
	}
	return prefix + "「" + name + "」"
}
