package engine

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// retryNotificationHandler 复用账号回调替身并同步收集事件；测试只在连接处理返回后读取事件，不并发访问。
type retryNotificationHandler struct {
	// recordingHandler 记录账号级续期次数，用于确认网络错误没有要求重新授权。
	recordingHandler
	// events 保存同一离线周期内的事件类别、标题和正文，便于核对通知语义及去重。
	events []struct {
		// kind 是订阅通知使用的事件类别。
		kind string
		// title 是用户看到的通知标题。
		title string
		// body 是用户看到的通知正文。
		body string
	}
}

// OnAccountEvent 将 kind、title、body 复制到测试事件列表；忽略没有参与断言的上下文、账号和级别。
func (h *retryNotificationHandler) OnAccountEvent(_ context.Context, _, kind, _, title, body string) {
	h.events = append(h.events, struct {
		// kind 是订阅通知使用的事件类别。
		kind string
		// title 是用户看到的通知标题。
		title string
		// body 是用户看到的通知正文。
		body string
	}{kind: kind, title: title, body: body})
}

// TestTokenNetworkFailureNotifiesRetryAndRecovery 验证请求超时或网络失败只提示自动重试、保留可取消退避，并按离线周期通知。
func TestTokenNetworkFailureNotifiesRetryAndRecovery(t *testing.T) {
	// cases 覆盖标准超时、具备 net.Error 语义的连接超时及生产日志中的常见文本包装。
	cases := map[string]error{
		"deadline":         context.DeadlineExceeded,
		"net-timeout":      &net.OpError{Op: "dial", Net: "tcp", Err: context.DeadlineExceeded},
		"tls-timeout":      errors.New("net/http: TLS handshake timeout"),
		"waiting-headers":  errors.New("token API 请求失败: net/http: request canceled while waiting for connection (Client.Timeout exceeded while awaiting headers)"),
		"connection-reset": errors.New("connection reset by peer"),
	}
	// name 标识当前网络故障；tokenErr 是消息凭证请求返回的错误，不代表账号 Session 已过期。
	for name, tokenErr := range cases {
		t.Run(name, func(t *testing.T) {
			// handler 保存本子测试的离线、恢复通知与续期调用记录。
			handler := &retryNotificationHandler{}
			// account 只使用内存状态，不访问真实账号、数据库或外部网络。
			account := New(Config{CookieID: name, Handler: handler})
			// coordinator 使用生产错误处理路径，保留真实的可取消退避。
			coordinator := &connectionCoordinator{account: account}
			// attempt 覆盖同一离线周期内的重复失败，通知应只发送一次。
			for attempt := 0; attempt < 2; attempt++ {
				// ctx、cancel 将生产退避压缩为短取消窗口；超时来自等待预算而非认证失败。
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
				// conn 是本次获取 Token 失败后应立即关闭的未注册连接。
				conn := &fakeWSConn{}
				// retry、err 是等待被取消后的返回结果，不能返回原始故障来永久停止账号。
				retry, err := coordinator.handleTokenAcquisitionFailure(ctx, conn, tokenErr)
				cancel()
				if retry || !errors.Is(err, context.DeadlineExceeded) || !conn.closed {
					t.Fatalf("应关闭连接并等待可取消退避: retry=%v err=%v closed=%v", retry, err, conn.closed)
				}
				if account.RuntimeStatus().State != RuntimeReconnecting || handler.refresh != 0 {
					t.Fatalf("网络失败不得判为凭证失效或发起续期: status=%+v refresh=%d", account.RuntimeStatus(), handler.refresh)
				}
			}
			if len(handler.events) != 1 {
				t.Fatalf("同一离线周期应只通知一次: %+v", handler.events)
			}
			// offline 是唯一离线通知，既要保留原始诊断，又不能要求重新扫码或声称不可自动重连。
			offline := handler.events[0]
			if offline.kind != EventAccountOffline || !strings.Contains(offline.title, "自动重试") || !strings.Contains(offline.body, tokenErr.Error()) || !strings.Contains(offline.body, "持续退避重试") {
				t.Fatalf("自动重试通知不完整: %+v", offline)
			}
			// forbidden 是网络故障通知中不允许出现的认证失效或错误次数提示。
			for _, forbidden := range []string{"重新登录", "重新扫码", "不可自动重连", "连续多次"} {
				if strings.Contains(offline.title+offline.body, forbidden) {
					t.Fatalf("网络失败通知含误导提示 %q: %+v", forbidden, offline)
				}
			}
			coordinator.markConnectionOnline(context.Background(), &fakeWSConn{})
			if account.RuntimeStatus().State != RuntimeOnline || len(handler.events) != 2 || handler.events[1].kind != EventAccountRecovered {
				t.Fatalf("重新上线后应发送恢复通知: status=%+v events=%+v", account.RuntimeStatus(), handler.events)
			}
		})
	}
}
