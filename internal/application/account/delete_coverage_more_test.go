package account

import (
	"context"
	"errors"
	"testing"
)

// TestDeleteServiceCoversGuardsAndRepositoryErrors 验证账号删除服务的构造、输入、归属和持久化错误边界。
func TestDeleteServiceCoversGuardsAndRepositoryErrors(t *testing.T) {
	// ctx 是删除用例使用的基础上下文。
	ctx := context.Background()
	// constructorErr 保存缺少删除仓储时的构造错误。
	if _, constructorErr := NewDeleteService(nil, nil); constructorErr == nil {
		t.Fatal("缺少删除仓储时构造应失败")
	}
	// nilService 表示未初始化的账号删除服务。
	var nilService *DeleteService
	if nilService.Delete(ctx, 7, "account") == nil {
		t.Fatal("空删除服务应返回错误")
	}
	// repositoryErr 是账号摘要查询需要返回的归属错误。
	repositoryErr := errors.New("summary lookup failed")
	// repository 保存摘要查询错误的删除仓储替身。
	repository := &deleteRepositoryStub{getErr: repositoryErr}
	// service 保存待验证的账号删除服务。
	service, serviceErr := NewDeleteService(repository, nil)
	if serviceErr != nil {
		t.Fatalf("构造删除服务失败: %v", serviceErr)
	}
	if service.Delete(ctx, 7, "") == nil {
		t.Fatal("空账号标识应返回错误")
	}
	if !errors.Is(service.Delete(ctx, 7, "account"), repositoryErr) {
		t.Fatal("摘要查询错误未透传")
	}
	// deleteErr 是持久化删除需要返回的稳定错误。
	deleteErr := errors.New("delete failed")
	// deleteRepository 保存删除阶段错误的仓储替身。
	deleteRepository := &deleteRepositoryStub{deleteErr: deleteErr}
	// deleteService 保存删除阶段错误场景的服务。
	deleteService, deleteServiceErr := NewDeleteService(deleteRepository, nil)
	if deleteServiceErr != nil {
		t.Fatalf("构造删除错误服务失败: %v", deleteServiceErr)
	}
	if !errors.Is(deleteService.Delete(ctx, 7, "account"), deleteErr) {
		t.Fatal("删除错误未透传")
	}
}
