package db

import (
	"context"
	"testing"
)

// TestNotificationsBindingRepositoryPaths 覆盖通知绑定的归属校验、增删改和批量清理路径。
func TestNotificationsBindingRepositoryPaths(t *testing.T) {
	// store、cleanup 保存带完整迁移的临时数据库及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// userID、cookieID 保存测试账号所属用户和账号标识。
	userID, cookieID := seedAccount(t, store)
	// created、createErr 保存第二个用户的创建结果和数据库错误。
	created, createErr := store.Users.Create(ctx, "binding-other", "binding-other@example.com", "pw")
	if createErr != nil || !created {
		t.Fatalf("create other user: created=%v err=%v", created, createErr)
	}
	// otherUser、otherUserErr 保存第二个用户对象及读取错误。
	otherUser, otherUserErr := store.Users.GetByUsername(ctx, "binding-other")
	if otherUserErr != nil {
		t.Fatal(otherUserErr)
	}
	// ownedChannel、ownedErr 保存当前用户通知渠道及创建错误。
	ownedChannel, ownedErr := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{
		Name: "owned", Type: "webhook", Config: `{}`, EventTypes: "message", Enabled: true, UserID: userID,
	})
	if ownedErr != nil {
		t.Fatal(ownedErr)
	}
	// foreignChannel、foreignErr 保存其他用户通知渠道及创建错误。
	foreignChannel, foreignErr := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{
		Name: "foreign", Type: "webhook", Config: `{}`, Enabled: true, UserID: otherUser.ID,
	})
	if foreignErr != nil {
		t.Fatal(foreignErr)
	}
	// owns、ownsErr 保存渠道归属检查结果及数据库错误。
	owns, ownsErr := store.Notifications.OwnsChannel(ctx, ownedChannel, userID)
	if ownsErr != nil || !owns {
		t.Fatalf("owned channel check: owns=%v err=%v", owns, ownsErr)
	}
	// foreignOwns、foreignOwnsErr 保存跨用户渠道归属检查结果及数据库错误。
	foreignOwns, foreignOwnsErr := store.Notifications.OwnsChannel(ctx, foreignChannel, userID)
	if foreignOwnsErr != nil || foreignOwns {
		t.Fatalf("foreign channel check: owns=%v err=%v", foreignOwns, foreignOwnsErr)
	}
	// setErr 保存启用绑定的数据库错误。
	if setErr := store.Notifications.SetSingleBinding(ctx, cookieID, ownedChannel, true); setErr != nil {
		t.Fatal(setErr)
	}
	// bindings、listErr 保存当前用户绑定列表及查询错误。
	bindings, listErr := store.Notifications.ListBindingsForUser(ctx, userID)
	if listErr != nil || len(bindings) != 1 || bindings[0].ChannelID != ownedChannel || !bindings[0].Enabled {
		t.Fatalf("bindings=%#v err=%v", bindings, listErr)
	}
	// bindingID 保存后续删除操作使用的绑定主键。
	bindingID := bindings[0].ID
	// deleteErr 保存错误用户删除绑定时的数据库错误；归属过滤应使该操作无副作用。
	if deleteErr := store.Notifications.DeleteBinding(ctx, otherUser.ID, bindingID); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	// bindingsAfterWrongDelete、wrongDeleteErr 保存错误用户删除后的绑定列表及查询错误。
	bindingsAfterWrongDelete, wrongDeleteErr := store.Notifications.ListBindingsForUser(ctx, userID)
	if wrongDeleteErr != nil || len(bindingsAfterWrongDelete) != 1 {
		t.Fatalf("wrong-user delete changed bindings=%#v err=%v", bindingsAfterWrongDelete, wrongDeleteErr)
	}
	// disableErr 保存关闭单个绑定的数据库错误。
	if disableErr := store.Notifications.SetSingleBinding(ctx, cookieID, ownedChannel, false); disableErr != nil {
		t.Fatal(disableErr)
	}
	// bindingsAfterDisable、disableListErr 保存关闭绑定后的列表及查询错误。
	bindingsAfterDisable, disableListErr := store.Notifications.ListBindingsForUser(ctx, userID)
	if disableListErr != nil || len(bindingsAfterDisable) != 0 {
		t.Fatalf("disabled binding remains: bindings=%#v err=%v", bindingsAfterDisable, disableListErr)
	}
	// rebindErr 保存重新启用绑定的数据库错误。
	if rebindErr := store.Notifications.SetSingleBinding(ctx, cookieID, ownedChannel, true); rebindErr != nil {
		t.Fatal(rebindErr)
	}
	// cleanupErr 保存错误用户批量清理时的数据库错误；归属过滤应保留当前用户绑定。
	if cleanupErr := store.Notifications.DeleteAccountBindings(ctx, otherUser.ID, cookieID); cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
	// bindingsAfterWrongCleanup、wrongCleanupErr 保存错误用户批量清理后的绑定列表及查询错误。
	bindingsAfterWrongCleanup, wrongCleanupErr := store.Notifications.ListBindingsForUser(ctx, userID)
	if wrongCleanupErr != nil || len(bindingsAfterWrongCleanup) != 1 {
		t.Fatalf("wrong-user account cleanup changed bindings=%#v err=%v", bindingsAfterWrongCleanup, wrongCleanupErr)
	}
	// finalCleanupErr 保存当前用户批量清理时的数据库错误。
	if finalCleanupErr := store.Notifications.DeleteAccountBindings(ctx, userID, cookieID); finalCleanupErr != nil {
		t.Fatal(finalCleanupErr)
	}
	// finalBindings、finalListErr 保存最终绑定列表及查询错误。
	finalBindings, finalListErr := store.Notifications.ListBindingsForUser(ctx, userID)
	if finalListErr != nil || len(finalBindings) != 0 {
		t.Fatalf("account bindings remain: bindings=%#v err=%v", finalBindings, finalListErr)
	}
}

// TestAnalyticsQueriesDelegatesToDatabase 验证分析查询边界透传单行和多行查询。
func TestAnalyticsQueriesDelegatesToDatabase(t *testing.T) {
	// store、cleanup 保存测试数据库及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 限制本测试数据库操作的生命周期。
	ctx := context.Background()
	// one、oneErr 保存单行查询结果及数据库错误。
	var one int
	// oneErr 保存单行分析查询的数据库错误。
	oneErr := store.Analytics.QueryRowContext(ctx, "SELECT 1").Scan(&one)
	if oneErr != nil || one != 1 {
		t.Fatalf("single-row query: one=%d err=%v", one, oneErr)
	}
	// rows、rowsErr 保存多行查询结果及数据库错误。
	rows, rowsErr := store.Analytics.QueryContext(ctx, "SELECT 1 AS value UNION ALL SELECT 2 AS value")
	if rowsErr != nil {
		t.Fatal(rowsErr)
	}
	defer rows.Close()
	// values 保存分析查询返回的整数行。
	values := make([]int, 0, 2)
	for rows.Next() {
		// value 保存当前分析结果行的数值。
		var value int
		// scanErr 保存当前分析结果行的扫描错误。
		if scanErr := rows.Scan(&value); scanErr != nil {
			t.Fatal(scanErr)
		}
		values = append(values, value)
	}
	// rowsErrAfterIteration 保存遍历结束后的驱动错误。
	rowsErrAfterIteration := rows.Err()
	if rowsErrAfterIteration != nil || len(values) != 2 || values[0] != 1 || values[1] != 2 {
		t.Fatalf("multi-row query: values=%v err=%v", values, rowsErrAfterIteration)
	}
}
