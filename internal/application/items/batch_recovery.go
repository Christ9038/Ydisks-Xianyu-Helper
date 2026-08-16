package items

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"time"
)

// BatchRecoveryRepository 定义批量发布恢复扫描器所需的最小状态端口。
type BatchRecoveryRepository interface {
	// RecoverableBatches 查询租约已过期或进程中断后可接管的批次。
	RecoverableBatches(context.Context, int64, int) ([]BatchInfo, error)
	// FinalizeExpiredCancellation 收口已过期的取消请求。
	FinalizeExpiredCancellation(context.Context, string, int64) (bool, error)
	// ClaimBatch 抢占批次恢复租约。
	ClaimBatch(context.Context, string, string, int64) (bool, error)
	// ResetInterrupted 只重置确认由进程中断造成的失败明细。
	ResetInterrupted(context.Context, string) error
	// RecountBatch 重算批次当前统计。
	RecountBatch(context.Context, string) error
	// PendingRows 查询接管后仍需处理的明细。
	PendingRows(context.Context, string, bool) ([]BatchRow, error)
	// FinalizeBatch 在接管后没有待处理明细时收口批次。
	FinalizeBatch(context.Context, string, string) (string, bool, error)
	// FailClaimedBatch 释放恢复初始化阶段失败的批次租约。
	FailClaimedBatch(context.Context, string, string) (bool, error)
}

// BatchWorkerStarter 是恢复服务启动批量 worker 的生命周期回调。
// 回调由 Server 提供，因此应用服务不持有 HTTP Server、锁或取消映射。
type BatchWorkerStarter func(context.Context, int64, string, string)

// BatchRecoveryOptions 配置恢复扫描器的租约、时钟、令牌和 worker 回调。
type BatchRecoveryOptions struct {
	// LeaseDuration 是恢复后重新声明的批次租约时长。
	LeaseDuration time.Duration
	// NewWorkerToken 生成本次恢复 worker 的租约令牌。
	NewWorkerToken func() string
	// Now 返回当前时间，用于计算租约和查询截止时间。
	Now func() time.Time
	// StartWorker 在批次准备完成后启动实际发布 worker。
	StartWorker BatchWorkerStarter
}

// BatchRecoveryService 编排过期批次接管，不依赖 HTTP、数据库模型或平台实现。
type BatchRecoveryService struct {
	// repository 保存恢复扫描所需的批次状态端口。
	repository BatchRecoveryRepository
	// options 保存恢复扫描的时间和生命周期策略。
	options BatchRecoveryOptions
}

// NewBatchRecoveryService 创建批量恢复服务并校验必需依赖。
func NewBatchRecoveryService(repository BatchRecoveryRepository, options BatchRecoveryOptions) (*BatchRecoveryService, error) {
	if repository == nil {
		return nil, errors.New("批量恢复仓储端口不能为空")
	}
	if options.StartWorker == nil {
		return nil, errors.New("批量恢复 worker 回调不能为空")
	}
	if options.LeaseDuration <= 0 {
		options.LeaseDuration = 5 * time.Minute
	}
	if options.NewWorkerToken == nil {
		options.NewWorkerToken = randomBatchWorkerToken
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &BatchRecoveryService{repository: repository, options: options}, nil
}

// Recover 扫描并接管可恢复批次；单个批次失败时继续处理其他批次。
func (service *BatchRecoveryService) Recover(ctx context.Context) error {
	if service == nil || service.repository == nil || service.options.StartWorker == nil {
		return errors.New("批量恢复服务未初始化")
	}
	// now 保存本轮扫描的当前 Unix 秒，确保查询和租约计算使用同一时间基准。
	now := service.options.Now().UTC()
	// batches 保存数据库返回的可恢复批次快照。
	batches, err := service.repository.RecoverableBatches(ctx, now.Unix(), 20)
	if err != nil {
		return err
	}
	// batch 表示当前待接管的批次快照。
	for _, batch := range batches {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if batch.Status == "canceling" {
			// cancelNow 保存取消请求收口所使用的当前 Unix 秒。
			cancelNow := service.options.Now().UTC().Unix()
			_, _ = service.repository.FinalizeExpiredCancellation(ctx, batch.ID, cancelNow)
			continue
		}
		// workerToken 保存本轮接管使用的批次租约令牌。
		workerToken := service.options.NewWorkerToken()
		if workerToken == "" {
			continue
		}
		// leaseExpiresAt 保存恢复 worker 的租约截止 Unix 秒。
		leaseExpiresAt := service.options.Now().UTC().Add(service.options.LeaseDuration).Unix()
		// claimed 表示当前进程是否成功接管批次。
		claimed, claimErr := service.repository.ClaimBatch(ctx, batch.ID, workerToken, leaseExpiresAt)
		if claimErr != nil || !claimed {
			continue
		}
		// resetErr 保存恢复前重置进程中断明细的错误。
		if resetErr := service.repository.ResetInterrupted(ctx, batch.ID); resetErr != nil {
			service.releaseClaimedBatch(ctx, batch.ID, workerToken)
			continue
		}
		// 统计重算失败不阻断恢复；随后 PendingRows 查询仍是是否启动 worker 的权威结果，
		// 与原有恢复扫描器先尽力重算再继续接管的兼容语义一致。
		_ = service.repository.RecountBatch(ctx, batch.ID)
		// pending 保存恢复后仍可交给 worker 的商品明细。
		pending, pendingErr := service.repository.PendingRows(ctx, batch.ID, false)
		if pendingErr != nil {
			service.releaseClaimedBatch(ctx, batch.ID, workerToken)
			continue
		}
		if len(pending) == 0 {
			_, _, _ = service.repository.FinalizeBatch(ctx, batch.ID, workerToken)
			continue
		}
		service.options.StartWorker(ctx, batch.UserID, batch.ID, workerToken)
	}
	return nil
}

// releaseClaimedBatch 在恢复初始化失败时释放当前 worker 的批次租约。
func (service *BatchRecoveryService) releaseClaimedBatch(ctx context.Context, batchID, workerToken string) {
	// released 和 err 保存租约释放结果；恢复扫描器不应因单批次补偿失败阻塞其他批次。
	_, _ = service.repository.FailClaimedBatch(ctx, batchID, workerToken)
}

// randomBatchWorkerToken 生成不携带业务信息的恢复 worker 令牌。
func randomBatchWorkerToken() string {
	// buffer 保存随机令牌的二进制内容。
	buffer := make([]byte, 16)
	// err 保存系统随机源读取令牌时的错误。
	if _, err := rand.Read(buffer); err != nil {
		return "recovery-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
	}
	return hex.EncodeToString(buffer)
}
