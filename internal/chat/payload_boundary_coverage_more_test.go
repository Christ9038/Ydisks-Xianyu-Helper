package chat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// failingRandomRead 模拟系统随机源不可用时的错误结果。
func failingRandomRead([]byte) (int, error) { return 0, errors.New("random source unavailable") }

// TestChatPayloadHelpersCoverTimestampAndOfficialContentBranches 验证聊天时间戳兼容和官方消息类型递归识别边界。
func TestChatPayloadHelpersCoverTimestampAndOfficialContentBranches(t *testing.T) {
	// timestampCases 保存秒级、毫秒级、候选字段和非法时间样例。
	timestampCases := []struct {
		// raw 是聊天载荷中的候选时间字段。
		raw map[string]any
		// want 是统一输出的毫秒时间戳。
		want int64
	}{
		{raw: map[string]any{"sendTime": "1700000000"}, want: 1700000000000},
		{raw: map[string]any{"timestamp": "1700000000123"}, want: 1700000000123},
		{raw: map[string]any{"createdAt": "invalid"}, want: 0},
		{raw: nil, want: 0},
	}
	// timestampCase 表示当前聊天时间样例。
	for _, timestampCase := range timestampCases {
		// got 保存时间戳归一化后的毫秒值。
		if got := extractUnixMilli(timestampCase.raw); got != timestampCase.want {
			t.Errorf("时间载荷=%v got=%d want=%d", timestampCase.raw, got, timestampCase.want)
		}
	}
	// preferredTimestamp 同时包含多个时间别名，用于验证候选键优先级不受 map 遍历顺序影响。
	preferredTimestamp := map[string]any{"timestamp": "1700000000123", "sendTime": "1700000000"}
	// iteration 表示重复执行确定性提取的测试轮次。
	for iteration := 0; iteration < 100; iteration++ {
		// got 保存当前轮次按候选优先级提取并归一化的时间戳。
		if got := extractUnixMilli(preferredTimestamp); got != 1700000000000 {
			t.Fatalf("时间候选优先级不稳定 iteration=%d got=%d", iteration, got)
		}
	}
	// preferredMessageID 同时包含新旧消息键别名，用于验证调用方顺序优先于对象字段排序。
	preferredMessageID := map[string]any{"message_id": "legacy-id", "messageId": "official-id"}
	// got 保存按新旧消息键候选优先级提取的标识。
	if got := extractString(preferredMessageID, "messageId", "message_id"); got != "official-id" {
		t.Fatalf("消息键候选优先级异常 got=%q", got)
	}
	// nestedMessageID 使用逆序构造的兄弟节点，验证递归搜索采用稳定字段顺序。
	nestedMessageID := map[string]any{"z-node": map[string]any{"messageId": "z-id"}, "a-node": map[string]any{"messageId": "a-id"}}
	// got 保存按稳定兄弟节点顺序递归提取的消息标识。
	if got := extractString(nestedMessageID, "messageId"); got != "a-id" {
		t.Fatalf("嵌套消息键递归顺序异常 got=%q", got)
	}
	// officialCases 保存直接字段、嵌套 JSON、数组和未识别内容类型样例。
	officialCases := []struct {
		// raw 是待识别的聊天内容载荷。
		raw any
		// want 是识别出的官方内容类型。
		want string
	}{
		{raw: map[string]any{"contentType": "14"}, want: "14"},
		{raw: `{"custom":{"data":"{\"contentType\":\"26\"}"}}`, want: "26"},
		{raw: []any{map[string]any{"contentType": "9"}}, want: ""},
		{raw: map[string]any{"first": map[string]any{"contentType": "14"}}, want: "14"},
		{raw: map[string]any{"second": map[string]any{"contentType": "26"}}, want: "26"},
		{raw: "not-json", want: ""},
	}
	// officialCase 表示当前官方消息内容类型样例。
	for _, officialCase := range officialCases {
		// got 保存递归内容类型识别结果。
		if got := findOfficialContentType(officialCase.raw); got != officialCase.want {
			t.Errorf("官方内容载荷=%v got=%q want=%q", officialCase.raw, got, officialCase.want)
		}
	}
	// fallbackOfficial 验证无协议类型但带官方提示文本时仍识别为系统消息。
	if !isOfficialSystemMessage(map[string]any{}, "buyer@goofish", "发来一条新消息") {
		t.Fatal("官方提示文本应识别为系统消息")
	}
	if !isOfficialSystemMessage(map[string]any{}, "1400@goofish", "普通文本") {
		t.Fatal("闲小蜜发送者应识别为系统消息")
	}
	// originalRandomRead 保存生产随机读取函数，测试结束后恢复全局依赖。
	originalRandomRead := readRandomBytes
	readRandomBytes = failingRandomRead
	defer func() { readRandomBytes = originalRandomRead }()
	// fallbackID 保存随机源失败时基于时间生成的本地消息键。
	fallbackID := randomID()
	if fallbackID == "" {
		t.Fatal("随机源失败时仍应生成非空消息键")
	}
	// arrayImage 保存数组嵌套图片载荷，覆盖内容递归遍历的数组分支。
	arrayImage := []any{map[string]any{"contentType": "2", "image": map[string]any{"url": "https://cdn/image.jpg"}}}
	// arrayKind、arrayContent 保存数组图片载荷的解析结果。
	arrayKind, arrayContent := extractMessageContent(map[string]any{"items": arrayImage}, "")
	if arrayKind != "image" || arrayContent != "https://cdn/image.jpg" {
		t.Fatalf("数组图片内容解析异常 kind=%q content=%q", arrayKind, arrayContent)
	}
	// nestedArray 保存对象再嵌套数组的媒体载荷，覆盖对象子节点递归分支。
	nestedArray := map[string]any{"children": []any{map[string]any{"video": map[string]any{"url": "https://cdn/video.mp4"}}}}
	// nestedKind、nestedContent 保存对象数组视频载荷的解析结果。
	nestedKind, nestedContent := extractMessageContent(nestedArray, "")
	if nestedKind != "video" || nestedContent != "https://cdn/video.mp4" {
		t.Fatalf("对象数组视频内容解析异常 kind=%q content=%q", nestedKind, nestedContent)
	}
	// systemService 保存本地未读查询失败时用于验证系统消息扣除规则的服务。
	systemService := NewWithRepository(&unreadRepository{err: errors.New("unread unavailable")})
	// systemLast 保存缺少未读数且未标记已读的系统消息。
	systemLast := map[string]any{"extension": map[string]any{"senderUserId": "100"}, "readStatus": 1}
	// defaultSystemUnread 保存系统消息默认扣除一条后的结果。
	defaultSystemUnread := systemService.conversationUnreadCount(context.Background(), "cid", "chat", "buyer", map[string]any{"redPoint": 3}, systemLast, "发来一条新消息")
	if defaultSystemUnread != 2 {
		t.Fatalf("系统消息默认扣除异常 got=%d", defaultSystemUnread)
	}
	// cappedSystemLast 保存系统未读数超过官方红点的载荷。
	cappedSystemLast := map[string]any{"extension": map[string]any{"senderUserId": "100"}, "unreadCount": 9}
	// cappedSystemUnread 保存系统未读数按官方红点封顶后的结果。
	cappedSystemUnread := systemService.conversationUnreadCount(context.Background(), "cid", "chat", "buyer", map[string]any{"redPoint": 3}, cappedSystemLast, "发来一条新消息")
	if cappedSystemUnread != 0 {
		t.Fatalf("系统消息扣除封顶异常 got=%d", cappedSystemUnread)
	}
}

// TestChatServiceCoversRepositoryAndIdentityBoundaries 验证聊天服务的空依赖、摘要键和订阅背压边界。
func TestChatServiceCoversRepositoryAndIdentityBoundaries(t *testing.T) {
	// _, subscribeErr 保存账号归属查询失败时的订阅结果。
	_, subscribeCancel, subscribeErr := NewWithRepository(&fakeRepository{ownedErr: errors.New("ownership failed")}).Subscribe(context.Background(), 1)
	if subscribeCancel != nil || subscribeErr == nil {
		t.Fatalf("订阅归属错误异常 cancelExists=%t err=%v", subscribeCancel != nil, subscribeErr)
	}
	// service 保存无数据库依赖的聊天服务，用于验证确定性消息键。
	service := NewWithRepository(&fakeRepository{})
	// nilService 验证 nil 接收者不会触发聊天持久化副作用。
	var nilService *Service
	// _, _, nilErr 保存 nil 接收者的初始化错误。
	_, _, nilErr := nilService.RecordIncoming(context.Background(), Incoming{})
	if nilErr == nil {
		t.Fatal("nil 聊天服务应返回初始化错误")
	}
	// digestMessage、digestInserted、digestErr 保存缺失平台消息键时的摘要幂等结果。
	digestMessage, digestInserted, digestErr := service.RecordIncoming(context.Background(), Incoming{AccountID: "cid", ChatID: "chat", BuyerID: "buyer", Text: "hello", Raw: map[string]any{"kind": "text"}})
	if digestErr != nil || !digestInserted || digestMessage == nil || !strings.HasPrefix(digestMessage.MessageKey, "in-") {
		t.Fatalf("摘要消息键异常 message=%+v inserted=%v err=%v", digestMessage, digestInserted, digestErr)
	}
	// invalidHistory 保存缺少 message 对象的历史模型。
	invalidHistory := map[string]any{"userMessageModels": []any{map[string]any{}, map[string]any{"message": map[string]any{}}}}
	// historyPage、historyErr 保存历史模型被安全跳过的结果。
	historyPage, historyErr := service.RecordHistoryPage(context.Background(), "cid", "chat", "me", db.ChatSession{}, invalidHistory)
	if historyErr != nil || len(historyPage.Messages) != 0 {
		t.Fatalf("无效历史模型处理异常 page=%+v err=%v", historyPage, historyErr)
	}
	// historyOfficial 保存发送者为闲小蜜的历史系统消息模型。
	historyOfficial := map[string]any{"message": map[string]any{"messageId": "official", "extension": map[string]any{"senderUserId": "1400@goofish"}, "content": map[string]any{"custom": map[string]any{"summary": "通知"}}}}
	// officialMessage、officialOK 保存历史系统消息解析结果。
	officialMessage, officialOK := parseHistoryMessage("cid", "chat", "me", historyOfficial)
	if !officialOK || officialMessage.MessageType != "system" || officialMessage.SenderName != "闲小蜜" {
		t.Fatalf("历史系统消息解析异常 message=%+v ok=%v", officialMessage, officialOK)
	}
	// invalidPeerBody 保存没有可用对端标识的联系人载荷。
	invalidPeerBody := map[string]any{"userConvs": []any{map[string]any{"singleChatUserConversation": map[string]any{"singleChatConversation": map[string]any{"cid": "chat@goofish", "pairFirst": "", "pairSecond": ""}}}}}
	// invalidPeerErr 保存无效对端联系人载荷的处理错误。
	_, invalidPeerErr := service.RecordConversationPage(context.Background(), "cid", "me", invalidPeerBody)
	if invalidPeerErr != nil {
		t.Fatalf("无效对端联系人不应报错: %v", invalidPeerErr)
	}
	// events、cancel、eventErr 保存订阅成功后的事件通道和取消函数。
	events, cancel, eventErr := service.Subscribe(context.Background(), 1)
	if eventErr != nil {
		t.Fatal(eventErr)
	}
	// index 表示用于填满订阅缓冲区的事件序号。
	for index := 0; index < 129; index++ {
		service.Publish("cid", Event{Type: "message.created"})
	}
	cancel()
	if events == nil {
		t.Fatal("订阅成功后事件通道不应为空")
	}
}
