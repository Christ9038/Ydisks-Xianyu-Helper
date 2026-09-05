package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestMultiDB_AutomationGuardSurvivesRuleDelete 用 t 验证完成运行及规则被物理删除后，独立痕迹仍阻断归属恢复。
func TestMultiDB_AutomationGuardSurvivesRuleDelete(t *testing.T) {
	// target 是可用方言的隔离数据库，子测试负责释放连接及数据。
	for _, target := range allTestTargets(t) {
		// t 管理当前方言的完整创建、完成、删除与恢复断言。
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// store、ctx 提供真实仓储及不包含外部调用的同步上下文。
			store, ctx := target.store, context.Background()
			// userID、expected、options 保存旧订单归属及可授权的卖家恢复参数。
			userID, expected, options := seedOwnershipRepair(t, store)
			// run 是使用实际规则外键的待执行订单任务。
			run := seedAutomationGuardRun(t, store, userID, expected.CookieID, expected.OrderID)
			// runID、started、startErr 保存真实入口授予的运行执行权。
			runID, started, startErr := store.Automation.TryStartRun(ctx, run)
			if startErr != nil || !started {
				t.Fatalf("创建运行失败: started=%v err=%v", started, startErr)
			}
			// finishErr 将已发送一次动作的运行置为成功，使原有物理删除条件成立。
			if finishErr := store.Automation.FinishRun(ctx, runID, 1, "success", 1, ""); finishErr != nil {
				t.Fatal(finishErr)
			}
			// deleteErr 必须保留真实 Delete 的物理删除语义，不以软删除规避本回归。
			if deleteErr := store.Automation.Delete(ctx, userID, run.RuleID); deleteErr != nil {
				t.Fatal(deleteErr)
			}
			// table 是必须已经清空的规则及级联运行表。
			for _, table := range []string{"automation_rules", "automation_runs"} {
				// count 仅统计隔离夹具行数，证明原记录已真实消失。
				var count int
				// readErr 保存固定表集合中的计数查询结果。
				if readErr := store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); readErr != nil || count != 0 {
					t.Fatalf("物理删除未完成: table=%s count=%d err=%v", table, count, readErr)
				}
			}
			assertAutomationGuardCount(t, store, run, 1)
			// repairErr 必须仅凭独立痕迹拒绝迁移，不能依赖已删除的运行或规则。
			if repairErr := store.Orders.RecoverSoldOwnership(ctx, userID, options.CookieID, expected, options); !errors.Is(repairErr, ErrOrderRecoveryUnsafe) {
				t.Fatalf("物理删除后仍应拒绝迁移: %v", repairErr)
			}
			assertOwnershipRepairRolledBack(t, store, userID, expected)
		})
	}
}

// TestAutomationGuardExistingRunStates 用 t 验证所有既有运行状态及安全重领都补写痕迹，重复入口保持联合主键幂等。
func TestAutomationGuardExistingRunStates(t *testing.T) {
	// state 区分不可重领状态与可以安全重试的失败状态。
	for _, state := range []string{"running", "success", "needs_review", "failed", "reclaim"} {
		// t 隔离每种历史运行，直接 SQL 夹具模拟尚未带有独立痕迹的记录。
		t.Run(state, func(t *testing.T) {
			// store、cleanup 提供 SQLite 夹具及测试退出清理职责。
			store, cleanup := newTestDB(t)
			defer cleanup()
			// ctx 控制本测试的同步数据库操作。
			ctx := context.Background()
			// userID、cookieID 提供规则及运行所需的真实账号外键。
			userID, cookieID := seedAccount(t, store)
			// run 保留非空但尚无本地订单行的合法兼容输入。
			run := seedAutomationGuardRun(t, store, userID, cookieID, "guard-existing")
			// status、attempt 分别是历史状态及已消耗次数，普通 failed 不允许自动重试。
			status, attempt := state, 3
			if state == "reclaim" {
				status, attempt = "failed", 1
			}
			// seedErr 保存历史运行创建结果，未来租约避免 running 被误当作过期重领。
			if _, seedErr := store.DB.ExecContext(ctx, `INSERT INTO automation_runs(rule_id,cookie_id,order_id,trigger_type,trigger_key,status,attempt_count,lease_expires_at) VALUES(?,?,?,?,?,?,?,?)`, run.RuleID, run.CookieID, run.OrderID, run.TriggerType, run.TriggerKey, status, attempt, time.Now().Add(time.Hour).Unix()); seedErr != nil {
				t.Fatal(seedErr)
			}
			// mismatched 保留相同幂等键但声明错误订单，不能借用既有运行伪造其他订单的痕迹。
			mismatched := run
			mismatched.OrderID = "wrong-order-identity"
			// wrongID、wrongStarted、wrongErr 必须保留未取得执行权的原有返回语义，且不写入任一身份的痕迹。
			wrongID, wrongStarted, wrongErr := store.Automation.TryStartRun(ctx, mismatched)
			if wrongErr != nil || wrongStarted || wrongID != 0 {
				t.Fatalf("错误订单身份不能借用运行: id=%d started=%v err=%v", wrongID, wrongStarted, wrongErr)
			}
			assertAutomationGuardCount(t, store, mismatched, 0)
			assertAutomationGuardCount(t, store, run, 0)
			// started、startErr 保存重复入口是否授予执行权，不可执行的既有状态也必须留下痕迹。
			_, started, startErr := store.Automation.TryStartRun(ctx, run)
			if startErr != nil || started != (state == "reclaim") {
				t.Fatalf("既有运行执行权异常: started=%v err=%v", started, startErr)
			}
			assertAutomationGuardCount(t, store, run, 1)
			// replayStarted、replayErr 验证重复补写没有重复授权或主键错误。
			_, replayStarted, replayErr := store.Automation.TryStartRun(ctx, run)
			if replayErr != nil || replayStarted {
				t.Fatalf("痕迹重放应幂等: started=%v err=%v", replayStarted, replayErr)
			}
			assertAutomationGuardCount(t, store, run, 1)
		})
	}
}

// TestAutomationGuardOrderCompatibility 用 t 验证无订单号不留痕迹、有订单号但无本地订单行仍可运行并留痕迹。
func TestAutomationGuardOrderCompatibility(t *testing.T) {
	// mode 使用非空子测试名称，避免 SQLite URI 路径被自动生成的井号名称截断。
	for _, mode := range []string{"empty-order-id", "absent-order-row"} {
		// t 拥有当前兼容路径的独立规则和数据库。
		t.Run(mode, func(t *testing.T) {
			// store、cleanup 提供隔离仓储和释放责任。
			store, cleanup := newTestDB(t)
			defer cleanup()
			// ctx 是同步数据库操作的上下文。
			ctx := context.Background()
			// userID、cookieID 是测试账号及规则归属。
			userID, cookieID := seedAccount(t, store)
			// orderID、wantGuards 描述两种历史任务的输入及预期痕迹数量。
			orderID, wantGuards := "", 0
			if mode == "absent-order-row" {
				orderID, wantGuards = "not-yet-local", 1
			}
			// run 是通过正式规则创建的兼容任务。
			run := seedAutomationGuardRun(t, store, userID, cookieID, orderID)
			// runID、started、startErr 保存合法执行权，不能因缺少订单行而拒绝任务。
			runID, started, startErr := store.Automation.TryStartRun(ctx, run)
			if startErr != nil || !started {
				t.Fatalf("兼容任务不应被拒绝: started=%v err=%v", started, startErr)
			}
			// finishErr 允许使用真实物理 Delete 验证独立痕迹生命周期。
			if finishErr := store.Automation.FinishRun(ctx, runID, 1, "success", 1, ""); finishErr != nil {
				t.Fatal(finishErr)
			}
			// replayStarted、replayErr 验证空订单与缺少本地行两种成功历史均保持去重。
			_, replayStarted, replayErr := store.Automation.TryStartRun(ctx, run)
			if replayErr != nil || replayStarted {
				t.Fatalf("兼容任务去重失败: started=%v err=%v", replayStarted, replayErr)
			}
			// deleteErr 保存原有物理删除操作结果。
			if deleteErr := store.Automation.Delete(ctx, userID, run.RuleID); deleteErr != nil {
				t.Fatal(deleteErr)
			}
			assertAutomationGuardCount(t, store, run, wantGuards)
			// orders 验证保护逻辑未伪造本地订单事实。
			var orders int
			// readErr 保存订单总数查询错误。
			if readErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders`).Scan(&orders); readErr != nil || orders != 0 {
				t.Fatalf("兼容任务不应创建订单: count=%d err=%v", orders, readErr)
			}
		})
	}
}

// TestAutomationGuardWriteFailureRollsBack 用 t 验证痕迹写入失败时，新建和重领的运行变更全部回滚且不授权动作。
func TestAutomationGuardWriteFailureRollsBack(t *testing.T) {
	// mode 区分全新运行与需要回滚状态、代次变更的重领运行。
	for _, mode := range []string{"new", "reclaim", "existing-success"} {
		// t 隔离故障触发器与运行夹具，避免不同路径互相影响。
		t.Run(mode, func(t *testing.T) {
			// store、cleanup 提供 SQLite 事务故障注入环境。
			store, cleanup := newTestDB(t)
			defer cleanup()
			// ctx 控制同步数据库操作。
			ctx := context.Background()
			// userID、cookieID 为测试运行建立合法规则归属。
			userID, cookieID := seedAccount(t, store)
			// run 是必须原子留存独立痕迹的非空订单任务。
			run := seedAutomationGuardRun(t, store, userID, cookieID, "guard-failure")
			// status 是故障前应保持不变的历史状态，新建模式不使用此状态。
			status := "failed"
			if mode == "existing-success" {
				status = "success"
			}
			if mode != "new" {
				// seedErr 创建无 guard 的历史记录，安全失败允许本轮尝试重领。
				if _, seedErr := store.DB.ExecContext(ctx, `INSERT INTO automation_runs(rule_id,cookie_id,order_id,trigger_type,trigger_key,status,attempt_count) VALUES(?,?,?,?,?,?,1)`, run.RuleID, run.CookieID, run.OrderID, run.TriggerType, run.TriggerKey, status); seedErr != nil {
					t.Fatal(seedErr)
				}
			}
			// triggerErr 注入运行写入之后的痕迹错误，证明两种写入不是分离事务。
			if _, triggerErr := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_guard BEFORE INSERT ON order_automation_guards BEGIN SELECT RAISE(ABORT,'fixture guard failure'); END`); triggerErr != nil {
				t.Fatal(triggerErr)
			}
			// runID、started、startErr 必须不返回任何可执行的运行身份，并显式传递写入失败。
			runID, started, startErr := store.Automation.TryStartRun(ctx, run)
			if startErr == nil || started || runID != 0 {
				t.Fatalf("痕迹失败不能授权运行: id=%d started=%v err=%v", runID, started, startErr)
			}
			assertAutomationGuardCount(t, store, run, 0)
			// runs 是失败事务后仍存在的运行数量，新建模式必须完全回滚。
			var runs int
			// readErr 保存运行数量查询错误。
			if readErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_runs`).Scan(&runs); readErr != nil {
				t.Fatal(readErr)
			}
			if mode == "new" {
				if runs != 0 {
					t.Fatal("痕迹失败后残留新建运行")
				}
				return
			}
			// persistedStatus、attempt 保存历史运行的回滚后状态及代次。
			var persistedStatus string
			// attempt 必须仍是故障前的首个代次。
			var attempt int
			// readErr 保存回滚后运行读取结果，不能残留重领产生的 running 或递增代次。
			if readErr := store.DB.QueryRowContext(ctx, `SELECT status,attempt_count FROM automation_runs WHERE rule_id=? AND trigger_key=?`, run.RuleID, run.TriggerKey).Scan(&persistedStatus, &attempt); readErr != nil || runs != 1 || persistedStatus != status || attempt != 1 {
				t.Fatalf("历史运行未正确回滚: count=%d status=%s attempt=%d err=%v", runs, persistedStatus, attempt, readErr)
			}
		})
	}
}

// seedAutomationGuardRun 为 t 的 store 创建 userID/cookieID 归属规则，返回带 orderID 的未持久化运行，不发送外部动作。
func seedAutomationGuardRun(t *testing.T, store *Store, userID int64, cookieID, orderID string) AutomationRun {
	t.Helper()
	// ruleID、ruleErr 保存真实规则外键和创建错误。
	ruleID, ruleErr := store.Automation.Create(context.Background(), AutomationRuleInput{UserID: userID, CookieID: cookieID, Name: "独立订单执行痕迹", TriggerType: "buyer_reviewed", Enabled: true})
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	return AutomationRun{RuleID: ruleID, CookieID: cookieID, OrderID: orderID, TriggerType: "buyer_reviewed", TriggerKey: "guard-event"}
}

// assertAutomationGuardCount 用 t 检查 store 中与 run 订单、账号匹配的痕迹数量是否等于 want，不读取任何敏感载荷。
func assertAutomationGuardCount(t *testing.T, store *Store, run AutomationRun, want int) {
	t.Helper()
	// count 只统计当前订单和账号组合，不把其他订单痕迹误认为保护成功。
	var count int
	// readErr 保存独立痕迹表查询结果，缺表属于迁移契约错误而非可忽略兼容情况。
	if readErr := store.DB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM order_automation_guards WHERE order_id=? AND cookie_id=?`, run.OrderID, run.CookieID).Scan(&count); readErr != nil || count != want {
		t.Fatalf("独立执行痕迹不符: count=%d want=%d err=%v", count, want, readErr)
	}
}
