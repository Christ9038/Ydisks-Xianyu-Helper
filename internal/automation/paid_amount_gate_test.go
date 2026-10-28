package automation

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

// seedOrderWithAmount 写入待发货订单事实，amount 为空时不写入金额，用于模拟服务预约单与缺金额订单。
func (f paidGateFixture) seedOrderWithAmount(t *testing.T, orderID, amount string) {
	t.Helper()
	// opts 是订单夹具字段，规格与规则动作保持一致，只有金额由调用方控制。
	opts := db.OrderUpsertOpts{ItemID: "gate-item", BuyerID: "gate-buyer", CookieID: "gate-acc", ChatID: "gate-chat", OrderStatus: "pending_ship", Quantity: "1", Amount: amount, SpecName: "颜色", SpecValue: "黑"}
	// err 保存订单写入错误，非空时测试立即失败。
	if err := f.store.Orders.Upsert(context.Background(), orderID, opts); err != nil {
		t.Fatalf("写入订单夹具失败: %v", err)
	}
}

// withDetailFetcher 返回使用指定订单详情查询器的夹具副本，沿用同一数据库、规则和发送器。
func (f paidGateFixture) withDetailFetcher(fetcher OrderDetailFetcher) paidGateFixture {
	f.center = NewWithDependencies(f.store, testSenderProvider{sender: f.sender}, nil, CenterDependencies{OrderDetailFetcher: fetcher})
	return f
}

// TestPaidAmountBlockedReason 验证金额门禁只放行严格正数，零金额、非法金额与空金额分别返回对应原因。
func TestPaidAmountBlockedReason(t *testing.T) {
	// cases 覆盖正数、零、非法与空金额的边界。
	cases := []struct {
		// name 是用例名称。
		name string
		// amount 是输入金额文本。
		amount string
		// want 是期望的拦截原因，空串表示放行。
		want string
	}{
		{"正金额放行", "9.90", ""},
		{"带货币符号放行", "¥12.50", ""},
		{"千位分隔放行", "1,234.00", ""},
		{"最小正金额放行", "0.01", ""},
		{"零金额", "0.00", reasonOrderAmountNotPositive},
		{"整数零", "0", reasonOrderAmountNotPositive},
		{"带符号零", "¥0.00", reasonOrderAmountNotPositive},
		{"负金额", "-1.00", reasonOrderAmountNotPositive},
		{"非法金额", "abc", reasonOrderAmountNotPositive},
		{"空金额", "  ", reasonOrderAmountMissing},
	}
	// testCase 是当前待验证的金额文本。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// got 保存被测函数对该金额文本返回的拦截原因。
			if got := paidAmountBlockedReason(testCase.amount); got != testCase.want {
				t.Fatalf("paidAmountBlockedReason(%q)=%q want %q", testCase.amount, got, testCase.want)
			}
		})
	}
}

// TestPaidEventSkipsNonPositiveAmountOrders 验证待发货 + 卖家角色 + 零金额的预约单不创建运行、不发卡，正金额订单保持原行为。
func TestPaidEventSkipsNonPositiveAmountOrders(t *testing.T) {
	// cases 覆盖零金额与正金额两种本地订单事实。
	cases := []struct {
		// name 是用例名称。
		name string
		// amount 是订单金额。
		amount string
		// wantDelivered 表示是否应发卡。
		wantDelivered bool
	}{
		{"零金额预约单", "0.00", false},
		{"带符号零金额", "¥0.00", false},
		{"正金额订单", "9.90", true},
	}
	// testCase 是当前待验证的订单金额。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// cleanup、fixture 分别保存：测试数据库的关闭函数；带账号、规则与假发送器的门禁测试夹具。
			fixture, cleanup := newPaidGateFixture(t)
			defer cleanup()
			fixture.seedOrderWithAmount(t, "amount-order", testCase.amount)
			// err 保存付款事件处理结果，金额不合格时应为空表示安全跳过。
			if err := fixture.center.HandleTask(context.Background(), paidTask("amount-order")); err != nil {
				t.Fatalf("金额门禁应安全跳过而不是报错: %v", err)
			}
			// delivered 表示是否创建了运行并发出内容。
			delivered := fixture.runCount(t, "amount-order") == 1 && len(fixture.sender.texts) > 0
			if delivered != testCase.wantDelivered {
				t.Fatalf("delivered=%v want %v: runs=%d texts=%v", delivered, testCase.wantDelivered, fixture.runCount(t, "amount-order"), fixture.sender.texts)
			}
			if !testCase.wantDelivered && (fixture.runCount(t, "amount-order") != 0 || len(fixture.sender.texts) != 0) {
				t.Fatalf("零金额订单不得创建运行或发卡: runs=%d texts=%v", fixture.runCount(t, "amount-order"), fixture.sender.texts)
			}
		})
	}
}

// TestPaidEventWithEmptyAmountFetchesDetailThenDecides 验证金额为空时先查询订单详情：详情仍为空或为零则跳过，详情为正数则继续发货。
func TestPaidEventWithEmptyAmountFetchesDetailThenDecides(t *testing.T) {
	// cases 覆盖详情补全后的金额结果。
	cases := []struct {
		// name 是用例名称。
		name string
		// detail 是假详情查询器返回的订单详情。
		detail *OrderDetail
		// wantDelivered 表示是否应发卡。
		wantDelivered bool
	}{
		{"详情仍为空", &OrderDetail{Quantity: "1", SpecName: "颜色", SpecValue: "黑"}, false},
		{"详情为零", &OrderDetail{Quantity: "1", SpecName: "颜色", SpecValue: "黑", Amount: "0.00"}, false},
		{"详情为非法金额", &OrderDetail{Quantity: "1", SpecName: "颜色", SpecValue: "黑", Amount: "abc"}, false},
		{"详情为正数", &OrderDetail{Quantity: "1", SpecName: "颜色", SpecValue: "黑", Amount: "9.90"}, true},
	}
	// testCase 是当前待验证的详情结果。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// cleanup、fixture 分别保存：测试数据库的关闭函数；带账号、规则与假发送器的门禁测试夹具。
			base, cleanup := newPaidGateFixture(t)
			defer cleanup()
			// calls 记录详情查询次数，空金额必须触发查询。
			calls := 0
			// fixture 是挂载了假详情查询器的夹具副本。
			fixture := base.withDetailFetcher(testFetcher{detail: testCase.detail, calls: &calls})
			fixture.seedOrderWithAmount(t, "empty-amount-order", "")
			// task 是带测试 Cookie 的付款事件，避免详情查询阶段读取账号凭证。
			task := paidTask("empty-amount-order")
			task.CookieStr = "unb=123; _m_h5_tk=tk_1;"
			// err 保存付款事件处理结果，补全后金额不合格时应为空表示安全跳过。
			if err := fixture.center.HandleTask(context.Background(), task); err != nil {
				t.Fatalf("补全后金额不合格应安全跳过而不是报错: %v", err)
			}
			if calls == 0 {
				t.Fatal("金额为空时必须先查询订单详情")
			}
			// delivered 表示是否创建了运行并发出内容。
			delivered := fixture.runCount(t, "empty-amount-order") == 1 && len(fixture.sender.texts) > 0
			if delivered != testCase.wantDelivered {
				t.Fatalf("delivered=%v want %v: runs=%d texts=%v", delivered, testCase.wantDelivered, fixture.runCount(t, "empty-amount-order"), fixture.sender.texts)
			}
		})
	}
}

// TestResumedPaidRunWithZeroAmountStopsWithoutSending 验证历史运行遇到零金额：零进度运行被取消，已有发送进度的运行转人工核对，且都不再发送内容。
func TestResumedPaidRunWithZeroAmountStopsWithoutSending(t *testing.T) {
	// cases 覆盖零进度与已有发送进度两种历史运行。
	cases := []struct {
		// name 是用例名称。
		name string
		// progress 表示运行是否已有发送进度。
		progress bool
		// want 是运行期望的最终状态。
		want string
	}{
		{"零进度运行取消", false, "canceled"},
		{"已有进度转人工核对", true, "needs_review"},
	}
	// testCase 是当前待验证的运行进度。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// cleanup、fixture 分别保存：测试数据库的关闭函数；带账号、规则与假发送器的门禁测试夹具。
			fixture, cleanup := newPaidGateFixture(t)
			defer cleanup()
			// ctx 限定本测试内的同步调用。
			ctx := context.Background()
			fixture.seedOrderWithAmount(t, "zero-resume", "0.00")
			// runID 是历史运行中记录。
			runID := catchupRun(t, fixture.store, fixture.ruleID, "gate-acc", "zero-resume", "{}")
			if testCase.progress {
				// err 保存构造发送进度的数据库错误。
				if _, err := fixture.store.DB.ExecContext(ctx, `UPDATE automation_runs SET action_cursor=1,sent_count=1 WHERE id=?`, runID); err != nil {
					t.Fatal(err)
				}
			}
			// task 携带运行 ID，模拟恢复链路继续执行历史运行。
			task := paidTask("zero-resume")
			task.Raw = map[string]any{"automation_run_id": runID}
			// err 保存恢复任务处理结果，零金额时应为空表示安全收口。
			if err := fixture.center.HandleTask(ctx, task); err != nil {
				t.Fatalf("应安全收口: %v", err)
			}
			// run、getErr 是处理后的运行状态。
			run, getErr := fixture.store.Automation.GetRun(ctx, runID)
			if getErr != nil || run.Status != testCase.want {
				t.Fatalf("运行状态应为 %s: run=%+v err=%v", testCase.want, run, getErr)
			}
			if testCase.progress && run.SentCount != 1 {
				t.Fatalf("人工核对应保留发送计数: %+v", run)
			}
			if len(fixture.sender.texts) != 0 {
				t.Fatalf("不应再次发送内容: %v", fixture.sender.texts)
			}
		})
	}
}
