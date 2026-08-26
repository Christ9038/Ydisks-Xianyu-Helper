package db

import (
	"context"
	"errors"
	"testing"
)

// TestRenewalRuntimeQueriesCoverMissingAndCorruptRows 验证续期窄查询对缺失账号和损坏密文的错误边界。
func TestRenewalRuntimeQueriesCoverMissingAndCorruptRows(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "renewal-runtime-boundary-key")
	// store、cleanup 保存本测试数据库及其关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试数据库操作共用的上下文。
	ctx := context.Background()
	// missingPlatformErr 保存平台运行视图读取不存在账号的错误。
	if _, missingPlatformErr := store.Cookies.GetCookiePlatformRuntimeData(ctx, "missing-platform"); !errors.Is(missingPlatformErr, ErrNotFound) {
		t.Fatalf("missing platform err=%v", missingPlatformErr)
	}
	// missingRenewalErr 保存续期运行视图读取不存在账号的错误。
	if _, missingRenewalErr := store.Cookies.GetRenewalRuntimeAccount(ctx, "missing-renewal"); !errors.Is(missingRenewalErr, ErrNotFound) {
		t.Fatalf("missing renewal err=%v", missingRenewalErr)
	}
	// ownerID 保存用于创建损坏密文账号的本地用户标识。
	var ownerID int64
	// createErr 保存创建损坏密文测试用户的数据库错误。
	if createErr := store.DB.QueryRowContext(ctx,
		`INSERT INTO users (username,email,password_hash) VALUES (?,?,?) RETURNING id`,
		"renewal-boundary-user", "renewal-boundary@example.com", "test-hash").Scan(&ownerID); createErr != nil {
		t.Fatalf("create user: %v", createErr)
	}
	// saveErr 保存创建损坏密文账号的结果。
	if saveErr := store.Cookies.Save(ctx, "renewal-corrupt", "sid=corrupt", ownerID); saveErr != nil {
		t.Fatalf("save cookie: %v", saveErr)
	}
	// corruptErr 保存将 metadata 替换为不可解密文本的数据库更新结果。
	if _, corruptErr := store.DB.ExecContext(ctx, `UPDATE cookies SET metadata_json=? WHERE id=?`, "enc:v1:broken", "renewal-corrupt"); corruptErr != nil {
		t.Fatalf("corrupt metadata: %v", corruptErr)
	}
	// platformErr 保存平台运行视图解密损坏 metadata 的错误。
	if _, platformErr := store.Cookies.GetCookiePlatformRuntimeData(ctx, "renewal-corrupt"); platformErr == nil {
		t.Fatal("corrupt platform metadata should fail")
	}
	// renewalErr 保存续期运行视图解密损坏 metadata 的错误。
	if _, renewalErr := store.Cookies.GetRenewalRuntimeAccount(ctx, "renewal-corrupt"); renewalErr == nil {
		t.Fatal("corrupt renewal metadata should fail")
	}
}

// TestUpdateRenewalCookieCoversInputAndDefaultTimestampBranches 验证续期 Cookie 更新的输入校验和默认时间戳路径。
func TestUpdateRenewalCookieCoversInputAndDefaultTimestampBranches(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "renewal-update-boundary-key")
	// store、cleanup 保存本测试数据库及其关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试数据库操作共用的上下文。
	ctx := context.Background()
	// blankErr 保存空账号标识的输入校验错误。
	if blankErr := store.Cookies.UpdateRenewalCookie(ctx, "   ", "sid=1", `{}`, 1); blankErr == nil {
		t.Fatal("blank cookie ID should fail")
	}
	// ownerID 保存有效续期账号所属用户标识。
	var ownerID int64
	// createErr 保存创建有效续期测试用户的数据库错误。
	if createErr := store.DB.QueryRowContext(ctx,
		`INSERT INTO users (username,email,password_hash) VALUES (?,?,?) RETURNING id`,
		"renewal-update-user", "renewal-update@example.com", "test-hash").Scan(&ownerID); createErr != nil {
		t.Fatalf("create user: %v", createErr)
	}
	// saveErr 保存有效续期账号创建结果。
	if saveErr := store.Cookies.Save(ctx, "renewal-default-time", "sid=old", ownerID); saveErr != nil {
		t.Fatalf("save cookie: %v", saveErr)
	}
	// updateErr 保存使用默认时间戳更新续期 Cookie 的结果。
	if updateErr := store.Cookies.UpdateRenewalCookie(ctx, "renewal-default-time", "sid=new", `{}`, 0); updateErr != nil {
		t.Fatalf("default timestamp update: %v", updateErr)
	}
	// refreshAt 保存数据库写回的默认刷新时间。
	var refreshAt int64
	// queryErr 保存读取默认刷新时间的错误。
	if queryErr := store.DB.QueryRowContext(ctx, `SELECT last_refresh_at FROM cookies WHERE id=?`, "renewal-default-time").Scan(&refreshAt); queryErr != nil {
		t.Fatalf("read refresh timestamp: %v", queryErr)
	}
	if refreshAt <= 0 {
		t.Fatalf("refresh timestamp=%d", refreshAt)
	}
}

// TestOrderWriteUnitOfWorkCoversInitializationGuards 验证订单写入事务及事务内窄方法的初始化保护。
func TestOrderWriteUnitOfWorkCoversInitializationGuards(t *testing.T) {
	// store、cleanup 保存有效 UnitOfWork 所需的测试数据库及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是事务初始化保护测试使用的上下文。
	ctx := context.Background()
	// nilUnitErr 保存 nil UnitOfWork 的明确初始化错误。
	var nilUnit *OrderWriteUnitOfWork
	// nilUnitErr 表示 nil UnitOfWork 调用返回的错误。
	if nilUnitErr := nilUnit.WithTransaction(ctx, func(*OrderWriteTransaction) error { return nil }); nilUnitErr == nil {
		t.Fatal("nil unit should fail")
	}
	// emptyUnitErr 保存缺少数据库和仓储依赖的初始化错误。
	if emptyUnitErr := (&OrderWriteUnitOfWork{}).WithTransaction(ctx, func(*OrderWriteTransaction) error { return nil }); emptyUnitErr == nil {
		t.Fatal("empty unit should fail")
	}
	// nilWorkErr 保存缺少事务回调的参数错误。
	if nilWorkErr := store.OrderWrites.WithTransaction(ctx, nil); nilWorkErr == nil {
		t.Fatal("nil work should fail")
	}
	// nilTransaction 保存缺少底层 SQL 事务的窄写入对象。
	nilTransaction := &OrderWriteTransaction{}
	// invalidOperations 保存四个事务内窄方法的初始化错误。
	invalidOperations := []error{
		nilTransaction.PatchOrder(ctx, "order", OrderPatch{}),
		nilTransaction.UpsertItemBasic(ctx, &ItemInfoRow{}),
		nilTransaction.UpsertOrder(ctx, "order", OrderUpsertOpts{}),
		nilTransaction.UpsertOrders(ctx, nil),
	}
	// operation 表示当前待验证的窄方法错误。
	for _, operation := range invalidOperations {
		if operation == nil {
			t.Fatal("uninitialized transaction operation should fail")
		}
	}
}
