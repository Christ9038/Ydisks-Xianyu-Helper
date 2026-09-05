package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestMultiDB_AutomationOwnershipFence 用 t 验证修复与运行/延期写入两种提交顺序，覆盖可用的三方言数据库。
func TestMultiDB_AutomationOwnershipFence(t *testing.T) {
	// target 是独立方言数据库，清理由对应子测试负责。
	for _, target := range allTestTargets(t) {
		// t 限定当前数据库的夹具与断言生命周期。
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// store、ctx 分别提供真实仓储和同步数据库操作上下文。
			store, ctx := target.store, context.Background()
			// userID、expected、options 保存旧买家归属及可信卖家修复夹具。
			userID, expected, options := seedOwnershipRepair(t, store)
			// ruleID、ruleErr 提供运行记录需要的规则外键，不配置外部动作。
			ruleID, ruleErr := store.Automation.Create(ctx, AutomationRuleInput{UserID: userID, CookieID: expected.CookieID, Name: "归属竞争", TriggerType: "buyer_reviewed", Enabled: true})
			if ruleErr != nil {
				t.Fatal(ruleErr)
			}
			// mode 是写入痕迹的种类，当前与历史字段名均必须受到保护。
			for _, mode := range []string{"run", "pending", "legacy-pending"} {
				// repairFirst 决定哪个操作先提交，两个顺序都不能出现迁移成功且旧任务写入成功。
				for _, repairFirst := range []bool{false, true} {
					// t 隔离当前顺序的断言；每轮使用不同订单号避免删除执行痕迹。
					t.Run(fmt.Sprintf("%s/repair-first-%v", mode, repairFirst), func(t *testing.T) {
						// snapshot 是本轮独立订单的旧归属证据，不影响其他顺序的记录。
						snapshot := expected
						snapshot.OrderID = fmt.Sprintf("fence-%s-%v", mode, repairFirst)
						// seedErr 创建包含软删除标记的旧账号订单。
						if _, seedErr := store.DB.ExecContext(ctx, `INSERT INTO orders(order_id,cookie_id,item_id,buyer_id,version,deleted_at) VALUES(?,?,?,?,?,CURRENT_TIMESTAMP)`, snapshot.OrderID, snapshot.CookieID, snapshot.ItemID, snapshot.BuyerID, snapshot.Version); seedErr != nil {
							t.Fatal(seedErr)
						}
						// writeTrace 在本轮同步写入执行痕迹；返回仓储错误供顺序断言。
						writeTrace := func() error {
							if mode == "run" {
								// started、startErr 表示本轮运行能否取得执行权。
								_, started, startErr := store.Automation.TryStartRun(ctx, AutomationRun{RuleID: ruleID, CookieID: snapshot.CookieID, OrderID: snapshot.OrderID, TriggerType: "buyer_reviewed", TriggerKey: snapshot.OrderID})
								if startErr == nil && !started {
									return errors.New("独立运行意外被当成重复事件")
								}
								return startErr
							}
							// field 兼容两种历史订单字段，未知任务内容不出现在失败日志中。
							field := "OrderID"
							if mode == "legacy-pending" {
								field = "order_id"
							}
							return store.Automation.DeferTask(ctx, DeferredAutomationTask{CookieID: snapshot.CookieID, TaskKey: snapshot.OrderID, TriggerType: "buyer_reviewed", TaskJSON: fmt.Sprintf(`{%q:%q}`, field, snapshot.OrderID)})
						}
						if repairFirst {
							// repairErr 是先行迁移结果，此时尚无任何执行痕迹。
							if repairErr := store.Orders.RecoverSoldOwnership(ctx, userID, options.CookieID, snapshot, options); repairErr != nil {
								t.Fatal(repairErr)
							}
							// writeErr 必须拒绝已经失去订单归属的旧任务。
							if writeErr := writeTrace(); !errors.Is(writeErr, ErrForbidden) {
								t.Fatalf("迁移后旧任务写入应被拒绝: %v", writeErr)
							}
						} else {
							// writeErr 保存先行运行或延期任务的持久化结果。
							if writeErr := writeTrace(); writeErr != nil {
								t.Fatal(writeErr)
							}
							// repairErr 必须因既有执行痕迹而拒绝迁移，且订单及审计全部回滚。
							if repairErr := store.Orders.RecoverSoldOwnership(ctx, userID, options.CookieID, snapshot, options); !errors.Is(repairErr, ErrOrderRecoveryUnsafe) {
								t.Fatalf("运行或延期任务先提交时必须拒绝迁移: %v", repairErr)
							}
							assertOwnershipRepairRolledBack(t, store, userID, snapshot)
						}
					})
				}
			}
		})
	}
}

// TestMultiDB_AutomationOwnershipUncommittedTrace 用 t 验证持有账号、订单锁的未提交运行不会被并发修复越过。
// 测试拥有修复协程，通过通道启动并等待其结束；数据库 Context 限制异常锁等待，不使用 sleep 决定交错。
func TestMultiDB_AutomationOwnershipUncommittedTrace(t *testing.T) {
	// target 是当前独立方言数据库。
	for _, target := range allTestTargets(t) {
		// t 管理当前数据库及并发事务的断言生命周期。
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// ctx、cancel 限定两个竞争事务的最长生命周期，测试退出负责取消。
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			// store 提供双方共用的数据库连接池，不共用单个 SQL 事务。
			store := target.store
			// userID、expected、options 提供仍满足修复授权的旧订单证据。
			userID, expected, options := seedOwnershipRepair(t, store)
			// ruleID、ruleErr 创建无外部动作的运行外键。
			ruleID, ruleErr := store.Automation.Create(ctx, AutomationRuleInput{UserID: userID, CookieID: expected.CookieID, Name: "事务互斥", TriggerType: "buyer_reviewed", Enabled: true})
			if ruleErr != nil {
				t.Fatal(ruleErr)
			}
			// transaction、beginErr 使用生产归属写入事务，故意将提交延后到竞争修复启动之后。
			transaction, beginErr := store.Automation.beginOwnershipWrite(ctx, expected.CookieID, expected.OrderID)
			if beginErr != nil {
				t.Fatal(beginErr)
			}
			defer transaction.Rollback()
			// started、startErr 使用生产 SQL 在持锁事务内创建尚不可被其他连接读取的运行。
			_, started, startErr := store.Automation.tryStartRun(ctx, transaction, AutomationRun{RuleID: ruleID, CookieID: expected.CookieID, OrderID: expected.OrderID, TriggerType: "buyer_reviewed", TriggerKey: "uncommitted"})
			if startErr != nil || !started {
				t.Fatalf("创建未提交运行失败: %v", startErr)
			}
			// entering 由修复协程关闭，通知测试当前并发操作已启动；finished 只传递一个修复结果。
			entering, finished := make(chan struct{}), make(chan error, 1)
			// 修复协程只执行数据库事务，不访问测试断言；测试下方始终等待其结果。
			go func() {
				close(entering)
				finished <- store.Orders.RecoverSoldOwnership(ctx, userID, options.CookieID, expected, options)
			}()
			<-entering
			// commitErr 保存自动化事务提交结果；此后修复必须看到运行，或因串行化竞争安全失败。
			commitErr := transaction.Commit()
			// repairErr 等待修复协程退出，任何成功迁移都说明执行痕迹保护被绕过。
			repairErr := <-finished
			if commitErr != nil || repairErr == nil {
				t.Fatalf("未提交运行互斥失败: 提交=%v 修复=%v", commitErr, repairErr)
			}
			assertOwnershipRepairRolledBack(t, store, userID, expected)
		})
	}
}

// TestAutomationOwnershipRejectsStaleActionAndAmbiguousTask 用 t 验证遗留错绑运行不能领取动作，矛盾延期订单号不能绕过归属锁。
func TestAutomationOwnershipRejectsStaleActionAndAmbiguousTask(t *testing.T) {
	// store、cleanup 提供隔离数据库和关闭职责。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试同步仓储调用的上下文。
	ctx := context.Background()
	// userID、expected、options 提供同用户的买家旧归属及卖家修复目标。
	userID, expected, options := seedOwnershipRepair(t, store)
	// ruleID、ruleErr 为遗留运行提供合法规则外键。
	ruleID, ruleErr := store.Automation.Create(ctx, AutomationRuleInput{UserID: userID, CookieID: expected.CookieID, Name: "遗留运行", TriggerType: "buyer_reviewed", Enabled: true})
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// repairErr 先完成没有执行痕迹的合法归属恢复。
	if repairErr := store.Orders.RecoverSoldOwnership(ctx, userID, options.CookieID, expected, options); repairErr != nil {
		t.Fatal(repairErr)
	}
	// result、seedErr 直接构造历史版本遗留的不一致运行；正式写入入口已禁止这种跨账号状态。
	result, seedErr := store.DB.ExecContext(ctx, `INSERT INTO automation_runs(rule_id,cookie_id,order_id,trigger_type,trigger_key,status,attempt_count) VALUES(?,?,?,'buyer_reviewed','stale','running',1)`, ruleID, expected.CookieID, expected.OrderID)
	if seedErr != nil {
		t.Fatal(seedErr)
	}
	// runID、idErr 保存遗留运行的测试主键。
	runID, idErr := result.LastInsertId()
	if idErr != nil {
		t.Fatal(idErr)
	}
	// started、actionErr 必须拒绝旧账号动作且不把检查点置为已启动。
	started, actionErr := store.Automation.StartRunAction(ctx, runID, 1, 0, time.Now().Add(time.Minute).Unix())
	if actionErr != nil || started {
		t.Fatalf("跨账号历史运行不能领取动作: started=%v err=%v", started, actionErr)
	}
	// persisted、readErr 验证失败领取没有改变动作检查点。
	persisted, readErr := store.Automation.GetRun(ctx, runID)
	if readErr != nil || persisted == nil || persisted.ActionStarted {
		t.Fatalf("拒绝动作后检查点异常: %v", readErr)
	}
	// taskErr 保存同一载荷包含矛盾新旧订单字段时的拒绝结果。
	taskErr := store.Automation.DeferTask(ctx, DeferredAutomationTask{CookieID: expected.CookieID, TaskKey: "ambiguous", TaskJSON: `{"OrderID":"absent","order_id":"repair-order"}`})
	if !errors.Is(taskErr, ErrForbidden) {
		t.Fatalf("矛盾订单字段不应进入延期队列: %v", taskErr)
	}
}

// TestAutomationOwnershipWriteFailureRollsBack 用 t 验证写入错误释放归属锁，失败事务不残留阻止恢复的执行痕迹。
func TestAutomationOwnershipWriteFailureRollsBack(t *testing.T) {
	// table 是当前通过 SQLite 触发器注入写入失败的执行痕迹表。
	for _, table := range []string{"automation_runs", "automation_pending_tasks"} {
		// t 为两种写入失败分别创建数据库，避免触发器互相干扰。
		t.Run(table, func(t *testing.T) {
			// store、cleanup 提供当前夹具仓储和释放函数。
			store, cleanup := newTestDB(t)
			defer cleanup()
			// ctx 控制同步写入及恢复操作。
			ctx := context.Background()
			// userID、expected、options 是符合恢复条件的旧订单和卖家目标。
			userID, expected, options := seedOwnershipRepair(t, store)
			// ruleID、ruleErr 保存测试运行的规则外键。
			ruleID, ruleErr := store.Automation.Create(ctx, AutomationRuleInput{UserID: userID, CookieID: expected.CookieID, Name: "失败回滚", TriggerType: "buyer_reviewed", Enabled: true})
			if ruleErr != nil {
				t.Fatal(ruleErr)
			}
			// triggerErr 保存固定表写入失败触发器的创建结果，不修改生产数据库结构。
			if _, triggerErr := store.DB.ExecContext(ctx, "CREATE TRIGGER reject_trace BEFORE INSERT ON "+table+" BEGIN SELECT RAISE(ABORT,'fixture failure'); END"); triggerErr != nil {
				t.Fatal(triggerErr)
			}
			// writeErr 保存被注入的写入错误；不能当成未匹配规则或重复事件吞掉。
			var writeErr error
			if table == "automation_runs" {
				_, _, writeErr = store.Automation.TryStartRun(ctx, AutomationRun{RuleID: ruleID, CookieID: expected.CookieID, OrderID: expected.OrderID, TriggerKey: "failed-write"})
			} else {
				writeErr = store.Automation.DeferTask(ctx, DeferredAutomationTask{CookieID: expected.CookieID, TaskKey: "failed-write", TaskJSON: `{"OrderID":"repair-order"}`})
			}
			if writeErr == nil {
				t.Fatal("执行痕迹写入错误被吞掉")
			}
			// repairErr 必须证明失败自动化写入已经回滚且释放锁，真实归属恢复可继续提交。
			if repairErr := store.Orders.RecoverSoldOwnership(ctx, userID, options.CookieID, expected, options); repairErr != nil {
				t.Fatalf("自动化写入回滚后应可恢复归属: %v", repairErr)
			}
		})
	}
}
