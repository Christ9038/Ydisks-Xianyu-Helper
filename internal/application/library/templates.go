package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	itemapp "xianyu-go/internal/application/items"
)

const (
	// maxTemplateImages 是商品主图数量上限，与批量发布一致。
	maxTemplateImages = 9
	// maxTemplateNameRunes 是模板名称长度上限。
	maxTemplateNameRunes = 100
	// maxTemplateTitleRunes 是商品标题长度上限，对应数据库列宽。
	maxTemplateTitleRunes = 255
)

// TemplateSpecs 是模板的多规格配置；规格图片引用图片库主键而不是文件路径。
type TemplateSpecs struct {
	// Specs 是规格维度，规格值的 ImageIndex 指向 SpecImageIDs。
	Specs []itemapp.PublishSpec `json:"specs"`
	// SKUs 是规格笛卡尔积的价格库存。
	SKUs []itemapp.PublishSKU `json:"skus"`
	// SpecImageIDs 是规格图片的图片库主键。
	SpecImageIDs []int64 `json:"spec_image_ids"`
}

// Template 是商品模板的应用模型。
type Template struct {
	// ID 是模板主键，新建前为 0。
	ID int64
	// UserID 是模板所属用户。
	UserID int64
	// Name 是资源库中的备注名。
	Name string
	// Title 是发布时的商品标题。
	Title string
	// Description 是发布时的商品描述。
	Description string
	// PriceCents 是单规格售价，单位为分；多规格时由 SKU 最低价推导。
	PriceCents int64
	// OriginalPriceCents 是原价，单位为分，0 表示不设置。
	OriginalPriceCents int64
	// Quantity 是单规格库存；多规格时由 SKU 库存汇总。
	Quantity int
	// PostageMode 是邮费模式 free 或 fixed。
	PostageMode string
	// PostageCents 是固定邮费，单位为分。
	PostageCents int64
	// Category 是类目；CatID 为空表示发布时沿用批次兜底或自动推荐。
	Category itemapp.BatchPreviewCategory
	// ImageIDs 是商品主图的图片库主键，顺序即发布顺序。
	ImageIDs []int64
	// Specs 是多规格配置，nil 表示单规格。
	Specs *TemplateSpecs
	// Automation 是发布后的自动发货等自动化配置。
	Automation itemapp.BatchPreviewAutomation
	// CreatedAt 是创建时间文本。
	CreatedAt string
	// UpdatedAt 是最近更新时间文本。
	UpdatedAt string
}

// ReferencedImageIDs 返回模板引用的全部图片（主图与规格图），已去重。
func (t Template) ReferencedImageIDs() []int64 {
	// seen 保存已出现的图片主键。
	seen := make(map[int64]struct{})
	// ids 保存去重后的结果，保持主图在前。
	var ids []int64
	// id 是当前候选图片主键。
	for _, id := range t.allImageIDs() {
		// ok 表示该图片主键是否已出现过。
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}

// allImageIDs 返回主图与规格图主键的拼接，不去重。
func (t Template) allImageIDs() []int64 {
	// ids 保存拼接结果。
	ids := append([]int64(nil), t.ImageIDs...)
	if t.Specs != nil {
		ids = append(ids, t.Specs.SpecImageIDs...)
	}
	return ids
}

// TemplateRepository 是商品模板持久化端口。
type TemplateRepository interface {
	// List 返回用户全部模板，按更新时间倒序。
	List(context.Context, int64) ([]Template, error)
	// Get 读取单个模板；不存在时返回 ErrTemplateNotFound。
	Get(context.Context, int64, int64) (Template, error)
	// Create 写入新模板并返回主键。
	Create(context.Context, Template) (int64, error)
	// Update 覆盖模板；不存在时返回 ErrTemplateNotFound。
	Update(context.Context, Template) error
	// Delete 删除模板；不存在时返回 ErrTemplateNotFound。
	Delete(context.Context, int64, int64) error
}

// AutomationValidator 复用批量预检的自动化校验规则，保证模板保存与发布使用同一套标准。
type AutomationValidator interface {
	// ValidateAutomationConfig 校验自动化配置的归属、动作范围及其与规格的绑定。
	ValidateAutomationConfig(ctx context.Context, userID int64, config itemapp.BatchPreviewAutomation, specs *itemapp.BatchSpecs) []string
}

// TemplateService 负责商品模板的增删改查与保存时校验。
type TemplateService struct {
	// repository 保存模板。
	repository TemplateRepository
	// images 校验模板引用的图片归属。
	images ImageRepository
	// automation 校验自动化配置。
	automation AutomationValidator
}

// NewTemplateService 创建模板服务并校验必需端口。
func NewTemplateService(repository TemplateRepository, images ImageRepository, automation AutomationValidator) (*TemplateService, error) {
	if repository == nil || images == nil || automation == nil {
		return nil, errors.New("商品模板依赖端口不能为空")
	}
	return &TemplateService{repository: repository, images: images, automation: automation}, nil
}

// ListTemplates 返回用户的全部模板。
func (s *TemplateService) ListTemplates(ctx context.Context, userID int64) ([]Template, error) {
	return s.repository.List(ctx, userID)
}

// GetTemplate 返回用户的单个模板。
func (s *TemplateService) GetTemplate(ctx context.Context, userID, id int64) (Template, error) {
	return s.repository.Get(ctx, userID, id)
}

// CreateTemplate 校验并创建模板。
func (s *TemplateService) CreateTemplate(ctx context.Context, input Template) (Template, error) {
	// normalized、err 保存规范化并通过校验的模板。
	normalized, err := s.normalize(ctx, input)
	if err != nil {
		return Template{}, err
	}
	// id、createErr 保存新模板主键。
	id, createErr := s.repository.Create(ctx, normalized)
	if createErr != nil {
		return Template{}, createErr
	}
	return s.repository.Get(ctx, normalized.UserID, id)
}

// UpdateTemplate 校验并覆盖已有模板；模板不属于用户时返回 ErrTemplateNotFound。
func (s *TemplateService) UpdateTemplate(ctx context.Context, input Template) (Template, error) {
	// err 保存 Get 调用的失败原因。
	if _, err := s.repository.Get(ctx, input.UserID, input.ID); err != nil {
		return Template{}, err
	}
	// normalized、err 保存规范化并通过校验的模板。
	normalized, err := s.normalize(ctx, input)
	if err != nil {
		return Template{}, err
	}
	// updateErr 保存 Update 调用的失败原因。
	if updateErr := s.repository.Update(ctx, normalized); updateErr != nil {
		return Template{}, updateErr
	}
	return s.repository.Get(ctx, normalized.UserID, normalized.ID)
}

// DeleteTemplate 删除模板；已排队批次使用的是发布时复制的快照，不受影响。
func (s *TemplateService) DeleteTemplate(ctx context.Context, userID, id int64) error {
	return s.repository.Delete(ctx, userID, id)
}

// normalize 清理文本、推导多规格的价格与库存，并执行全部业务校验。
func (s *TemplateService) normalize(ctx context.Context, input Template) (Template, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > maxTemplateNameRunes {
		return Template{}, invalid(fmt.Sprintf("模板名称必填且不超过 %d 个字符", maxTemplateNameRunes))
	}
	if input.Title == "" || utf8.RuneCountInString(input.Title) > maxTemplateTitleRunes {
		return Template{}, invalid("商品标题必填")
	}
	if input.PostageMode != "free" && input.PostageMode != "fixed" {
		return Template{}, invalid("邮费模式必须是 free 或 fixed")
	}
	if input.PostageMode == "free" {
		input.PostageCents = 0
	} else if input.PostageCents < 0 {
		return Template{}, invalid("固定邮费不能为负数")
	}
	if input.OriginalPriceCents < 0 {
		return Template{}, invalid("原价格式错误")
	}
	// err 保存 checkImages 调用的失败原因。
	if err := s.checkImages(ctx, input); err != nil {
		return Template{}, err
	}
	// specs 是用占位引用组装的批量规格快照，仅用于复用规格校验。
	specs := input.batchSpecs()
	// err 保存 ValidateBatchSpecs 调用的失败原因。
	if err := itemapp.ValidateBatchSpecs(specs); err != nil {
		return Template{}, invalid(err.Error())
	}
	if specs.IsMultiSpec() {
		// 多规格商品的顶层价格与库存由 SKU 推导，忽略客户端传值。
		input.PriceCents, input.Quantity = minSKUPrice(specs), specs.TotalQuantity()
	} else {
		input.Specs = nil
		if input.PriceCents <= 0 {
			return Template{}, invalid("价格必须大于 0")
		}
		if input.Quantity <= 0 {
			return Template{}, invalid("库存必须大于 0")
		}
	}
	// problems 保存自动化配置的校验提示。
	problems := s.automation.ValidateAutomationConfig(ctx, input.UserID, input.Automation, specs)
	if len(problems) > 0 {
		return Template{}, invalid(strings.Join(problems, "；"))
	}
	return input, nil
}

// checkImages 校验主图数量、规格图索引范围，以及全部图片都属于当前用户。
func (s *TemplateService) checkImages(ctx context.Context, input Template) error {
	if len(input.ImageIDs) == 0 {
		return invalid("至少选择 1 张商品图片")
	}
	if len(input.ImageIDs) > maxTemplateImages {
		return invalid(fmt.Sprintf("商品图片最多 %d 张", maxTemplateImages))
	}
	// distinct 保存主图去重集合，重复主图视为输入错误。
	distinct := make(map[int64]struct{}, len(input.ImageIDs))
	// id 是当前主图主键。
	for _, id := range input.ImageIDs {
		// ok 表示该图片主键是否重复。
		if _, ok := distinct[id]; ok {
			return invalid("商品图片不能重复")
		}
		distinct[id] = struct{}{}
	}
	// unique 保存主图与规格图的去重集合，用于归属核对。
	unique := input.ReferencedImageIDs()
	// owned、err 保存属于用户的图片。
	owned, err := s.images.GetMany(ctx, input.UserID, unique)
	if err != nil {
		return err
	}
	if len(owned) != len(unique) {
		return invalid("模板引用了不存在的图片")
	}
	return nil
}

// batchSpecs 把模板规格转换为批量规格快照；图片引用以主键占位，仅用于校验和索引范围检查。
func (t Template) batchSpecs() *itemapp.BatchSpecs {
	if t.Specs == nil {
		return nil
	}
	// refs 保存与 SpecImageIDs 等长的占位引用。
	refs := make([]string, len(t.Specs.SpecImageIDs))
	// index 是规格图片的下标。
	for index := range refs {
		refs[index] = fmt.Sprintf("image-%d", t.Specs.SpecImageIDs[index])
	}
	return &itemapp.BatchSpecs{Specs: t.Specs.Specs, SKUs: t.Specs.SKUs, SpecImages: refs}
}

// minSKUPrice 返回所有 SKU 的最低价，作为多规格商品的展示价。
func minSKUPrice(specs *itemapp.BatchSpecs) int64 {
	// lowest 保存当前最低价，初始为第一个 SKU。
	lowest := specs.SKUs[0].PriceCents
	// sku 是当前比较的 SKU。
	for _, sku := range specs.SKUs[1:] {
		if sku.PriceCents < lowest {
			lowest = sku.PriceCents
		}
	}
	return lowest
}
