package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// TestStoreLockPricingModeBoundaries 覆盖改价互斥锁的空 Store 与正常释放边界。
func TestStoreLockPricingModeBoundaries(t *testing.T) {
	// nilStore 用于验证空 Store 不应要求调用方额外判空。
	var nilStore *Store
	// nilUnlock 用于验证空 Store 返回的释放函数可安全调用。
	nilUnlock := nilStore.LockPricingMode()
	nilUnlock()

	// store、cleanup 用于验证真实 Store 的锁获取与释放生命周期。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// unlock 用于释放本次测试持有的进程内互斥锁。
	unlock := store.LockPricingMode()
	unlock()
}

// TestCookiesIsPausedMissingAccount 覆盖查询不存在账号时的统一未找到错误。
func TestCookiesIsPausedMissingAccount(t *testing.T) {
	// store、cleanup 用于提供隔离数据库及其关闭逻辑。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// err 用于接收不存在账号的查询结果。
	_, _, err := store.Cookies.IsPaused(context.Background(), "missing-cookie")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("IsPaused missing error=%v", err)
	}
}

// TestResolveDeferredIssueDeleteAndMissing 覆盖死信任务删除及不存在任务的错误分支。
func TestResolveDeferredIssueDeleteAndMissing(t *testing.T) {
	// store、cleanup 用于提供隔离数据库及其关闭逻辑。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于控制本次数据库操作的生命周期。
	ctx := context.Background()
	// userID、cookieID 用于建立任务所属的账号边界。
	userID, cookieID := seedAccount(t, store)
	// task 用于创建后转为死信状态的延迟任务。
	task := DeferredAutomationTask{TaskKey: "delete-deferred", CookieID: cookieID, TriggerType: "paid", TaskJSON: `{}`, DueAt: 0}
	// err 用于接收延迟任务写入错误。
	if err := store.Automation.DeferTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	// taskID 用于定位刚写入的死信任务。
	var taskID int64
	// err 用于接收任务标识查询错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT id FROM automation_pending_tasks WHERE task_key=?`, task.TaskKey).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	// err 用于接收将任务转为死信状态的数据库错误。
	if _, err := store.DB.ExecContext(ctx, `UPDATE automation_pending_tasks SET status='dead_letter' WHERE id=?`, taskID); err != nil {
		t.Fatal(err)
	}
	// err 用于接收删除死信任务的业务错误。
	if err := store.Automation.ResolveDeferredIssue(ctx, userID, taskID, false); err != nil {
		t.Fatalf("delete deferred issue: %v", err)
	}
	// remaining 用于确认删除操作没有遗留同一任务。
	var remaining int
	// err 用于接收删除结果查询错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_pending_tasks WHERE id=?`, taskID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("deleted task count=%d", remaining)
	}
	// err 用于接收重复删除不存在任务的业务错误。
	if err := store.Automation.ResolveDeferredIssue(ctx, userID, taskID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing deferred issue error=%v", err)
	}
}

// TestRequestCancelMissingAndIdle 覆盖取消不存在批次与尚未运行批次的事务路径。
func TestRequestCancelMissingAndIdle(t *testing.T) {
	// store、cleanup 用于提供隔离数据库及其关闭逻辑。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于控制本次数据库操作的生命周期。
	ctx := context.Background()
	// err 用于接收不存在批次的查询结果。
	_, _, err := store.PublishBatches.RequestCancel(ctx, "missing-batch")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing batch error=%v", err)
	}
	// userID 用于建立批次所属用户的外键关系。
	userID, _ := seedAccount(t, store)
	// err 用于接收未运行批次的创建错误。
	if err := store.PublishBatches.Create(ctx, makePublishBatch(userID, "idle-cancel"), []ItemPublishBatchRow{{RowNo: 1, Title: "idle", Price: "1"}}); err != nil {
		t.Fatal(err)
	}
	// token、running、err 用于验证未运行批次会在事务中直接完成取消。
	token, running, err := store.PublishBatches.RequestCancel(ctx, "idle-cancel")
	if err != nil || running || token != "" {
		t.Fatalf("idle cancel token=%q running=%v err=%v", token, running, err)
	}
	// batch 用于确认批次已进入终态且不再保留工作器凭证。
	batch, err := store.PublishBatches.Get(ctx, userID, "idle-cancel")
	if err != nil || batch.Status != "canceled" || batch.WorkerToken != "" {
		t.Fatalf("idle canceled batch=%+v err=%v", batch, err)
	}
}

// TestAutomationRetryDelayAndBoolIntBoundaries 覆盖延迟上限、最小尝试次数和布尔持久化转换。
func TestAutomationRetryDelayAndBoolIntBoundaries(t *testing.T) {
	// cases 用于验证负数、普通值和超过上限的重试延迟。
	cases := []struct {
		// attempt 表示自动化任务当前尝试次数。
		attempt int
		// wantMinutes 表示期望的退避分钟数。
		wantMinutes int
	}{
		{attempt: 0, wantMinutes: 5},
		{attempt: 1, wantMinutes: 5},
		{attempt: 3, wantMinutes: 20},
		{attempt: 9, wantMinutes: 60},
	}
	// testCase 表示当前待验证的重试延迟样例。
	for _, testCase := range cases {
		// gotMinutes 表示当前尝试次数计算出的退避分钟数。
		gotMinutes := int(deferredRetryDelay(testCase.attempt).Minutes())
		if gotMinutes != testCase.wantMinutes {
			t.Fatalf("attempt=%d delay=%d want=%d", testCase.attempt, gotMinutes, testCase.wantMinutes)
		}
	}
	if boolInt(true) != 1 || boolInt(false) != 0 {
		t.Fatal("boolInt must map true/false to 1/0")
	}
}

// TestFirstBatchDataBoundaries 覆盖批量卡券不存在、空库存和无有效行的读取错误。
func TestFirstBatchDataBoundaries(t *testing.T) {
	// store、cleanup 用于提供隔离数据库及其关闭逻辑。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于控制本次数据库操作的生命周期。
	ctx := context.Background()
	// userID 用于建立卡券所属用户的外键关系。
	userID, _ := seedAccount(t, store)
	// missingErr 用于接收不存在卡券的查询错误。
	_, _, missingErr := store.Cards.FirstBatchData(ctx, 99999)
	if !errors.Is(missingErr, ErrNotFound) {
		t.Fatalf("missing card error=%v", missingErr)
	}
	// emptyID、emptyErr 用于验证空 data 内容的业务错误。
	emptyID, emptyErr := store.Cards.Create(ctx, &CardFull{Name: "empty-data", Type: "data", Enabled: true, UserID: userID})
	if emptyErr != nil {
		t.Fatal(emptyErr)
	}
	// emptyReadErr 用于接收空库存读取错误。
	_, _, emptyReadErr := store.Cards.FirstBatchData(ctx, emptyID)
	if emptyReadErr == nil || !strings.Contains(emptyReadErr.Error(), "为空") {
		t.Fatalf("empty data error=%v", emptyReadErr)
	}
	// invalidID、invalidErr 用于验证只含空行的库存内容错误。
	invalidID, invalidErr := store.Cards.Create(ctx, &CardFull{Name: "invalid-data", Type: "data", DataContent: "\n\r\n", Enabled: true, UserID: userID})
	if invalidErr != nil {
		t.Fatal(invalidErr)
	}
	// invalidReadErr 用于接收无有效库存行的读取错误。
	_, _, invalidReadErr := store.Cards.FirstBatchData(ctx, invalidID)
	if invalidReadErr == nil || !strings.Contains(invalidReadErr.Error(), "无有效行") {
		t.Fatalf("invalid data error=%v", invalidReadErr)
	}
}

// TestGetOrCreateDeviceIDRejectsEmptyCandidate 覆盖设备标识候选为空时的输入校验。
func TestGetOrCreateDeviceIDRejectsEmptyCandidate(t *testing.T) {
	// store、cleanup 用于提供隔离数据库及其关闭逻辑。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// err 用于接收空设备标识候选的输入错误。
	_, err := store.Tokens.GetOrCreateDeviceID(context.Background(), "cookie", "")
	if err == nil || !strings.Contains(err.Error(), "device_id") {
		t.Fatalf("empty candidate error=%v", err)
	}
}

// TestRepositoryInputAndTerminalBoundaries 覆盖账号创建输入校验、补偿记录不存在和报价终态错误。
func TestRepositoryInputAndTerminalBoundaries(t *testing.T) {
	// store、cleanup 用于提供隔离数据库及其关闭逻辑。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于控制本次数据库操作的生命周期。
	ctx := context.Background()
	// err 表示空账号标识输入校验错误。
	if err := store.Cookies.CreateOwned(ctx, "", "cookie", 1); err == nil {
		t.Fatal("empty cookie ID must fail")
	}
	// err 表示无效用户标识输入校验错误。
	if err := store.Cookies.CreateOwned(ctx, "cookie", "cookie", 0); err == nil {
		t.Fatal("non-positive user ID must fail")
	}
	// attemptErr 用于接收不存在补偿记录的更新错误。
	attemptErr := store.Reconciliations.RecordAttempt(ctx, "missing-reconciliation", "failure")
	if !errors.Is(attemptErr, sql.ErrNoRows) {
		t.Fatalf("missing reconciliation error=%v", attemptErr)
	}
	// quoteErr 用于接收不存在报价的终态更新错误。
	quoteErr := store.AIReply.FinishQuote(ctx, 99999, "success", "")
	if !errors.Is(quoteErr, ErrNotFound) {
		t.Fatalf("missing quote error=%v", quoteErr)
	}
}

// TestInsertReturningIDAcrossDialectBranches 覆盖自增主键辅助函数的 SQLite、RETURNING 和 SQL 错误路径。
func TestInsertReturningIDAcrossDialectBranches(t *testing.T) {
	// store、cleanup 用于提供隔离数据库及其关闭逻辑。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于控制本次数据库操作的生命周期。
	ctx := context.Background()
	// err 用于接收测试表创建错误。
	if _, err := store.DB.ExecContext(ctx, `CREATE TABLE coverage_insert_returning (id INTEGER PRIMARY KEY AUTOINCREMENT, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	// sqliteID、sqliteErr 保存 SQLite LastInsertId 分支的结果。
	sqliteID, sqliteErr := insertReturningID(ctx, store.DB, DialectSQLite, `INSERT INTO coverage_insert_returning (value) VALUES (?)`, "sqlite")
	if sqliteErr != nil || sqliteID != 1 {
		t.Fatalf("sqlite id=%d err=%v", sqliteID, sqliteErr)
	}
	// returningID、returningErr 保存 RETURNING 分支的结果。
	returningID, returningErr := insertReturningID(ctx, store.DB, DialectPostgres, `INSERT INTO coverage_insert_returning (value) VALUES (?)`, "returning")
	if returningErr != nil || returningID != 2 {
		t.Fatalf("returning id=%d err=%v", returningID, returningErr)
	}
	// _, invalidErr 接收非法 INSERT 语句的数据库错误。
	_, invalidErr := insertReturningID(ctx, store.DB, DialectSQLite, `INSERT INTO coverage_insert_returning (missing) VALUES (?)`, "invalid")
	if invalidErr == nil {
		t.Fatal("invalid insert must return an error")
	}
}

// TestLegacyTokenAndNotificationMigrationBranches 覆盖历史令牌与通知配置的加密迁移及幂等重跑。
func TestLegacyTokenAndNotificationMigrationBranches(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "legacy-migration-coverage-key")
	// store、cleanup 用于提供隔离数据库及其关闭逻辑。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于控制本次迁移事务的生命周期。
	ctx := context.Background()
	// userID、cookieID 用于建立令牌与通知渠道的外键关系。
	userID, cookieID := seedAccount(t, store)
	// err 用于接收历史令牌明文写入错误。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO account_tokens (cookie_id,device_id,access_token) VALUES (?,?,?)`, cookieID, "legacy-device", "legacy-token"); err != nil {
		t.Fatal(err)
	}
	// err 用于接收历史通知渠道明文写入错误。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES (?,?,?,?,?)`, "legacy-channel", "webhook", `{"url":"legacy-url"}`, 1, userID); err != nil {
		t.Fatal(err)
	}
	// codec 保存当前迁移使用的秘密编解码器。
	codec := secretCodecFromEnvironment()
	// tx、err 保存首次历史秘密迁移事务及其错误。
	tx, err := store.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// err 表示首次令牌迁移的执行错误。
	if err := migrateLegacyTokens(ctx, tx, codec); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	// err 表示首次通知渠道迁移的执行错误。
	if err := migrateLegacyNotificationChannels(ctx, tx, codec); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	// err 表示首次迁移事务提交错误。
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// tokenValue、channelConfig 保存迁移后的持久化文本，验证不再保留明文。
	var tokenValue, channelConfig string
	// err 表示迁移后令牌密文查询错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT device_id||':'||access_token FROM account_tokens WHERE cookie_id=?`, cookieID).Scan(&tokenValue); err != nil {
		t.Fatal(err)
	}
	// err 表示迁移后通知配置密文查询错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT config FROM notification_channels WHERE name=?`, "legacy-channel").Scan(&channelConfig); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tokenValue, "legacy-device") || strings.Contains(tokenValue, "legacy-token") || strings.Contains(channelConfig, "legacy-url") {
		t.Fatal("legacy secrets were not encrypted")
	}
	// rerunTx、rerunErr 保存已迁移数据幂等重跑事务及其错误。
	rerunTx, rerunErr := store.DB.BeginTx(ctx, nil)
	if rerunErr != nil {
		t.Fatal(rerunErr)
	}
	// err 表示幂等重跑的令牌迁移错误。
	if err := migrateLegacyTokens(ctx, rerunTx, codec); err != nil {
		_ = rerunTx.Rollback()
		t.Fatal(err)
	}
	// err 表示幂等重跑的通知渠道迁移错误。
	if err := migrateLegacyNotificationChannels(ctx, rerunTx, codec); err != nil {
		_ = rerunTx.Rollback()
		t.Fatal(err)
	}
	// err 表示幂等重跑事务提交错误。
	if err := rerunTx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// TestTryStartRunPostgresReturningAndReclaimBranches 使用 SQLite 的 RETURNING 兼容语法覆盖 PostgreSQL 分支。
func TestTryStartRunPostgresReturningAndReclaimBranches(t *testing.T) {
	// store、cleanup 用于提供隔离数据库及其关闭逻辑。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于控制本次运行记录操作的生命周期。
	ctx := context.Background()
	// userID、cookieID 用于建立规则所属用户与账号关系。
	userID, cookieID := seedAccount(t, store)
	// ruleID、ruleErr 保存测试规则创建结果。
	ruleID, ruleErr := store.Automation.Create(ctx, makeAutomationRule(cookieID, userID, "", "paid", true, 0))
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// postgresRules 使用 PostgreSQL 方言分支连接 SQLite 兼容测试库。
	postgresRules := &AutomationRules{DB: store.DB, Dialect: DialectPostgres, codec: secretCodecFromEnvironment()}
	// run 保存首次插入使用的自动化运行输入。
	run := AutomationRun{RuleID: ruleID, CookieID: cookieID, TriggerType: "paid", TriggerKey: "postgres-returning"}
	// runID、started、err 保存 RETURNING 插入结果。
	runID, started, err := postgresRules.TryStartRun(ctx, run)
	if err != nil || !started || runID <= 0 {
		t.Fatalf("postgres returning run=%d started=%v err=%v", runID, started, err)
	}
	// err 表示让首次运行租约失效的更新错误。
	if _, err := store.DB.ExecContext(ctx, `UPDATE automation_runs SET lease_expires_at=0 WHERE id=?`, runID); err != nil {
		t.Fatal(err)
	}
	// reclaimedID、reclaimed、reclaimErr 保存重复触发的租约回收结果。
	reclaimedID, reclaimed, reclaimErr := postgresRules.TryStartRun(ctx, run)
	if reclaimErr != nil || !reclaimed || reclaimedID != runID {
		t.Fatalf("postgres reclaim run=%d started=%v err=%v", reclaimedID, reclaimed, reclaimErr)
	}
}

// TestNotificationUpdateAndCredentialWakeErrorBranches 覆盖通知渠道归属回读和凭证唤醒数据库错误。
func TestNotificationUpdateAndCredentialWakeErrorBranches(t *testing.T) {
	// store、cleanup 用于提供隔离数据库及其关闭逻辑。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于控制本次数据库操作的生命周期。
	ctx := context.Background()
	// userID 用于建立通知渠道所属用户关系。
	userID, _ := seedAccount(t, store)
	// channelID、createErr 保存通知渠道创建结果。
	channelID, createErr := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{Name: "update-channel", Type: "webhook", Config: `{}`, Enabled: true, UserID: userID})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// updateErr 保存缺少用户标识时由仓储回读归属后的更新错误。
	updateErr := store.Notifications.UpdateChannel(ctx, &NotificationChannelRow{ID: channelID, Name: "updated-channel", Type: "webhook", Config: `{}`, Enabled: false})
	if updateErr != nil {
		t.Fatalf("update channel without user=%v", updateErr)
	}
	// closedStore、closedCleanup 提供确定返回数据库错误的自动化仓储。
	closedStore, closedCleanup := newTestDB(t)
	// err 表示关闭错误测试数据库连接时的错误。
	if err := closedStore.DB.Close(); err != nil {
		t.Fatal(err)
	}
	// wakeErr 保存凭证唤醒在数据库关闭后的错误。
	wakeErr := closedStore.Automation.WakeCredentialBlocked(ctx, "cookie")
	if wakeErr == nil {
		t.Fatal("closed database wake must fail")
	}
	closedCleanup()
}
