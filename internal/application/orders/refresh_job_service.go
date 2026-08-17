package orders

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// RefreshJobOwner 定义创建刷新任务时验证账号归属所需的最小端口。
type RefreshJobOwner interface {
	// OwnsAccount 判断指定账号是否属于当前用户；错误时调用方应拒绝继续创建任务。
	OwnsAccount(context.Context, int64, string) (bool, error)
}

// RefreshJobIDFactory 生成不携带业务信息的订单刷新任务标识。
type RefreshJobIDFactory func() string

// RefreshJobServiceOptions 配置刷新任务创建时使用的标识、令牌、时钟和租约策略。
type RefreshJobServiceOptions struct {
	// LeaseDuration 是新任务抢占后的租约时长，非正值使用运行器默认租约。
	LeaseDuration time.Duration
	// Now 返回创建和抢占任务时使用的当前时间。
	Now func() time.Time
	// NewJobID 生成新任务标识；nil 时使用随机实现。
	NewJobID RefreshJobIDFactory
	// NewToken 生成新 worker 租约令牌；nil 时使用随机实现。
	NewToken func() string
}

// RefreshJobStartResult 是创建刷新任务并成功启动 worker 后返回的应用结果。
type RefreshJobStartResult struct {
	// Job 是已经持有 running 租约的任务快照。
	Job *RefreshJob
	// Token 是本次 worker 使用的租约令牌，仅供应用内部生命周期关联。
	Token string `json:"-"`
}

// RefreshJobCancelResult 是用户取消刷新任务后的状态结果。
type RefreshJobCancelResult struct {
	// Job 是取消请求完成后的最新任务快照；已取消时状态为 cancelled。
	Job *RefreshJob
	// Cancelled 表示本次调用是否原子地将 queued 或 running 任务改为 cancelled。
	Cancelled bool
}

// RefreshJobService 编排刷新任务的创建、归属校验、租约声明、读取和取消。
// 业务 worker 的取消与恢复生命周期由 runner 持有，本服务不复制取消表。
type RefreshJobService struct {
	// repository 保存刷新任务的持久化状态和租约。
	repository RefreshJobRepository
	// owner 执行账号归属校验，不读取或解密平台凭证。
	owner RefreshJobOwner
	// runner 持有 worker 取消表和后台生命周期。
	runner *RefreshJobRunner
	// options 保存任务 ID、令牌和租约策略。
	options RefreshJobServiceOptions
}

// NewRefreshJobService 创建刷新任务应用 facade 并校验必需端口。
func NewRefreshJobService(repository RefreshJobRepository, owner RefreshJobOwner, runner *RefreshJobRunner, options RefreshJobServiceOptions) (*RefreshJobService, error) {
	if repository == nil {
		return nil, errors.New("订单刷新任务 facade 仓储端口不能为空")
	}
	if owner == nil {
		return nil, errors.New("订单刷新任务账号归属端口不能为空")
	}
	if runner == nil {
		return nil, errors.New("订单刷新任务运行器不能为空")
	}
	if options.LeaseDuration <= 0 {
		options.LeaseDuration = defaultRefreshJobLease
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewJobID == nil {
		options.NewJobID = randomRefreshJobID
	}
	if options.NewToken == nil {
		options.NewToken = randomRefreshJobToken
	}
	return &RefreshJobService{repository: repository, owner: owner, runner: runner, options: options}, nil
}

// CreateAndStart 校验账号归属、创建任务、声明租约并启动应用层 worker。
// requestCtx 仅用于数据库操作；lifecycleCtx 必须来自应用生命周期，不能使用 HTTP 请求 Context。
func (service *RefreshJobService) CreateAndStart(requestCtx, lifecycleCtx context.Context, userID int64, cookieID, status string) (RefreshJobStartResult, error) {
	if service == nil || service.repository == nil || service.owner == nil || service.runner == nil {
		return RefreshJobStartResult{}, errors.New("订单刷新任务 facade 未初始化")
	}
	if requestCtx == nil || lifecycleCtx == nil {
		return RefreshJobStartResult{}, errors.New("订单刷新任务 Context 不能为空")
	}
	if userID <= 0 {
		return RefreshJobStartResult{}, ErrForbidden
	}
	if cookieID != "" {
		// owned、ownerErr 保存账号归属校验结果；归属失败时不创建任务，避免泄露账号存在性。
		owned, ownerErr := service.owner.OwnsAccount(requestCtx, userID, cookieID)
		if ownerErr != nil {
			return RefreshJobStartResult{}, ownerErr
		}
		if !owned {
			return RefreshJobStartResult{}, ErrForbidden
		}
	}
	// job 保存新创建的应用层任务快照；仓储负责将初始状态规范化为 queued。
	job := &RefreshJob{ID: service.options.NewJobID(), UserID: userID, CookieID: cookieID, FilterStatus: status}
	if strings.TrimSpace(job.ID) == "" {
		return RefreshJobStartResult{}, errors.New("订单刷新任务 ID 生成失败")
	}
	// token 保存本次 worker 的不透明租约令牌；校验通过后才创建任务，避免生成空令牌的孤儿任务。
	token := service.options.NewToken()
	if strings.TrimSpace(token) == "" {
		return RefreshJobStartResult{}, errors.New("订单刷新任务租约令牌生成失败")
	}
	// err 保存任务创建持久化错误。
	if err := service.repository.Create(requestCtx, job); err != nil {
		return RefreshJobStartResult{}, err
	}
	// leaseExpiresAt 保存新 worker 租约截止 Unix 秒。
	leaseExpiresAt := service.options.Now().UTC().Add(service.options.LeaseDuration).Unix()
	// claimed、claimErr 保存数据库原子抢占结果及错误。
	claimed, claimErr := service.repository.Claim(requestCtx, job.ID, token, leaseExpiresAt)
	if claimErr != nil {
		return RefreshJobStartResult{}, claimErr
	}
	if !claimed {
		return RefreshJobStartResult{}, ErrRefreshJobCompletionNotApplied
	}
	// err 保存 worker 注册到应用生命周期时的错误。
	if err := service.runner.StartJob(lifecycleCtx, job, token); err != nil {
		// _, releaseErr 保存 worker 启动失败后的租约收口错误；优先保留启动错误。
		_, releaseErr := service.repository.Complete(requestCtx, job.ID, token, "failed", "{}", err.Error())
		if releaseErr != nil {
			return RefreshJobStartResult{}, errors.Join(err, releaseErr)
		}
		return RefreshJobStartResult{}, err
	}
	job.Status = "running"
	job.WorkerToken = token
	job.LeaseExpiresAt = leaseExpiresAt
	return RefreshJobStartResult{Job: job, Token: token}, nil
}

// GetJob 按用户归属读取订单刷新任务，避免 transport 层直接调用任务仓储。
func (service *RefreshJobService) GetJob(ctx context.Context, userID int64, jobID string) (*RefreshJob, error) {
	if service == nil || service.repository == nil {
		return nil, errors.New("订单刷新任务 facade 未初始化")
	}
	if ctx == nil || userID <= 0 || strings.TrimSpace(jobID) == "" {
		return nil, ErrRefreshJobNotFound
	}
	return service.repository.Get(ctx, userID, jobID)
}

// CancelForUser 按用户归属原子取消任务，并通知 runner 停止仍在内存中的 worker。
func (service *RefreshJobService) CancelForUser(ctx context.Context, userID int64, jobID string) (RefreshJobCancelResult, error) {
	if service == nil || service.repository == nil || service.runner == nil {
		return RefreshJobCancelResult{}, errors.New("订单刷新任务 facade 未初始化")
	}
	if ctx == nil || userID <= 0 || strings.TrimSpace(jobID) == "" {
		return RefreshJobCancelResult{}, ErrRefreshJobNotFound
	}
	// cancelled、cancelErr 保存数据库原子取消结果及错误。
	cancelled, cancelErr := service.repository.Cancel(ctx, userID, jobID)
	if cancelErr != nil {
		return RefreshJobCancelResult{}, cancelErr
	}
	if cancelled {
		service.runner.CancelJob(jobID)
		return RefreshJobCancelResult{Job: &RefreshJob{ID: jobID, UserID: userID, Status: "cancelled"}, Cancelled: true}, nil
	}
	// job、getErr 保存取消未生效时的最新任务状态及查询错误。
	job, getErr := service.repository.Get(ctx, userID, jobID)
	if getErr != nil {
		return RefreshJobCancelResult{}, getErr
	}
	return RefreshJobCancelResult{Job: job}, nil
}

// StartRecovery 启动订单刷新任务恢复扫描，并由 runner 管理其停止与等待。
func (service *RefreshJobService) StartRecovery(ctx context.Context) error {
	if service == nil || service.runner == nil {
		return errors.New("订单刷新任务 facade 未初始化")
	}
	return service.runner.StartRecovery(ctx)
}

// Close 停止恢复扫描和全部订单刷新 worker，并等待其退出。
func (service *RefreshJobService) Close(ctx context.Context) error {
	if service == nil || service.runner == nil {
		return nil
	}
	return service.runner.Close(ctx)
}

// Wait 等待应用层拥有的恢复扫描和刷新 worker 全部退出。
func (service *RefreshJobService) Wait() {
	if service == nil || service.runner == nil {
		return
	}
	service.runner.Wait()
}

// OwnsAccount 让 RefreshService 复用其已有刷新仓储的非敏感归属查询能力。
func (service *RefreshService) OwnsAccount(ctx context.Context, userID int64, cookieID string) (bool, error) {
	if service == nil || service.repository == nil {
		return false, errors.New("订单刷新服务未初始化")
	}
	if ctx == nil || userID <= 0 || strings.TrimSpace(cookieID) == "" {
		return false, nil
	}
	return service.repository.ExistsOwned(ctx, userID, cookieID)
}

// randomRefreshJobID 生成任务 ID；随机源失败时使用高精度时间戳保证进程内唯一性。
func randomRefreshJobID() string {
	// buffer 保存任务 ID 的随机二进制部分，不包含用户或账号信息。
	buffer := make([]byte, 16)
	// _, err 保存系统随机源读取结果。
	if _, err := rand.Read(buffer); err == nil {
		return "order-refresh-" + hex.EncodeToString(buffer)
	}
	return "order-refresh-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
}
