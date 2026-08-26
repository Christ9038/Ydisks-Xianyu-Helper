package account

import (
	"context"
	"errors"
	"testing"
)

// longLoginSummaryRepositoryFake 是长登录用例测试使用的非敏感摘要仓储替身。
type longLoginSummaryRepositoryFake struct {
	// summary 保存归属校验成功时返回的账号摘要。
	summary Summary
	// err 保存摘要查询失败时返回的错误。
	err error
}

// GetOwnedSummary 返回预设的账号摘要或归属错误。
func (f longLoginSummaryRepositoryFake) GetOwnedSummary(context.Context, int64, string) (Summary, error) {
	return f.summary, f.err
}

// longLoginPortFake 是长登录用例测试使用的平台端口替身。
type longLoginPortFake struct {
	// result 保存平台请求成功时返回的非敏感状态。
	result LongLoginResult
	// calls 记录平台调用次数，用于确认归属失败不会访问平台。
	calls int
	// queryErr 保存长登录查询错误。
	queryErr error
	// setErr 保存长登录设置错误。
	setErr error
}

// QueryLongLogin 返回预设的长登录查询结果。
func (f *longLoginPortFake) QueryLongLogin(context.Context, string) (LongLoginResult, error) {
	f.calls++
	return f.result, f.queryErr
}

// SetLongLogin 返回预设的长登录设置结果。
func (f *longLoginPortFake) SetLongLogin(context.Context, string, bool) (LongLoginResult, error) {
	f.calls++
	return f.result, f.setErr
}

// TestLongLoginServiceChecksOwnershipAndMapsResult 验证长登录用例先校验归属，再返回脱敏状态。
func TestLongLoginServiceChecksOwnershipAndMapsResult(t *testing.T) {
	// port 保存平台端口替身及调用计数。
	port := &longLoginPortFake{result: LongLoginResult{CanOpenLongLogin: true, Enabled: true}}
	// service、serviceErr 保存长登录应用服务及其装配错误。
	service, serviceErr := NewLongLoginService(longLoginSummaryRepositoryFake{summary: Summary{ID: "account-1"}}, port)
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	// result、callErr 保存长登录查询结果和平台调用错误。
	result, callErr := service.Query(context.Background(), 1, "account-1")
	if callErr != nil || result != (LongLoginResult{CanOpenLongLogin: true, Enabled: true}) || port.calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, callErr, port.calls)
	}
	// queryErr 是平台查询失败时需要透传的错误。
	queryErr := errors.New("长登录查询失败")
	// queryFailureService 是绑定平台查询失败替身的服务。
	queryFailureService, queryFailureServiceErr := NewLongLoginService(longLoginSummaryRepositoryFake{}, &longLoginPortFake{queryErr: queryErr})
	if queryFailureServiceErr != nil {
		t.Fatalf("NewLongLoginService query failure error: %v", queryFailureServiceErr)
	}
	// failedResult、failedErr 保存平台查询失败结果。
	failedResult, failedErr := queryFailureService.Query(context.Background(), 1, "account-1")
	if failedResult != (LongLoginResult{}) || !errors.Is(failedErr, queryErr) {
		t.Fatalf("query failure result=%+v err=%v", failedResult, failedErr)
	}
	// ownershipErr 是查询账号摘要失败时需要透传的归属错误。
	ownershipErr := errors.New("账号归属查询失败")
	// ownershipFailureService 是绑定账号摘要查询失败替身的服务。
	ownershipFailureService, ownershipFailureServiceErr := NewLongLoginService(longLoginSummaryRepositoryFake{err: ownershipErr}, port)
	if ownershipFailureServiceErr != nil {
		t.Fatalf("NewLongLoginService ownership failure error: %v", ownershipFailureServiceErr)
	}
	// ownershipFailureResult、ownershipFailureCallErr 保存归属查询失败结果。
	ownershipFailureResult, ownershipFailureCallErr := ownershipFailureService.Query(context.Background(), 1, "account-1")
	if ownershipFailureResult != (LongLoginResult{}) || !errors.Is(ownershipFailureCallErr, ownershipErr) {
		t.Fatalf("ownership failure result=%+v err=%v", ownershipFailureResult, ownershipFailureCallErr)
	}
}

// TestLongLoginServiceStopsOnOwnershipError 验证账号归属失败时不会调用平台端口。
func TestLongLoginServiceStopsOnOwnershipError(t *testing.T) {
	// port 保存平台端口替身及调用计数。
	port := &longLoginPortFake{}
	// service、serviceErr 保存长登录应用服务及其装配错误。
	service, serviceErr := NewLongLoginService(longLoginSummaryRepositoryFake{err: ErrForbidden}, port)
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	// callErr 保存归属校验返回的应用错误。
	_, callErr := service.Set(context.Background(), 1, "account-1", true)
	if !errors.Is(callErr, ErrForbidden) || port.calls != 0 {
		t.Fatalf("err=%v calls=%d", callErr, port.calls)
	}
	// nilRepositoryService、nilRepositoryErr 保存缺少账号摘要仓储时的构造错误。
	_, nilRepositoryErr := NewLongLoginService(nil, port)
	if nilRepositoryErr == nil {
		t.Fatal("缺少长登录摘要仓储时构造不应成功")
	}
	// nilPortService、nilPortErr 保存缺少平台端口时的构造错误。
	_, nilPortErr := NewLongLoginService(longLoginSummaryRepositoryFake{}, nil)
	if nilPortErr == nil {
		t.Fatal("缺少长登录平台端口时构造不应成功")
	}
}

// TestLongLoginServiceRejectsUninitializedQuery 验证未初始化的长登录服务不会访问任何外部端口。
func TestLongLoginServiceRejectsUninitializedQuery(t *testing.T) {
	// nilService 表示未初始化的长登录服务指针。
	var nilService *LongLoginService
	// queryErr 保存空服务查询返回的装配错误。
	_, queryErr := nilService.Query(context.Background(), 1, "account-1")
	if queryErr == nil {
		t.Fatal("空长登录服务不应查询成功")
	}
}
