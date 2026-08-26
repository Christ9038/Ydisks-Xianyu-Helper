package runtime

import (
	"context"
	"testing"
	"time"

	composition "xianyu-go/internal/composition"
)

// TestBuildRuntimeRejectsIncompleteInfrastructure 覆盖运行时组合根拒绝空基础设施的启动保护。
func TestBuildRuntimeRejectsIncompleteInfrastructure(t *testing.T) {
	// runtimeValue、buildErr 保存缺少数据库和日志依赖时的构造结果。
	runtimeValue, buildErr := BuildRuntime(RuntimeOptions{}, RuntimeInfrastructure{})
	if buildErr == nil || runtimeValue.HTTPServer != nil || runtimeValue.Lifecycle != nil {
		t.Fatalf("incomplete runtime value=%+v err=%v", runtimeValue, buildErr)
	}
}

// TestServerDependenciesRejectsNilComposition 覆盖 HTTP transport 投影拒绝空组合服务的保护。
func TestServerDependenciesRejectsNilComposition(t *testing.T) {
	// dependencies、dependencyErr 保存空组合服务的投影结果和错误。
	dependencies, dependencyErr := ServerDependencies(nil, HTTPDependencies{}, nil)
	if dependencyErr == nil || dependencies.Applications != nil {
		t.Fatalf("nil composition dependencies=%+v err=%v", dependencies, dependencyErr)
	}
}

// runtimeAccountLoginFake 是组合运行时账号登录 transport 的内存替身。
type runtimeAccountLoginFake struct{}

// CreateCookie 模拟创建 Cookie 登录。
func (runtimeAccountLoginFake) CreateCookie(context.Context, string, string, int64, string) error {
	return nil
}

// UpdateCookie 模拟更新 Cookie 登录。
func (runtimeAccountLoginFake) UpdateCookie(context.Context, string, string, int64, string, int64) error {
	return nil
}

// PersistQRLoginSuccess 模拟扫码登录成功持久化。
func (runtimeAccountLoginFake) PersistQRLoginSuccess(context.Context, int64, string, map[string]any, string) (composition.CookieLoginResult, error) {
	return composition.CookieLoginResult{AccountID: "account"}, nil
}

// RegisterQRSession 模拟登记扫码会话。
func (runtimeAccountLoginFake) RegisterQRSession(string, int64, time.Time) {}

// AuthorizeQRSession 模拟校验扫码会话归属。
func (runtimeAccountLoginFake) AuthorizeQRSession(string, int64) error { return nil }

// CleanupQRSessions 模拟清理扫码会话。
func (runtimeAccountLoginFake) CleanupQRSessions(time.Time) []string { return []string{"session"} }

// runtimeQRLoginFake 是组合运行时二维码 transport 的内存替身。
type runtimeQRLoginFake struct{}

// GenerateQRCode 模拟生成二维码会话。
func (runtimeQRLoginFake) GenerateQRCode(context.Context) (string, string, error) {
	return "session", "data:image/png;base64,test", nil
}

// GetSessionStatus 模拟读取二维码会话状态。
func (runtimeQRLoginFake) GetSessionStatus(string) map[string]any {
	return map[string]any{"status": "waiting"}
}

// CompleteVerification 模拟完成二维码风控验证。
func (runtimeQRLoginFake) CompleteVerification(context.Context, string) (string, string, error) {
	return "cookies", "account", nil
}

// TestRuntimeTransportAdaptersForwardCalls 覆盖组合运行时对账号登录、二维码和会话恢复端口的转发。
func TestRuntimeTransportAdaptersForwardCalls(t *testing.T) {
	// accountTransport 保存账号登录转发适配器。
	accountTransport := accountLoginTransport{service: runtimeAccountLoginFake{}}
	// createErr 保存创建 Cookie 转发错误。
	if createErr := accountTransport.CreateCookie(context.Background(), "account", "cookies", 1, "manual"); createErr != nil {
		t.Fatal(createErr)
	}
	// updateErr 保存更新 Cookie 转发错误。
	if updateErr := accountTransport.UpdateCookie(context.Background(), "account", "cookies", 1, "manual", 2); updateErr != nil {
		t.Fatal(updateErr)
	}
	// result、persistErr 保存扫码成功转发结果。
	result, persistErr := accountTransport.PersistQRLoginSuccess(context.Background(), 1, "session", nil, "account")
	if persistErr != nil || result.AccountID != "account" {
		t.Fatalf("persist result=%+v err=%v", result, persistErr)
	}
	accountTransport.RegisterQRSession("session", 1, time.Now())
	// authorizeErr 保存二维码会话归属转发错误。
	if authorizeErr := accountTransport.AuthorizeQRSession("session", 1); authorizeErr != nil {
		t.Fatal(authorizeErr)
	}
	// sessions 保存二维码会话清理转发结果。
	if sessions := accountTransport.CleanupQRSessions(time.Now()); len(sessions) != 1 {
		t.Fatalf("cleanup sessions=%v", sessions)
	}
	// qrTransport 保存二维码转发适配器。
	qrTransport := qrLoginTransport{service: runtimeQRLoginFake{}}
	// sessionID、qrURL、generateErr 保存二维码生成转发结果。
	sessionID, qrURL, generateErr := qrTransport.GenerateQRCode(context.Background())
	if generateErr != nil || sessionID == "" || qrURL == "" {
		t.Fatalf("generate session=%q url=%q err=%v", sessionID, qrURL, generateErr)
	}
	// status 保存二维码会话状态转发结果。
	if status := qrTransport.GetSessionStatus(sessionID); status["status"] != "waiting" {
		t.Fatalf("session status=%v", status)
	}
	// cookies、accountID、completeErr 保存二维码验证转发结果。
	cookies, accountID, completeErr := qrTransport.CompleteVerification(context.Background(), sessionID)
	if completeErr != nil || cookies == "" || accountID == "" {
		t.Fatalf("complete cookies=%q account=%q err=%v", cookies, accountID, completeErr)
	}
	qrTransport.DeleteSession(sessionID)
	// recoveryTransport 保存会话恢复回调适配器。
	recoveryTransport := sessionRecoveryTransport{handler: func(context.Context, string, error) bool { return true }}
	if !recoveryTransport.Recover(context.Background(), "account", context.Canceled) {
		t.Fatal("session recovery should forward to handler")
	}
}

// TestQRLoginTransportDeleteWithoutOptionalCleaner 覆盖二维码服务不提供删除扩展能力时的安全转发。
func TestQRLoginTransportDeleteWithoutOptionalCleaner(t *testing.T) {
	// transport 保存不实现 DeleteSession 扩展接口的二维码服务。
	transport := qrLoginTransport{service: runtimeQRLoginFake{}}
	transport.DeleteSession("session")
}
