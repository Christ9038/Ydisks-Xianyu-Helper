package account

import (
	"context"
	"errors"
	"testing"
)

// summaryRepositoryFake 是账号摘要应用服务测试用的可控持久化端口替身。
type summaryRepositoryFake struct {
	// ids 是待返回的账号 ID 列表。
	ids []string
	// summaries 是待返回的普通用户账号摘要。
	summaries []AccountSummary
	// adminSummaries 是待返回的管理员账号摘要。
	adminSummaries []AdminAccountSummary
	// summary 是待返回的单个账号摘要。
	summary AccountSummary
	// ownerID 是待返回的账号所有者。
	ownerID int64
	// owned 是待返回的联合所有权结论。
	owned bool
	// listErr、summaryErr、ownedErr、ownerErr、statusErr 模拟各查询阶段错误。
	listErr    error
	summaryErr error
	ownedErr   error
	ownerErr   error
	statusErr  error
	// enabled 是待返回的账号启用状态。
	enabled bool
}

// ListOwnedIDs 返回测试预置的账号 ID 列表或列表错误。
func (f *summaryRepositoryFake) ListOwnedIDs(context.Context, int64) ([]string, error) {
	return f.ids, f.listErr
}

// ListSummaries 返回测试预置的账号摘要或列表错误。
func (f *summaryRepositoryFake) ListSummaries(context.Context, int64) ([]AccountSummary, error) {
	return f.summaries, f.listErr
}

// GetOwnedSummary 返回测试预置的账号摘要或摘要错误。
func (f *summaryRepositoryFake) GetOwnedSummary(context.Context, int64, string) (AccountSummary, error) {
	return f.summary, f.summaryErr
}

// ExistsOwned 返回测试预置的所有权结论或查询错误。
func (f *summaryRepositoryFake) ExistsOwned(context.Context, int64, string) (bool, error) {
	return f.owned, f.ownedErr
}

// GetOwnerID 返回测试预置的账号所有者或查询错误。
func (f *summaryRepositoryFake) GetOwnerID(context.Context, string) (int64, error) {
	return f.ownerID, f.ownerErr
}

// StatusOwned 返回测试预置的启用状态或查询错误。
func (f *summaryRepositoryFake) StatusOwned(context.Context, int64, string) (bool, error) {
	return f.enabled, f.statusErr
}

// ListAdminSummaries 返回测试预置的管理员账号摘要或查询错误。
func (f *summaryRepositoryFake) ListAdminSummaries(context.Context) ([]AdminAccountSummary, error) {
	return f.adminSummaries, f.listErr
}

// TestSummaryServiceValidatesInputs 验证账号摘要服务拒绝无效用户和空账号标识。
func TestSummaryServiceValidatesInputs(t *testing.T) {
	// service 是使用可控摘要端口构造的应用服务。
	service, err := NewSummaryService(&summaryRepositoryFake{}, &summaryRepositoryFake{})
	if err != nil {
		t.Fatalf("构造摘要服务失败: %v", err)
	}
	// _, listErr 保存无效用户列表查询的验证错误。
	if _, listErr := service.ListOwnedIDs(context.Background(), 0); listErr == nil {
		t.Fatal("无效用户 ID 不应读取账号列表")
	}
	// _, summaryErr 保存空账号摘要查询的验证错误。
	if _, summaryErr := service.GetOwnedSummary(context.Background(), 7, ""); summaryErr == nil {
		t.Fatal("空账号 ID 不应读取账号摘要")
	}
}

// TestSummaryServicePropagatesReadAndAdminErrors 验证摘要服务不吞掉各读取阶段错误。
func TestSummaryServicePropagatesReadAndAdminErrors(t *testing.T) {
	// readErr 是底层摘要查询模拟的基础设施故障。
	readErr := errors.New("摘要读取失败")
	// repository 是返回读取故障的摘要端口替身。
	repository := &summaryRepositoryFake{listErr: readErr, summaryErr: readErr, ownedErr: readErr, statusErr: readErr}
	// service 是绑定故障端口的应用服务。
	service, err := NewSummaryService(repository, repository)
	if err != nil {
		t.Fatalf("构造摘要服务失败: %v", err)
	}
	// _, listErr 保存摘要列表读取的底层错误。
	if _, listErr := service.ListSummaries(context.Background(), 7); !errors.Is(listErr, readErr) {
		t.Fatalf("列表错误未保留: %v", listErr)
	}
	// _, summaryErr 保存单个账号摘要读取的底层错误。
	if _, summaryErr := service.GetOwnedSummary(context.Background(), 7, "acc1"); !errors.Is(summaryErr, readErr) {
		t.Fatalf("单项摘要错误未保留: %v", summaryErr)
	}
	// _, ownedErr 保存账号归属查询的底层错误。
	if _, ownedErr := service.ExistsOwned(context.Background(), 7, "acc1"); !errors.Is(ownedErr, readErr) {
		t.Fatalf("归属错误未保留: %v", ownedErr)
	}
	// _, statusErr 保存账号状态读取的底层错误。
	if _, statusErr := service.StatusOwned(context.Background(), 7, "acc1"); !errors.Is(statusErr, readErr) {
		t.Fatalf("状态错误未保留: %v", statusErr)
	}
	// _, adminErr 保存管理员摘要读取的底层错误。
	if _, adminErr := service.ListAdminSummaries(context.Background()); !errors.Is(adminErr, readErr) {
		t.Fatalf("管理员摘要错误未保留: %v", adminErr)
	}
}

// TestSummaryServiceRequireOwnershipDistinguishesResults 验证所有权服务区分本人、越权、不存在和查询故障。
func TestSummaryServiceRequireOwnershipDistinguishesResults(t *testing.T) {
	// cases 覆盖所有权判断的四种结果，并确保越权不会读取敏感数据。
	cases := []struct {
		// name 是当前所有权场景名称。
		name string
		// repository 是当前场景使用的摘要端口替身。
		repository *summaryRepositoryFake
		// wantErr 是期望的稳定应用错误；nil 表示归属通过。
		wantErr error
	}{
		{name: "owned", repository: &summaryRepositoryFake{owned: true}, wantErr: nil},
		{name: "forbidden", repository: &summaryRepositoryFake{ownerID: 8}, wantErr: ErrForbidden},
		{name: "missing", repository: &summaryRepositoryFake{ownerErr: ErrNotFound}, wantErr: ErrNotFound},
		{name: "query-failure", repository: &summaryRepositoryFake{ownedErr: errors.New("exists 查询失败")}, wantErr: errors.New("exists 查询失败")},
	}
	// testCase 表示当前正在验证的所有权场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// service 是当前场景的账号摘要应用服务。
			service, err := NewSummaryService(testCase.repository, testCase.repository)
			if err != nil {
				t.Fatalf("构造摘要服务失败: %v", err)
			}
			// gotErr 保存应用服务返回的所有权结果。
			gotErr := service.RequireOwnership(context.Background(), 7, "acc1")
			if testCase.wantErr == nil {
				if gotErr != nil {
					t.Fatalf("本人账号不应失败: %v", gotErr)
				}
				return
			}
			if gotErr == nil || gotErr.Error() != testCase.wantErr.Error() {
				t.Fatalf("所有权错误=%v，期望=%v", gotErr, testCase.wantErr)
			}
		})
	}
}

// TestNewSummaryServiceRequiresRepository 验证摘要服务拒绝缺失普通用户端口，但允许管理员端口按需缺省。
func TestNewSummaryServiceRequiresRepository(t *testing.T) {
	// adminRepository 是满足管理员摘要能力的测试端口。
	adminRepository := &summaryRepositoryFake{}
	// _, missingRepositoryErr 保存缺少普通用户端口时的构造错误。
	if _, missingRepositoryErr := NewSummaryService(nil, adminRepository); missingRepositoryErr == nil {
		t.Fatal("缺少普通用户摘要端口时应构造失败")
	}
	// service 是仅装配普通用户端口的应用服务。
	service, err := NewSummaryService(adminRepository, nil)
	if err != nil {
		t.Fatalf("管理员端口可选时构造失败: %v", err)
	}
	// _, missingAdminErr 保存缺少管理员端口时的调用错误。
	if _, missingAdminErr := service.ListAdminSummaries(context.Background()); missingAdminErr == nil {
		t.Fatal("未装配管理员端口时不应伪装成功")
	}
}
