package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// ownershipRaceExecer 在实际写入前注入另一写入者；测试协程同步执行，不使用全局钩子。
type ownershipRaceExecer struct {
	// database 提供真实 SQLite 查询和写入，复现检查与写入之间的数据变化。
	database *sql.DB
	// beforeWrite 由测试提供一次性竞争操作；query 决定需要截获的写入位置。
	beforeWrite func(query string)
}

// QueryRowContext 将 ctx、query 和 args 原样交给测试数据库，返回真实查询行。
func (race *ownershipRaceExecer) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return race.database.QueryRowContext(ctx, query, args...)
}

// ExecContext 在 ctx 管理的 query 写入之前运行竞争回调，再按 args 执行并返回数据库结果或错误。
func (race *ownershipRaceExecer) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	race.beforeWrite(query)
	return race.database.ExecContext(ctx, query, args...)
}

// TestOrdersBatchConcurrentInsertCannotSteal 验证 t 中批量预检查后出现其他账号订单时必须报错且保留其事实。
func TestOrdersBatchConcurrentInsertCannotSteal(t *testing.T) {
	// store、cleanup 提供独立数据库及连接释放职责。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 贯穿本测试同步数据库操作。
	ctx := context.Background()
	// userID 提供两个虚构账号的管理用户外键。
	userID, _ := seedAccount(t, store)
	// accountID 是本用例中互相竞争的虚构账号。
	for _, accountID := range []string{"other", "seller"} {
		// err 保存账号夹具创建结果。
		if err := store.Cookies.CreateOwned(ctx, accountID, "fixture", userID); err != nil {
			t.Fatal(err)
		}
	}
	// raced 保证只在第一条订单 INSERT 前模拟一次并发插入。
	raced := false
	// execer 的 query 回调在前置检查完成后插入其他账号的同号订单。
	execer := &ownershipRaceExecer{database: store.DB, beforeWrite: func(query string) {
		if raced || !strings.Contains(query, "INTO orders") {
			return
		}
		raced = true
		// err 保存另一写入者创建订单的结果；测试夹具不依赖真实账号。
		_, err := store.DB.ExecContext(ctx, `INSERT INTO orders(order_id,cookie_id,order_status,version) VALUES('race-order','other','completed',1)`)
		if err != nil {
			t.Fatal(err)
		}
	}}
	// err 必须明确拒绝冲突，不能让上层将未写入订单计为成功。
	err := upsertManyOrders(ctx, execer, DialectSQLite, []BatchOrderUpsert{{OrderID: "race-order", Options: OrderUpsertOpts{CookieID: "seller", OrderStatus: "paid"}}})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("并发插入必须拒绝归属抢占，实际错误=%v", err)
	}
	// account、status 读取并发写入者的非敏感事实，以验证未被候选值覆盖。
	var account, status string
	// err 保存竞争后实际归属读取错误，断言不读取凭证或收货字段。
	if err := store.DB.QueryRowContext(ctx, `SELECT cookie_id,order_status FROM orders WHERE order_id='race-order'`).Scan(&account, &status); err != nil {
		t.Fatal(err)
	}
	if account != "other" || status != "completed" {
		t.Fatal("并发插入者的归属或状态被覆盖")
	}
}

// TestOrdersBatchConcurrentOwnershipChange 验证 t 中读取版本后即使旧写入者未推进版本，UPDATE 本身仍保护账号归属。
func TestOrdersBatchConcurrentOwnershipChange(t *testing.T) {
	// store、cleanup 提供独立数据库和连接释放。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// userID、expected、options 提供两个已存在账号及同号订单。
	userID, expected, options := seedOwnershipRepair(t, store)
	// ctx 控制同步数据库操作，没有后台协程。
	ctx := context.Background()
	// raced 确保只模拟一次旧式不递增版本的账号变化。
	raced := false
	// execer 的 query 回调在候选 UPDATE 执行前切换账号，复现检查之后的竞争窗口。
	execer := &ownershipRaceExecer{database: store.DB, beforeWrite: func(query string) {
		if raced || !strings.HasPrefix(query, "UPDATE orders SET") {
			return
		}
		raced = true
		// err 保存模拟并发账号变更错误；故意保持旧版本以证明 cookie 条件不可省略。
		if _, err := store.DB.ExecContext(ctx, `UPDATE orders SET cookie_id=? WHERE order_id=?`, options.CookieID, expected.OrderID); err != nil {
			t.Fatal(err)
		}
	}}
	// err 保存原账号基于过期归属写入的结果，不能将冲突作为成功返回。
	err := upsertManyOrders(ctx, execer, DialectSQLite, []BatchOrderUpsert{{OrderID: expected.OrderID, Options: OrderUpsertOpts{CookieID: expected.CookieID, Amount: "999"}}})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("过期账号写入没有被拒绝: %v", err)
	}
	// got、err 验证被并发修改的归属仍在，旧批量未推进版本或清除软删除。
	got, err := store.Orders.FindOwnership(ctx, userID, expected.OrderID)
	if err != nil || got.CookieID != options.CookieID || got.Version != expected.Version || !got.Deleted {
		t.Fatalf("CAS 未保护并发归属: %v", err)
	}
}
