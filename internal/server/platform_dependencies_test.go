package server

import (
	"context"
	"testing"

	"xianyu-go/internal/adapter"
	"xianyu-go/internal/xianyu/renew"
)

// serverPlatformQRFake 是 Server 平台访问器测试使用的二维码服务替身。
type serverPlatformQRFake struct{}

// GenerateQRCode 返回固定二维码数据，验证 Server 不参与平台协议编排。
func (serverPlatformQRFake) GenerateQRCode(context.Context) (string, string, error) {
	return "session", "https://example.test/qr", nil
}

// GetSessionStatus 返回固定轮询状态，验证二维码能力可由显式依赖提供。
func (serverPlatformQRFake) GetSessionStatus(string) map[string]any {
	return map[string]any{"status": "waiting"}
}

// CompleteVerification 返回脱敏验证结果，测试不输出真实凭证。
func (serverPlatformQRFake) CompleteVerification(context.Context, string) (string, string, error) {
	return "masked-cookie", "account", nil
}

// TestServerPlatformAccessorsUseInjectedDependencies 验证生产访问器读取构造阶段的平台依赖。
func TestServerPlatformAccessorsUseInjectedDependencies(t *testing.T) {
	// mtopClient、longLoginClient 和 qrService 保存本测试注入的客户端能力。
	mtopClient := adapter.NewMTOPClient()
	// longLoginClient 保存注入的长登录协议实现。
	longLoginClient := renew.Service{}
	// qrService 保存注入的二维码协议实现。
	qrService := serverPlatformQRFake{}
	// platformDependencies 保存通过 adapter 边界校验的平台能力集合。
	platformDependencies, err := adapter.NewPlatformDependencies(mtopClient, longLoginClient, qrService)
	if err != nil {
		t.Fatalf("NewPlatformDependencies: %v", err)
	}
	// server 保存仅含平台能力的最小 HTTP 服务测试对象。
	server := &Server{platformDependencies: platformDependencies}
	if server.mtopClient() != mtopClient {
		t.Fatalf("mtopClient 未返回显式注入客户端")
	}
	if server.longLoginClient() != longLoginClient {
		t.Fatalf("longLoginClient 未返回显式注入客户端")
	}
	if server.qrLoginService() != qrService {
		t.Fatalf("qrLoginService 未返回显式注入服务")
	}
}

// TestServerPlatformAccessorsKeepLegacyTestAliases 验证兼容字段仍能替换平台能力以支撑现有测试。
func TestServerPlatformAccessorsKeepLegacyTestAliases(t *testing.T) {
	// platformDependencies 保存生产默认平台能力，legacyMTop 和 legacyQR 保存旧测试替身。
	platformDependencies, err := adapter.NewDefaultPlatformDependencies(nil)
	if err != nil {
		t.Fatalf("NewDefaultPlatformDependencies: %v", err)
	}
	// legacyMTop 保存仅用于兼容测试注入的 MTOP 客户端。
	legacyMTop := adapter.NewMTOPClient()
	// legacyQR 保存仅用于兼容测试注入的二维码服务。
	legacyQR := serverPlatformQRFake{}
	// legacyLongLogin 保存仅用于兼容测试注入的长登录客户端。
	legacyLongLogin := renew.Service{}
	// server 保存同时包含新边界和旧测试字段的过渡期对象。
	server := &Server{
		platformDependencies: platformDependencies,
		MTop:                 legacyMTop,
		CookieRenew:          legacyLongLogin,
		QRLogin:              legacyQR,
	}
	if server.mtopClient() != legacyMTop || server.longLoginClient() != legacyLongLogin || server.qrLoginService() != legacyQR {
		t.Fatalf("旧测试兼容字段未优先覆盖平台访问器")
	}
}
