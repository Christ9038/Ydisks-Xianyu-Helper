package adapter

import (
	"context"
	"testing"

	accountmanager "xianyu-go/internal/account"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/engine"
)

// runtimePortHandler 是账号运行时适配器测试使用的最小 Handler 替身。
type runtimePortHandler struct {
	// refreshResult 保存凭证恢复调用应返回的测试结果。
	refreshResult bool
}

// HandleChatMessage 满足账号运行时聊天事件接口，测试中不产生外部副作用。
func (runtimePortHandler) HandleChatMessage(context.Context, engine.ChatMessage) error { return nil }

// HandleSystemEvent 满足账号运行时系统事件接口，测试中不产生外部副作用。
func (runtimePortHandler) HandleSystemEvent(context.Context, automation.Task) error { return nil }

// OnPasswordLoginRefresh 返回测试预置的凭证恢复结果。
func (h runtimePortHandler) OnPasswordLoginRefresh(context.Context, string) bool {
	return h.refreshResult
}

// OnAccountAlert 满足账号运行时告警接口，测试中不发送通知。
func (runtimePortHandler) OnAccountAlert(context.Context, string, string, string, string) {}

// TestAccountRuntimePortForwardsStartedManagerOperations 验证运行时适配器透传在线查询、Cookie 同步、状态和凭证恢复。
func TestAccountRuntimePortForwardsStartedManagerOperations(t *testing.T) {
	// store、cleanup 保存账号管理器启动和 Cookie 同步使用的隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// manager 保存拥有一个测试账号运行实例的账号管理器。
	manager := accountmanager.NewManager(store, runtimePortHandler{refreshResult: true}, nil)
	// ctx、cancel 保存运行实例的生命周期上下文。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// startErr 保存测试账号运行实例登记结果。
	startErr := manager.Start(ctx, "cid", "unb=1; _m_h5_tk=tk;")
	if startErr != nil {
		t.Fatalf("启动测试账号失败: %v", startErr)
	}
	// port 保存绑定账号管理器的应用层运行时端口。
	portValue := NewAccountRuntimePort(manager)
	if portValue == nil {
		t.Fatal("已装配管理器应构造运行时端口")
	}
	// port 保存断言后的运行时端口值，便于直接验证各个应用端口方法。
	port := portValue.(AccountRuntimePort)
	// lookup 保存绑定管理器的账号在线查询函数。
	lookup := AccountRunningLookup(manager)
	if !lookup("cid") {
		t.Fatal("已启动账号应被识别为在线")
	}
	// updateErr 保存 Cookie 同步到运行实例和数据库的结果。
	updateErr := port.UpdateCookie(ctx, "cid", "unb=1; _m_h5_tk=updated;")
	if updateErr != nil {
		t.Fatalf("Cookie 同步失败: %v", updateErr)
	}
	// statuses、statusErr 保存运行实例状态快照转换结果。
	statuses, statusErr := port.RuntimeStatuses(ctx)
	if statusErr != nil || len(statuses) != 1 {
		t.Fatalf("运行状态转换异常 statuses=%+v err=%v", statuses, statusErr)
	}
	// recovered 保存 Manager 转发的凭证恢复结果。
	recovered := port.RecoverExpiredCredential(ctx, "cid")
	if !recovered {
		t.Fatal("凭证恢复结果未从 Handler 透传")
	}
	manager.Stop("cid")
	// stoppedStatuses、stoppedStatusErr 保存账号停止后的空状态快照。
	stoppedStatuses, stoppedStatusErr := port.RuntimeStatuses(ctx)
	if stoppedStatusErr != nil || len(stoppedStatuses) != 0 {
		t.Fatalf("停止后运行状态异常 statuses=%+v err=%v", stoppedStatuses, stoppedStatusErr)
	}
	if lookup("cid") {
		t.Fatal("停止后的账号不应被识别为在线")
	}
}
