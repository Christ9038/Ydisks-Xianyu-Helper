package automation

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

// TestConfirmShipmentWithProofCoversPreflightBranches 验证确认发货在订单号缺失、账号关闭自动确认和数据库失败时的前置分支。
func TestConfirmShipmentWithProofCoversPreflightBranches(t *testing.T) {
	// ctx 是本测试确认发货入口共用的非取消上下文。
	ctx := context.Background()
	// emptyStore、emptyCleanup 保存确认发货前置测试数据库及清理函数。
	emptyStore, emptyCleanup := newAutomationTestStore(t)
	defer emptyCleanup()
	// center 是使用本地存储且不执行平台调用的自动化中心。
	center := New(emptyStore, nil, nil)
	// missingOrderErr 保存订单号缺失时的前置校验错误。
	missingOrderErr := center.confirmShipment(ctx, Task{AccountID: "cid"})
	if missingOrderErr == nil {
		t.Fatal("缺少订单号应拒绝确认发货")
	}
	// disabledAutoConfirm 保存关闭账号自动确认设置的值。
	disabledAutoConfirm := false
	// _, disableErr 保存关闭账号自动确认设置的更新错误。
	_, disableErr := emptyStore.Cookies.UpdateSettings(ctx, "cid", db.AccountSettingsUpdate{UserID: 1, AutoConfirm: &disabledAutoConfirm})
	if disableErr != nil {
		t.Fatal(disableErr)
	}
	// skipErr 保存非强制确认在账号设置关闭时的跳过结果。
	skipErr := center.confirmShipment(ctx, Task{AccountID: "cid", OrderID: "disabled-auto-confirm"})
	if skipErr != nil {
		t.Fatalf("关闭自动确认时应安全跳过：%v", skipErr)
	}

	// closedStore、closedCleanup 保存随后关闭数据库连接的测试存储。
	closedStore, closedCleanup := newAutomationTestStore(t)
	defer closedCleanup()
	// closeErr 保存关闭测试数据库连接的资源释放错误。
	if closeErr := closedStore.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// closedCenter 是绑定关闭数据库的自动化中心。
	closedCenter := New(closedStore, nil, nil)
	// readErr 保存读取自动确认设置失败的基础设施错误。
	readErr := closedCenter.confirmShipment(ctx, Task{AccountID: "cid", OrderID: "closed-db"})
	if readErr == nil {
		t.Fatal("关闭数据库后读取自动确认设置应返回错误")
	}
}
