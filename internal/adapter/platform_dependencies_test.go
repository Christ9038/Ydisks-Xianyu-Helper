package adapter

import (
	"context"
	"strings"
	"testing"

	"xianyu-go/internal/xianyu/renew"
)

// platformDependenciesQRFake 是平台依赖测试使用的二维码服务替身。
type platformDependenciesQRFake struct{}

// GenerateQRCode 返回固定会话数据，验证依赖边界不会改写二维码协议。
func (platformDependenciesQRFake) GenerateQRCode(context.Context) (string, string, error) {
	return "session", "https://example.test/qr", nil
}

// GetSessionStatus 返回最小状态对象，验证二维码服务可以通过边界读取。
func (platformDependenciesQRFake) GetSessionStatus(string) map[string]any {
	return map[string]any{"status": "waiting"}
}

// CompleteVerification 返回固定验证结果，避免测试输出任何真实凭证。
func (platformDependenciesQRFake) CompleteVerification(context.Context, string) (string, string, error) {
	return "masked-cookie", "account", nil
}

// TestNewPlatformDependenciesRejectsIncomplete 验证平台依赖不得以半初始化状态进入 Server。
func TestNewPlatformDependenciesRejectsIncomplete(t *testing.T) {
	// qrService 是测试使用的二维码能力替身。
	qrService := platformDependenciesQRFake{}
	// cases 保存每个缺失必需能力的构造场景和预期错误关键字。
	cases := []struct {
		name string
		mtop MTOPClient
		long LongLoginClient
		qr   QRLoginService
		want string
	}{
		{name: "mtop", long: renew.Service{}, qr: qrService, want: "MTOP"},
		{name: "long_login", mtop: NewMTOPClient(), qr: qrService, want: "长登录"},
		{name: "qr_login", mtop: NewMTOPClient(), long: renew.Service{}, want: "二维码"},
	}
	// testCase 表示当前平台能力缺失场景，循环变量仅用于表格驱动测试。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// dependencies、err 保存本次构造结果及校验错误。
			dependencies, err := NewPlatformDependencies(testCase.mtop, testCase.long, testCase.qr)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("NewPlatformDependencies error = %v, want substring %q", err, testCase.want)
			}
			if dependencies != nil {
				t.Fatalf("缺失依赖时不应返回平台依赖实例")
			}
		})
	}
}

// TestPlatformDependenciesAccessorsExposeInjectedClients 验证访问器只返回构造时注入的客户端。
func TestPlatformDependenciesAccessorsExposeInjectedClients(t *testing.T) {
	// mtopClient、longLoginClient 和 qrService 保存本测试注入的三个独立能力。
	mtopClient := NewMTOPClient()
	// longLoginClient 是本测试注入的长登录协议替身。
	longLoginClient := renew.Service{}
	// qrService 是本测试注入的二维码协议替身。
	qrService := platformDependenciesQRFake{}
	// dependencies、err 保存平台依赖边界及构造结果。
	dependencies, err := NewPlatformDependencies(mtopClient, longLoginClient, qrService)
	if err != nil {
		t.Fatalf("NewPlatformDependencies: %v", err)
	}
	if dependencies.MTOPClient() != mtopClient {
		t.Fatalf("MTOPClient 未返回注入客户端")
	}
	if dependencies.LongLoginClient() != longLoginClient {
		t.Fatalf("LongLoginClient 未返回注入客户端")
	}
	if dependencies.QRLoginService() != qrService {
		t.Fatalf("QRLoginService 未返回注入客户端")
	}
}

// TestPlatformDependenciesNilAccessors 验证 nil 接收者访问器安全返回 nil，便于错误路径诊断。
func TestPlatformDependenciesNilAccessors(t *testing.T) {
	// dependencies 显式保持 nil，模拟 Server 构造前的平台依赖缺失。
	var dependencies *PlatformDependencies
	if dependencies.MTOPClient() != nil || dependencies.LongLoginClient() != nil || dependencies.QRLoginService() != nil {
		t.Fatalf("nil 平台依赖访问器必须返回 nil")
	}
}
