package server

import (
	"context"
	"testing"

	accountapp "xianyu-go/internal/application/account"
)

// serverPlatformCredentialPortFake 返回固定的窄平台凭证视图，验证 Server 不直连数据库仓储。
type serverPlatformCredentialPortFake struct {
	// detail 是当前测试端口返回的平台凭证视图。
	detail *accountapp.CredentialDetail
}

// LoadPlatformDetail 返回测试平台凭证视图。
func (fake serverPlatformCredentialPortFake) LoadPlatformDetail(context.Context, string) (*accountapp.CredentialDetail, error) {
	return fake.detail, nil
}

// TestLoadCookiePlatformDetailRequiresCredentialPort 验证缺少应用 Port 时 Server 不会绕过边界读取数据库。
func TestLoadCookiePlatformDetailRequiresCredentialPort(t *testing.T) {
	// server 是故意缺失平台凭证 Port 的 transport 实例。
	server := &Server{applications: NewApplicationPorts(ApplicationPortsInput{})}
	// loadErr 是缺失 Port 时阻止 Server 越过应用边界的预期错误。
	if _, loadErr := server.loadCookiePlatformDetail(context.Background(), "cid"); loadErr == nil {
		t.Fatal("缺少凭证 Port 时不应继续读取平台运行视图")
	}
}

// TestLoadCookiePlatformDetailUsesCredentialApplication 验证平台运行时读取只通过应用 Port。
func TestLoadCookiePlatformDetailUsesCredentialApplication(t *testing.T) {
	// service、serviceErr 分别是测试凭证应用服务及其构造错误。
	service, serviceErr := accountapp.NewPlatformCredentialService(serverPlatformCredentialPortFake{detail: &accountapp.CredentialDetail{ID: "cid", UserID: 9, Value: "sid=masked"}})
	if serviceErr != nil {
		t.Fatalf("构造凭证服务失败: %v", serviceErr)
	}
	// server 是仅通过凭证应用 Port 访问平台运行视图的 transport 实例。
	server := &Server{applications: NewApplicationPorts(ApplicationPortsInput{PlatformCredentials: service})}
	// detail、loadErr 分别是应用层返回的脱敏凭证视图及读取错误。
	detail, loadErr := server.loadCookiePlatformDetail(context.Background(), "cid")
	if loadErr != nil || detail == nil || detail.ID != "cid" || detail.Value == "" {
		t.Fatalf("平台凭证视图读取异常: detail=%+v err=%v", detail, loadErr)
	}
}
