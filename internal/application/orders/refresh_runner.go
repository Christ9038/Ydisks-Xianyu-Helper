package orders

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// defaultRefreshJobTimeout 限制单个订单刷新 worker 的最长运行时间。
	defaultRefreshJobTimeout = 30 * time.Minute
	// defaultRefreshJobLease 为运行中的订单刷新任务提供可恢复租约。
	defaultRefreshJobLease = defaultRefreshJobTimeout + time.Minute
	// defaultRefreshRecoveryInterval 控制恢复扫描器的默认轮询周期。
	defaultRefreshRecoveryInterval = time.Minute
	// defaultRefreshRecoveryBatchSize 限制每次恢复扫描接管的任务数量。
	defaultRefreshRecoveryBatchSize = 20
)

var (
	// ErrRefreshJobRunnerStopped 表示运行器已经进入关闭状态，不能再接收新任务。
	ErrRefreshJobRunnerStopped = errors.New("订单刷新任务运行器已停止")
	// ErrRefreshJobRunnerInvalidJob 表示调用方没有提供可执行的任务或租约令牌。
	ErrRefreshJobRunnerInvalidJob = errors.New("订单刷新任务或租约令牌无效")
	// ErrRefreshJobCompletionNotApplied 表示终态写入没有命中当前 worker 的租约。
	ErrRefreshJobCompletionNotApplied = errors.New("订单刷新任务终态未写入")
)

// RefreshJobRefresher 定义运行器执行一次订单刷新所需的最小业务端口。
type RefreshJobRefresher interface {
	// Refresh 按任务所属用户、账号和状态筛选条件执行订单刷新。
	Refresh(context.Context, int64, string, string) (RefreshResult, error)
}

// RefreshJobResult 是订单刷新任务持久化和跨应用边界传递的稳定结果模型。
// 字段标签固定为既有任务存储契约的 snake_case 形状，不依赖 HTTP 包或 Server DTO。
type RefreshJobResult struct {
	// PartialFailure 表示本次批量刷新是否有部分账号或订单失败。
	PartialFailure bool `json:"partial_failure"`
	// Message 是面向管理界面的刷新结果说明。
	Message string `json:"message"`
	// Summary 保存本次刷新各阶段的数量统计。
	Summary RefreshJobSummary `json:"summary"`
	// Results 保存按账号或订单拆分的稳定结果行。
	Results []RefreshJobResultItem `json:"results"`
}

// RefreshJobSummary 是订单刷新任务结果中的统计摘要。
type RefreshJobSummary struct {
	// Restored 是同账号软删除订单恢复数量；旧任务缺失该字段时按零读取。
	Restored int `json:"restored,omitempty"`
	// Reassigned 是历史错误归属修正数量；与新增和同账号恢复分别统计。
	Reassigned int `json:"reassigned,omitempty"`
	// Discovered 是从平台发现并导入的新订单数量。
	Discovered int `json:"discovered"`
	// ListUpdated 是订单列表阶段发生字段变化的数量。
	ListUpdated int `json:"list_updated"`
	// SoftDeleted 是平台已不存在而被本地标记删除的数量。
	SoftDeleted int `json:"soft_deleted"`
	// DetailTotal 是进入详情补全阶段的订单数量。
	DetailTotal int `json:"detail_total"`
	// Total 是本次详情补全处理的订单总数量。
	Total int `json:"total"`
	// Updated 是详情补全后状态或字段发生变化的数量。
	Updated int `json:"updated"`
	// NoChange 是详情补全后没有变化的数量。
	NoChange int `json:"no_change"`
	// Failed 是刷新过程中失败的数量。
	Failed int `json:"failed"`
}

// RefreshJobResultItem 是订单刷新任务结果中的单条兼容结果行。
type RefreshJobResultItem struct {
	// Success 表示当前账号或订单阶段是否完成。
	Success bool `json:"success"`
	// CookieID 是当前结果关联的账号标识。
	CookieID string `json:"cookie_id,omitempty"`
	// Discovered 是当前账号发现的新订单数量；非账号结果保持为空。
	Discovered *int `json:"discovered,omitempty"`
	// Updated 是当前账号订单列表更新数量；非账号结果保持为空。
	Updated *int `json:"updated,omitempty"`
	// SoftDeleted 表示当前账号是否标记了失效订单；非账号结果保持为空。
	SoftDeleted *bool `json:"soft_deleted,omitempty"`
	// OrderID 是单订单刷新结果关联的平台订单标识。
	OrderID string `json:"order_id,omitempty"`
	// Stage 是当前结果所处的 discover、detail 或 persist_cookie 阶段。
	Stage string `json:"stage,omitempty"`
	// Message 是面向客户端的结果说明。
	Message string `json:"message,omitempty"`
	// Error 是当前结果的兼容错误文本。
	Error string `json:"error,omitempty"`
	// OldStatus 是详情刷新前的订单状态。
	OldStatus string `json:"old_status,omitempty"`
	// NewStatus 是详情刷新后的订单状态。
	NewStatus string `json:"new_status,omitempty"`
}

// NewRefreshJobResult 将订单刷新应用结果转换为稳定的任务持久化模型。
func NewRefreshJobResult(result RefreshResult) RefreshJobResult {
	// items 保存转换后的结果行；即使没有结果也使用非 nil 空切片维持 [] 契约。
	items := make([]RefreshJobResultItem, 0, len(result.Results))
	// item 表示当前待转换的应用层刷新结果行。
	for _, item := range result.Results {
		// converted 保存当前结果行的基础字段。
		converted := RefreshJobResultItem{
			Success: item.Success, CookieID: item.CookieID, OrderID: item.OrderID,
			Stage: item.Stage, Message: item.Message, Error: item.Error,
			OldStatus: item.OldStatus, NewStatus: item.NewStatus,
		}
		if item.CookieID != "" {
			// discovered 保存账号发现阶段的数量，即使值为零也必须显式持久化。
			discovered := item.Discovered
			// updated 保存账号订单列表阶段的变化数量，即使值为零也必须显式持久化。
			updated := item.Updated
			converted.Discovered, converted.Updated = &discovered, &updated
			if item.Success {
				// softDeleted 保存成功账号刷新是否标记了远端缺失订单。
				softDeleted := item.SoftDeleted != 0
				converted.SoftDeleted = &softDeleted
			}
		}
		items = append(items, converted)
	}
	return RefreshJobResult{
		PartialFailure: result.PartialFailure,
		Message:        result.Message,
		Summary: RefreshJobSummary{
			Restored: result.Summary.Restored, Reassigned: result.Summary.Reassigned,
			Discovered: result.Summary.Discovered, ListUpdated: result.Summary.ListUpdated,
			SoftDeleted: result.Summary.SoftDeleted, DetailTotal: result.Summary.DetailTotal,
			Total: result.Summary.Total, Updated: result.Summary.Updated,
			NoChange: result.Summary.NoChange, Failed: result.Summary.Failed,
		},
		Results: items,
	}
}

// RefreshJobResultMarshaler 将应用层结果模型编码为任务仓储保存的 JSON。
type RefreshJobResultMarshaler func(RefreshJobResult) ([]byte, error)

// RefreshJobRunnerOptions 配置 worker 超时、租约、恢复扫描和可测试时间源。
type RefreshJobRunnerOptions struct {
	// JobTimeout 是单个刷新 worker 的超时时长，非正值使用默认值。
	JobTimeout time.Duration
	// LeaseDuration 是 worker 抢占任务后写入的租约时长，非正值使用默认值。
	LeaseDuration time.Duration
	// RecoveryInterval 是恢复扫描器两次扫描之间的等待时长，非正值使用默认值。
	RecoveryInterval time.Duration
	// RecoveryBatchSize 是每轮恢复扫描最多接管的任务数，非正值使用默认值。
	RecoveryBatchSize int
	// Now 返回当前时间，用于恢复截止时间和租约计算。
	Now func() time.Time
	// NewToken 生成不携带用户信息的 worker 租约令牌。
	NewToken func() string
	// MarshalResult 编码带 snake_case 标签的应用结果；nil 时使用标准 JSON 编码器。
	MarshalResult RefreshJobResultMarshaler
	// OnWorkerError 接收异步 worker 的业务或终态错误；nil 时错误不向外部传播。
	OnWorkerError func(string, error)
	// OnRecoveryError 接收恢复扫描的数据库错误；nil 时错误只结束本轮扫描。
	OnRecoveryError func(error)
}

// refreshJobWorker 保存单个订单刷新 worker 的租约和取消控制。
type refreshJobWorker struct {
	// token 是当前 worker 持有的数据库租约令牌。
	token string
	// cancel 取消当前 worker 的超时 Context。
	cancel context.CancelFunc
}

// RefreshJobRunner 拥有订单刷新 worker、恢复扫描器和关闭等待生命周期。
// mu 保护 workers、active、done、stopped 与 recoveryCancel；锁内只执行内存状态变更和取消，不调用仓储或平台 I/O。
type RefreshJobRunner struct {
	// repository 保存订单刷新任务状态、租约和终态。
	repository RefreshJobRepository
	// refresher 执行实际订单刷新业务。
	refresher RefreshJobRefresher
	// options 保存运行器生命周期和时间策略。
	options RefreshJobRunnerOptions
	// mu 保护 worker 表和生命周期计数，避免 Stop 与异步 worker 竞争。
	mu sync.Mutex
	// workers 保存按任务 ID 索引的可取消 worker；指针身份用于区分相同令牌的重复启动。
	workers map[string]*refreshJobWorker
	// active 是正在运行的 worker 与恢复扫描 goroutine 数量。
	active int
	// done 在 active 归零时关闭；下一轮有任务时重新创建。
	done chan struct{}
	// recoveryCancel 取消当前恢复扫描循环；同时只能存在一个恢复扫描循环。
	recoveryCancel context.CancelFunc
	// stopped 表示运行器不再接受新 worker 或恢复扫描。
	stopped bool
}

// NewRefreshJobRunner 创建订单刷新任务运行器并校验必需端口。
func NewRefreshJobRunner(repository RefreshJobRepository, refresher RefreshJobRefresher, options RefreshJobRunnerOptions) (*RefreshJobRunner, error) {
	if repository == nil {
		return nil, errors.New("订单刷新任务仓储端口不能为空")
	}
	if refresher == nil {
		return nil, errors.New("订单刷新业务端口不能为空")
	}
	if options.JobTimeout <= 0 {
		options.JobTimeout = defaultRefreshJobTimeout
	}
	if options.LeaseDuration <= 0 {
		options.LeaseDuration = defaultRefreshJobLease
	}
	if options.RecoveryInterval <= 0 {
		options.RecoveryInterval = defaultRefreshRecoveryInterval
	}
	if options.RecoveryBatchSize <= 0 {
		options.RecoveryBatchSize = defaultRefreshRecoveryBatchSize
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewToken == nil {
		options.NewToken = randomRefreshJobToken
	}
	if options.MarshalResult == nil {
		options.MarshalResult = func(result RefreshJobResult) ([]byte, error) {
			return json.Marshal(result)
		}
	}
	return &RefreshJobRunner{repository: repository, refresher: refresher, options: options, workers: make(map[string]*refreshJobWorker), done: closedRefreshJobSignal()}, nil
}

// StartJob 启动一个已经由调用方声明租约的订单刷新任务。
// parent 是应用生命周期 Context；调用方不能传入 HTTP 请求 Context，以免请求结束误取消后台任务。
func (runner *RefreshJobRunner) StartJob(parent context.Context, job *RefreshJob, token string) error {
	if runner == nil || runner.repository == nil || runner.refresher == nil {
		return errors.New("订单刷新任务运行器未初始化")
	}
	if parent == nil {
		return errors.New("订单刷新任务生命周期 Context 不能为空")
	}
	if job == nil || strings.TrimSpace(job.ID) == "" || strings.TrimSpace(token) == "" {
		return ErrRefreshJobRunnerInvalidJob
	}
	// jobCtx、cancel 限制后台刷新最长执行时间，并继承应用生命周期取消信号。
	jobCtx, cancel := context.WithTimeout(parent, runner.options.JobTimeout)
	runner.mu.Lock()
	if runner.stopped {
		runner.mu.Unlock()
		cancel()
		return ErrRefreshJobRunnerStopped
	}
	// previous 保存同一任务旧 worker 的取消控制；旧 worker 必须停止以避免并发写入终态。
	previous := runner.workers[job.ID]
	if previous != nil && previous.cancel != nil {
		previous.cancel()
	}
	// worker 保存新登记的任务控制句柄；指针身份避免旧 worker 退出时误删新 worker。
	worker := &refreshJobWorker{token: token, cancel: cancel}
	runner.workers[job.ID] = worker
	runner.beginActiveLocked()
	runner.mu.Unlock()
	go runner.runWorker(jobCtx, cancel, job, token, worker)
	return nil
}

// CancelJob 取消指定任务当前登记的 worker，不改变数据库中的任务终态。
func (runner *RefreshJobRunner) CancelJob(jobID string) bool {
	if runner == nil || strings.TrimSpace(jobID) == "" {
		return false
	}
	runner.mu.Lock()
	// worker 保存当前任务的内存取消控制句柄。
	worker, ok := runner.workers[jobID]
	runner.mu.Unlock()
	if !ok || worker == nil || worker.cancel == nil {
		return false
	}
	worker.cancel()
	return true
}

// RunJob 同步执行一次订单刷新 worker，供恢复、集成测试和生命周期封装复用。
func (runner *RefreshJobRunner) RunJob(ctx context.Context, job *RefreshJob, token string) error {
	if runner == nil || runner.repository == nil || runner.refresher == nil {
		return errors.New("订单刷新任务运行器未初始化")
	}
	if ctx == nil {
		return errors.New("订单刷新任务 Context 不能为空")
	}
	if job == nil || strings.TrimSpace(job.ID) == "" || strings.TrimSpace(token) == "" {
		return ErrRefreshJobRunnerInvalidJob
	}
	// result、err 保存刷新业务结果及错误。
	result, err := runner.refresher.Refresh(ctx, job.UserID, job.CookieID, job.FilterStatus)
	if err != nil {
		// completionErr 保存失败终态写入错误；业务根因始终保留在返回值中。
		completionErr := runner.complete(ctx, job.ID, token, "failed", "{}", err.Error())
		if completionErr != nil {
			return errors.Join(err, completionErr)
		}
		return err
	}
	// resultJSON、marshalErr 保存成功结果的 snake_case JSON 及序列化错误。
	resultJSON, marshalErr := runner.options.MarshalResult(NewRefreshJobResult(result))
	if marshalErr != nil {
		// completionErr 保存序列化失败后的失败终态写入错误。
		completionErr := runner.complete(ctx, job.ID, token, "failed", "{}", marshalErr.Error())
		if completionErr != nil {
			return errors.Join(marshalErr, completionErr)
		}
		return marshalErr
	}
	return runner.complete(ctx, job.ID, token, "succeeded", string(resultJSON), "")
}

// RunRecovery 扫描一次租约过期的订单刷新任务并启动可恢复 worker。
func (runner *RefreshJobRunner) RunRecovery(ctx context.Context) error {
	if runner == nil || runner.repository == nil || runner.refresher == nil {
		return errors.New("订单刷新任务运行器未初始化")
	}
	if ctx == nil {
		return errors.New("订单刷新恢复 Context 不能为空")
	}
	// now 保存本轮扫描统一使用的当前时间，保证截止时间和新租约计算一致。
	now := runner.options.Now().UTC()
	// jobs、err 保存仓储返回的可恢复任务及扫描错误。
	jobs, err := runner.repository.Recoverable(ctx, now.Unix(), runner.options.RecoveryBatchSize)
	if err != nil {
		return err
	}
	// job 表示当前待重新入队并接管的任务快照。
	for _, job := range jobs {
		// err 表示恢复扫描上下文是否已被取消。
		if err := ctx.Err(); err != nil {
			return err
		}
		// requeued、requeueErr 保存过期任务重新入队的结果及错误。
		requeued, requeueErr := runner.repository.RequeueExpired(ctx, job.ID, now.Unix())
		if requeueErr != nil || !requeued {
			continue
		}
		// token 保存本轮恢复 worker 的新租约令牌。
		token := runner.options.NewToken()
		if strings.TrimSpace(token) == "" {
			continue
		}
		// leaseExpiresAt 保存本轮恢复 worker 的租约截止 Unix 秒。
		leaseExpiresAt := now.Add(runner.options.LeaseDuration).Unix()
		// claimed、claimErr 保存恢复 worker 的原子抢占结果及错误。
		claimed, claimErr := runner.repository.Claim(ctx, job.ID, token, leaseExpiresAt)
		if claimErr != nil || !claimed {
			continue
		}
		// jobCopy 保存当前恢复任务的独立副本，避免异步 worker 捕获 range 变量。
		jobCopy := job
		// startErr 保存恢复任务加入应用生命周期 worker 表的错误。
		if startErr := runner.StartJob(ctx, &jobCopy, token); startErr != nil {
			// _, releaseErr 保存恢复 worker 启动失败后的租约终态收口错误。
			_, releaseErr := runner.repository.Complete(ctx, job.ID, token, "failed", "{}", startErr.Error())
			if releaseErr != nil {
				startErr = errors.Join(startErr, releaseErr)
			}
			if runner.options.OnRecoveryError != nil {
				runner.options.OnRecoveryError(fmt.Errorf("启动订单刷新恢复任务 %s: %w", job.ID, startErr))
			}
		}
	}
	return nil
}

// StartRecovery 启动单一恢复扫描循环，并将其纳入运行器 Stop/Wait 生命周期。
func (runner *RefreshJobRunner) StartRecovery(parent context.Context) error {
	if runner == nil {
		return errors.New("订单刷新任务运行器未初始化")
	}
	if parent == nil {
		return errors.New("订单刷新恢复生命周期 Context 不能为空")
	}
	runner.mu.Lock()
	if runner.stopped {
		runner.mu.Unlock()
		return ErrRefreshJobRunnerStopped
	}
	if runner.recoveryCancel != nil {
		runner.mu.Unlock()
		return nil
	}
	// recoveryCtx、cancel 只负责恢复循环本身；Close 时由运行器统一触发取消。
	recoveryCtx, cancel := context.WithCancel(parent)
	runner.recoveryCancel = cancel
	runner.beginActiveLocked()
	runner.mu.Unlock()
	go runner.runRecoveryLoop(recoveryCtx, cancel)
	return nil
}

// Stop 取消所有已登记 worker 和恢复循环；方法可安全重复调用。
func (runner *RefreshJobRunner) Stop() {
	if runner == nil {
		return
	}
	runner.mu.Lock()
	if runner.stopped {
		runner.mu.Unlock()
		return
	}
	runner.stopped = true
	// cancels 保存锁内快照，解锁后再调用取消函数，避免回调重入生命周期锁。
	cancels := make([]context.CancelFunc, 0, len(runner.workers)+1)
	// worker 表示当前仍登记在内存取消表中的任务控制句柄。
	for _, worker := range runner.workers {
		if worker != nil && worker.cancel != nil {
			cancels = append(cancels, worker.cancel)
		}
	}
	if runner.recoveryCancel != nil {
		cancels = append(cancels, runner.recoveryCancel)
	}
	// runner.workers 立即清空，保证 Stop 返回后新的取消请求不会继续引用已停止任务。
	runner.workers = make(map[string]*refreshJobWorker)
	runner.mu.Unlock()
	// cancel 保存当前待触发的取消函数；取消本身不执行网络、数据库或浏览器 I/O。
	for _, cancel := range cancels {
		cancel()
	}
}

// Wait 等待运行器当前已登记的 worker 和恢复循环全部退出。
func (runner *RefreshJobRunner) Wait() {
	if runner == nil {
		return
	}
	runner.mu.Lock()
	// done 保存当前 active 计数归零信号；读取后立即解锁避免阻塞生命周期变更。
	done := runner.done
	runner.mu.Unlock()
	<-done
}

// Close 停止运行器并在给定 Context 内等待所有异步任务退出。
func (runner *RefreshJobRunner) Close(ctx context.Context) error {
	if runner == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("关闭订单刷新任务运行器的 Context 不能为空")
	}
	runner.Stop()
	runner.mu.Lock()
	// done 保存 Stop 后仍在退出的 worker 和恢复循环完成信号。
	done := runner.done
	runner.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runWorker 执行异步 worker，并负责清除租约对应的取消句柄和 active 计数。
func (runner *RefreshJobRunner) runWorker(ctx context.Context, cancel context.CancelFunc, job *RefreshJob, token string, worker *refreshJobWorker) {
	defer cancel()
	defer runner.finishWorker(job.ID, token, worker)
	// err 保存业务刷新或终态写入错误；异步错误通过可选回调交给装配层观测。
	err := runner.RunJob(ctx, job, token)
	if err != nil && runner.options.OnWorkerError != nil {
		runner.options.OnWorkerError(job.ID, err)
	}
}

// runRecoveryLoop 按固定间隔执行恢复扫描，直到 Context 取消。
func (runner *RefreshJobRunner) runRecoveryLoop(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	defer runner.finishRecovery()
	// err 保存首次恢复扫描的错误；错误通过可选回调报告后继续下一轮。
	if err := runner.RunRecovery(ctx); err != nil && runner.options.OnRecoveryError != nil && !errors.Is(err, context.Canceled) {
		runner.options.OnRecoveryError(err)
	}
	// ticker 保存恢复扫描间隔计时器，由本 goroutine 独占并负责停止。
	ticker := time.NewTicker(runner.options.RecoveryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// err 保存本轮恢复扫描错误；单轮失败不阻塞后续轮次。
			if err := runner.RunRecovery(ctx); err != nil && runner.options.OnRecoveryError != nil && !errors.Is(err, context.Canceled) {
				runner.options.OnRecoveryError(err)
			}
		}
	}
}

// complete 将当前 worker 的结果以租约令牌写入任务终态，并拒绝租约失配。
func (runner *RefreshJobRunner) complete(ctx context.Context, jobID, token, status, resultJSON, errorMessage string) error {
	if ctx == nil {
		return errors.New("订单刷新任务终态写入 Context 不能为空")
	}
	// applied、err 保存终态写入是否命中当前租约及仓储错误。
	applied, err := runner.repository.Complete(ctx, jobID, token, status, resultJSON, errorMessage)
	if err != nil {
		return fmt.Errorf("订单刷新任务终态写入失败: %w", err)
	}
	if !applied {
		return fmt.Errorf("%w: job_id=%s", ErrRefreshJobCompletionNotApplied, jobID)
	}
	return nil
}

// beginActiveLocked 登记一个 worker 或恢复循环；调用方必须持有 runner.mu。
func (runner *RefreshJobRunner) beginActiveLocked() {
	if runner.active == 0 {
		runner.done = make(chan struct{})
	}
	runner.active++
}

// finishWorker 清除仍匹配当前租约的 worker 控制句柄并减少生命周期计数。
func (runner *RefreshJobRunner) finishWorker(jobID, token string, worker *refreshJobWorker) {
	runner.mu.Lock()
	// current 保存任务当前登记的控制句柄，用于防止旧 worker 删除新租约。
	current := runner.workers[jobID]
	if current == worker && current.token == token {
		delete(runner.workers, jobID)
	}
	runner.finishActiveLocked()
	runner.mu.Unlock()
}

// finishRecovery 清除恢复循环句柄并减少生命周期计数。
func (runner *RefreshJobRunner) finishRecovery() {
	runner.mu.Lock()
	runner.recoveryCancel = nil
	runner.finishActiveLocked()
	runner.mu.Unlock()
}

// finishActiveLocked 减少活动任务计数并在归零时关闭 done；调用方必须持有 runner.mu。
func (runner *RefreshJobRunner) finishActiveLocked() {
	if runner.active <= 0 {
		return
	}
	runner.active--
	if runner.active == 0 {
		close(runner.done)
	}
}

// closedRefreshJobSignal 创建已经关闭的生命周期信号，供没有后台任务的 Wait 立即返回。
func closedRefreshJobSignal() chan struct{} {
	// done 保存初始无活动任务状态的关闭信号。
	done := make(chan struct{})
	close(done)
	return done
}

// randomRefreshJobToken 生成不携带用户信息的订单刷新 worker 租约令牌。
func randomRefreshJobToken() string {
	// buffer 保存系统随机源生成的租约令牌二进制内容。
	buffer := make([]byte, 16)
	// _, err 保存系统随机源读取结果；异常时回退到不可预测的时间戳文本。
	if _, err := readRefreshRandomBytes(buffer); err == nil {
		return hex.EncodeToString(buffer)
	}
	return "refresh-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
}
