package reconciliation

import (
	"context"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// nilReconciliationContext 返回用于验证补偿服务 nil Context 保护的空上下文接口。
func nilReconciliationContext() context.Context { return nil }

// TestServiceValidationAndRun 验证服务空依赖、数据库错误和后台扫描生命周期的安全边界。
func TestServiceValidationAndRun(t *testing.T) {
	// ctx 是空依赖校验使用的上下文。
	ctx := context.Background()
	// nilService 表示未初始化的补偿服务指针。
	var nilService *Service
	nilService.Run(ctx)
	if nilService.RunOnce(ctx) == nil {
		t.Fatal("nil service should return an initialization error")
	}
	// emptyStore 是字段未装配的补偿服务依赖。
	emptyStore := &db.Store{}
	// nilContextRun 覆盖字段未装配时的 nil Context 保护路径。
	New(emptyStore, nil).Run(nilReconciliationContext())
	if New(emptyStore, nil).RunOnce(ctx) == nil {
		t.Fatal("empty store should return an initialization error")
	}
	// database、dialect、err 保存后台扫描测试数据库及打开结果。
	database, dialect, err := db.Open(ctx, t.TempDir()+"/reconciliation-run.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	// store 是绑定迁移后数据库的补偿服务依赖。
	store := db.NewStore(database, dialect)
	// nilContextService 覆盖完整服务收到 nil 生命周期 Context 时立即返回的保护路径。
	New(store, nil).Run(nilReconciliationContext())
	// canceledContext、cancel 控制后台补偿扫描的退出生命周期。
	canceledContext, cancel := context.WithCancel(ctx)
	// service 是使用极短扫描间隔验证 ticker 分支的服务实例。
	service := New(store, nil)
	service.interval = time.Nanosecond
	// done 表示后台扫描 goroutine 已经退出。
	done := make(chan struct{})
	go func() {
		service.Run(canceledContext)
		close(done)
	}()
	time.Sleep(time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reconciliation service did not stop")
	}
	// closedDatabase、closedDialect、closeErr 保存关闭数据库后的查询错误场景。
	closedDatabase, closedDialect, closeErr := db.Open(ctx, t.TempDir()+"/reconciliation-closed.db")
	if closeErr != nil {
		t.Fatalf("Open closed database: %v", closeErr)
	}
	// closedStore 是底层连接已关闭的补偿服务依赖。
	closedStore := db.NewStore(closedDatabase, closedDialect)
	closedDatabase.Close()
	if New(closedStore, nil).RunOnce(ctx) == nil {
		t.Fatal("closed database should return a scan error")
	}
	// canceledContext 保存已取消的生命周期上下文，确保首次扫描错误后立即退出循环。
	canceledContext, cancelClosed := context.WithCancel(ctx)
	cancelClosed()
	New(closedStore, nil).Run(canceledContext)
}

// TestServiceReconcileRecordErrors 验证单条补偿的订单查询、订单写入和状态收尾错误路径。
func TestServiceReconcileRecordErrors(t *testing.T) {
	// ctx 是单条补偿错误场景使用的上下文。
	ctx := context.Background()
	// database、dialect、err 保存错误路径测试数据库及打开结果。
	database, dialect, err := db.Open(ctx, t.TempDir()+"/reconciliation-errors.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	// store 是错误路径共用的数据库访问聚合。
	store := db.NewStore(database, dialect)
	// service 是待验证单条补偿方法的服务实例。
	service := New(store, nil)
	// unsupportedRecord 表示不受支持的外部动作。
	unsupportedRecord := db.OrderReconciliation{Kind: "unsupported"}
	if service.reconcileRecord(ctx, unsupportedRecord) == nil {
		t.Fatal("unsupported reconciliation kind should fail")
	}
	// missingOrderRecord 表示查询不到本地订单的补偿记录。
	missingOrderRecord := db.OrderReconciliation{Kind: "manual_status_ship", OrderID: "missing-order"}
	if service.reconcileRecord(ctx, missingOrderRecord) == nil {
		t.Fatal("missing order should fail")
	}
	// err 表示插入缺少账号订单时的数据库错误。
	if _, err := database.ExecContext(ctx, `INSERT INTO orders(order_id,order_status,cookie_id) VALUES(?,?,NULL)`, "blank-cookie-order", "pending_ship"); err != nil {
		t.Fatalf("insert blank cookie order: %v", err)
	}
	// blankCookieRecord 表示本地订单存在但没有账号归属的补偿记录。
	blankCookieRecord := db.OrderReconciliation{Kind: "manual_status_ship", OrderID: "blank-cookie-order"}
	if service.reconcileRecord(ctx, blankCookieRecord) == nil {
		t.Fatal("order without cookie should fail")
	}
	// closedDatabase、closedDialect、closeErr 保存订单查询失败使用的关闭数据库。
	closedDatabase, closedDialect, closeErr := db.Open(ctx, t.TempDir()+"/reconciliation-order-closed.db")
	if closeErr != nil {
		t.Fatalf("Open order closed database: %v", closeErr)
	}
	// closedOrdersStore 是补偿记录数据库正常但订单仓储已关闭的组合依赖。
	closedOrdersStore := db.NewStore(database, dialect)
	// closedOrders 是底层连接已关闭的订单仓储。
	closedOrders := &db.Orders{DB: closedDatabase, Dialect: closedDialect}
	closedOrdersStore.Orders = closedOrders
	closedDatabase.Close()
	if New(closedOrdersStore, nil).reconcileRecord(ctx, db.OrderReconciliation{Kind: "manual_status_ship", OrderID: "any-order"}) == nil {
		t.Fatal("closed order repository should fail")
	}
	// adminID、err 保存创建订单归属账号的结果。
	if _, err := store.Users.Create(ctx, "reconcile-errors", "reconcile-errors@example.com", "pw"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	// admin、err 保存订单测试账号及查询错误。
	admin, err := store.Users.GetByUsername(ctx, "reconcile-errors")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	// err 表示保存订单测试账号 Cookie 的错误。
	if err := store.Cookies.Save(ctx, "reconcile-cookie", "cookie=1", admin.ID); err != nil {
		t.Fatalf("save cookie: %v", err)
	}
	// err 表示创建可触发订单写入错误的订单。
	if err := store.Orders.Upsert(ctx, "upsert-error-order", db.OrderUpsertOpts{CookieID: "reconcile-cookie", OrderStatus: "pending_ship"}); err != nil {
		t.Fatalf("create upsert error order: %v", err)
	}
	// err 表示创建订单写入失败触发器的错误。
	if _, err := database.ExecContext(ctx, `CREATE TRIGGER fail_reconcile_order_update BEFORE UPDATE ON orders BEGIN SELECT RAISE(ABORT,'forced order update failure'); END`); err != nil {
		t.Fatalf("create order trigger: %v", err)
	}
	// upsertErrorRecord 表示订单状态补偿写入会失败的记录。
	upsertErrorRecord := db.OrderReconciliation{Kind: "manual_status_ship", OrderID: "upsert-error-order"}
	if service.reconcileRecord(ctx, upsertErrorRecord) == nil {
		t.Fatal("order upsert failure should be returned")
	}
	// err 表示删除订单写入失败触发器的错误。
	if _, err := database.ExecContext(ctx, `DROP TRIGGER fail_reconcile_order_update`); err != nil {
		t.Fatalf("drop order trigger: %v", err)
	}
	// err 表示创建待补偿记录的错误。
	reconciliationID, err := store.Reconciliations.CreatePending(ctx, "upsert-error-order", "reconcile-cookie", "manual_status_ship", "pending")
	if err != nil {
		t.Fatalf("create reconciliation: %v", err)
	}
	// err 表示创建状态收尾失败触发器的错误。
	if _, err := database.ExecContext(ctx, `CREATE TRIGGER fail_reconcile_mark BEFORE UPDATE ON order_reconciliations BEGIN SELECT RAISE(ABORT,'forced mark failure'); END`); err != nil {
		t.Fatalf("create mark trigger: %v", err)
	}
	// markErrorRecord 表示订单写入成功但补偿记录收尾会失败的记录。
	markErrorRecord := db.OrderReconciliation{ID: reconciliationID, Kind: "manual_status_ship", OrderID: "upsert-error-order"}
	if service.reconcileRecord(ctx, markErrorRecord) == nil {
		t.Fatal("mark resolved failure should be returned")
	}
}

// TestServiceRunOnceRecordsAttemptError 验证失败次数写入失败时扫描仍然安全返回。
func TestServiceRunOnceRecordsAttemptError(t *testing.T) {
	// ctx 是补偿扫描错误记录使用的上下文。
	ctx := context.Background()
	// database、dialect、err 保存补偿扫描数据库及打开结果。
	database, dialect, err := db.Open(ctx, t.TempDir()+"/reconciliation-attempt-error.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	// store 是待注入失败触发器的数据库访问聚合。
	store := db.NewStore(database, dialect)
	// id、err 保存未知补偿动作记录及创建错误。
	id, err := store.Reconciliations.CreatePending(ctx, "attempt-order", "attempt-cookie", "unknown_kind", "initial")
	if err != nil {
		t.Fatalf("CreatePending: %v", err)
	}
	// err 表示创建失败次数写入触发器的错误。
	// triggerErr 保存创建失败次数写入触发器的数据库错误。
	if _, triggerErr := database.ExecContext(ctx, `CREATE TRIGGER fail_reconcile_attempt BEFORE UPDATE ON order_reconciliations BEGIN SELECT RAISE(ABORT,'forced attempt failure'); END`); triggerErr != nil {
		t.Fatalf("create attempt trigger: %v", triggerErr)
	}
	// err 表示扫描在记录失败次数写入失败后仍应返回 nil。
	// runErr 保存记录失败次数时扫描返回的错误；该场景应被服务吞掉。
	if runErr := New(store, nil).RunOnce(ctx); runErr != nil {
		t.Fatalf("RunOnce should tolerate RecordAttempt failure: %v", runErr)
	}
	// status、err 保存触发器失败后仍未解决的记录状态。
	var status string
	// statusErr 保存查询补偿记录最终状态时的数据库错误。
	if statusErr := database.QueryRowContext(ctx, `SELECT status FROM order_reconciliations WHERE id=?`, id).Scan(&status); statusErr != nil {
		t.Fatalf("query status: %v", statusErr)
	}
	if status != "pending" {
		t.Fatalf("status=%q", status)
	}
}

// TestServiceRunOnceReconcilesManualShipment 验证手动发货补偿会补齐本地订单状态并关闭 pending 记录。
func TestServiceRunOnceReconcilesManualShipment(t *testing.T) {
	// ctx 是本测试使用的数据库上下文。
	ctx := context.Background()
	// database、dialect、err 保存临时 SQLite 数据库及打开结果。
	database, dialect, err := db.Open(ctx, t.TempDir()+"/reconciliation.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	// store 是绑定最新迁移的数据库访问聚合。
	store := db.NewStore(database, dialect)
	// err 表示创建补偿测试用户失败。
	if _, err := store.Users.Create(ctx, "admin", "admin@example.com", "pw"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	// admin 保存补偿测试账号所属用户。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// err 表示创建补偿测试账号失败。
	if err := store.Cookies.Save(ctx, "acc-reconcile", "unb=1", admin.ID); err != nil {
		t.Fatalf("save cookie: %v", err)
	}
	// err 表示创建待补偿订单失败。
	if err := store.Orders.Upsert(ctx, "order-reconcile", db.OrderUpsertOpts{CookieID: "acc-reconcile", ItemID: "item-1", BuyerID: "buyer-1", ChatID: "chat-1", OrderStatus: "pending_ship"}); err != nil {
		t.Fatalf("create order: %v", err)
	}
	// id、err 保存待补偿记录标识及创建错误。
	id, err := store.Reconciliations.CreatePending(ctx, "order-reconcile", "acc-reconcile", "manual_status_ship", "本地订单写入失败")
	if err != nil {
		t.Fatalf("CreatePending: %v", err)
	}
	// service 是待执行的订单补偿服务。
	service := New(store, nil)
	// err 表示首次补偿执行错误。
	if err := service.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	// order、err 保存补偿后的订单及查询错误。
	order, err := store.Orders.Get(ctx, "order-reconcile")
	if err != nil || order.OrderStatus != "shipped" || !order.SystemShipped {
		t.Fatalf("reconciled order=%+v err=%v", order, err)
	}
	// pending、err 保存补偿后的待处理记录及查询错误。
	pending, err := store.Reconciliations.ListPending(ctx, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	// record、err 保存已完成补偿记录及查询错误。
	var status string
	// err 表示读取补偿完成状态的错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT status FROM order_reconciliations WHERE id=?`, id).Scan(&status); err != nil || status != "resolved" {
		t.Fatalf("status=%q err=%v", status, err)
	}
}

// TestServiceRunOnceRecordsRetryFailure 验证未知补偿类型失败后仍保留 pending 并递增尝试次数。
func TestServiceRunOnceRecordsRetryFailure(t *testing.T) {
	// ctx 是本测试使用的数据库上下文。
	ctx := context.Background()
	// database、dialect、err 保存临时 SQLite 数据库及打开结果。
	database, dialect, err := db.Open(ctx, t.TempDir()+"/reconciliation-failure.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	// store 是绑定最新迁移的数据库访问聚合。
	store := db.NewStore(database, dialect)
	// id、err 保存未知动作补偿记录标识及创建错误。
	id, err := store.Reconciliations.CreatePending(ctx, "unknown-order", "unknown-cookie", "unknown_kind", "初始错误")
	if err != nil {
		t.Fatalf("CreatePending: %v", err)
	}
	// err 表示未知补偿动作扫描错误。
	if err := New(store, nil).RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	// varAttempts、err 保存补偿失败后的尝试次数及查询错误。
	var attempts int
	// status 保存补偿失败后仍保留的 pending 状态。
	var status string
	// err 表示读取补偿重试状态的错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT attempts,status FROM order_reconciliations WHERE id=?`, id).Scan(&attempts, &status); err != nil {
		t.Fatalf("query retry: %v", err)
	}
	if attempts != 1 || status != "pending" {
		t.Fatalf("attempts=%d status=%s", attempts, status)
	}
}
