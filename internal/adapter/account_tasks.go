package adapter

import (
	"context"
	"errors"

	automationapp "xianyu-go/internal/application/automation"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
)

// AccountTaskRepository 将账号任务数据库模型适配为应用层模型。
type AccountTaskRepository struct {
	// store 保存账号任务持久化入口。
	store *db.Store
}

// NewAccountTaskRepository 构造账号任务仓储适配器。
func NewAccountTaskRepository(store *db.Store) *AccountTaskRepository {
	return &AccountTaskRepository{store: store}
}

// GetSettings 读取账号任务设置并移除数据库模型依赖。
func (r *AccountTaskRepository) GetSettings(ctx context.Context, accountID string) (automationapp.AccountTaskSettings, error) {
	// validateErr 表示适配器缺少数据库任务仓储时的装配错误。
	if validateErr := r.validate(); validateErr != nil {
		return automationapp.AccountTaskSettings{}, validateErr
	}
	// settings、err 保存数据库设置及读取错误。
	settings, err := r.store.AccountTasks.Get(ctx, accountID)
	return accountTaskSettingsModel(settings), err
}

// SaveSettings 将应用层账号任务设置写入数据库。
func (r *AccountTaskRepository) SaveSettings(ctx context.Context, settings automationapp.AccountTaskSettings) error {
	// validateErr 表示适配器缺少数据库任务仓储时的装配错误。
	if validateErr := r.validate(); validateErr != nil {
		return validateErr
	}
	return r.store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{
		CookieID: settings.CookieID, AutoRateEnabled: settings.AutoRateEnabled, RateContent: settings.RateContent,
		AutoPolishEnabled: settings.AutoPolishEnabled, PolishTime: settings.PolishTime,
		LastRateScanAt: settings.LastRateScanAt, LastPolishDate: settings.LastPolishDate, LastPolishAt: settings.LastPolishAt,
	})
}

// ListRuns 读取账号任务运行记录并转换为应用层模型。
func (r *AccountTaskRepository) ListRuns(ctx context.Context, accountID string, limit int) ([]automationapp.AccountTaskRun, error) {
	// validateErr 表示适配器缺少数据库任务仓储时的装配错误。
	if validateErr := r.validate(); validateErr != nil {
		return nil, validateErr
	}
	// runs、err 保存数据库运行记录及读取错误。
	runs, err := r.store.AccountTasks.RecentRuns(ctx, accountID, limit)
	if err != nil {
		return nil, err
	}
	// result 保存转换后的应用层运行记录。
	result := make([]automationapp.AccountTaskRun, 0, len(runs))
	// run 是当前待转换的数据库运行记录。
	for _, run := range runs {
		result = append(result, automationapp.AccountTaskRun{
			ID: run.ID, RunKey: run.RunKey, CookieID: run.CookieID, TaskType: run.TaskType, TargetID: run.TargetID,
			RunDate: run.RunDate, Status: run.Status, SuccessCount: run.SuccessCount, FailedCount: run.FailedCount,
			ErrorMessage: run.ErrorMessage, NextRetryAt: run.NextRetryAt, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
		})
	}
	return result, nil
}

// validate 检查账号任务数据库适配器是否具备必需的 Store 和子仓储。
func (r *AccountTaskRepository) validate() error {
	if r == nil || r.store == nil || r.store.AccountTasks == nil {
		return errors.New("账号任务数据库适配器未初始化")
	}
	return nil
}

// accountTaskSettingsModel 将数据库账号任务设置转换为应用模型。
func accountTaskSettingsModel(settings db.AccountTaskSettings) automationapp.AccountTaskSettings {
	return automationapp.AccountTaskSettings{
		CookieID: settings.CookieID, AutoRateEnabled: settings.AutoRateEnabled, RateContent: settings.RateContent,
		AutoPolishEnabled: settings.AutoPolishEnabled, PolishTime: settings.PolishTime,
		LastRateScanAt: settings.LastRateScanAt, LastPolishDate: settings.LastPolishDate, LastPolishAt: settings.LastPolishAt,
	}
}

// AccountTaskRunner 将自动化中心的执行结果适配为应用层摘要。
type AccountTaskRunner struct {
	// center 执行账号评价和商品擦亮任务。
	center *automation.Center
}

// NewAccountTaskRunner 构造账号任务执行适配器。
func NewAccountTaskRunner(center *automation.Center) *AccountTaskRunner {
	return &AccountTaskRunner{center: center}
}

// RunAccountTask 执行任务并转换非敏感结果摘要。
func (r *AccountTaskRunner) RunAccountTask(ctx context.Context, accountID, taskType string) (automationapp.TaskSummary, error) {
	if r == nil || r.center == nil {
		return automationapp.TaskSummary{}, automationapp.ErrUnavailable
	}
	// summary、err 保存自动化中心结果及执行错误。
	summary, err := r.center.RunAccountTask(ctx, accountID, taskType)
	return automationapp.TaskSummary{
		TaskType: summary.TaskType, Found: summary.Found, Success: summary.Success,
		Failed: summary.Failed, Skipped: summary.Skipped, Message: summary.Message,
	}, err
}

var _ automationapp.Repository = (*AccountTaskRepository)(nil)
var _ automationapp.Runner = (*AccountTaskRunner)(nil)
