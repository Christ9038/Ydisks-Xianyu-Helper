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
}

// QueryLongLogin 返回预设的长登录查询结果。
func (f *longLoginPortFake) QueryLongLogin(context.Context, string) (LongLoginResult, error) {
	f.calls++
	return f.result, nil
}

// SetLongLogin 返回预设的长登录设置结果。
func (f *longLoginPortFake) SetLongLogin(context.Context, string, bool) (LongLoginResult, error) {
	f.calls++
	return f.result, nil
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
}
