package items

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

var (
	// readBatchLeaseRandomBytes 是批次租约令牌的随机读取器；测试可替换它验证降级令牌语义。
	readBatchLeaseRandomBytes = rand.Read
)

var (
	// ErrBatchNotFound 表示批次不存在或不属于当前用户。
	ErrBatchNotFound = errors.New("商品批量任务不存在")
	// ErrBatchConflict 表示批次租约或状态发生并发冲突。
	ErrBatchConflict = errors.New("商品批量任务状态冲突")
	// ErrBatchInvalidState 表示批次当前状态不允许执行请求操作。
	ErrBatchInvalidState = errors.New("商品批量任务状态不允许执行该操作")
	// ErrBatchNoRows 表示批次没有可继续处理的明细行。
	ErrBatchNoRows = errors.New("商品批量任务没有可处理的明细行")
)

// BatchDetails 是批次查询结果及其明细行的纯应用模型。
type BatchDetails struct {
	// Batch 是批次状态和非敏感展示字段。
	Batch BatchInfo
	// Rows 是批次内按导入顺序排列的明细。
	Rows []BatchRow
}

// BatchManagementRepository 定义批次管理用例所需的最小持久化端口。
type BatchManagementRepository interface {
	// GetBatch 查询指定用户拥有的批次。
	GetBatch(context.Context, int64, string) (BatchInfo, error)
	// ClaimBatch 抢占批次 worker 租约。
	ClaimBatch(context.Context, string, string, int64) (bool, error)
	// PendingRows 查询批次中可处理的明细。
	PendingRows(context.Context, string, bool) ([]BatchRow, error)
	// FinalizeBatch 在没有可处理明细时收口批次。
	FinalizeBatch(context.Context, string, string) (string, bool, error)
	// ListBatchesForUser 查询用户的批次摘要。
	ListBatchesForUser(context.Context, int64, int) ([]BatchInfo, error)
	// ListBatchRows 查询指定批次的全部明细。
	ListBatchRows(context.Context, string) ([]BatchRow, error)
	// RequestCancel 请求取消指定批次并返回当前 worker 状态。
	RequestCancel(context.Context, string) (string, bool, error)
	// DeleteBatch 删除指定用户拥有的非运行批次及其上传文件。
	DeleteBatch(context.Context, int64, string) error
	// ResetFailed 将可重试失败明细恢复为待处理状态。
	ResetFailed(context.Context, string) error
	// RecountBatch 重算批次统计字段。
	RecountBatch(context.Context, string) error
	// ExpiredUploadBatches 查询超过保留期限且仍记录上传目录的批次。
	ExpiredUploadBatches(context.Context, string, int) ([]BatchInfo, error)
	// DeleteUpload 删除批次上传目录并清除持久化路径记录。
	DeleteUpload(context.Context, string, string) error
	// FailClaimedBatch 释放当前 worker 持有的批次租约。
	FailClaimedBatch(context.Context, string, string) (bool, error)
}

// BatchManagementRuntime 定义批次管理服务控制后台 worker 的最小端口。
type BatchManagementRuntime interface {
	// StartBatch 启动指定批次的后台 worker。
	StartBatch(int64, string, string) error
	// CancelBatch 请求停止指定批次的后台 worker。
	CancelBatch(string, string)
}

// BatchManagementService 编排批次启动、查询、取消、删除和重试，不依赖 HTTP 或数据库模型。
type BatchManagementService struct {
	// repository 保存批次状态和明细。
	repository BatchManagementRepository
	// runtime 控制已经声明租约的后台 worker。
	runtime BatchManagementRuntime
	// now 提供当前时间，便于稳定测试租约判断。
	now func() time.Time
	// tokenFactory 生成批次 worker 的租约令牌。
	tokenFactory func() string
}

// NewBatchManagementService 构造批次管理服务并校验必需端口。
func NewBatchManagementService(repository BatchManagementRepository, runtime BatchManagementRuntime) (*BatchManagementService, error) {
	if repository == nil {
		return nil, errors.New("批次管理 repository 未初始化")
	}
	// now 提供生产默认时间源，测试可通过构造后替换为固定时间。
	now := time.Now
	// tokenFactory 提供生产默认租约令牌生成器，测试可通过构造后替换为固定值。
	tokenFactory := func() string { return randomBatchToken() }
	return &BatchManagementService{repository: repository, runtime: runtime, now: now, tokenFactory: tokenFactory}, nil
}

// StartBatch 校验批次状态、声明租约并启动后台 worker。
func (s *BatchManagementService) StartBatch(ctx context.Context, userID int64, batchID string, lease time.Duration) (string, error) {
	if s == nil || s.repository == nil || userID <= 0 || strings.TrimSpace(batchID) == "" {
		return "", ErrBatchNotFound
	}
	// batch 保存当前用户可见的批次快照。
	batch, err := s.repository.GetBatch(ctx, userID, batchID)
	if err != nil {
		return "", ErrBatchNotFound
	}
	// now 保存本次请求使用的统一当前时间。
	now := s.now()
	if batch.Status == "running" && batch.LeaseExpiresAt > now.UTC().Unix() {
		return "", ErrBatchConflict
	}
	if batch.Status != "preview" && batch.Status != "pending" && batch.Status != "completed" && batch.Status != "running" {
		return "", ErrBatchInvalidState
	}
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	// workerToken 保存本次批次租约令牌。
	workerToken := s.tokenFactory()
	// claimed 表示数据库是否成功声明当前租约。
	claimed, err := s.repository.ClaimBatch(ctx, batch.ID, workerToken, now.UTC().Add(lease).Unix())
	if err != nil {
		return "", err
	}
	if !claimed {
		return "", ErrBatchConflict
	}
	// pending 保存租约声明后仍待处理的明细。
	pending, err := s.repository.PendingRows(ctx, batch.ID, false)
	if err != nil {
		// releaseErr 保存读取明细失败后的租约释放错误；主错误保持原始阶段信息。
		if releaseErr := s.releaseClaim(ctx, batch.ID, workerToken); releaseErr != nil {
			return "", errors.Join(err, releaseErr)
		}
		return "", err
	}
	if len(pending) == 0 {
		_, _, _ = s.repository.FinalizeBatch(ctx, batch.ID, workerToken)
		return "", ErrBatchNoRows
	}
	if s.runtime == nil {
		return batch.ID, nil
	}
	// startErr 保存生命周期协调器登记 worker 时的错误。
	if startErr := s.runtime.StartBatch(userID, batch.ID, workerToken); startErr != nil {
		// releaseErr 保存 worker 启动失败后的租约释放错误。
		if releaseErr := s.releaseClaim(ctx, batch.ID, workerToken); releaseErr != nil {
			return "", errors.Join(startErr, releaseErr)
		}
		return "", startErr
	}
	return batch.ID, nil
}

// ListBatches 查询用户的批次摘要。
func (s *BatchManagementService) ListBatches(ctx context.Context, userID int64, limit int) ([]BatchInfo, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return nil, ErrBatchNotFound
	}
	return s.repository.ListBatchesForUser(ctx, userID, limit)
}

// GetBatch 查询用户拥有的批次及其明细。
func (s *BatchManagementService) GetBatch(ctx context.Context, userID int64, batchID string) (BatchDetails, error) {
	if s == nil || s.repository == nil || userID <= 0 || strings.TrimSpace(batchID) == "" {
		return BatchDetails{}, ErrBatchNotFound
	}
	// batch 保存用户归属校验后的批次。
	batch, err := s.repository.GetBatch(ctx, userID, batchID)
	if err != nil {
		return BatchDetails{}, ErrBatchNotFound
	}
	// rows 保存批次全部明细。
	rows, err := s.repository.ListBatchRows(ctx, batch.ID)
	if err != nil {
		return BatchDetails{}, err
	}
	return BatchDetails{Batch: batch, Rows: rows}, nil
}

// CancelBatch 请求批次取消，并通知仍在运行的 worker。
func (s *BatchManagementService) CancelBatch(ctx context.Context, userID int64, batchID string) (string, error) {
	if s == nil || s.repository == nil || userID <= 0 || strings.TrimSpace(batchID) == "" {
		return "", ErrBatchNotFound
	}
	// batch 用于先执行用户归属复核。
	if _, err := s.repository.GetBatch(ctx, userID, batchID); err != nil {
		return "", ErrBatchNotFound
	}
	// workerToken、running 保存取消请求返回的 worker 状态。
	workerToken, running, err := s.repository.RequestCancel(ctx, batchID)
	if err != nil {
		return "", err
	}
	if running && s.runtime != nil {
		s.runtime.CancelBatch(batchID, workerToken)
		return "canceling", nil
	}
	return "canceled", nil
}

// DeleteBatch 删除非运行批次及其上传文件。
func (s *BatchManagementService) DeleteBatch(ctx context.Context, userID int64, batchID string) error {
	if s == nil || s.repository == nil || userID <= 0 || strings.TrimSpace(batchID) == "" {
		return ErrBatchNotFound
	}
	// batch 保存删除前的状态并完成用户归属校验。
	batch, err := s.repository.GetBatch(ctx, userID, batchID)
	if err != nil {
		return ErrBatchNotFound
	}
	if batch.Status == "running" || batch.Status == "canceling" {
		return ErrBatchConflict
	}
	return s.repository.DeleteBatch(ctx, userID, batchID)
}

// FailClaimedBatch 释放指定 worker 持有的批次租约，供异常退出补偿路径使用。
func (s *BatchManagementService) FailClaimedBatch(ctx context.Context, batchID, workerToken string) (bool, error) {
	if s == nil || s.repository == nil || strings.TrimSpace(batchID) == "" || strings.TrimSpace(workerToken) == "" {
		return false, ErrBatchNotFound
	}
	return s.repository.FailClaimedBatch(ctx, batchID, workerToken)
}

// CleanupExpiredUploads 清理超过七天保留期限的批次上传目录。
func (s *BatchManagementService) CleanupExpiredUploads(ctx context.Context, now time.Time, limit int) error {
	if s == nil || s.repository == nil {
		return errors.New("批次管理 repository 未初始化")
	}
	if now.IsZero() {
		now = s.now()
	}
	if limit <= 0 {
		limit = 100
	}
	// cutoff 保存上传目录保留期限的数据库时间文本。
	cutoff := now.UTC().Add(-7 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	// batches 保存待清理的批次快照；路径只交给 repository 处理，不向 HTTP 层暴露数据库模型。
	batches, err := s.repository.ExpiredUploadBatches(ctx, cutoff, limit)
	if err != nil {
		return err
	}
	// cleanupErr 汇总单个批次清理失败，确保一个坏目录不会阻止后续批次清理。
	var cleanupErr error
	// batch 表示当前待清理上传目录的批次快照。
	for _, batch := range batches {
		// err 表示清理循环检测到的上下文取消错误。
		if err := ctx.Err(); err != nil {
			return err
		}
		// err 表示当前批次上传目录或数据库路径清理错误。
		if err := s.repository.DeleteUpload(ctx, batch.ID, batch.UploadDir); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}

// RetryFailedBatch 重置失败明细、声明新租约并启动重试 worker。
func (s *BatchManagementService) RetryFailedBatch(ctx context.Context, userID int64, batchID string, lease time.Duration) (string, error) {
	if s == nil || s.repository == nil || userID <= 0 || strings.TrimSpace(batchID) == "" {
		return "", ErrBatchNotFound
	}
	// batch 保存重试前的用户归属和租约状态。
	batch, err := s.repository.GetBatch(ctx, userID, batchID)
	if err != nil {
		return "", ErrBatchNotFound
	}
	// now 保存本次重试使用的统一当前时间。
	now := s.now()
	if batch.Status == "running" && batch.LeaseExpiresAt > now.UTC().Unix() {
		return "", ErrBatchConflict
	}
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	// workerToken 保存本次重试租约令牌。
	workerToken := s.tokenFactory()
	// claimed 表示数据库是否成功声明重试租约。
	claimed, err := s.repository.ClaimBatch(ctx, batchID, workerToken, now.UTC().Add(lease).Unix())
	if err != nil {
		return "", err
	}
	if !claimed {
		return "", ErrBatchConflict
	}
	// resetErr 保存失败明细重置错误。
	if resetErr := s.repository.ResetFailed(ctx, batchID); resetErr != nil {
		// releaseErr 保存失败重置失败后的租约释放错误；主错误保持重置阶段信息。
		if releaseErr := s.releaseClaim(ctx, batchID, workerToken); releaseErr != nil {
			return "", errors.Join(resetErr, releaseErr)
		}
		return "", resetErr
	}
	// recountErr 保存失败明细重置后的批次统计错误；统计失败不能继续启动 worker。
	if recountErr := s.repository.RecountBatch(ctx, batchID); recountErr != nil {
		// releaseErr 保存统计重算失败后的租约释放错误；主错误保持重算阶段信息。
		if releaseErr := s.releaseClaim(ctx, batchID, workerToken); releaseErr != nil {
			return "", errors.Join(recountErr, releaseErr)
		}
		return "", recountErr
	}
	// pending 保存重置后可重试的明细。
	pending, err := s.repository.PendingRows(ctx, batchID, false)
	if err != nil {
		// releaseErr 保存读取重试明细失败后的租约释放错误；主错误保持查询阶段信息。
		if releaseErr := s.releaseClaim(ctx, batchID, workerToken); releaseErr != nil {
			return "", errors.Join(err, releaseErr)
		}
		return "", err
	}
	if len(pending) == 0 {
		_, _, _ = s.repository.FinalizeBatch(ctx, batchID, workerToken)
		return "", ErrBatchNoRows
	}
	if s.runtime != nil {
		// startErr 保存重试 worker 登记失败错误。
		if startErr := s.runtime.StartBatch(userID, batchID, workerToken); startErr != nil {
			// releaseErr 保存重试 worker 启动失败后的租约释放错误。
			if releaseErr := s.releaseClaim(ctx, batchID, workerToken); releaseErr != nil {
				return "", errors.Join(startErr, releaseErr)
			}
			return "", startErr
		}
	}
	return batchID, nil
}

// releaseClaim 释放批次管理服务已声明但尚未启动 worker 的租约。
func (s *BatchManagementService) releaseClaim(ctx context.Context, batchID, workerToken string) error {
	if s == nil || s.repository == nil {
		return errors.New("批次管理 repository 未初始化")
	}
	// _, releaseErr 保存租约释放调用的结果；布尔值仅表示是否成功匹配租约。
	_, releaseErr := s.repository.FailClaimedBatch(ctx, batchID, workerToken)
	return releaseErr
}

// randomBatchToken 生成批次租约所需的不透明令牌；具体随机源由服务默认实现负责。
func randomBatchToken() string {
	// bytes 保存租约令牌的随机字节，令牌不携带用户或商品信息。
	bytes := make([]byte, 16)
	// _, err 保存随机源读取结果；系统随机源失败时回退到时间戳以保持旧接口可用。
	if _, err := readBatchLeaseRandomBytes(bytes); err == nil {
		return hex.EncodeToString(bytes)
	}
	return time.Now().UTC().Format("20060102150405.000000000")
}
