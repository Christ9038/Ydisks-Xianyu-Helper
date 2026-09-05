package automation

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/db"
)

// TestCenter_OwnershipRepairBetweenFactsAndRun 用 t 在事实成功写入与运行创建之间同步插入真实修复事务。
// 测试通过既有 prepareTask 依赖控制交错，不使用 sleep、全局钩子或真实网络。
func TestCenter_OwnershipRepairBetweenFactsAndRun(t *testing.T) {
	// store、cleanup 保存本测试的 SQLite 仓储和连接清理职责。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于整段同步交错流程的数据库操作。
	ctx := context.Background()
	// admin、adminErr 提供同用户买卖账号和规则的管理用户身份。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil || admin == nil {
		t.Fatalf("读取夹具用户失败: %v", adminErr)
	}
	// saveErr 创建归属修复的目标卖家账号。
	if saveErr := store.Cookies.CreateOwned(ctx, "repair-seller", "", admin.ID); saveErr != nil {
		t.Fatal(saveErr)
	}
	// statusErr 启用旧账号，确保运行前确实经过规则匹配而非被停用门禁过滤。
	if statusErr := store.Cookies.SetStatus(ctx, "cid", true); statusErr != nil {
		t.Fatal(statusErr)
	}
	// ruleErr 创建可观察动作的评价规则，消息只发送到本地夹具。
	if _, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: admin.ID, CookieID: "cid", Name: "旧任务竞争", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "不应发送", Enabled: true}},
	}); ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// sender 保存本地消息副作用，迁移后旧任务不能再调用它。
	sender := &testSender{}
	// center 使用真实事件事实、规则及运行仓储，仅在准备返回处插入竞争者。
	center := New(store, testSenderProvider{sender: sender}, nil)
	// originalPrepare 保留生产准备逻辑，不能用测试替身跳过现有业务约束。
	originalPrepare := center.runs.prepareTask
	// repaired 证明精确的交错点实际被执行，避免测试因更早退出而假通过。
	repaired := false
	// preparationCtx、task 是运行准备阶段输入；回调在真实准备成功之后、TryStartRun 之前提交修复事务。
	center.runs.prepareTask = func(preparationCtx context.Context, task Task) (Task, error) {
		// prepared、prepareErr 保存既有准备结果，不改变准备失败语义。
		prepared, prepareErr := originalPrepare(preparationCtx, task)
		if prepareErr != nil {
			return prepared, prepareErr
		}
		// expected、readErr 读取刚由 facts.record 写入的归属及版本，不使用提前保存的过期证据。
		expected, readErr := store.Orders.FindOwnership(preparationCtx, admin.ID, task.OrderID)
		if readErr != nil {
			return prepared, readErr
		}
		// repairErr 模拟手动已售同步的归属恢复，事务内再次检查所有执行痕迹。
		repairErr := store.Orders.RecoverSoldOwnership(preparationCtx, admin.ID, "repair-seller", expected, db.OrderUpsertOpts{CookieID: "repair-seller", ItemID: task.ItemID, BuyerID: task.BuyerID})
		repaired = repairErr == nil
		return prepared, repairErr
	}
	// task 是已通过早期角色识别但在排队后失去订单归属的历史任务副本。
	task := Task{Source: "ws", AccountID: "cid", OrderID: "race-order", ItemID: "race-item", BuyerID: "cid", ChatID: "old-chat", TriggerType: TriggerBuyerReviewed}
	// handleErr 必须阻止迁移完成后的旧运行创建。
	handleErr := center.HandleTask(ctx, task)
	if !repaired || !errors.Is(handleErr, db.ErrForbidden) || len(sender.texts) != 0 {
		t.Fatalf("旧任务越过修复边界: 修复完成=%v 处理错误=%v 消息数=%d", repaired, handleErr, len(sender.texts))
	}
	// deferErr 验证暂停、准备失败等路径即使持有旧 Task，也不能在修复后留下新延期任务。
	if deferErr := center.deferTask(ctx, task, 0); !errors.Is(deferErr, db.ErrForbidden) {
		t.Fatalf("迁移后旧延期任务应被拒绝: %v", deferErr)
	}
	// table 是需要保持为空的运行与延期表，不删除历史痕迹来掩盖竞争。
	for _, table := range []string{"automation_runs", "automation_pending_tasks"} {
		// count 是测试中的执行痕迹总数。
		var count int
		// queryErr 保存固定测试表的计数错误。
		if queryErr := store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); queryErr != nil || count != 0 {
			t.Fatalf("迁移后旧任务留下 %s 痕迹: 数量=%d 错误=%v", table, count, queryErr)
		}
	}
}

// TestCenter_RecoverySnapshotMustMatchRun 用 t 验证恢复任务不能借用其他账号或订单的既有运行，合法恢复仍发送一次。
func TestCenter_RecoverySnapshotMustMatchRun(t *testing.T) {
	// store、cleanup 提供本地数据库及连接清理职责。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 管理本测试同步仓储及恢复流程。
	ctx := context.Background()
	// admin、adminErr 提供创建规则所需的管理用户身份。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil || admin == nil {
		t.Fatalf("读取恢复夹具用户失败: %v", adminErr)
	}
	// statusErr 保证合法恢复不会因账号停用而被提前跳过。
	if statusErr := store.Cookies.SetStatus(ctx, "cid", true); statusErr != nil {
		t.Fatal(statusErr)
	}
	// ruleID、createErr 创建仅向本地记录器发送消息的评价规则。
	ruleID, createErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: admin.ID, CookieID: "cid", Name: "恢复身份", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "合法恢复", Enabled: true}},
	})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// rule、readErr 保存待恢复规则的完整动作计划。
	rule, readErr := store.Automation.Get(ctx, ruleID)
	if readErr != nil || rule == nil {
		t.Fatalf("读取恢复规则失败: %v", readErr)
	}
	// runID、started、startErr 建立尚未执行动作的持久化运行，兼容本地订单稍后由准备阶段补齐。
	runID, started, startErr := store.Automation.TryStartRun(ctx, db.AutomationRun{RuleID: ruleID, CookieID: "cid", OrderID: "original-order", TriggerType: TriggerBuyerReviewed, TriggerKey: "recovery"})
	if startErr != nil || !started {
		t.Fatalf("建立恢复运行失败: %v", startErr)
	}
	// sender 保存可观察的消息副作用，不访问真实平台。
	sender := &testSender{}
	// center 使用完整生产准备及恢复路径，不替换身份检查依赖。
	center := New(store, testSenderProvider{sender: sender}, nil)
	// task 是依次尝试借用账号、借用订单及合法恢复的快照，合法项必须最后执行。
	for _, task := range []Task{
		{AccountID: "other-account", OrderID: ""},
		{AccountID: "cid", OrderID: "other-order"},
		{AccountID: "cid", OrderID: "original-order"},
	} {
		task.Source, task.TriggerType, task.UpdateKey = "scheduler", TriggerBuyerReviewed, "recovery"
		task.ChatID, task.BuyerID = "recovery-chat", "buyer"
		task.Raw = map[string]any{"automation_run_id": runID}
		// runErr 保存恢复执行结果，错误身份必须在领取动作之前被拒绝。
		runErr := center.executeRule(ctx, task, *rule)
		if task.AccountID != "cid" || task.OrderID != "original-order" {
			if !errors.Is(runErr, db.ErrForbidden) || len(sender.texts) != 0 {
				t.Fatalf("不一致恢复快照执行了动作: 错误=%v 消息数=%d", runErr, len(sender.texts))
			}
		} else if runErr != nil || len(sender.texts) != 1 {
			t.Fatalf("合法恢复未保留: 错误=%v 消息数=%d", runErr, len(sender.texts))
		}
	}
}
