package composition

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"xianyu-go/internal/adapter"
	itemapp "xianyu-go/internal/application/items"
	libraryapp "xianyu-go/internal/application/library"
	"xianyu-go/internal/uploadroot"
)

// maxLibraryPublishRows 是单次模板发布展开的最大行数，与批量铺货表格导入的单批上限保持一致。
const maxLibraryPublishRows = 50

// buildLibraryService 构造本地资源库应用服务：图片库、商品模板和「模板 × 账号」发布预检。
// 发布预检直接复用批量铺货的预检校验与批次落库，因此模板发布后的 worker、进度和重试与表格导入完全一致。
func buildLibraryService(dependencies Dependencies, batchPreview *itemapp.BatchPreviewService, previewPersistence *itemapp.BatchPreviewPersistenceService) (*libraryapp.Service, error) {
	// fileStore 把图片字节保存在上传根目录的 library 子目录；上传根目录与批量铺货共用同一个已持久化位置。
	fileStore, fileStoreErr := adapter.NewLibraryFileStore(uploadroot.Dir())
	if fileStoreErr != nil {
		return nil, fmt.Errorf("构造资源库文件存储失败: %w", fileStoreErr)
	}
	// imageRepository、templateRepository 是图片元数据与模板的数据库适配器。
	imageRepository := dependencies.ItemDependencies.NewLibraryImageRepository()
	// templateRepository 保存 NewLibraryTemplateRepository 的返回结果。
	templateRepository := dependencies.ItemDependencies.NewLibraryTemplateRepository()
	// images 是图片库用例。
	images, imagesErr := libraryapp.NewImageService(imageRepository, fileStore, adapter.LibraryImageInspector{})
	if imagesErr != nil {
		return nil, fmt.Errorf("构造图片库服务失败: %w", imagesErr)
	}
	// templates 是商品模板用例，自动化校验复用批量预检规则。
	templates, templatesErr := libraryapp.NewTemplateService(templateRepository, imageRepository, batchPreview)
	if templatesErr != nil {
		return nil, fmt.Errorf("构造商品模板服务失败: %w", templatesErr)
	}
	// publish 是模板发布预检用例。
	publish, publishErr := libraryapp.NewTemplatePublishService(templateRepository, imageRepository, fileStore, batchPreview, previewPersistence, newLibraryBatchID, maxLibraryPublishRows)
	if publishErr != nil {
		return nil, fmt.Errorf("构造模板发布服务失败: %w", publishErr)
	}
	return libraryapp.NewService(images, templates, publish), nil
}

// newLibraryBatchID 生成批次标识，格式与表格导入一致；随机源不可用时退化为时间戳。
func newLibraryBatchID() string {
	// buf 保存 12 字节随机数。
	buf := make([]byte, 12)
	// err 保存 Read 调用的失败原因。
	if _, err := rand.Read(buf); err != nil {
		return "batch_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return "batch_" + hex.EncodeToString(buf)
}

// buildCatalogAndLibrary 构造商品目录服务集合和本地资源库服务；资源库的发布预检复用目录服务的预检落库，因此两者一起构造。
func buildCatalogAndLibrary(dependencies Dependencies, sessionRecovery adapter.SessionRecoveryHandler, batchPreview *itemapp.BatchPreviewService) (itemCatalogServices, *libraryapp.Service, error) {
	// catalogServices、catalogErr 是商品目录服务集合及其构造错误。
	catalogServices, catalogErr := buildItemCatalogServices(dependencies, sessionRecovery)
	if catalogErr != nil {
		return itemCatalogServices{}, nil, catalogErr
	}
	// libraryService、libraryErr 是本地资源库应用服务及其构造错误。
	libraryService, libraryErr := buildLibraryService(dependencies, batchPreview, catalogServices.previewPersistence)
	if libraryErr != nil {
		return itemCatalogServices{}, nil, libraryErr
	}
	return catalogServices, libraryService, nil
}
