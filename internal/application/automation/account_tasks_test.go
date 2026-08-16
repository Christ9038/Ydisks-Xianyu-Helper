package automation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// accountTaskRepositoryFake 保存账号任务应用服务测试所需的内存状态。
type accountTaskRepositoryFake struct {
	// settings 是按账号保存的任务设置。
	settings AccountTaskSettings
	// runs 是待返回的历史运行记录。
	runs []AccountTaskRun
	// saveErr 是保存设置时模拟的错误。
	saveErr error
}

// GetSettings 返回测试设置。
func (r *accountTaskRepositoryFake) GetSettings(context.Context, string) (AccountTaskSettings, error) {
	return r.settings, nil
}

// SaveSettings 保存测试设置。
func (r *accountTaskRepositoryFake) SaveSettings(_ context.Context, settings AccountTaskSettings) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.settings = settings
	return nil
}

// ListRuns 返回测试运行记录并保留调用上限。
func (r *accountTaskRepositoryFake) ListRuns(_ context.Context, _ string, limit int) ([]AccountTaskRun, error) {
	if len(r.runs) > limit {
		return r.runs[:limit], nil
	}
	return r.runs, nil
}

// accountTaskRunnerFake 保存手动执行调用参数。
type accountTaskRunnerFake struct {
	// summary 是模拟的任务结果。
	summary TaskSummary
	// accountID 是最近一次执行的账号标识。
	accountID string
	// taskType 是最近一次执行的任务类型。
	taskType string
}

// RunAccountTask 记录并返回测试任务结果。
func (r *accountTaskRunnerFake) RunAccountTask(_ context.Context, accountID, taskType string) (TaskSummary, error) {
	r.accountID = accountID
	r.taskType = taskType
	return r.summary, nil
}

// TestServiceUpdateSettings 验证设置规范化、校验和最终值读取。
func TestServiceUpdateSettings(t *testing.T) {
	// repository 保存测试用例的设置状态。
	repository := &accountTaskRepositoryFake{}
	// service 是待验证的账号任务应用服务。
	service := NewService(repository, nil)
	// stored 保存校验通过后的最终设置。
	stored, err := service.UpdateSettings(context.Background(), AccountTaskSettings{
		CookieID: " account-1 ", AutoRateEnabled: true, RateContent: " 交易愉快 ", PolishTime: "03:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if stored.CookieID != "account-1" || stored.RateContent != "交易愉快" {
		t.Fatalf("设置未按应用规则规范化: %+v", stored)
	}
	// saveErr 表示测试仓储未预置保存失败。
	if saveErr := repository.saveErr; saveErr != nil {
		t.Fatal(err)
	}
}

// TestServiceRejectsInvalidSettings 验证非法设置不会进入仓储。
func TestServiceRejectsInvalidSettings(t *testing.T) {
	// cases 保存需要拒绝的输入及稳定错误片段。
	cases := []struct {
		// name 是测试分支名称。
		name string
		// settings 是待校验的设置。
		settings AccountTaskSettings
		// want 是错误信息中应包含的业务提示。
		want string
	}{
		{name: "missing content", settings: AccountTaskSettings{CookieID: "a", AutoRateEnabled: true, PolishTime: "03:00"}, want: "评价内容不能为空"},
		{name: "invalid time", settings: AccountTaskSettings{CookieID: "a", PolishTime: "3:00"}, want: "格式必须"},
	}
	// testCase 是当前待验证的非法账号任务设置分支。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// repository 保存当前分支的持久化假对象。
			repository := &accountTaskRepositoryFake{}
			// err 保存非法设置返回的校验错误。
			_, err := NewService(repository, nil).UpdateSettings(context.Background(), testCase.settings)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("错误=%v，期望包含=%q", err, testCase.want)
			}
		})
	}
}

// TestServiceRunValidatesTypeAndPropagatesRunner 验证任务类型校验和执行摘要透传。
func TestServiceRunValidatesTypeAndPropagatesRunner(t *testing.T) {
	// runner 保存手动任务执行替身。
	runner := &accountTaskRunnerFake{summary: TaskSummary{TaskType: TaskAutoRate, Success: 2}}
	// service 是绑定执行替身的应用服务。
	service := NewService(nil, runner)
	// summary 保存合法任务的结果。
	summary, err := service.Run(context.Background(), "account-1", TaskAutoRate)
	if err != nil || summary.Success != 2 {
		t.Fatalf("执行结果错误: summary=%+v err=%v", summary, err)
	}
	if runner.accountID != "account-1" || runner.taskType != TaskAutoRate {
		t.Fatalf("执行参数错误: account=%q task=%q", runner.accountID, runner.taskType)
	}
	// invalidErr 保存非法任务类型返回的应用层错误。
	if invalidErr := serviceRunInvalid(service); !errors.Is(invalidErr, ErrInvalidTaskType) {
		t.Fatalf("非法任务类型错误=%v", err)
	}
}

// serviceRunInvalid 返回非法任务类型错误，保持主测试只关注应用服务契约。
func serviceRunInvalid(service *Service) error {
	// err 表示应用服务拒绝未知任务类型的契约错误。
	_, err := service.Run(context.Background(), "account-1", "unknown")
	return err
}
