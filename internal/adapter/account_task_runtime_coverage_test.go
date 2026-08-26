package adapter

import (
	"context"
	"errors"
	"testing"

	accountapp "xianyu-go/internal/application/account"
	automationapp "xianyu-go/internal/application/automation"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// nilAdapterContext 返回用于覆盖适配器兼容 nil Context 分支的空上下文接口。
func nilAdapterContext() context.Context { return nil }

// profileMTopFake 是账号资料测试使用的平台客户端替身。
type profileMTopFake struct {
	// orderRuntimeMTopFake 提供资料测试不涉及的其他平台接口默认实现。
	orderRuntimeMTopFake
	// result、err 保存资料接口返回结果和平台错误。
	result *mtop.UserProfileResult
	err    error
}

// FetchUserProfile 返回测试预置的账号资料结果。
func (f *profileMTopFake) FetchUserProfile(context.Context, string) (*mtop.UserProfileResult, error) {
	return f.result, f.err
}

// longLoginClientFake 是长登录适配器测试使用的平台客户端替身。
type longLoginClientFake struct {
	// result、err 保存长登录查询结果和平台错误。
	result *xrenew.LongLoginSettings
	err    error
}

// QueryLongLoginSettings 返回测试预置的长登录查询结果。
func (f *longLoginClientFake) QueryLongLoginSettings(context.Context, string, ...[]cookierefresh.BrowserCookie) (*xrenew.LongLoginSettings, error) {
	return f.result, f.err
}

// SetLongLoginSettings 返回测试预置的长登录设置结果。
func (f *longLoginClientFake) SetLongLoginSettings(context.Context, string, bool, ...[]cookierefresh.BrowserCookie) (*xrenew.LongLoginSettings, error) {
	return f.result, f.err
}

// TestAccountTaskRepositoryCRUDAndRunMapping 验证账号任务设置、运行记录和空依赖边界。
func TestAccountTaskRepositoryCRUDAndRunMapping(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试所有数据库调用共用的非取消上下文。
	ctx := context.Background()
	// repository 是绑定测试存储的账号任务适配器。
	repository := NewAccountTaskRepository(store)
	// defaultSettings、defaultErr 保存未创建设置时的默认值。
	defaultSettings, defaultErr := repository.GetSettings(ctx, "cid")
	if defaultErr != nil || defaultSettings.CookieID != "cid" || defaultSettings.RateContent == "" || defaultSettings.PolishTime != "03:00" {
		t.Fatalf("default settings=%+v err=%v", defaultSettings, defaultErr)
	}
	// settings 是待持久化的账号任务设置。
	settings := automationapp.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true, RateContent: "交易愉快", AutoPolishEnabled: true, PolishTime: "08:30", LastRateScanAt: 11, LastPolishDate: "2026-08-26", LastPolishAt: 22}
	// saveErr 保存账号任务设置写入错误。
	saveErr := repository.SaveSettings(ctx, settings)
	if saveErr != nil {
		t.Fatal(saveErr)
	}
	// savedSettings、savedErr 保存读取后的最终设置。
	savedSettings, savedErr := repository.GetSettings(ctx, "cid")
	if savedErr != nil || savedSettings != settings {
		t.Fatalf("saved settings=%+v want=%+v err=%v", savedSettings, settings, savedErr)
	}
	// claimOK、claimErr 保存账号任务运行记录的幂等创建结果。
	claimOK, claimErr := store.AccountTasks.ClaimRun(ctx, db.AccountTaskRun{RunKey: "adapter-run", CookieID: "cid", TaskType: automationapp.TaskAutoRate, TargetID: "order-1", RunDate: "2026-08-26"}, 100)
	if claimErr != nil || !claimOK {
		t.Fatalf("claim=%v err=%v", claimOK, claimErr)
	}
	// finishErr 保存账号任务运行完成写入错误。
	finishErr := store.AccountTasks.FinishRun(ctx, "adapter-run", "failed", 2, 1, "部分失败", 200)
	if finishErr != nil {
		t.Fatal(finishErr)
	}
	// runs、runsErr 保存应用层运行记录及转换错误。
	runs, runsErr := repository.ListRuns(ctx, "cid", 10)
	if runsErr != nil || len(runs) != 1 || runs[0].RunKey != "adapter-run" || runs[0].SuccessCount != 2 || runs[0].FailedCount != 1 || runs[0].Status != "failed" {
		t.Fatalf("runs=%+v err=%v", runs, runsErr)
	}
	// nilRepository 表示未装配数据库存储的账号任务适配器。
	nilRepository := NewAccountTaskRepository(nil)
	// nilSaveErr、nilRuns、nilListErr 保存空依赖操作的稳定结果。
	nilSaveErr := nilRepository.SaveSettings(ctx, settings)
	// nilRuns、nilListErr 保存空依赖读取运行记录的结果和错误。
	nilRuns, nilListErr := nilRepository.ListRuns(ctx, "cid", 1)
	if nilSaveErr == nil || nilRuns != nil || nilListErr == nil {
		t.Fatalf("nil repository save=%v runs=%v listErr=%v", nilSaveErr, nilRuns, nilListErr)
	}
	// closedStore、closedCleanup 保存账号任务读取数据库关闭错误的 Store。
	closedStore, closedCleanup := newAdapterTestStore(t)
	defer closedCleanup()
	// closedRepository 保存底层数据库连接已关闭的账号任务适配器。
	closedRepository := NewAccountTaskRepository(closedStore)
	// closeErr 保存关闭账号任务测试数据库连接的结果。
	if closeErr := closedStore.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// closedRuns、closedListErr 保存数据库关闭后的运行记录读取结果。
	closedRuns, closedListErr := closedRepository.ListRuns(ctx, "cid", 1)
	if closedRuns != nil || closedListErr == nil {
		t.Fatalf("数据库关闭后运行记录不应成功 runs=%v err=%v", closedRuns, closedListErr)
	}
	// nilRunner 表示未装配自动化中心的账号任务执行器。
	nilRunner := NewAccountTaskRunner(nil)
	// summary、runErr 保存未装配执行器的任务摘要和错误。
	summary, runErr := nilRunner.RunAccountTask(ctx, "cid", automationapp.TaskAutoRate)
	if summary != (automationapp.TaskSummary{}) || !errors.Is(runErr, automationapp.ErrUnavailable) {
		t.Fatalf("nil runner summary=%+v err=%v", summary, runErr)
	}
	// center 保存使用本地 Store 和空平台依赖的自动化中心。
	center := automation.New(store, nil, nil)
	// configuredRunner 保存已装配自动化中心的任务执行适配器。
	configuredRunner := NewAccountTaskRunner(center)
	// configuredSummary、configuredErr 保存不支持任务类型的转换结果。
	configuredSummary, configuredErr := configuredRunner.RunAccountTask(ctx, "cid", "unsupported-task")
	if configuredSummary.TaskType != "unsupported-task" || configuredErr == nil {
		t.Fatalf("不支持任务类型结果异常 summary=%+v err=%v", configuredSummary, configuredErr)
	}
}

// TestAccountRuntimePortContextAndNilManager 验证运行时适配器在取消和未启用账号管理器时的幂等行为。
func TestAccountRuntimePortContextAndNilManager(t *testing.T) {
	// ctx 是本测试使用的非取消上下文。
	ctx := context.Background()
	// canceledContext、cancel 保存用于验证取消传播的上下文及取消函数。
	canceledContext, cancel := context.WithCancel(ctx)
	cancel()
	// nilPort 表示没有账号管理器的运行时端口。
	var nilPort AccountRuntimePort
	// statuses、statusErr 保存未启用管理器时的空状态快照。
	statuses, statusErr := nilPort.RuntimeStatuses(ctx)
	if statusErr != nil || statuses == nil || len(statuses) != 0 {
		t.Fatalf("nil statuses=%v err=%v", statuses, statusErr)
	}
	// updateErr 保存未启用管理器时 Cookie 同步的幂等结果。
	updateErr := nilPort.UpdateCookie(ctx, "cid", "cookie")
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	// restartErr 保存未启用管理器时重启请求的幂等结果。
	restartErr := nilPort.Restart(ctx, "cid")
	if restartErr != nil {
		t.Fatal(restartErr)
	}
	if nilPort.RecoverExpiredCredential(ctx, "cid") {
		t.Fatal("nil manager must not recover credential")
	}
	if AccountRunningLookup(nil)("cid") {
		t.Fatal("nil manager must report account as stopped")
	}
	if NewAccountRuntimePort(nil) != nil {
		t.Fatal("nil manager should not construct runtime port")
	}
	// canceledStatus、canceledStatusErr 保存取消上下文下的状态读取结果。
	canceledStatus, canceledStatusErr := nilPort.RuntimeStatuses(canceledContext)
	if canceledStatus != nil || !errors.Is(canceledStatusErr, context.Canceled) {
		t.Fatalf("canceled statuses=%v err=%v", canceledStatus, canceledStatusErr)
	}
	// canceledUpdateErr、canceledRestartErr 保存取消上下文下的副作用调用错误。
	canceledUpdateErr := nilPort.UpdateCookie(canceledContext, "cid", "cookie")
	// canceledRestartErr 保存取消上下文下的重启调用错误。
	canceledRestartErr := nilPort.Restart(canceledContext, "cid")
	if !errors.Is(canceledUpdateErr, context.Canceled) || !errors.Is(canceledRestartErr, context.Canceled) || nilPort.RecoverExpiredCredential(canceledContext, "cid") {
		t.Fatalf("canceled update=%v restart=%v", canceledUpdateErr, canceledRestartErr)
	}
	if contextError(nilAdapterContext()) != nil || contextError(ctx) != nil || !errors.Is(contextError(canceledContext), context.Canceled) {
		t.Fatal("context error mapping is incorrect")
	}
}

// TestAccountPlatformAdaptersRejectMissingDependencies 验证平台资料和长登录适配器在依赖缺失时不执行副作用。
func TestAccountPlatformAdaptersRejectMissingDependencies(t *testing.T) {
	// ctx 是本测试使用的非取消上下文。
	ctx := context.Background()
	// input 是资料刷新所需的最小账号摘要。
	input := accountapp.ProfileInput{UserID: 1, AccountID: "cid"}
	// profileResult、profileErr 保存未初始化资料适配器的结果。
	profileResult, profileErr := (*AccountProfilePort)(nil).RefreshProfile(ctx, input)
	if profileResult != (accountapp.ProfileResult{}) || profileErr == nil {
		t.Fatalf("profile result=%+v err=%v", profileResult, profileErr)
	}
	// longLoginAdapter 是未初始化长登录依赖的适配器。
	longLoginAdapter := NewLongLoginAdapter(nil, nil, nil, nil)
	// queryResult、queryErr 保存查询长登录状态的初始化错误。
	queryResult, queryErr := longLoginAdapter.QueryLongLogin(ctx, "cid")
	// setResult、setErr 保存设置长登录状态的初始化错误。
	setResult, setErr := longLoginAdapter.SetLongLogin(ctx, "cid", true)
	if queryResult != (accountapp.LongLoginResult{}) || setResult != (accountapp.LongLoginResult{}) || queryErr == nil || setErr == nil {
		t.Fatalf("long login query=%+v/%v set=%+v/%v", queryResult, queryErr, setResult, setErr)
	}
}

// TestAccountProfileAndLongLoginSuccessPaths 验证平台成功结果、响应 Cookie 写回和展示资料保存。
func TestAccountProfileAndLongLoginSuccessPaths(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试所有平台和数据库调用共用的非取消上下文。
	ctx := context.Background()
	// repository 是绑定测试存储的账号登录适配器。
	repository := NewAccountLoginRepository(store)
	// updatedCookies 保存平台响应后由资料适配器同步到运行时的 Cookie。
	updatedCookies := ""
	// profilePort 是返回成功资料和新扁平 Cookie 的平台资料适配器。
	profilePort := NewAccountProfilePort(repository, func() mtop.Client {
		return &profileMTopFake{result: &mtop.UserProfileResult{Nickname: " 新昵称 ", AvatarURL: "http://img.test/avatar", UpdatedCookies: "sid=profile"}}
	}, func(_ context.Context, _ string, value string) {
		updatedCookies = value
	}, nil, nil)
	// profileResult、profileErr 保存资料成功结果。
	profileResult, profileErr := profilePort.RefreshProfile(ctx, accountapp.ProfileInput{UserID: 1, AccountID: "cid", Summary: accountapp.Summary{ID: "cid", Remark: "备注", AvatarURL: "https://cached.test/avatar"}})
	if profileErr != nil || profileResult.Nickname != "新昵称" || profileResult.AvatarURL != "https://img.test/avatar" || profileResult.ErrorMessage != "" || updatedCookies != "sid=profile" {
		t.Fatalf("profile result=%+v err=%v updated=%q", profileResult, profileErr, updatedCookies)
	}
	// summary、summaryErr 保存资料接口成功后写回的本地展示摘要。
	summary, summaryErr := repository.GetOwnedSummary(ctx, 1, "cid")
	if summaryErr != nil || summary.Nickname != "新昵称" || summary.AvatarURL != "https://img.test/avatar" {
		t.Fatalf("saved profile=%+v err=%v", summary, summaryErr)
	}
	// longLoginResult、longLoginErr 保存长登录查询和 Cookie 持久化结果。
	longLogin := NewLongLoginAdapter(repository, func() LongLoginClient {
		return &longLoginClientFake{result: &xrenew.LongLoginSettings{CanOpenLongLogin: true, Enabled: true, SetCookies: []string{"sid=long"}}}
	}, nil, nil)
	// longLoginResult、longLoginErr 保存长登录查询和 Cookie 持久化结果。
	longLoginResult, longLoginErr := longLogin.QueryLongLogin(ctx, "cid")
	if longLoginErr != nil || !longLoginResult.CanOpenLongLogin || !longLoginResult.Enabled {
		t.Fatalf("long login result=%+v err=%v", longLoginResult, longLoginErr)
	}
	// detail、detailErr 保存长登录 Cookie 持久化后的平台凭证视图。
	detail, detailErr := repository.LoadPlatformDetail(ctx, "cid")
	if detailErr != nil || detail.Value != "sid=long" {
		t.Fatalf("long login detail=%+v err=%v", detail, detailErr)
	}
}
