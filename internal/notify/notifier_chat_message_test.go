package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestNotifyChatMessageQueuesIdempotentNotification 验证新消息通知的类别、字段、深链接与平台消息幂等键。
func TestNotifyChatMessageQueuesIdempotentNotification(t *testing.T) {
	// repository 提供一个订阅全部事件的渠道并捕获 outbox 入队内容。
	repository := &queueRepository{
		scriptedOutboxRepository: &scriptedOutboxRepository{},
		channels:                 []db.NotificationChannel{{ID: 1, Type: "webhook"}},
	}
	// notifier 使用带尾部斜杠的对外地址，验证构造期归一化不会产生双斜杠。
	notifier := newNotifier("cid", repository, nilLogger(), Options{PublicBaseURL: " https://admin.example/console/ "})
	// longPreview 保存超过摘要上限的文本，用于同时验证字符级截断。
	longPreview := strings.Repeat("长", 200)
	notifier.NotifyChatMessage(context.Background(), "acc id", "店铺A", "chat&1", "买家", "buyer-1", "item-1", "text", longPreview, "m-1")
	if len(repository.enqueued) != 1 {
		t.Fatalf("新消息通知应入队一条记录，实际 %+v", repository.enqueued)
	}
	// record 保存唯一入队记录，用于断言事件类别、投递键和展示正文。
	record := repository.enqueued[0]
	if record.EventType != EventChatMessageReceived {
		t.Fatalf("event_type=%q want=%q", record.EventType, EventChatMessageReceived)
	}
	if record.IdempotencyKey != "chat-message:m-1" {
		t.Fatalf("idempotency_key=%q", record.IdempotencyKey)
	}
	// got 保存被测函数返回的新消息事件标签，用于与期望值比较。
	if got := eventLabel(EventChatMessageReceived); got != "新消息" {
		t.Fatalf("eventLabel=%q want=新消息", got)
	}
	// wantLink 是账号和会话都经过 URL 编码后的消息页地址。
	wantLink := "https://admin.example/console/app/chat?account=acc+id&chat=chat%261"
	if !strings.Contains(record.Body, "【新消息】") || !strings.Contains(record.Body, "收到买家消息") {
		t.Fatalf("通知正文缺少事件标题: %s", record.Body)
	}
	if !strings.Contains(record.Body, "账号名称: 店铺A") || !strings.Contains(record.Body, "会话用户: 买家 (ID: buyer-1)") {
		t.Fatalf("通知正文缺少账号或买家字段: %s", record.Body)
	}
	if !strings.Contains(record.Body, "消息类型: 文本") || !strings.Contains(record.Body, "消息摘要: "+strings.Repeat("长", 160)+"…") {
		t.Fatalf("通知正文缺少类型或截断摘要: %s", record.Body)
	}
	if !strings.Contains(record.Body, "查看链接: "+wantLink) {
		t.Fatalf("通知正文缺少查看链接 %s: %s", wantLink, record.Body)
	}
	if strings.Contains(record.Body, strings.Repeat("长", 161)) {
		t.Fatal("消息摘要应按字符数截断")
	}
	if !strings.Contains(record.Body, "有新消息，请打开管理后台查看。") {
		t.Fatalf("通知正文缺少固定提示: %s", record.Body)
	}
}

// TestNotifyChatMessageOmitsLinkWhenPublicBaseURLMissing 验证未配置对外地址时通知不生成查看链接。
func TestNotifyChatMessageOmitsLinkWhenPublicBaseURLMissing(t *testing.T) {
	// repository 提供一个订阅全部事件的渠道并捕获 outbox 入队内容。
	repository := &queueRepository{
		scriptedOutboxRepository: &scriptedOutboxRepository{},
		channels:                 []db.NotificationChannel{{ID: 1, Type: "webhook"}},
	}
	// notifier 不配置对外地址，链接字段必须整体省略。
	notifier := newNotifier("cid", repository, nilLogger(), Options{})
	notifier.NotifyChatMessage(context.Background(), "cid", "", "chat-1", "", "", "", "image", "https://img.example/a.jpg", "m-2")
	if len(repository.enqueued) != 1 {
		t.Fatalf("新消息通知应入队一条记录，实际 %+v", repository.enqueued)
	}
	// body 保存格式化后的渠道正文，用于断言回退值和省略行为。
	body := repository.enqueued[0].Body
	if strings.Contains(body, "查看链接") || strings.Contains(body, "/app/chat") {
		t.Fatalf("未配置对外地址不应生成查看链接: %s", body)
	}
	if !strings.Contains(body, "账号名称: cid") {
		t.Fatalf("账号名称为空时应回退账号 ID: %s", body)
	}
	if !strings.Contains(body, "会话用户: 未知") || !strings.Contains(body, "消息摘要: [图片]") {
		t.Fatalf("缺少买家或图片摘要回退值: %s", body)
	}
	if !strings.Contains(body, "消息类型: 图片") {
		t.Fatalf("图片消息类型标签错误: %s", body)
	}
}

// TestNotifyChatMessageLeavesIdempotencyKeyEmptyWithoutMessageKey 验证缺失平台消息键时保持普通通知语义。
func TestNotifyChatMessageLeavesIdempotencyKeyEmptyWithoutMessageKey(t *testing.T) {
	// repository 提供一个订阅全部事件的渠道并捕获 outbox 入队内容。
	repository := &queueRepository{
		scriptedOutboxRepository: &scriptedOutboxRepository{},
		channels:                 []db.NotificationChannel{{ID: 1, Type: "webhook"}},
	}
	// notifier 已启动，确保空幂等键也进入持久化 outbox 而不是同步发送。
	notifier := newNotifier("cid", repository, nilLogger(), Options{})
	notifier.started.Store(true)
	notifier.NotifyChatMessage(context.Background(), "cid", "店铺", "chat", "买家", "buyer", "item", "text", "你好", "  ")
	if len(repository.enqueued) != 1 || repository.enqueued[0].IdempotencyKey != "" {
		t.Fatalf("空闲消息键应保持空投递键: %+v", repository.enqueued)
	}
}

// TestNotifyChatMessageNilNotifierIsSafe 验证空通知器接收者不会因新消息通知而 panic。
func TestNotifyChatMessageNilNotifierIsSafe(t *testing.T) {
	// nilNotifier 是未初始化的通知器指针，调用应安全返回。
	var nilNotifier *Notifier
	nilNotifier.NotifyChatMessage(context.Background(), "cid", "店铺", "chat", "买家", "buyer", "item", "text", "你好", "m")
}

// TestChatMessagePreviewNormalizesMediaAndTruncatesText 验证媒体消息只展示类型说明且文本按字符安全截断。
func TestChatMessagePreviewNormalizesMediaAndTruncatesText(t *testing.T) {
	// cases 保存消息类型到期望摘要的确定性映射样例。
	cases := []struct {
		// messageType、preview 是输入的消息类型与原始内容。
		messageType, preview string
		// want 是期望的展示摘要。
		want string
	}{
		{messageType: "image", preview: "https://img.example/a.jpg", want: "[图片]"},
		{messageType: "item", preview: `{"itemId":"1"}`, want: "[商品卡片]"},
		{messageType: "video", preview: "https://video.example/a.mp4", want: "[视频]"},
		{messageType: "audio", preview: "https://audio.example/a.amr", want: "[语音]"},
		{messageType: "text", preview: "  你好  ", want: "你好"},
		{messageType: "", preview: "  ", want: "[新消息]"},
	}
	// tc 表示当前待验证的摘要样例。
	for _, tc := range cases {
		// got 保存被测函数生成的摘要。
		if got := chatMessagePreview(tc.messageType, tc.preview); got != tc.want {
			t.Fatalf("messageType=%q preview=%q got=%q want=%q", tc.messageType, tc.preview, got, tc.want)
		}
	}
	// longText 保存混合中文与表情的超长文本，用于验证按字符而非字节截断。
	longText := strings.Repeat("闲鱼", 100)
	// got 保存超长文本的截断摘要。
	got := chatMessagePreview("text", longText)
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != 161 {
		t.Fatalf("截断结果 rune 长度=%d got=%q", len([]rune(got)), got)
	}
}

// TestNewWithOptionsChatMessageNotificationOmitsCredentials 验证构造期配置的链接生效且渠道正文不包含账号凭证。
func TestNewWithOptionsChatMessageNotificationOmitsCredentials(t *testing.T) {
	// store、cleanup 保存带有 Cookie 明文的隔离通知数据库及关闭责任。
	store, cleanup := newNotifyStoreBare(t)
	defer cleanup()
	// received 保存本地 webhook 收到的通知正文，空值表示未触发同步发送。
	var received string
	// server 是接收同步通知的本地 webhook 目标。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// body 保存渠道请求正文；读取失败时保留空串供断言发现。
		body, _ := io.ReadAll(r.Body)
		received = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	addWebhookChannel(t, store, "cid", "消息渠道", server.URL)
	// notifier 通过构造期配置获得对外地址，链接应直接出现在同步渠道正文中。
	notifier := NewWithOptions("cid", store, nilLogger(), Options{PublicBaseURL: "https://admin.example/"})
	notifier.NotifyChatMessage(context.Background(), "cid", "店铺", "chat-1", "买家", "buyer", "item-1", "text", "你好", "")
	if received == "" {
		t.Fatal("未启动 outbox worker 时应同步发送通知")
	}
	// payload 保存 webhook JSON 载荷，正文位于 message 字段且其中的 & 会被 JSON 转义。
	var payload struct {
		// Message 是 webhook 渠道发送的格式化通知正文。
		Message string `json:"message"`
	}
	// decodeErr 保存 webhook JSON 载荷解析失败的原因。
	if decodeErr := json.Unmarshal([]byte(received), &payload); decodeErr != nil {
		t.Fatalf("解析 webhook 载荷: %v", decodeErr)
	}
	if !strings.Contains(payload.Message, "https://admin.example/app/chat?account=cid&chat=chat-1") {
		t.Fatalf("同步通知缺少构造期配置的查看链接: %s", payload.Message)
	}
	if strings.Contains(payload.Message, "tk_1") || strings.Contains(payload.Message, "unb=123") {
		t.Fatalf("通知正文不得包含账号 Cookie: %s", payload.Message)
	}
}
