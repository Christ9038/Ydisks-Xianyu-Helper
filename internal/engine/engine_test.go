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

func (h *recordingHandler) HandleChatMessage(_ context.Context, m ChatMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.chats = append(h.chats, m)
	return nil
}
func (h *recordingHandler) HandleSystemEvent(_ context.Context, task automation.Task) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	_ = task
	return nil
}
func (h *recordingHandler) OnPasswordLoginRefresh(_ context.Context, _ string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.refresh++
	return true
}
func (h *recordingHandler) OnAccountAlert(_ context.Context, _, _, _, _ string) {}

func newAccountForTest(t *testing.T) (*Account, *recordingHandler, *db.Store, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store := db.NewStore(d, db.DialectSQLite)
	store.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	store.Cookies.Save(context.Background(), "cid", "unb=123; _m_h5_tk=tk_1;", admin.ID)
	store.Cookies.SetStatus(context.Background(), "cid", true)

	h := &recordingHandler{}
	acc := New(Config{
		CookieID:  "cid",
		CookieStr: "unb=123; _m_h5_tk=tk_1;",
		Store:     store,
		Handler:   h,
	})
	return acc, h, store, func() { d.Close() }
}

// TestExtractChatMessage_RealSample 用真实抓包样本验证消息字段提取。
func TestExtractChatMessage_RealSample(t *testing.T) {
	decrypted := mustDecryptGoldenSample(t)
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

func TestExtractChatMessage_FiltersContentType14Notice(t *testing.T) {
	decrypted := mustContentType14Notice(t)
	if chat := extractChatMessage(decrypted, "cid", "cookie"); chat != nil {
		t.Fatalf("contentType=14 系统提示不应进入聊天回复: %+v", chat)
	}
}

func TestExtractChatMessage_FiltersRefundTradeCard(t *testing.T) {
	decrypted := mustRefundTradeCard(t)
	if chat := extractChatMessage(decrypted, "cid", "cookie"); chat != nil {
		t.Fatalf("退款交易卡片不应进入聊天回复: %+v", chat)
	}
}

func TestExtractChatMessage_FiltersPaidDeliveryCardFromChat(t *testing.T) {
	decrypted := mustPaidDeliveryCard(t)
	if chat := extractChatMessage(decrypted, "cid", "cookie"); chat != nil {
		t.Fatalf("付款待发货系统卡片不应进入聊天回复链: %+v", chat)
	}
	task := automation.ExtractTaskFromWS("cid", "cookie", decrypted)
	if task == nil || task.TriggerType != automation.TriggerOrderPaid {
		t.Fatalf("付款待发货卡片应进入自动化中心: %+v", task)
	}
}

// TestDedup_SkipsDuplicateWithinExpiry 同一消息 ID 1 小时内只处理一次。
func TestDedup_SkipsDuplicateWithinExpiry(t *testing.T) {
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	decrypted := mustDecryptGoldenSample(t)
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
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	decrypted := mustDecryptGoldenSample(t)
	chat := extractChatMessage(decrypted, "cid", "cookie")
	// 修改文本模拟连续多条。
	chat1 := *chat
	chat1.Text = "第一条"
	chat2 := *chat
	chat2.Text = "第二条"
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
	cases := map[string]string{
		"fleamarket://message_chat?itemId=900052644277&peerUserId=3149637063": "900052644277",
		"noitemid":                     "",
		"fleamarket://x?itemId=ABC123": "ABC123",
	}
	for in, want := range cases {
		if got := extractItemID(in); got != want {
			t.Errorf("extractItemID(%q)=%q want %q", in, got, want)
		}
	}
}

// mustDecryptGoldenSample 复用 protocol 包的真实样本：直接硬编码一条最小解密结构，
// 避免循环依赖。这里构造一个等价的最小消息用于字段提取测试。
func mustDecryptGoldenSample(t *testing.T) map[string]any {
	t.Helper()
	// 真实样本关键字段（来自 protocol golden test 解密输出）：
	// message["1"]["2"]="47983389009@goofish", ["1"]["10"]["reminderContent"]="[我已拍下，待付款]",
	// ["1"]["10"]["reminderUrl"] 含 itemId=900052644277
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
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

func mustContentType14Notice(t *testing.T) map[string]any {
	t.Helper()
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
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

func mustRefundTradeCard(t *testing.T) map[string]any {
	t.Helper()
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
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

func mustPaidDeliveryCard(t *testing.T) map[string]any {
	t.Helper()
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
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// TestRetryDelay 复刻 _calculate_retry_delay 的分段逻辑。
func TestRetryDelay(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	acc.connFailures = 1
	expectDelayRange(t, acc.retryDelay("no close frame received or sent"), 3*time.Second)
	acc.connFailures = 10
	expectDelayRange(t, acc.retryDelay("no close frame received or sent"), 15*time.Second)
	acc.connFailures = 2
	expectDelayRange(t, acc.retryDelay("connection refused"), 20*time.Second)
	acc.connFailures = 10
	expectDelayRange(t, acc.retryDelay("connection refused"), 60*time.Second)
	acc.connFailures = 1
	expectDelayRange(t, acc.retryDelay("some other error"), 5*time.Second)
}

func expectDelayRange(t *testing.T, got, base time.Duration) {
	t.Helper()
	max := base + base/3
	if got < base || got > max {
		t.Fatalf("retryDelay=%v want in [%v,%v]", got, base, max)
	}
}

func TestRuntimeStatusClassifiesAuthenticationFailures(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()

	acc.setRuntimeError(context.Background(), fmt.Errorf("token API 登录凭证已失效: FAIL_SYS_TOKEN_EXOIRED"))
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
