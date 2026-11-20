package adapter

import (
	"context"
	"errors"
	"strings"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/xianyu/mtop"
)

// recordingReconciliation 记录补偿记录调用，隔离订单补偿存储。
type recordingReconciliation struct {
	// calls 统计收到的补偿请求数。
	calls int
}

// RecordReconciliation 返回固定补偿标识。
func (r *recordingReconciliation) RecordReconciliation(context.Context, string, string, string, string) (string, error) {
	r.calls++
	return "rec-1", nil
}

// fetchCapableMTopFake 同时具备订单详情与已售订单分页能力。
type fetchCapableMTopFake struct {
	// orderRuntimeMTopFake 提供基础客户端接口默认实现。
	orderRuntimeMTopFake
}

// FetchOrderDetail 满足订单详情能力接口。
func (f *fetchCapableMTopFake) FetchOrderDetail(context.Context, string, string) (*mtop.OrderDetailResult, error) {
	return nil, nil
}

// FetchSoldOrdersPage 满足已售订单分页能力接口。
func (f *fetchCapableMTopFake) FetchSoldOrdersPage(context.Context, string, int, int) (*mtop.SoldOrdersPage, error) {
	return nil, nil
}

// TestOrderRuntimeNilHooksAndReporting 覆盖运行时回调缺失、补偿记录与持久化失败上报。
func TestOrderRuntimeNilHooksAndReporting(t *testing.T) {
	// ctx 是调用共用的后台上下文。
	ctx := context.Background()
	// nilRuntime 是空指针运行时。
	var nilRuntime *OrderRuntime
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := nilRuntime.ManualFullDelivery(ctx, nil); err == nil {
		t.Fatal("空运行时应拒绝手动发货")
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := (&OrderRuntime{}).ManualFullDelivery(ctx, nil); err == nil {
		t.Fatal("缺回调应拒绝手动发货")
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := nilRuntime.RecordReconciliation(ctx, "o", "cid", "k", "m"); err == nil {
		t.Fatal("空运行时应拒绝补偿记录")
	}
	// recorder 记录补偿调用。
	recorder := &recordingReconciliation{}
	// runtime 是带补偿记录与失败上报回调的运行时。
	var reported error
	// runtime 测试中的局部对象，承载被测流程的输入、依赖或输出。
	runtime := NewOrderRuntime(nil, OrderRuntimeHooks{ReportPersistenceFailure: func(_ string, err error) { reported = err }}, recorder, nil)
	// err、id 依次为：保存本步骤调用返回的错误，用于断言预期的成功或失败路径；测试中的局部对象，承载被测流程的输入、依赖或输出。
	if id, err := runtime.RecordReconciliation(ctx, "o", "cid", "k", "m"); err != nil || id != "rec-1" || recorder.calls != 1 {
		t.Fatalf("补偿记录: %q %v", id, err)
	}
	nilRuntime.ReportPersistenceFailure("o", errors.New("x"))
	runtime.ReportPersistenceFailure("o", nil)
	runtime.ReportPersistenceFailure("o", errors.New("persist failed"))
	if reported == nil {
		t.Fatal("应触发上报回调")
	}
	// 未配置上报回调时仅写日志。
	NewOrderRuntime(nil, OrderRuntimeHooks{}, nil, nil).ReportPersistenceFailure("o", errors.New("persist failed"))
	// 刷新聊天联系人回调缺失与存在两种路径。
	if err := nilRuntime.RefreshChatConversations(ctx, "cid"); err != nil {
		t.Fatal(err)
	}
	// refreshed 记录联系人刷新回调参数。
	refreshed := ""
	// hooked 测试中的局部对象，承载被测流程的输入、依赖或输出。
	hooked := NewOrderRuntime(nil, OrderRuntimeHooks{RefreshChatConversations: func(_ context.Context, id string) error { refreshed = id; return nil }}, nil, nil)
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if err := hooked.RefreshChatConversations(ctx, "cid"); err != nil || refreshed != "cid" {
		t.Fatalf("联系人刷新回调: %q %v", refreshed, err)
	}
	// 空明细的平台请求直接拒绝。
	capable := NewOrderRuntime(nil, OrderRuntimeHooks{Client: func() mtop.Client { return &fetchCapableMTopFake{} }}, nil, nil)
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := capable.FetchOrderDetail(ctx, nil, "o"); err == nil {
		t.Fatal("空凭证应拒绝详情请求")
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := capable.FetchSoldOrders(ctx, nil); err == nil {
		t.Fatal("空凭证应拒绝已售订单请求")
	}
	// 持久化保护：空明细、未处理、未变更与缺存储三种返回。
	if _, _, handled, err := capable.PersistCookieSession(ctx, nil, orderapp.RefreshCookieUpdate{}); handled || err != nil {
		t.Fatalf("空明细: %v %v", handled, err)
	}
	// detail 是持久化使用的平台凭证视图。
	detail := &orderapp.PlatformRuntimeData{ID: "cid", Value: "unb=1"}
	// err、handled 依次为：保存本步骤调用返回的错误，用于断言预期的成功或失败路径；标记返回的会话变更是否已被处理，用于断言。
	if _, _, handled, err := capable.PersistCookieSession(ctx, detail, orderapp.RefreshCookieUpdate{Handled: true, Changed: true, Value: "unb=2"}); !handled || err == nil {
		t.Fatalf("缺存储: %v %v", handled, err)
	}
}

// TestOrderRuntimeConfirmShipmentFailureBranches 覆盖确认发货中凭证为空、客户端缺失、写回冲突及无 Cookie 更新的成功路径。
func TestOrderRuntimeConfirmShipmentFailureBranches(t *testing.T) {
	// ctx 是调用共用的后台上下文。
	ctx := context.Background()
	// store、cleanup 是隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// admin 是账号所有者。
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// client 是可编排确认发货结果的平台替身。
	client := &orderRuntimeMTopFake{}
	// runtime 绑定存储与客户端。
	runtime := NewOrderRuntime(store, OrderRuntimeHooks{Client: func() mtop.Client { return client }}, nil, nil)
	// 无 Cookie 更新的成功路径。
	client.consign = func(context.Context, string, string) (bool, []string, string, error) {
		return true, []string{"ok"}, "", nil
	}
	// result 保存被测函数返回的结果对象，用于断言其内容。
	if result := runtime.ConfirmShipment(ctx, "cid", "o", admin.ID); !result.Success || result.Err != nil || result.RuntimeCookieChanged {
		t.Fatalf("无更新成功: %+v", result)
	}
	// 平台调用期间凭证被改写：写回复核失败，既有平台错误与写回错误合并，或单独返回写回错误。
	client.consign = func(context.Context, string, string) (bool, []string, string, error) {
		_ = store.Cookies.UpdateValueExisting(ctx, "cid", "unb=1; changed=1")
		return true, nil, "unb=1; new=1", errors.New("platform")
	}
	// result 保存被测函数返回的结果对象，用于断言其内容。
	if result := runtime.ConfirmShipment(ctx, "cid", "o", admin.ID); result.Err == nil {
		t.Fatalf("合并错误应返回: %+v", result)
	}
	client.consign = func(context.Context, string, string) (bool, []string, string, error) {
		_ = store.Cookies.UpdateValueExisting(ctx, "cid", "unb=1; changed=2")
		return true, nil, "unb=1; new=2", nil
	}
	// result 保存被测函数返回的结果对象，用于断言其内容。
	if result := runtime.ConfirmShipment(ctx, "cid", "o", admin.ID); result.Err == nil {
		t.Fatalf("写回冲突应返回: %+v", result)
	}
	// 客户端缺失。
	noClient := NewOrderRuntime(store, OrderRuntimeHooks{Client: func() mtop.Client { return nil }}, nil, nil)
	// result 保存被测函数返回的结果对象，用于断言其内容。
	if result := noClient.ConfirmShipment(ctx, "cid", "o", admin.ID); result.Err == nil {
		t.Fatal("缺客户端应失败")
	}
	// 凭证为空。
	if err := store.Cookies.UpdateValueExisting(ctx, "cid", ""); err != nil {
		t.Fatal(err)
	}
	// result 保存被测函数返回的结果对象，用于断言其内容。
	if result := runtime.ConfirmShipment(ctx, "cid", "o", admin.ID); result.Err == nil {
		t.Fatal("空凭证应失败")
	}
}

// TestOrderRuntimeCookiePersistenceBranches 覆盖订单响应 Cookie 写回的缺存储、读取失败与数据库写入失败路径。
func TestOrderRuntimeCookiePersistenceBranches(t *testing.T) {
	// ctx 是调用共用的后台上下文。
	ctx := context.Background()
	// store、cleanup 是隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// admin 是账号所有者。
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// runtime 绑定存储；bare 缺少存储。
	runtime := NewOrderRuntime(store, OrderRuntimeHooks{}, nil, nil)
	// bare 测试中的局部对象，承载被测流程的输入、依赖或输出。
	bare := &OrderRuntime{}
	// latest 是当前凭证视图，用于构造与最新版本一致的明细。
	latest, err := store.Cookies.GetCookiePlatformRuntimeData(ctx, "cid")
	if err != nil {
		t.Fatal(err)
	}
	// _, changedSession 是已改写 Jar 的会话。
	_, changedSession := mtop.WithCookieSnapshot(ctx, []BrowserCookie{{Name: "unb", Value: "1", Domain: ".goofish.com", Path: "/"}})
	changedSession.ReplaceSnapshot([]BrowserCookie{{Name: "unb", Value: "9", Domain: ".goofish.com", Path: "/"}})
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, _, _, err := bare.persistOrderCookieSession(ctx, latest, changedSession, ""); err == nil {
		t.Fatal("缺存储应失败")
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, _, _, err := bare.persistCurrentOrderCookieSession(ctx, latest, changedSession, "", admin.ID); err == nil {
		t.Fatal("缺存储应失败")
	}
	// missing 是不存在的账号明细，复核读取应失败。
	missing := latest
	missing.ID = "missing"
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, _, _, err := runtime.persistCurrentOrderCookieSession(ctx, missing, changedSession, "", admin.ID); err == nil {
		t.Fatal("读取失败应传播")
	}
	// 用触发器让账号更新全部失败，覆盖会话写回与扁平写回错误。
	if _, err := store.DB.ExecContext(ctx, `CREATE TRIGGER block_cookie_update BEFORE UPDATE ON cookies BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, _, _, err := runtime.persistCurrentOrderCookieSession(ctx, latest, changedSession, "", admin.ID); err == nil {
		t.Fatal("会话写回失败应传播")
	}
	// _, flatSession 是未变更的扁平会话，平台只返回扁平 Cookie 时走条件写回。
	_, flatSession := mtop.WithFlatCookieSession(ctx, latest.Value)
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, _, _, err := runtime.persistCurrentOrderCookieSession(ctx, latest, flatSession, "unb=1; flat=1", admin.ID); err == nil {
		t.Fatal("扁平写回失败应传播")
	}
	// PersistCookieSession 的数据库写入失败。
	if _, _, handled, err := runtime.PersistCookieSession(ctx, &orderapp.PlatformRuntimeData{ID: "cid", Value: "unb=1"}, orderapp.RefreshCookieUpdate{Handled: true, Changed: true, Value: "unb=2"}); !handled || err == nil {
		t.Fatalf("PersistCookieSession 写入失败: %v %v", handled, err)
	}
}

// TestOrderRuntimeAdjustPriceBranches 覆盖订单改价的成功、业务失败、Cookie 写回冲突、越权、空凭证、客户端缺失和存储缺失路径，并确认每次只调用一次平台。
func TestOrderRuntimeAdjustPriceBranches(t *testing.T) {
	// ctx 是调用共用的后台上下文。
	ctx := context.Background()
	// store、cleanup 是隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// admin 是账号所有者。
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// client 是可编排改价结果的平台替身。
	client := &orderRuntimeMTopFake{}
	// runtime 绑定存储与客户端。
	runtime := NewOrderRuntime(store, OrderRuntimeHooks{Client: func() mtop.Client { return client }}, nil, nil)

	// 成功：订单号与整数分原样传给平台，且只调用一次。
	client.adjustPrice = func(_ context.Context, _ string, orderID string, cents int64) (bool, []string, string, error) {
		if orderID != "o" || cents != 1250 {
			t.Fatalf("平台入参 order=%q cents=%d", orderID, cents)
		}
		return true, []string{"SUCCESS"}, "", nil
	}
	// result 保存本步骤的改价结果，用于断言对应分支。
	if result := runtime.AdjustPrice(ctx, "cid", "o", admin.ID, 1250); !result.Success || result.Err != nil || result.RuntimeCookieChanged {
		t.Fatalf("成功路径: %+v", result)
	}
	if client.adjustPriceCalls != 1 {
		t.Fatalf("单次改价应只调用一次平台: %d", client.adjustPriceCalls)
	}

	// 业务失败：保留平台提示且不标记成功。
	client.adjustPrice = func(context.Context, string, string, int64) (bool, []string, string, error) {
		return false, []string{"FAIL_BIZ"}, "", nil
	}
	// result 保存本步骤的改价结果，用于断言对应分支。
	if result := runtime.AdjustPrice(ctx, "cid", "o", admin.ID, 100); result.Success || len(result.Messages) != 1 {
		t.Fatalf("业务失败: %+v", result)
	}

	// 传输错误：不标记成功并带回错误。
	client.adjustPrice = func(context.Context, string, string, int64) (bool, []string, string, error) {
		return true, nil, "", errors.New("platform")
	}
	// result 保存本步骤的改价结果，用于断言对应分支。
	if result := runtime.AdjustPrice(ctx, "cid", "o", admin.ID, 100); result.Err == nil || result.Success {
		t.Fatalf("调用错误不应视为成功: %+v", result)
	}

	// 平台调用期间凭证被改写：写回复核失败，返回写回错误。
	client.adjustPrice = func(context.Context, string, string, int64) (bool, []string, string, error) {
		_ = store.Cookies.UpdateValueExisting(ctx, "cid", "unb=1; changed=1")
		return true, nil, "unb=1; new=1", nil
	}
	// result 保存本步骤的改价结果，用于断言对应分支。
	if result := runtime.AdjustPrice(ctx, "cid", "o", admin.ID, 100); result.Err == nil || !strings.Contains(result.Err.Error(), "改价响应") {
		t.Fatalf("写回冲突应带改价上下文: %+v", result)
	}

	// 越权：账号不属于该用户，且不得触达平台。
	client.adjustPriceCalls = 0
	// result 保存本步骤的改价结果，用于断言对应分支。
	if result := runtime.AdjustPrice(ctx, "cid", "o", admin.ID+99, 100); !errors.Is(result.Err, orderapp.ErrForbidden) || client.adjustPriceCalls != 0 {
		t.Fatalf("越权: %+v calls=%d", result, client.adjustPriceCalls)
	}

	// 客户端缺失。
	noClient := NewOrderRuntime(store, OrderRuntimeHooks{Client: func() mtop.Client { return nil }}, nil, nil)
	// result 保存本步骤的改价结果，用于断言对应分支。
	if result := noClient.AdjustPrice(ctx, "cid", "o", admin.ID, 100); result.Err == nil {
		t.Fatal("缺客户端应失败")
	}
	// 凭证为空。
	if err := store.Cookies.UpdateValueExisting(ctx, "cid", ""); err != nil {
		t.Fatal(err)
	}
	// result 保存本步骤的改价结果，用于断言对应分支。
	if result := runtime.AdjustPrice(ctx, "cid", "o", admin.ID, 100); result.Err == nil {
		t.Fatal("空凭证应失败")
	}
	// 存储缺失。
	// result 保存本步骤的改价结果，用于断言对应分支。
	if result := (*OrderRuntime)(nil).AdjustPrice(ctx, "cid", "o", admin.ID, 100); result.Err == nil {
		t.Fatal("空运行时应失败")
	}
}
