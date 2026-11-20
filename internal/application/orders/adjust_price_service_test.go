package orders

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// adjustPriceRepositoryFake 是订单改价应用服务测试使用的内存 repository。
type adjustPriceRepositoryFake struct {
	// ownedIDs 是测试用户拥有的账号标识。
	ownedIDs []string
	// orders 是测试订单夹具。
	orders map[string]*Order
	// listErr 是账号列表查询错误。
	listErr error
	// getErr 是订单读取错误。
	getErr error
	// upsertErr 是本地金额写入错误。
	upsertErr error
	// upsertCalls 记录本地写入次数，用于断言失败路径不写库。
	upsertCalls int
	// upsertID 是最近一次写入的订单标识。
	upsertID string
	// upsertOptions 是最近一次写入的订单字段。
	upsertOptions UpsertOptions
}

// ListOwnedIDs 返回测试用户拥有的账号标识。
func (f *adjustPriceRepositoryFake) ListOwnedIDs(context.Context, int64) ([]string, error) {
	return f.ownedIDs, f.listErr
}

// GetOrder 返回测试订单。
func (f *adjustPriceRepositoryFake) GetOrder(_ context.Context, orderID string) (*Order, error) {
	return f.orders[orderID], f.getErr
}

// UpsertOrder 记录本地金额写入。
func (f *adjustPriceRepositoryFake) UpsertOrder(_ context.Context, orderID string, options UpsertOptions) error {
	f.upsertCalls++
	f.upsertID = orderID
	f.upsertOptions = options
	return f.upsertErr
}

// adjustPriceRuntimeFake 是订单改价应用服务测试使用的平台运行时 Port。
type adjustPriceRuntimeFake struct {
	// mtopAvailable 表示平台客户端是否可用。
	mtopAvailable bool
	// call 是平台改价调用的预设结果。
	call AdjustPriceCallResult
	// calls 记录平台改价调用次数，用于断言单次操作最多一次请求。
	calls int
	// gotCookieID、gotOrderID、gotUserID、gotCents 记录最近一次调用参数。
	gotCookieID, gotOrderID string
	gotUserID, gotCents     int64
	// updatedCookie 记录同步到运行时的 Cookie。
	updatedCookie string
	// failures 记录本地持久化失败告警。
	failures []string
}

// MTopAvailable 返回预设的平台可用性。
func (f *adjustPriceRuntimeFake) MTopAvailable() bool { return f.mtopAvailable }

// AdjustPrice 记录调用参数并返回预设结果。
func (f *adjustPriceRuntimeFake) AdjustPrice(_ context.Context, cookieID, orderID string, userID, priceCents int64) AdjustPriceCallResult {
	f.calls++
	f.gotCookieID, f.gotOrderID, f.gotUserID, f.gotCents = cookieID, orderID, userID, priceCents
	return f.call
}

// UpdateRunningCookie 记录同步到运行时的 Cookie。
func (f *adjustPriceRuntimeFake) UpdateRunningCookie(_ context.Context, _, value string) {
	f.updatedCookie = value
}

// ReportPersistenceFailure 记录持久化失败告警。
func (f *adjustPriceRuntimeFake) ReportPersistenceFailure(orderID string, _ error) {
	f.failures = append(f.failures, orderID)
}

// newAdjustPriceFixture 构造一个用户拥有 acc1、订单 o1 处于未付款状态的测试夹具。
func newAdjustPriceFixture() (*AdjustPriceService, *adjustPriceRepositoryFake, *adjustPriceRuntimeFake) {
	// repository 是含未付款订单 o1 的内存订单仓储，账号 acc1 归当前用户所有。
	repository := &adjustPriceRepositoryFake{ownedIDs: []string{"acc1"}, orders: map[string]*Order{
		"o1": {OrderID: "o1", CookieID: "acc1", OrderStatus: "processing", ItemID: "item", BuyerID: "buyer", ChatID: "chat", Quantity: "2", Amount: "10.00"},
	}}
	// runtime 是默认平台可用且改价成功的运行时替身。
	runtime := &adjustPriceRuntimeFake{mtopAvailable: true, call: AdjustPriceCallResult{Success: true}}
	return NewAdjustPriceService(repository, runtime), repository, runtime
}

// validAdjustRequest 是各门禁用例共用的合法改价请求，单独某一项非法的用例在此基础上覆盖对应字段。
var validAdjustRequest = AdjustPriceRequest{UserID: 1, OrderID: "o1", Price: "5"}

// TestAdjustPriceSuccessWritesAmountAndSyncsCookie 验证成功时以整数分调用平台、写回两位小数金额并同步变化的 Cookie。
func TestAdjustPriceSuccessWritesAmountAndSyncsCookie(t *testing.T) {
	// service、repository、runtime 分别是被测服务及其仓储、运行时替身。
	service, repository, runtime := newAdjustPriceFixture()
	runtime.call = AdjustPriceCallResult{Success: true, RuntimeCookie: "fresh", RuntimeCookieChanged: true}
	// result、err 是改价结果与错误；订单号带空白以验证被裁剪。
	result, err := service.AdjustPrice(context.Background(), AdjustPriceRequest{UserID: 7, OrderID: " o1 ", Price: "8.5"})
	if err != nil {
		t.Fatalf("改价应成功: %v", err)
	}
	if result.Status != "succeeded" || result.Amount != "8.50" || result.OrderID != "o1" || !strings.Contains(result.Message, "8.50") {
		t.Fatalf("结果=%+v", result)
	}
	if runtime.calls != 1 || runtime.gotCents != 850 || runtime.gotCookieID != "acc1" || runtime.gotOrderID != "o1" || runtime.gotUserID != 7 {
		t.Fatalf("平台调用参数=%+v", runtime)
	}
	if runtime.updatedCookie != "fresh" {
		t.Fatalf("变化的 Cookie 应同步到运行时: %q", runtime.updatedCookie)
	}
	if repository.upsertID != "o1" || repository.upsertOptions.Amount != "8.50" || repository.upsertOptions.OrderStatus != "processing" || repository.upsertOptions.Quantity != "2" || repository.upsertOptions.ChatID != "chat" {
		t.Fatalf("本地写入=%+v", repository.upsertOptions)
	}
}

// TestAdjustPriceAcceptsPlatformPendingAliases 验证旧数字状态码 1 同样视为未付款。
func TestAdjustPriceAcceptsPlatformPendingAliases(t *testing.T) {
	// service、repository 是被测服务及其订单仓储，订单状态被改写为历史数字别名 1。
	service, repository, _ := newAdjustPriceFixture()
	repository.orders["o1"].OrderStatus = "1"
	if // err 是本次调用返回的错误，用于断言预期的成功或失败分支。
	_, err := service.AdjustPrice(context.Background(), AdjustPriceRequest{UserID: 1, OrderID: "o1", Price: "1"}); err != nil {
		t.Fatalf("数字状态 1 应可改价: %v", err)
	}
	if repository.upsertOptions.OrderStatus != "processing" {
		t.Fatalf("写回状态应归一为 processing: %q", repository.upsertOptions.OrderStatus)
	}
}

// TestAdjustPriceRejectsInvalidPrice 验证非法金额在任何读取和平台调用之前被拒绝。
func TestAdjustPriceRejectsInvalidPrice(t *testing.T) {
	// price 是被拒绝的非法金额文本：空、零、负数、非数字、超过两位小数、缺整数或小数部分、科学计数法和溢出。
	for _, price := range []string{"", "0", "0.00", "-1", "abc", "1.234", "1.", ".5", "1e3", "99999999999999999999"} {
		// service、repository、runtime 是每个金额独立的服务与替身，用于断言未触达外部。
		service, repository, runtime := newAdjustPriceFixture()
		// err 是非法金额应得到的错误。
		_, err := service.AdjustPrice(context.Background(), AdjustPriceRequest{UserID: 1, OrderID: "o1", Price: price})
		if !errors.Is(err, ErrInvalidPrice) {
			t.Fatalf("price=%q err=%v，应为 ErrInvalidPrice", price, err)
		}
		if runtime.calls != 0 || repository.upsertCalls != 0 {
			t.Fatalf("price=%q 不应触达平台或写库", price)
		}
	}
}

// TestAdjustPriceDependencyAndLookupGuards 验证依赖缺失、订单号为空、账号列表失败、读取失败和订单不存在的门禁。
func TestAdjustPriceDependencyAndLookupGuards(t *testing.T) {
	if // err 是本次调用返回的错误，用于断言预期的成功或失败分支。
	_, err := (*AdjustPriceService)(nil).AdjustPrice(context.Background(), validAdjustRequest); err == nil {
		t.Fatal("空服务应返回错误")
	}
	if // err 是本次调用返回的错误，用于断言预期的成功或失败分支。
	_, err := NewAdjustPriceService(nil, nil).AdjustPrice(context.Background(), validAdjustRequest); err == nil {
		t.Fatal("缺少依赖应返回错误")
	}

	// service 是用于空订单号用例的默认夹具服务。
	service, _, _ := newAdjustPriceFixture()
	if // err 是本次调用返回的错误，用于断言预期的成功或失败分支。
	_, err := service.AdjustPrice(context.Background(), AdjustPriceRequest{UserID: 1, OrderID: "  ", Price: "5"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("空订单号 err=%v", err)
	}

	// listFailService、listFailRepository 用于账号列表查询失败的用例。
	listFailService, listFailRepository, _ := newAdjustPriceFixture()
	listFailRepository.listErr = errors.New("db down")
	if // err 是本次调用返回的错误，用于断言预期的成功或失败分支。
	_, err := listFailService.AdjustPrice(context.Background(), validAdjustRequest); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("账号列表失败应原样返回: %v", err)
	}

	// getFailService、getFailRepository 用于订单读取失败的用例。
	getFailService, getFailRepository, _ := newAdjustPriceFixture()
	getFailRepository.getErr = errors.New("db down")
	if // err 是本次调用返回的错误，用于断言预期的成功或失败分支。
	_, err := getFailService.AdjustPrice(context.Background(), validAdjustRequest); !errors.Is(err, ErrNotFound) {
		t.Fatalf("读取失败应视为不存在: %v", err)
	}

	if // err 是本次调用返回的错误，用于断言预期的成功或失败分支。
	_, err := service.AdjustPrice(context.Background(), AdjustPriceRequest{UserID: 1, OrderID: "missing", Price: "5"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("订单不存在 err=%v", err)
	}
}

// TestAdjustPriceOwnershipStatusAndAvailabilityGuards 验证越权、非未付款状态和平台未装配时不触达平台。
func TestAdjustPriceOwnershipStatusAndAvailabilityGuards(t *testing.T) {
	// foreignService、foreignRepository、foreignRuntime 用于订单属于他人账号的用例。
	foreignService, foreignRepository, foreignRuntime := newAdjustPriceFixture()
	foreignRepository.ownedIDs = []string{"other"}
	if // err 是本次调用返回的错误，用于断言预期的成功或失败分支。
	_, err := foreignService.AdjustPrice(context.Background(), validAdjustRequest); !errors.Is(err, ErrForbidden) {
		t.Fatalf("越权 err=%v", err)
	}
	if foreignRuntime.calls != 0 {
		t.Fatal("越权不得触达平台")
	}

	// status 是不允许改价的订单状态：待发货、已发货、已完成、已取消、退款中和未知。
	for _, status := range []string{"pending_ship", "shipped", "completed", "cancelled", "refunding", "unknown"} {
		// service、repository、runtime 是每个状态独立的服务与替身。
		service, repository, runtime := newAdjustPriceFixture()
		repository.orders["o1"].OrderStatus = status
		if // err 是本次调用返回的错误，用于断言预期的成功或失败分支。
		_, err := service.AdjustPrice(context.Background(), validAdjustRequest); !errors.Is(err, ErrOrderNotAwaitingPayment) {
			t.Fatalf("status=%s err=%v", status, err)
		}
		if runtime.calls != 0 {
			t.Fatalf("status=%s 不得触达平台", status)
		}
	}

	// unavailableService、unavailableRuntime 用于平台客户端未装配的用例。
	unavailableService, _, unavailableRuntime := newAdjustPriceFixture()
	unavailableRuntime.mtopAvailable = false
	if // err 是本次调用返回的错误，用于断言预期的成功或失败分支。
	_, err := unavailableService.AdjustPrice(context.Background(), validAdjustRequest); !errors.Is(err, ErrPriceAdjustUnavailable) {
		t.Fatalf("平台未装配 err=%v", err)
	}
}

// TestAdjustPriceRemoteFailures 验证平台失败不写本地金额，并按错误、业务提示、空原因三种形态返回拒绝错误。
func TestAdjustPriceRemoteFailures(t *testing.T) {
	// cases 是三种平台失败形态：调用错误、业务提示列表、无任何原因。
	cases := []struct {
		// name 是用例名称，仅用于失败输出。
		name string
		// call 是运行时返回的平台失败结果。
		call AdjustPriceCallResult
		// want 是错误文本应包含的原因；为空表示应返回不带原因的裸错误。
		want string
	}{
		{"调用错误", AdjustPriceCallResult{Err: errors.New("平台拒绝")}, "平台拒绝"},
		{"业务提示", AdjustPriceCallResult{Messages: []string{"FAIL_A", "FAIL_B"}}, "FAIL_A; FAIL_B"},
		{"无原因", AdjustPriceCallResult{}, ""},
	}
	// tc 是当前待验证的平台失败用例。
	for _, tc := range cases {
		// service、repository、runtime 是每个用例独立的服务与替身。
		service, repository, runtime := newAdjustPriceFixture()
		runtime.call = tc.call
		// err 是平台失败应转换成的拒绝错误。
		_, err := service.AdjustPrice(context.Background(), validAdjustRequest)
		if !errors.Is(err, ErrPriceAdjustRejected) {
			t.Fatalf("%s err=%v", tc.name, err)
		}
		if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s 错误应包含 %q: %v", tc.name, tc.want, err)
		}
		if tc.want == "" && err.Error() != ErrPriceAdjustRejected.Error() {
			t.Fatalf("%s 无原因时应为裸错误: %v", tc.name, err)
		}
		if repository.upsertCalls != 0 {
			t.Fatalf("%s 平台失败不得写本地金额", tc.name)
		}
	}
}

// TestAdjustPriceForbiddenFromRuntimeAndCookieSyncOnFailure 验证运行时报告越权时映射为 ErrForbidden，且失败时变化的 Cookie 仍被同步。
func TestAdjustPriceForbiddenFromRuntimeAndCookieSyncOnFailure(t *testing.T) {
	// service、runtime 是被测服务与运行时替身，替身返回越权错误并带有变化的 Cookie。
	service, _, runtime := newAdjustPriceFixture()
	runtime.call = AdjustPriceCallResult{Err: errors.Join(errors.New("x"), ErrForbidden), RuntimeCookie: "c2", RuntimeCookieChanged: true}
	if // err 是本次调用返回的错误，用于断言预期的成功或失败分支。
	_, err := service.AdjustPrice(context.Background(), validAdjustRequest); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err=%v", err)
	}
	if runtime.updatedCookie != "c2" {
		t.Fatalf("失败时变化的 Cookie 也必须同步: %q", runtime.updatedCookie)
	}
}

// TestAdjustPriceLocalWriteFailureIsReportedNotRetried 验证远端成功而本地写入失败时返回待同步状态、告警一次且不重复调用平台。
func TestAdjustPriceLocalWriteFailureIsReportedNotRetried(t *testing.T) {
	// service、repository、runtime 是被测服务及替身，仓储写入会失败。
	service, repository, runtime := newAdjustPriceFixture()
	repository.upsertErr = errors.New("disk full")
	// result、err 是远端成功但本地写入失败时的结果与错误。
	result, err := service.AdjustPrice(context.Background(), AdjustPriceRequest{UserID: 1, OrderID: "o1", Price: "3.05"})
	if err != nil {
		t.Fatalf("远端已成功时不应返回错误: %v", err)
	}
	if result.Status != "local_sync_pending" || result.Amount != "3.05" || !strings.Contains(result.Message, "请勿重复改价") {
		t.Fatalf("结果=%+v", result)
	}
	if runtime.calls != 1 || len(runtime.failures) != 1 || runtime.failures[0] != "o1" {
		t.Fatalf("平台调用=%d 告警=%v", runtime.calls, runtime.failures)
	}
}

// TestFormatCents 验证整数分到元文本的固定两位小数格式化。
func TestFormatCents(t *testing.T) {
	// cents、want 是输入的整数分和期望的元文本。
	for cents, want := range map[int64]string{1: "0.01", 100: "1.00", 850: "8.50", 123456: "1234.56"} {
		// got 是实际格式化结果。
		got := FormatCents(cents)
		if got != want {
			t.Fatalf("FormatCents(%d)=%q want %q", cents, got, want)
		}
	}
}
