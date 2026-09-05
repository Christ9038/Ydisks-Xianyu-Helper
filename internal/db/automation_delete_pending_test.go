package db

import (
	"context"
	"errors"
	"testing"
)

// TestMultiDB_AutomationDeletePendingState 验证删除规则与升级清理采用相同待处理判断，保留安全重试和未知结果。
func TestMultiDB_AutomationDeletePendingState(t *testing.T) {
	// target 是当前可用的独立方言数据库。
	for _, target := range allTestTargets(t) {
		// t 管理当前方言资源及断言。
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// store、ctx 提供当前数据库和本地操作生命周期。
			store, ctx := target.store, context.Background()
			// userID、cookieID 是合成规则归属，不使用真实账号。
			userID, cookieID := seedAccount(t, store)
			// scenario 描述终态、部分重试和未知动作的保护边界。
			for _, scenario := range []struct {
				// name 标识规则及运行的唯一测试名称。
				name string
				// sent、attempts、started 是运行恢复所需的检查点状态。
				sent, attempts, started int
				// message 指定普通失败或安全重试标志。
				message string
				// keep 表示规则与执行记录必须保留。
				keep bool
			}{{"safe-retry", 1, 1, 0, "[safe_retry] retry", true}, {"unknown", 1, 3, 1, "unknown", true}, {"retry", 0, 1, 0, "retry", true}, {"terminal", 1, 3, 0, "failed", false}} {
				// ruleID、err 创建当前场景的独立规则。
				ruleID, err := store.Automation.Create(ctx, makeAutomationRule(cookieID, userID, scenario.name, "paid", true, 1))
				if err != nil {
					t.Fatal(err)
				}
				// runID、started、err 建立真实运行和独立执行守卫。
				runID, started, err := store.Automation.TryStartRun(ctx, AutomationRun{RuleID: ruleID, CookieID: cookieID, OrderID: scenario.name, TriggerType: "paid", TriggerKey: scenario.name})
				if err != nil || !started {
					t.Fatal(err)
				}
				if _, err = store.DB.ExecContext(ctx, "UPDATE automation_runs SET status='failed',sent_count=?,attempt_count=?,action_started=?,error_message=? WHERE id=?", scenario.sent, scenario.attempts, scenario.started, scenario.message, runID); err != nil {
					t.Fatal(err)
				}
				err = store.Automation.Delete(ctx, userID, ruleID)
				if scenario.keep && !errors.Is(err, ErrAutomationRunActive) {
					t.Fatalf("待处理运行被删除: %s err=%v", scenario.name, err)
				}
				if !scenario.keep && err != nil {
					t.Fatal(err)
				}
				// count 保存删除后仍存在的运行数，不读取动作秘密。
				var count int
				if err = store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM automation_runs WHERE id=?", runID).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if (count == 1) != scenario.keep {
					t.Fatalf("检查点留存错误: %s count=%d", scenario.name, count)
				}
			}
		})
	}
}
