package orders

import (
	"context"
	"errors"
	"strings"
)

// DeleteRepository 定义订单删除用例所需的最小持久化能力。
type DeleteRepository interface {
	// ExistsOwned 判断订单绑定的账号是否归属于当前用户。
	ExistsOwned(ctx context.Context, userID int64, cookieID string) (bool, error)
	// GetOrder 读取待删除订单。
	GetOrder(ctx context.Context, orderID string) (*Order, error)
	// SoftDeleteOrder 逻辑删除订单并保留审计历史。
	SoftDeleteOrder(ctx context.Context, orderID string) (bool, error)
}

// DeleteService 承载订单删除的存在性、所有权和逻辑删除规则。
type DeleteService struct {
	// repository 保存订单删除用例所需的窄数据访问 Port。
	repository DeleteRepository
}

// NewDeleteService 创建订单删除应用服务。
func NewDeleteService(repository DeleteRepository) *DeleteService {
	return &DeleteService{repository: repository}
}

// Delete 校验订单归属后执行逻辑删除，不物理清理历史数据。
func (s *DeleteService) Delete(ctx context.Context, userID int64, orderID string) error {
	if s == nil || s.repository == nil {
		return errors.New("订单删除 repository 未初始化")
	}
	// order 保存待删除订单主体。
	order, err := s.repository.GetOrder(ctx, orderID)
	if err != nil {
		return err
	}
	if order == nil {
		return ErrNotFound
	}
	if strings.TrimSpace(order.CookieID) == "" {
		return ErrForbidden
	}
	// owned 保存订单账号归属校验结果。
	owned, err := s.repository.ExistsOwned(ctx, userID, order.CookieID)
	if err != nil {
		return err
	}
	if !owned {
		return ErrForbidden
	}
	// deleted 保存逻辑删除是否实际影响订单的结果。
	_, err = s.repository.SoftDeleteOrder(ctx, orderID)
	return err
}
