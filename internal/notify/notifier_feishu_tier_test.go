package notify

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestFeishuAppNotificationTiers 验证分类开关的显式值、字段缺省与异常配置回退行为。
func TestFeishuAppNotificationTiers(t *testing.T) {
	// cases 保存渠道配置到期望开关值的确定性映射样例。
	cases := []struct {
		// name 是当前样例的说明。
		name string
		// config 是已解密的渠道配置 JSON。
		config string
		// wantWarning、wantInfo 是期望的警告类与普通提示类开关。
		wantWarning, wantInfo bool
	}{
		{name: "空配置", config: "", wantWarning: true, wantInfo: true},
		{name: "缺少字段", config: `{}`, wantWarning: true, wantInfo: true},
		{name: "非法 JSON", config: "not-json", wantWarning: true, wantInfo: true},
		{name: "字段类型错误", config: `{"notify_warning":"false"}`, wantWarning: true, wantInfo: true},
		{name: "字段为 null", config: `{"notify_warning":null,"notify_info":null}`, wantWarning: true, wantInfo: true},
		{name: "警告关闭", config: `{"notify_warning":false}`, wantWarning: false, wantInfo: true},
		{name: "提示关闭", config: `{"notify_info":false}`, wantWarning: true, wantInfo: false},
		{name: "全部关闭", config: `{"notify_warning":false,"notify_info":false}`, wantWarning: false, wantInfo: false},
		{name: "带密钥显式开启", config: `{"app_secret":"super-secret","notify_warning":true,"notify_info":true}`, wantWarning: true, wantInfo: true},
		{name: "带密钥缺省开启", config: `{"app_secret":"super-secret"}`, wantWarning: true, wantInfo: true},
	}
	// tc 是当前待验证的配置样例。
	for _, tc := range cases {
		// warning、info 保存被测函数解析出的分类开关。
		warning, info := feishuAppNotificationTiers(tc.config)
		if warning != tc.wantWarning || info != tc.wantInfo {
			t.Fatalf("%s: warning=%t info=%t want=(%t,%t)", tc.name, warning, info, tc.wantWarning, tc.wantInfo)
		}
	}
}

// TestIsWarningNotificationEvent 验证警告类事件由级别、失败结果和固定事件类型共同判定。
func TestIsWarningNotificationEvent(t *testing.T) {
	// 以下固定事件类型无论级别如何都必须归类为警告；warningType 是当前待验证的类型。
	for _, warningType := range []string{EventAccountOffline, EventAccountDisabled, EventSecurityVerification, EventManualInterventionRequired, EventSystemError, EventChatManualRequested} {
		if !isWarningNotificationEvent(NotificationEvent{Type: warningType}) {
			t.Fatalf("事件类型 %q 应归类为警告", warningType)
		}
	}
	// cases 保存分类边界样例及期望结论。
	cases := []struct {
		// name 是当前样例的说明。
		name string
		// event 是待分类的通知事件。
		event NotificationEvent
		// want 表示期望的警告类结论；false 代表普通提示类。
		want bool
	}{
		{name: "warn 级别", event: NotificationEvent{Level: "warn"}, want: true},
		{name: "critical 级别", event: NotificationEvent{Level: "critical"}, want: true},
		{name: "大写与空白级别", event: NotificationEvent{Level: " WARN "}, want: true},
		{name: "失败结果", event: NotificationEvent{Type: EventDeliveryResult, Outcome: "失败"}, want: true},
		{name: "成功结果", event: NotificationEvent{Type: EventDeliveryResult, Outcome: "成功"}, want: false},
		{name: "新消息", event: NotificationEvent{Type: EventChatMessageReceived, Level: "info"}, want: false},
		{name: "Token 续期", event: NotificationEvent{Type: EventTokenRenewal}, want: false},
		{name: "账号恢复", event: NotificationEvent{Type: EventAccountRecovered}, want: false},
		{name: "自动化成功", event: NotificationEvent{Type: EventAutomationOrderPaid, Level: "info", Outcome: "成功"}, want: false},
		{name: "手动发货成功", event: NotificationEvent{Type: EventManualDeliveryResult, Outcome: "成功"}, want: false},
	}
	// tc 是当前待验证的分类样例。
	for _, tc := range cases {
		// got 保存当前事件的警告类结论，用于与期望值比较。
		if got := isWarningNotificationEvent(tc.event); got != tc.want {
			t.Fatalf("%s: got=%t want=%t", tc.name, got, tc.want)
		}
	}
}

// TestFeishuAppAllowsNotification 验证只有 feishu_app 渠道读取分类开关，其他渠道类型始终放行。
func TestFeishuAppAllowsNotification(t *testing.T) {
	// warningEvent 是固定事件类型代表的警告类通知。
	warningEvent := NotificationEvent{Type: EventAccountOffline, Level: "warn"}
	// infoEvent 是新消息代表的普通提示类通知。
	infoEvent := NotificationEvent{Type: EventChatMessageReceived, Level: "info"}
	// cases 保存渠道配置与事件类别到放行结论的确定性映射。
	cases := []struct {
		// name 是当前样例的说明。
		name string
		// channel 是待判断的通知渠道。
		channel db.NotificationChannel
		// event 是当前通知事件。
		event NotificationEvent
		// want 表示期望是否放行。
		want bool
	}{
		{name: "非飞书渠道忽略开关", channel: db.NotificationChannel{Type: "webhook", Config: `{"notify_warning":false,"notify_info":false}`}, event: warningEvent, want: true},
		{name: "飞书警告关闭拒绝警告", channel: db.NotificationChannel{Type: "feishu_app", Config: `{"notify_warning":false}`}, event: warningEvent, want: false},
		{name: "飞书警告关闭放行提示", channel: db.NotificationChannel{Type: "feishu_app", Config: `{"notify_warning":false}`}, event: infoEvent, want: true},
		{name: "飞书提示关闭放行警告", channel: db.NotificationChannel{Type: "feishu_app", Config: `{"notify_info":false}`}, event: warningEvent, want: true},
		{name: "飞书提示关闭拒绝提示", channel: db.NotificationChannel{Type: "feishu_app", Config: `{"notify_info":false}`}, event: infoEvent, want: false},
		{name: "飞书全部关闭拒绝警告", channel: db.NotificationChannel{Type: "feishu_app", Config: `{"notify_warning":false,"notify_info":false}`}, event: warningEvent, want: false},
		{name: "飞书全部关闭拒绝提示", channel: db.NotificationChannel{Type: "feishu_app", Config: `{"notify_warning":false,"notify_info":false}`}, event: infoEvent, want: false},
		{name: "飞书缺省字段放行警告", channel: db.NotificationChannel{Type: "feishu_app", Config: `{}`}, event: warningEvent, want: true},
		{name: "飞书非法配置放行提示", channel: db.NotificationChannel{Type: "feishu_app", Config: "not-json"}, event: infoEvent, want: true},
	}
	// tc 是当前待验证的渠道与事件组合。
	for _, tc := range cases {
		// got 保存当前渠道对事件的放行结论，用于与期望值比较。
		if got := feishuAppAllowsNotification(tc.channel, tc.event); got != tc.want {
			t.Fatalf("%s: got=%t want=%t", tc.name, got, tc.want)
		}
	}
}

// TestEligibleChannelsAppliesFeishuAppTierFilter 验证渠道在订阅匹配后继续受飞书分类开关过滤，且其他渠道不受影响。
func TestEligibleChannelsAppliesFeishuAppTierFilter(t *testing.T) {
	// repository 保存一个警告关闭的飞书渠道、一个提示关闭的飞书渠道和一个开关全关的普通渠道。
	repository := &queueRepository{
		scriptedOutboxRepository: &scriptedOutboxRepository{},
		channels: []db.NotificationChannel{
			{ID: 1, Type: "feishu_app", Config: `{"notify_warning":false,"notify_info":true}`},
			{ID: 2, Type: "feishu_app", Config: `{"notify_warning":true,"notify_info":false}`},
			{ID: 3, Type: "webhook", Config: `{"notify_warning":false,"notify_info":false}`},
		},
	}
	// notifier 使用上述渠道替身。
	notifier := NewWithRepository("cid", repository, nilLogger())
	// ctx 是渠道过滤调用的本地上下文。
	ctx := context.Background()
	// warningChannels、warningErr 保存警告事件过滤后的渠道及错误。
	warningChannels, warningErr := notifier.eligibleChannels(ctx, NotificationEvent{AccountID: "cid", Type: EventAccountOffline, Level: "warn"})
	if warningErr != nil || len(warningChannels) != 2 || warningChannels[0].ID != 2 || warningChannels[1].ID != 3 {
		t.Fatalf("警告事件渠道=%+v err=%v", warningChannels, warningErr)
	}
	// infoChannels、infoErr 保存普通提示事件过滤后的渠道及错误。
	infoChannels, infoErr := notifier.eligibleChannels(ctx, NotificationEvent{AccountID: "cid", Type: EventChatMessageReceived, Level: "info"})
	if infoErr != nil || len(infoChannels) != 2 || infoChannels[0].ID != 1 || infoChannels[1].ID != 3 {
		t.Fatalf("提示事件渠道=%+v err=%v", infoChannels, infoErr)
	}
}

// TestFeishuAppTierFilterDoesNotLogChannelSecret 验证分类过滤日志不包含渠道配置中的 app_secret。
func TestFeishuAppTierFilterDoesNotLogChannelSecret(t *testing.T) {
	// logs 保存分类过滤期间产生的结构化日志文本。
	var logs bytes.Buffer
	// logger 输出包含 Debug 级别，用于捕获渠道被分类开关跳过的日志。
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	// repository 提供一个分类开关全部关闭且含敏感密钥的飞书渠道。
	repository := &queueRepository{
		scriptedOutboxRepository: &scriptedOutboxRepository{},
		channels: []db.NotificationChannel{
			{ID: 1, Type: "feishu_app", Config: `{"app_secret":"super-secret","notify_warning":false,"notify_info":false}`},
		},
	}
	// notifier 使用可观察日志的通知器。
	notifier := NewWithRepository("cid", repository, logger)
	// channels、err 保存过滤结果；该渠道应被分类开关跳过。
	channels, err := notifier.eligibleChannels(context.Background(), NotificationEvent{AccountID: "cid", Type: EventAccountOffline, Level: "warn"})
	if err != nil || len(channels) != 0 {
		t.Fatalf("分类开关全部关闭应跳过渠道: channels=%+v err=%v", channels, err)
	}
	if strings.Contains(logs.String(), "super-secret") {
		t.Fatalf("过滤日志泄漏 app_secret: %s", logs.String())
	}
}
