package library

import (
	"context"
	"errors"
	"fmt"
	"strings"

	itemapp "xianyu-go/internal/application/items"
)

// defaultPublishIntervalSeconds 是未指定发布间隔时沿用的批量铺货默认间隔。
const defaultPublishIntervalSeconds = 5

// PublishSnapshotStore 是批次图片快照端口：把图片库文件复制到批次专属上传目录，
// 之后模板修改或图片删除都不会影响已排队的批次，且批次目录沿用现有的过期清理。
type PublishSnapshotStore interface {
	// Prepare 为批次创建专属上传目录并返回其绝对路径。
	Prepare(ctx context.Context, batchID string) (uploadDir string, err error)
	// CopyImage 把图片库字节复制为上传目录下的 name 文件。
	CopyImage(ctx context.Context, uploadDir, storageKey, name string) error
	// Discard 在预检失败时删除批次目录。
	Discard(ctx context.Context, uploadDir string) error
}

// PreviewRowsValidator 复用批量预检对结构化行的确定性校验。
type PreviewRowsValidator interface {
	// PreviewRows 校验结构化预检行，逐行错误写入各行。
	PreviewRows(ctx context.Context, input itemapp.BatchPreviewInput, rows []itemapp.BatchPreviewRow) ([]itemapp.BatchPreviewRow, error)
}

// PreviewPersister 复用批量预检的批次落库。
type PreviewPersister interface {
	// Persist 持久化预检批次与全部明细。
	Persist(ctx context.Context, batch itemapp.BatchPreviewPersistenceBatch, rows []itemapp.BatchPreviewRow) (itemapp.BatchPreviewPersistenceResult, error)
}

// PublishPreviewInput 是「模板 × 账号」批量预检的输入。
type PublishPreviewInput struct {
	// UserID 是发起发布的用户。
	UserID int64
	// TemplateIDs 是选中的模板，顺序决定行顺序。
	TemplateIDs []int64
	// CookieIDs 是目标账号，每个模板会发布到每个账号。
	CookieIDs []string
	// Location 是批次统一发货地。
	Location itemapp.Location
	// PublishIntervalSeconds 是相邻两次最终发布请求的最小间隔；非正数使用默认值。
	PublishIntervalSeconds int
}

// TemplatePublishService 把商品模板展开成批量铺货预检批次，后续发布完全沿用现有批量 worker。
type TemplatePublishService struct {
	// templates 读取模板。
	templates TemplateRepository
	// images 读取模板引用图片的元数据。
	images ImageRepository
	// snapshots 复制图片快照。
	snapshots PublishSnapshotStore
	// validator 校验展开后的行。
	validator PreviewRowsValidator
	// persister 保存预检批次。
	persister PreviewPersister
	// newBatchID 生成批次标识。
	newBatchID func() string
	// maxRows 是单批最大行数，与批量铺货上限保持一致。
	maxRows int
}

// NewTemplatePublishService 创建发布预检服务并校验必需依赖。
func NewTemplatePublishService(templates TemplateRepository, images ImageRepository, snapshots PublishSnapshotStore, validator PreviewRowsValidator, persister PreviewPersister, newBatchID func() string, maxRows int) (*TemplatePublishService, error) {
	if templates == nil || images == nil || snapshots == nil || validator == nil || persister == nil || newBatchID == nil || maxRows <= 0 {
		return nil, errors.New("模板发布依赖不能为空")
	}
	return &TemplatePublishService{templates: templates, images: images, snapshots: snapshots, validator: validator, persister: persister, newBatchID: newBatchID, maxRows: maxRows}, nil
}

// PreviewPublish 展开「模板 × 账号」并生成 preview 状态的批次；预检阶段不访问任何平台接口。
func (s *TemplatePublishService) PreviewPublish(ctx context.Context, input PublishPreviewInput) (itemapp.BatchPreviewPersistenceResult, error) {
	// templateIDs、cookieIDs 是去重后的选择。
	templateIDs, cookieIDs := uniqueInts(input.TemplateIDs), uniqueStrings(input.CookieIDs)
	if len(templateIDs) == 0 {
		return itemapp.BatchPreviewPersistenceResult{}, invalid("请选择要发布的商品模板")
	}
	if len(cookieIDs) == 0 {
		return itemapp.BatchPreviewPersistenceResult{}, invalid("请选择要发布到的账号")
	}
	// total 保存 len 的返回结果。
	if total := len(templateIDs) * len(cookieIDs); total > s.maxRows {
		return itemapp.BatchPreviewPersistenceResult{}, invalid(fmt.Sprintf("单次最多发布 %d 条商品，当前选择 %d 条", s.maxRows, total))
	}
	// templates 按选择顺序读取，任何一个不属于用户都整体失败。
	templates := make([]Template, 0, len(templateIDs))
	// id 是当前读取的模板主键。
	for _, id := range templateIDs {
		// template 保存 Get 的返回结果，err 保存其失败原因。
		template, err := s.templates.Get(ctx, input.UserID, id)
		if err != nil {
			return itemapp.BatchPreviewPersistenceResult{}, err
		}
		templates = append(templates, template)
	}
	// batchID 标识新批次；uploadDir 是其专属上传目录。
	batchID := s.newBatchID()
	// uploadDir 保存 Prepare 的返回结果，err 保存其失败原因。
	uploadDir, err := s.snapshots.Prepare(ctx, batchID)
	if err != nil {
		return itemapp.BatchPreviewPersistenceResult{}, err
	}
	// result、previewErr 保存预检结果；失败时回收批次目录。
	result, previewErr := s.buildAndPersist(ctx, input, batchID, uploadDir, templates, cookieIDs)
	if previewErr != nil {
		_ = s.snapshots.Discard(ctx, uploadDir)
		return itemapp.BatchPreviewPersistenceResult{}, previewErr
	}
	return result, nil
}

// buildAndPersist 复制图片快照、展开行、校验并落库。
func (s *TemplatePublishService) buildAndPersist(ctx context.Context, input PublishPreviewInput, batchID, uploadDir string, templates []Template, cookieIDs []string) (itemapp.BatchPreviewPersistenceResult, error) {
	// refs 保存图片主键到批次目录内文件名的映射。
	refs, err := s.copyImages(ctx, input.UserID, uploadDir, templates)
	if err != nil {
		return itemapp.BatchPreviewPersistenceResult{}, err
	}
	// rows 按「模板 × 账号」展开。
	rows := make([]itemapp.BatchPreviewRow, 0, len(templates)*len(cookieIDs))
	// template 是当前模板。
	for _, template := range templates {
		// cookieID 是当前目标账号。
		for _, cookieID := range cookieIDs {
			rows = append(rows, template.previewRow(cookieID, refs))
		}
	}
	// validated、validateErr 保存逐行校验结果。
	validated, validateErr := s.validator.PreviewRows(ctx, itemapp.BatchPreviewInput{UserID: input.UserID, DefaultCookieID: cookieIDs[0], UploadDir: uploadDir}, rows)
	if validateErr != nil {
		return itemapp.BatchPreviewPersistenceResult{}, validateErr
	}
	// interval 是生效的发布间隔。
	interval := input.PublishIntervalSeconds
	if interval <= 0 {
		interval = defaultPublishIntervalSeconds
	}
	return s.persister.Persist(ctx, itemapp.BatchPreviewPersistenceBatch{ID: batchID, UserID: input.UserID, DefaultCookieID: cookieIDs[0],
		Filename: "商品模板发布", UploadDir: uploadDir, Location: input.Location, PublishIntervalSeconds: interval, Status: "preview"}, validated)
}

// copyImages 把所有模板用到的图片复制进批次目录，返回图片主键到文件名的映射。
func (s *TemplatePublishService) copyImages(ctx context.Context, userID int64, uploadDir string, templates []Template) (map[int64]string, error) {
	// ids 汇总所有模板引用的图片并去重。
	var ids []int64
	// template 是当前模板。
	for _, template := range templates {
		ids = append(ids, template.allImageIDs()...)
	}
	ids = uniqueInts(ids)
	// images、err 保存属于用户的图片元数据。
	images, err := s.images.GetMany(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	if len(images) != len(ids) {
		return nil, invalid("模板引用的图片已被删除，请编辑模板后重试")
	}
	// refs 保存映射结果。
	refs := make(map[int64]string, len(images))
	// image 是当前复制的图片。
	for _, image := range images {
		// name 是批次目录内的文件名，由主键和扩展名组成，不含用户输入。
		name := fmt.Sprintf("lib_%d%s", image.ID, extensionOf(image.StorageKey))
		// copyErr 保存 CopyImage 调用的失败原因。
		if copyErr := s.snapshots.CopyImage(ctx, uploadDir, image.StorageKey, name); copyErr != nil {
			return nil, copyErr
		}
		refs[image.ID] = name
	}
	return refs, nil
}

// previewRow 把模板展开为发往指定账号的预检行；多规格时价格取最低 SKU 价、库存取各 SKU 之和。
func (t Template) previewRow(cookieID string, refs map[int64]string) itemapp.BatchPreviewRow {
	// images 是商品主图在批次目录内的文件名，保持模板顺序。
	images := make([]string, 0, len(t.ImageIDs))
	// id 是当前主图主键。
	for _, id := range t.ImageIDs {
		images = append(images, refs[id])
	}
	// row 保存展开结果。
	row := itemapp.BatchPreviewRow{
		CookieID: cookieID, Title: t.Title, Description: t.Description, Price: centsText(t.PriceCents),
		Quantity: t.Quantity, PostageMode: t.PostageMode, Postage: centsText(t.PostageCents), Images: images,
		Category: t.Category, Automation: t.Automation,
	}
	if t.OriginalPriceCents > 0 {
		row.OriginalPrice = centsText(t.OriginalPriceCents)
	}
	if t.Specs != nil {
		// specImages 是规格图片在批次目录内的文件名。
		specImages := make([]string, 0, len(t.Specs.SpecImageIDs))
		// id 是当前规格图片主键。
		for _, id := range t.Specs.SpecImageIDs {
			specImages = append(specImages, refs[id])
		}
		row.Specs = &itemapp.BatchSpecs{Specs: t.Specs.Specs, SKUs: t.Specs.SKUs, SpecImages: specImages}
	}
	return row
}

// centsText 把分转换为两位小数的元文本。
func centsText(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

// extensionOf 返回存储路径的扩展名（含点），没有扩展名时返回空串。
func extensionOf(key string) string {
	// index 保存 LastIndex 的返回结果。
	if index := strings.LastIndex(key, "."); index >= 0 {
		return key[index:]
	}
	return ""
}

// uniqueInts 去重并保持首次出现的顺序。
func uniqueInts(values []int64) []int64 {
	// seen 保存已出现的值。
	seen := make(map[int64]struct{}, len(values))
	// result 保存去重结果。
	var result []int64
	// value 是当前候选值。
	for _, value := range values {
		// ok 表示该模板主键是否已出现过。
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

// uniqueStrings 去除空白与重复并保持首次出现的顺序。
func uniqueStrings(values []string) []string {
	// seen 保存已出现的值。
	seen := make(map[string]struct{}, len(values))
	// result 保存去重结果。
	var result []string
	// value 是当前候选值。
	for _, value := range values {
		value = strings.TrimSpace(value)
		// ok 表示该账号标识是否已出现过。
		if _, ok := seen[value]; value != "" && !ok {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}
