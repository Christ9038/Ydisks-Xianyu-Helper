package defaultreply

import (
	"context"
	"errors"
	"testing"
)

// fakeRepository 是默认回复服务测试使用的最小内存 Port。
type fakeRepository struct {
	// ownership 保存模拟的账号所有权结果。
	ownership AccountOwnership
	// ownershipErr 保存模拟的所有权查询错误。
	ownershipErr error
	// reply 保存模拟的默认回复配置。
	reply Reply
	// getErr 保存模拟的默认回复查询错误。
	getErr error
	// listed 保存模拟的默认回复列表。
	listed []Summary
	// listErr 保存模拟的默认回复列表查询错误。
	listErr error
	// mutationErr 保存模拟的写入、删除或清理错误。
	mutationErr error
	// mutationCookieID 保存最近一次写操作的账号标识。
	mutationCookieID string
}

// CheckOwnership 返回测试预设的账号所有权结果。
func (f *fakeRepository) CheckOwnership(context.Context, int64, string) (AccountOwnership, error) {
	return f.ownership, f.ownershipErr
}

// Get 返回测试预设的默认回复配置或查询错误。
func (f *fakeRepository) Get(context.Context, string) (Reply, error) {
	return f.reply, f.getErr
}

// Upsert 保存最近一次写入的账号标识并返回预设错误。
func (f *fakeRepository) Upsert(_ context.Context, cookieID string, _ Reply) error {
	f.mutationCookieID = cookieID
	return f.mutationErr
}

// ListForUser 返回测试预设的默认回复列表或查询错误。
func (f *fakeRepository) ListForUser(context.Context, int64) ([]Summary, error) {
	return f.listed, f.listErr
}

// Delete 保存最近一次删除的账号标识并返回预设错误。
func (f *fakeRepository) Delete(_ context.Context, cookieID string) error {
	f.mutationCookieID = cookieID
	return f.mutationErr
}

// ClearRecords 保存最近一次清理的账号标识并返回预设错误。
func (f *fakeRepository) ClearRecords(_ context.Context, cookieID string) error {
	f.mutationCookieID = cookieID
	return f.mutationErr
}

// TestServiceOwnershipAndConfigErrors 验证服务区分跨用户、账号缺失和配置缺失。
func TestServiceOwnershipAndConfigErrors(t *testing.T) {
	// ctx 是本测试所有应用调用使用的非取消上下文。
	ctx := context.Background()
	// accountMissing 是模拟账号不存在的持久化错误。
	accountMissing := errors.New("账号查询失败")
	// cases 是覆盖所有权和配置错误语义的表驱动场景。
	cases := []struct {
		// name 是场景名称。
		name string
		// repository 是当前场景使用的内存 Port。
		repository *fakeRepository
		// wantErr 是期望返回的稳定应用错误。
		wantErr error
	}{
		{name: "cross-user", repository: &fakeRepository{ownership: AccountOwnership{OwnerID: 2}}, wantErr: ErrForbidden},
		{name: "missing-account", repository: &fakeRepository{ownershipErr: ErrAccountNotFound}, wantErr: ErrAccountNotFound},
		{name: "missing-config", repository: &fakeRepository{ownership: AccountOwnership{OwnerID: 1}, getErr: ErrConfigNotFound}, wantErr: ErrConfigNotFound},
		{name: "storage-error", repository: &fakeRepository{ownership: AccountOwnership{OwnerID: 1}, getErr: accountMissing}, wantErr: accountMissing},
	}
	// item 是当前遍历的错误语义场景。
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			// service 是使用当前场景内存 Port 的应用服务。
			service := NewService(item.repository)
			// _, err 保存默认回复读取结果及错误。
			_, err := service.Get(ctx, 1, "account-1")
			if !errors.Is(err, item.wantErr) {
				t.Fatalf("错误=%v，期望=%v", err, item.wantErr)
			}
		})
	}
}

// TestServiceMutationsRequireOwnership 验证所有写操作均先执行账号归属校验。
func TestServiceMutationsRequireOwnership(t *testing.T) {
	// ctx 是本测试所有应用调用使用的非取消上下文。
	ctx := context.Background()
	// repository 是账号归属当前用户的内存 Port。
	repository := &fakeRepository{ownership: AccountOwnership{OwnerID: 7}}
	// service 是使用内存 Port 的默认回复应用服务。
	service := NewService(repository)
	// reply 是待保存的默认回复应用模型。
	reply := Reply{Enabled: true, ReplyContent: "你好", ReplyOnce: true}
	// upsertErr 表示保存默认回复的执行结果。
	upsertErr := service.Upsert(ctx, 7, "account-7", reply)
	if upsertErr != nil || repository.mutationCookieID != "account-7" {
		t.Fatalf("保存结果 err=%v cookie=%q", upsertErr, repository.mutationCookieID)
	}
	// deleteErr 表示删除默认回复的执行结果。
	deleteErr := service.Delete(ctx, 7, "account-7")
	if deleteErr != nil || repository.mutationCookieID != "account-7" {
		t.Fatalf("删除结果 err=%v cookie=%q", deleteErr, repository.mutationCookieID)
	}
	// clearErr 表示清空投递记录的执行结果。
	clearErr := service.ClearRecords(ctx, 7, "account-7")
	if clearErr != nil || repository.mutationCookieID != "account-7" {
		t.Fatalf("清理结果 err=%v cookie=%q", clearErr, repository.mutationCookieID)
	}
	// repository.ownership 保存跨用户归属结果，验证写操作不会继续调用 Port。
	repository.ownership = AccountOwnership{OwnerID: 9}
	// forbiddenErr 表示跨用户保存返回的错误。
	forbiddenErr := service.Upsert(ctx, 7, "account-9", reply)
	if !errors.Is(forbiddenErr, ErrForbidden) {
		t.Fatalf("跨用户保存错误=%v，期望=%v", forbiddenErr, ErrForbidden)
	}
}

// TestServiceInputValidation 验证无效服务依赖、用户和账号标识会被拒绝。
func TestServiceInputValidation(t *testing.T) {
	// ctx 是本测试使用的非取消上下文。
	ctx := context.Background()
	// invalidCases 是需要在 Port 前拦截的输入场景。
	invalidCases := []struct {
		// name 是场景名称。
		name string
		// service 是待验证的应用服务实例。
		service *Service
		// userID 是当前场景使用的用户身份。
		userID int64
		// cookieID 是当前场景使用的账号标识。
		cookieID string
		// wantErr 是期望的稳定校验错误。
		wantErr error
	}{
		{name: "nil-service", service: NewService(nil), userID: 1, cookieID: "a", wantErr: errors.New("默认回复 repository 未初始化")},
		{name: "invalid-user", service: NewService(&fakeRepository{}), userID: 0, cookieID: "a", wantErr: ErrInvalidUser},
		{name: "invalid-account", service: NewService(&fakeRepository{}), userID: 1, cookieID: "", wantErr: ErrInvalidCookieID},
	}
	// item 是当前遍历的输入校验场景。
	for _, item := range invalidCases {
		// _, err 保存当前场景的读取错误。
		_, err := item.service.Get(ctx, item.userID, item.cookieID)
		if item.name == "nil-service" {
			if err == nil || err.Error() != item.wantErr.Error() {
				t.Fatalf("场景=%s 错误=%v", item.name, err)
			}
			continue
		}
		if !errors.Is(err, item.wantErr) {
			t.Fatalf("场景=%s 错误=%v，期望=%v", item.name, err, item.wantErr)
		}
	}
}

// TestServiceListReturnsApplicationSummaries 验证列表结果保持应用模型而不是数据库模型。
func TestServiceListReturnsApplicationSummaries(t *testing.T) {
	// ctx 是本测试使用的非取消上下文。
	ctx := context.Background()
	// repository 是返回单条默认回复摘要的内存 Port。
	repository := &fakeRepository{ownership: AccountOwnership{OwnerID: 3}, listed: []Summary{{CookieID: "account-3", Reply: Reply{Enabled: true, ReplyContent: "欢迎"}}}}
	// result、err 保存列表查询结果及错误。
	result, err := NewService(repository).List(ctx, 3)
	if err != nil || len(result) != 1 || result[0].Reply.ReplyContent != "欢迎" {
		t.Fatalf("列表结果=%+v err=%v", result, err)
	}
}

// TestServiceListAndMutationValidation 验证列表、删除和清理入口共享用户与账号校验。
func TestServiceListAndMutationValidation(t *testing.T) {
	// ctx 是本测试所有服务调用共用的非取消上下文。
	ctx := context.Background()
	// repository 是用于触发输入校验的空内存 Port。
	repository := &fakeRepository{}
	// service 是待验证的默认回复应用服务。
	service := NewService(repository)
	// _, listErr 保存非法用户列表结果。
	_, listErr := service.List(ctx, 0)
	if !errors.Is(listErr, ErrInvalidUser) {
		t.Fatalf("List invalid user error=%v", listErr)
	}
	// deleteErr 保存空账号删除结果。
	deleteErr := service.Delete(ctx, 1, "")
	if !errors.Is(deleteErr, ErrInvalidCookieID) {
		t.Fatalf("Delete invalid account error=%v", deleteErr)
	}
	// clearErr 保存空账号清理结果。
	clearErr := service.ClearRecords(ctx, 1, "")
	if !errors.Is(clearErr, ErrInvalidCookieID) {
		t.Fatalf("ClearRecords invalid account error=%v", clearErr)
	}
}

var _ Repository = (*fakeRepository)(nil)
