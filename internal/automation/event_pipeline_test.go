package automation

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestActionPlannerPaidEventKeepsCardBeforeShipment 验证付款事件只生成匹配卡密动作并保持发货顺序。
func TestActionPlannerPaidEventKeepsCardBeforeShipment(t *testing.T) {
	// task 是包含规格事实的付款事件。
	task := Task{TriggerType: TriggerOrderPaid, SpecName: "颜色", SpecValue: "蓝", Quantity: "2"}
	// actions 是待匹配的规则动作，故意包含一个规格不匹配的卡密动作。
	actions := []db.AutomationAction{
		{ID: 1, ActionType: ActionConfirmShipment, Enabled: true},
		{ID: 2, ActionType: ActionSendCard, Enabled: true, ConfigJSON: `{"spec_name":"颜色","spec_value":"蓝"}`},
		{ID: 3, ActionType: ActionSendCard, Enabled: true, ConfigJSON: `{"spec_name":"颜色","spec_value":"红"}`},
	}
	// original 用于确认规划过程不会修改规则动作输入。
	original := append([]db.AutomationAction(nil), actions...)
	// planner 是不执行外部 I/O 的纯动作计划组件。
	planner := actionPlanner{}
	// plan 是按发卡优先规则生成的不可变动作快照。
	plan := planner.plan(task, actions)
	if // got 用于本次流程后续判断的got
	got := []int64{plan[0].ID, plan[1].ID}; !reflect.DeepEqual(got, []int64{2, 1}) {
		t.Fatalf("付款事件动作顺序=%v，want [2 1]", got)
	}
	if !reflect.DeepEqual(actions, original) {
		t.Fatal("动作计划不应修改规则动作输入")
	}
}

// TestActionPlannerMultiSKUSelectsOnlyExactCombination 验证多 SKU 订单不会因首个维度相同而误发其他组合的卡密。
func TestActionPlannerMultiSKUSelectsOnlyExactCombination(t *testing.T) {
	// task 保存包含颜色和尺码两个维度的订单事实。
	task := Task{TriggerType: TriggerOrderPaid, SpecName: "颜色；尺码", SpecValue: "红色；M"}
	// actions 保存同色不同尺码和精确组合动作，检验完整规格边界。
	actions := []db.AutomationAction{
		{ID: 1, ActionType: ActionSendCard, Enabled: true, ConfigJSON: `{"spec_name":"颜色；尺码","spec_value":"红色；L"}`},
		{ID: 2, ActionType: ActionSendCard, Enabled: true, ConfigJSON: `{"spec_name":"颜色；尺码","spec_value":"红色；M"}`},
		{ID: 4, ActionType: ActionConfirmShipment, Enabled: true},
	}
	// plan 保存按规格精确匹配后的动作顺序。
	plan := (actionPlanner{}).plan(task, actions)
	// got 保存精确组合和确认动作的规划顺序。
	if got := []int64{plan[0].ID, plan[1].ID}; !reflect.DeepEqual(got, []int64{2, 4}) {
		t.Fatalf("多 SKU 动作计划=%v want [2 4]", got)
	}
}

// TestEventFactRecorderWithoutOrderIsNoOp 验证没有订单事实时记录组件不执行任何持久化动作。
func TestEventFactRecorderWithoutOrderIsNoOp(t *testing.T) {
	// recorder 未注入数据库时应对无订单任务安全忽略。
	recorder := newEventFactRecorder(nil)
	if // err 用于本次流程后续判断的err
	err := recorder.record(context.Background(), Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed}); err != nil {
		t.Fatalf("无订单事实应安全忽略，err=%v", err)
	}
}

// TestEventFactRecorderPersistsPaidAndReviewedFacts 验证付款与评价事件会写入订单事实及对应事件时间。
func TestEventFactRecorderPersistsPaidAndReviewedFacts(t *testing.T) {
	// ctx 保存本地数据库测试共用的上下文。
	ctx := context.Background()
	// database、dialect、openErr 保存内存隔离数据库的打开结果。
	database, dialect, openErr := db.Open(ctx, filepath.Join(t.TempDir(), "automation-events.db"))
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer database.Close()
	// store 保存事件事实记录使用的应用仓储集合。
	store := db.NewStore(database, dialect)
	// created、createErr 保存事件测试所需管理员用户的创建结果。
	created, createErr := store.Users.Create(ctx, "event-admin", "event-admin@example.com", "event-password")
	if createErr != nil || !created {
		t.Fatalf("创建事件测试用户失败 created=%v err=%v", created, createErr)
	}
	// user、userErr 保存事件测试用户的读取结果。
	user, userErr := store.Users.GetByUsername(ctx, "event-admin")
	if userErr != nil || user == nil {
		t.Fatalf("读取事件测试用户失败 user=%+v err=%v", user, userErr)
	}
	// saveCookieErr 保存事件测试账号的凭证归属写入错误。
	if saveCookieErr := store.Cookies.Save(ctx, "account-1", "event-cookie", user.ID); saveCookieErr != nil {
		t.Fatalf("保存事件测试账号失败: %v", saveCookieErr)
	}
	// recorder 保存使用本地数据库的事件事实记录组件。
	recorder := newEventFactRecorder(store)
	// paidErr 保存付款事件事实写入错误。
	paidErr := recorder.record(ctx, Task{AccountID: "account-1", OrderID: "order-paid", ItemID: "item-1", BuyerID: "buyer-1", ChatID: "chat-1", TriggerType: TriggerOrderPaid, OrderStatus: "paid", Quantity: "1", Amount: "2.00"})
	if paidErr != nil {
		t.Fatalf("付款事实写入失败: %v", paidErr)
	}
	// reviewedErr 保存评价事件事实写入错误。
	reviewedErr := recorder.record(ctx, Task{AccountID: "account-1", OrderID: "order-reviewed", ItemID: "item-2", BuyerID: "buyer-2", ChatID: "chat-2", TriggerType: TriggerBuyerReviewed, OrderStatus: "reviewed", Quantity: "1", Amount: "3.00"})
	if reviewedErr != nil {
		t.Fatalf("评价事实写入失败: %v", reviewedErr)
	}
	// paidOrder、paidReadErr 保存付款订单读取结果。
	paidOrder, paidReadErr := store.Orders.Get(ctx, "order-paid")
	if paidReadErr != nil || paidOrder.PaidAt == "" {
		t.Fatalf("付款订单事实异常 order=%+v err=%v", paidOrder, paidReadErr)
	}
	// reviewedOrder、reviewedReadErr 保存评价订单读取结果。
	reviewedOrder, reviewedReadErr := store.Orders.Get(ctx, "order-reviewed")
	if reviewedReadErr != nil || reviewedOrder.BuyerReviewedAt == "" {
		t.Fatalf("评价订单事实异常 order=%+v err=%v", reviewedOrder, reviewedReadErr)
	}
	// paidSeedErr 保存付款事件错误分支预置订单的写入错误。
	if paidSeedErr := store.Orders.Upsert(ctx, "order-paid-error", db.OrderUpsertOpts{CookieID: "account-1", OrderStatus: "paid"}); paidSeedErr != nil {
		t.Fatal(paidSeedErr)
	}
	// paidTriggerErr 保存阻断付款时间更新的 SQLite 触发器创建错误。
	if _, paidTriggerErr := database.ExecContext(ctx, `CREATE TRIGGER paid_event_failure BEFORE UPDATE OF paid_at ON orders BEGIN SELECT RAISE(ABORT, 'paid event failure'); END`); paidTriggerErr != nil {
		t.Fatal(paidTriggerErr)
	}
	// paidRecordErr 保存付款时间写入失败时的业务阶段错误。
	paidRecordErr := recorder.record(ctx, Task{AccountID: "account-1", OrderID: "order-paid-error", TriggerType: TriggerOrderPaid})
	if paidRecordErr == nil || !strings.Contains(paidRecordErr.Error(), "记录订单付款时间") {
		t.Fatalf("付款时间错误未包装: %v", paidRecordErr)
	}
	// reviewedSeedErr 保存评价事件错误分支预置订单的写入错误。
	if reviewedSeedErr := store.Orders.Upsert(ctx, "order-reviewed-error", db.OrderUpsertOpts{CookieID: "account-1", OrderStatus: "reviewed"}); reviewedSeedErr != nil {
		t.Fatal(reviewedSeedErr)
	}
	// reviewedTriggerErr 保存阻断评价时间更新的 SQLite 触发器创建错误。
	if _, reviewedTriggerErr := database.ExecContext(ctx, `CREATE TRIGGER reviewed_event_failure BEFORE UPDATE OF buyer_reviewed_at ON orders BEGIN SELECT RAISE(ABORT, 'reviewed event failure'); END`); reviewedTriggerErr != nil {
		t.Fatal(reviewedTriggerErr)
	}
	// reviewedRecordErr 保存评价时间写入失败时的业务阶段错误。
	reviewedRecordErr := recorder.record(ctx, Task{AccountID: "account-1", OrderID: "order-reviewed-error", TriggerType: TriggerBuyerReviewed})
	if reviewedRecordErr == nil || !strings.Contains(reviewedRecordErr.Error(), "记录买家评价时间") {
		t.Fatalf("评价时间错误未包装: %v", reviewedRecordErr)
	}
}

// TestEventFactRecorderWrapsPersistenceFailure 验证订单事实持久化失败会保留业务阶段上下文。
func TestEventFactRecorderWrapsPersistenceFailure(t *testing.T) {
	// ctx 保存已取消的数据库操作上下文。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// database、dialect、openErr 保存本地数据库打开结果。
	database, dialect, openErr := db.Open(context.Background(), filepath.Join(t.TempDir(), "automation-events-error.db"))
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer database.Close()
	// store 保存事件事实记录使用的数据库仓储集合。
	store := db.NewStore(database, dialect)
	// recordErr 保存取消上下文导致的订单事实写入错误。
	recordErr := newEventFactRecorder(store).record(ctx, Task{AccountID: "account-1", OrderID: "order-error", TriggerType: TriggerOrderPaid})
	if recordErr == nil || !strings.Contains(recordErr.Error(), "记录自动化事件订单事实") {
		t.Fatalf("持久化错误未保留阶段上下文: %v", recordErr)
	}
}

// TestRuleMatcherCoversRunSnapshotAndNormalLookup 验证恢复运行快照、非运行状态和普通规则匹配路径。
func TestRuleMatcherCoversRunSnapshotAndNormalLookup(t *testing.T) {
	// ctx 保存规则匹配数据库测试共用的上下文。
	ctx := context.Background()
	// database、dialect、openErr 保存规则匹配本地数据库的打开结果。
	database, dialect, openErr := db.Open(ctx, filepath.Join(t.TempDir(), "automation-matcher.db"))
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer database.Close()
	// store 保存规则匹配使用的仓储集合。
	store := db.NewStore(database, dialect)
	// created、createErr 保存规则匹配用户的创建结果。
	created, createErr := store.Users.Create(ctx, "matcher-admin", "matcher-admin@example.com", "matcher-password")
	if createErr != nil || !created {
		t.Fatalf("创建匹配用户失败 created=%v err=%v", created, createErr)
	}
	// user、userErr 保存规则匹配用户的读取结果。
	user, userErr := store.Users.GetByUsername(ctx, "matcher-admin")
	if userErr != nil || user == nil {
		t.Fatalf("读取匹配用户失败 user=%+v err=%v", user, userErr)
	}
	// cookieErr 保存规则匹配账号的归属写入错误。
	if cookieErr := store.Cookies.Save(ctx, "matcher-account", "matcher-cookie", user.ID); cookieErr != nil {
		t.Fatalf("保存匹配账号失败: %v", cookieErr)
	}
	// ruleID、ruleErr 保存普通规则创建结果。
	ruleID, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: user.ID, CookieID: "matcher-account", ItemID: "matcher-item", Name: "匹配规则", TriggerType: TriggerOrderPaid, Enabled: true, Priority: 1})
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// runID、started、runErr 保存恢复运行创建结果。
	runID, started, runErr := store.Automation.TryStartRun(ctx, db.AutomationRun{RuleID: ruleID, CookieID: "matcher-account", ItemID: "matcher-item", TriggerType: TriggerOrderPaid, TriggerKey: "matcher-run"})
	if runErr != nil || !started {
		t.Fatalf("创建匹配运行失败 id=%d started=%v err=%v", runID, started, runErr)
	}
	// matcher 保存使用本地数据库的规则匹配组件。
	matcher := newRuleMatcher(store)
	// snapshotRules、snapshotErr 保存恢复运行快照匹配结果。
	snapshotRules, snapshotErr := matcher.match(ctx, Task{Raw: map[string]any{"automation_run_id": runID}})
	if snapshotErr != nil || len(snapshotRules) != 1 || snapshotRules[0].ID != ruleID {
		t.Fatalf("运行快照匹配异常 rules=%+v err=%v", snapshotRules, snapshotErr)
	}
	// missingRules、missingErr 保存不存在运行快照的匹配结果。
	missingRules, missingErr := matcher.match(ctx, Task{Raw: map[string]any{"automation_run_id": runID + 99999}})
	if missingErr == nil || missingRules != nil {
		t.Fatalf("不存在运行快照结果异常 rules=%+v err=%v", missingRules, missingErr)
	}
	// finishErr 保存结束运行快照的数据库错误。
	if finishErr := store.Automation.FinishRun(ctx, runID, 1, "completed", 0, ""); finishErr != nil {
		t.Fatal(finishErr)
	}
	// stoppedRules、stoppedErr 保存非运行状态快照匹配结果。
	stoppedRules, stoppedErr := matcher.match(ctx, Task{Raw: map[string]any{"automation_run_id": runID}})
	if stoppedErr != nil || stoppedRules != nil {
		t.Fatalf("非运行快照结果异常 rules=%+v err=%v", stoppedRules, stoppedErr)
	}
	// normalRules、normalErr 保存普通事件规则匹配结果。
	normalRules, normalErr := matcher.match(ctx, Task{AccountID: "matcher-account", ItemID: "matcher-item", TriggerType: TriggerOrderPaid})
	if normalErr != nil || len(normalRules) != 1 || normalRules[0].ID != ruleID {
		t.Fatalf("普通规则匹配异常 rules=%+v err=%v", normalRules, normalErr)
	}
	// secondRunID、secondStarted、secondRunErr 保存规则删除错误分支所需的第二条运行。
	secondRunID, secondStarted, secondRunErr := store.Automation.TryStartRun(ctx, db.AutomationRun{RuleID: ruleID, CookieID: "matcher-account", ItemID: "matcher-item", TriggerType: TriggerOrderPaid, TriggerKey: "matcher-deleted-rule"})
	if secondRunErr != nil || !secondStarted {
		t.Fatalf("创建第二条匹配运行失败 id=%d started=%v err=%v", secondRunID, secondStarted, secondRunErr)
	}
	// deleteRuleErr 保存直接标记规则删除的数据库错误，模拟运行快照引用已消失规则。
	if _, deleteRuleErr := database.ExecContext(ctx, "UPDATE automation_rules SET deleted_at=CURRENT_TIMESTAMP, enabled=0 WHERE id=?", ruleID); deleteRuleErr != nil {
		t.Fatal(deleteRuleErr)
	}
	// deletedRuleRules、deletedRuleErr 保存运行快照找不到规则时的匹配结果。
	deletedRuleRules, deletedRuleErr := matcher.match(ctx, Task{Raw: map[string]any{"automation_run_id": secondRunID}})
	if deletedRuleErr == nil || deletedRuleRules != nil {
		t.Fatalf("删除规则快照结果异常 rules=%+v err=%v", deletedRuleRules, deletedRuleErr)
	}
	// nilRules、nilErr 保存未装配自动化仓储时的安全结果。
	nilRules, nilErr := newRuleMatcher(nil).match(ctx, Task{TriggerType: TriggerOrderPaid})
	if nilErr != nil || nilRules != nil {
		t.Fatalf("空规则匹配器结果异常 rules=%+v err=%v", nilRules, nilErr)
	}
}
