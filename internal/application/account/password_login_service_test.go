package account

import (
	"context"
	"errors"
	"testing"
)

// TestPasswordLoginServiceKeepsDisabledPolicy 验证密码登录的启动、查询和取消均由应用层统一拒绝。
func TestPasswordLoginServiceKeepsDisabledPolicy(t *testing.T) {
	// service 是当前密码登录关闭策略的应用服务实例。
	service := NewPasswordLoginService()
	// startErr 保存启动密码登录的策略结果。
	startErr := service.Start(context.Background(), PasswordLoginStartInput{UserID: 7})
	// checkErr 保存查询密码登录会话的策略结果。
	checkErr := service.Check(context.Background(), PasswordLoginSessionInput{UserID: 7, SessionID: "session-1"})
	// cancelErr 保存取消密码登录会话的策略结果。
	cancelErr := service.Cancel(context.Background(), PasswordLoginSessionInput{UserID: 7, SessionID: "session-1"})
	if !errors.Is(startErr, ErrPasswordLoginDisabled) {
		t.Fatalf("启动操作应返回密码登录关闭错误，got %v", startErr)
	}
	if !errors.Is(checkErr, ErrPasswordLoginDisabled) {
		t.Fatalf("查询操作应返回密码登录关闭错误，got %v", checkErr)
	}
	if !errors.Is(cancelErr, ErrPasswordLoginDisabled) {
		t.Fatalf("取消操作应返回密码登录关闭错误，got %v", cancelErr)
	}
}

// TestPasswordLoginServiceDoesNotRequireCredentialInput 验证应用服务接口不存在密码、Cookie 或平台秘密参数。
func TestPasswordLoginServiceDoesNotRequireCredentialInput(t *testing.T) {
	// service 是不保存任何密码登录状态的应用服务实例。
	service := NewPasswordLoginService()
	// emptyErr 保存空身份请求的结果；关闭策略应先于任何凭证读取返回。
	emptyErr := service.Start(context.Background(), PasswordLoginStartInput{})
	if !errors.Is(emptyErr, ErrPasswordLoginDisabled) {
		t.Fatalf("空身份请求仍应保持关闭策略，got %v", emptyErr)
	}
}
