package server

import (
	"errors"
	"fmt"

	itemapp "xianyu-go/internal/application/items"
	libraryapp "xianyu-go/internal/application/library"
)

// libraryImageResponse 是资源库图片的元数据响应 DTO；图片字节通过 content 端点读取，响应不暴露存储路径。
type libraryImageResponse struct {
	// ID 是图片主键。
	ID int64 `json:"id"`
	// Filename 是上传时的原始文件名。
	Filename string `json:"filename"`
	// ContentType 是图片 MIME 类型。
	ContentType string `json:"content_type"`
	// SizeBytes 是图片字节数。
	SizeBytes int64 `json:"size_bytes"`
	// CreatedAt 是上传时间文本。
	CreatedAt string `json:"created_at"`
}

// libraryImageUploadItem 是单个上传文件的结果。
type libraryImageUploadItem struct {
	// Filename 是上传文件的原始文件名。
	Filename string `json:"filename"`
	// Duplicate 表示同内容图片已存在，没有重复保存。
	Duplicate bool `json:"duplicate"`
	// Error 是该文件失败时的原因，成功时为空串。
	Error string `json:"error"`
	// Image 是成功或命中去重时的图片；失败时为 null。
	Image *libraryImageResponse `json:"image"`
}

// libraryImageUploadResponse 是图片上传响应 DTO。
type libraryImageUploadResponse struct {
	// Results 按上传顺序给出每个文件的结果。
	Results []libraryImageUploadItem `json:"results"`
}

// libraryTemplateBinding 是发货模板变量与卡密组的绑定 DTO。
type libraryTemplateBinding struct {
	// VariableKey 是模板变量键。
	VariableKey string `json:"variable_key"`
	// CardID 是绑定的卡密组标识。
	CardID int64 `json:"card_id"`
	// DeliveryCount 是每件商品准备的卡密份数。
	DeliveryCount int `json:"delivery_count"`
}

// libraryTemplateAction 是模板自动化中的一个发货动作 DTO，支持发卡与发货模板两种类型。
type libraryTemplateAction struct {
	// ActionType 是动作类型 send_card 或 send_template。
	ActionType string `json:"action_type"`
	// CardID 是发卡动作的卡密组标识。
	CardID int64 `json:"card_id"`
	// DeliveryCount 是发卡动作每件商品发送的份数。
	DeliveryCount int `json:"delivery_count"`
	// DelaySeconds 是动作执行前的延迟秒数。
	DelaySeconds int `json:"delay_seconds"`
	// DeliveryTemplateID 是发货模板动作的模板标识。
	DeliveryTemplateID int64 `json:"delivery_template_id"`
	// TemplateBindings 是发货模板变量绑定。
	TemplateBindings []libraryTemplateBinding `json:"template_bindings"`
	// CustomVariables 是传给发货模板的自定义字符串键值表。
	CustomVariables map[string]string `json:"custom_variables"`
	// SpecName 是多规格商品中动作绑定的规范化规格名称，单规格为空串。
	SpecName string `json:"spec_name"`
	// SpecValue 是与 SpecName 同序的规格值，单规格为空串。
	SpecValue string `json:"spec_value"`
}

// libraryTemplateCardAutomation 是一组发货动作及其开关 DTO。
type libraryTemplateCardAutomation struct {
	// Enabled 表示是否启用该自动化。
	Enabled bool `json:"enabled"`
	// Actions 是按顺序执行的发货动作。
	Actions []libraryTemplateAction `json:"actions"`
}

// libraryTemplateReviewRequest 是超时求评价配置 DTO。
type libraryTemplateReviewRequest struct {
	// Enabled 表示是否启用求评价。
	Enabled bool `json:"enabled"`
	// AfterShippedHours 是发货后等待小时数。
	AfterShippedHours int `json:"after_shipped_hours"`
	// Message 是求评价文案。
	Message string `json:"message"`
	// MaxAttempts 是最多提醒次数。
	MaxAttempts int `json:"max_attempts"`
	// DelaySeconds 是提醒动作的延迟秒数。
	DelaySeconds int `json:"delay_seconds"`
}

// libraryTemplateAutomation 是模板发布后自动化配置 DTO。
type libraryTemplateAutomation struct {
	// PaidDelivery 是付款后自动发货。
	PaidDelivery libraryTemplateCardAutomation `json:"paid_delivery"`
	// ReviewGift 是评价后赠品。
	ReviewGift libraryTemplateCardAutomation `json:"review_gift"`
	// ReviewRequest 是超时求评价。
	ReviewRequest libraryTemplateReviewRequest `json:"review_request"`
}

// libraryTemplateSpecValue 是规格值 DTO。
type libraryTemplateSpecValue struct {
	// Value 是展示给买家的规格值。
	Value string `json:"value"`
	// ImageIndex 是规格图片在 spec_image_ids 中的下标，-1 表示没有图片。
	ImageIndex int `json:"image_index"`
}

// libraryTemplateSpec 是规格维度 DTO。
type libraryTemplateSpec struct {
	// PropertyName 是规格名称。
	PropertyName string `json:"property_name"`
	// SupportImage 表示规格值是否允许绑定图片。
	SupportImage bool `json:"support_image"`
	// Values 是该维度下的规格值。
	Values []libraryTemplateSpecValue `json:"values"`
}

// libraryTemplateSKUProperty 是 SKU 中的规格名称与值对 DTO。
type libraryTemplateSKUProperty struct {
	// PropertyText 是规格名称。
	PropertyText string `json:"property_text"`
	// ValueText 是规格值。
	ValueText string `json:"value_text"`
}

// libraryTemplateSKU 是一个 SKU 组合 DTO，价格使用元为单位的小数字符串。
type libraryTemplateSKU struct {
	// Price 是 SKU 售价，单位为元。
	Price string `json:"price"`
	// Quantity 是 SKU 库存。
	Quantity int `json:"quantity"`
	// PropertyList 是该 SKU 的规格组合。
	PropertyList []libraryTemplateSKUProperty `json:"property_list"`
}

// libraryTemplateSpecs 是模板多规格配置 DTO；specs 为空数组表示单规格。
type libraryTemplateSpecs struct {
	// Specs 是规格维度。
	Specs []libraryTemplateSpec `json:"specs"`
	// SKUs 是规格组合。
	SKUs []libraryTemplateSKU `json:"skus"`
	// SpecImageIDs 是规格图片的图片库主键。
	SpecImageIDs []int64 `json:"spec_image_ids"`
}

// libraryTemplateRequest 是创建或更新商品模板的请求 DTO，金额均为元为单位的小数字符串。
type libraryTemplateRequest struct {
	// Name 是模板备注名。
	Name string `json:"name"`
	// Title 是发布时的商品标题。
	Title string `json:"title"`
	// Description 是发布时的商品描述。
	Description string `json:"description"`
	// Price 是单规格售价；多规格时被忽略并由 SKU 推导。
	Price string `json:"price"`
	// OriginalPrice 是原价，空串表示不设置。
	OriginalPrice string `json:"original_price"`
	// Quantity 是单规格库存；多规格时被忽略并由 SKU 推导。
	Quantity int `json:"quantity"`
	// PostageMode 是邮费模式：free、distance、fixed 或 none。
	PostageMode string `json:"postage_mode"`
	// Postage 是固定邮费。
	Postage string `json:"postage"`
	// Category 是类目，cat_id 为空表示发布时沿用兜底或自动推荐。
	Category publishCategoryResponse `json:"category"`
	// ImageIDs 是商品主图的图片库主键，顺序即发布顺序。
	ImageIDs []int64 `json:"image_ids"`
	// Specs 是多规格配置。
	Specs libraryTemplateSpecs `json:"specs"`
	// Automation 是发布后自动化配置。
	Automation libraryTemplateAutomation `json:"automation"`
}

// libraryTemplateResponse 是商品模板响应 DTO。
type libraryTemplateResponse struct {
	// ID 是模板主键。
	ID int64 `json:"id"`
	// Name 是模板备注名。
	Name string `json:"name"`
	// Title 是发布时的商品标题。
	Title string `json:"title"`
	// Description 是发布时的商品描述。
	Description string `json:"description"`
	// Price 是售价（多规格时为最低 SKU 价），单位为元。
	Price string `json:"price"`
	// OriginalPrice 是原价，空串表示未设置。
	OriginalPrice string `json:"original_price"`
	// Quantity 是库存（多规格时为各 SKU 之和）。
	Quantity int `json:"quantity"`
	// PostageMode 是邮费模式。
	PostageMode string `json:"postage_mode"`
	// Postage 是固定邮费。
	Postage string `json:"postage"`
	// Category 是类目。
	Category publishCategoryResponse `json:"category"`
	// ImageIDs 是商品主图主键。
	ImageIDs []int64 `json:"image_ids"`
	// Specs 是多规格配置。
	Specs libraryTemplateSpecs `json:"specs"`
	// Automation 是发布后自动化配置。
	Automation libraryTemplateAutomation `json:"automation"`
	// CreatedAt 是创建时间文本。
	CreatedAt string `json:"created_at"`
	// UpdatedAt 是最近更新时间文本。
	UpdatedAt string `json:"updated_at"`
}

// libraryTemplatePublishRequest 是「模板 × 账号」发布预检的请求 DTO。
type libraryTemplatePublishRequest struct {
	// TemplateIDs 是选中的模板。
	TemplateIDs []int64 `json:"template_ids"`
	// CookieIDs 是目标账号。
	CookieIDs []string `json:"cookie_ids"`
	// Location 是批次统一发货地。
	Location itemPublishLocationRequest `json:"location"`
	// PublishIntervalSeconds 是相邻两次发布请求的最小间隔，0 表示使用默认值。
	PublishIntervalSeconds int `json:"publish_interval_seconds"`
}

// newLibraryImageResponse 把应用层图片转换为响应 DTO。
func newLibraryImageResponse(image libraryapp.Image) libraryImageResponse {
	return libraryImageResponse{ID: image.ID, Filename: image.Filename, ContentType: image.ContentType, SizeBytes: image.SizeBytes, CreatedAt: image.CreatedAt}
}

// newLibraryUploadResponse 把应用层上传结果转换为响应 DTO。
func newLibraryUploadResponse(results []libraryapp.UploadResult) libraryImageUploadResponse {
	// items 保存逐文件结果。
	items := make([]libraryImageUploadItem, 0, len(results))
	// result 是当前文件的上传结果。
	for _, result := range results {
		// item 保存当前文件的响应项，失败时 Image 保持 nil。
		item := libraryImageUploadItem{Filename: result.Filename, Duplicate: result.Duplicate, Error: result.Error}
		if result.Error == "" {
			// image 是上传成功或命中去重的图片。
			image := newLibraryImageResponse(result.Image)
			item.Image = &image
		}
		items = append(items, item)
	}
	return libraryImageUploadResponse{Results: items}
}

// centsDecimal 把分转换为两位小数的元文本。
func centsDecimal(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

// toTemplate 把请求 DTO 转换为应用层模板，金额解析失败返回面向用户的错误。
func (req libraryTemplateRequest) toTemplate(userID, id int64) (libraryapp.Template, error) {
	// priceCents、originalCents、postageCents 是解析后的金额，空串按 0 处理。
	priceCents, priceErr := parseMoneyCents(req.Price)
	// originalCents 保存 parseMoneyCents 的返回结果，originalErr 保存其失败原因。
	originalCents, originalErr := parseMoneyCents(req.OriginalPrice)
	// postageCents 保存 parseMoneyCents 的返回结果，postageErr 保存其失败原因。
	postageCents, postageErr := parseMoneyCents(req.Postage)
	if priceErr != nil || originalErr != nil || postageErr != nil {
		return libraryapp.Template{}, errors.New("金额格式错误")
	}
	// specs、specsErr 保存多规格配置转换结果。
	specs, specsErr := req.Specs.toApplication()
	if specsErr != nil {
		return libraryapp.Template{}, specsErr
	}
	return libraryapp.Template{
		ID: id, UserID: userID, Name: req.Name, Title: req.Title, Description: req.Description,
		PriceCents: priceCents, OriginalPriceCents: originalCents, Quantity: req.Quantity,
		PostageMode: req.PostageMode, PostageCents: postageCents,
		Category: itemapp.BatchPreviewCategory{CatID: req.Category.CatID, CatName: req.Category.CatName, ChannelCatID: req.Category.ChannelCatID, TBCatID: req.Category.TBCatID},
		ImageIDs: req.ImageIDs, Specs: specs, Automation: req.Automation.toApplication(),
	}, nil
}

// toApplication 把规格 DTO 转换为应用层规格，没有规格维度时返回 nil 表示单规格。
func (specs libraryTemplateSpecs) toApplication() (*libraryapp.TemplateSpecs, error) {
	if len(specs.Specs) == 0 && len(specs.SKUs) == 0 {
		return nil, nil
	}
	// result 保存转换后的规格配置。
	result := &libraryapp.TemplateSpecs{SpecImageIDs: specs.SpecImageIDs}
	// spec 是当前规格维度 DTO。
	for _, spec := range specs.Specs {
		// converted 保存当前维度及其规格值。
		converted := itemapp.PublishSpec{PropertyName: spec.PropertyName, SupportImage: spec.SupportImage}
		// value 是当前规格值 DTO。
		for _, value := range spec.Values {
			converted.Values = append(converted.Values, itemapp.PublishSpecValue{Value: value.Value, ImageIndex: value.ImageIndex})
		}
		result.Specs = append(result.Specs, converted)
	}
	// sku 是当前 SKU DTO。
	for _, sku := range specs.SKUs {
		// priceCents、err 是 SKU 价格解析结果。
		priceCents, err := parseMoneyCents(sku.Price)
		if err != nil {
			return nil, errors.New("SKU 价格格式错误")
		}
		// converted 保存当前 SKU 及其规格组合。
		converted := itemapp.PublishSKU{PriceCents: priceCents, Quantity: sku.Quantity}
		// property 是当前规格名称与值对。
		for _, property := range sku.PropertyList {
			converted.PropertyList = append(converted.PropertyList, itemapp.PublishSKUProperty{PropertyText: property.PropertyText, ValueText: property.ValueText})
		}
		result.SKUs = append(result.SKUs, converted)
	}
	return result, nil
}

// newLibraryTemplateSpecs 把应用层规格转换为响应 DTO，单规格返回全部为空数组的对象。
func newLibraryTemplateSpecs(specs *libraryapp.TemplateSpecs) libraryTemplateSpecs {
	// result 保存响应规格，切片初始化为空数组而不是 null。
	result := libraryTemplateSpecs{Specs: []libraryTemplateSpec{}, SKUs: []libraryTemplateSKU{}, SpecImageIDs: []int64{}}
	if specs == nil {
		return result
	}
	if specs.SpecImageIDs != nil {
		result.SpecImageIDs = specs.SpecImageIDs
	}
	// spec 是当前应用层规格维度。
	for _, spec := range specs.Specs {
		// values 保存当前维度的规格值 DTO。
		values := make([]libraryTemplateSpecValue, 0, len(spec.Values))
		// value 是当前应用层规格值。
		for _, value := range spec.Values {
			values = append(values, libraryTemplateSpecValue{Value: value.Value, ImageIndex: value.ImageIndex})
		}
		result.Specs = append(result.Specs, libraryTemplateSpec{PropertyName: spec.PropertyName, SupportImage: spec.SupportImage, Values: values})
	}
	// sku 是当前应用层 SKU。
	for _, sku := range specs.SKUs {
		// properties 保存当前 SKU 的规格对 DTO。
		properties := make([]libraryTemplateSKUProperty, 0, len(sku.PropertyList))
		// property 是当前应用层规格对。
		for _, property := range sku.PropertyList {
			properties = append(properties, libraryTemplateSKUProperty{PropertyText: property.PropertyText, ValueText: property.ValueText})
		}
		result.SKUs = append(result.SKUs, libraryTemplateSKU{Price: centsDecimal(sku.PriceCents), Quantity: sku.Quantity, PropertyList: properties})
	}
	return result
}

// newLibraryTemplateResponse 把应用层模板转换为响应 DTO，所有集合保证非 null。
func newLibraryTemplateResponse(template libraryapp.Template) libraryTemplateResponse {
	// imageIDs 保证主图集合非 null。
	imageIDs := template.ImageIDs
	if imageIDs == nil {
		imageIDs = []int64{}
	}
	// original 保存原价文本，未设置时为空串。
	original := ""
	if template.OriginalPriceCents > 0 {
		original = centsDecimal(template.OriginalPriceCents)
	}
	return libraryTemplateResponse{
		ID: template.ID, Name: template.Name, Title: template.Title, Description: template.Description,
		Price: centsDecimal(template.PriceCents), OriginalPrice: original, Quantity: template.Quantity,
		PostageMode: template.PostageMode, Postage: centsDecimal(template.PostageCents),
		Category: publishCategoryResponse{CatID: template.Category.CatID, CatName: template.Category.CatName, ChannelCatID: template.Category.ChannelCatID, TBCatID: template.Category.TBCatID},
		ImageIDs: imageIDs, Specs: newLibraryTemplateSpecs(template.Specs), Automation: newLibraryTemplateAutomation(template.Automation),
		CreatedAt: template.CreatedAt, UpdatedAt: template.UpdatedAt,
	}
}
