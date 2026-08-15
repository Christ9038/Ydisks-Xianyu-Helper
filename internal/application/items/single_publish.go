// Package items 提供商品发布相关的应用用例与消费者定义的端口。
package items

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// Image 是商品发布所需的图片内容；图片字节只在当前用例生命周期内由端口消费。
type Image struct {
	// Filename 是上传到平台时使用的原始文件名。
	Filename string
	// ContentType 是经过请求校验的 MIME 类型。
	ContentType string
	// Data 是图片二进制内容，禁止写入日志或响应。
	Data []byte
}

// Location 是平台发布所需的发货地信息，不依赖外部平台包的 DTO。
type Location struct {
	// Area 是发货地的区县名称。
	Area string
	// City 是发货地的城市名称。
	City string
	// DivisionID 是平台行政区划标识。
	DivisionID string
	// Longitude 是发货地经度。
	Longitude float64
	// Latitude 是发货地纬度。
	Latitude float64
	// POIID 是地图服务返回的兴趣点标识。
	POIID string
	// POIName 是地图服务返回的兴趣点名称。
	POIName string
	// Province 是发货地的省份名称。
	Province string
}

// PublishInput 是单商品发布用例输入，携带已由 HTTP 层验证的业务字段。
type PublishInput struct {
	// UserID 是发起发布操作的用户标识。
	UserID int64
	// CookieID 是执行发布的账号标识。
	CookieID string
	// Title 是商品标题。
	Title string
	// Description 是商品描述。
	Description string
	// PriceCents 是商品售价，单位为分。
	PriceCents int64
	// OriginalPriceCents 是商品原价，单位为分。
	OriginalPriceCents int64
	// Quantity 是商品库存数量。
	Quantity int
	// PostageMode 是邮费模式，例如 free 或 fixed。
	PostageMode string
	// PostageCents 是邮费金额，单位为分。
	PostageCents int64
	// Location 是可选的发货地。
	Location *Location
	// Images 是待上传的商品图片。
	Images []Image
}

// PublishResult 是平台返回的商品结果，经过端口转换后不暴露平台 DTO。
type PublishResult struct {
	// ItemID 是平台商品标识。
	ItemID string
	// ItemURL 是平台商品详情地址。
	ItemURL string
	// Title 是平台最终确认的商品标题。
	Title string
	// PriceText 是平台格式化后的商品价格。
	PriceText string
	// CategoryID 是平台商品类目标识。
	CategoryID string
	// CategoryName 是平台商品类目名称。
	CategoryName string
	// ImageURL 是平台主图地址。
	ImageURL string
	// Quantity 是平台确认的库存数量。
	Quantity int
	// RawData 是平台原始结果的结构化副本，仅供本地详情持久化。
	RawData map[string]any
}

// PublishOutcome 是发布端口结果及响应 Cookie 持久化风险。
type PublishOutcome struct {
	// Result 是平台返回的商品结果；平台未返回结果时为空。
	Result *PublishResult
	// ResponseCookieErr 是平台响应 Cookie 未能持久化时的错误。
	ResponseCookieErr error
	// LocalSaveErr 是本地商品落库失败时的错误，由应用服务补充。
	LocalSaveErr error
}

// PublishPort 是商品发布基础设施端口；实现方负责平台会话和凭证更新。
type PublishPort interface {
	// Publish 调用外部平台并返回已脱离平台 DTO 的结果。
	Publish(context.Context, PublishInput) (PublishOutcome, error)
}

// ItemRecord 是商品发布成功后写入本地商品表的最小模型。
type ItemRecord struct {
	// CookieID 是发布所用账号标识。
	CookieID string
	// ItemID 是平台商品标识。
	ItemID string
	// ItemTitle 是商品标题。
	ItemTitle string
	// ItemDescription 是商品描述。
	ItemDescription string
	// ItemCategory 是平台类目标识。
	ItemCategory string
	// ItemPrice 是平台格式化后的价格文本。
	ItemPrice string
	// ItemDetail 是供后续详情页使用的 JSON 字符串。
	ItemDetail string
	// MultiQuantityDelivery 表示该商品是否启用多库存交付。
	MultiQuantityDelivery bool
}

// ItemRepository 是商品本地持久化端口。
type ItemRepository interface {
	// Upsert 保存或更新已在平台发布成功的商品。
	Upsert(context.Context, ItemRecord) error
}

// Service 是单商品发布应用服务，负责平台结果与本地商品状态的编排。
type Service struct {
	// publisher 执行平台发布及响应凭证持久化。
	publisher PublishPort
	// repository 保存平台成功后的本地商品记录。
	repository ItemRepository
}

// NewService 创建单商品发布应用服务，并校验必需端口。
func NewService(publisher PublishPort, repository ItemRepository) (*Service, error) {
	if publisher == nil {
		return nil, errors.New("商品发布端口不能为空")
	}
	if repository == nil {
		return nil, errors.New("商品仓储端口不能为空")
	}
	return &Service{publisher: publisher, repository: repository}, nil
}

// PublishSingle 执行发布用例；平台成功但本地保存失败时保留远端结果供 HTTP 层提示补偿。
func (svc *Service) PublishSingle(ctx context.Context, input PublishInput) (PublishOutcome, error) {
	// outcome 保存平台端口返回的远端结果；err 保存平台调用或凭证持久化错误。
	outcome, err := svc.publisher.Publish(ctx, input)
	if err != nil || outcome.Result == nil || strings.TrimSpace(outcome.Result.ItemID) == "" {
		return outcome, err
	}
	// detail 是供商品详情页使用的本地扩展信息；其中 RawData 仅进入受控数据库字段。
	detail := map[string]any{
		"item_image":    outcome.Result.ImageURL,
		"web_url":       outcome.Result.ItemURL,
		"category_name": outcome.Result.CategoryName,
		"quantity":      outcome.Result.Quantity,
		"publish_raw":   outcome.Result.RawData,
	}
	// detailJSON 是商品扩展信息的 JSON 表示；序列化失败不应阻断远端结果回传。
	detailJSON, _ := json.Marshal(detail)
	// localErr 是平台成功后本地商品落库错误，交由调用方呈现补偿提示。
	localErr := svc.repository.Upsert(ctx, ItemRecord{
		CookieID: input.CookieID, ItemID: outcome.Result.ItemID, ItemTitle: outcome.Result.Title,
		ItemDescription: input.Description, ItemCategory: outcome.Result.CategoryID,
		ItemPrice: outcome.Result.PriceText, ItemDetail: string(detailJSON),
		MultiQuantityDelivery: input.Quantity > 1,
	})
	outcome.LocalSaveErr = localErr
	return outcome, nil
}
