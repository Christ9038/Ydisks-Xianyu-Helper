package automation

import (
	"fmt"
	"testing"
)

// roleEventFixture 根据 t 测试上下文、trigger 交易种类、source 角色来源和 role 接收方角色构造单交易消息。
// 返回报文的业务键、链接和嵌套卡片始终指向同一订单；空 role 模拟已有卖家评价的无角色协议。
func roleEventFixture(t *testing.T, trigger, source, role string) map[string]any {
	t.Helper()
	// text、businessKey 分别提供平台展示文案和交易事件业务键，二者均不用于推断接收账号角色。
	text, businessKey := "[我完成了评价]", "BUYER_RATE_SELLER"
	switch trigger {
	case TriggerOrderPaid:
		text, businessKey = "[我已付款，等待你发货]", "TRADE_PAID"
	case TriggerOrderCreated:
		text, businessKey = "[我已拍下，待付款]", "BUYER_CREATE_ORDER"
	}
	// raw 保存单交易固定路径；发送者恰好与接收账号相同也不能单独决定交易角色。
	raw := mustMap(t, fmt.Sprintf(`{"1":{"2":"62904549781@goofish","10":{
		"reminderContent":%q,"senderUserId":"cid",
		"reminderUrl":"fleamarket://message_chat?itemId=1063217820795&peerUserId=cid&sid=62904549781",
		"extJson":%q}}}`, text, fmt.Sprintf(`{"updateKey":"62904549781:3310145690545023994:10:%s:26","contentType":"26"}`, businessKey)))
	if role == "" {
		return raw
	}
	// notice 保存固定路径通知字段；角色只写入 source 指定的位置，以验证每个已有来源独立生效。
	notice := mapAt(mapAt(raw, "1"), "10")
	// link 同时携带该交易订单号和接收方角色，避免跨卡片拼接事实。
	link := "fleamarket://order_detail?id=3310145690545023994&role=" + role
	switch source {
	case "bizTag":
		// label 是平台 taskName 使用的中文交易角色。
		label := "卖家"
		if role == "buyer" {
			label = "买家"
		}
		notice["bizTag"] = fmt.Sprintf(`{"taskName":"交易通知_%s"}`, label)
	case "reminderUrl":
		notice["reminderUrl"] = link + "&itemId=1063217820795"
	case "contentCard":
		mapAt(raw, "1")["6"] = map[string]any{"3": map[string]any{
			"5": fmt.Sprintf(`{"dxCard":{"item":{"main":{"targetUrl":%q}}}}`, link),
		}}
	case "nestedURL":
		raw["payload"] = []any{map[string]any{"targetUrl": link}}
	case "nestedRole":
		raw["payload"] = fmt.Sprintf(`{"trade":{"orderId":"3310145690545023994","orderRole":%q}}`, role)
	default:
		t.Fatalf("未知角色来源 %q", source)
	}
	return raw
}

// TestExtractTaskFromWS_TransactionRoles 使用 t 验证三种交易在所有已有角色来源下拒绝 buyer 并保留 seller/无角色兼容。
func TestExtractTaskFromWS_TransactionRoles(t *testing.T) {
	// trigger 是当前测试的交易种类，入口门禁必须覆盖付款、拍下和评价。
	for _, trigger := range []string{TriggerOrderPaid, TriggerOrderCreated, TriggerBuyerReviewed} {
		// source 是本轮唯一携带角色的位置，避免其他来源掩盖解析遗漏。
		for _, source := range []string{"bizTag", "reminderUrl", "contentCard", "nestedURL", "nestedRole"} {
			// role 是接收方角色；空字符串对应合法旧版无角色卖家事件。
			for _, role := range []string{"buyer", "seller", ""} {
				// t 隔离当前事件种类、来源和角色组合的断言。
				t.Run(trigger+"/"+source+"/"+role, func(t *testing.T) {
					// task 是统一 WS 入口的解析结果；测试 Cookie 为空且禁止输出整个任务。
					task := ExtractTaskFromWS("cid", "", roleEventFixture(t, trigger, source, role))
					if role == "buyer" {
						if task != nil {
							t.Fatal("明确 buyer 的交易副本不能创建卖家自动化任务")
						}
						return
					}
					if task == nil {
						t.Fatal("seller 或合法无角色事件必须保留")
					}
					if task.TriggerType != trigger || task.AccountID != "cid" || task.Source != "ws" ||
						task.OrderID != "3310145690545023994" || task.ChatID != "62904549781" ||
						task.ItemID != "1063217820795" || task.BuyerID != "cid" {
						t.Fatal("合法交易事件的触发类型或订单事实不完整")
					}
				})
			}
		}
	}
}

// TestIsBuyerReviewedEvent_RoleDefense 使用 t 直接验证评价判定函数的防御，避免其他内部调用绕过统一入口。
func TestIsBuyerReviewedEvent_RoleDefense(t *testing.T) {
	// role 是接收方角色；评价动作名称中的 BUYER 并不代表接收方角色。
	for _, role := range []string{"buyer", "seller", ""} {
		// key 覆盖大小写兼容以及没有交易业务键的普通评价邀请。
		for _, key := range []string{"chat:order:10:BUYER_RATE_SELLER:26", "chat:order:10:buyer_rate_seller:26", ""} {
			// want 仅允许非买家接收方携带交易评价业务键时被识别。
			want := role != "buyer" && key != ""
			if isBuyerReviewedEvent(rawFields{orderRole: role, updateKey: key, text: "[我完成了评价]", redReminder: "有新交易评价"}) != want {
				t.Errorf("评价角色=%q 业务键=%q，期望识别=%v", role, key, want)
			}
		}
	}
}
