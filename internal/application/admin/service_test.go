package admin

import (
	"context"
	"errors"
	"testing"
)

// adminRepositoryStub 保存管理员应用服务测试中的可控仓储行为和调用记录。
type adminRepositoryStub struct {
	// listResult 保存用户列表返回值。
	listResult []UserSummary
	// listErr 保存用户列表错误。
	listErr error
	// deleteErr 保存用户删除错误。
	deleteErr error
	// statsResult 保存统计返回值。
	statsResult Stats
	// statsErr 保存统计错误。
	statsErr error
	// deletedUserID 保存最后一次删除的目标用户。
	deletedUserID int64
}

// ListUsers 返回测试预置的用户摘要。
func (r *adminRepositoryStub) ListUsers(context.Context) ([]UserSummary, error) {
	return r.listResult, r.listErr
}

// DeleteUser 记录删除目标并返回测试预置错误。
func (r *adminRepositoryStub) DeleteUser(_ context.Context, userID int64) error {
	r.deletedUserID = userID
	return r.deleteErr
}

// Stats 返回测试预置的仪表盘统计。
func (r *adminRepositoryStub) Stats(context.Context) (Stats, error) {
	return r.statsResult, r.statsErr
}

// TestServiceListUsersAndStats 验证管理员摘要与统计成功、错误和空服务分支。
func TestServiceListUsersAndStats(t *testing.T) {
	// expectedUsers、expectedStats 保存成功路径的应用模型结果。
	expectedUsers := []UserSummary{{ID: 7, Username: "admin"}}
	// expectedStats 保存成功路径的统计应用模型结果。
	expectedStats := Stats{TotalUsers: 2, TotalOrders: 3}
	// repository 保存成功路径测试仓储。
	repository := &adminRepositoryStub{listResult: expectedUsers, statsResult: expectedStats}
	// service 保存待验证的管理员应用服务。
	service := NewService(repository)
	// users、usersErr 保存用户摘要查询结果。
	users, usersErr := service.ListUsers(context.Background())
	if usersErr != nil || len(users) != 1 || users[0].ID != 7 {
		t.Fatalf("用户摘要查询异常 users=%v err=%v", users, usersErr)
	}
	// stats、statsErr 保存统计查询结果。
	stats, statsErr := service.Stats(context.Background())
	if statsErr != nil || stats.TotalOrders != 3 {
		t.Fatalf("统计查询异常 stats=%+v err=%v", stats, statsErr)
	}
	// listFailure、statsFailure 保存基础设施错误分支。
	listFailure := errors.New("list failed")
	// statsFailure、failureService 保存统计错误和组合错误仓储服务。
	statsFailure := errors.New("stats failed")
	// failureService 保存返回基础设施错误的管理员应用服务。
	failureService := NewService(&adminRepositoryStub{listErr: listFailure, statsErr: statsFailure})
	if !errors.Is(mustListUsers(failureService), listFailure) || !errors.Is(mustStats(failureService), statsFailure) {
		t.Fatal("管理员查询错误未原样返回")
	}
	// err 保存空管理员服务的装配错误。
	if _, err := (*Service)(nil).ListUsers(context.Background()); err == nil {
		t.Fatal("空管理员服务应拒绝用户列表查询")
	}
}

// mustListUsers 将列表错误转换为便于测试断言的 error。
func mustListUsers(service *Service) error {
	// err 保存管理员列表查询错误。
	_, err := service.ListUsers(context.Background())
	return err
}

// mustStats 将统计错误转换为便于测试断言的 error。
func mustStats(service *Service) error {
	// err 保存管理员统计查询错误。
	_, err := service.Stats(context.Background())
	return err
}

// TestServiceDeleteUser 验证无效身份、自删、成功删除和仓储失败分支。
func TestServiceDeleteUser(t *testing.T) {
	// repository 保存可记录删除调用的测试仓储。
	repository := &adminRepositoryStub{}
	// service 保存待验证的管理员应用服务。
	service := NewService(repository)
	if !errors.Is(service.DeleteUser(context.Background(), 0, 2), ErrInvalidUser) {
		t.Fatal("无效当前用户应被拒绝")
	}
	if !errors.Is(service.DeleteUser(context.Background(), 1, 1), ErrSelfDelete) {
		t.Fatal("删除当前用户应被拒绝")
	}
	// err 保存管理员删除操作的错误。
	if err := service.DeleteUser(context.Background(), 1, 2); err != nil || repository.deletedUserID != 2 {
		t.Fatalf("删除成功路径异常 err=%v id=%d", err, repository.deletedUserID)
	}
	// deleteFailure 保存仓储删除失败结果。
	deleteFailure := errors.New("delete failed")
	// err 保存删除仓储返回的错误。
	if err := NewService(&adminRepositoryStub{deleteErr: deleteFailure}).DeleteUser(context.Background(), 1, 2); !errors.Is(err, deleteFailure) {
		t.Fatal("删除错误未原样返回")
	}
	// unavailableErr 保存空服务的装配错误。
	unavailableErr := (*Service)(nil).DeleteUser(context.Background(), 1, 2)
	if unavailableErr == nil {
		t.Fatal("空管理员服务应拒绝删除")
	}
}
