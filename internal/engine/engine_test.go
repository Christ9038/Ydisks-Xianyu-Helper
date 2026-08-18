package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
)

// recordingHandler 记录收到的聊天消息，用于断言防抖与去重行为。
type recordingHandler struct {
	mu      sync.Mutex
	chats   []ChatMessage
	refresh int
}

// HandleChatMessage 处理聊天消息。
func (h *recordingHandler) HandleChatMessage(_ context.Context, m ChatMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.chats = append(h.chats, m)
	return nil
}

// HandleSystemEvent 处理系统Event。
func (h *recordingHandler) HandleSystemEvent(_ context.Context, task automation.Task) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	_ = task
	return nil
}

// OnPasswordLoginRefresh 封装On密码登录Refresh业务协调。
func (h *recordingHandler) OnPasswordLoginRefresh(_ context.Context, _ string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.refresh++
	return true
}

// OnAccountAlert 封装On账号Alert业务协调。
func (h *recordingHandler) OnAccountAlert(_ context.Context, _, _, _, _ string) {}

// newAccountForTest 封装new账号ForTest业务协调。
func newAccountForTest(t *testing.T) (*Account, *recordingHandler, *db.Store, func()) {
	t.Helper()
	// dbPath 用于本次流程后续判断的db路径
	dbPath := filepath.Join(t.TempDir(), "test.db")
	// d、err 用于本次流程后续判断的d、err
	d, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// store 用于本次流程后续判断的store
	store := db.NewStore(d, db.DialectSQLite)
	store.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	store.Cookies.Save(context.Background(), "cid", "unb=123; _m_h5_tk=tk_1;", admin.ID)
	store.Cookies.SetStatus(context.Background(), "cid", true)

	// h 用于本次流程后续判断的h
	h := &recordingHandler{}
	// acc 用于本次流程后续判断的acc
	acc := New(Config{
		CookieID:  "cid",
		CookieStr: "unb=123; _m_h5_tk=tk_1;",
		Store:     store,
		Handler:   h,
	})
	// 通用账号测试默认不启用 loginuser.get；需要验证登录态恢复的用例会
	// 显式注入 statusMtop，避免单元测试访问真实网络。
	acc.mtop = &fakeRunMtop{token: "test-token"}
	return acc, h, store, func() { d.Close() }
}

// TestExtractChatMessage_RealSample 用真实抓包样本验证消息字段提取。
func TestExtractChatMessage_RealSample(t *testing.T) {
	// decrypted 用于本次流程后续判断的decrypted
	decrypted := mustDecryptGoldenSample(t)
	// chat 用于本次流程后续判断的聊天
	chat := extractChatMessage(decrypted, "cid", "cookie")
	if chat == nil {
		t.Fatal("应为聊天消息")
	}
	if chat.ChatID != "47983389009" {
		t.Errorf("ChatID=%q want 47983389009", chat.ChatID)
	}
	if chat.Text != "[我已拍下，待付款]" {
		t.Errorf("Text=%q want [我已拍下，待付款]", chat.Text)
	}
	if chat.ItemID != "900052644277" {
		t.Errorf("ItemID=%q want 900052644277", chat.ItemID)
	}
}

// TestExtractChatMessage_FiltersContentType14Notice 封装TestExtract聊天消息Filters内容Type14Notice业务协调。
func TestExtractChatMessage_FiltersContentType14Notice(t *testing.T) {
	// decrypted 用于本次流程后续判断的decrypted
	decrypted := mustContentType14Notice(t)
	if // chat 用于本次流程后续判断的聊天
	chat := extractChatMessage(decrypted, "cid", "cookie"); chat != nil {
		t.Fatalf("contentType=14 系统提示不应进入聊天回复: %+v", chat)
	}
}

// TestExtractChatMessageUsesReminderTitleAsNickname 封装TestExtract聊天消息UsesReminder标题AsNickname业务协调。
func TestExtractChatMessageUsesReminderTitleAsNickname(t *testing.T) {
	// decrypted 用于本次流程后续判断的decrypted
	decrypted := map[string]any{"1": map[string]any{
		"2":  "chat-1@goofish",
		"10": map[string]any{"reminderContent": "你好", "reminderTitle": "真实昵称", "senderUserId": "buyer-1", "sessionType": "1"},
	}}
	// chat 用于本次流程后续判断的聊天
	chat := extractChatMessage(decrypted, "account-1", "cookie")
	if chat == nil || chat.SenderName != "真实昵称" {
		t.Fatalf("chat=%+v", chat)
	}
}

// TestExtractChatMessageIgnoresOwnWebSocketEcho 验证账号自身的 WebSocket 回显不会进入回复链。
func TestExtractChatMessageIgnoresOwnWebSocketEcho(t *testing.T) {
	// decrypted 模拟账号自身发送后由同一 WebSocket 回传的解密聊天信封。
	decrypted := map[string]any{"1": map[string]any{
		"2":  "chat-1@goofish",
		"10": map[string]any{"reminderContent": "我在官方客户端发送的消息", "senderUserId": "self-1", "senderNick": "我"},
	}}
	// chat 是提取结果；账号自身的回显必须被过滤，因此应为 nil。
	if chat := extractChatMessage(decrypted, "account-1", "unb=self-1;"); chat != nil {
		t.Fatalf("账号自身发送的 WS 回显不应进入自动回复链: %+v", chat)
	}
}

// TestExtractChatMessage_FiltersRefundTradeCard 验证退款交易卡不会被识别为用户聊天。
func TestExtractChatMessage_FiltersRefundTradeCard(t *testing.T) {
	// decrypted 用于本次流程后续判断的decrypted
	decrypted := mustRefundTradeCard(t)
	if // chat 用于本次流程后续判断的聊天
	chat := extractChatMessage(decrypted, "cid", "cookie"); chat != nil {
		t.Fatalf("退款交易卡片不应进入聊天回复: %+v", chat)
	}
}

// TestExtractChatMessage_FiltersPaidDeliveryCardFromChat 封装TestExtract聊天消息FiltersPaid发货卡密From聊天业务协调。
func TestExtractChatMessage_FiltersPaidDeliveryCardFromChat(t *testing.T) {
	// decrypted 用于本次流程后续判断的decrypted
	decrypted := mustPaidDeliveryCard(t)
	if // chat 用于本次流程后续判断的聊天
	chat := extractChatMessage(decrypted, "cid", "cookie"); chat != nil {
		t.Fatalf("付款待发货系统卡片不应进入聊天回复链: %+v", chat)
	}
	// task 用于本次流程后续判断的任务
	task := automation.ExtractTaskFromWS("cid", "cookie", decrypted)
	if task == nil || task.TriggerType != automation.TriggerOrderPaid {
		t.Fatalf("付款待发货卡片应进入自动化中心: %+v", task)
	}
}

// TestDedup_SkipsDuplicateWithinExpiry 同一消息 ID 1 小时内只处理一次。
func TestDedup_SkipsDuplicateWithinExpiry(t *testing.T) {
	// acc、h、cleanup 用于本次流程后续判断的acc、h、cleanup
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	// decrypted 用于本次流程后续判断的decrypted
	decrypted := mustDecryptGoldenSample(t)
	// chat 用于本次流程后续判断的聊天
	chat := extractChatMessage(decrypted, "cid", "cookie")

	// 首次：标记并应继续。
	if !acc.markAndCheckDedup(decrypted, chat) {
		t.Fatal("首次应允许处理")
	}
	// 第二次同一消息 ID：应跳过。
	if acc.markAndCheckDedup(decrypted, chat) {
		t.Fatal("重复消息应被去重跳过")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.chats) != 0 {
		t.Errorf("去重阶段不应投递消息，got %d", len(h.chats))
	}
}

// TestDebounce_CoalescesRapidMessages 连续消息只投递最后一条。
func TestDebounce_CoalescesRapidMessages(t *testing.T) {
	// acc、h、cleanup 用于本次流程后续判断的acc、h、cleanup
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	// decrypted 用于本次流程后续判断的decrypted
	decrypted := mustDecryptGoldenSample(t)
	// chat 用于本次流程后续判断的聊天
	chat := extractChatMessage(decrypted, "cid", "cookie")
	// 修改文本模拟连续多条。
	chat1 := *chat
	chat1.Text = "第一条"
	// chat2 用于本次流程后续判断的chat2
	chat2 := *chat
	chat2.Text = "第二条"
	// chat3 用于本次流程后续判断的chat3
	chat3 := *chat
	chat3.Text = "第三条"

	// 用不同 decrypted（不同 msgID）触发防抖，验证同一 chat_id 合并。
	acc.scheduleDebouncedReply(chat1)
	acc.scheduleDebouncedReply(chat2)
	acc.scheduleDebouncedReply(chat3)

	// 等待防抖延迟 + 余量。
	time.Sleep(MessageDebounceDelay + 200*time.Millisecond)

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.chats) != 1 {
		t.Fatalf("防抖应只投递最后一条，got %d", len(h.chats))
	}
	if h.chats[0].Text != "第三条" {
		t.Errorf("应投递最后一条，got %q", h.chats[0].Text)
	}
}

// TestExtractItemID reminderUrl 中提取 itemId。
func TestExtractItemID(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := map[string]string{
		"fleamarket://message_chat?itemId=900052644277&peerUserId=3149637063": "900052644277",
		"noitemid":                     "",
		"fleamarket://x?itemId=ABC123": "ABC123",
	}
	// in、want 表示当前遍历过程中的in、want
	for in, want := range cases {
		if // got 用于本次流程后续判断的got
		got := extractItemID(in); got != want {
			t.Errorf("extractItemID(%q)=%q want %q", in, got, want)
		}
	}
}

// mustDecryptGoldenSample 复用 protocol 包的真实样本：直接硬编码一条最小解密结构，
// 避免循环依赖。这里构造一个等价的最小消息用于字段提取测试。
// mustDecryptGoldenSample 封装mustDecryptGoldenSample业务协调。
func mustDecryptGoldenSample(t *testing.T) map[string]any {
	t.Helper()
	// 真实样本关键字段（来自 protocol golden test 解密输出）：
	// message["1"]["2"]="47983389009@goofish", ["1"]["10"]["reminderContent"]="[我已拍下，待付款]",
	// ["1"]["10"]["reminderUrl"] 含 itemId=900052644277
	// s 用于本次流程后续判断的s
	s := `{
	  "1": {
	    "2": "47983389009@goofish",
	    "10": {
	      "reminderContent": "[我已拍下，待付款]",
	      "senderNick": "买家昵称",
	      "senderUserId": "3149637063",
	      "reminderUrl": "fleamarket://message_chat?itemId=900052644277&peerUserId=3149637063"
	    }
	  },
	  "3": {
	    "redReminder": "等待买家付款",
	    "userId": "3149637063"
	  }
	}`
	// m 用于本次流程后续判断的m
	var m map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// mustContentType14Notice 封装must内容Type14Notice业务协调。
func mustContentType14Notice(t *testing.T) map[string]any {
	t.Helper()
	// s 用于本次流程后续判断的s
	s := "{" +
		"\"1\":{" +
		"\"2\":\"63107041124@goofish\"," +
		"\"10\":{" +
		"\"extJson\":\"{\\\"contentType\\\":\\\"14\\\",\\\"messageId\\\":\\\"d050b73332b94d5a8901cff78519483a\\\"}\"," +
		"\"reminderContent\":\"[不想宝贝被砍价?设置不砍价回复  ]\"," +
		"\"senderUserId\":\"2222315258815\"," +
		"\"reminderUrl\":\"fleamarket://message_chat?itemId=1063177864132&peerUserId=2222315258815\"" +
		"}," +
		"\"6\":{\"3\":{\"4\":14,\"5\":\"{\\\"contentType\\\":14,\\\"tip\\\":{\\\"argInfo\\\":{\\\"arg1\\\":\\\"NoBargainGuide\\\"}}}\"}}" +
		"}" +
		"}"
	// m 用于本次流程后续判断的m
	var m map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// mustRefundTradeCard 封装mustRefundTrade卡密业务协调。
func mustRefundTradeCard(t *testing.T) map[string]any {
	t.Helper()
	// s 用于本次流程后续判断的s
	s := "{" +
		"\"1\":{" +
		"\"2\":\"63107041124@goofish\"," +
		"\"10\":{" +
		"\"bizTag\":\"{\\\"sourceId\\\":\\\"C2C:eeg858GGuju9\\\",\\\"taskName\\\":\\\"发起退款申请_卖家-新逆向url\\\"}\"," +
		"\"extJson\":\"{\\\"msgArg1\\\":\\\"MsgCard\\\",\\\"contentType\\\":\\\"26\\\",\\\"messageId\\\":\\\"3a03978b7a374da898b3d7a084cbedb6\\\"}\"," +
		"\"redReminder\":\"买家申请退款\"," +
		"\"reminderContent\":\"[我发起了退款申请]\"," +
		"\"senderUserId\":\"2222315258815\"," +
		"\"reminderUrl\":\"fleamarket://message_chat?itemId=1063177864132&peerUserId=2222315258815\"" +
		"}," +
		"\"6\":{\"3\":{\"4\":26,\"5\":\"{\\\"contentType\\\":26}\"}}" +
		"}" +
		"}"
	// m 用于本次流程后续判断的m
	var m map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// mustPaidDeliveryCard 封装mustPaid发货卡密业务协调。
func mustPaidDeliveryCard(t *testing.T) map[string]any {
	t.Helper()
	// s 用于本次流程后续判断的s
	s := "{" +
		"\"1\":{" +
		"\"2\":\"63107041124@goofish\"," +
		"\"10\":{" +
		"\"bizTag\":\"{\\\"sourceId\\\":\\\"C2C:4Ytd4BSQKIiz\\\",\\\"taskName\\\":\\\"付款完成待发货_卖家-正向升级\\\"}\"," +
		"\"extJson\":\"{\\\"msgArg1\\\":\\\"MsgCard\\\",\\\"contentType\\\":\\\"26\\\",\\\"messageId\\\":\\\"4e449a32c59c499594c4c5dffa5ddef0\\\"}\"," +
		"\"redReminder\":\"等待卖家发货\"," +
		"\"reminderContent\":\"[我已付款，等待你发货]\"," +
		"\"senderUserId\":\"2222315258815\"," +
		"\"reminderUrl\":\"fleamarket://message_chat?itemId=1063177864132&peerUserId=2222315258815\"" +
		"}," +
		"\"6\":{\"3\":{\"4\":26,\"5\":\"{\\\"contentType\\\":26}\"}}" +
		"}" +
		"}"
	// m 用于本次流程后续判断的m
	var m map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// TestRetryDelay 复刻 _calculate_retry_delay 的分段逻辑。
func TestRetryDelay(t *testing.T) {
	// acc、cleanup 用于本次流程后续判断的acc、cleanup
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	acc.connFailures = 1
	expectDelayRange(t, acc.retryDelay("no close frame received or sent"), 2*time.Second)
	acc.connFailures = 10
	expectDelayRange(t, acc.retryDelay("no close frame received or sent"), 30*time.Second)
	acc.connFailures = 2
	expectDelayRange(t, acc.retryDelay("connection refused"), 8*time.Second)
	acc.connFailures = 10
	expectDelayRange(t, acc.retryDelay("connection refused"), 90*time.Second)
	acc.connFailures = 1
	expectDelayRange(t, acc.retryDelay("some other error"), 2*time.Second)
}

// expectDelayRange 封装expect延迟Range业务协调。
func expectDelayRange(t *testing.T, got, base time.Duration) {
	t.Helper()
	// max 用于本次流程后续判断的max
	max := base + base*3/10
	if got < base || got > max {
		t.Fatalf("retryDelay=%v want in [%v,%v]", got, base, max)
	}
}

// TestRuntimeStatusClassifiesAuthenticationFailures 封装TestRuntime状态ClassifiesAuthenticationFailures业务协调。
func TestRuntimeStatusClassifiesAuthenticationFailures(t *testing.T) {
	// acc、cleanup 用于本次流程后续判断的acc、cleanup
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()

	acc.setRuntimeError(context.Background(), fmt.Errorf("token API 登录凭证已失效: FAIL_SYS_TOKEN_EXOIRED"))
	// status 用于本次流程后续判断的状态
	status := acc.RuntimeStatus()
	if status.State != RuntimeAuthExpired || status.Connected {
		t.Fatalf("status=%+v", status)
	}

	acc.setRuntimeError(context.Background(), fmt.Errorf("FAIL_SYS_USER_VALIDATE: captcha required"))
	status = acc.RuntimeStatus()
	if status.State != RuntimeVerificationRequired {
		t.Fatalf("status=%+v", status)
	}
}
