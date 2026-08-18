package server

import (
	"context"

	itemapp "xianyu-go/internal/application/items"
)

// newBatchLocalPublishService 返回构造阶段已完成依赖校验的批量本地收口应用服务。
func (s *Server) newBatchLocalPublishService() (ItemBatchLocalPublishPort, error) {
	if s == nil || s.applicationServiceSet() == nil || s.applicationServiceSet().itemBatchLocalPublish == nil {
		return nil, itemapp.ErrBatchLocalPublishUnavailable
	}
	return s.applicationServiceSet().itemBatchLocalPublish, nil
}

// createPublishAutomationRules 保留旧测试入口，但实际规则编排已由应用服务负责。
func (s *Server) createPublishAutomationRules(ctx context.Context, userID int64, row itemapp.BatchRow, result *itemapp.BatchPublishResult) error {
	// service 保存已装配的批量本地收口应用服务。
	service, err := s.newBatchLocalPublishService()
	if err != nil {
		return err
	}
	// applicationRow 只转换规则编排所需的非敏感字段。
	return service.EnsureAutomationRules(ctx, userID, row, result)
}
