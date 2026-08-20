package automation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// TestAdjustOrderPriceActionSuccess 验证改价动作成功时返回一个外部结果并传递整数分价格。
func TestAdjustOrderPriceActionSuccess(t *testing.T) {
	// store、cleanup 保存测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// fake 保存返回业务成功的 MTOP 客户端。
	fake := &fakeMTop{adjustOk: true, adjustRet: []string{"SUCCESS::调用成功"}}
	// center 保存注入测试 MTOP 客户端的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: fake})
	// sent、err 分别是动作报告的外部结果数量和执行错误。
	sent, err := center.executeAction(context.Background(), Task{AccountID: "cid", OrderID: "order-1"},
		db.AutomationAction{ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"9.9"}`})
	if err != nil || sent != 1 {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	if fake.adjustCalls != 1 || fake.adjustOrderIn != "order-1" || fake.adjustCentsIn != 990 {
		t.Fatalf("改价入参错误: calls=%d order=%q cents=%d", fake.adjustCalls, fake.adjustOrderIn, fake.adjustCentsIn)
	}
}

// TestAdjustOrderPriceActionMissingOrderID 验证缺少订单号时动作标记为明确未执行。
func TestAdjustOrderPriceActionMissingOrderID(t *testing.T) {
	// store、cleanup 保存测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// fake 保存不应被调用的 MTOP 客户端。
	fake := &fakeMTop{adjustOk: true}
	// center 保存注入测试 MTOP 客户端的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: fake})
	// sent、err 分别是动作报告的外部结果数量和执行错误。
	sent, err := center.executeAction(context.Background(), Task{AccountID: "cid"},
		db.AutomationAction{ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"9.9"}`})
	if sent != 0 || !errors.Is(err, errActionNotPerformed) {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	if fake.adjustCalls != 0 {
		t.Fatalf("缺少订单号不应触达外部改价: calls=%d", fake.adjustCalls)
	}
}

// TestAdjustOrderPriceActionInvalidConfig 验证目标价格非法时动作标记为明确未执行且不触网。
func TestAdjustOrderPriceActionInvalidConfig(t *testing.T) {
	// store、cleanup 保存测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// fake 保存不应被调用的 MTOP 客户端。
	fake := &fakeMTop{adjustOk: true}
	// center 保存注入测试 MTOP 客户端的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: fake})
	// configs 是应当被拒绝的非法目标价格配置样本。
	configs := []string{`{}`, `{"target_price":""}`, `{"target_price":"abc"}`, `{"target_price":"1.234"}`, `{"target_price":"0"}`, `{"target_price":"-1"}`}
	// config 是当前被验证的非法配置。
	for _, config := range configs {
		// sent、err 分别是动作报告的外部结果数量和执行错误。
		sent, err := center.executeAction(context.Background(), Task{AccountID: "cid", OrderID: "order-1"},
			db.AutomationAction{ActionType: ActionAdjustPrice, ConfigJSON: config})
		if sent != 0 || !errors.Is(err, errActionNotPerformed) {
			t.Fatalf("config=%s sent=%d err=%v", config, sent, err)
		}
	}
	if fake.adjustCalls != 0 {
		t.Fatalf("非法配置不应触达外部改价: calls=%d", fake.adjustCalls)
	}
}

// TestAdjustOrderPriceActionBizFailure 验证平台业务拒绝时返回带业务返回文本的失败。
func TestAdjustOrderPriceActionBizFailure(t *testing.T) {
	// store、cleanup 保存测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// fake 保存返回业务失败的 MTOP 客户端。
	fake := &fakeMTop{adjustOk: false, adjustRet: []string{"FAIL_BIZ_ORDER_NOT_ALLOW_MODIFY::当前订单不允许修改价格"}}
	// center 保存注入测试 MTOP 客户端的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: fake})
	// sent、err 分别是动作报告的外部结果数量和执行错误。
	sent, err := center.executeAction(context.Background(), Task{AccountID: "cid", OrderID: "order-1"},
		db.AutomationAction{ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"9.9"}`})
	if sent != 0 || err == nil || !strings.Contains(err.Error(), "FAIL_BIZ_ORDER_NOT_ALLOW_MODIFY") {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	// uncertain 用于确认业务拒绝不会被误判为结果未知。
	var uncertain *uncertainActionError
	if errors.As(err, &uncertain) {
		t.Fatalf("业务拒绝不应标记为结果未知: %v", err)
	}
}

// TestAdjustOrderPriceActionTransportErrorIsUncertain 验证传输错误标记为结果未知，禁止自动重放。
func TestAdjustOrderPriceActionTransportErrorIsUncertain(t *testing.T) {
	// store、cleanup 保存测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// fake 保存返回传输错误的 MTOP 客户端。
	fake := &fakeMTop{adjustErr: errors.New("网络中断")}
	// center 保存注入测试 MTOP 客户端的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: fake})
	// sent、err 分别是动作报告的外部结果数量和执行错误。
	sent, err := center.executeAction(context.Background(), Task{AccountID: "cid", OrderID: "order-1"},
		db.AutomationAction{ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"9.9"}`})
	if sent != 0 || err == nil {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	// uncertain 用于确认传输错误被标记为结果未知。
	var uncertain *uncertainActionError
	if !errors.As(err, &uncertain) {
		t.Fatalf("传输错误应标记为结果未知: %v", err)
	}
}

// TestAdjustOrderPriceRecoversExpiredSessionOnce 验证 Session 明确失效时只恢复一次凭证，并使用新凭证重新提交改价。
func TestAdjustOrderPriceRecoversExpiredSessionOnce(t *testing.T) {
	// store、cleanup 保存测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是本测试共用的上下文。
	ctx := context.Background()
	// fake 保存先返回会话失效、恢复后返回成功的 MTOP 结果序列。
	fake := &fakeMTop{adjustResults: []fakeAdjustPriceResult{
		{ret: []string{"FAIL_SYS_SESSION_EXPIRED::Session过期"}},
		{ok: true, ret: []string{"SUCCESS::调用成功"}},
	}}
	// recoverer 会把测试账号的 Cookie 更新为新的有效会话。
	recoverer := &fakeCredentialRecoverer{store: store}
	// center 保存注入 MTOP 客户端和凭证恢复器的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: fake, OrderDetailFetcher: recoverer})
	// sent、err 分别保存改价产生的外部结果数和恢复后的执行错误。
	sent, err := center.executeAction(ctx, Task{AccountID: "cid", OrderID: "order-session"},
		db.AutomationAction{ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"9.9"}`})
	if err != nil || sent != 1 {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	if recoverer.calls != 1 || fake.adjustCalls != 2 {
		t.Fatalf("recover calls=%d adjust calls=%d，期望 1/2", recoverer.calls, fake.adjustCalls)
	}
	if len(fake.adjustCookies) != 2 || !strings.Contains(fake.adjustCookies[1], "fresh_1") {
		t.Fatalf("恢复后的改价未使用新凭证: %v", fake.adjustCookies)
	}
}

// TestAdjustOrderPriceDoesNotOverwriteConcurrentCookie 验证改价旧响应返回的 Cookie 不会覆盖外部调用期间并发写入的新凭证。
func TestAdjustOrderPriceDoesNotOverwriteConcurrentCookie(t *testing.T) {
	// store、cleanup 保存测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是本测试共用的上下文。
	ctx := context.Background()
	// initial 是外部请求开始时写入的旧 Cookie。
	initial := "sid=old"
	// err 保存写入初始凭证的错误。
	if err := store.Cookies.UpdateRenewalCookie(ctx, "cid", initial, `{"origin":"old"}`, 1); err != nil {
		t.Fatal(err)
	}
	// started 在改价请求读取旧凭证后通知测试进入并发更新窗口。
	started := make(chan struct{})
	// release 控制暂停的改价请求何时返回旧响应。
	release := make(chan struct{})
	// fake 在调用中暂停，以便测试并发凭证更新；返回成功与过期 Cookie 响应。
	fake := &fakeMTop{adjustOk: true, adjustUpdated: "sid=stale-response", adjustStarted: started, adjustRelease: release}
	// center 保存注入改价替身的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: fake})
	// latest 是并发写入后的新 Cookie，旧响应不允许覆盖它。
	latest := "sid=new-runtime"
	// result 异步接收改价的外部结果数和执行错误，令测试可在网络调用期间写入新凭证。
	result := make(chan struct {
		// sent 是改价产生的外部结果数量。
		sent int
		// err 是改价执行返回的错误。
		err error
	}, 1)
	go func() {
		// sent、runErr 分别保存异步改价产生的外部结果数和执行错误。
		sent, runErr := center.executeAction(ctx, Task{AccountID: "cid", OrderID: "credential-conflict"},
			db.AutomationAction{ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"9.9"}`})
		result <- struct {
			sent int
			err  error
		}{sent: sent, err: runErr}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("订单改价外部调用未开始")
	}
	// err 保存并发更新操作错误；该写入发生在旧响应持久化之前，验证其不会被覆盖。
	if err := store.Cookies.UpdateRenewalCookie(ctx, "cid", latest, `{"origin":"new"}`, 2); err != nil {
		t.Fatal(err)
	}
	close(release)
	// outcome 保存异步改价完成后的外部结果和错误。
	outcome := <-result
	if outcome.sent != 0 || outcome.err == nil || !strings.Contains(outcome.err.Error(), "并发更新冲突") {
		t.Fatalf("sent=%d err=%v", outcome.sent, outcome.err)
	}
	// detail、detailErr 分别保存最终凭证运行视图和读取错误。
	detail, detailErr := store.Cookies.GetDetails(ctx, "cid")
	if detailErr != nil {
		t.Fatal(detailErr)
	}
	if detail.Value != latest {
		t.Fatalf("Cookie 更新错误: got=%q want=%q", detail.Value, latest)
	}
}

// TestParseYuanToCents 验证十进制金额文本到整数分的转换边界。
func TestParseYuanToCents(t *testing.T) {
	// cases 是金额文本与期望整数分（-1 表示期望解析失败）的样本。
	cases := []struct {
		// raw 是输入的金额文本。
		raw string
		// cents 是期望的整数分结果；-1 表示期望返回错误。
		cents int64
	}{
		{"9.9", 990}, {"0.01", 1}, {"12.34", 1234}, {"12", 1200}, {" 5.20 ", 520},
		{"1000000", 100000000},
		{"", -1}, {"abc", -1}, {"1.234", -1}, {"0", -1}, {"-1", -1}, {".5", -1}, {"1000000.01", -1},
	}
	// c 是当前被验证的样本。
	for _, c := range cases {
		// got、err 分别是解析出的整数分和解析错误。
		got, err := parseYuanToCents(c.raw)
		if c.cents == -1 {
			if err == nil {
				t.Fatalf("raw=%q 应解析失败, got=%d", c.raw, got)
			}
			continue
		}
		if err != nil || got != c.cents {
			t.Fatalf("raw=%q got=%d err=%v want %d", c.raw, got, err, c.cents)
		}
	}
}
