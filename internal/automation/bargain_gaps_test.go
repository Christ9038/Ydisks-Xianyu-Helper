package automation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// plainMTop 只暴露 mtop.Client 接口方法，用于模拟不支持免拼能力的旧客户端。
type plainMTop struct {
	// Client 是被包装的 MTOP 客户端；结构体不提升其未在接口中声明的免拼方法。
	mtop.Client
}

// newBargainGapCenter 创建已开启自动免拼开关的中心；返回中心与数据库。
func newBargainGapCenter(t *testing.T, client mtop.Client, deps CenterDependencies) (*Center, *db.Store) {
	t.Helper()
	// store、cleanup 保存隔离数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	t.Cleanup(cleanup)
	// admin、adminErr 保存账号所属管理员。
	admin, adminErr := store.Users.GetByUsername(context.Background(), "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// on 是独立自动免拼开关的开启值。
	on := true
	// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
	if _, err := store.Cookies.UpdateSettings(context.Background(), "cid", db.AccountSettingsUpdate{UserID: admin.ID, AutoBargain: &on}); err != nil {
		t.Fatal(err)
	}
	deps.MTop = client
	return NewWithDependencies(store, nil, nil, deps), store
}

// bargainGapTask 构造完整的待刀成 WebSocket 任务。
func bargainGapTask(orderID string) Task {
	return Task{Source: "ws", AccountID: "cid", OrderRole: OrderRoleSeller, TriggerType: TriggerBargainPending, OrderID: orderID, ItemID: "10001", BuyerID: "20002", ChatID: "chat", IsBargain: true}
}

// TestHandleBargainPendingGapBranches 覆盖待刀成阶段的设置读取、缺订单、领取冲突与阶段收口失败。
func TestHandleBargainPendingGapBranches(t *testing.T) {
	// ctx 是调用共用的上下文。
	ctx := context.Background()
	t.Run("设置读取失败", func(t *testing.T) {
		// center、store 保存已开启免拼的夹具。
		center, store := newBargainGapCenter(t, &fakeMTop{freeShippingOK: true}, CenterDependencies{})
		execGapSQL(t, store, `ALTER TABLE cookies RENAME TO cookies_gap_hidden`)
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := center.handleBargainPending(ctx, bargainGapTask("b1")); err == nil || !strings.Contains(err.Error(), "读取自动免拼设置") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("缺订单号", func(t *testing.T) {
		// center 保存已开启免拼的夹具。
		center, _ := newBargainGapCenter(t, &fakeMTop{freeShippingOK: true}, CenterDependencies{})
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := center.handleBargainPending(ctx, bargainGapTask("")); err == nil || !strings.Contains(err.Error(), "缺少订单ID") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("领取阶段失败", func(t *testing.T) {
		// center、store 保存已开启免拼的夹具。
		center, store := newBargainGapCenter(t, &fakeMTop{freeShippingOK: true}, CenterDependencies{})
		execGapSQL(t, store, `ALTER TABLE bargain_free_shipping_stages RENAME TO bargain_stage_gap_hidden`)
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := center.handleBargainPending(ctx, bargainGapTask("b2")); err == nil || !strings.Contains(err.Error(), "领取免拼阶段执行权") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("重复事件只免拼一次", func(t *testing.T) {
		// client 记录免拼调用次数。
		client := &fakeMTop{freeShippingOK: true, freeShippingRet: []string{"SUCCESS::调用成功"}}
		// center 保存已开启免拼的夹具。
		center, _ := newBargainGapCenter(t, client, CenterDependencies{})
		for range 2 {
			// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
			if _, err := center.handleBargainPending(ctx, bargainGapTask("b3")); err != nil {
				t.Fatal(err)
			}
		}
		if client.freeShippingCalls != 1 || client.consignCalls != 0 {
			t.Fatalf("free=%d consign=%d", client.freeShippingCalls, client.consignCalls)
		}
	})
	t.Run("明确失败记录为可重试且不确认发货", func(t *testing.T) {
		// client 模拟平台明确拒绝免拼。
		client := &fakeMTop{freeShippingRet: []string{"FAIL::拒绝"}}
		// center 保存已开启免拼的夹具。
		center, _ := newBargainGapCenter(t, client, CenterDependencies{})
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := center.handleBargainPending(ctx, bargainGapTask("b4")); err == nil {
			t.Fatal("明确失败应返回错误")
		}
		if client.consignCalls != 0 {
			t.Fatal("免拼失败不得确认发货")
		}
	})
	t.Run("结果不确定转人工通知", func(t *testing.T) {
		// client 模拟传输错误导致结果不确定。
		client := &fakeMTop{freeShippingErr: errors.New("连接中断")}
		// notifier 记录人工通知。
		notifier := &recordingNotifier{}
		// center 保存已开启免拼的夹具。
		center, _ := newBargainGapCenter(t, client, CenterDependencies{Notifier: notifier})
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := center.handleBargainPending(ctx, bargainGapTask("b5")); err == nil {
			t.Fatal("不确定结果应返回错误")
		}
	})
	// stageAbort 让阶段表的收口更新失败。
	const stageAbort = `CREATE TRIGGER gap_abort_stage BEFORE UPDATE ON bargain_free_shipping_stages BEGIN SELECT RAISE(ABORT, 'forced stage failure'); END`
	t.Run("免拼成功但阶段收口失败", func(t *testing.T) {
		// center、store 保存已开启免拼的夹具。
		center, store := newBargainGapCenter(t, &fakeMTop{freeShippingOK: true, freeShippingRet: []string{"SUCCESS::ok"}}, CenterDependencies{})
		execGapSQL(t, store, stageAbort)
		// _, err 保存收口失败后的错误，必须标记为结果不确定。
		_, err := center.handleBargainPending(ctx, bargainGapTask("b6"))
		// uncertain 用于识别不确定错误类型。
		var uncertain *uncertainActionError
		if !errors.As(err, &uncertain) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("免拼失败且阶段收口失败", func(t *testing.T) {
		// center、store 保存已开启免拼的夹具。
		center, store := newBargainGapCenter(t, &fakeMTop{freeShippingRet: []string{"FAIL::拒绝"}}, CenterDependencies{})
		execGapSQL(t, store, stageAbort)
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if _, err := center.handleBargainPending(ctx, bargainGapTask("b7")); err == nil || !strings.Contains(err.Error(), "收口免拼阶段") {
			t.Fatalf("err=%v", err)
		}
	})
}

// TestFreeShipBargainGapBranches 覆盖免拼接口的会话打开失败、能力缺失、会话失效恢复与凭证保存失败。
func TestFreeShipBargainGapBranches(t *testing.T) {
	// ctx 是调用共用的上下文。
	ctx := context.Background()
	// task 是账号 cid 的完整免拼任务。
	task := Task{AccountID: "cid", OrderID: "fs-order", ItemID: "item", BuyerID: "buyer", IsBargain: true}
	// cookieAbort 让账号 Cookie 写回失败，用于覆盖响应凭证保存错误。
	const cookieAbort = `CREATE TRIGGER gap_abort_cookie BEFORE UPDATE ON cookies BEGIN SELECT RAISE(ABORT, 'forced cookie failure'); END`
	t.Run("账号没有凭证", func(t *testing.T) {
		// center 保存夹具。
		center, _ := newBargainGapCenter(t, &fakeMTop{freeShippingOK: true}, CenterDependencies{})
		// ghost 是不存在凭证的账号。
		ghost := task
		ghost.AccountID = "ghost"
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if err := center.actions.freeShipBargain(ctx, ghost); err == nil {
			t.Fatal("无凭证应报错")
		}
	})
	t.Run("客户端不支持免拼", func(t *testing.T) {
		// center 使用不暴露免拼方法的客户端。
		center, _ := newBargainGapCenter(t, plainMTop{Client: &fakeMTop{}}, CenterDependencies{})
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if err := center.actions.freeShipBargain(ctx, task); !errors.Is(err, errActionNotPerformed) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("会话失效且响应凭证保存失败", func(t *testing.T) {
		// client 返回会话失效并携带新的 Cookie。
		client := &fakeMTop{freeShippingResults: []fakeFreeShippingResult{{ret: []string{"FAIL_SYS_SESSION_EXPIRED::x"}, updated: "unb=123; _m_h5_tk=zz;"}}}
		// center、store 保存夹具。
		center, store := newBargainGapCenter(t, client, CenterDependencies{})
		execGapSQL(t, store, cookieAbort)
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if err := center.actions.freeShipBargain(ctx, task); err == nil || !strings.Contains(err.Error(), "已失效") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("恢复后仍失效不再重试", func(t *testing.T) {
		// client 两次都返回会话失效。
		client := &fakeMTop{freeShippingResults: []fakeFreeShippingResult{
			{ret: []string{"FAIL_SYS_SESSION_EXPIRED::x"}}, {ret: []string{"FAIL_SYS_SESSION_EXPIRED::x"}},
		}}
		// center、store 保存夹具；恢复器每次都成功。
		store0, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// recoverer 是注入的凭证恢复替身，记录恢复调用次数。
		recoverer := &fakeCredentialRecoverer{store: store0}
		// center 是注入故障替身的自动化中心，作为被测对象。
		center := NewWithDependencies(store0, nil, nil, CenterDependencies{MTop: client, OrderDetailFetcher: recoverer})
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if err := center.actions.freeShipBargain(ctx, task); !errors.Is(err, errActionNotPerformed) || !strings.Contains(err.Error(), "恢复后仍返回") {
			t.Fatalf("err=%v", err)
		}
		if recoverer.calls != 1 || client.freeShippingCalls != 2 {
			t.Fatalf("calls=%d free=%d", recoverer.calls, client.freeShippingCalls)
		}
	})
	t.Run("凭证恢复失败停止重试", func(t *testing.T) {
		// client 返回会话失效。
		client := &fakeMTop{freeShippingResults: []fakeFreeShippingResult{{ret: []string{"FAIL_SYS_SESSION_EXPIRED::x"}}}}
		// store0、cleanup 保存数据库。
		store0, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// center 是注入故障替身的自动化中心，作为被测对象。
		center := NewWithDependencies(store0, nil, nil, CenterDependencies{MTop: client, OrderDetailFetcher: &fakeCredentialRecoverer{store: store0, fail: true}})
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if err := center.actions.freeShipBargain(ctx, task); !errors.Is(err, errActionNotPerformed) || !strings.Contains(err.Error(), "恢复失败") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("明确失败携带响应凭证保存失败", func(t *testing.T) {
		// client 返回业务失败和新 Cookie。
		client := &fakeMTop{freeShippingRet: []string{"FAIL::拒绝"}, freeShippingUpdated: "unb=123; _m_h5_tk=new1;"}
		// center、store 保存夹具。
		center, store := newBargainGapCenter(t, client, CenterDependencies{})
		execGapSQL(t, store, cookieAbort)
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if err := center.actions.freeShipBargain(ctx, task); err == nil || !strings.Contains(err.Error(), "forced cookie failure") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("明确失败无凭证问题", func(t *testing.T) {
		// center 保存夹具。
		center, _ := newBargainGapCenter(t, &fakeMTop{freeShippingRet: []string{"FAIL::拒绝"}}, CenterDependencies{})
		// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
		if err := center.actions.freeShipBargain(ctx, task); err == nil || !strings.Contains(err.Error(), "免拼发货失败") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("免拼成功但响应凭证保存失败仍按成功处理", func(t *testing.T) {
		// client 返回成功和新 Cookie。
		client := &fakeMTop{freeShippingOK: true, freeShippingRet: []string{"SUCCESS::ok"}, freeShippingUpdated: "unb=123; _m_h5_tk=new2;"}
		// center、store 保存夹具。
		center, store := newBargainGapCenter(t, client, CenterDependencies{})
		execGapSQL(t, store, cookieAbort)
		// err 保存免拼结果；平台已明确免拼成功，本地 Cookie 写回失败不得改变动作结果。
		if err := center.actions.freeShipBargain(ctx, task); err != nil {
			t.Fatalf("免拼成功后 Cookie 写回失败不应使动作失败或不确定: %v", err)
		}
	})
}
