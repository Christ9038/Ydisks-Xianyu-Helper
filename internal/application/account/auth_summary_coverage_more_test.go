package account

import (
	"context"
	"errors"
	"testing"
)

// TestAuthenticationServiceCoversNilReceiverGuards 覆盖认证服务各公开入口的未初始化保护。
func TestAuthenticationServiceCoversNilReceiverGuards(t *testing.T) {
	// service 保存 nil 接收者，验证应用层不会触发空指针。
	var service *AuthenticationService
	// err 保存 nil 认证服务初始化查询错误。
	if _, err := service.IsSystemInitialized(context.Background()); err == nil {
		t.Fatal("IsSystemInitialized 未拒绝 nil 服务")
	}
	// err 保存 nil 认证服务管理员初始化错误。
	if _, err := service.InitializeAdmin(context.Background(), "email", "password"); err == nil {
		t.Fatal("InitializeAdmin 未拒绝 nil 服务")
	}
	// err 保存 nil 认证服务邮箱查询错误。
	if _, err := service.UsernameByEmail(context.Background(), "email"); err == nil {
		t.Fatal("UsernameByEmail 未拒绝 nil 服务")
	}
	// err 保存 nil 认证服务登录错误。
	if _, _, err := service.Login(context.Background(), "user", "password"); err == nil {
		t.Fatal("Login 未拒绝 nil 服务")
	}
	// err 保存 nil 认证服务密码校验错误。
	if _, _, err := service.VerifyPassword(context.Background(), "user", "password"); err == nil {
		t.Fatal("VerifyPassword 未拒绝 nil 服务")
	}
	// err 保存 nil 认证服务密码更新错误。
	if _, err := service.UpdatePassword(context.Background(), "user", "password"); err == nil {
		t.Fatal("UpdatePassword 未拒绝 nil 服务")
	}
	// err 保存 nil 认证服务凭据更新错误。
	if err := service.UpdateCredentials(context.Background(), 7, "user", "password"); err == nil {
		t.Fatal("UpdateCredentials 未拒绝 nil 服务")
	}
}

// TestSummaryServiceCoversRemainingOwnershipBranches 覆盖摘要列表成功、服务未初始化和账号存在性边界。
func TestSummaryServiceCoversRemainingOwnershipBranches(t *testing.T) {
	// repository 保存非敏感摘要测试数据。
	repository := &summaryRepositoryFake{ids: []string{"acc-1"}, ownerID: 7}
	// service 保存完整装配的摘要服务。
	service, err := NewSummaryService(repository, repository)
	if err != nil {
		t.Fatalf("构造摘要服务失败: %v", err)
	}
	// ids、idsErr 保存账号 ID 列表结果。
	ids, idsErr := service.ListOwnedIDs(context.Background(), 7)
	if idsErr != nil || len(ids) != 1 || ids[0] != "acc-1" {
		t.Fatalf("账号 ID 列表异常: ids=%v err=%v", ids, idsErr)
	}
	// notFoundErr 保存账号属于当前用户但目标摘要不存在时的错误。
	notFoundErr := service.RequireOwnership(context.Background(), 7, "acc-1")
	if !errors.Is(notFoundErr, ErrNotFound) {
		t.Fatalf("本人账号不存在分支异常: %v", notFoundErr)
	}
	// _, invalidAccountErr 保存空账号标识拒绝结果。
	_, invalidAccountErr := service.ExistsOwned(context.Background(), 7, "")
	if invalidAccountErr == nil {
		t.Fatal("空账号标识未被拒绝")
	}

	// nilService 保存 nil 接收者，覆盖摘要服务的所有公开保护分支。
	var nilService *SummaryService
	// err 保存 nil 摘要服务账号列表错误。
	if _, err := nilService.ListOwnedIDs(context.Background(), 7); err == nil {
		t.Fatal("ListOwnedIDs 未拒绝 nil 服务")
	}
	// err 保存 nil 摘要服务列表错误。
	if _, err := nilService.ListSummaries(context.Background(), 7); err == nil {
		t.Fatal("ListSummaries 未拒绝 nil 服务")
	}
	// err 保存 nil 摘要服务单项查询错误。
	if _, err := nilService.GetOwnedSummary(context.Background(), 7, "acc-1"); err == nil {
		t.Fatal("GetOwnedSummary 未拒绝 nil 服务")
	}
	// err 保存 nil 摘要服务归属查询错误。
	if _, err := nilService.ExistsOwned(context.Background(), 7, "acc-1"); err == nil {
		t.Fatal("ExistsOwned 未拒绝 nil 服务")
	}
	// err 保存 nil 摘要服务状态查询错误。
	if _, err := nilService.StatusOwned(context.Background(), 7, "acc-1"); err == nil {
		t.Fatal("StatusOwned 未拒绝 nil 服务")
	}
	// err 保存 nil 摘要服务所有权校验错误。
	if err := nilService.RequireOwnership(context.Background(), 7, "acc-1"); err == nil {
		t.Fatal("RequireOwnership 未拒绝 nil 服务")
	}
	// err 保存 nil 摘要服务管理员列表错误。
	if _, err := nilService.ListAdminSummaries(context.Background()); err == nil {
		t.Fatal("ListAdminSummaries 未拒绝 nil 服务")
	}
}
