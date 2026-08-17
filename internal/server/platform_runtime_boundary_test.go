package server

import (
	"context"
	"testing"

	"xianyu-go/internal/adapter"
	accountapp "xianyu-go/internal/application/account"
)

// serverPlatformCredentialPortFake 返回固定的窄平台凭证视图，验证 Server 不直连数据库仓储。
type serverPlatformCredentialPortFake struct {
	// detail 是当前测试端口返回的平台凭证视图。
	detail *accountapp.CredentialDetail
}

// LoadPlatformDetail 返回测试平台凭证视图。
func (f serverPlatformCredentialPortFake) LoadPlatformDetail(context.Context, string) (*accountapp.CredentialDetail, error) {
	return f.detail, nil
}

// TestLoadCookiePlatformDetailRequiresCredentialPort 验证平台运行时读取不会在缺少应用 Port 时直接访问 Store。
func TestLoadCookiePlatformDetailRequiresCredentialPort(t *testing.T) {
	// srv 是仅装配空账号登录服务的测试 Server，模拟凭证适配器缺失场景。
	srv := &Server{applications: &applicationServices{accountLogin: &accountLoginService{}}}
	// loadErr 保存缺少凭证 Port 时的平台视图读取错误。
	_, loadErr := srv.loadCookiePlatformDetail(context.Background(), "cid")
	if loadErr == nil {
		t.Fatal("缺少凭证 Port 时不应继续读取平台运行视图")
	}
}

// TestLoadCookiePlatformDetailUsesCredentialApplication 验证平台运行视图通过应用 Port 读取。
func TestLoadCookiePlatformDetailUsesCredentialApplication(t *testing.T) {
	// service 是绑定最小凭证读取端口的平台应用服务。
	service, serviceErr := accountapp.NewPlatformCredentialService(serverPlatformCredentialPortFake{detail: &accountapp.CredentialDetail{ID: "cid", UserID: 9, Value: "sid=masked"}})
	if serviceErr != nil {
		t.Fatalf("构造凭证服务失败: %v", serviceErr)
	}
	// srv 是只装配平台凭证应用服务的测试 Server。
	srv := &Server{applications: &applicationServices{platformCredentials: service}}
	// detail、loadErr 保存应用 Port 返回的平台视图及错误。
	detail, loadErr := srv.loadCookiePlatformDetail(context.Background(), "cid")
	if loadErr != nil || detail == nil || detail.ID != "cid" || detail.Value == "" {
		t.Fatalf("平台凭证视图读取异常: detail=%+v err=%v", detail, loadErr)
	}
}

// TestPersistMTopCookieSessionRequiresCredentialPort 验证 MTOP Cookie 会话写回通过凭证 Port 而非直接访问 Store。
func TestPersistMTopCookieSessionRequiresCredentialPort(t *testing.T) {
	// srv 是仅装配空账号登录服务的测试 Server，模拟写回适配器缺失场景。
	srv := &Server{applications: &applicationServices{accountLogin: &accountLoginService{}}}
	// detail 是不含登录密码的兼容平台凭证模型。
	detail := &accountapp.CredentialDetail{ID: "cid", Value: "sid=old", MetadataJSON: "{}"}
	// ctx、session 保存发生权威 Cookie 变化的 MTOP 会话。
	ctx, session := adapter.WithFlatCookieSession(context.Background(), detail.Value)
	session.ReplaceSnapshot([]adapter.BrowserCookie{{Name: "sid", Value: "new", Domain: ".goofish.com", Path: "/"}})
	// value、changed、handled、persistErr 保存会话写回结果及其错误阶段。
	_, changed, handled, persistErr := srv.persistMTopCookieSessionLocked(ctx, detail, session)
	if !changed || !handled || persistErr == nil {
		t.Fatalf("缺少凭证 Port 时应报告写回错误: changed=%v handled=%v err=%v", changed, handled, persistErr)
	}
}

// TestPlatformCredentialSessionPortIsAvailable 验证平台会话写回通过专用最小端口暴露。
func TestPlatformCredentialSessionPortIsAvailable(t *testing.T) {
	// srv、cleanup 保存完整应用装配的测试 Server 及资源清理函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// sessionPort 保存仅包含 Cookie 写回能力的平台会话端口。
	sessionPort := srv.platformCredentialSessionPort()
	if sessionPort == nil {
		t.Fatal("完整应用装配应提供平台会话写回端口")
	}
}
