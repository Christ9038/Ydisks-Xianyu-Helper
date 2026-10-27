package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
)

const (
	// MaxImageBytes 是图片库单张图片的大小上限，与批量发布读取图片的上限一致。
	MaxImageBytes = 10 << 20
	// MaxUploadFiles 是单次上传请求允许携带的图片数量上限。
	MaxUploadFiles = 20
)

// Image 是图片库中一张图片的非二进制元数据。
type Image struct {
	// ID 是图片主键。
	ID int64
	// UserID 是图片所属用户。
	UserID int64
	// Filename 是上传时的原始文件名，仅用于展示。
	Filename string
	// StorageKey 是服务端生成的相对存储路径，不接受用户输入。
	StorageKey string
	// ContentType 是内容嗅探确认的 MIME 类型。
	ContentType string
	// SizeBytes 是图片字节数。
	SizeBytes int64
	// SHA256 是图片内容摘要，同一用户内用于去重。
	SHA256 string
	// CreatedAt 是入库时间文本。
	CreatedAt string
}

// TemplateImageRef 描述某个模板引用了哪些图片，用于删除图片前的引用检查。
type TemplateImageRef struct {
	// TemplateName 是引用图片的模板名称。
	TemplateName string
	// ImageIDs 是该模板引用的全部图片（主图与规格图）。
	ImageIDs []int64
}

// ImageRepository 是图片元数据持久化端口。
type ImageRepository interface {
	// List 返回用户全部图片，按上传时间倒序。
	List(context.Context, int64) ([]Image, error)
	// Get 读取单张图片；不存在或不属于用户时返回 ErrImageNotFound。
	Get(context.Context, int64, int64) (Image, error)
	// GetMany 读取属于用户的一组图片，缺失的主键被忽略。
	GetMany(context.Context, int64, []int64) ([]Image, error)
	// FindBySHA256 读取用户名下同摘要图片；不存在时返回 ErrImageNotFound。
	FindBySHA256(context.Context, int64, string) (Image, error)
	// Insert 写入图片元数据；并发写入同内容时返回已有记录和 created=false。
	Insert(context.Context, Image) (Image, bool, error)
	// Delete 删除图片元数据；不存在时返回 ErrImageNotFound。
	Delete(context.Context, int64, int64) error
	// TemplateImageRefs 返回用户全部模板的图片引用。
	TemplateImageRefs(context.Context, int64) ([]TemplateImageRef, error)
}

// ImageBlobStore 是图片字节的本地存储端口；key 由服务端生成，实现方负责路径安全。
type ImageBlobStore interface {
	// Save 原子写入图片字节。
	Save(ctx context.Context, key string, data []byte) error
	// Read 读取图片字节。
	Read(ctx context.Context, key string) ([]byte, error)
	// Remove 删除图片字节，文件不存在视为成功。
	Remove(ctx context.Context, key string) error
}

// ImageInspector 通过内容嗅探确认字节是受支持的图片，不信任客户端声明的类型。
type ImageInspector interface {
	// Inspect 返回 MIME 类型和不带点的扩展名；不是受支持的图片时返回错误。
	Inspect(data []byte) (contentType, extension string, err error)
}

// ImageUpload 是一次上传中的一个文件。
type ImageUpload struct {
	// Filename 是客户端提供的原始文件名，仅用于展示。
	Filename string
	// Data 是文件内容。
	Data []byte
}

// UploadResult 是单个文件的上传结果；失败不会中断同一请求中的其他文件。
type UploadResult struct {
	// Filename 是对应上传文件的原始文件名。
	Filename string
	// Image 是上传成功或命中去重时的图片元数据。
	Image Image
	// Duplicate 表示同内容图片已存在，未重复保存。
	Duplicate bool
	// Error 是该文件失败时的用户可见原因，成功时为空。
	Error string
}

// ImageService 负责图片库的上传、读取和删除用例。
type ImageService struct {
	// repository 保存图片元数据。
	repository ImageRepository
	// blobs 保存图片字节。
	blobs ImageBlobStore
	// inspector 校验图片内容。
	inspector ImageInspector
}

// NewImageService 创建图片库服务并校验必需端口。
func NewImageService(repository ImageRepository, blobs ImageBlobStore, inspector ImageInspector) (*ImageService, error) {
	if repository == nil || blobs == nil || inspector == nil {
		return nil, errors.New("图片库依赖端口不能为空")
	}
	return &ImageService{repository: repository, blobs: blobs, inspector: inspector}, nil
}

// ListImages 返回用户的全部图片。
func (s *ImageService) ListImages(ctx context.Context, userID int64) ([]Image, error) {
	return s.repository.List(ctx, userID)
}

// UploadImages 保存一组图片；按内容摘要去重，单个文件失败只记录在对应结果中。
func (s *ImageService) UploadImages(ctx context.Context, userID int64, uploads []ImageUpload) ([]UploadResult, error) {
	if len(uploads) == 0 {
		return nil, invalid("请选择要上传的图片")
	}
	if len(uploads) > MaxUploadFiles {
		return nil, invalid(fmt.Sprintf("单次最多上传 %d 张图片", MaxUploadFiles))
	}
	// results 按上传顺序保存每个文件的结果。
	results := make([]UploadResult, 0, len(uploads))
	// upload 是当前处理的文件。
	for _, upload := range uploads {
		// result、err 保存当前文件的处理结果；仓储和存储故障属于系统错误，直接中断请求。
		result, err := s.uploadOne(ctx, userID, upload)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

// uploadOne 处理单个文件：校验大小与内容、去重、写入字节再写入元数据；元数据写入失败时回收刚写入的字节。
func (s *ImageService) uploadOne(ctx context.Context, userID int64, upload ImageUpload) (UploadResult, error) {
	// result 先带上文件名，校验失败时直接返回。
	result := UploadResult{Filename: upload.Filename}
	if len(upload.Data) == 0 {
		result.Error = "图片文件为空"
		return result, nil
	}
	if len(upload.Data) > MaxImageBytes {
		result.Error = "图片不能超过 10MB"
		return result, nil
	}
	// contentType、extension、inspectErr 保存内容嗅探结果。
	contentType, extension, inspectErr := s.inspector.Inspect(upload.Data)
	if inspectErr != nil {
		result.Error = inspectErr.Error()
		return result, nil
	}
	// sum、digest 是内容摘要及其十六进制文本。
	sum := sha256.Sum256(upload.Data)
	// digest 保存 EncodeToString 的返回结果。
	digest := hex.EncodeToString(sum[:])
	// existing、findErr 保存同内容图片的查询结果。
	existing, findErr := s.repository.FindBySHA256(ctx, userID, digest)
	if findErr == nil {
		result.Image, result.Duplicate = existing, true
		return result, nil
	}
	if !errors.Is(findErr, ErrImageNotFound) {
		return result, findErr
	}
	// key 是服务端生成的存储路径，完全不使用用户提供的文件名。
	key := strconv.FormatInt(userID, 10) + "/" + digest + "." + extension
	// saveErr 保存 Save 调用的失败原因。
	if saveErr := s.blobs.Save(ctx, key, upload.Data); saveErr != nil {
		return result, saveErr
	}
	// saved、created、insertErr 保存元数据写入结果。
	saved, created, insertErr := s.repository.Insert(ctx, Image{UserID: userID, Filename: upload.Filename, StorageKey: key,
		ContentType: contentType, SizeBytes: int64(len(upload.Data)), SHA256: digest})
	if insertErr != nil {
		_ = s.blobs.Remove(ctx, key)
		return result, insertErr
	}
	result.Image, result.Duplicate = saved, !created
	return result, nil
}

// ImageContent 返回图片元数据与字节，供内容下载端点使用。
func (s *ImageService) ImageContent(ctx context.Context, userID, id int64) (Image, []byte, error) {
	// image、err 保存元数据查询结果。
	image, err := s.repository.Get(ctx, userID, id)
	if err != nil {
		return Image{}, nil, err
	}
	// data、readErr 保存字节读取结果。
	data, readErr := s.blobs.Read(ctx, image.StorageKey)
	if readErr != nil {
		return Image{}, nil, readErr
	}
	return image, data, nil
}

// DeleteImage 删除未被模板引用的图片：先删元数据再删字节，字节删除失败不回滚，避免出现悬空元数据。
func (s *ImageService) DeleteImage(ctx context.Context, userID, id int64) error {
	// image、err 保存待删除图片，同时确认归属。
	image, err := s.repository.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	// refs、refsErr 保存模板对图片的引用。
	refs, refsErr := s.repository.TemplateImageRefs(ctx, userID)
	if refsErr != nil {
		return refsErr
	}
	// users 保存引用该图片的模板名称。
	var users []string
	// ref 是当前模板的引用集合。
	for _, ref := range refs {
		// imageID 是模板引用的某张图片。
		for _, imageID := range ref.ImageIDs {
			if imageID == id {
				users = append(users, ref.TemplateName)
				break
			}
		}
	}
	if len(users) > 0 {
		return &ImageInUseError{Templates: users}
	}
	// deleteErr 保存 Delete 调用的失败原因。
	if deleteErr := s.repository.Delete(ctx, userID, id); deleteErr != nil {
		return deleteErr
	}
	// 字节删除失败只留下孤立文件，不影响用户可见状态；文件按摘要命名，之后重新上传同内容会被覆盖。
	_ = s.blobs.Remove(ctx, image.StorageKey)
	return nil
}
