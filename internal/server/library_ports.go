package server

import (
	"context"

	itemapp "xianyu-go/internal/application/items"
	libraryapp "xianyu-go/internal/application/library"
)

// LibraryPort 定义本地资源库能力：图片库、商品模板、「模板 × 账号」发布预检，以及 Excel 导入商品模板的预检、分段提交与丢弃。
type LibraryPort interface {
	ListImages(context.Context, int64) ([]libraryapp.Image, error)
	UploadImages(context.Context, int64, []libraryapp.ImageUpload) ([]libraryapp.UploadResult, error)
	ImageContent(context.Context, int64, int64) (libraryapp.Image, []byte, error)
	DeleteImage(context.Context, int64, int64) error
	ListTemplates(context.Context, int64) ([]libraryapp.Template, error)
	GetTemplate(context.Context, int64, int64) (libraryapp.Template, error)
	CreateTemplate(context.Context, libraryapp.Template) (libraryapp.Template, error)
	UpdateTemplate(context.Context, libraryapp.Template) (libraryapp.Template, error)
	DeleteTemplate(context.Context, int64, int64) error
	PreviewPublish(context.Context, libraryapp.PublishPreviewInput) (itemapp.BatchPreviewPersistenceResult, error)
	// PreviewImport 解析 Excel 表格与可选图片压缩包并逐行预检，有可导入行时建立暂存任务。
	PreviewImport(context.Context, libraryapp.PreviewImportInput) (libraryapp.ImportPreview, error)
	// CommitImport 以当前用户身份继续导入指定任务中待导入的行，返回本次提交后的进度。
	CommitImport(context.Context, int64, string) (libraryapp.ImportProgress, error)
	// DiscardImport 删除当前用户指定导入任务的暂存，已创建的模板保留。
	DiscardImport(context.Context, int64, string) error
}
