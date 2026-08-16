package server

import (
	"context"
	"testing"

	"xianyu-go/internal/adapter"
	accountapp "xianyu-go/internal/application/account"
)

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
