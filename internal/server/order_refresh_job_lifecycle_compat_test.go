package server

import (
	"context"
	"errors"

	orderapp "xianyu-go/internal/application/orders"
)

// orderRefreshJobRefresh 是历史生命周期测试使用的 HTTP 结果回调类型。
type orderRefreshJobRefresh func(context.Context, int64, string, string) (orderRefreshResponse, error)

// orderRefreshJobMarshal 是历史生命周期测试使用的结果序列化回调类型。
type orderRefreshJobMarshal func(any) ([]byte, error)

// orderRefreshJobComplete 是历史生命周期测试使用的任务终态写入回调类型。
type orderRefreshJobComplete func(context.Context, string, string, string, string, string) (bool, error)

// errOrderRefreshJobCompletionNotApplied 保留旧测试对应用层租约失配错误的引用。
var errOrderRefreshJobCompletionNotApplied = orderapp.ErrRefreshJobCompletionNotApplied

// orderRefreshLifecycleRefresher 将历史 HTTP 结果回调适配到订单应用刷新结果。
type orderRefreshLifecycleRefresher struct {
	// refresh 保存测试注入的刷新业务回调。
	refresh orderRefreshJobRefresh
}

// Refresh 执行测试回调并把 HTTP 结果转换为应用层刷新结果。
func (refresher orderRefreshLifecycleRefresher) Refresh(ctx context.Context, userID int64, cookieID, status string) (orderapp.RefreshResult, error) {
	// result、err 保存历史刷新回调的结果及错误。
	result, err := refresher.refresh(ctx, userID, cookieID, status)
	if err != nil {
		return orderapp.RefreshResult{}, err
	}
	return orderapp.RefreshResult{PartialFailure: result.PartialFailure, Message: result.Message, Summary: orderapp.RefreshSummary{
		Discovered: result.Summary.Discovered, ListUpdated: result.Summary.ListUpdated,
		SoftDeleted: result.Summary.SoftDeleted, DetailTotal: result.Summary.DetailTotal,
		Total: result.Summary.Total, Updated: result.Summary.Updated,
		NoChange: result.Summary.NoChange, Failed: result.Summary.Failed,
	}}, nil
}

// orderRefreshLifecycleRepository 将测试终态回调适配到应用层任务仓储。
type orderRefreshLifecycleRepository struct {
	// complete 保存测试注入的终态写入回调。
	complete orderRefreshJobComplete
}

// Create 满足订单刷新任务仓储接口；生命周期测试不验证创建流程。
func (repository orderRefreshLifecycleRepository) Create(context.Context, *orderapp.RefreshJob) error {
	return nil
}

// Get 满足订单刷新任务仓储接口；生命周期测试不读取任务。
func (repository orderRefreshLifecycleRepository) Get(context.Context, int64, string) (*orderapp.RefreshJob, error) {
	return nil, orderapp.ErrRefreshJobNotFound
}

// Claim 满足订单刷新任务仓储接口；生命周期测试直接执行已声明租约的任务。
func (repository orderRefreshLifecycleRepository) Claim(context.Context, string, string, int64) (bool, error) {
	return true, nil
}

// Cancel 满足订单刷新任务仓储接口；生命周期测试不验证取消持久化。
func (repository orderRefreshLifecycleRepository) Cancel(context.Context, int64, string) (bool, error) {
	return false, nil
}

// Complete 调用测试注入的终态写入回调。
func (repository orderRefreshLifecycleRepository) Complete(ctx context.Context, jobID, token, status, resultJSON, errorMessage string) (bool, error) {
	return repository.complete(ctx, jobID, token, status, resultJSON, errorMessage)
}

// Recoverable 满足订单刷新任务仓储接口；生命周期测试不验证恢复扫描。
func (repository orderRefreshLifecycleRepository) Recoverable(context.Context, int64, int) ([]orderapp.RefreshJob, error) {
	return nil, nil
}

// RequeueExpired 满足订单刷新任务仓储接口；生命周期测试不验证恢复扫描。
func (repository orderRefreshLifecycleRepository) RequeueExpired(context.Context, string, int64) (bool, error) {
	return false, nil
}

// runOrderRefreshJobWith 通过应用层 runner 执行历史生命周期测试的可注入任务。
func runOrderRefreshJobWith(ctx context.Context, job *orderapp.RefreshJob, token string, refresh orderRefreshJobRefresh, marshal orderRefreshJobMarshal, complete orderRefreshJobComplete) error {
	if refresh == nil || marshal == nil || complete == nil {
		return errors.New("订单刷新测试依赖未初始化")
	}
	// repository 保存测试终态回调的应用仓储适配器。
	repository := orderRefreshLifecycleRepository{complete: complete}
	// runner、err 保存应用层订单刷新运行器及构造错误。
	runner, err := orderapp.NewRefreshJobRunner(repository, orderRefreshLifecycleRefresher{refresh: refresh}, orderapp.RefreshJobRunnerOptions{
		MarshalResult: func(result orderapp.RefreshJobResult) ([]byte, error) { return marshal(result) },
	})
	if err != nil {
		return err
	}
	return runner.RunJob(ctx, job, token)
}
