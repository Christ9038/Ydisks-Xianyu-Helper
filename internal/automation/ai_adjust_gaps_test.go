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

// aiPricingGapFixture 汇总 AI 自动改价测试所需的数据库、中心与订单创建任务。
type aiPricingGapFixture struct {
	// store 是隔离测试数据库，测试通过它制造表级故障并核对报价终态。
	store *db.Store
	// center 是注入平台改价替身的自动化中心。
	center *Center
	// task 是与待领取报价完全匹配的买家拍下订单事件。
	task Task
}

// newAIPricingGapFixture 开启账号 AI 议价与真实改价开关，写入一条 9.90 元待领取报价，并用 fake 作为平台客户端。
func newAIPricingGapFixture(t *testing.T, fake *fakeMTop) aiPricingGapFixture {
	t.Helper()
	// store、cleanup 是隔离数据库及其关闭函数。
	store, cleanup := newAutomationTestStore(t)
	t.Cleanup(cleanup)
	// ctx 是夹具写入共用的上下文。
	ctx := context.Background()
	// settingsErr 是读取账号自动免拼等开关时的错误，用于断言读取失败不放行外部动作。
	if settingsErr := store.AIReply.UpsertSettings(ctx, "cid", db.AIReplySettings{AIEnabled: true, AutoAdjustPriceEnabled: true, MaxDiscountPercent: 10, MaxDiscountAmount: 20, MaxBargainRounds: 3, AIMode: "bargain_only"}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// quote 是发送给买家后等待订单创建消费的报价。
	quote := db.AIBargainQuote{CookieID: "cid", ChatID: "chat-gap", BuyerID: "buyer-gap", ItemID: "item-gap", PriceCents: 990}
	// quoteErr 是写入或读取 AI 报价夹具时的数据库错误。
	if quoteErr := store.AIReply.ReplacePendingQuote(ctx, quote, time.Now().Add(time.Hour).Unix()); quoteErr != nil {
		t.Fatal(quoteErr)
	}
	return aiPricingGapFixture{
		store:  store,
		center: NewWithDependencies(store, nil, nil, CenterDependencies{MTop: fake}),
		task:   Task{AccountID: "cid", TriggerType: TriggerOrderCreated, OrderID: "order-gap", ChatID: "chat-gap", BuyerID: "buyer-gap", ItemID: "item-gap"},
	}
}

// TestAIPricingModeGapBranches 覆盖 AI 报价改价的模式读取、事实不足、领取、折算越界与各终态收口分支。
func TestAIPricingModeGapBranches(t *testing.T) {
	// ctx 是调用共用的上下文。
	ctx := context.Background()
	// quoteAbort 让报价收口到任何终态的写入失败，模拟平台已响应但本地状态保存失败。
	const quoteAbort = `CREATE TRIGGER gap_abort_quote BEFORE UPDATE ON ai_bargain_quotes WHEN NEW.status IN ('adjusted','failed','needs_review') BEGIN SELECT RAISE(ABORT, 'forced quote finish failure'); END`

	t.Run("模式读取失败", func(t *testing.T) {
		// f 保存夹具。
		f := newAIPricingGapFixture(t, &fakeMTop{})
		execGapSQL(t, f.store, `ALTER TABLE ai_reply_settings RENAME TO ai_reply_settings_gap_hidden`)
		// active 表示 AI 议价模式是否接管了本次订单创建事件（接管后不再执行固定价格规则）；modeErr 是读取 AI 议价模式开关时的错误，用于断言读取失败不会误判为未接管。
		if active, modeErr := f.center.handleAIPricingMode(ctx, f.task); active || modeErr == nil {
			t.Fatalf("active=%v err=%v", active, modeErr)
		}
	})
	t.Run("真实改价开启但订单事实不足", func(t *testing.T) {
		// f 保存夹具；任务缺少商品号。
		f := newAIPricingGapFixture(t, &fakeMTop{})
		// incomplete 是缺少商品号的订单创建事件。
		incomplete := f.task
		incomplete.ItemID = ""
		// active 表示 AI 议价模式是否接管了本次订单创建事件（接管后不再执行固定价格规则）；factErr 是订单事实不足场景下的处理错误，应为空（只记录日志并接管）。
		if active, factErr := f.center.handleAIPricingMode(ctx, incomplete); !active || factErr != nil {
			t.Fatalf("active=%v err=%v", active, factErr)
		}
	})
	t.Run("领取报价失败", func(t *testing.T) {
		// f 保存夹具。
		f := newAIPricingGapFixture(t, &fakeMTop{})
		execGapSQL(t, f.store, `ALTER TABLE ai_bargain_quotes RENAME TO ai_bargain_quotes_gap_hidden`)
		// active 表示 AI 议价模式是否接管了本次订单创建事件（接管后不再执行固定价格规则）；claimErr 是领取 AI 报价或阶段执行权时的错误，用于断言领取失败不执行外部动作。
		if active, claimErr := f.center.handleAIPricingMode(ctx, f.task); !active || claimErr == nil || !strings.Contains(claimErr.Error(), "领取 AI 自动改价报价") {
			t.Fatalf("active=%v err=%v", active, claimErr)
		}
	})
	t.Run("数量折算后超出平台金额上限且收口失败", func(t *testing.T) {
		// f 保存夹具；把报价改成接近上限的单价。
		f := newAIPricingGapFixture(t, &fakeMTop{})
		execGapSQL(t, f.store, `UPDATE ai_bargain_quotes SET price_cents=60000000`)
		execGapSQL(t, f.store, quoteAbort)
		// bulk 是购买 3 件的订单，折算后超过 1 亿分。
		bulk := f.task
		bulk.Quantity = "3"
		// active 表示 AI 议价模式是否接管了本次订单创建事件（接管后不再执行固定价格规则）；finishErr 是报价终态收口失败的错误，用于断言远端已成功时按结果不明处理。
		if active, finishErr := f.center.handleAIPricingMode(ctx, bulk); !active || finishErr == nil || !strings.Contains(finishErr.Error(), "forced quote finish failure") {
			t.Fatalf("active=%v err=%v", active, finishErr)
		}
	})
	t.Run("平台明确拒绝记为失败", func(t *testing.T) {
		// fake 返回业务拒绝。
		fake := &fakeMTop{adjustRet: []string{"FAIL_BIZ_ORDER_CLOSED::订单已关闭"}}
		// f 保存夹具。
		f := newAIPricingGapFixture(t, fake)
		// active 表示 AI 议价模式是否接管了本次订单创建事件（接管后不再执行固定价格规则）；adjustErr 是订单改价（含暂时性重试）的最终结果，用于决定 AI 报价的终态。
		if active, adjustErr := f.center.handleAIPricingMode(ctx, f.task); !active || adjustErr == nil {
			t.Fatalf("active=%v err=%v", active, adjustErr)
		}
		// status 是报价终态。
		var status string
		// scanErr 是读取数据库统计值时的扫描错误，应为空以保证断言可信。
		if scanErr := f.store.DB.QueryRowContext(ctx, `SELECT status FROM ai_bargain_quotes WHERE order_id='order-gap'`).Scan(&status); scanErr != nil || status != "failed" {
			t.Fatalf("status=%q err=%v", status, scanErr)
		}
	})
	t.Run("传输中断记为人工核对", func(t *testing.T) {
		// fake 返回无法分类的传输错误。
		fake := &fakeMTop{adjustErr: errors.New("连接中断")}
		// f 保存夹具。
		f := newAIPricingGapFixture(t, fake)
		// adjustErr 是订单改价（含暂时性重试）的最终结果，用于决定 AI 报价的终态。
		if _, adjustErr := f.center.handleAIPricingMode(ctx, f.task); adjustErr == nil {
			t.Fatal("应返回不确定错误")
		}
		// status 是报价终态。
		var status string
		// scanErr 是读取数据库统计值时的扫描错误，应为空以保证断言可信。
		if scanErr := f.store.DB.QueryRowContext(ctx, `SELECT status FROM ai_bargain_quotes WHERE order_id='order-gap'`).Scan(&status); scanErr != nil || status != "needs_review" {
			t.Fatalf("status=%q err=%v", status, scanErr)
		}
	})
	t.Run("改价成功但本地状态保存失败", func(t *testing.T) {
		// fake 返回成功。
		fake := &fakeMTop{adjustOk: true, adjustRet: []string{"SUCCESS::ok"}}
		// f 保存夹具。
		f := newAIPricingGapFixture(t, fake)
		execGapSQL(t, f.store, quoteAbort)
		// _, finishErr 必须被标记为结果不确定。
		_, finishErr := f.center.handleAIPricingMode(ctx, f.task)
		// uncertain 用于识别不确定错误类型。
		var uncertain *uncertainActionError
		if !errors.As(finishErr, &uncertain) {
			t.Fatalf("err=%v", finishErr)
		}
	})
	t.Run("改价失败且状态保存失败", func(t *testing.T) {
		// fake 返回业务拒绝。
		fake := &fakeMTop{adjustRet: []string{"FAIL_BIZ_ORDER_CLOSED::订单已关闭"}}
		// f 保存夹具。
		f := newAIPricingGapFixture(t, fake)
		execGapSQL(t, f.store, quoteAbort)
		// joinedErr 是改价失败与状态保存失败合并后的错误，用于断言两类原因都被保留。
		if _, joinedErr := f.center.handleAIPricingMode(ctx, f.task); joinedErr == nil || !strings.Contains(joinedErr.Error(), "保存 AI 报价改价状态") {
			t.Fatalf("err=%v", joinedErr)
		}
	})
}

// TestAdjustOrderPriceAttemptGapBranches 覆盖订单改价单次尝试的会话、凭证恢复、错误分类与响应凭证保存分支。
func TestAdjustOrderPriceAttemptGapBranches(t *testing.T) {
	// ctx 是调用共用的上下文。
	ctx := context.Background()
	// task 是账号 cid 的改价任务。
	task := Task{AccountID: "cid", OrderID: "adjust-gap"}
	// cookieAbort 让账号 Cookie 写回失败。
	const cookieAbort = `CREATE TRIGGER gap_abort_cookie BEFORE UPDATE ON cookies BEGIN SELECT RAISE(ABORT, 'forced cookie failure'); END`
	// newCenter 创建带指定平台替身与可选恢复器的中心。
	newCenter := func(t *testing.T, fake *fakeMTop, recoverFail *bool) (*Center, *db.Store, *fakeCredentialRecoverer) {
		t.Helper()
		// store、cleanup 是隔离数据库及关闭函数。
		store, cleanup := newAutomationTestStore(t)
		t.Cleanup(cleanup)
		// deps 是待注入依赖。
		deps := CenterDependencies{MTop: fake}
		// recoverer 是可选凭证恢复替身。
		var recoverer *fakeCredentialRecoverer
		if recoverFail != nil {
			recoverer = &fakeCredentialRecoverer{store: store, fail: *recoverFail}
			deps.OrderDetailFetcher = recoverer
		}
		return NewWithDependencies(store, nil, nil, deps), store, recoverer
	}
	// expired 是平台返回的明确会话失效结果。
	expired := fakeAdjustPriceResult{ret: []string{"FAIL_SYS_SESSION_EXPIRED::x"}}

	t.Run("账号没有凭证", func(t *testing.T) {
		// center 保存中心。
		center, _, _ := newCenter(t, &fakeMTop{}, nil)
		// ghost 是不存在的账号。
		ghost := task
		ghost.AccountID = "ghost"
		// attemptErr 是单次平台调用尝试（含凭证恢复与响应凭证保存）的最终错误，用于断言分类与是否禁止自动重试。
		if attemptErr := center.actions.adjustOrderPriceAttempt(ctx, ghost, 100, true); attemptErr == nil {
			t.Fatal("无凭证应报错")
		}
	})
	t.Run("会话失效且响应凭证保存失败", func(t *testing.T) {
		// center、store 保存中心与数据库。
		center, store, _ := newCenter(t, &fakeMTop{adjustResults: []fakeAdjustPriceResult{{ret: expired.ret, updated: "unb=123; _m_h5_tk=zz;"}}}, nil)
		execGapSQL(t, store, cookieAbort)
		// attemptErr 是单次平台调用尝试（含凭证恢复与响应凭证保存）的最终错误，用于断言分类与是否禁止自动重试。
		if attemptErr := center.actions.adjustOrderPriceAttempt(ctx, task, 100, true); attemptErr == nil || !strings.Contains(attemptErr.Error(), "forced cookie failure") {
			t.Fatalf("err=%v", attemptErr)
		}
	})
	t.Run("恢复后仍失效", func(t *testing.T) {
		// recoverFail 表示恢复器会成功。
		recoverFail := false
		// center、recoverer 保存中心与恢复替身。
		center, _, recoverer := newCenter(t, &fakeMTop{adjustResults: []fakeAdjustPriceResult{expired, expired}}, &recoverFail)
		// attemptErr 是单次平台调用尝试（含凭证恢复与响应凭证保存）的最终错误，用于断言分类与是否禁止自动重试。
		if attemptErr := center.actions.adjustOrderPriceAttempt(ctx, task, 100, true); !errors.Is(attemptErr, errActionNotPerformed) || !strings.Contains(attemptErr.Error(), "恢复后仍返回") {
			t.Fatalf("err=%v", attemptErr)
		}
		if recoverer.calls != 1 {
			t.Fatalf("恢复调用次数=%d", recoverer.calls)
		}
	})
	t.Run("凭证恢复失败", func(t *testing.T) {
		// recoverFail 表示恢复器会失败。
		recoverFail := true
		// center 保存中心。
		center, _, _ := newCenter(t, &fakeMTop{adjustResults: []fakeAdjustPriceResult{expired}}, &recoverFail)
		// attemptErr 是单次平台调用尝试（含凭证恢复与响应凭证保存）的最终错误，用于断言分类与是否禁止自动重试。
		if attemptErr := center.actions.adjustOrderPriceAttempt(ctx, task, 100, true); !errors.Is(attemptErr, errActionNotPerformed) || !strings.Contains(attemptErr.Error(), "恢复失败") {
			t.Fatalf("err=%v", attemptErr)
		}
	})
	t.Run("平台业务错误禁止重试", func(t *testing.T) {
		// businessErr 是平台明确的普通业务错误。
		businessErr := &mtop.MTopResponseError{API: "改价", Kind: mtop.MTopErrorBusiness, Ret: []string{"FAIL_BIZ_X::拒绝"}}
		// center 保存中心。
		center, _, _ := newCenter(t, &fakeMTop{adjustErr: businessErr}, nil)
		// attemptErr 保存尝试结果。
		attemptErr := center.actions.adjustOrderPriceAttempt(ctx, task, 100, true)
		if attemptErr == nil {
			t.Fatalf("err=%v", attemptErr)
		}
	})
	t.Run("平台系统错误保留可恢复", func(t *testing.T) {
		// systemErr 是平台暂时性系统错误。
		systemErr := &mtop.MTopResponseError{API: "改价", Kind: mtop.MTopErrorSystem, Ret: []string{"FAIL_SYS_BUSY::繁忙"}}
		// center 保存中心。
		center, _, _ := newCenter(t, &fakeMTop{adjustErr: systemErr}, nil)
		// attemptErr 保存尝试结果，不应是不确定错误。
		attemptErr := center.actions.adjustOrderPriceAttempt(ctx, task, 100, true)
		// uncertain 用于确认它不是不确定错误。
		var uncertain *uncertainActionError
		if attemptErr == nil || errors.As(attemptErr, &uncertain) {
			t.Fatalf("err=%v", attemptErr)
		}
	})
	t.Run("业务拒绝携带响应凭证保存失败", func(t *testing.T) {
		// center、store 保存中心与数据库。
		center, store, _ := newCenter(t, &fakeMTop{adjustRet: []string{"FAIL_BIZ_X::拒绝"}, adjustUpdated: "unb=123; _m_h5_tk=n1;"}, nil)
		execGapSQL(t, store, cookieAbort)
		// attemptErr 是单次平台调用尝试（含凭证恢复与响应凭证保存）的最终错误，用于断言分类与是否禁止自动重试。
		if attemptErr := center.actions.adjustOrderPriceAttempt(ctx, task, 100, true); attemptErr == nil || !strings.Contains(attemptErr.Error(), "forced cookie failure") {
			t.Fatalf("err=%v", attemptErr)
		}
	})
	t.Run("改价成功但响应凭证保存失败仍按成功处理", func(t *testing.T) {
		// center、store 保存中心与数据库。
		center, store, _ := newCenter(t, &fakeMTop{adjustOk: true, adjustRet: []string{"SUCCESS::ok"}, adjustUpdated: "unb=123; _m_h5_tk=n2;"}, nil)
		execGapSQL(t, store, cookieAbort)
		// attemptErr 平台已明确改价成功，本地 Cookie 写回失败不得改变动作结果。
		if attemptErr := center.actions.adjustOrderPriceAttempt(ctx, task, 100, true); attemptErr != nil {
			t.Fatalf("改价成功后 Cookie 写回失败不应使动作失败或不确定: %v", attemptErr)
		}
	})
}

// TestAIPricingUncertainResultNotifiesManualIntervention 验证 AI 自动改价结果不确定时发送独立的人工处理通知，明确失败则不打扰用户。
func TestAIPricingUncertainResultNotifiesManualIntervention(t *testing.T) {
	// ctx 是调用共用的上下文。
	ctx := context.Background()
	// cases 覆盖传输中断（报价转人工核对）、改价成功但本地状态保存失败、平台明确拒绝三种结果。
	cases := []struct {
		// name 是用例名称。
		name string
		// fake 是注入的平台改价替身。
		fake *fakeMTop
		// abortQuote 表示是否让报价终态写入失败。
		abortQuote bool
		// wantCalls 是期望的人工处理通知次数。
		wantCalls int
	}{
		{"传输中断", &fakeMTop{adjustErr: errors.New("连接中断")}, false, 1},
		{"改价成功但状态保存失败", &fakeMTop{adjustOk: true, adjustRet: []string{"SUCCESS::ok"}}, true, 1},
		{"平台明确拒绝", &fakeMTop{adjustRet: []string{"FAIL_BIZ_ORDER_CLOSED::订单已关闭"}}, false, 0},
	}
	// testCase 是当前待验证的改价结果。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// f 保存夹具。
			f := newAIPricingGapFixture(t, testCase.fake)
			// probe 记录自动化中心发出的人工处理通知。
			probe := &triggerAwareNotificationProbe{}
			f.center = NewWithDependencies(f.store, nil, nil, CenterDependencies{MTop: testCase.fake, Notifier: probe})
			if testCase.abortQuote {
				execGapSQL(t, f.store, `CREATE TRIGGER gap_abort_quote BEFORE UPDATE ON ai_bargain_quotes WHEN NEW.status IN ('adjusted','failed','needs_review') BEGIN SELECT RAISE(ABORT, 'forced quote finish failure'); END`)
			}
			_, _ = f.center.handleAIPricingMode(ctx, f.task)
			if probe.manualCalls != testCase.wantCalls {
				t.Fatalf("人工处理通知次数=%d，期望 %d", probe.manualCalls, testCase.wantCalls)
			}
			if testCase.wantCalls > 0 && (probe.manualAction != "AI 自动改价" || !strings.HasPrefix(probe.manualKey, "manual-intervention:ai-pricing:")) {
				t.Fatalf("通知内容异常: action=%q key=%q", probe.manualAction, probe.manualKey)
			}
		})
	}
}
