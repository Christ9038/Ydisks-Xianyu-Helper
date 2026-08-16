package items

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrBatchLeaseLost 表示当前 worker 已不再拥有批次或明细租约。
var ErrBatchLeaseLost = errors.New("批量任务租约已失效")

// BatchRow 是批量发布 worker 使用的纯业务明细，不暴露数据库行类型。
type BatchRow struct {
	// ID 是批量明细行的持久化标识。
	ID int64
	// BatchID 是所属批次标识。
	BatchID string
	// RowNo 是导入文件中的稳定行号，用于保持发布顺序和导出结果对应关系。
	RowNo int
	// CookieID 是执行发布的账号标识。
	CookieID string
	// Title 是商品标题。
	Title string
	// Description 是商品描述。
	Description string
	// Price 是用户输入的价格文本。
	Price string
	// OriginalPrice 是用户输入的原价文本。
	OriginalPrice string
	// Quantity 是商品库存数量。
	Quantity int
	// PostageMode 是邮费模式。
	PostageMode string
	// Postage 是用户输入的邮费文本。
	Postage string
	// ImagesJSON 是当前商品图片列表的 JSON。
	ImagesJSON string
	// CategoryJSON 是默认类目配置的 JSON。
	CategoryJSON string
	// AutomationJSON 是发布后自动化配置的 JSON。
	AutomationJSON string
	// RawJSON 是平台发布成功后保存的原始结果 JSON。
	RawJSON string
	// ItemID 是已知的平台商品标识；重试已远端成功的行时会复用它。
	ItemID string
	// ItemURL 是已知的平台商品地址。
	ItemURL string
	// Status 是明细当前状态，供管理查询和 worker 过滤使用。
	Status string
	// ErrorMessage 是最近一次失败的用户可见原因，不包含凭证内容。
	ErrorMessage string
	// FailureKind 是失败分类，用于区分可重试、校验失败和远端结果不确定。
	FailureKind string
	// WorkerToken 是当前处理租约令牌，仅用于适配器状态复核。
	WorkerToken string
	// CreatedAt 是明细创建时间文本，沿用数据库时间格式。
	CreatedAt string
	// UpdatedAt 是明细最近更新时间文本，沿用数据库时间格式。
	UpdatedAt string
}

// BatchInfo 是批量发布状态收口所需的非敏感批次信息。
type BatchInfo struct {
	// ID 是批次标识。
	ID string
	// UserID 是批次所属用户标识。
	UserID int64
	// Status 是当前批次状态。
	Status string
	// WorkerToken 是当前租约令牌。
	WorkerToken string
	// DefaultCookieID 是未指定账号行使用的默认发布账号。
	DefaultCookieID string
	// Filename 是用户上传的原始表格文件名。
	Filename string
	// UploadDir 是批次上传文件的受控目录。
	UploadDir string
	// LocationJSON 是批次统一发货地配置的 JSON。
	LocationJSON string
	// TotalCount 是批次明细总数。
	TotalCount int
	// SuccessCount 是已成功发布的明细数。
	SuccessCount int
	// FailedCount 是已失败的明细数。
	FailedCount int
	// LeaseExpiresAt 是当前 worker 租约的 Unix 秒时间戳。
	LeaseExpiresAt int64
	// CreatedAt 是批次创建时间文本，沿用数据库时间格式。
	CreatedAt string
	// UpdatedAt 是批次最近更新时间文本，沿用数据库时间格式。
	UpdatedAt string
}

// BatchRepository 是批量发布 worker 所需的最小持久化端口。
type BatchRepository interface {
	// PendingRows 查询当前批次中可处理的明细。
	PendingRows(context.Context, string, bool) ([]BatchRow, error)
	// RenewBatchLease 为当前 worker 延长批次租约。
	RenewBatchLease(context.Context, string, string, int64) (bool, error)
	// GetBatch 查询批次状态及其所属用户。
	GetBatch(context.Context, int64, string) (BatchInfo, error)
	// ClaimRow 抢占单条明细的处理租约。
	ClaimRow(context.Context, int64, string) (bool, error)
	// BatchStatus 读取批次状态用于分类失败原因。
	BatchStatus(context.Context, string) (string, error)
	// MarkClaimedRowFailed 保存当前 worker 处理失败的明细。
	MarkClaimedRowFailed(context.Context, int64, string, string, string) (bool, error)
	// RecountBatch 重算批次成功和失败统计。
	RecountBatch(context.Context, string) error
	// FinalizeBatch 尝试收口正常完成的批次。
	FinalizeBatch(context.Context, string, string) (string, bool, error)
	// FinalizeCanceled 收口已取消的批次。
	FinalizeCanceled(context.Context, string, string) (bool, error)
	// FinalizeInterrupted 收口被中断或超时的批次。
	FinalizeInterrupted(context.Context, string, string, string) (string, bool, error)
	// DeleteUpload 清理已完成批次的上传文件及其数据库记录。
	DeleteUpload(context.Context, string, string) error
}

// BatchPublisher 执行单条商品发布；平台、凭证和自动化细节由适配器负责。
type BatchPublisher interface {
	// PublishRow 发布一条批量商品并完成本地结果落库。
	PublishRow(context.Context, int64, BatchRow, string) error
}

// PostPublishError 表示平台发布成功后，响应 Cookie 或本地后置步骤未能完成。
// 该错误会阻止当前行自动重试，避免重复创建远端商品。
type PostPublishError struct {
	// Err 保存不含凭证内容的后置处理错误。
	Err error
}

// Error 返回后置处理错误文本。
func (e *PostPublishError) Error() string {
	if e == nil || e.Err == nil {
		return "批量发布后置处理失败"
	}
	return e.Err.Error()
}

// Unwrap 暴露后置处理的原始错误供分类逻辑检查。
func (e *PostPublishError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// UncertainRemotePublishError 表示远端请求结果未知且检查点未可靠保存。
// 该错误禁止自动重试，必须由用户核对平台商品状态。
type UncertainRemotePublishError struct {
	// Err 保存远端结果不确定的原因，不得包含 Cookie 明文。
	Err error
}

// Error 返回远端结果不确定的错误文本。
func (e *UncertainRemotePublishError) Error() string {
	if e == nil || e.Err == nil {
		return "远端发布结果未知"
	}
	return e.Err.Error()
}

// Unwrap 暴露远端结果不确定的原始错误。
func (e *UncertainRemotePublishError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// BatchPublishResult 是批量平台端口返回的非敏感商品结果。
type BatchPublishResult struct {
	// ItemID 是平台商品标识。
	ItemID string
	// ItemURL 是平台商品地址。
	ItemURL string
	// Title 是平台确认后的商品标题。
	Title string
	// PriceText 是平台确认后的价格文本。
	PriceText string
	// CategoryID 是平台确认后的类目标识。
	CategoryID string
	// CategoryName 是平台确认后的类目名称。
	CategoryName string
	// ImageURL 是平台返回的主图地址。
	ImageURL string
	// Quantity 是平台确认后的库存数量。
	Quantity int
	// RawData 是平台原始结果的结构化数据，仅用于受控本地持久化。
	RawData map[string]any
}

// BatchPublishOutcome 是批量平台端口的结果及响应 Cookie 后置错误。
type BatchPublishOutcome struct {
	// Result 是平台商品结果；重试已保存远端结果的行也会提供该字段。
	Result *BatchPublishResult
	// ResponseCookieErr 是发布成功后 Cookie 会话写回失败，不包含 Cookie 内容。
	ResponseCookieErr error
}

// BatchPublishPort 定义单行批量远端发布能力；凭证和平台 DTO 由适配器内部处理。
type BatchPublishPort interface {
	// PublishRemoteRow 执行远端发布并保存远端检查点，不负责商品自动化规则。
	PublishRemoteRow(context.Context, int64, BatchRow, string) (BatchPublishOutcome, error)
}

// FailureClassifier 将发布错误转换为用户可见消息和稳定失败分类。
type FailureClassifier func(error, string) (string, string)

// BatchRunOptions 是 worker 生命周期与平台错误策略的可测试配置。
type BatchRunOptions struct {
	// LeaseDuration 是每次续租所使用的租约时长。
	LeaseDuration time.Duration
	// JobDelay 根据已处理行下标返回下一行前的等待时长。
	JobDelay func(int) time.Duration
	// Wait 在等待行间隔期间响应 Context 取消。
	Wait func(context.Context, time.Duration) error
	// IsSessionExpired 判断错误是否要求立即中断剩余明细。
	IsSessionExpired func(error) bool
	// ClassifyFailure 生成失败明细的消息和分类。
	ClassifyFailure FailureClassifier
}

// BatchRunner 编排批量发布 worker，不依赖 HTTP、数据库或平台 DTO。
type BatchRunner struct {
	// repository 保存批次租约与状态。
	repository BatchRepository
	// publisher 执行单条商品发布适配。
	publisher BatchPublisher
	// options 保存时间、失败分类和取消策略。
	options BatchRunOptions
}

// NewBatchRunner 创建批量发布 worker 编排器并校验必需端口。
func NewBatchRunner(repository BatchRepository, publisher BatchPublisher, options BatchRunOptions) (*BatchRunner, error) {
	if repository == nil {
		return nil, errors.New("批量发布仓储端口不能为空")
	}
	if publisher == nil {
		return nil, errors.New("批量发布平台端口不能为空")
	}
	if options.LeaseDuration <= 0 {
		options.LeaseDuration = 5 * time.Minute
	}
	if options.JobDelay == nil {
		options.JobDelay = func(index int) time.Duration { return time.Duration(10+index%21) * time.Second }
	}
	if options.Wait == nil {
		options.Wait = waitWithContext
	}
	if options.IsSessionExpired == nil {
		options.IsSessionExpired = func(error) bool { return false }
	}
	if options.ClassifyFailure == nil {
		options.ClassifyFailure = defaultFailureClassifier
	}
	return &BatchRunner{repository: repository, publisher: publisher, options: options}, nil
}

// Run 执行一个批次的租约续期、逐行发布、失败记录和最终状态收口。
func (runner *BatchRunner) Run(ctx context.Context, userID int64, batchID, workerToken string, failedOnly bool) error {
	// rows 保存本次 worker 读取到的待处理明细。
	rows, err := runner.repository.PendingRows(ctx, batchID, failedOnly)
	if err != nil {
		if ctx.Err() != nil {
			runner.finishInterrupted(ctx, userID, batchID, workerToken)
		}
		return err
	}
	// rowIndex 表示当前明细在 worker 队列中的下标；row 保存待发布商品明细。
	for rowIndex, row := range rows {
		if ctx.Err() != nil {
			runner.finishInterrupted(ctx, userID, batchID, workerToken)
			return ctx.Err()
		}
		// leaseCtx、leaseCancel 限制状态写入等待时间，避免 worker 退出时继续阻塞。
		leaseCtx, leaseCancel := statusContext(ctx)
		// leaseErr 保存批次续租或租约失效错误。
		leaseErr := runner.renewLease(leaseCtx, batchID, workerToken)
		leaseCancel()
		if leaseErr != nil {
			runner.finishInterrupted(ctx, userID, batchID, workerToken)
			return leaseErr
		}
		// batch 保存租约校验后的批次快照。
		batch, batchErr := runner.repository.GetBatch(ctx, userID, batchID)
		if batchErr != nil || batch.Status != "running" || batch.WorkerToken != workerToken {
			runner.finishInterrupted(ctx, userID, batchID, workerToken)
			return ErrBatchLeaseLost
		}
		// claimed 表示当前 worker 是否抢到这条明细。
		claimed, claimErr := runner.repository.ClaimRow(ctx, row.ID, workerToken)
		if claimErr != nil {
			runner.finishInterrupted(ctx, userID, batchID, workerToken)
			return claimErr
		}
		if !claimed {
			continue
		}
		// rowErr 保存当前商品发布及本地结果落库错误。
		if rowErr := runner.publisher.PublishRow(ctx, userID, row, workerToken); rowErr != nil {
			// statusCtx、statusCancel 让外部动作已返回后的失败事实写入不受请求取消影响，并限制补偿等待时间。
			statusCtx, statusCancel := statusContext(ctx)
			// status 保存失败分类所需的批次状态。
			status, _ := runner.repository.BatchStatus(statusCtx, batchID)
			// message、failureKind 保存用户可见失败信息和稳定分类。
			message, failureKind := runner.options.ClassifyFailure(rowErr, status)
			// marked、markErr 保存失败状态是否成功写入及其错误。
			marked, markErr := runner.repository.MarkClaimedRowFailed(statusCtx, row.ID, workerToken, message, failureKind)
			statusCancel()
			if markErr != nil || !marked {
				runner.finishInterrupted(ctx, userID, batchID, workerToken)
				return fmt.Errorf("保存批量发布失败状态失败: %w", firstNonNil(markErr, ErrBatchLeaseLost))
			}
			if runner.options.IsSessionExpired(rowErr) {
				runner.finishInterrupted(ctx, userID, batchID, workerToken)
				return rowErr
			}
		}
		// recountCtx、recountCancel 让失败明细或外部动作完成后的本地统计重算不受请求取消影响。
		recountCtx, recountCancel := statusContext(ctx)
		// recountErr 保存批次统计重算错误。
		recountErr := runner.repository.RecountBatch(recountCtx, batchID)
		recountCancel()
		if recountErr != nil {
			if ctx.Err() != nil {
				runner.finishInterrupted(ctx, userID, batchID, workerToken)
			}
			return recountErr
		}
		if rowIndex < len(rows)-1 {
			// waitErr 保存行间隔等待期间的取消错误。
			if waitErr := runner.options.Wait(ctx, runner.options.JobDelay(rowIndex)); waitErr != nil {
				runner.finishInterrupted(ctx, userID, batchID, workerToken)
				return waitErr
			}
		}
	}
	runner.finish(ctx, userID, batchID, workerToken)
	return nil
}

// renewLease 续租批次并将失去租约转换为统一应用错误。
func (runner *BatchRunner) renewLease(ctx context.Context, batchID, workerToken string) error {
	// renewed 表示数据库是否仍认可当前 worker 的租约。
	renewed, err := runner.repository.RenewBatchLease(ctx, batchID, workerToken, time.Now().UTC().Add(runner.options.LeaseDuration).Unix())
	if err != nil {
		return err
	}
	if !renewed {
		return ErrBatchLeaseLost
	}
	return nil
}

// finishInterrupted 在 worker 取消、超时或租约丢失后收口批次状态。
func (runner *BatchRunner) finishInterrupted(ctx context.Context, userID int64, batchID, workerToken string) {
	// statusCtx、statusCancel 为状态收口提供独立的短超时。
	statusCtx, statusCancel := statusContext(ctx)
	defer statusCancel()
	// batch、err 保存收口前的批次状态。
	batch, err := runner.repository.GetBatch(statusCtx, userID, batchID)
	if err != nil {
		return
	}
	if batch.Status == "canceling" && batch.WorkerToken == workerToken {
		_, _ = runner.repository.FinalizeCanceled(statusCtx, batchID, workerToken)
		return
	}
	if batch.Status == "canceled" {
		return
	}
	_, _, _ = runner.repository.FinalizeInterrupted(statusCtx, batchID, workerToken, "任务超时或已中断")
}

// finish 收口正常完成或取消中的批次，并清理已完成批次的上传文件。
func (runner *BatchRunner) finish(ctx context.Context, userID int64, batchID, workerToken string) {
	// statusCtx、statusCancel 为最终状态写入提供独立的短超时。
	statusCtx, statusCancel := statusContext(ctx)
	defer statusCancel()
	// batch、err 保存最终收口前的批次状态。
	batch, err := runner.repository.GetBatch(statusCtx, userID, batchID)
	if err != nil || batch.WorkerToken != workerToken || batch.Status == "canceled" {
		return
	}
	if batch.Status == "canceling" {
		_, _ = runner.repository.FinalizeCanceled(statusCtx, batchID, workerToken)
		return
	}
	// finalStatus、finished、finishErr 保存数据库收口结果。
	finalStatus, finished, finishErr := runner.repository.FinalizeBatch(statusCtx, batchID, workerToken)
	if finishErr == nil && finished && finalStatus == "completed" && strings.TrimSpace(batch.UploadDir) != "" {
		_ = runner.repository.DeleteUpload(statusCtx, batch.ID, batch.UploadDir)
	}
}

// statusContext 将状态写入限制在五秒内，同时在父 Context 已取消时保证可收口。
func statusContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent != nil && parent.Err() == nil {
		return context.WithTimeout(parent, 5*time.Second)
	}
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// waitWithContext 按指定时长等待并响应 worker 取消。
func waitWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	// timer 保存当前行间隔定时器，必须在返回前停止或自然释放。
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// defaultFailureClassifier 提供平台无关的基础失败分类，平台适配器可注入更精确策略。
func defaultFailureClassifier(err error, batchStatus string) (string, string) {
	// message 保存错误文本；取消状态只向用户展示取消结果。
	message := err.Error()
	if batchStatus == "canceled" || batchStatus == "canceling" {
		return "任务已取消", "publish"
	}
	return message, "publish"
}

// firstNonNil 返回第一个非空错误，避免租约丢失时丢失更准确的数据库错误。
func firstNonNil(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}
