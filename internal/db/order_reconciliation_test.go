package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// TestOrderReconciliationsLifecycle 验证补偿记录可创建、扫描并标记完成。
func TestOrderReconciliationsLifecycle(t *testing.T) {
	// ctx 是本测试使用的数据库上下文。
	ctx := context.Background()
	// database、dialect、err 保存临时 SQLite 数据库及打开结果。
	database, dialect, err := Open(ctx, t.TempDir()+"/reconciliation.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	// store 是绑定最新迁移的数据库访问聚合。
	store := NewStore(database, dialect)
	// id、err 保存新建补偿记录的标识及错误。
	id, err := store.Reconciliations.CreatePending(ctx, "order-1", "cookie-1", "manual_status_ship", "本地订单写入失败")
	if err != nil || id == "" {
		t.Fatalf("CreatePending id=%q err=%v", id, err)
	}
	// records、err 保存待补偿扫描结果及错误。
	records, err := store.Reconciliations.ListPending(ctx, 10)
	if err != nil || len(records) != 1 || records[0].ID != id || records[0].Status != "pending" {
		t.Fatalf("ListPending records=%+v err=%v", records, err)
	}
	// err 表示补偿记录首次完成失败。
	if err := store.Reconciliations.MarkResolved(ctx, id, "本地订单状态已补齐"); err != nil {
		t.Fatalf("MarkResolved: %v", err)
	}
	// remaining、err 保存标记完成后的待补偿记录及错误。
	remaining, err := store.Reconciliations.ListPending(ctx, 10)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("resolved record remains: %+v err=%v", remaining, err)
	}
	// err 表示重复完成同一补偿记录的结果。
	if err := store.Reconciliations.MarkResolved(ctx, id, "重复完成"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("重复完成应返回 sql.ErrNoRows，got %v", err)
	}
}
