package items

import (
	"context"
	"errors"
	"testing"
)

// syncRepositoryFake 是隔离应用服务测试的商品同步 Port 桩。
type syncRepositoryFake struct {
	// owned 表示桩返回的账号归属结果。
	owned bool
	// ownershipErr 保存归属查询错误。
	ownershipErr error
	// allResult 保存全量同步桩的结果。
	allResult SyncAllResult
	// pageResult 保存分页同步桩的结果。
	pageResult SyncPageResult
	// err 保存桩返回的错误。
	err error
	// allQuery 保存最近一次全量查询。
	allQuery SyncQuery
	// pageQuery 保存最近一次分页查询。
	pageQuery SyncQuery
}

// OwnsAccount 返回预设的账号归属结果。
func (f *syncRepositoryFake) OwnsAccount(_ context.Context, _ int64, _ string) (bool, error) {
	return f.owned, f.ownershipErr
}

// SyncAll 记录全量查询并返回预设结果。
func (f *syncRepositoryFake) SyncAll(_ context.Context, query SyncQuery) (SyncAllResult, error) {
	f.allQuery = query
	return f.allResult, f.err
}

// SyncPage 记录分页查询并返回预设结果。
func (f *syncRepositoryFake) SyncPage(_ context.Context, query SyncQuery) (SyncPageResult, error) {
	f.pageQuery = query
	return f.pageResult, f.err
}

// TestSyncServiceForwardsQueries 验证应用服务只校验输入并转发业务查询。
func TestSyncServiceForwardsQueries(t *testing.T) {
	// repository 保存本次测试使用的 Port 桩。
	repository := &syncRepositoryFake{owned: true, allResult: SyncAllResult{TotalCount: 2}, pageResult: SyncPageResult{SavedCount: 1}}
	// service 保存本次测试使用的应用服务。
	service := NewSyncService(repository)
	// query 保存需要传入应用服务的业务查询。
	query := SyncQuery{UserID: 7, CookieID: "acc1", PageNumber: 2, PageSize: 10, MaxPages: 3}
	// allResult、err 保存全量同步结果和错误。
	allResult, err := service.SyncAll(context.Background(), query)
	if err != nil || allResult.TotalCount != 2 || repository.allQuery != query {
		t.Fatalf("all result=%+v err=%v query=%+v", allResult, err, repository.allQuery)
	}
	// pageResult、err 保存分页同步结果和错误。
	pageResult, err := service.SyncPage(context.Background(), query)
	if err != nil || pageResult.SavedCount != 1 || repository.pageQuery != query {
		t.Fatalf("page result=%+v err=%v query=%+v", pageResult, err, repository.pageQuery)
	}
}

// TestSyncServiceRejectsInvalidInput 验证用户和账号参数在进入基础设施前被拒绝。
func TestSyncServiceRejectsInvalidInput(t *testing.T) {
	// cases 保存需要覆盖的无效输入及期望错误。
	cases := []struct {
		// name 是测试场景名称。
		name string
		// query 是待校验的同步查询。
		query SyncQuery
		// wantErr 是期望的稳定错误。
		wantErr error
	}{
		{name: "invalid-user", query: SyncQuery{CookieID: "acc1"}, wantErr: ErrSyncInvalidUser},
		{name: "invalid-cookie", query: SyncQuery{UserID: 1}, wantErr: ErrSyncInvalidCookie},
	}
	// testCase 表示当前输入场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// err 是本次校验返回的错误。
			_, err := NewSyncService(&syncRepositoryFake{}).SyncAll(context.Background(), testCase.query)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("err=%v want=%v", err, testCase.wantErr)
			}
		})
	}
}

// TestSyncServicePreservesErrorKind 验证应用服务保留平台和持久化错误阶段。
func TestSyncServicePreservesErrorKind(t *testing.T) {
	// platformErr 是模拟平台失败的阶段错误。
	platformErr := &SyncError{Kind: SyncErrorPlatform, Err: errors.New("平台失败")}
	// persistenceErr 是模拟持久化失败的阶段错误。
	persistenceErr := &SyncError{Kind: SyncErrorPersistence, Err: errors.New("保存失败")}
	// cases 保存需要透传的阶段错误。
	cases := []struct {
		// name 是测试场景名称。
		name string
		// err 是 Port 返回的错误。
		err error
		// wantKind 是期望保留的阶段。
		wantKind SyncErrorKind
	}{
		{name: "platform", err: platformErr, wantKind: SyncErrorPlatform},
		{name: "persistence", err: persistenceErr, wantKind: SyncErrorPersistence},
	}
	// testCase 表示当前阶段错误场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// result、err 保存 Port 调用结果。
			_, err := NewSyncService(&syncRepositoryFake{owned: true, err: testCase.err}).SyncAll(context.Background(), SyncQuery{UserID: 1, CookieID: "acc1"})
			// stageErr 保存可识别的阶段错误。
			var stageErr *SyncError
			if !errors.As(err, &stageErr) || stageErr.Kind != testCase.wantKind {
				t.Fatalf("err=%v stage=%+v", err, stageErr)
			}
		})
	}
}

// TestSyncServiceRejectsUnownedAccount 验证应用服务在读取平台凭证前拒绝跨用户账号。
func TestSyncServiceRejectsUnownedAccount(t *testing.T) {
	// err 保存跨用户账号应返回的归属错误。
	_, err := NewSyncService(&syncRepositoryFake{owned: false}).SyncPage(context.Background(), SyncQuery{UserID: 1, CookieID: "other"})
	if !errors.Is(err, ErrSyncNotOwned) {
		t.Fatalf("err=%v want=%v", err, ErrSyncNotOwned)
	}
}
