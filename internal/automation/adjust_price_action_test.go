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

// TestAdjustOrderPriceActionRetriesTransientBusy 验证平台返回“暂无法修改价格”时动作内短间隔重试直至成功。
func TestAdjustOrderPriceActionRetriesTransientBusy(t *testing.T) {
	// store、cleanup 保存测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// originalGap 保存原始重试间隔，测试结束后恢复。
	originalGap := adjustPriceTransientRetryGap
	adjustPriceTransientRetryGap = time.Millisecond
	defer func() { adjustPriceTransientRetryGap = originalGap }()
	// fake 先两次返回暂不可改价、第三次返回成功。
	fake := &fakeMTop{adjustResults: []fakeConsignResult{
		{ok: false, ret: []string{"FAIL_BIZ_CANNOT_MODIFY_FEE::暂无法修改价格，请稍后重试"}},
		{ok: false, ret: []string{"FAIL_BIZ_CANNOT_MODIFY_FEE::暂无法修改价格，请稍后重试"}},
		{ok: true, ret: []string{"SUCCESS::调用成功"}},
	}}
	// center 保存注入测试 MTOP 客户端的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: fake})
	// sent、err 分别是动作报告的外部结果数量和执行错误。
	sent, err := center.executeAction(context.Background(), Task{AccountID: "cid", OrderID: "order-1"},
		db.AutomationAction{ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"9.9"}`})
	if err != nil || sent != 1 || fake.adjustCalls != 3 {
		t.Fatalf("sent=%d calls=%d err=%v", sent, fake.adjustCalls, err)
	}
}

// TestAdjustOrderPriceActionTransientBusyExhausted 验证暂不可改价持续存在时按上限停止并返回错误。
func TestAdjustOrderPriceActionTransientBusyExhausted(t *testing.T) {
	// store、cleanup 保存测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// originalGap 保存原始重试间隔，测试结束后恢复。
	originalGap := adjustPriceTransientRetryGap
	adjustPriceTransientRetryGap = time.Millisecond
	defer func() { adjustPriceTransientRetryGap = originalGap }()
	// fake 始终返回暂不可改价。
	fake := &fakeMTop{adjustRet: []string{"FAIL_BIZ_CANNOT_MODIFY_FEE::暂无法修改价格，请稍后重试"}}
	// center 保存注入测试 MTOP 客户端的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: fake})
	// sent、err 分别是动作报告的外部结果数量和执行错误。
	sent, err := center.executeAction(context.Background(), Task{AccountID: "cid", OrderID: "order-1"},
		db.AutomationAction{ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"9.9"}`})
	if sent != 0 || err == nil || !strings.Contains(err.Error(), "CANNOT_MODIFY_FEE") {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	if fake.adjustCalls != adjustPriceTransientRetryLimit {
		t.Fatalf("重试次数应达到上限: calls=%d want=%d", fake.adjustCalls, adjustPriceTransientRetryLimit)
	}
}

// TestAdjustOrderPriceActionOrderStateFinalSkips 验证订单状态不支持改价时按放弃处理，不重试也不报错。
func TestAdjustOrderPriceActionOrderStateFinalSkips(t *testing.T) {
	// store、cleanup 保存测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// fake 返回订单状态不支持改价的终态业务错误。
	fake := &fakeMTop{adjustRet: []string{"FAIL_BIZ_BAD_REQUEST::当前订单状态不支持改价"}}
	// center 保存注入测试 MTOP 客户端的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: fake})
	// sent、err 分别是动作报告的外部结果数量和执行错误。
	sent, err := center.executeAction(context.Background(), Task{AccountID: "cid", OrderID: "order-1"},
		db.AutomationAction{ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"9.9"}`})
	if sent != 0 || err != nil || fake.adjustCalls != 1 {
		t.Fatalf("终态失败应放弃且不报错: sent=%d calls=%d err=%v", sent, fake.adjustCalls, err)
	}
}
