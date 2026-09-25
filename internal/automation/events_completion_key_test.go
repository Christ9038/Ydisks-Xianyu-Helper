package automation

import (
	"context"
	"testing"
)

// TestParseUpdateKeyCompletionLayout 验证 t 中的确认收货业务键以首段为订单号，不能把状态码 20 作为订单号。
func TestParseUpdateKeyCompletionLayout(t *testing.T) {
	// cases 的三列依次为业务键、预期会话号和预期订单号；兼容原五段交易卡片格式。
	cases := [][3]string{
		{"900000000001:20:BUYER_CONFIRM_RATE_SELLER:74", "", "900000000001"},
		{"20:20:BUYER_CONFIRM_RATE_SELLER:74", "", ""},
		{"not-an-order:20:BUYER_CONFIRM_RATE_SELLER:74", "", ""},
		{"80000000001:900000000001:10:BUYER_RATE_SELLER:26", "80000000001", "900000000001"},
		{"", "", ""},
	}
	// sample 保存本轮格式样本及其预期身份字段。
	for _, sample := range cases {
		// chatID、orderID 是待验证的解析结果，未知会话必须保持为空。
		chatID, orderID := parseUpdateKey(sample[0])
		if chatID != sample[1] || orderID != sample[2] {
			t.Errorf("业务键 %q 解析为 (%q, %q)，预期 (%q, %q)", sample[0], chatID, orderID, sample[1], sample[2])
		}
	}
}

// TestCompletionKeyCreatesRealAutoRateCandidate 验证 t 中两种真实协议信封都把完成事实及自动评价候选写到真实订单。
func TestCompletionKeyCreatesRealAutoRateCandidate(t *testing.T) {
	// envelopes 保存旧嵌套信封与新版通知信封，样本只含虚构身份和系统文案。
	envelopes := []string{
		`{"1":{"2":"80000000001@goofish","7":1,"10":{"reminderContent":"快给ta一个评价吧～","extJson":"{\"updateKey\":\"900000000001:20:BUYER_CONFIRM_RATE_SELLER:74\",\"contentType\":\"25\"}"}}}`,
		`{"1":"message-1","2":"80000000001@goofish","3":1,"4":{"reminderContent":"快给ta一个评价吧～","extJson":"{\"updateKey\":\"900000000001:20:BUYER_CONFIRM_RATE_SELLER:74\",\"contentType\":\"25\"}"}}`,
	}
	// index、envelope 分别标识信封版本和对应的系统消息样本。
	for index, envelope := range envelopes {
		// store、cleanup 提供隔离 SQLite 和已归属账号，测试结束关闭数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// task 是纯协议解析得到的确认收货任务，不调用任何真实平台。
		task := ExtractTaskFromWS("cid", "", mustMap(t, envelope))
		if task == nil || task.TriggerType != TriggerOrderCompleted || task.OrderID != "900000000001" || task.ChatID != "80000000001" {
			t.Fatalf("信封 %d 未提取真实订单及会话: %+v", index, task)
		}
		// recorder 复用生产完成事实写入路径，验证评价扫描实际消费的记录。
		recorder := newEventFactRecorder(store)
		// recordErr 表示本地完成事实写入失败。
		recordErr := recorder.record(context.Background(), *task)
		if recordErr != nil {
			t.Fatal(recordErr)
		}
		// candidates、queryErr 是自动评价扫描使用的真实仓储结果。
		candidates, queryErr := store.AccountTasks.DueAutoRateOrderIDs(context.Background(), "cid", 10)
		if queryErr != nil || len(candidates) != 1 || candidates[0] != "900000000001" {
			t.Fatalf("评价候选错误: %v, %v", candidates, queryErr)
		}
	}
}
