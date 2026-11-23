package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	chatapp "xianyu-go/internal/application/chat"
)

// captureDefaultLogger 把默认 slog 记录器替换为文本缓冲，返回缓冲与恢复函数；测试结束时自动恢复。
func captureDefaultLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	// buffer 保存测试期间写入默认记录器的全部日志文本。
	buffer := &bytes.Buffer{}
	// previous 是替换前的默认记录器，测试结束后恢复。
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buffer, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return buffer
}

// TestLogChatFailureRecordsReasonAndLevel 验证平台拒绝原因被记录、等级符合规范，且凭证被脱敏。
func TestLogChatFailureRecordsReasonAndLevel(t *testing.T) {
	// platformReason 模拟平台内容审核拦截后包进错误链的原因文本。
	platformReason := "闲鱼聊天发送结果: kind=rejected code=400 reason=reason=您的内容存在不当信息"
	// cases 覆盖各类错误对应的等级与是否带会话字段。
	cases := []struct {
		name      string
		operation string
		chatID    string
		err       error
		wantLevel string
	}{
		{"platform rejected", "text", "chat1", fmt.Errorf("%w: %v", chatapp.ErrSend, errors.New(platformReason)), "level=WARN"},
		{"uncertain", "image", "chat1", fmt.Errorf("%w: x", chatapp.ErrSendUncertain), "level=WARN"},
		{"offline", "library_image", "chat1", chatapp.ErrOffline, "level=WARN"},
		{"status save", "text", "chat1", chatapp.ErrStatusSave, "level=ERROR"},
		{"item create", "item_card_send", "", chatapp.ErrChatItemCreate, "level=ERROR"},
		{"unmapped send error", "text", "chat1", errors.New("数据库写入失败"), "level=ERROR"},
		{"unmapped list error", "item_card_list", "", errors.New("平台暂时不可用"), "level=WARN"},
	}
	// testCase 是当前错误类型场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// buffer 是本场景的日志输出。
			buffer := captureDefaultLogger(t)
			logChatFailure(context.Background(), testCase.operation, "acc1", testCase.chatID, testCase.err)
			// output 是写入的日志文本。
			output := buffer.String()
			if !strings.Contains(output, testCase.wantLevel) || !strings.Contains(output, "聊天请求失败") || !strings.Contains(output, "operation="+testCase.operation) || !strings.Contains(output, "account=acc1") {
				t.Fatalf("日志缺少必要字段: %s", output)
			}
			if !strings.Contains(output, "chat_failure_log_test.go") {
				t.Fatalf("caller 应指向调用方而不是封装函数: %s", output)
			}
			if testCase.chatID == "" && strings.Contains(output, "chat_id=") {
				t.Fatalf("会话为空时不应写 chat_id: %s", output)
			}
			if testCase.chatID != "" && !strings.Contains(output, "chat_id="+testCase.chatID) {
				t.Fatalf("缺少 chat_id: %s", output)
			}
		})
	}
	// buffer 用于验证原因文本与脱敏。
	buffer := captureDefaultLogger(t)
	logChatFailure(context.Background(), "text", "acc1", "chat1", fmt.Errorf("%w: %v", chatapp.ErrSend, errors.New(platformReason+" https://x.test/a?token=SECRETVALUE")))
	// redacted 是脱敏后写入的日志文本。
	if redacted := buffer.String(); !strings.Contains(redacted, "您的内容存在不当信息") || strings.Contains(redacted, "SECRETVALUE") {
		t.Fatalf("原因应被记录且凭证被移除: %s", redacted)
	}
}

// TestLogChatFailureIgnoresNilError 验证没有错误时不产生日志。
func TestLogChatFailureIgnoresNilError(t *testing.T) {
	// buffer 保存应保持为空的日志输出。
	buffer := captureDefaultLogger(t)
	logChatFailure(context.Background(), "text", "acc1", "chat1", nil)
	if buffer.Len() != 0 {
		t.Fatalf("nil 错误不应记录日志: %s", buffer.String())
	}
}

// TestChatSendHandlerLogsPlatformReason 验证文字发送被平台拒绝时，接口仍返回固定文案，而原因进入日志。
func TestChatSendHandlerLogsPlatformReason(t *testing.T) {
	// srv、cleanup 是启用聊天应用的测试服务及资源释放函数。
	srv, _, cleanup := newTestServerWithChat(t)
	defer cleanup()
	// port 是返回平台拒绝错误的文字发送端口。
	port := &chatHandlerCoveragePort{sendingAvailable: true, imageUploadAvailable: true, ownsAccountResult: true,
		sendTextErr:    fmt.Errorf("%w: 闲鱼聊天发送结果: kind=rejected code=400 reason=reason=内容存在不当信息", chatapp.ErrSend),
		sendTextResult: &chatapp.Message{AccountID: "acc1", ChatID: "chat1", Status: "failed"}}
	srv.applications.chat = port
	// handler 是注入可控端口后的真实路由。
	handler := srv.Router()
	// cookie 是通过真实登录取得的管理员会话。
	cookie := loginHelper(t, handler)
	// buffer 在登录之后再接管日志，只捕获发送请求。
	buffer := captureDefaultLogger(t)
	// recorder 保存发送失败响应。
	recorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/chat/messages", `{"account_id":"acc1","chat_id":"chat1","peer_user_id":"buyer1","text":"你好"}`)
	if recorder.Code != http.StatusBadGateway || strings.Contains(recorder.Body.String(), "不当信息") {
		t.Fatalf("响应应保持固定文案: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	// logged 是发送失败时写入的日志文本。
	if logged := buffer.String(); !strings.Contains(logged, "内容存在不当信息") || !strings.Contains(logged, "operation=text") || strings.Contains(logged, "你好") {
		t.Fatalf("日志应含平台原因且不含消息正文: %s", logged)
	}
}
