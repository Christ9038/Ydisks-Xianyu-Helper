package reconciliation

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

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
