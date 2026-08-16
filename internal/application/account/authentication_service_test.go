package account

import (
	"context"
	"errors"
	"testing"
)

// authenticationRepositoryFake 是认证应用服务测试使用的可控持久化端口替身。
type authenticationRepositoryFake struct {
	// initialized、initializedErr 保存系统初始化查询结果及错误。
	initialized    bool
	initializedErr error
	// initCreated、initErr 保存管理员初始化结果及错误。
	initCreated bool
	initErr     error
	// emailUsername、emailErr 保存邮箱查询结果及错误。
	emailUsername string
	emailErr      error
	// verifyUser、verifyMatched、verifyErr 保存密码校验结果及错误。
	verifyUser    AuthUser
	verifyMatched bool
	verifyErr     error
	// sessionID、sessionErr 保存会话创建结果及错误。
	sessionID  string
	sessionErr error
	// updatePasswordResult、updatePasswordErr 保存密码更新结果及错误。
	updatePasswordResult bool
	updatePasswordErr    error
	// updateCredentialsErr 保存登录凭据更新错误。
	updateCredentialsErr error
}

// IsSystemInitialized 返回测试预置的初始化状态。
func (f *authenticationRepositoryFake) IsSystemInitialized(context.Context) (bool, error) {
	return f.initialized, f.initializedErr
}

// InitializeAdmin 返回测试预置的管理员初始化结果。
func (f *authenticationRepositoryFake) InitializeAdmin(context.Context, string, string) (bool, error) {
	return f.initCreated, f.initErr
}

// UsernameByEmail 返回测试预置的邮箱映射结果。
func (f *authenticationRepositoryFake) UsernameByEmail(context.Context, string) (string, error) {
	return f.emailUsername, f.emailErr
}

// VerifyPassword 返回测试预置的密码校验结果。
func (f *authenticationRepositoryFake) VerifyPassword(context.Context, string, string) (AuthUser, bool, error) {
	return f.verifyUser, f.verifyMatched, f.verifyErr
}

// UpdatePassword 返回测试预置的密码更新结果。
func (f *authenticationRepositoryFake) UpdatePassword(context.Context, string, string) (bool, error) {
	return f.updatePasswordResult, f.updatePasswordErr
}

// UpdateCredentials 返回测试预置的登录凭据更新错误。
func (f *authenticationRepositoryFake) UpdateCredentials(context.Context, int64, string, string) error {
	return f.updateCredentialsErr
}

// CreateSession 返回测试预置的会话创建结果。
func (f *authenticationRepositoryFake) CreateSession(context.Context, AuthUser) (string, error) {
	return f.sessionID, f.sessionErr
}

// TestNewAuthenticationServiceRequiresRepository 验证认证服务拒绝缺少持久化端口。
func TestNewAuthenticationServiceRequiresRepository(t *testing.T) {
	// service、serviceErr 保存缺少 repository 时的构造结果。
	service, serviceErr := NewAuthenticationService(nil)
	if service != nil || serviceErr == nil {
		t.Fatalf("缺少 repository 应构造失败 service=%v err=%v", service, serviceErr)
	}
}

// TestAuthenticationServiceLoginBranches 覆盖登录成功、认证失败、校验错误和会话写入错误。
func TestAuthenticationServiceLoginBranches(t *testing.T) {
	// sessionErr 是模拟会话写入失败的固定错误。
	sessionErr := errors.New("session unavailable")
	// verifyErr 是模拟密码校验基础设施失败的固定错误。
	verifyErr := errors.New("verify unavailable")
	// testCases 保存登录分支及其预期结果。
	testCases := []struct {
		// name 是当前分支的可读名称。
		name string
		// repository 是当前分支使用的认证端口替身。
		repository *authenticationRepositoryFake
		// wantSession 表示预期返回的会话 ID。
		wantSession string
		// wantUser 表示预期返回的非敏感用户身份是否存在。
		wantUser bool
		// wantErr 表示预期返回的错误。
		wantErr error
	}{
		{name: "success", repository: &authenticationRepositoryFake{
			verifyUser: AuthUser{ID: 7, Username: "admin", IsAdmin: true}, verifyMatched: true, sessionID: "sid-1",
		}, wantSession: "sid-1", wantUser: true},
		{name: "mismatch", repository: &authenticationRepositoryFake{}, wantErr: nil},
		{name: "verify error", repository: &authenticationRepositoryFake{verifyErr: verifyErr}, wantErr: verifyErr},
		{name: "session error", repository: &authenticationRepositoryFake{
			verifyUser: AuthUser{ID: 7, Username: "admin"}, verifyMatched: true, sessionErr: sessionErr,
		}, wantErr: sessionErr},
	}
	// testCase 表示当前遍历到的登录分支测试参数。
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// service、serviceErr 保存当前分支的认证服务构造结果。
			service, serviceErr := NewAuthenticationService(testCase.repository)
			if serviceErr != nil {
				t.Fatalf("构造认证服务失败: %v", serviceErr)
			}
			// sessionID、user、loginErr 保存当前分支的登录结果。
			sessionID, user, loginErr := service.Login(context.Background(), "admin", "password")
			if !errors.Is(loginErr, testCase.wantErr) || sessionID != testCase.wantSession || (user != nil) != testCase.wantUser {
				t.Fatalf("登录结果 session=%q user=%+v err=%v", sessionID, user, loginErr)
			}
		})
	}
}

// TestAuthenticationServiceDelegatesAccountOperations 验证初始化、邮箱查询和改密操作保持端口结果。
func TestAuthenticationServiceDelegatesAccountOperations(t *testing.T) {
	// repository 是预置全部成功结果的认证端口替身。
	repository := &authenticationRepositoryFake{
		initialized: true, initCreated: true, emailUsername: "admin", verifyUser: AuthUser{ID: 7}, verifyMatched: true,
		updatePasswordResult: true,
	}
	// service、serviceErr 保存认证应用服务构造结果。
	service, serviceErr := NewAuthenticationService(repository)
	if serviceErr != nil {
		t.Fatalf("构造认证服务失败: %v", serviceErr)
	}
	// initialized、initializedErr 保存初始化状态查询结果。
	initialized, initializedErr := service.IsSystemInitialized(context.Background())
	if initializedErr != nil || !initialized {
		t.Fatalf("初始化状态异常 initialized=%v err=%v", initialized, initializedErr)
	}
	// created、createErr 保存管理员初始化结果。
	created, createErr := service.InitializeAdmin(context.Background(), "admin@example.com", "password")
	if createErr != nil || !created {
		t.Fatalf("管理员初始化异常 created=%v err=%v", created, createErr)
	}
	// username、usernameErr 保存邮箱映射结果。
	username, usernameErr := service.UsernameByEmail(context.Background(), "admin@example.com")
	if usernameErr != nil || username != "admin" {
		t.Fatalf("邮箱登录名异常 username=%q err=%v", username, usernameErr)
	}
	// user、matched、verifyErr 保存当前密码校验结果。
	user, matched, verifyErr := service.VerifyPassword(context.Background(), "admin", "password")
	if verifyErr != nil || !matched || user.ID != 7 {
		t.Fatalf("密码校验异常 user=%+v matched=%v err=%v", user, matched, verifyErr)
	}
	// updated、updateErr 保存密码更新结果。
	updated, updateErr := service.UpdatePassword(context.Background(), "admin", "new-password")
	if updateErr != nil || !updated {
		t.Fatalf("密码更新异常 updated=%v err=%v", updated, updateErr)
	}
	// credentialsErr 保存登录凭据更新结果。
	if credentialsErr := service.UpdateCredentials(context.Background(), 7, "operator", "new-password"); credentialsErr != nil {
		t.Fatalf("登录凭据更新异常: %v", credentialsErr)
	}
}

// TestAuthenticationServicePreservesCredentialErrors 验证认证服务不吞掉底层更新错误。
func TestAuthenticationServicePreservesCredentialErrors(t *testing.T) {
	// updateErr 是模拟凭据更新失败的固定错误。
	updateErr := errors.New("credential update unavailable")
	// repository 是返回凭据更新错误的认证端口替身。
	repository := &authenticationRepositoryFake{updateCredentialsErr: updateErr}
	// service、serviceErr 保存认证应用服务构造结果。
	service, serviceErr := NewAuthenticationService(repository)
	if serviceErr != nil {
		t.Fatalf("构造认证服务失败: %v", serviceErr)
	}
	// credentialsErr 保存应用服务透传的凭据更新错误。
	credentialsErr := service.UpdateCredentials(context.Background(), 7, "operator", "password")
	if !errors.Is(credentialsErr, updateErr) {
		t.Fatalf("凭据更新错误未透传 got=%v want=%v", credentialsErr, updateErr)
	}
}
