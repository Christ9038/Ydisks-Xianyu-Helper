package engine

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// observeLockedMatched 在测试中持锁调用 observeLocked 并只返回是否消费了等待器，诊断事件由专门用例断言。
func observeLockedMatched(tracker *outgoingEchoTracker, message OutgoingChatMessage) bool {
	// matched 是被测调用是否确认了等待器。
	matched, _ := tracker.observeLocked(message)
	return matched
}

// TestOutgoingEchoIgnoredPushIsRecorded 验证正文匹配的无 mid 推送不会确认带 mid 的等待器，但会记录到达时刻供超时日志使用。
func TestOutgoingEchoIgnoredPushIsRecorded(t *testing.T) {
	// paths 分别覆盖带平台消息 ID 的推送与不带消息 ID 的推送两条入口。
	paths := map[string]OutgoingChatMessage{
		"with-pnm":    {ChatID: "chat", BuyerID: "buyer", MessageType: "text", Text: "hello", MessageKey: "pnm-push"},
		"without-pnm": {ChatID: "chat", BuyerID: "buyer", MessageType: "text", Text: "hello"},
	}
	// name、push 是当前入口名称与推送回显。
	for name, push := range paths {
		// tracker 是被测账号级回显跟踪器。
		tracker := newOutgoingEchoTracker()
		// waiter 是绑定请求 mid 的自动化发送等待项。
		waiter := tracker.register("chat", "buyer", "text", "hello", "mid-1")
		// seen 表示推送到达前是否已存在被忽略推送记录，此时必须为 false。
		if _, seen := waiter.ignoredPushDelay(); seen {
			t.Fatalf("%s: 推送到达前不应有记录", name)
		}
		tracker.observeMessage(push)
		select {
		case <-waiter.done:
			t.Fatalf("%s: 无 mid 推送不得确认带 mid 等待器", name)
		default:
		}
		// delay、seen 是记录的推送到达延迟及是否出现。
		delay, seen := waiter.ignoredPushDelay()
		if !seen || delay < 0 {
			t.Fatalf("%s: 应记录被忽略的推送 delay=%v seen=%v", name, delay, seen)
		}
		waiter.cancel()
	}
	// nilWaiter 覆盖空等待项的防御分支。
	var nilWaiter *outgoingEchoWaiter
	// seen 表示空等待项是否错误地报告了推送记录。
	if _, seen := nilWaiter.ignoredPushDelay(); seen {
		t.Fatal("空等待项不应报告推送")
	}
}

// TestOutgoingEchoLatePushAfterCancelIsLogged 验证带 mid 发送超时取消后，迟到的无 mid 推送输出诊断日志、不确认后续同文发送且日志不含正文。
func TestOutgoingEchoLatePushAfterCancelIsLogged(t *testing.T) {
	// buffer 收集诊断日志输出。
	var buffer bytes.Buffer
	// tracker 是配置了诊断日志器的跟踪器。
	tracker := newOutgoingEchoTracker()
	tracker.logger = slog.New(slog.NewTextHandler(&buffer, nil))
	// timedOut 是已经超时取消的带 mid 发送。
	timedOut := tracker.register("chat", "buyer", "text", "秘密卡密", "mid-old")
	timedOut.cancel()
	tracker.observeMessage(OutgoingChatMessage{ChatID: "chat", BuyerID: "buyer", MessageType: "text", Text: "秘密卡密", MessageKey: "pnm-late"})
	// output 是迟到推送产生的日志。
	output := buffer.String()
	if !strings.Contains(output, "request_mid=mid-old") || !strings.Contains(output, "since_cancel_ms=") {
		t.Fatalf("应输出迟到推送诊断 output=%s", output)
	}
	if strings.Contains(output, "秘密卡密") {
		t.Fatal("诊断日志不得包含消息正文")
	}
	buffer.Reset()
	// fresh 是之后登记的同文新发送；迟到推送诊断不得改变其确认语义。
	fresh := tracker.register("chat", "buyer", "text", "秘密卡密", "mid-new")
	tracker.observeMessage(OutgoingChatMessage{ChatID: "chat", BuyerID: "buyer", MessageType: "text", Text: "秘密卡密"})
	select {
	case <-fresh.done:
		t.Fatal("无 mid 推送不得确认新的带 mid 发送")
	default:
	}
	fresh.cancel()
}

// TestOutgoingEchoDiagnosticSkippedWithoutLogger 验证未配置日志器或没有迟到事件时诊断输出安全跳过。
func TestOutgoingEchoDiagnosticSkippedWithoutLogger(t *testing.T) {
	// tracker 是未配置日志器的跟踪器。
	tracker := newOutgoingEchoTracker()
	tracker.logDiagnostic(outgoingEchoDiagnostic{latePush: true, requestID: "mid", sinceCancel: time.Second})
	// buffer 收集日志，用于确认非迟到事件不输出。
	var buffer bytes.Buffer
	tracker.logger = slog.New(slog.NewTextHandler(&buffer, nil))
	tracker.logDiagnostic(outgoingEchoDiagnostic{})
	if buffer.Len() != 0 {
		t.Fatalf("非迟到事件不应输出 output=%s", buffer.String())
	}
	// nilTracker 覆盖空跟踪器的防御分支。
	var nilTracker *outgoingEchoTracker
	nilTracker.logDiagnostic(outgoingEchoDiagnostic{latePush: true})
}

// TestConfirmOutgoingEchoTimeoutLogsPushSignal 验证确认超时日志携带请求 mid 与被忽略推送信号。
func TestConfirmOutgoingEchoTimeoutLogsPushSignal(t *testing.T) {
	// buffer 收集账号日志输出。
	var buffer bytes.Buffer
	// tracker 是被测跟踪器。
	tracker := newOutgoingEchoTracker()
	// coordinator 是只配置日志与短确认窗口的出站协调器。
	coordinator := &outgoingMessageCoordinator{account: &Account{accountDependencies: accountDependencies{logger: slog.New(slog.NewTextHandler(&buffer, nil))}}, echoTracker: tracker, echoWaitTimeout: 20 * time.Millisecond}
	// waiter 是绑定 mid 的等待项，先收到一条会被忽略的推送。
	waiter := tracker.register("chat", "buyer", "text", "hello", "mid-timeout")
	tracker.observeMessage(OutgoingChatMessage{ChatID: "chat", BuyerID: "buyer", MessageType: "text", Text: "hello"})
	// err 是确认等待的结果，超时必须返回未确认错误。
	if err := coordinator.confirmOutgoingEcho(t.Context(), waiter, "chat"); err == nil {
		t.Fatal("超时应返回未确认错误")
	}
	// output 是超时日志文本。
	output := buffer.String()
	if !strings.Contains(output, "request_mid=mid-timeout") || !strings.Contains(output, "push_echo_seen=true") {
		t.Fatalf("超时日志缺少诊断字段 output=%s", output)
	}
}
