package db

import (
	"context"
	"testing"
)

// TestAvailableDataStockCountsOnlyEnabledDataCards 验证库存查询只统计启用的数据卡密组中的非空行，且不返回卡密内容。
func TestAvailableDataStockCountsOnlyEnabledDataCards(t *testing.T) {
	// store、cleanup 保存临时 SQLite Store 及测试完成时关闭连接池的函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存卡密组写入和库存查询共用的取消上下文。
	ctx := context.Background()
	// createUserOK、createUserErr 保存创建卡密所属用户的结果。
	createUserOK, createUserErr := store.Users.Create(ctx, "card-stock-user", "card-stock@example.com", "password")
	if createUserErr != nil || !createUserOK {
		t.Fatalf("创建卡密库存测试用户失败: ok=%v err=%v", createUserOK, createUserErr)
	}
	// user、userErr 保存新增用户，用于建立卡密组所有权。
	user, userErr := store.Users.GetByUsername(ctx, "card-stock-user")
	if userErr != nil {
		t.Fatalf("读取卡密库存测试用户失败: %v", userErr)
	}
	// enabledDataID、enabledDataErr 保存启用数据卡密组的创建结果。
	enabledDataID, enabledDataErr := store.Cards.Create(ctx, &CardFull{Name: "启用数据组", Type: "data", DataContent: "KEY-1\n\n KEY-2 \n", Enabled: true, UserID: user.ID})
	if enabledDataErr != nil || enabledDataID <= 0 {
		t.Fatalf("创建启用数据卡密组失败: id=%d err=%v", enabledDataID, enabledDataErr)
	}
	// disabledDataID、disabledDataErr 保存禁用数据卡密组的创建结果；其内容不能进入库存。
	disabledDataID, disabledDataErr := store.Cards.Create(ctx, &CardFull{Name: "禁用数据组", Type: "data", DataContent: "KEY-3", Enabled: false, UserID: user.ID})
	if disabledDataErr != nil || disabledDataID <= 0 {
		t.Fatalf("创建禁用数据卡密组失败: id=%d err=%v", disabledDataID, disabledDataErr)
	}
	// textCardID、textCardErr 保存启用文本卡密组的创建结果；其正文不能进入数据卡密库存。
	textCardID, textCardErr := store.Cards.Create(ctx, &CardFull{Name: "文本组", Type: "text", TextContent: "KEY-4", Enabled: true, UserID: user.ID})
	if textCardErr != nil || textCardID <= 0 {
		t.Fatalf("创建文本卡密组失败: id=%d err=%v", textCardID, textCardErr)
	}
	// stock、stockErr 保存 db 层返回的数量和查询错误。
	stock, stockErr := store.Cards.AvailableDataStock(ctx, user.ID)
	if stockErr != nil {
		t.Fatalf("统计可用数据卡密库存失败: %v", stockErr)
	}
	if stock != 2 {
		t.Fatalf("可用数据卡密库存应为 2，got %d", stock)
	}
}
