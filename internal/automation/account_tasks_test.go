package automation

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// fakeAccountTaskClient 用于本次流程后续判断的fake账号任务Client
type fakeAccountTaskClient struct {
	pendingCalls int
	rateCalls    int
	polishCalls  int
	pending      []mtop.PendingRateOrder
	pendingErr   error
	rateErr      error
	items        []mtop.ItemListItem
	// fetchItemsCalls 记录商品列表查询次数，用于确认空列表成功不会触发下一轮调度重试。
	fetchItemsCalls int
	fetchItemsErr   error
	fetchPageSize   int
	fetchMaxPages   int
	polishErr       error
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
	f.fetchItemsCalls++
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

// seedLocalCompletedOrders 写入已由买家确认收货事件确认的本地订单，供自动评价任务读取而无需查询平台列表。
func seedLocalCompletedOrders(t *testing.T, store *db.Store, ctx context.Context, orderIDs ...string) {
	t.Helper()
	// orderID 表示当前要写入确认收货事实的本地订单标识。
	for _, orderID := range orderIDs {
		// upsertErr 保存“已完成”订单事实写入错误；完成时间是自动评价任务的本地消费门槛。
		upsertErr := store.Orders.Upsert(ctx, orderID, db.OrderUpsertOpts{CookieID: "cid", OrderStatus: "completed"})
		if upsertErr != nil {
			t.Fatalf("写入本地已完成订单 %q: %v", orderID, upsertErr)
		}
		// markErr 保存确认收货事件时间写入错误，缺少该时间的订单不得触发远端评价。
		markErr := store.Automation.MarkOrderEventTime(ctx, orderID, "completed_at")
		if markErr != nil {
			t.Fatalf("写入确认收货时间 %q: %v", orderID, markErr)
		}
	}
}

// TestScanAccountTasksTreatsEmptyItemListAsSuccess 验证调度扫描拿到空商品列表后完成当日任务且不会重复重试。
func TestScanAccountTasksTreatsEmptyItemListAsSuccess(t *testing.T) {
	// store、cleanup 保存扫描测试使用的 SQLite 存储及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是扫描和任务仓储调用共用的上下文。
	ctx := context.Background()
	// client 返回成功但为空的商品列表，并记录查询调用次数。
	client := &fakeAccountTaskClient{}
	// center 是注入本地平台替身后的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	// settings 是到达擦亮时间的启用任务配置。
	settings := db.AccountTaskSettings{CookieID: "cid", AutoPolishEnabled: true, RateContent: "交易愉快", PolishTime: beijingNow().Format("15:04")}
	// saveErr 保存任务配置写入错误。
	if saveErr := store.AccountTasks.Upsert(ctx, settings); saveErr != nil {
		t.Fatal(saveErr)
	}
	center.scanAccountTasks(ctx)
	// firstSettings、firstSettingsErr 保存第一次空列表扫描后写入的当日完成状态。
	firstSettings, firstSettingsErr := store.AccountTasks.Get(ctx, "cid")
	if firstSettingsErr != nil || firstSettings.LastPolishDate != beijingNow().Format("2006-01-02") || client.fetchItemsCalls != 1 || client.polishCalls != 0 {
		t.Fatalf("空列表首次扫描状态错误: settings=%+v fetch=%d polish=%d err=%v", firstSettings, client.fetchItemsCalls, client.polishCalls, firstSettingsErr)
	}
	center.scanAccountTasks(ctx)
	// secondSettings、secondSettingsErr 保存第二次扫描后的状态，确认成功日期阻止重复商品列表请求。
	secondSettings, secondSettingsErr := store.AccountTasks.Get(ctx, "cid")
	if secondSettingsErr != nil || secondSettings.LastPolishDate != firstSettings.LastPolishDate || client.fetchItemsCalls != 1 || client.polishCalls != 0 {
		t.Fatalf("空列表成功后不应重试: settings=%+v fetch=%d polish=%d err=%v", secondSettings, client.fetchItemsCalls, client.polishCalls, secondSettingsErr)
	}
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
	seedLocalCompletedOrders(t, store, ctx, "order-1", "order-2")
	// first、err 用于本次流程后续判断的first、err
	first, err := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if err != nil || first.Success != 2 || client.rateCalls != 2 {
		t.Fatalf("first=%+v calls=%d err=%v", first, client.rateCalls, err)
	}
	// second、err 用于本次流程后续判断的second、err
	second, err := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	// 已办完的订单不再是候选（issue #96），二次扫描既不发请求也不计入候选。
	if err != nil || second.Found != 0 || second.Skipped != 0 || client.rateCalls != 2 {
		t.Fatalf("second=%+v calls=%d err=%v", second, client.rateCalls, err)
	}
}

// TestAccountTaskRatePermanentPlatformFailureStopsFutureRequests 验证平台明确返回超期不可评价后只请求一次并永久跳过该订单。
func TestAccountTaskRatePermanentPlatformFailureStopsFutureRequests(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 自动化存储及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是自动评价永久失败测试共用的数据库上下文。
	ctx := context.Background()
	// expiredErr 保存平台明确拒绝超过 30 天订单的业务错误。
	expiredErr := &mtop.MTopResponseError{API: "mtop.taobao.idle.rate.create", Kind: mtop.MTopErrorBusiness,
		Ret: []string{"FAIL_BIZ_BAD_REQUEST::超出30天的订单不允许评价||rate failed"}}
	// client 模拟待评价列表持续返回同一超期订单，并统计评价请求次数。
	client := &fakeAccountTaskClient{pending: []mtop.PendingRateOrder{{TradeID: "expired-order"}}, rateErr: expiredErr}
	// center 是注入平台错误替身后的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	// settingsErr 保存自动评价设置写入错误。
	if settingsErr := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true,
		RateContent: "交易愉快", PolishTime: "03:00"}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	seedLocalCompletedOrders(t, store, ctx, "expired-order")
	// firstSummary、firstErr 保存首次识别永久失败后的执行结果。
	firstSummary, firstErr := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if firstErr != nil || firstSummary.Failed != 1 || client.rateCalls != 1 {
		t.Fatalf("首次超期评价结果错误: summary=%+v calls=%d err=%v", firstSummary, client.rateCalls, firstErr)
	}
	// status、message、nextRetryAt 保存永久失败运行记录的状态、原因和下一次重试时间。
	var status, message string
	// nextRetryAt 保存永久失败运行记录的下一次自动执行时间，永久失败必须保持为零。
	var nextRetryAt int64
	// queryErr 保存读取永久失败运行记录的数据库错误。
	queryErr := store.DB.QueryRowContext(ctx, `SELECT status,error_message,next_retry_at FROM account_task_runs WHERE run_key=?`,
		"rate:cid:expired-order").Scan(&status, &message, &nextRetryAt)
	if queryErr != nil {
		t.Fatal(queryErr)
	}
	if status != "failed" || !strings.HasPrefix(message, db.NoRetryErrorPrefix) || !strings.Contains(message, "超出30天的订单不允许评价") || nextRetryAt != 0 {
		t.Fatalf("永久失败运行记录错误: status=%q message=%q next_retry_at=%d", status, message, nextRetryAt)
	}
	// secondSummary、secondErr 保存后续扫描跳过永久失败订单的结果。
	secondSummary, secondErr := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	// 永久失败订单不再是候选，后续扫描不再请求评价。
	if secondErr != nil || secondSummary.Found != 0 || secondSummary.Skipped != 0 || client.rateCalls != 1 {
		t.Fatalf("超期订单后续不应再次请求评价: summary=%+v calls=%d err=%v", secondSummary, client.rateCalls, secondErr)
	}
}

// TestAccountTaskRateAlreadyRatedCountsAsSuccessAndNotRateableStopsRetry 验证平台回答“已评价”时按成功收口且不再请求，
// 回答“当前订单不能评价”时与超期同等视为永久失败。
func TestAccountTaskRateAlreadyRatedCountsAsSuccessAndNotRateableStopsRetry(t *testing.T) {
	// cases 保存两种平台业务拒绝及其对运行记录与统计的期望。
	cases := []struct {
		name        string
		ret         string
		wantStatus  string
		wantSuccess int
		wantFailed  int
	}{
		{name: "already-rated", ret: "FAIL_BIZ_BAD_REQUEST::亲，您已经评价过了||rate failed", wantStatus: "success", wantSuccess: 1},
		{name: "not-rateable", ret: "FAIL_BIZ_BAD_REQUEST::当前订单不能评价||rate failed", wantStatus: "failed", wantFailed: 1},
	}
	// testCase 是当前子测试使用的平台拒绝样本。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// store、cleanup 保存本子测试使用的 SQLite 自动化存储及关闭责任。
			store, cleanup := newAutomationTestStore(t)
			defer cleanup()
			// ctx 是本子测试共用的数据库上下文。
			ctx := context.Background()
			// rejectErr 保存平台返回的业务拒绝错误。
			rejectErr := &mtop.MTopResponseError{API: "mtop.taobao.idle.rate.create", Kind: mtop.MTopErrorBusiness, Ret: []string{testCase.ret}}
			// client 模拟评价接口持续返回同一业务拒绝，并统计评价请求次数。
			client := &fakeAccountTaskClient{rateErr: rejectErr}
			// center 是注入平台错误替身后的自动化中心。
			center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
			// settingsErr 保存自动评价设置写入错误。
			if settingsErr := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true, RateContent: "交易愉快", PolishTime: "03:00"}); settingsErr != nil {
				t.Fatal(settingsErr)
			}
			seedLocalCompletedOrders(t, store, ctx, "order-x")
			// firstSummary、firstErr 保存首次执行结果。
			firstSummary, firstErr := center.RunAccountTask(ctx, "cid", TaskAutoRate)
			if firstErr != nil || firstSummary.Success != testCase.wantSuccess || firstSummary.Failed != testCase.wantFailed || client.rateCalls != 1 {
				t.Fatalf("首次结果错误: summary=%+v calls=%d err=%v", firstSummary, client.rateCalls, firstErr)
			}
			// status、message 保存运行记录状态与原因，nextRetryAt 保存下一次自动执行时间。
			var status, message string
			// nextRetryAt 保存运行记录的下一次自动执行时间，终态必须为零。
			var nextRetryAt int64
			// queryErr 保存读取运行记录的数据库错误。
			if queryErr := store.DB.QueryRowContext(ctx, `SELECT status,error_message,next_retry_at FROM account_task_runs WHERE run_key=?`, "rate:cid:order-x").Scan(&status, &message, &nextRetryAt); queryErr != nil {
				t.Fatal(queryErr)
			}
			if status != testCase.wantStatus || nextRetryAt != 0 {
				t.Fatalf("运行记录错误: status=%q message=%q next_retry_at=%d", status, message, nextRetryAt)
			}
			if testCase.wantFailed == 1 && !strings.HasPrefix(message, db.NoRetryErrorPrefix) {
				t.Fatalf("不能评价应带永久失败前缀: %q", message)
			}
			// secondSummary、secondErr 保存后续扫描结果；终态订单不应再次请求平台。
			secondSummary, secondErr := center.RunAccountTask(ctx, "cid", TaskAutoRate)
			if secondErr != nil || client.rateCalls != 1 {
				t.Fatalf("终态后不应再次请求评价: summary=%+v calls=%d err=%v", secondSummary, client.rateCalls, secondErr)
			}
		})
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
	// logs 捕获扫描器的脱敏完成日志，验证下一轮成功状态可被观察。
	var logs bytes.Buffer
	// center 保存注入账号任务平台替身的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}},
		slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})), CenterDependencies{AccountTaskClient: client})
	// settings 保存应被扫描器发现的启用账号任务配置。
	settings := db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true, RateContent: "交易愉快", PolishTime: "03:00"}
	// saveErr 保存账号任务配置写入错误。
	if saveErr := store.AccountTasks.Upsert(ctx, settings); saveErr != nil {
		t.Fatal(saveErr)
	}
	seedLocalCompletedOrders(t, store, ctx, "scan-order")
	center.scanAccountTasks(ctx)
	if client.pendingCalls != 0 || client.rateCalls != 1 {
		t.Fatalf("扫描器不应扫描平台待评价列表 pending=%d rate=%d", client.pendingCalls, client.rateCalls)
	}
	if !strings.Contains(logs.String(), "自动评价扫描完成") || !strings.Contains(logs.String(), "success=1") {
		t.Fatalf("缺少自动评价完成日志: %s", logs.String())
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
	seedLocalCompletedOrders(t, store, ctx, "order-finish-failure")
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
	seedLocalCompletedOrders(t, store, ctx, "order-double-failure")
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
	seedLocalCompletedOrders(t, store, setupCtx, "order-cancelled")
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
	// 人工核对订单不再是候选，不会被自动重放。
	if retryErr != nil || retrySummary.Found != 0 || retrySummary.Skipped != 0 || client.rateCalls != 1 {
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
	client := &fakeAccountTaskClient{rateErr: sessionErr}
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
	seedLocalCompletedOrders(t, store, ctx, "session-order")

	if // err 用于本次流程后续判断的err
	_, err := center.RunAccountTask(ctx, "cid", TaskAutoRate); err == nil || !mtop.IsSessionExpiredErr(err) {
		t.Fatalf("首次 session 失效应触发续期并返回原始分类错误: %v", err)
	}
	if client.rateCalls != 1 || recoverer.calls != 1 {
		t.Fatalf("first calls: rate=%d recover=%d want 1/1", client.rateCalls, recoverer.calls)
	}
	if // err 用于本次流程后续判断的err
	_, err := center.RunAccountTask(ctx, "cid", TaskAutoRate); err == nil || !strings.Contains(err.Error(), "已停止自动化 API 请求") {
		t.Fatalf("未更新凭证时应保持阻断: %v", err)
	}
	if client.rateCalls != 1 || recoverer.calls != 1 {
		t.Fatalf("blocked run must not call API/recovery again: rate=%d recover=%d", client.rateCalls, recoverer.calls)
	}

	if // err 用于本次流程后续判断的err
	err := store.Cookies.UpdateValueExisting(ctx, "cid", "unb=1; _m_h5_tk=fresh_1; renewed=1"); err != nil {
		t.Fatal(err)
	}
	client.rateErr = nil
	if // err 用于本次流程后续判断的err
	_, err := center.RunAccountTask(ctx, "cid", TaskAutoRate); err != nil {
		t.Fatalf("凭证变化后应自动解除阻断: %v", err)
	}
	if client.rateCalls != 1 {
		t.Fatalf("凭证变化后失败订单仍在冷却期，不应再次评价 rate=%d", client.rateCalls)
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
	seedLocalCompletedOrders(t, store, ctx, "order-1", "order-2")

	if // err 用于本次流程后续判断的err
	_, err := center.RunAccountTask(ctx, "cid", TaskAutoRate); err == nil {
		t.Fatal("session expiry must be returned")
	}
	if client.rateCalls != 1 || recoverer.calls != 1 {
		t.Fatalf("remaining orders must stop immediately: rate=%d recover=%d", client.rateCalls, recoverer.calls)
	}
}

// TestAccountTaskStopsRemainingOrdersOnMTopTokenExpired 验证 Token 内部刷新耗尽后停止当前批次，但不恢复账号；t 管理本地 SQLite。
func TestAccountTaskStopsRemainingOrdersOnMTopTokenExpired(t *testing.T) {
	// store、cleanup 用于 Token 失效恢复测试的 SQLite 存储及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于任务与凭证恢复调用共用的上下文。
	ctx := context.Background()
	// tokenErr 模拟业务请求经过自身 Token 重试后仍无法恢复的错误。
	tokenErr := &mtop.MTopResponseError{API: "评价接口", Kind: mtop.MTopErrorTokenExpired, HTTPStatus: 200}
	// client 预置两个订单，确认第一个 Token 失效后不会继续消费第二个订单。
	client := &fakeAccountTaskClient{
		pending: []mtop.PendingRateOrder{{TradeID: "order-1"}, {TradeID: "order-2"}},
		rateErr: tokenErr,
	}
	// recoverer 记录 Token 失效后触发的统一凭证恢复。
	recoverer := &fakeCredentialRecoverer{store: store}
	// center 注入任务客户端和凭证恢复器。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{
		AccountTaskClient:  client,
		OrderDetailFetcher: recoverer,
	})
	// err 表示写入自动评价任务配置时的数据库错误。
	if err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true, RateContent: "交易愉快", PolishTime: "03:00"}); err != nil {
		t.Fatal(err)
	}
	seedLocalCompletedOrders(t, store, ctx, "order-1", "order-2")
	// err 表示运行自动评价任务后保留的凭证失效错误。
	if _, err := center.RunAccountTask(ctx, "cid", TaskAutoRate); err == nil || !mtop.IsMTopTokenExpiredErr(err) {
		t.Fatalf("Token 失效应返回原始分类错误: %v", err)
	}
	if client.rateCalls != 1 || recoverer.calls != 0 {
		t.Fatalf("Token 失效后必须停止剩余订单且不得恢复账号: rate=%d recover=%d", client.rateCalls, recoverer.calls)
	}
}

// TestAccountTaskStopsRemainingOrdersOnRiskVerification 验证一次评价动作触发风控后，当前本地批次不会继续请求其他订单。
func TestAccountTaskStopsRemainingOrdersOnRiskVerification(t *testing.T) {
	// store、cleanup 保存风控停止测试使用的 SQLite 存储及其释放函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是本轮本地订单事实和自动评价任务共用的上下文。
	ctx := context.Background()
	// riskErr 模拟平台在评价动作时要求安全验证，而非可立即刷新的普通 Token 失效。
	riskErr := &mtop.RiskVerificationError{API: "评价接口", Ret: []string{"FAIL_SYS_USER_VALIDATE::RGV587_ERROR"}}
	// client 返回风控错误，并保留调用次数以确认第二个订单未触达平台。
	client := &fakeAccountTaskClient{rateErr: riskErr}
	// recoverer 记录统一凭证恢复调用次数；风控不允许触发该恢复路径。
	recoverer := &fakeCredentialRecoverer{store: store}
	// center 注入评价客户端和凭证恢复器。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{
		AccountTaskClient:  client,
		OrderDetailFetcher: recoverer,
	})
	// settingsErr 保存自动评价任务配置写入错误。
	settingsErr := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true, RateContent: "交易愉快", PolishTime: "03:00"})
	if settingsErr != nil {
		t.Fatal(settingsErr)
	}
	seedLocalCompletedOrders(t, store, ctx, "risk-order-1", "risk-order-2")
	// summary、runErr 保存风控动作后的任务统计和原始错误。
	summary, runErr := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if !mtop.IsRiskVerificationErr(runErr) || summary.Failed != 1 {
		t.Fatalf("风控结果应保留分类且记录首单失败: summary=%+v err=%v", summary, runErr)
	}
	if client.rateCalls != 1 || recoverer.calls != 0 {
		t.Fatalf("风控后不得继续评价或续期: rate=%d recover=%d", client.rateCalls, recoverer.calls)
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

// TestPolishStopsRemainingItemsOnRiskVerification 验证擦亮遇到风控后立即停止，不再请求剩余商品。
func TestPolishStopsRemainingItemsOnRiskVerification(t *testing.T) {
	// store、cleanup 保存测试使用的 SQLite 存储及其释放函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// riskErr 模拟平台在擦亮第一件商品时要求安全验证。
	riskErr := &mtop.RiskVerificationError{API: "擦亮接口", Ret: []string{"FAIL_SYS_USER_VALIDATE::RGV587_ERROR"}}
	// client 返回风控错误并统计擦亮调用次数。
	client := &fakeAccountTaskClient{items: []mtop.ItemListItem{{ID: "item-1"}, {ID: "item-2"}, {ID: "item-3"}}, polishErr: riskErr}
	// center 注入擦亮客户端。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	// ctx 是任务共用的上下文。
	ctx := context.Background()
	// settingsErr 保存擦亮任务配置写入错误。
	settingsErr := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoPolishEnabled: true,
		RateContent: "交易愉快", PolishTime: "00:00"})
	if settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// summary、err 保存风控后的任务统计与原始错误。
	summary, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if !mtop.IsRiskVerificationErr(err) || summary.Failed != 1 || client.polishCalls != 1 {
		t.Fatalf("风控后必须停止剩余商品: summary=%+v calls=%d err=%v", summary, client.polishCalls, err)
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

// TestAccountTaskPolishEmptyItemListCompletesDay 验证成功获取空商品列表时完成当日任务且不重复请求平台。
func TestAccountTaskPolishEmptyItemListCompletesDay(t *testing.T) {
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
	if firstErr != nil || first.Found != 0 || first.Success != 0 || first.Failed != 0 || first.Skipped != 0 || client.polishCalls != 0 || first.Message == "" {
		t.Fatalf("first=%+v calls=%d err=%v", first, client.polishCalls, firstErr)
	}
	// settings、settingsErr 分别是空列表运行后的账号任务设置和读取错误；当天日期必须已经标记完成。
	settings, settingsErr := store.AccountTasks.Get(ctx, "cid")
	if settingsErr != nil || settings.LastPolishDate != beijingNow().Format("2006-01-02") || settings.LastPolishAt <= 0 {
		t.Fatalf("空商品运行必须写入擦亮日期: settings=%+v err=%v", settings, settingsErr)
	}
	// client.items 模拟商品列表恢复；同日再次执行必须被成功运行记录跳过，而不是重复擦亮。
	client.items = []mtop.ItemListItem{{ID: "item-1"}}
	// second、secondErr 分别是同日重复运行的摘要和不应出现的错误。
	second, secondErr := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if secondErr != nil || second.Skipped != 1 || client.polishCalls != 0 {
		t.Fatalf("second=%+v calls=%d err=%v", second, client.polishCalls, secondErr)
	}
}
