package notifications

import (
	"context"
	"errors"
	"testing"
)

// fakeRepository 是通知不确定状态应用服务测试使用的端口替身。
type fakeRepository struct {
	// userItems 保存普通用户查询应返回的脱敏摘要。
	userItems []UncertainSummary
	// adminItems 保存管理员查询应返回的脱敏摘要。
	adminItems []UncertainSummary
	// userTotal 保存普通用户查询应返回的总数。
	userTotal int
	// adminTotal 保存管理员查询应返回的总数。
	adminTotal int
	// listErr 保存列表查询错误。
	listErr error
	// countErr 保存统计查询错误。
	countErr error
	// userID 保存普通用户查询收到的归属标识。
	userID int64
	// userLimit 保存普通用户查询收到的上限。
	userLimit int
	// adminLimit 保存管理员查询收到的上限。
	adminLimit int
}

// ListUncertainForUser 返回普通用户预置摘要并记录归属参数。
func (r *fakeRepository) ListUncertainForUser(_ context.Context, userID int64, limit int) ([]UncertainSummary, error) {
	r.userID = userID
	r.userLimit = limit
	return r.userItems, r.listErr
}

// CountUncertainForUser 返回普通用户预置总数。
func (r *fakeRepository) CountUncertainForUser(context.Context, int64) (int, error) {
	return r.userTotal, r.countErr
}

// ListUncertainForAdmin 返回管理员预置摘要并记录查询上限。
func (r *fakeRepository) ListUncertainForAdmin(_ context.Context, limit int) ([]UncertainSummary, error) {
	r.adminLimit = limit
	return r.adminItems, r.listErr
}

// CountUncertainForAdmin 返回管理员预置总数。
func (r *fakeRepository) CountUncertainForAdmin(context.Context) (int, error) {
	return r.adminTotal, r.countErr
}

// TestListForUserReturnsScopedSummary 验证普通用户查询传递归属并返回列表与总数。
func TestListForUserReturnsScopedSummary(t *testing.T) {
	// repository 是带有非敏感摘要夹具的持久化端口替身。
	repository := &fakeRepository{userItems: []UncertainSummary{{ID: 7, EventType: "order"}}, userTotal: 3}
	// service 是待验证的通知应用服务。
	service := New(repository)
	// items、total、err 保存普通用户查询结果。
	items, total, err := service.ListForUser(context.Background(), 9, 17)
	if err != nil || total != 3 || len(items) != 1 || repository.userID != 9 || repository.userLimit != 17 {
		t.Fatalf("普通用户查询结果异常: items=%+v total=%d err=%v repository=%+v", items, total, err, repository)
	}
}

// TestListForAdminReturnsGlobalSummary 验证管理员查询使用全局端口并保留运维上限。
func TestListForAdminReturnsGlobalSummary(t *testing.T) {
	// repository 是带有管理员摘要夹具的持久化端口替身。
	repository := &fakeRepository{adminItems: []UncertainSummary{{ID: 8, OwnerUserID: 2}}, adminTotal: 4}
	// service 是待验证的通知应用服务。
	service := New(repository)
	// items、total、err 保存管理员查询结果。
	items, total, err := service.ListForAdmin(context.Background(), 50)
	if err != nil || total != 4 || len(items) != 1 || repository.adminLimit != 50 || items[0].OwnerUserID != 2 {
		t.Fatalf("管理员查询结果异常: items=%+v total=%d err=%v repository=%+v", items, total, err, repository)
	}
}

// TestListRejectsInvalidServiceAndUser 验证缺少端口或无效用户归属时拒绝执行查询。
func TestListRejectsInvalidServiceAndUser(t *testing.T) {
	// repository 是满足接口但不应被无效输入调用的端口替身。
	repository := &fakeRepository{}
	// cases 保存需要返回参数错误的服务与用户组合。
	cases := []struct {
		// name 是当前无效输入场景名称。
		name string
		// service 是当前场景的应用服务。
		service *Service
		// userID 是当前场景的用户归属标识。
		userID int64
	}{
		{name: "空服务", service: nil, userID: 1},
		{name: "空端口", service: New(nil), userID: 1},
		{name: "无效用户", service: New(repository), userID: 0},
	}
	// testCase 表示当前遍历的无效输入场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// _, err 保存当前场景的普通用户查询结果。
			_, _, err := testCase.service.ListForUser(context.Background(), testCase.userID, 10)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("无效输入未被拒绝: %v", err)
			}
		})
	}
	// _, adminErr 保存空端口管理员查询结果。
	_, _, adminErr := New(nil).ListForAdmin(context.Background(), 10)
	if !errors.Is(adminErr, ErrInvalidInput) {
		t.Fatalf("空端口管理员查询未被拒绝: %v", adminErr)
	}
}

// TestListPropagatesRepositoryErrors 验证列表或统计失败都会原样返回且不伪造结果。
func TestListPropagatesRepositoryErrors(t *testing.T) {
	// listErr 是持久化列表查询失败原因。
	listErr := errors.New("列表端口失败")
	// listRepository 是模拟列表失败的端口替身。
	listRepository := &fakeRepository{listErr: listErr}
	// _, _, gotListErr 保存列表失败的应用服务结果。
	_, _, gotListErr := New(listRepository).ListForUser(context.Background(), 1, 10)
	if !errors.Is(gotListErr, listErr) {
		t.Fatalf("列表错误未透传: %v", gotListErr)
	}
	// countErr 是持久化统计查询失败原因。
	countErr := errors.New("统计端口失败")
	// countRepository 是模拟统计失败的端口替身。
	countRepository := &fakeRepository{countErr: countErr}
	// _, _, gotCountErr 保存统计失败的应用服务结果。
	_, _, gotCountErr := New(countRepository).ListForAdmin(context.Background(), 10)
	if !errors.Is(gotCountErr, countErr) {
		t.Fatalf("统计错误未透传: %v", gotCountErr)
	}
}
