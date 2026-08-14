package automation

import (
	"encoding/json"
	"testing"
)

// TestExtractTaskFromWS_BuyerReviewed 负责TestExtract任务FromWS买家Reviewed相关处理。
func TestExtractTaskFromWS_BuyerReviewed(t *testing.T) {
	// raw 保存原始，供当前处理流程使用
	raw := mustMap(t, `{
	  "1": {
	    "2": "62904549781@goofish",
	    "10": {
	      "redReminder": "有新交易评价",
	      "reminderContent": "[我完成了评价]",
	      "reminderTitle": "我完成了评价",
	      "senderUserId": "2222315258815",
	      "reminderUrl": "fleamarket://message_chat?itemId=1063217820795&peerUserId=2222315258815&sid=62904549781&messageId=abc&adv=no",
	      "extJson": "{\"updateKey\":\"62904549781:3310145690545023994:10:BUYER_RATE_SELLER:26\",\"contentType\":\"26\"}"
	    }
	  }
	}`)
	// task 保存任务，供当前处理流程使用
	task := ExtractTaskFromWS("acc1", "cookie", raw)
	if task == nil {
		t.Fatal("评价卡片应解析为自动化事件")
	}
	if task.TriggerType != TriggerBuyerReviewed || task.OrderID != "3310145690545023994" ||
		task.ChatID != "62904549781" || task.ItemID != "1063217820795" || task.BuyerID != "2222315258815" {
		t.Fatalf("task=%+v", task)
	}
}

// TestExtractTaskFromWS_BuyerReviewedUsesBusinessKeyAcrossCopyVariants 负责TestExtract任务FromWS买家ReviewedUsesBusinessKeyAcrossCopyVariants相关处理。
func TestExtractTaskFromWS_BuyerReviewedUsesBusinessKeyAcrossCopyVariants(t *testing.T) {
	// raw 保存原始，供当前处理流程使用
	raw := mustMap(t, `{
	  "1":{"2":"62904549781@goofish","10":{
	    "reminderContent":"感谢您的再次购买，评价已经完成",
	    "senderUserId":"buyer-2",
	    "reminderUrl":"fleamarket://message_chat?itemId=item-2&peerUserId=buyer-2&sid=62904549781",
	    "extJson":"{\"updateKey\":\"62904549781:order-second:10:buyer_rate_seller:26\",\"contentType\":\"26\"}"
	  }}
	}`)
	// task 保存任务，供当前处理流程使用
	task := ExtractTaskFromWS("acc1", "cookie", raw)
	if task == nil || task.TriggerType != TriggerBuyerReviewed || task.OrderID != "order-second" {
		t.Fatalf("second-purchase review task=%+v", task)
	}
}

// TestExtractTaskFromWS_ServiceReviewInvitationIgnored 负责TestExtract任务FromWSServiceReviewInvitationIgnored相关处理。
func TestExtractTaskFromWS_ServiceReviewInvitationIgnored(t *testing.T) {
	// raw 保存原始，供当前处理流程使用
	raw := mustMap(t, `{
	  "1": {
	    "2": "62854995941@goofish",
	    "10": {
	      "reminderContent": "为了给您提供更好的服务，诚邀您参与服务评价>>",
	      "reminderTitle": "闲小蜜发来一条新消息",
	      "senderUserId": "1400",
	      "extJson": "{\"messageId\":\"e5e96\"}"
	    }
	  }
	}`)
	if // task 保存任务，供当前处理流程使用
	task := ExtractTaskFromWS("acc1", "cookie", raw); task != nil {
		t.Fatalf("服务评价邀请不能触发买家评价赠品: %+v", task)
	}
}

// TestExtractTaskFromWS_OrderPaid 负责TestExtract任务FromWS订单Paid相关处理。
func TestExtractTaskFromWS_OrderPaid(t *testing.T) {
	// raw 保存原始，供当前处理流程使用
	raw := mustMap(t, `{
	  "1": {
	    "2": "63107041124@goofish",
	    "10": {
	      "redReminder": "等待卖家发货",
	      "reminderContent": "[我已付款，等待你发货]",
	      "senderUserId": "2222315258815",
	      "reminderUrl": "fleamarket://message_chat?itemId=1063177864132&peerUserId=2222315258815&sid=63107041124"
	    },
	    "6": {"3": {"5": "{\"dxCard\":{\"item\":{\"main\":{\"targetUrl\":\"fleamarket://order_detail?id=3310145690545023994&role=seller\"}}}}"}}
	  }
	}`)
	// task 保存任务，供当前处理流程使用
	task := ExtractTaskFromWS("acc1", "cookie", raw)
	if task == nil || task.TriggerType != TriggerOrderPaid || task.OrderID != "3310145690545023994" {
		t.Fatalf("task=%+v", task)
	}
}

// TestExtractTaskFromWS_BuyerOrderPaidIgnored 负责TestExtract任务FromWS买家订单PaidIgnored相关处理。
func TestExtractTaskFromWS_BuyerOrderPaidIgnored(t *testing.T) {
	// raw 保存原始，供当前处理流程使用
	raw := mustMap(t, `{
	  "1": {
	    "2": "63107041124@goofish",
	    "10": {
	      "redReminder": "等待卖家发货",
	      "reminderContent": "[我已付款，等待你发货]",
	      "senderUserId": "2222315258815",
	      "reminderUrl": "fleamarket://message_chat?itemId=1063177864132&peerUserId=2222315258815&sid=63107041124"
	    },
	    "6": {"3": {"5": "{\"dxCard\":{\"item\":{\"main\":{\"targetUrl\":\"fleamarket://order_detail?id=3310145690545023994&role=buyer\"}}}}"}}
	  }
	}`)
	if // task 保存任务，供当前处理流程使用
	task := ExtractTaskFromWS("acc1", "cookie", raw); task != nil {
		t.Fatalf("买家订单不应进入卖家自动化和订单管理: %+v", task)
	}
}

// TestExtractTaskFromWS_BuyerOrderPaidTaskNameIgnored 负责TestExtract任务FromWS买家订单Paid任务名称Ignored相关处理。
func TestExtractTaskFromWS_BuyerOrderPaidTaskNameIgnored(t *testing.T) {
	// raw 保存原始，供当前处理流程使用
	raw := mustMap(t, `{
	  "1": {"2": "63107041124@goofish", "10": {
	    "bizTag": "{\"taskName\":\"付款完成待发货_买家\"}",
	    "redReminder": "等待卖家发货",
	    "reminderContent": "[我已付款，等待你发货]"
	  }}
	}`)
	if // task 保存任务，供当前处理流程使用
	task := ExtractTaskFromWS("acc1", "cookie", raw); task != nil {
		t.Fatalf("买家侧 taskName 不应进入卖家自动化和订单管理: %+v", task)
	}
}

// mustMap 负责mustMap相关处理。
func mustMap(t *testing.T, s string) map[string]any {
	t.Helper()
	// m 保存m，供当前处理流程使用
	var m map[string]any
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}
