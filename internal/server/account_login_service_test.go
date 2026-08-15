package server

import (
	"context"
	"testing"
)

// TestAccountLoginServiceValidateCookieInput 验证账号登录输入的基础校验规则。
func TestAccountLoginServiceValidateCookieInput(t *testing.T) {
	// service 使用空 Server 即可测试纯输入校验。
	service := (&Server{}).accountLoginApplication()
	// err 表示空账号输入的校验结果。
	if err := service.ValidateCookieInput(accountLoginInput{AccountID: "", Cookies: "cookie"}); err == nil {
		t.Fatal("空账号 ID 应该校验失败")
	}
	// err 表示有效账号输入的校验结果。
	if err := service.ValidateCookieInput(accountLoginInput{AccountID: "acc1", Cookies: "cookie"}); err != nil {
		t.Fatalf("有效账号登录输入不应失败: %v", err)
	}
}

// TestAccountLoginServicePersistQRLoginRejectsIncompleteResult 验证扫码结果缺少凭证时不会写入账号。
func TestAccountLoginServicePersistQRLoginRejectsIncompleteResult(t *testing.T) {
	// service 使用空 Server 即可在凭证校验前验证失败结果。
	service := (&Server{}).accountLoginApplication()
	// err 保存扫码结果校验错误。
	_, err := service.PersistQRLoginSuccess(context.Background(), 1, "session", map[string]any{"status": "success"}, "")
	if err == nil {
		t.Fatal("缺少 cookies 或 unb 的扫码结果应该失败")
	}
}
