package orders

import (
	"context"
	"errors"
	"strings"
)

// ErrNotFound 表示订单不存在或无法读取。
var ErrNotFound = errors.New("订单不存在")

// DetailResult 是订单详情用例返回的订单和关联商品信息。
type DetailResult struct {
	// Order 是经过当前用户所有权校验的订单实体。
	Order *Order
	// Item 是关联商品信息，读取失败时允许为空。
	Item *ItemInfo
}

// DetailRepository 定义订单详情用例所需的最小数据访问能力。
type DetailRepository interface {
	// ExistsOwned 判断账号是否归属于指定用户。
	ExistsOwned(ctx context.Context, userID int64, cookieID string) (bool, error)
	// GetOrder 读取订单实体。
	GetOrder(ctx context.Context, orderID string) (*Order, error)
	// GetItem 读取订单关联商品信息。
	GetItem(ctx context.Context, cookieID, itemID string) (*ItemInfo, error)
}

// DetailService 承载订单详情读取和所有权规则，不依赖 HTTP 或数据库实现。
type DetailService struct {
	// repository 保存订单详情用例所需的窄数据访问 Port。
	repository DetailRepository
}

// NewDetailService 创建订单详情应用服务。
func NewDetailService(repository DetailRepository) *DetailService {
	return &DetailService{repository: repository}
}

// Get 读取订单并校验订单绑定账号属于当前用户。
func (s *DetailService) Get(ctx context.Context, userID int64, orderID string) (*Order, error) {
	if s == nil || s.repository == nil {
		return nil, errors.New("订单详情 repository 未初始化")
	}
	// order、err 保存订单读取结果及错误。
	order, err := s.repository.GetOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, ErrNotFound
	}
	if strings.TrimSpace(order.CookieID) == "" {
		return nil, ErrForbidden
	}
	// owned、ownershipErr 保存订单账号归属结果及错误。
	owned, ownershipErr := s.repository.ExistsOwned(ctx, userID, order.CookieID)
	if ownershipErr != nil {
		return nil, ownershipErr
	}
	if !owned {
		return nil, ErrForbidden
	}
	return order, nil
}

// GetView 读取订单并补全关联商品信息，商品读取失败时保留订单主体。
func (s *DetailService) GetView(ctx context.Context, userID int64, orderID string) (DetailResult, error) {
	// order、err 保存经过所有权校验的订单及错误。
	order, err := s.Get(ctx, userID, orderID)
	if err != nil {
		return DetailResult{}, err
	}
	// item、itemErr 保存关联商品及读取错误。
	item, itemErr := s.repository.GetItem(ctx, order.CookieID, order.ItemID)
	if itemErr != nil {
		item = nil
	}
	return DetailResult{Order: order, Item: item}, nil
}
