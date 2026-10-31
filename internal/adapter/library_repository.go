package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	libraryapp "xianyu-go/internal/application/library"
	"xianyu-go/internal/db"
)

// LibraryImageRepository 把图片库元数据数据库仓储适配为应用层端口；不接触图片字节。
type LibraryImageRepository struct {
	// store 提供图片与模板表访问。
	store *db.Store
}

// NewLibraryImageRepository 创建图片元数据适配器。
func NewLibraryImageRepository(store *db.Store) *LibraryImageRepository {
	return &LibraryImageRepository{store: store}
}

// validate 确认依赖已装配。
func (r *LibraryImageRepository) validate() error {
	if r == nil || r.store == nil || r.store.LibraryImages == nil || r.store.ProductTemplates == nil {
		return errors.New("图片库数据库适配器未初始化")
	}
	return nil
}

// libraryImageModel 把数据库图片转换为应用模型。
func libraryImageModel(row db.LibraryImage) libraryapp.Image {
	return libraryapp.Image{ID: row.ID, UserID: row.UserID, Filename: row.Filename, StorageKey: row.StorageKey,
		ContentType: row.ContentType, SizeBytes: row.SizeBytes, SHA256: row.SHA256, CreatedAt: row.CreatedAt}
}

// mapLibraryImageError 把数据库不存在错误转换为应用层错误。
func mapLibraryImageError(err error) error {
	if errors.Is(err, db.ErrNotFound) {
		return libraryapp.ErrImageNotFound
	}
	return err
}

// List 返回用户全部图片。
func (r *LibraryImageRepository) List(ctx context.Context, userID int64) ([]libraryapp.Image, error) {
	// err 保存 validate 调用的失败原因。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// rows、err 保存数据库图片列表。
	rows, err := r.store.LibraryImages.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	// images 保存转换后的应用模型。
	images := make([]libraryapp.Image, 0, len(rows))
	// row 是当前数据库图片。
	for _, row := range rows {
		images = append(images, libraryImageModel(row))
	}
	return images, nil
}

// Get 读取单张图片。
func (r *LibraryImageRepository) Get(ctx context.Context, userID, id int64) (libraryapp.Image, error) {
	// err 保存 validate 调用的失败原因。
	if err := r.validate(); err != nil {
		return libraryapp.Image{}, err
	}
	// row、err 保存数据库查询结果。
	row, err := r.store.LibraryImages.Get(ctx, userID, id)
	if err != nil {
		return libraryapp.Image{}, mapLibraryImageError(err)
	}
	return libraryImageModel(*row), nil
}

// GetMany 读取属于用户的一组图片。
func (r *LibraryImageRepository) GetMany(ctx context.Context, userID int64, ids []int64) ([]libraryapp.Image, error) {
	// err 保存 validate 调用的失败原因。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// rows、err 保存数据库查询结果。
	rows, err := r.store.LibraryImages.GetMany(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	// images 保存转换后的应用模型。
	images := make([]libraryapp.Image, 0, len(rows))
	// row 是当前数据库图片。
	for _, row := range rows {
		images = append(images, libraryImageModel(row))
	}
	return images, nil
}

// FindBySHA256 按内容摘要查找用户图片。
func (r *LibraryImageRepository) FindBySHA256(ctx context.Context, userID int64, sha256 string) (libraryapp.Image, error) {
	// err 保存 validate 调用的失败原因。
	if err := r.validate(); err != nil {
		return libraryapp.Image{}, err
	}
	// row、err 保存数据库查询结果。
	row, err := r.store.LibraryImages.FindBySHA256(ctx, userID, sha256)
	if err != nil {
		return libraryapp.Image{}, mapLibraryImageError(err)
	}
	return libraryImageModel(*row), nil
}

// Insert 写入图片元数据。
func (r *LibraryImageRepository) Insert(ctx context.Context, image libraryapp.Image) (libraryapp.Image, bool, error) {
	// err 保存 validate 调用的失败原因。
	if err := r.validate(); err != nil {
		return libraryapp.Image{}, false, err
	}
	// row、created、err 保存数据库写入结果。
	row, created, err := r.store.LibraryImages.Insert(ctx, db.LibraryImage{UserID: image.UserID, Filename: image.Filename,
		StorageKey: image.StorageKey, ContentType: image.ContentType, SizeBytes: image.SizeBytes, SHA256: image.SHA256})
	if err != nil {
		return libraryapp.Image{}, false, err
	}
	return libraryImageModel(row), created, nil
}

// Delete 删除图片元数据。
func (r *LibraryImageRepository) Delete(ctx context.Context, userID, id int64) error {
	// err 保存 validate 调用的失败原因。
	if err := r.validate(); err != nil {
		return err
	}
	return mapLibraryImageError(r.store.LibraryImages.Delete(ctx, userID, id))
}

// TemplateImageRefs 返回用户全部模板引用的图片，用于删除图片前的引用检查。
func (r *LibraryImageRepository) TemplateImageRefs(ctx context.Context, userID int64) ([]libraryapp.TemplateImageRef, error) {
	// err 保存 validate 调用的失败原因。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// rows、err 保存用户全部模板。
	rows, err := r.store.ProductTemplates.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	// refs 保存转换后的引用。
	refs := make([]libraryapp.TemplateImageRef, 0, len(rows))
	// row 是当前数据库模板。
	for _, row := range rows {
		// template、convertErr 保存模板转换结果；损坏的 JSON 不能被忽略，否则可能误删仍被引用的图片。
		template, convertErr := libraryTemplateModel(row)
		if convertErr != nil {
			return nil, convertErr
		}
		refs = append(refs, libraryapp.TemplateImageRef{TemplateName: "商品模板「" + template.Name + "」", ImageIDs: template.ReferencedImageIDs()})
	}
	// usages、usageErr 保存回复、卡密、快捷回复和发货模板对图片的引用；读取失败不能当作无引用。
	usages, usageErr := r.store.LibraryImages.Usages(ctx, userID)
	if usageErr != nil {
		return nil, usageErr
	}
	// usage 是当前一处引用，转换为与商品模板相同的引用结构。
	for _, usage := range usages {
		refs = append(refs, libraryapp.TemplateImageRef{TemplateName: usage.Label, ImageIDs: []int64{usage.ImageID}})
	}
	return refs, nil
}

// LibraryTemplateRepository 把商品模板数据库仓储适配为应用层端口，并负责 JSON 列的编解码。
type LibraryTemplateRepository struct {
	// store 提供模板表访问。
	store *db.Store
}

// NewLibraryTemplateRepository 创建模板适配器。
func NewLibraryTemplateRepository(store *db.Store) *LibraryTemplateRepository {
	return &LibraryTemplateRepository{store: store}
}

// validate 确认依赖已装配。
func (r *LibraryTemplateRepository) validate() error {
	if r == nil || r.store == nil || r.store.ProductTemplates == nil {
		return errors.New("商品模板数据库适配器未初始化")
	}
	return nil
}

// mapLibraryTemplateError 把数据库不存在错误转换为应用层错误。
func mapLibraryTemplateError(err error) error {
	if errors.Is(err, db.ErrNotFound) {
		return libraryapp.ErrTemplateNotFound
	}
	return err
}

// libraryTemplateModel 把数据库模板转换为应用模型，解码全部 JSON 列。
func libraryTemplateModel(row db.ProductTemplate) (libraryapp.Template, error) {
	// template 保存标量字段。
	template := libraryapp.Template{ID: row.ID, UserID: row.UserID, Name: row.Name, Title: row.Title, Description: row.Description,
		PriceCents: row.PriceCents, OriginalPriceCents: row.OriginalPriceCents, Quantity: row.Quantity, PostageMode: row.PostageMode,
		PostageCents: row.PostageCents, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	// err 保存 Unmarshal 调用的失败原因。
	if err := json.Unmarshal([]byte(row.CategoryJSON), &template.Category); err != nil {
		return libraryapp.Template{}, fmt.Errorf("商品模板 %d 类目数据损坏", row.ID)
	}
	// err 保存 Unmarshal 调用的失败原因。
	if err := json.Unmarshal([]byte(row.ImageIDsJSON), &template.ImageIDs); err != nil {
		return libraryapp.Template{}, fmt.Errorf("商品模板 %d 图片数据损坏", row.ID)
	}
	// err 保存 Unmarshal 调用的失败原因。
	if err := json.Unmarshal([]byte(row.AutomationJSON), &template.Automation); err != nil {
		return libraryapp.Template{}, fmt.Errorf("商品模板 %d 自动化数据损坏", row.ID)
	}
	// specs 保存多规格 JSON；空对象表示单规格。
	var specs libraryapp.TemplateSpecs
	// err 保存 Unmarshal 调用的失败原因。
	if err := json.Unmarshal([]byte(row.SpecsJSON), &specs); err != nil {
		return libraryapp.Template{}, fmt.Errorf("商品模板 %d 规格数据损坏", row.ID)
	}
	if len(specs.Specs) > 0 {
		template.Specs = &specs
	}
	return template, nil
}

// libraryTemplateRow 把应用模板转换为数据库行，编码全部 JSON 列。
func libraryTemplateRow(template libraryapp.Template) (db.ProductTemplate, error) {
	// category、images、automation、specs 分别是各 JSON 列的编码结果。
	category, err := json.Marshal(template.Category)
	if err != nil {
		return db.ProductTemplate{}, err
	}
	// imageIDs 保证 nil 切片也编码为 []。
	imageIDs := template.ImageIDs
	if imageIDs == nil {
		imageIDs = []int64{}
	}
	// images 保存 Marshal 的返回结果，err 保存其失败原因。
	images, err := json.Marshal(imageIDs)
	if err != nil {
		return db.ProductTemplate{}, err
	}
	// automation 保存 Marshal 的返回结果，err 保存其失败原因。
	automation, err := json.Marshal(template.Automation)
	if err != nil {
		return db.ProductTemplate{}, err
	}
	// specs 默认为空对象，表示单规格。
	specs := []byte("{}")
	if template.Specs != nil {
		if specs, err = json.Marshal(template.Specs); err != nil {
			return db.ProductTemplate{}, err
		}
	}
	return db.ProductTemplate{ID: template.ID, UserID: template.UserID, Name: template.Name, Title: template.Title, Description: template.Description,
		PriceCents: template.PriceCents, OriginalPriceCents: template.OriginalPriceCents, Quantity: template.Quantity, PostageMode: template.PostageMode,
		PostageCents: template.PostageCents, CategoryJSON: string(category), ImageIDsJSON: string(images), SpecsJSON: string(specs), AutomationJSON: string(automation)}, nil
}

// List 返回用户全部模板。
func (r *LibraryTemplateRepository) List(ctx context.Context, userID int64) ([]libraryapp.Template, error) {
	// err 保存 validate 调用的失败原因。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// rows、err 保存数据库模板列表。
	rows, err := r.store.ProductTemplates.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	// templates 保存转换后的应用模型。
	templates := make([]libraryapp.Template, 0, len(rows))
	// row 是当前数据库模板。
	for _, row := range rows {
		// template、convertErr 保存转换结果。
		template, convertErr := libraryTemplateModel(row)
		if convertErr != nil {
			return nil, convertErr
		}
		templates = append(templates, template)
	}
	return templates, nil
}

// Get 读取单个模板。
func (r *LibraryTemplateRepository) Get(ctx context.Context, userID, id int64) (libraryapp.Template, error) {
	// err 保存 validate 调用的失败原因。
	if err := r.validate(); err != nil {
		return libraryapp.Template{}, err
	}
	// row、err 保存数据库查询结果。
	row, err := r.store.ProductTemplates.Get(ctx, userID, id)
	if err != nil {
		return libraryapp.Template{}, mapLibraryTemplateError(err)
	}
	return libraryTemplateModel(*row)
}

// Create 写入模板。
func (r *LibraryTemplateRepository) Create(ctx context.Context, template libraryapp.Template) (int64, error) {
	// err 保存 validate 调用的失败原因。
	if err := r.validate(); err != nil {
		return 0, err
	}
	// row、err 保存编码后的数据库行。
	row, err := libraryTemplateRow(template)
	if err != nil {
		return 0, err
	}
	return r.store.ProductTemplates.Create(ctx, row)
}

// Update 覆盖模板。
func (r *LibraryTemplateRepository) Update(ctx context.Context, template libraryapp.Template) error {
	// err 保存 validate 调用的失败原因。
	if err := r.validate(); err != nil {
		return err
	}
	// row、err 保存编码后的数据库行。
	row, err := libraryTemplateRow(template)
	if err != nil {
		return err
	}
	return mapLibraryTemplateError(r.store.ProductTemplates.Update(ctx, row))
}

// Delete 删除模板。
func (r *LibraryTemplateRepository) Delete(ctx context.Context, userID, id int64) error {
	// err 保存 validate 调用的失败原因。
	if err := r.validate(); err != nil {
		return err
	}
	return mapLibraryTemplateError(r.store.ProductTemplates.Delete(ctx, userID, id))
}

// 编译期确认适配器满足应用层端口。
var (
	_ libraryapp.ImageRepository    = (*LibraryImageRepository)(nil)
	_ libraryapp.TemplateRepository = (*LibraryTemplateRepository)(nil)
)
