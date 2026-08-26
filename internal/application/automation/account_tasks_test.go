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
	// getErr、listErr 是读取设置和历史记录时模拟的错误。
	getErr, listErr error
}

// GetSettings 返回测试设置。
func (r *accountTaskRepositoryFake) GetSettings(context.Context, string) (AccountTaskSettings, error) {
	return r.settings, r.getErr
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
	if r.listErr != nil {
		return nil, r.listErr
	}
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
	// err 是手动执行时模拟的自动化中心错误。
	err error
}

// RunAccountTask 记录并返回测试任务结果。
func (r *accountTaskRunnerFake) RunAccountTask(_ context.Context, accountID, taskType string) (TaskSummary, error) {
	r.accountID = accountID
	r.taskType = taskType
	return r.summary, r.err
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

// TestServiceReadsHistoryAndPropagatesTaskErrors 验证设置读取、历史分页、保存失败和手动任务错误路径。
func TestServiceReadsHistoryAndPropagatesTaskErrors(t *testing.T) {
	// wantErr 是账号任务仓储和执行端口返回的确定性错误。
	wantErr := errors.New("account task failed")
	// repository 是带设置和历史记录的任务仓储替身。
	repository := &accountTaskRepositoryFake{settings: AccountTaskSettings{CookieID: "account-1"}, runs: []AccountTaskRun{{ID: 1}, {ID: 2}}}
	// service 是绑定任务仓储的应用服务。
	service := NewService(repository, nil)
	// settings、settingsErr 保存设置读取结果。
	settings, settingsErr := service.GetSettings(context.Background(), " account-1 ")
	if settingsErr != nil || settings.CookieID != "account-1" {
		t.Fatalf("settings=%+v err=%v", settings, settingsErr)
	}
	// runs、runsErr 保存默认分页后的历史运行记录。
	runs, runsErr := service.ListRuns(context.Background(), " account-1 ", 0)
	if runsErr != nil || len(runs) != 2 {
		t.Fatalf("runs=%+v err=%v", runs, runsErr)
	}
	// updateErr 保存设置写入失败结果。
	repository.saveErr = wantErr
	// updateErr 保存设置写入失败结果。
	if _, updateErr := service.UpdateSettings(context.Background(), AccountTaskSettings{CookieID: "account-1", PolishTime: "03:00"}); !errors.Is(updateErr, wantErr) {
		t.Fatalf("update error=%v", updateErr)
	}
	// repository.saveErr 清除保存错误，供后续覆盖保存成功后读取最终值失败的分支。
	repository.saveErr = nil
	// repository.getErr 保存设置查询失败结果。
	repository.getErr = wantErr
	// getErr 保存设置查询失败结果。
	if _, getErr := service.GetSettings(context.Background(), "account-1"); !errors.Is(getErr, wantErr) {
		t.Fatalf("get error=%v", getErr)
	}
	// updateAfterReadErr 保存保存成功但读取最终设置失败的结果。
	if _, updateAfterReadErr := service.UpdateSettings(context.Background(), AccountTaskSettings{CookieID: "account-1", PolishTime: "03:00"}); !errors.Is(updateAfterReadErr, wantErr) {
		t.Fatalf("update after read error=%v", updateAfterReadErr)
	}
	// repository.listErr 保存历史查询失败结果。
	repository.listErr = wantErr
	// listErr 保存历史查询失败结果。
	if _, listErr := service.ListRuns(context.Background(), "account-1", 101); !errors.Is(listErr, wantErr) {
		t.Fatalf("list error=%v", listErr)
	}
	// runner 是返回错误的手动任务执行端口。
	runner := &accountTaskRunnerFake{err: wantErr}
	// runService 是绑定失败执行端口的应用服务。
	runService := NewService(nil, runner)
	// runErr 保存手动任务执行端口返回的错误。
	if _, runErr := runService.Run(context.Background(), " account-1 ", TaskAutoPolish); !errors.Is(runErr, wantErr) || runner.accountID != "account-1" {
		t.Fatalf("run error=%v account=%q", runErr, runner.accountID)
	}
	// invalidService 是缺少任务执行端口的服务。
	invalidService := NewService(nil, nil)
	// unavailableErr 保存缺少执行端口时的错误。
	if _, unavailableErr := invalidService.Run(context.Background(), "account-1", TaskAutoRate); !errors.Is(unavailableErr, ErrUnavailable) {
		t.Fatalf("unavailable run error=%v", unavailableErr)
	}
	// longContent 是超过评价内容上限的输入。
	longContent := strings.Repeat("评", 501)
	// longContentErr 保存超长评价内容的校验错误。
	if _, longContentErr := service.UpdateSettings(context.Background(), AccountTaskSettings{CookieID: "account-1", RateContent: longContent, PolishTime: "03:00"}); longContentErr == nil {
		t.Fatal("long rate content should be rejected")
	}
	// emptyAccountErr 保存空账号标识的校验错误。
	if _, emptyAccountErr := service.UpdateSettings(context.Background(), AccountTaskSettings{PolishTime: "03:00"}); emptyAccountErr == nil {
		t.Fatal("empty account should be rejected")
	}
}

// TestServiceRejectsMissingAccountTaskRepository 验证缺少账号任务仓储时读取和分页接口不会伪装成功。
func TestServiceRejectsMissingAccountTaskRepository(t *testing.T) {
	// service 是缺少持久化仓储的账号任务服务。
	service := NewService(nil, nil)
	// settingsErr 保存读取账号任务设置时的装配错误。
	if _, settingsErr := service.GetSettings(context.Background(), "account-1"); settingsErr == nil {
		t.Fatal("缺少仓储时 GetSettings 不应成功")
	}
	// runsErr 保存查询账号任务历史时的装配错误。
	if _, runsErr := service.ListRuns(context.Background(), "account-1", 20); runsErr == nil {
		t.Fatal("缺少仓储时 ListRuns 不应成功")
	}
	// nilService 表示未初始化的账号任务服务指针。
	var nilService *Service
	// nilSettingsErr、nilRunsErr 保存空服务指针的设置读取和历史分页错误。
	_, nilSettingsErr := nilService.GetSettings(context.Background(), "account-1")
	// nilRunsErr 保存空服务指针的历史分页错误。
	_, nilRunsErr := nilService.ListRuns(context.Background(), "account-1", 20)
	// nilUpdateResult、nilUpdateErr 保存空服务指针的设置更新错误。
	nilUpdateResult, nilUpdateErr := nilService.UpdateSettings(context.Background(), AccountTaskSettings{CookieID: "account-1", PolishTime: "03:00"})
	if nilSettingsErr == nil || nilRunsErr == nil || nilUpdateErr == nil || nilUpdateResult.CookieID != "" {
		t.Fatal("空服务指针的读取不应成功")
	}
}

// serviceRunInvalid 返回非法任务类型错误，保持主测试只关注应用服务契约。
func serviceRunInvalid(service *Service) error {
	// err 表示应用服务拒绝未知任务类型的契约错误。
	_, err := service.Run(context.Background(), "account-1", "unknown")
	return err
}
