package orders

import (
	"context"
	"errors"
)

// ErrForbidden 表示当前用户无权访问订单筛选范围。
var ErrForbidden = errors.New("无权访问订单")

// ListQuery 是订单列表用例的分页和筛选条件。
type ListQuery struct {
	// UserID 是当前用户标识。
	UserID int64
	// CookieID 是可选的账号筛选条件。
	CookieID string
	// Status 是可选的订单状态筛选条件。
	Status string
	// Search 是订单号、商品或买家搜索词。
	Search string
	// Page 是从 1 开始的页码。
	Page int
	// PageSize 是单页记录数上限。
	PageSize int
}

// ListResult 是订单列表用例的分页结果。
type ListResult struct {
	// Rows 是当前页订单展示行。
	Rows []OrderRow
	// Total 是符合筛选条件的订单总数。
	Total int
	// Page 是规范化后的页码。
	Page int
	// PageSize 是规范化后的单页记录数。
	PageSize int
	// TotalPages 是根据总数计算出的总页数。
	TotalPages int
}

// ListRepository 定义订单列表用例所需的最小数据访问能力。
type ListRepository interface {
	// ExistsOwned 判断账号是否归属于指定用户。
	ExistsOwned(ctx context.Context, userID int64, cookieID string) (bool, error)
	// ListOrdersForUser 查询用户范围内的订单展示行。
	ListOrdersForUser(ctx context.Context, filter ListFilter) ([]OrderRow, int, error)
}

// ListService 承载订单列表分页和所有权规则，不依赖 HTTP 或数据库实现。
type ListService struct {
	// repository 保存订单列表用例所需的窄数据访问 Port。
	repository ListRepository
}

// NewListService 创建订单列表应用服务。
func NewListService(repository ListRepository) *ListService {
	return &ListService{repository: repository}
}

// List 查询当前用户可见的订单，并集中处理分页和账号所有权规则。
func (s *ListService) List(ctx context.Context, query ListQuery) (ListResult, error) {
	if s == nil || s.repository == nil {
		return ListResult{}, errors.New("订单列表 repository 未初始化")
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 20
	}
	if query.PageSize > 200 {
		query.PageSize = 200
	}
	if query.CookieID != "" {
		// owned、err 保存账号归属检查结果及错误。
		owned, err := s.repository.ExistsOwned(ctx, query.UserID, query.CookieID)
		if err != nil {
			return ListResult{}, err
		}
		if !owned {
			return ListResult{}, ErrForbidden
		}
	}
	// offset 是本次列表查询对应的数据库偏移量。
	offset := (query.Page - 1) * query.PageSize
	// rows、total、err 保存订单列表查询结果及错误。
	rows, total, err := s.repository.ListOrdersForUser(ctx, ListFilter{
		UserID: query.UserID, CookieID: query.CookieID, Status: query.Status,
		Search: query.Search, Limit: query.PageSize, Offset: offset,
	})
	if err != nil {
		return ListResult{}, err
	}
	return ListResult{
		Rows: rows, Total: total, Page: query.Page, PageSize: query.PageSize,
		TotalPages: (total + query.PageSize - 1) / query.PageSize,
	}, nil
}
