package automation

import (
	"context"
	"errors"
	"testing"
)

// fakeAuditRepository 记录收到的查询并返回预设结果，用于验证应用服务的规范化行为。
type fakeAuditRepository struct {
	// got 保存最近一次收到的仓储查询。
	got AuditQuery
	// page 与 err 是预设的返回值。
	page AuditPage
	err  error
}

// ListAuditEvents 记录查询并返回预设结果。
func (f *fakeAuditRepository) ListAuditEvents(_ context.Context, query AuditQuery) (AuditPage, error) {
	f.got = query
	return f.page, f.err
}

// TestAuditServiceListEventsNormalizesQuery 验证分页、空白、时间线正序和用户隔离参数被正确传给仓储。
func TestAuditServiceListEventsNormalizesQuery(t *testing.T) {
	// repo 是被注入的仓储替身，service 是被测应用服务。
	repo := &fakeAuditRepository{page: AuditPage{Total: 3}}
	// service 是被注入仓储替身的被测应用服务。
	service := NewAuditService(repo)

	// page 是默认分页的查询结果。
	page, err := service.ListEvents(context.Background(), 7, AuditListInput{Status: " skipped ", CookieID: " acc ", Search: " o1 ", Page: -3, PageSize: 0})
	if err != nil || page.Page != 1 || page.PageSize != DefaultAuditPageSize || page.Total != 3 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if repo.got.UserID != 7 || repo.got.Status != "skipped" || repo.got.CookieID != "acc" || repo.got.Search != "o1" || repo.got.Offset != 0 || repo.got.Ascending {
		t.Fatalf("query=%+v", repo.got)
	}

	// 超限分页取最大值，偏移按页码计算；指定运行时按正序返回时间线。
	page, err = service.ListEvents(context.Background(), 7, AuditListInput{Page: 3, PageSize: 9999, RunID: 5})
	if err != nil || page.PageSize != MaxAuditPageSize || repo.got.Offset != 2*MaxAuditPageSize || !repo.got.Ascending || repo.got.RunID != 5 {
		t.Fatalf("page=%+v query=%+v err=%v", page, repo.got, err)
	}
}

// TestAuditServiceListEventsRejectsInvalidInput 验证空服务、非法用户和未知状态被拒绝，仓储错误原样返回。
func TestAuditServiceListEventsRejectsInvalidInput(t *testing.T) {
	// repo 是仓储替身。
	repo := &fakeAuditRepository{}
	// service 是被注入仓储替身的被测应用服务。
	service := NewAuditService(repo)
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.ListEvents(context.Background(), 0, AuditListInput{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("非法用户应返回 ErrInvalidInput, got %v", err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.ListEvents(context.Background(), 1, AuditListInput{Status: "bogus"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("未知状态应返回 ErrInvalidInput, got %v", err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := NewAuditService(nil).ListEvents(context.Background(), 1, AuditListInput{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("空端口应返回 ErrInvalidInput, got %v", err)
	}
	// nilService 是空指针服务，用于验证零值保护。
	var nilService *AuditService
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := nilService.ListEvents(context.Background(), 1, AuditListInput{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("空服务应返回 ErrInvalidInput, got %v", err)
	}
	// repoErr 是仓储失败错误，应用服务不应吞掉。
	repoErr := errors.New("db down")
	repo.err = repoErr
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.ListEvents(context.Background(), 1, AuditListInput{}); !errors.Is(err, repoErr) {
		t.Fatalf("仓储错误应原样返回, got %v", err)
	}
}
