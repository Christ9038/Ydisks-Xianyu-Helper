package automation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// leaseTestRepository 用原子计数验证心跳和失败取消；不存储真实凭证。
type leaseTestRepository struct {
	// calls 统计并发续租次数。
	calls atomic.Int64
	// failAfter 控制第几次请求起模拟数据库失权，零表示始终成功。
	failAfter int64
}

// RenewExecutingRunLease 为 repository 的测试调用计数；ctx 与运行标识仅满足生产消费端口。
func (repository *leaseTestRepository) RenewExecutingRunLease(ctx context.Context, _ int64, _ int, _ int64) error {
	// err 表示测试父请求已经取消。
	if err := ctx.Err(); err != nil {
		return err
	}
	// call 是本次续租序号。
	if call := repository.calls.Add(1); repository.failAfter > 0 && call >= repository.failAfter {
		return db.ErrAutomationRunLeaseLost
	}
	return nil
}

// TestRunExecutionLeaseLifecycle 验证初始拒绝、后台失权取消、主动检查、重复停止及无租约上下文；t 拥有全部测试资源。
func TestRunExecutionLeaseLifecycle(t *testing.T) {
	// rejected 在初次续租时拒绝启动，不得留下协程。
	rejected := &leaseTestRepository{failAfter: 1}
	// err 保存初次启动错误。
	if _, _, err := startRunExecutionLease(context.Background(), rejected, 1, 1, time.Millisecond); !errors.Is(err, db.ErrAutomationRunLeaseLost) {
		t.Fatal("初次失权未拒绝", err)
	}
	// failing 先允许两次后台续租，再模拟续租失权，动作 Context 必须被取消。
	failing := &leaseTestRepository{failAfter: 4}
	// ctx、stop、err 保存当前受心跳控制的执行上下文与资源释放函数。
	ctx, stop, err := startRunExecutionLease(context.Background(), failing, 1, 1, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("失权未取消动作")
	}
	if !errors.Is(context.Cause(ctx), db.ErrAutomationRunLeaseLost) {
		t.Fatal("取消丢失失权原因")
	}
	stop()
	stop()
	// successful 用于验证发送前主动续租与正常停止。
	successful := &leaseTestRepository{}
	ctx, stop, err = startRunExecutionLease(context.Background(), successful, 2, 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// err 保存主动执行权检查结果。
	if err := checkRunExecution(ctx); err != nil {
		t.Fatal(err)
	}
	if successful.calls.Load() != 2 {
		t.Fatal("发送前没有重新检查执行权")
	}
	stop()
	if !errors.Is(checkRunExecution(ctx), context.Canceled) {
		t.Fatal("停止后仍允许发送")
	}
	if checkRunExecution(nilAutomationContext()) != nil || checkRunExecution(context.Background()) != nil {
		t.Fatal("独立动作兼容路径失败")
	}
	// manualFailure 让第一次逐消息检查而非心跳触发失权。
	manualFailure := &leaseTestRepository{failAfter: 2}
	ctx, stop, err = startRunExecutionLease(context.Background(), manualFailure, 3, 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if !errors.Is(checkRunExecution(ctx), db.ErrAutomationRunLeaseLost) {
		t.Fatal("逐消息检查未返回失权")
	}
}

// TestDeliveryReplayExpiredOwnerCannotSend 验证旧补发即使保留完整内存快照也不能在失权后发送；t 提供 SQLite 和消息替身。
func TestDeliveryReplayExpiredOwnerCannotSend(t *testing.T) {
	// ctx、store、center、sender、order、cleanup 保存隔离的运行和外部发送记录。
	ctx, store, center, sender, _, order, cleanup := newManualDeliveryFixture(t, "expired-replay-owner")
	defer cleanup()
	// runID 标识两单位中已完成一单位的历史运行。
	runID := seedPartialReplay(t, ctx, store, order)
	// run、err 保存旧请求独占的快照。
	run, err := store.Automation.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	// attempt、claimed、err 保存首次领取结果。
	attempt, claimed, err := store.Automation.ClaimDeliveryReplay(ctx, runID, run.AttemptCount, time.Now().Add(time.Minute).Unix())
	if err != nil || !claimed {
		t.Fatal("首次领取失败", err)
	}
	run.AttemptCount, run.Status, run.ActionStarted = attempt, "running", true
	// err 用数据库时间推进模拟五分钟后的停顿。
	if _, err := store.DB.ExecContext(ctx, `UPDATE automation_runs SET lease_expires_at=1 WHERE id=?`, runID); err != nil {
		t.Fatal(err)
	}
	// err 保存到期扫描隔离结果。
	if err := NewScheduler(center).runRecoveryTasks(ctx); err != nil {
		t.Fatal(err)
	}
	// claimed、err 保存新执行代次的领取结果。
	if _, claimed, err := store.Automation.ClaimDeliveryReplay(ctx, runID, attempt, time.Now().Add(time.Minute).Unix()); err != nil || !claimed {
		t.Fatal("新请求领取失败", err)
	}
	// task、err 保存旧执行栈中已经准备好的任务。
	task, err := center.prepareManualDeliveryTask(ctx, order)
	if err != nil {
		t.Fatal(err)
	}
	// err 保存旧补发失权结果。
	if _, err := center.replayDeliveryProof(ctx, task, run); !errors.Is(err, db.ErrAutomationRunLeaseLost) {
		t.Fatal("旧执行栈未返回失权", err)
	}
	// err 验证普通动作共用的执行包装也拒绝同一旧代次。
	if _, err := center.runs.executeLeasedAction(ctx, task, db.AutomationAction{}, shipmentDeliveryProof{}, run); !errors.Is(err, db.ErrAutomationRunLeaseLost) {
		t.Fatal("普通动作未拒绝旧代次", err)
	}
	if len(sender.texts) != 0 {
		t.Fatal("旧请求失权后仍发送消息")
	}
	// current、err 验证旧请求的失败收尾不能释放新代次。
	current, err := store.Automation.GetRun(ctx, runID)
	if err != nil || current.Status != "running" || current.AttemptCount != attempt+1 {
		t.Fatal("旧请求覆盖新执行权", err)
	}
}

// TestRecoverySnapshotCannotQuarantineRenewedRun 验证同一代次续租后旧扫描不能隔离，过期租约也不能自行复活；t 使用真实 SQLite 条件更新。
func TestRecoverySnapshotCannotQuarantineRenewedRun(t *testing.T) {
	// ctx、store、center、order、cleanup 提供可领取的部分发货运行。
	ctx, store, center, _, _, order, cleanup := newManualDeliveryFixture(t, "stale-recovery-snapshot")
	defer cleanup()
	// runID 是要验证执行权竞争的历史运行。
	runID := seedPartialReplay(t, ctx, store, order)
	// run、err 保存初始代次。
	run, err := store.Automation.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	// attempt、claimed、err 保存补发执行权。
	attempt, claimed, err := store.Automation.ClaimDeliveryReplay(ctx, runID, run.AttemptCount, time.Now().Add(time.Minute).Unix())
	if err != nil || !claimed {
		t.Fatal("领取失败", err)
	}
	// stale、err 模拟扫描已经缓存的旧租约快照。
	stale, err := store.Automation.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	// err 保存有效执行者续租结果。
	if err := store.Automation.RenewExecutingRunLease(ctx, runID, attempt, time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	// err 保存旧扫描正常竞争结果。
	if err := NewScheduler(center).quarantineRunForReview(ctx, *stale, "旧扫描"); err != nil {
		t.Fatal(err)
	}
	// current、err 验证旧快照没有覆盖续租。
	current, err := store.Automation.GetRun(ctx, runID)
	if err != nil || current.Status != "running" {
		t.Fatal("旧扫描隔离了续租运行", err)
	}
	// err 保存无效租约输入结果。
	if err := store.Automation.RenewExecutingRunLease(ctx, runID, attempt, time.Now().Unix()); !errors.Is(err, db.ErrAutomationRunLeaseLost) {
		t.Fatal("非法截止时间被接受", err)
	}
	// err 推进真实数据库租约到过期状态。
	if _, err := store.DB.ExecContext(ctx, `UPDATE automation_runs SET lease_expires_at=1 WHERE id=?`, runID); err != nil {
		t.Fatal(err)
	}
	// err 保存过期续租拒绝结果。
	if err := store.Automation.RenewExecutingRunLease(ctx, runID, attempt, time.Now().Add(time.Hour).Unix()); !errors.Is(err, db.ErrAutomationRunLeaseLost) {
		t.Fatal("过期执行者复活", err)
	}
}

// leaseSwitchSender 在首条消息返回时切换数据库执行代次，以确定性模拟批次中途失权。
type leaseSwitchSender struct {
	// sent 仅由当前执行 goroutine 写入，测试在调用返回后读取。
	sent int
	// afterFirst 只执行一次，用于模拟恢复入口已经转移执行权。
	afterFirst func()
}

// SendText 记录 sender 的消息数并在第一条后执行失权钩子，ctx 和消息字段不包含真实账号。
func (sender *leaseSwitchSender) SendText(context.Context, string, string, string) error {
	sender.sent++
	if sender.sent == 1 {
		sender.afterFirst()
	}
	return nil
}

// SendImage 与文字共用 sender 的计数和失权钩子，保证两种消息都受相同保护。
func (sender *leaseSwitchSender) SendImage(ctx context.Context, chatID, buyerID, content string, _ int64, _ int, _ int) error {
	return sender.SendText(ctx, chatID, buyerID, content)
}

// UpdateCookie 满足发送端口；测试不接收或存储任何凭证。
func (sender *leaseSwitchSender) UpdateCookie(string) {}

// leaseSwitchProvider 只提供测试独占发送器，不启动账号运行时。
type leaseSwitchProvider struct {
	// sender 是当前测试拥有的消息发送替身。
	sender MessageSender
}

// Sender 返回 provider 的唯一发送器，账号参数仅用于满足生产端口。
func (provider leaseSwitchProvider) Sender(string) (MessageSender, bool) {
	return provider.sender, true
}

// TestLeasedDeliveryStopsBetweenMessages 验证自动执行和人工补发在第一条之后失权时都不会继续第二条；t 管理隔离夹具。
func TestLeasedDeliveryStopsBetweenMessages(t *testing.T) {
	for _, replay := range []bool{false, true} { // replay 选择首次完整发货或历史快照补发路径。
		t.Run(map[bool]string{false: "initial", true: "replay"}[replay], func(t *testing.T) { // t 隔离两种执行路径的运行和库存。
			// ctx、store、center、platform、order、cleanup 提供真实数据库和确认发货计数。
			ctx, store, center, _, platform, order, cleanup := newManualDeliveryFixture(t, "mid-batch-lease")
			defer cleanup()
			// err 将规则设为两条消息以验证第二条不会发送。
			if _, err := store.DB.ExecContext(ctx, `UPDATE automation_rule_actions SET delivery_count=2 WHERE action_type='send_card'`); err != nil {
				t.Fatal(err)
			}

			if replay {
				seedPartialReplay(t, ctx, store, order)
			}
			// sender 在第一条发送完成时原子推进运行代次，模拟旧执行栈已失权。
			sender := &leaseSwitchSender{afterFirst: func() { // 当前回调只更新测试数据库，不进行真实外部操作。
				// err 保存测试执行权转移结果。
				if _, err := store.DB.ExecContext(ctx, `UPDATE automation_runs SET attempt_count=attempt_count+1 WHERE cookie_id=? AND order_id=?`, order.CookieID, order.OrderID); err != nil {
					t.Error(err)
				}
			}}
			center.actions.senders = leaseSwitchProvider{sender: sender}
			// err 必须阻止整个批次成功收口。
			if _, err := center.ManualFullDelivery(ctx, order); err == nil {
				t.Fatal("失权运行错误地成功")
			}
			if sender.sent != 1 || platform.consignCalls != 0 {
				t.Fatalf("失权后仍有副作用: sends=%d confirms=%d", sender.sent, platform.consignCalls)
			}
		})
	}
}

// TestCanceledExecutionStopsAllSideEffects 验证取消在各逐单位入口都先于发送、库存消费和 API 取卡；t 不创建任何真实外部连接。
func TestCanceledExecutionStopsAllSideEffects(t *testing.T) {
	// ctx、cancel 模拟心跳失权后已经取消的动作上下文。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// repository 用于证明启动时已取消不会访问续租端口。
	repository := &leaseTestRepository{}
	// err 保存已经取消的父请求启动结果。
	if _, _, err := startRunExecutionLease(ctx, repository, 1, 1, time.Hour); !errors.Is(err, context.Canceled) || repository.calls.Load() != 0 {
		t.Fatal("取消后仍尝试启动执行", err)
	}
	// fetcher 记录任何误发的 API 请求；本测试要求始终为空。
	fetcher := &apiCardFetcherStub{}
	// executor 没有数据库和发送器，取消保护必须先于任何外部依赖访问。
	executor := automationActionExecutor{apiFetcher: func() APICardFetcher { return fetcher }} // 当前回调仅返回本测试独占的 API 替身。
	// err 保存已取消的文本发送结果。
	if err := executor.sendText(ctx, Task{}, "text"); !errors.Is(err, ErrMessageNotSent) {
		t.Fatal(err)
	}
	// err 保存已取消的图片发送结果。
	if err := executor.sendImage(ctx, Task{}, "image", 0); !errors.Is(err, ErrMessageNotSent) {
		t.Fatal(err)
	}
	// err 保存已取消的确认发货结果。
	if err := executor.confirmShipmentWithProof(ctx, Task{}, shipmentDeliveryProof{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// card 只提供单位循环需要的卡组身份，取消后不得访问数据库。
	card := &db.CardFull{ID: 1, Type: "data"}
	// err 保存数据卡循环在首次消费前的取消结果。
	if _, err := executor.sendDataCardWithProof(ctx, Task{}, card, 2); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// err 保存普通 API 卡在首次取卡前的取消结果。
	if _, err := executor.sendAPICardWithProof(ctx, Task{}, db.AutomationAction{}, card, 2); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// err 保存模板消息在首条变量加载前的取消结果。
	if _, err := executor.sendTemplate(ctx, Task{}, db.AutomationAction{ConfigJSON: `{}`, TemplateMessages: []string{"first", "second"}}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// binding 用于验证单个模板变量内部的逐份消费保护。
	binding := templateBindingCard{card: card, count: 2}
	// err 保存模板数据卡在首次消费前的取消结果。
	if _, _, _, _, _, _, err := executor.loadTemplateBatchLines(ctx, Task{}, binding, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// state 保存模板 API 测试替身，不能产生真实请求。
	state := templateDeliveryState{apiFetcher: fetcher}
	// err 保存模板 API 卡在首次取卡前的取消结果。
	if _, _, _, _, _, _, err := executor.loadTemplateAPILines(ctx, Task{}, db.AutomationAction{}, &state, binding, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(fetcher.requests) != 0 {
		t.Fatal("取消后仍请求 API 卡密")
	}
}
