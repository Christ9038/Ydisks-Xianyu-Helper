// Package reconciliation 负责恢复外部动作成功但本地状态未完成的订单。
package reconciliation

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

// Service 是订单本地状态补偿服务。
type Service struct {
	// store 提供补偿记录和订单状态持久化能力。
	store *db.Store
	// logger 记录补偿扫描和单条任务失败原因。
	logger *slog.Logger
	// interval 是后台扫描 pending 记录的时间间隔。
	interval time.Duration
}

// New 构造订单补偿服务。
func New(store *db.Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, logger: logger, interval: 30 * time.Second}
}

// Run 按固定间隔扫描并重试 pending 补偿记录，直到 ctx 取消。
func (s *Service) Run(ctx context.Context) {
	if s == nil || s.store == nil || s.store.Reconciliations == nil {
		return
	}
	// scanCtx 是每轮扫描继承的生命周期上下文。
	scanCtx := ctx
	if scanCtx == nil {
		scanCtx = context.Background()
	}
	// runOnce 保存启动时立即执行的一轮补偿结果错误。
	if err := s.RunOnce(scanCtx); err != nil {
		s.logger.Warn("订单补偿扫描失败", "err", err)
	}
	// ticker 负责按固定周期唤醒下一轮补偿扫描。
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-scanCtx.Done():
			return
		case <-ticker.C:
			// err 表示定时扫描本轮返回的数据库级错误。
			if err := s.RunOnce(scanCtx); err != nil {
				s.logger.Warn("订单补偿扫描失败", "err", err)
			}
		}
	}
}

// RunOnce 扫描并处理一批 pending 补偿记录，返回扫描级数据库错误。
func (s *Service) RunOnce(ctx context.Context) error {
	if s == nil || s.store == nil || s.store.Reconciliations == nil || s.store.Orders == nil {
		return errors.New("订单补偿服务未初始化")
	}
	// records、err 保存待补偿记录及扫描错误。
	records, err := s.store.Reconciliations.ListPending(ctx, 100)
	if err != nil {
		return err
	}
	// record 表示当前待处理的补偿记录。
	for _, record := range records {
		// recordErr 保存当前补偿记录的处理错误。
		recordErr := s.reconcileRecord(ctx, record)
		if recordErr == nil {
			continue
		}
		// err 表示补偿失败次数写入错误。
		if err := s.store.Reconciliations.RecordAttempt(ctx, record.ID, recordErr.Error()); err != nil {
			s.logger.Warn("记录订单补偿失败次数失败", "reconciliation_id", record.ID, "err", err)
		}
	}
	return nil
}

// reconcileRecord 将已确认发货的订单本地状态补齐为 shipped。
func (s *Service) reconcileRecord(ctx context.Context, record db.OrderReconciliation) error {
	if record.Kind != "manual_status_ship" {
		return errors.New("不支持的订单补偿动作: " + record.Kind)
	}
	// order、err 保存待补偿订单及查询错误。
	order, err := s.store.Orders.Get(ctx, record.OrderID)
	if err != nil {
		return err
	}
	if order == nil || strings.TrimSpace(order.CookieID) == "" {
		return errors.New("补偿订单不存在或缺少账号")
	}
	// systemShipped 表示外部平台已确认发货，本次仅补齐本地事实。
	systemShipped := true
	// err 表示本地订单状态补偿写入错误。
	if err := s.store.Orders.Upsert(ctx, record.OrderID, db.OrderUpsertOpts{
		CookieID: order.CookieID, ItemID: order.ItemID, BuyerID: order.BuyerID,
		ChatID: order.ChatID, OrderStatus: "shipped", SystemShipped: &systemShipped,
	}); err != nil {
		return err
	}
	return s.store.Reconciliations.MarkResolved(ctx, record.ID, "本地订单状态已补偿为 shipped")
}
