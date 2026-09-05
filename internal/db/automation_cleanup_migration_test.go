package db

import (
	"context"
	"testing"

	"github.com/pressly/goose/v3"
)

// assertCleanupCount 为 t 在 store 中执行只返回计数的 query；want 为预期行数，args 仅包含夹具标识。
func assertCleanupCount(t *testing.T, store *Store, want int, query string, args ...any) {
	t.Helper()
	// count 保存关联记录数，不读取卡密内容或凭据。
	var count int
	// err 保存计数查询错误，失败不能当作记录已删除。
	if err := store.DB.QueryRowContext(context.Background(), query, args...).Scan(&count); err != nil || count != want {
		t.Fatalf("清理计数=%d，期望=%d，错误=%v，查询=%s", count, want, err, query)
	}
}

// TestMultiDB_DeletedAutomationRulesUpgrade 为 t 验证旧库升级解除两种卡密引用，并保护活跃运行和未删除规则。
// 各方言串行操作 Goose 配置；所有写入与删除只针对隔离测试数据库。
func TestMultiDB_DeletedAutomationRulesUpgrade(t *testing.T) {
	// target 是当前可用的隔离数据库，资源由子测试释放。
	for _, target := range allTestTargets(t) {
		// t 管理当前方言的旧库构造、升级和删除断言。
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// store、ctx 为当前方言提供仓储及同步测试上下文。
			store, ctx := target.store, context.Background()
			// subdir、gooseDialect 指定嵌入迁移目录和 Goose 方言。
			subdir, gooseDialect := migrationTestSubdir(t, target.dialect)
			// err 保存设置迁移方言失败。
			if err := goose.SetDialect(gooseDialect); err != nil {
				t.Fatal(err)
			}
			goose.SetBaseFS(migrationsFS)
			// err 保存回退到清理迁移之前的错误；41 也用于验证 42 守卫回填先于 43 删除。
			if err := goose.DownTo(store.DB, "migrations/"+subdir, 41); err != nil {
				t.Fatal(err)
			}
			// userID、cookieID 为所有测试规则提供合法归属。
			userID, cookieID := seedAccount(t, store)
			// scenario 枚举无需运行、终态、重试边界及未删除规则；每条规则同时持有两种引用。
			for _, scenario := range []struct {
				// name 为场景和订单提供稳定虚构标识。
				name string
				// status 为空代表规则从未执行，否则构造该状态的历史运行。
				status string
				// attempts、sent 分别表示运行已尝试次数和已发送动作数。
				attempts, sent int
				// started 表示外部动作结果是否仍未知，迁移不得丢弃这种记录。
				started int
				// message 是人工构造的重试策略标记，不含平台内容。
				message string
				// live 表示未删除规则；disabled 区分仅停用和启用的规则。
				live, disabled bool
				// keep 表示迁移后必须保留整条规则及所有关联记录。
				keep bool
			}{
				{name: "never-run"},
				{name: "success", status: "success", attempts: 1, sent: 2},
				{name: "canceled", status: "canceled", attempts: 1},
				{name: "exhausted", status: "failed", attempts: 3},
				{name: "no-retry", status: "failed", attempts: 1, message: "[no_retry] stopped"},
				{name: "partial-terminal", status: "failed", attempts: 1, sent: 1},
				{name: "running", status: "running", attempts: 1, keep: true},
				{name: "review", status: "needs_review", attempts: 3, sent: 1, keep: true},
				{name: "retryable", status: "failed", attempts: 2, keep: true},
				{name: "safe-retry", status: "failed", attempts: 2, sent: 1, message: "[safe_retry] retry", keep: true},
				{name: "unknown-result", status: "failed", attempts: 3, started: 1, keep: true},
				{name: "live", live: true, keep: true},
				{name: "disabled", live: true, disabled: true, keep: true},
			} {
				// t 管理单个业务场景的独立规则和卡密，不并行操作迁移配置。
				t.Run(scenario.name, func(t *testing.T) {
					// cardID、err 创建虚构卡密组，内容不会触发任何外部调用。
					cardID, err := store.Cards.Create(ctx, &CardFull{Name: scenario.name, Type: "text", TextContent: "fixture", Enabled: true, UserID: userID})
					if err != nil {
						t.Fatal(err)
					}
					// ruleID、err 为旧版规则创建普通卡密动作，另用原始 SQL 构造模板变量历史引用。
					ruleID, err := store.Automation.Create(ctx, makeAutomationRule(cookieID, userID, scenario.name, "paid", !scenario.disabled, 100,
						AutomationActionInput{ActionType: "send_card", CardID: cardID, Enabled: true}))
					if err != nil {
						t.Fatal(err)
					}
					// err 保存历史模板动作插入错误，模板选择不参与此次卡密外键清理。
					if _, err := store.DB.ExecContext(ctx, `INSERT INTO automation_rule_actions(rule_id,action_type,message_template,config_json) VALUES(?,'send_template','fixture template','{}')`, ruleID); err != nil {
						t.Fatal(err)
					}
					// err 保存模板变量引用写入错误；引用另一动作才能分别验证两条级联链路。
					if _, err := store.DB.ExecContext(ctx, `INSERT INTO automation_action_template_bindings(action_id,variable_key,card_id)
						SELECT id,'key',? FROM automation_rule_actions WHERE rule_id=? AND action_type='send_template'`, cardID, ruleID); err != nil {
						t.Fatal(err)
					}
					if !scenario.live {
						// err 模拟旧版点击删除后数据库保留规则和动作的状态。
						if _, err := store.DB.ExecContext(ctx, `UPDATE automation_rules SET deleted_at=CURRENT_TIMESTAMP,enabled=0 WHERE id=?`, ruleID); err != nil {
							t.Fatal(err)
						}
					}
					if scenario.status != "" {
						// err 构造升级前执行历史，后续 42 必须先留下独立订单守卫。
						if _, err := store.DB.ExecContext(ctx, `INSERT INTO automation_runs
							(rule_id,cookie_id,order_id,trigger_type,trigger_key,status,attempt_count,sent_count,action_started,error_message,raw_event_json,delivery_proof)
							VALUES(?,?,?,'paid',?,?,?,?,?,?,'{}','')`, ruleID, cookieID, scenario.name, scenario.name, scenario.status, scenario.attempts, scenario.sent, scenario.started, scenario.message); err != nil {
							t.Fatal(err)
						}
					}
					// err 验证旧库确实会因外键拒绝删除卡密，避免测试未构造真实阻塞。
					if err := store.Cards.Delete(ctx, cardID); err == nil {
						t.Fatal("升级前关联卡密不应能被删除")
					}
					// err 先执行 42 回填旧执行守卫，再验证 43 升级不会随历史运行删除这些证据。
					if err := goose.UpTo(store.DB, "migrations/"+subdir, 42); err != nil {
						t.Fatal(err)
					}
					// err 保存从 42 执行完整启动迁移的错误。
					if err := Migrate(ctx, store.DB, target.dialect); err != nil {
						t.Fatal(err)
					}
					assertCleanupCount(t, store, 1, `SELECT COUNT(*) FROM cards WHERE id=?`, cardID)
					if scenario.status != "" {
						assertCleanupCount(t, store, 1, `SELECT COUNT(*) FROM order_automation_guards WHERE order_id=? AND cookie_id=?`, scenario.name, cookieID)
					}
					if scenario.keep {
						assertCleanupCount(t, store, 1, `SELECT COUNT(*) FROM automation_rules WHERE id=?`, ruleID)
						assertCleanupCount(t, store, 2, `SELECT COUNT(*) FROM automation_rule_actions WHERE rule_id=?`, ruleID)
						assertCleanupCount(t, store, 1, `SELECT COUNT(*) FROM automation_action_template_bindings WHERE card_id=?`, cardID)
						if scenario.status != "" {
							assertCleanupCount(t, store, 1, `SELECT COUNT(*) FROM automation_runs WHERE rule_id=? AND status=? AND attempt_count=? AND sent_count=? AND action_started=?`, ruleID, scenario.status, scenario.attempts, scenario.sent, scenario.started)
						}
						// err 验证未清理的规则仍保护其卡密，不能通过解除外键绕过。
						if err := store.Cards.Delete(ctx, cardID); err == nil {
							t.Fatal("仍被有效或待处理规则引用的卡密被错误删除")
						}
					} else {
						assertCleanupCount(t, store, 0, `SELECT COUNT(*) FROM automation_rules WHERE id=?`, ruleID)
						assertCleanupCount(t, store, 0, `SELECT COUNT(*) FROM automation_rule_actions WHERE rule_id=?`, ruleID)
						assertCleanupCount(t, store, 0, `SELECT COUNT(*) FROM automation_action_template_bindings WHERE card_id=?`, cardID)
						assertCleanupCount(t, store, 0, `SELECT COUNT(*) FROM automation_runs WHERE rule_id=?`, ruleID)
						// err 使用真实卡密仓储验证用户升级后删除可成功，而不是只断言引用数量。
						if err := store.Cards.Delete(ctx, cardID); err != nil {
							t.Fatalf("升级后删除卡密失败: %v", err)
						}
						assertCleanupCount(t, store, 0, `SELECT COUNT(*) FROM cards WHERE id=?`, cardID)
					}
					// err 验证重启重复迁移不报错，也不重新执行已完成的数据清理。
					if err := Migrate(ctx, store.DB, target.dialect); err != nil {
						t.Fatal(err)
					}
					if scenario.status != "" {
						assertCleanupCount(t, store, 1, `SELECT COUNT(*) FROM order_automation_guards WHERE order_id=? AND cookie_id=?`, scenario.name, cookieID)
					}
					// err 回退到守卫回填之前，下一场景仍从历史运行模拟完整升级链。
					if err := goose.DownTo(store.DB, "migrations/"+subdir, 41); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}

// TestMultiDB_DeletedAutomationRulesSharedCard 为 t 验证共享卡密只解除旧引用，延期任务与订单均保留。
func TestMultiDB_DeletedAutomationRulesSharedCard(t *testing.T) {
	// target 为每个可用方言提供独立数据库和清理函数。
	for _, target := range allTestTargets(t) {
		// t 管理当前方言的共享引用场景，不并行修改 Goose 配置。
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// store、ctx 提供隔离仓储和同步测试上下文。
			store, ctx := target.store, context.Background()
			// subdir、gooseDialect 指定当前方言迁移位置。
			subdir, gooseDialect := migrationTestSubdir(t, target.dialect)
			// err 保存迁移方言设置错误。
			if err := goose.SetDialect(gooseDialect); err != nil {
				t.Fatal(err)
			}
			goose.SetBaseFS(migrationsFS)
			// err 保存退回升级前版本的错误。
			if err := goose.DownTo(store.DB, "migrations/"+subdir, 42); err != nil {
				t.Fatal(err)
			}
			// userID、cookieID 是共享卡密和订单的合法归属。
			userID, cookieID := seedAccount(t, store)
			// cardID、err 保存共享卡密创建结果，测试数据不包含真实密钥。
			cardID, err := store.Cards.Create(ctx, &CardFull{Name: "shared", Type: "text", Enabled: true, UserID: userID})
			if err != nil {
				t.Fatal(err)
			}
			// ruleIDs 保留两条引用同一卡密的规则，首条删除、第二条保留。
			var ruleIDs []int64
			// item 为同账号的两个商品提供独立规则匹配条件。
			for _, item := range []string{"deleted", "live"} {
				// ruleID、err 保存引用共享卡密的规则创建结果。
				ruleID, err := store.Automation.Create(ctx, makeAutomationRule(cookieID, userID, item, "paid", true, 100,
					AutomationActionInput{ActionType: "send_card", CardID: cardID, Enabled: true}))
				if err != nil {
					t.Fatal(err)
				}
				ruleIDs = append(ruleIDs, ruleID)
			}
			// err 模拟旧版规则删除，不触及另一条规则。
			if _, err := store.DB.ExecContext(ctx, `UPDATE automation_rules SET deleted_at=CURRENT_TIMESTAMP,enabled=0 WHERE id=?`, ruleIDs[0]); err != nil {
				t.Fatal(err)
			}
			// err 保存关联账号的待处理事件；延期事件不是规则外键，升级不得删除它。
			if err := store.Automation.DeferTask(ctx, DeferredAutomationTask{TaskKey: "shared-pending", CookieID: cookieID, TriggerType: "paid", TaskJSON: `{}`}); err != nil {
				t.Fatal(err)
			}
			// err 保存无外部副作用的订单夹具写入结果。
			if err := store.Orders.Upsert(ctx, "shared-order", OrderUpsertOpts{CookieID: cookieID}); err != nil {
				t.Fatal(err)
			}
			// err 保存真实启动迁移结果。
			if err := Migrate(ctx, store.DB, target.dialect); err != nil {
				t.Fatal(err)
			}
			assertCleanupCount(t, store, 0, `SELECT COUNT(*) FROM automation_rules WHERE id=?`, ruleIDs[0])
			assertCleanupCount(t, store, 1, `SELECT COUNT(*) FROM automation_rule_actions WHERE card_id=?`, cardID)
			assertCleanupCount(t, store, 1, `SELECT COUNT(*) FROM automation_pending_tasks WHERE task_key='shared-pending' AND status='pending'`)
			assertCleanupCount(t, store, 1, `SELECT COUNT(*) FROM orders WHERE order_id='shared-order'`)
			// err 验证另一个有效规则仍然保护共享卡密。
			if err := store.Cards.Delete(ctx, cardID); err == nil {
				t.Fatal("共享卡密仍有有效引用，不得删除")
			}
			// err 验证新版正常删除规则能解除最后一条引用，无需再次升级。
			if err := store.Automation.Delete(ctx, userID, ruleIDs[1]); err != nil {
				t.Fatal(err)
			}
			// err 验证共享引用全部解除后卡密可被真实删除。
			if err := store.Cards.Delete(ctx, cardID); err != nil {
				t.Fatal(err)
			}
		})
	}
}
