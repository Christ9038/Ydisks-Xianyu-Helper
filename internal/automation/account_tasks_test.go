package automation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// fakeAccountTaskClient 用于本次流程后续判断的fake账号任务Client
type fakeAccountTaskClient struct {
	pendingCalls  int
	rateCalls     int
	polishCalls   int
	pending       []mtop.PendingRateOrder
	pendingErr    error
	rateErr       error
	items         []mtop.ItemListItem
	fetchItemsErr error
	fetchPageSize int
	fetchMaxPages int
	polishErr     error
}

// cancelingAccountTaskClient 在评价动作已经返回成功前取消调用方上下文，用于验证补偿收口不会依赖已取消请求。
type cancelingAccountTaskClient struct {
	// fakeAccountTaskClient 提供除取消场景外的账号任务平台响应。
	*fakeAccountTaskClient
	// cancel 负责模拟外部动作完成后请求生命周期结束。
	cancel context.CancelFunc
}

// RateBuyer 在记录远端动作成功结果前取消调用方上下文。
func (c *cancelingAccountTaskClient) RateBuyer(_ context.Context, cookiesStr, tradeID, feedback string) (*mtop.AccountTaskResult, error) {
	c.rateCalls++
	c.cancel()
	return &mtop.AccountTaskResult{Success: true, Message: "ok"}, nil
}

// FetchPendingRateOrders 封装FetchPendingRate订单列表业务协调。
func (f *fakeAccountTaskClient) FetchPendingRateOrders(context.Context, string, int, int) (*mtop.PendingRateResult, error) {
	f.pendingCalls++
	if f.pendingErr != nil {
		return nil, f.pendingErr
	}
	return &mtop.PendingRateResult{Orders: f.pending}, nil
}

// RateBuyer 封装Rate买家业务协调。
func (f *fakeAccountTaskClient) RateBuyer(context.Context, string, string, string) (*mtop.AccountTaskResult, error) {
	f.rateCalls++
	if f.rateErr != nil {
		return nil, f.rateErr
	}
	return &mtop.AccountTaskResult{Success: true, Message: "ok"}, nil
}

// FetchAllItems 封装FetchAll商品列表业务协调。
func (f *fakeAccountTaskClient) FetchAllItems(_ context.Context, _ string, pageSize, maxPages int) (*mtop.ItemListResult, error) {
	f.fetchPageSize, f.fetchMaxPages = pageSize, maxPages
	if f.fetchItemsErr != nil {
		return nil, f.fetchItemsErr
	}
	return &mtop.ItemListResult{Items: f.items}, nil
}

// PolishItem 封装Polish商品业务协调。
func (f *fakeAccountTaskClient) PolishItem(context.Context, string, string) (*mtop.AccountTaskResult, error) {
	f.polishCalls++
	if f.polishErr != nil {
		return nil, f.polishErr
	}
	return &mtop.AccountTaskResult{Success: true, Message: "ok"}, nil
}

// TestAccountTaskRateIsOrderIdempotent 封装Test账号任务RateIs订单Idempotent业务协调。
func TestAccountTaskRateIsOrderIdempotent(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// client 用于本次流程后续判断的client
	client := &fakeAccountTaskClient{pending: []mtop.PendingRateOrder{{TradeID: "order-1"}, {TradeID: "order-2"}}}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true,
		RateContent: "交易愉快", PolishTime: "03:00"}); err != nil {
		t.Fatal(err)
	}
	// first、err 用于本次流程后续判断的first、err
	first, err := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if err != nil || first.Success != 2 || client.rateCalls != 2 {
		t.Fatalf("first=%+v calls=%d err=%v", first, client.rateCalls, err)
	}
	// second、err 用于本次流程后续判断的second、err
	second, err := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if err != nil || second.Skipped != 2 || client.rateCalls != 2 {
		t.Fatalf("second=%+v calls=%d err=%v", second, client.rateCalls, err)
	}
}

// TestScanAccountTasksRunsEnabledRateConfiguration 验证账号任务扫描器发现启用配置后执行自动评价。
func TestScanAccountTasksRunsEnabledRateConfiguration(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 自动化存储及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是本测试扫描和任务执行共用的上下文。
	ctx := context.Background()
	// client 保存扫描器使用的本地账号任务平台替身。
	client := &fakeAccountTaskClient{pending: []mtop.PendingRateOrder{{TradeID: "scan-order"}}}
	// center 保存注入账号任务平台替身的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	// settings 保存应被扫描器发现的启用账号任务配置。
	settings := db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true, RateContent: "交易愉快", PolishTime: "03:00"}
	// saveErr 保存账号任务配置写入错误。
	if saveErr := store.AccountTasks.Upsert(ctx, settings); saveErr != nil {
		t.Fatal(saveErr)
	}
	center.scanAccountTasks(ctx)
	if client.pendingCalls != 1 || client.rateCalls != 1 {
		t.Fatalf("扫描器未执行评价任务 pending=%d rate=%d", client.pendingCalls, client.rateCalls)
	}
	// emptyCoordinator 保存未装配客户端时应安全返回的扫描器。
	emptyCoordinator := &accountTaskCoordinator{client: func() AccountTaskClient { return nil }}
	emptyCoordinator.scanAccountTasks(ctx)
}

// TestAccountTaskRateFinishFailureQuarantinesExternalSuccess 验证评价动作已成功但运行结果写入失败时会隔离记录，避免下一轮重复评价。
func TestAccountTaskRateFinishFailureQuarantinesExternalSuccess(t *testing.T) {
	// store 是当前测试使用的 SQLite 自动化存储。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是测试数据库操作共用的上下文。
	ctx := context.Background()
	// triggerErr 表示故意阻止 success 状态写入的 SQLite 触发器创建错误。
	if _, triggerErr := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_account_task_success
		BEFORE UPDATE OF status ON account_task_runs
		WHEN NEW.status='success'
		BEGIN SELECT RAISE(ABORT, 'forced account task finish failure'); END`); triggerErr != nil {
		t.Fatal(triggerErr)
	}
	// client 记录远端评价调用次数，验证本地状态异常不会触发同一轮的重复外部动作。
	client := &fakeAccountTaskClient{pending: []mtop.PendingRateOrder{{TradeID: "order-finish-failure"}}}
	// center 是待验证账号任务结果隔离逻辑的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	// settingsErr 表示写入自动评价设置时的数据库错误。
	if settingsErr := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true,
		RateContent: "交易愉快", PolishTime: "03:00"}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// runErr 保存外部动作成功但本地运行结果收口失败后的人工核对错误。
	_, runErr := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if !errors.Is(runErr, errAutomationNeedsReview) || !strings.Contains(runErr.Error(), "保存账号任务运行结果失败") {
		t.Fatalf("运行结果写入失败应返回人工核对错误，err=%v", runErr)
	}
	if client.rateCalls != 1 {
		t.Fatalf("外部评价动作应只执行一次，calls=%d", client.rateCalls)
	}
	// status、successCount、message 保存隔离后的任务状态、已完成动作数和人工核对原因。
	var status, message string
	// successCount 保存已确认完成的评价动作数量。
	var successCount int
	// queryErr 表示读取隔离后任务状态时的数据库错误。
	queryErr := store.DB.QueryRowContext(ctx, `SELECT status,success_count,error_message FROM account_task_runs WHERE run_key=?`,
		"rate:cid:order-finish-failure").Scan(&status, &successCount, &message)
	if queryErr != nil {
		t.Fatal(queryErr)
	}
	if status != "needs_review" || successCount != 1 || !strings.Contains(message, "禁止自动重放") {
		t.Fatalf("任务未正确隔离: status=%q success=%d message=%q", status, successCount, message)
	}
}

// TestAccountTaskRateFinishAndQuarantineFailureJoinsErrors 验证运行结果和人工核对状态均无法落库时不会吞掉任一错误。
func TestAccountTaskRateFinishAndQuarantineFailureJoinsErrors(t *testing.T) {
	// store 是当前测试使用的 SQLite 自动化存储。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是测试数据库操作共用的上下文。
	ctx := context.Background()
	// triggerErr 表示故意阻止 success 与 needs_review 状态写入的 SQLite 触发器创建错误。
	if _, triggerErr := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_account_task_result_states
		BEFORE UPDATE OF status ON account_task_runs
		WHEN NEW.status IN ('success','needs_review')
		BEGIN SELECT RAISE(ABORT, 'forced account task result failure'); END`); triggerErr != nil {
		t.Fatal(triggerErr)
	}
	// client 记录已经成功执行的远端评价动作。
	client := &fakeAccountTaskClient{pending: []mtop.PendingRateOrder{{TradeID: "order-double-failure"}}}
	// center 是待验证双重落库错误传播逻辑的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	// settingsErr 表示写入自动评价设置时的数据库错误。
	if settingsErr := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true,
		RateContent: "交易愉快", PolishTime: "03:00"}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// runErr 保存结果写入和隔离写入均失败后的组合错误。
	_, runErr := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if !errors.Is(runErr, errAutomationNeedsReview) || !strings.Contains(runErr.Error(), "保存账号任务运行结果失败") ||
		!strings.Contains(runErr.Error(), "保存账号任务人工核对状态失败") {
		t.Fatalf("双重落库失败应返回完整错误链，err=%v", runErr)
	}
	if client.rateCalls != 1 {
		t.Fatalf("外部评价动作应只执行一次，calls=%d", client.rateCalls)
	}
}

// TestAccountTaskRateCancellationQuarantinesAndBlocksRetry 验证外部评价成功后请求取消仍能隔离运行并阻止重复重试。
func TestAccountTaskRateCancellationQuarantinesAndBlocksRetry(t *testing.T) {
	// store 是当前测试使用的 SQLite 自动化存储。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// setupCtx 是测试设置写入共用的数据库上下文。
	setupCtx := context.Background()
	// settingsErr 表示写入自动评价设置时的数据库错误。
	if settingsErr := store.AccountTasks.Upsert(setupCtx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true,
		RateContent: "交易愉快", PolishTime: "03:00"}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// requestCtx、cancel 模拟外部动作完成后被上层请求取消的生命周期。
	requestCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// client 在评价动作返回成功前取消 requestCtx，并保留调用次数用于重试断言。
	client := &cancelingAccountTaskClient{
		fakeAccountTaskClient: &fakeAccountTaskClient{pending: []mtop.PendingRateOrder{{TradeID: "order-cancelled"}}},
		cancel:                cancel,
	}
	// center 是待验证取消后补偿收口逻辑的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	// runErr 保存外部评价已完成但调用方取消后的人工核对错误。
	_, runErr := center.RunAccountTask(requestCtx, "cid", TaskAutoRate)
	if !errors.Is(runErr, errAutomationNeedsReview) || !strings.Contains(runErr.Error(), "保存账号任务运行结果失败") {
		t.Fatalf("取消后的外部成功应隔离运行，err=%v", runErr)
	}
	if client.rateCalls != 1 {
		t.Fatalf("首次外部评价调用次数=%d want 1", client.rateCalls)
	}
	// status、successCount 保存补偿收口后的任务状态和已确认成功数量。
	var status string
	// successCount 保存数据库记录的外部评价成功数，用于确认取消请求未抹掉已经完成的动作结果。
	var successCount int
	// queryErr 表示读取补偿状态时的数据库错误。
	queryErr := store.DB.QueryRowContext(context.Background(), `SELECT status,success_count FROM account_task_runs WHERE run_key=?`,
		"rate:cid:order-cancelled").Scan(&status, &successCount)
	if queryErr != nil {
		t.Fatal(queryErr)
	}
	if status != "needs_review" || successCount != 1 {
		t.Fatalf("取消后任务状态错误: status=%q success=%d", status, successCount)
	}
	// retrySummary、retryErr 保存新请求尝试重跑时的结果，确认人工核对状态不会被自动重放。
	retrySummary, retryErr := center.RunAccountTask(context.Background(), "cid", TaskAutoRate)
	if retryErr != nil || retrySummary.Skipped != 1 || client.rateCalls != 1 {
		t.Fatalf("人工核对任务不应自动重试: summary=%+v calls=%d err=%v", retrySummary, client.rateCalls, retryErr)
	}
}

// TestAccountTaskPolishMarkFailureQuarantinesExternalSuccess 验证商品擦亮成功但日期索引写入失败时会隔离运行记录。
func TestAccountTaskPolishMarkFailureQuarantinesExternalSuccess(t *testing.T) {
	// store 是当前测试使用的 SQLite 自动化存储。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是测试数据库操作共用的上下文。
	ctx := context.Background()
	// settingsErr 表示写入擦亮设置时的数据库错误。
	if settingsErr := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoPolishEnabled: true,
		RateContent: "交易愉快", PolishTime: "00:00"}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// triggerErr 表示故意阻止擦亮日期索引写入的 SQLite 触发器创建错误。
	if _, triggerErr := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_account_task_polish_mark
		BEFORE UPDATE OF last_polish_date ON account_task_settings
		WHEN NEW.last_polish_date <> ''
		BEGIN SELECT RAISE(ABORT, 'forced account task polish mark failure'); END`); triggerErr != nil {
		t.Fatal(triggerErr)
	}
	// client 记录远端擦亮调用次数，验证本地状态异常不会导致同一轮重复执行。
	client := &fakeAccountTaskClient{items: []mtop.ItemListItem{{ID: "item-mark-failure"}}}
	// center 是待验证擦亮结果隔离逻辑的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	// runErr 保存外部擦亮成功但日期索引收口失败后的人工核对错误。
	_, runErr := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if !errors.Is(runErr, errAutomationNeedsReview) || !strings.Contains(runErr.Error(), "保存商品擦亮日期") {
		t.Fatalf("擦亮日期写入失败应返回人工核对错误，err=%v", runErr)
	}
	if client.polishCalls != 1 {
		t.Fatalf("外部擦亮动作应只执行一次，calls=%d", client.polishCalls)
	}
	// status 保存被隔离的擦亮运行状态。
	var status string
	// queryErr 表示读取隔离后擦亮运行状态时的数据库错误。
	queryErr := store.DB.QueryRowContext(ctx, `SELECT status FROM account_task_runs WHERE run_key LIKE 'polish:cid:%'`).Scan(&status)
	if queryErr != nil {
		t.Fatal(queryErr)
	}
	if status != "needs_review" {
		t.Fatalf("擦亮日期写入失败后应隔离任务，status=%q", status)
	}
}

// TestAccountTaskSessionExpiredRecoversOnceAndBlocksFurtherAPIRequests 封装Test账号任务会话ExpiredRecoversOnceAndBlocksFurtherAPI请求列表业务协调。
func TestAccountTaskSessionExpiredRecoversOnceAndBlocksFurtherAPIRequests(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// sessionErr 用于本次流程后续判断的会话Err
	sessionErr := &mtop.SessionExpiredError{API: "自动评价接口", Ret: []string{"FAIL_SYS_SESSION_EXPIRED::Session过期"}}
	// client 用于本次流程后续判断的client
	client := &fakeAccountTaskClient{pendingErr: sessionErr}
	// recoverer 用于本次流程后续判断的recoverer
	recoverer := &fakeCredentialRecoverer{store: store, fail: true}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{
		AccountTaskClient:  client,
		OrderDetailFetcher: recoverer,
	})
	if // err 用于本次流程后续判断的err
	err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true,
		RateContent: "交易愉快", PolishTime: "03:00"}); err != nil {
		t.Fatal(err)
	}

	if // err 用于本次流程后续判断的err
	_, err := center.RunAccountTask(ctx, "cid", TaskAutoRate); err == nil || !mtop.IsSessionExpiredErr(err) {
		t.Fatalf("首次 session 失效应触发续期并返回原始分类错误: %v", err)
	}
	if client.pendingCalls != 1 || recoverer.calls != 1 {
		t.Fatalf("first calls: api=%d recover=%d want 1/1", client.pendingCalls, recoverer.calls)
	}
	if // err 用于本次流程后续判断的err
	_, err := center.RunAccountTask(ctx, "cid", TaskAutoRate); err == nil || !strings.Contains(err.Error(), "已停止自动化 API 请求") {
		t.Fatalf("未更新凭证时应保持阻断: %v", err)
	}
	if client.pendingCalls != 1 || recoverer.calls != 1 {
		t.Fatalf("blocked run must not call API/recovery again: api=%d recover=%d", client.pendingCalls, recoverer.calls)
	}

	if // err 用于本次流程后续判断的err
	err := store.Cookies.UpdateValueExisting(ctx, "cid", "unb=1; _m_h5_tk=fresh_1; renewed=1"); err != nil {
		t.Fatal(err)
	}
	client.pendingErr = nil
	if // err 用于本次流程后续判断的err
	_, err := center.RunAccountTask(ctx, "cid", TaskAutoRate); err != nil {
		t.Fatalf("凭证变化后应自动解除阻断: %v", err)
	}
	if client.pendingCalls != 2 {
		t.Fatalf("api calls after credential update=%d want 2", client.pendingCalls)
	}
}

// TestAccountTaskStopsRemainingOrdersOnSessionExpired 封装Test账号任务StopsRemaining订单列表On会话Expired业务协调。
func TestAccountTaskStopsRemainingOrdersOnSessionExpired(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// sessionErr 用于本次流程后续判断的会话Err
	sessionErr := &mtop.SessionExpiredError{API: "评价接口", Ret: []string{"FAIL_SYS_SESSION_EXPIRED::Session过期"}}
	// client 用于本次流程后续判断的client
	client := &fakeAccountTaskClient{
		pending: []mtop.PendingRateOrder{{TradeID: "order-1"}, {TradeID: "order-2"}},
		rateErr: sessionErr,
	}
	// recoverer 用于本次流程后续判断的recoverer
	recoverer := &fakeCredentialRecoverer{store: store, fail: true}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{
		AccountTaskClient:  client,
		OrderDetailFetcher: recoverer,
	})
	if // err 用于本次流程后续判断的err
	err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true,
		RateContent: "交易愉快", PolishTime: "03:00"}); err != nil {
		t.Fatal(err)
	}

	if // err 用于本次流程后续判断的err
	_, err := center.RunAccountTask(ctx, "cid", TaskAutoRate); err == nil {
		t.Fatal("session expiry must be returned")
	}
	if client.rateCalls != 1 || recoverer.calls != 1 {
		t.Fatalf("remaining orders must stop immediately: rate=%d recover=%d", client.rateCalls, recoverer.calls)
	}
}

// TestAccountTaskPolishRunsOncePerBeijingDay 封装Test账号任务Polish运行记录OncePerBeijingDay业务协调。
func TestAccountTaskPolishRunsOncePerBeijingDay(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// client 用于本次流程后续判断的client
	client := &fakeAccountTaskClient{items: []mtop.ItemListItem{{ID: "item-1"}, {ID: "item-2"}}}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoPolishEnabled: true,
		RateContent: "交易愉快", PolishTime: "00:00"}); err != nil {
		t.Fatal(err)
	}
	// first、err 用于本次流程后续判断的first、err
	first, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if err != nil || first.Success != 2 || client.polishCalls != 2 {
		t.Fatalf("first=%+v calls=%d err=%v", first, client.polishCalls, err)
	}
	if client.fetchPageSize != 20 || client.fetchMaxPages != 20 {
		t.Fatalf("unexpected item pagination: pageSize=%d maxPages=%d", client.fetchPageSize, client.fetchMaxPages)
	}
	// second、err 用于本次流程后续判断的second、err
	second, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if err != nil || second.Skipped != 1 || client.polishCalls != 2 {
		t.Fatalf("second=%+v calls=%d err=%v", second, client.polishCalls, err)
	}
	// settings、err 用于本次流程后续判断的settings、err
	settings, err := store.AccountTasks.Get(ctx, "cid")
	if err != nil || settings.LastPolishDate != beijingNow().Format("2006-01-02") {
		t.Fatalf("settings=%+v err=%v", settings, err)
	}
}

// TestManualPolishReportsItemFailures 封装TestManualPolishReports商品Failures业务协调。
func TestManualPolishReportsItemFailures(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// client 用于本次流程后续判断的client
	client := &fakeAccountTaskClient{items: []mtop.ItemListItem{{ID: "item-1"}}, polishErr: errors.New("both polish APIs failed")}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoPolishEnabled: true,
		RateContent: "交易愉快", PolishTime: "00:00"}); err != nil {
		t.Fatal(err)
	}
	// summary、err 用于本次流程后续判断的summary、err
	summary, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if err == nil || summary.Failed != 1 || summary.Success != 0 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

// TestManualPolishCanRetryImmediatelyAfterFailure 封装TestManualPolishCan重试ImmediatelyAfterFailure业务协调。
func TestManualPolishCanRetryImmediatelyAfterFailure(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// client 用于本次流程后续判断的client
	client := &fakeAccountTaskClient{items: []mtop.ItemListItem{{ID: "item-1"}}, fetchItemsErr: errors.New("upstream 502")}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoPolishEnabled: true,
		RateContent: "交易愉快", PolishTime: "00:00"}); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	_, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish); err == nil {
		t.Fatal("first polish should expose the upstream failure")
	}
	client.fetchItemsErr = nil
	// second、err 用于本次流程后续判断的second、err
	second, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if err != nil || second.Success != 1 || second.Skipped != 0 || client.polishCalls != 1 {
		t.Fatalf("manual retry=%+v calls=%d err=%v", second, client.polishCalls, err)
	}
	// third、err 用于本次流程后续判断的third、err
	third, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if err != nil || third.Skipped != 1 || client.polishCalls != 1 {
		t.Fatalf("successful day must remain idempotent: summary=%+v calls=%d err=%v", third, client.polishCalls, err)
	}
}

// TestPolishDueHonorsConfiguredTimeAndDate 封装TestPolishDueHonorsConfigured时间And日期业务协调。
func TestPolishDueHonorsConfiguredTimeAndDate(t *testing.T) {
	// now 用于本次流程后续判断的now
	now := beijingNow()
	// settings 用于本次流程后续判断的设置
	settings := db.AccountTaskSettings{PolishTime: now.Add(2 * time.Hour).Format("15:04")}
	if polishDue(settings, now) && now.Hour() < 22 {
		t.Fatal("task must not run before configured time")
	}
	settings.PolishTime = "00:00"
	if !polishDue(settings, now) {
		t.Fatal("task should be due after midnight")
	}
	settings.LastPolishDate = now.Format("2006-01-02")
	if polishDue(settings, now) {
		t.Fatal("task must not run twice on the same Beijing date")
	}
}

// TestAccountTaskPolishEmptyItemListDoesNotLockDay 验证未获取到在售商品时保留同日重试机会，商品恢复后仍可正常擦亮。
func TestAccountTaskPolishEmptyItemListDoesNotLockDay(t *testing.T) {
	// store、cleanup 保存内存测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// client 初始返回空商品列表，随后模拟商品列表恢复。
	client := &fakeAccountTaskClient{}
	// center 是注入商品列表替身的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	// ctx 是两次手动擦亮共用的测试上下文。
	ctx := context.Background()
	// err 是启用自动擦亮设置时不应出现的存储错误。
	if err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoPolishEnabled: true, RateContent: "交易愉快", PolishTime: "00:00"}); err != nil {
		t.Fatal(err)
	}
	// first、firstErr 分别是空商品列表时的运行摘要和不应出现的业务错误。
	first, firstErr := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if firstErr != nil || first.Found != 0 || first.Success != 0 || client.polishCalls != 0 || first.Message == "" {
		t.Fatalf("first=%+v calls=%d err=%v", first, client.polishCalls, firstErr)
	}
	// settings、settingsErr 分别是空列表运行后的账号任务设置和读取错误；当天日期必须保持为空。
	settings, settingsErr := store.AccountTasks.Get(ctx, "cid")
	if settingsErr != nil || settings.LastPolishDate != "" {
		t.Fatalf("空商品运行不能写入擦亮日期: settings=%+v err=%v", settings, settingsErr)
	}
	// client.items 模拟商品列表恢复，手动执行必须立即重新领取失败运行并实际擦亮。
	client.items = []mtop.ItemListItem{{ID: "item-1"}}
	// second、secondErr 分别是商品恢复后的运行摘要和不应出现的错误。
	second, secondErr := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if secondErr != nil || second.Success != 1 || client.polishCalls != 1 {
		t.Fatalf("second=%+v calls=%d err=%v", second, client.polishCalls, secondErr)
	}
}
