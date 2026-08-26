package automation

import "testing"

// TestEventFactHelpersCoverProtocolAliases 验证交易事实解析器对字段别名、嵌套 JSON 和交易链接的兼容分支。
func TestEventFactHelpersCoverProtocolAliases(t *testing.T) {
	// fields 保存待补齐的交易事实集合。
	fields := rawFields{}
	supplementEventFactByKey(&fields, "biz_order_id", "123456789012")
	supplementEventFactByKey(&fields, "update-key", "chat-1:123456789012:10:SELLER:26")
	supplementEventFactByKey(&fields, "session_id", "chat-2@goofish")
	supplementEventFactByKey(&fields, "auction_id", "item-1")
	supplementEventFactByKey(&fields, "peer_user_id", "buyer-1")
	supplementEventFactByKey(&fields, "order_role", " seller ")
	if fields.orderID != "123456789012" || fields.updateKey == "" || fields.chatID != "chat-1" || fields.itemID != "item-1" || fields.buyerID != "buyer-1" || fields.orderRole != "seller" {
		t.Fatalf("字段别名解析异常: %+v", fields)
	}
	// urlFields 保存通过 deeplink 补齐事实的空字段集合。
	urlFields := rawFields{}
	supplementEventFactByKey(&urlFields, "deep-link", "fleamarket://message_chat?itemId=item-2&peerUserId=buyer-2&sid=chat-2&role=buyer&id=123456789013")
	if urlFields.itemID != "item-2" || urlFields.buyerID != "buyer-2" || urlFields.chatID != "chat-2" || urlFields.orderID != "123456789013" || urlFields.orderRole != "buyer" {
		t.Fatalf("链接事实解析异常: %+v", urlFields)
	}
	// nestedFields 保存从数组和内嵌 JSON 中补齐的交易事实。
	nestedFields := rawFields{}
	supplementEventFacts(&nestedFields, []any{map[string]any{"trade": `{"bizOrderId":"123456789014","taskName":"卖家待付款"}`}}, 0)
	if nestedFields.orderID != "123456789014" || nestedFields.orderRole != "seller" {
		t.Fatalf("嵌套事实解析异常: %+v", nestedFields)
	}
	supplementEventFacts(&nestedFields, map[string]any{"ignored": "plain text"}, fallbackEventFactsMaxDepth+1)
	supplementEventFacts(nil, map[string]any{"bizOrderId": "123456789015"}, 0)
}

// TestEventFactHelpersCoverValidationAndFallbacks 验证订单标识、角色、扩展字段及更新键的失败与默认分支。
func TestEventFactHelpersCoverValidationAndFallbacks(t *testing.T) {
	supplementEventFactByKey(nil, "orderId", "123456789012")
	supplementEventFactsFromURL(nil, "https://example.test?id=123456789012")
	supplementEventFactsFromURL(&rawFields{}, " ")
	if directOrderID("123") != "" || directOrderID("12345678901x") != "" || directOrderID("123456789012") != "123456789012" {
		t.Fatal("直接订单标识校验异常")
	}
	if trimGoofishSID("chat@goofish") != "chat" || trimGoofishSID("chat") != "chat" {
		t.Fatal("会话标识清理异常")
	}
	// chatID、orderID 保存短更新键解析出的会话和订单标识。
	if chatID, orderID := parseUpdateKey("only-one"); chatID != "" || orderID != "" {
		t.Fatalf("短更新键解析异常: %q %q", chatID, orderID)
	}
	// updateKey、contentType 保存错误扩展字段解析出的空结果。
	if updateKey, contentType := extFields("bad-json"); updateKey != "" || contentType != "" {
		t.Fatalf("错误扩展字段解析异常: %q %q", updateKey, contentType)
	}
	if queryValue("://bad", "id") != "" || orderRoleFromURL(" ") != "" || extractOrderRoleFromContent("bad-json") != "" || extractOrderIDFromContent("bad-json") != "" {
		t.Fatal("链接和内容回退校验异常")
	}
	if orderRoleFromTaskName("未知任务") != "" || normalizedOrderRole("unknown") != "" || bizTaskName("bad-json") != "" {
		t.Fatal("角色回退校验异常")
	}
	// fields 保存已有事实，用于验证后续字段不会覆盖固定路径结果。
	fields := rawFields{orderID: "123456789016", chatID: "fixed-chat", orderRole: "seller"}
	supplementEventFactByKey(&fields, "orderId", "123456789017")
	supplementEventFactByKey(&fields, "chatId", "other-chat")
	supplementEventFactByKey(&fields, "role", "buyer")
	if fields.orderID != "123456789016" || fields.chatID != "fixed-chat" || fields.orderRole != "seller" {
		t.Fatalf("固定事实被覆盖: %+v", fields)
	}
}

// TestFieldsFromRawCoversFallbackPaths 验证平台报文在备用顶层字段和不同卡片内容路径下的事实提取。
func TestFieldsFromRawCoversFallbackPaths(t *testing.T) {
	// raw 保存使用备用字段位置和第三种动态卡片路径的交易报文。
	raw := map[string]any{
		"3": map[string]any{"redReminder": "等待买家付款"},
		"4": map[string]any{
			"reminderContent": "我已拍下，待付款",
			"reminderTitle":   "交易提醒",
			"detailNotice":    "请及时处理",
			"reminderUrl":     "fleamarket://message_chat?itemId=item-3&peerUserId=buyer-3&sid=chat-3&id=123456789017",
			"extJson":         `{"updateKey":"chat-3:123456789017:10:BUYER_CREATE_ORDER:26","contentType":"26"}`,
		},
		"1": map[string]any{"6": map[string]any{"3": map[string]any{"5": `{"dynamicOperation":{"changeContent":{"dxCard":{"item":{"main":{"exContent":{"button":{"targetUrl":"fleamarket://order_detail?id=123456789018&role=seller"}}}}}}}}`}}},
	}
	// fields 保存备用字段路径提取后的交易事实。
	fields := fieldsFromRaw(raw)
	if fields.text != "我已拍下，待付款" || fields.title != "交易提醒" || fields.detail != "请及时处理" || fields.chatID != "chat-3" || fields.itemID != "item-3" || fields.buyerID != "buyer-3" || fields.orderID != "123456789018" {
		t.Fatalf("备用字段提取异常: %+v", fields)
	}
	// sellerRole 保存动态卡片内容中的卖家角色。
	sellerRole := extractOrderRoleFromContent(`{"dynamicOperation":{"changeContent":{"dxCard":{"item":{"main":{"exContent":{"button":{"targetUrl":"fleamarket://order_detail?id=123456789018&role=seller"}}}}}}}}`)
	if sellerRole != "seller" {
		t.Fatalf("动态卡片角色=%q", sellerRole)
	}
	// sellerOrderID 保存动态卡片内容中的订单标识。
	sellerOrderID := extractOrderIDFromContent(`{"dynamicOperation":{"changeContent":{"dxCard":{"item":{"main":{"exContent":{"button":{"targetUrl":"fleamarket://order_detail?id=123456789018&role=seller"}}}}}}}}`)
	if sellerOrderID != "123456789018" {
		t.Fatalf("动态卡片订单号=%q", sellerOrderID)
	}
}

// TestExtractTaskFromWSCoversEmptyAndUnsupportedEvents 验证空报文和无法识别业务事件不会创建自动化任务。
func TestExtractTaskFromWSCoversEmptyAndUnsupportedEvents(t *testing.T) {
	if ExtractTaskFromWS("account", "cookie", nil) != nil {
		t.Fatal("空报文不应生成任务")
	}
	if ExtractTaskFromWS("account", "cookie", map[string]any{}) != nil {
		t.Fatal("无事实报文不应生成任务")
	}
	// unsupportedRaw 保存含有展示文本但不属于自动化交易事件的报文。
	unsupportedRaw := map[string]any{"1": map[string]any{"10": map[string]any{"reminderContent": "普通聊天"}}}
	if ExtractTaskFromWS("account", "cookie", unsupportedRaw) != nil {
		t.Fatal("不支持事件不应生成任务")
	}
}
