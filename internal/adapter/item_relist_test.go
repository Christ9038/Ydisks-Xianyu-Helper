package adapter

import (
	"context"
	"errors"
	"testing"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// relistClientStub 是自动上架端口测试使用的平台客户端替身，实现 mtop.ItemRelistClient。
type relistClientStub struct {
	// mtop.Client 提供未涉及本测试的方法默认实现。
	mtop.Client
	// relist 保存重新上架的可控行为。
	relist func(context.Context, string, string) (*mtop.ItemRelistResult, error)
}

// RelistItem 调用测试注入的行为。
func (c relistClientStub) RelistItem(ctx context.Context, cookies, itemID string) (*mtop.ItemRelistResult, error) {
	return c.relist(ctx, cookies, itemID)
}

// TestItemPublishPortRelistItemSuccessPersistsCookie 验证成功转换结果并写回响应 Cookie。
func TestItemPublishPortRelistItemSuccessPersistsCookie(t *testing.T) {
	// store、cleanup 是带隔离 SQLite 的账号存储及清理操作。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// client 返回补回库存的成功结果和更新后的 Cookie。
	client := relistClientStub{relist: func(_ context.Context, cookies, itemID string) (*mtop.ItemRelistResult, error) {
		if cookies == "" || itemID != "item-1" {
			t.Fatalf("参数异常: %q %q", cookies, itemID)
		}
		return &mtop.ItemRelistResult{ItemID: itemID, RestoredQuantity: true, UpdatedCookies: "unb=2; _m_h5_tk=tk2; havana_lgc2_77=lgc;"}, nil
	}}
	// port 是绑定测试数据库与平台替身的商品发布端口。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, nil)
	// outcome、err 是重新上架结果。
	outcome, err := port.RelistItem(context.Background(), 1, "cid", "item-1")
	if err != nil || !outcome.RestoredQuantity || outcome.AlreadyOnSale {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	// value 是写回后读取到的账号 Cookie。
	if value, _ := store.Cookies.GetValue(context.Background(), "cid"); value != "unb=2; _m_h5_tk=tk2; havana_lgc2_77=lgc;" {
		t.Fatalf("Cookie 未写回: %q", value)
	}
}

// TestItemPublishPortRelistItemErrorMapping 验证归属错误、能力缺失、空结果以及平台错误的映射和会话恢复。
func TestItemPublishPortRelistItemErrorMapping(t *testing.T) {
	// store、cleanup 是带隔离 SQLite 的账号存储及清理操作。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// recovered 记录会话恢复回调的触发次数。
	recovered := 0
	// run 用指定行为构造一次重新上架调用。
	run := func(userID int64, relist func(context.Context, string, string) (*mtop.ItemRelistResult, error)) error {
		// client 是返回预设行为的平台客户端替身。
		client := relistClientStub{relist: relist}
		// port 是绑定测试数据库与平台替身的商品发布端口。
		port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, func(context.Context, string, error) bool {
			recovered++
			return true
		})
		// err 是本次调用返回的错误，用于断言错误映射或失败分类。
		_, err := port.RelistItem(context.Background(), userID, "cid", "item-1")
		return err
	}
	// fail 返回固定错误的平台行为。
	fail := func(err error) func(context.Context, string, string) (*mtop.ItemRelistResult, error) {
		return func(context.Context, string, string) (*mtop.ItemRelistResult, error) { return nil, err }
	}
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if err := run(99, fail(errors.New("不应调用"))); !errors.Is(err, itemapp.ErrRelistCredentialChanged) {
		t.Fatalf("越权 err=%v", err)
	}
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if err := run(1, fail(mtop.ErrItemEditUnsupported)); !errors.Is(err, itemapp.ErrRelistEditUnsupported) {
		t.Fatalf("类目不支持 err=%v", err)
	}
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if err := run(1, fail(&mtop.MTopResponseError{Kind: mtop.MTopErrorRiskVerification, API: "x"})); !errors.Is(err, itemapp.ErrRelistRiskVerification) || recovered != 0 {
		t.Fatalf("风控 err=%v recovered=%d", err, recovered)
	}
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if err := run(1, fail(&mtop.MTopResponseError{Kind: mtop.MTopErrorSessionExpired, API: "x"})); !errors.Is(err, itemapp.ErrRelistSessionExpired) || recovered != 1 {
		t.Fatalf("会话失效 err=%v recovered=%d", err, recovered)
	}
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if err := run(1, func(context.Context, string, string) (*mtop.ItemRelistResult, error) { return nil, nil }); err == nil {
		t.Fatal("空结果应报错")
	}
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if err := run(1, fail(errors.New("网络错误"))); err == nil || errors.Is(err, itemapp.ErrRelistRiskVerification) {
		t.Fatalf("未知错误应原样返回: %v", err)
	}
	// port 绑定没有重新上架能力的客户端替身。
	port := NewItemPublishPort(store, func() mtop.Client { return stockClientStub{} }, nil, nil, nil)
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if _, err := port.RelistItem(context.Background(), 1, "cid", "item-1"); !errors.Is(err, itemapp.ErrRelistUnsupported) {
		t.Fatalf("缺少能力 err=%v", err)
	}
}

// TestAutoRelistRepository 验证仓储读取开关、归属用户和商品归属，并拒绝缺少存储。
func TestAutoRelistRepository(t *testing.T) {
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if _, err := NewAutoRelistRepository(nil); err == nil {
		t.Fatal("缺少存储应报错")
	}
	// nilDeps 是未初始化（nil）的商品基础设施依赖，用于验证防御分支。
	var nilDeps *ItemDependencies
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if _, err := nilDeps.NewAutoRelistRepository(); err == nil {
		t.Fatal("未初始化依赖应报错")
	}
	// store、cleanup 是带隔离 SQLite 的账号存储及清理操作。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// err 是本次调用返回的错误，用于断言错误映射或失败分类；repository 是绑定测试数据库的自动上架仓储。
	repository, err := NewAutoRelistRepository(store)
	if err != nil {
		t.Fatal(err)
	}
	// ctx 是本用例所有请求共用的上下文。
	ctx := context.Background()
	// enabled 是账号自动上架开关的读取结果；getErr 是读取操作返回的错误。
	if enabled, getErr := repository.AutoRelistEnabled(ctx, "cid"); getErr != nil || enabled {
		t.Fatalf("默认应关闭: %v %v", enabled, getErr)
	}
	// getErr 是读取操作返回的错误；userID 是账号所属本地用户 ID。
	if userID, getErr := repository.AccountUserID(ctx, "cid"); getErr != nil || userID <= 0 {
		t.Fatalf("归属用户: %d %v", userID, getErr)
	}
	// getErr 是读取操作返回的错误；owned 是商品是否属于该账号的判定结果。
	if owned, getErr := repository.OwnsItem(ctx, "cid", "none"); getErr != nil || owned {
		t.Fatalf("未同步商品不应归属: %v %v", owned, getErr)
	}
}

// TestRelistAfterPaidTriggerRules 验证只有 WebSocket 付款事件触发，买家角色和其他触发类型被忽略，缺失商品从本地订单回填。
func TestRelistAfterPaidTriggerRules(t *testing.T) {
	// store、cleanup 是带隔离 SQLite 的账号存储及清理操作。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// calls 记录回调收到的商品和订单。
	var calls [][2]string
	// adapter 是带自动上架回调替身的事件适配器。
	adapter := &Adapter{store: store, soldItemRelist: func(_ context.Context, accountID, itemID, orderID string) {
		calls = append(calls, [2]string{itemID, orderID})
	}}
	// ctx 是本用例所有请求共用的上下文。
	ctx := context.Background()
	// paid 是来源于 WebSocket 的付款任务基准样例。
	paid := automation.Task{Source: "ws", AccountID: "cid", TriggerType: automation.TriggerOrderPaid, ItemID: "item-1", OrderID: "o1"}
	adapter.relistAfterPaid(ctx, paid)
	// buyer 是买家角色的付款任务变体，不应触发上架。
	buyer := paid
	buyer.OrderRole = automation.OrderRoleBuyer
	adapter.relistAfterPaid(ctx, buyer)
	// scheduler 是来源为调度器的付款任务变体，不应触发上架。
	scheduler := paid
	scheduler.Source = "scheduler"
	adapter.relistAfterPaid(ctx, scheduler)
	// created 是订单创建触发类型的任务变体，不应触发上架。
	created := paid
	created.TriggerType = automation.TriggerOrderCreated
	adapter.relistAfterPaid(ctx, created)
	if len(calls) != 1 || calls[0] != [2]string{"item-1", "o1"} {
		t.Fatalf("只应触发一次: %v", calls)
	}
	// 缺少商品且本地无订单时不触发。
	missing := paid
	missing.ItemID = ""
	adapter.relistAfterPaid(ctx, missing)
	// 本地订单属于本账号时回填商品；属于其他账号时忽略。
	if err := store.Orders.Upsert(ctx, "o1", db.OrderUpsertOpts{ItemID: "item-9", CookieID: "cid"}); err != nil {
		t.Fatalf("写入订单: %v", err)
	}
	adapter.relistAfterPaid(ctx, missing)
	// other 是账号不同于订单归属账号的任务变体。
	other := missing
	other.AccountID = "other"
	adapter.relistAfterPaid(ctx, other)
	if len(calls) != 2 || calls[1] != [2]string{"item-9", "o1"} {
		t.Fatalf("应从本地订单回填: %v", calls)
	}
	// 未配置回调时直接忽略。
	(&Adapter{store: store}).relistAfterPaid(ctx, paid)
}
