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
	// pageInvalidErr 保存分页同步入口拒绝无效用户的错误。
	if _, pageInvalidErr := NewSyncService(&syncRepositoryFake{}).SyncPage(context.Background(), SyncQuery{CookieID: "acc1"}); !errors.Is(pageInvalidErr, ErrSyncInvalidUser) {
		t.Fatalf("page invalid user error=%v", pageInvalidErr)
	}
	// pageCookieErr 保存分页同步入口拒绝空账号的错误。
	if _, pageCookieErr := NewSyncService(&syncRepositoryFake{}).SyncPage(context.Background(), SyncQuery{UserID: 1}); !errors.Is(pageCookieErr, ErrSyncInvalidCookie) {
		t.Fatalf("page invalid cookie error=%v", pageCookieErr)
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

// TestSyncServicePropagatesOwnershipAndRepositoryErrors 验证账号归属查询、同步仓储和 nil 服务错误路径。
func TestSyncServicePropagatesOwnershipAndRepositoryErrors(t *testing.T) {
	// wantErr 是同步基础设施返回的确定性错误。
	wantErr := errors.New("sync backend failed")
	// ownershipFailure 是账号归属查询失败的同步仓储替身。
	ownershipFailure := &syncRepositoryFake{ownershipErr: wantErr}
	// ownershipErr 保存账号归属查询返回的错误。
	if _, ownershipErr := NewSyncService(ownershipFailure).SyncPage(context.Background(), SyncQuery{UserID: 1, CookieID: "account"}); !errors.Is(ownershipErr, wantErr) {
		t.Fatalf("ownership error=%v", ownershipErr)
	}
	// allOwnershipErr 保存全量同步入口的账号归属查询错误。
	if _, allOwnershipErr := NewSyncService(ownershipFailure).SyncAll(context.Background(), SyncQuery{UserID: 1, CookieID: "account"}); !errors.Is(allOwnershipErr, wantErr) {
		t.Fatalf("all ownership error=%v", allOwnershipErr)
	}
	// syncFailure 是具体同步操作失败的仓储替身。
	syncFailure := &syncRepositoryFake{owned: true, err: wantErr}
	// pageErr 保存分页同步端口返回的错误。
	if _, pageErr := NewSyncService(syncFailure).SyncPage(context.Background(), SyncQuery{UserID: 1, CookieID: "account"}); !errors.Is(pageErr, wantErr) {
		t.Fatalf("page error=%v", pageErr)
	}
	// allErr 保存全量同步端口返回的错误。
	if _, allErr := NewSyncService(syncFailure).SyncAll(context.Background(), SyncQuery{UserID: 1, CookieID: "account"}); !errors.Is(allErr, wantErr) {
		t.Fatalf("all error=%v", allErr)
	}
	// nilService 表示未初始化的商品同步服务指针。
	var nilService *SyncService
	// nilAllErr 保存未初始化服务执行全量同步时的错误。
	if _, nilAllErr := nilService.SyncAll(context.Background(), SyncQuery{UserID: 1, CookieID: "account"}); nilAllErr == nil {
		t.Fatal("nil SyncAll service should fail")
	}
	// nilPageErr 保存未初始化服务执行分页同步时的错误。
	if _, nilPageErr := nilService.SyncPage(context.Background(), SyncQuery{UserID: 1, CookieID: "account"}); nilPageErr == nil {
		t.Fatal("nil SyncPage service should fail")
	}
	// missingRepositoryService 是字段未装配仓储的商品同步服务。
	missingRepositoryService := NewSyncService(nil)
	// missingRepositoryAllErr、missingRepositoryPageErr 保存缺少仓储时两个同步入口的错误。
	if _, missingRepositoryAllErr := missingRepositoryService.SyncAll(context.Background(), SyncQuery{UserID: 1, CookieID: "account"}); missingRepositoryAllErr == nil {
		t.Fatal("缺少仓储的 SyncAll 不应成功")
	}
	// missingRepositoryPageErr 保存缺少仓储时分页同步的错误。
	if _, missingRepositoryPageErr := missingRepositoryService.SyncPage(context.Background(), SyncQuery{UserID: 1, CookieID: "account"}); missingRepositoryPageErr == nil {
		t.Fatal("缺少仓储的 SyncPage 不应成功")
	}
}

// TestSyncServiceRejectsUnownedAccount 验证应用服务在读取平台凭证前拒绝跨用户账号。
func TestSyncServiceRejectsUnownedAccount(t *testing.T) {
	// err 保存跨用户账号应返回的归属错误。
	_, err := NewSyncService(&syncRepositoryFake{owned: false}).SyncPage(context.Background(), SyncQuery{UserID: 1, CookieID: "other"})
	if !errors.Is(err, ErrSyncNotOwned) {
		t.Fatalf("err=%v want=%v", err, ErrSyncNotOwned)
	}
	// allErr 保存全量同步拒绝未授权账号的错误。
	_, allErr := NewSyncService(&syncRepositoryFake{owned: false}).SyncAll(context.Background(), SyncQuery{UserID: 1, CookieID: "other"})
	if !errors.Is(allErr, ErrSyncNotOwned) {
		t.Fatalf("allErr=%v want=%v", allErr, ErrSyncNotOwned)
	}
}

// TestSyncErrorNilAndUnwrap 验证同步阶段错误在 nil、底层错误和无底层错误时的脱敏文本与错误链语义。
func TestSyncErrorNilAndUnwrap(t *testing.T) {
	// var nilError 表示未初始化的同步错误指针。
	var nilError *SyncError
	if nilError.Error() != "商品同步失败" || nilError.Unwrap() != nil {
		t.Fatalf("nil sync error text=%q unwrap=%v", nilError.Error(), nilError.Unwrap())
	}
	// wantErr 是同步错误需要保留的底层错误。
	wantErr := errors.New("底层错误")
	// syncError 是包含阶段和底层错误的同步错误。
	syncError := &SyncError{Kind: SyncErrorCredential, Err: wantErr}
	if !errors.Is(syncError, wantErr) || syncError.Error() != wantErr.Error() {
		t.Fatalf("sync error=%v unwrap=%v", syncError, syncError.Unwrap())
	}
	// emptyError 是没有底层错误的同步错误。
	emptyError := &SyncError{}
	if emptyError.Error() != "商品同步失败" || emptyError.Unwrap() != nil {
		t.Fatalf("empty sync error text=%q unwrap=%v", emptyError.Error(), emptyError.Unwrap())
	}
}
