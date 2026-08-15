package account

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakeQRLoginRepository 是扫码登录应用服务测试使用的凭证端口替身。
type fakeQRLoginRepository struct {
	// account 保存待返回的非敏感账号归属。
	account QRLoginAccount
	// findErr 保存账号存在性查询错误。
	findErr error
	// createErr 保存新账号凭证创建错误。
	createErr error
	// flatErr 保存扁平 Cookie 更新错误。
	flatErr error
	// snapshotErr 保存 Cookie 快照更新错误。
	snapshotErr error
	// clearErr 保存旧 Token 清理错误。
	clearErr error
	// createdCookies 保存新账号端口收到的 Cookie，用于验证敏感值只在端口边界出现。
	createdCookies string
	// updatedCookies 保存已有账号端口收到的 Cookie，用于验证更新路径。
	updatedCookies string
	// updatedSnapshot 保存端口收到的完整 Cookie 快照。
	updatedSnapshot []CookieSnapshot
	// lockCount 记录凭证锁获取次数。
	lockCount int
	// clearCount 记录旧 Token 清理次数。
	clearCount int
}

// LockCredentials 记录测试服务获取凭证锁，并返回无副作用的解锁函数。
func (r *fakeQRLoginRepository) LockCredentials(string) func() {
	r.lockCount++
	return func() {}
}

// FindAccount 返回预置账号归属或存在性错误。
func (r *fakeQRLoginRepository) FindAccount(context.Context, string) (QRLoginAccount, error) {
	return r.account, r.findErr
}

// CreateCookieOwned 记录新账号 Cookie 写入请求。
func (r *fakeQRLoginRepository) CreateCookieOwned(_ context.Context, _ string, cookies string, _ int64) error {
	r.createdCookies = cookies
	return r.createErr
}

// UpdateFlatCookieOwned 记录已有账号的扁平 Cookie 更新。
func (r *fakeQRLoginRepository) UpdateFlatCookieOwned(_ context.Context, _ string, cookies string) error {
	r.updatedCookies = cookies
	return r.flatErr
}

// UpdateCookieSnapshotOwned 记录带完整快照的 Cookie 更新。
func (r *fakeQRLoginRepository) UpdateCookieSnapshotOwned(_ context.Context, _ string, cookies string, snapshot []CookieSnapshot) error {
	r.updatedCookies = cookies
	r.updatedSnapshot = snapshot
	return r.snapshotErr
}

// ClearTokens 记录旧连接 Token 清理请求。
func (r *fakeQRLoginRepository) ClearTokens(context.Context, string) error {
	r.clearCount++
	return r.clearErr
}

// fakeQRLoginLifecycle 是扫码登录后续动作测试用的生命周期端口替身。
type fakeQRLoginLifecycle struct {
	// calls 记录生命周期端口调用次数。
	calls int
	// userID 保存生命周期端口收到的用户标识。
	userID int64
	// accountID 保存生命周期端口收到的账号标识。
	accountID string
	// cleanupErr 保存生命周期端口收到的旧 Token 清理失败。
	cleanupErr error
}

// AfterSuccessfulQRLogin 记录扫码成功后的生命周期调用参数。
func (l *fakeQRLoginLifecycle) AfterSuccessfulQRLogin(_ context.Context, userID int64, accountID string) {
	l.calls++
	l.userID = userID
	l.accountID = accountID
}

// ReportQRLoginCleanupFailure 记录旧 Token 清理失败，验证该错误不阻断登录成功结果。
func (l *fakeQRLoginLifecycle) ReportQRLoginCleanupFailure(_ context.Context, _ string, err error) {
	l.cleanupErr = err
}

// TestNewQRLoginServiceRequiresPorts 验证扫码登录应用服务拒绝缺失依赖。
func TestNewQRLoginServiceRequiresPorts(t *testing.T) {
	// repository 是满足凭证端口的测试替身。
	repository := &fakeQRLoginRepository{}
	// lifecycle 是满足登录后续动作端口的测试替身。
	lifecycle := &fakeQRLoginLifecycle{}
	// cases 保存构造阶段必须拒绝的依赖缺失场景。
	cases := []struct {
		// name 是当前构造失败场景名称。
		name string
		// repository 是当前场景的凭证端口。
		repository QRLoginRepository
		// lifecycle 是当前场景的生命周期端口。
		lifecycle QRLoginLifecycle
	}{
		{name: "缺少凭证端口", lifecycle: lifecycle},
		{name: "缺少生命周期端口", repository: repository},
	}
	// testCase 表示当前遍历的构造失败场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// err 保存当前场景的构造结果错误。
			_, err := NewQRLoginService(testCase.repository, testCase.lifecycle)
			if err == nil {
				t.Fatal("缺少必需端口时应构造失败")
			}
		})
	}
}

// TestQRLoginPersistSuccessCreatesAccountAndHidesCookie 验证新账号成功写入及敏感值不进入结果。
func TestQRLoginPersistSuccessCreatesAccountAndHidesCookie(t *testing.T) {
	// repository 返回账号不存在，模拟首次扫码登录。
	repository := &fakeQRLoginRepository{findErr: ErrNotFound}
	// lifecycle 记录成功后的审计与运行时同步调用。
	lifecycle := &fakeQRLoginLifecycle{}
	// service 是待验证的扫码登录应用服务。
	service, err := NewQRLoginService(repository, lifecycle)
	if err != nil {
		t.Fatalf("构造扫码登录服务失败: %v", err)
	}
	// input 携带平台扫码成功结果；Cookie 只允许进入凭证端口。
	input := QRLoginInput{
		UserID: 7, ScannedAccountID: "unb-7", Cookies: "unb=unb-7; token=secret-cookie",
		Snapshot: []CookieSnapshot{{Name: "unb", Value: "unb-7", Domain: ".example.com", Path: "/"}},
	}
	// result 保存应用服务返回的非敏感成功结果。
	result, err := service.PersistSuccess(context.Background(), input)
	if err != nil {
		t.Fatalf("扫码登录持久化失败: %v", err)
	}
	if result.AccountID != "unb-7" || !result.IsNew || lifecycle.calls != 1 {
		t.Fatalf("成功结果异常: result=%+v lifecycle=%+v", result, lifecycle)
	}
	if repository.createdCookies != input.Cookies || len(repository.updatedSnapshot) != 1 || repository.clearCount != 1 {
		t.Fatalf("凭证端口调用异常: %+v", repository)
	}
	// encodedResult 是 HTTP 层可能序列化的应用结果，用于确认不会泄露 Cookie 明文。
	encodedResult, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		t.Fatalf("序列化结果失败: %v", marshalErr)
	}
	if strings.Contains(string(encodedResult), "secret-cookie") {
		t.Fatalf("应用结果泄露 Cookie: %s", encodedResult)
	}
}

// TestQRLoginPersistSuccessUpdatesOwnedAccount 验证已有账号归属通过后更新扁平 Cookie。
func TestQRLoginPersistSuccessUpdatesOwnedAccount(t *testing.T) {
	// repository 返回当前用户拥有的账号，且没有完整快照。
	repository := &fakeQRLoginRepository{account: QRLoginAccount{ID: "unb-7", UserID: 7}}
	// lifecycle 记录成功后的后续动作。
	lifecycle := &fakeQRLoginLifecycle{}
	// service 是待验证的扫码登录应用服务。
	service, err := NewQRLoginService(repository, lifecycle)
	if err != nil {
		t.Fatalf("构造扫码登录服务失败: %v", err)
	}
	// result 保存已有账号更新后的非敏感结果。
	result, err := service.PersistSuccess(context.Background(), QRLoginInput{
		UserID: 7, ScannedAccountID: "unb-7", Cookies: "unb=unb-7; token=rotated",
	})
	if err != nil || result.AccountID != "unb-7" || result.IsNew {
		t.Fatalf("已有账号更新异常: result=%+v err=%v", result, err)
	}
	if repository.updatedCookies == "" || lifecycle.accountID != "unb-7" {
		t.Fatalf("已有账号更新端口未调用: %+v %+v", repository, lifecycle)
	}
}

// TestQRLoginPersistRejectsOwnershipAndMismatch 验证跨用户、目标不一致和不完整结果均不会写入凭证。
func TestQRLoginPersistRejectsOwnershipAndMismatch(t *testing.T) {
	// ownershipRepository 返回其他用户拥有的账号。
	ownershipRepository := &fakeQRLoginRepository{account: QRLoginAccount{ID: "unb-7", UserID: 8}}
	// lifecycle 是不应被失败流程调用的生命周期替身。
	lifecycle := &fakeQRLoginLifecycle{}
	// ownershipService 是待验证跨用户拒绝服务。
	ownershipService, err := NewQRLoginService(ownershipRepository, lifecycle)
	if err != nil {
		t.Fatalf("构造归属服务失败: %v", err)
	}
	// _, ownershipErr 保存跨用户写入结果。
	_, ownershipErr := ownershipService.PersistSuccess(context.Background(), QRLoginInput{UserID: 7, ScannedAccountID: "unb-7", Cookies: "secret"})
	if !errors.Is(ownershipErr, ErrForbidden) || lifecycle.calls != 0 {
		t.Fatalf("跨用户账号未被拒绝: err=%v lifecycle=%d", ownershipErr, lifecycle.calls)
	}

	// mismatchService 是待验证扫码账号与目标账号不一致的服务。
	mismatchService, err := NewQRLoginService(&fakeQRLoginRepository{}, lifecycle)
	if err != nil {
		t.Fatalf("构造不一致服务失败: %v", err)
	}
	// _, mismatchErr 保存账号不一致结果。
	_, mismatchErr := mismatchService.PersistSuccess(context.Background(), QRLoginInput{UserID: 7, ScannedAccountID: "unb-7", TargetAccountID: "unb-8", Cookies: "secret"})
	if !errors.Is(mismatchErr, ErrQRLoginAccountMismatch) {
		t.Fatalf("账号不一致未被拒绝: %v", mismatchErr)
	}

	// incompleteService 是待验证缺少 Cookie 的服务。
	incompleteService, err := NewQRLoginService(&fakeQRLoginRepository{}, lifecycle)
	if err != nil {
		t.Fatalf("构造不完整结果服务失败: %v", err)
	}
	// _, incompleteErr 保存不完整结果错误。
	_, incompleteErr := incompleteService.PersistSuccess(context.Background(), QRLoginInput{UserID: 7, ScannedAccountID: "unb-7"})
	if !errors.Is(incompleteErr, ErrQRLoginIncomplete) {
		t.Fatalf("不完整扫码结果未被拒绝: %v", incompleteErr)
	}
}

// TestQRLoginPersistPropagatesWriteFailure 验证凭证写入失败时不触发生命周期动作。
func TestQRLoginPersistPropagatesWriteFailure(t *testing.T) {
	// writeErr 是测试凭证端口返回的持久化错误。
	writeErr := errors.New("凭证存储不可用")
	// repository 模拟新账号写入失败。
	repository := &fakeQRLoginRepository{findErr: ErrNotFound, createErr: writeErr}
	// lifecycle 记录不应发生的成功后续动作。
	lifecycle := &fakeQRLoginLifecycle{}
	// service 是待验证的扫码登录服务。
	service, err := NewQRLoginService(repository, lifecycle)
	if err != nil {
		t.Fatalf("构造写入失败服务失败: %v", err)
	}
	// _, persistErr 保存凭证端口错误。
	_, persistErr := service.PersistSuccess(context.Background(), QRLoginInput{UserID: 7, ScannedAccountID: "unb-7", Cookies: "secret"})
	if !errors.Is(persistErr, writeErr) || lifecycle.calls != 0 {
		t.Fatalf("凭证写入错误未正确传播: err=%v lifecycle=%d", persistErr, lifecycle.calls)
	}
}

// TestQRLoginPersistIgnoresTokenCleanupFailure 验证凭证已保存时旧 Token 清理失败不会伪装成登录失败。
func TestQRLoginPersistIgnoresTokenCleanupFailure(t *testing.T) {
	// repository 模拟已有账号更新成功但旧 Token 清理失败。
	repository := &fakeQRLoginRepository{
		account:  QRLoginAccount{ID: "unb-7", UserID: 7},
		clearErr: errors.New("旧 Token 清理失败"),
	}
	// lifecycle 记录凭证保存成功后的后续动作。
	lifecycle := &fakeQRLoginLifecycle{}
	// service 是待验证的扫码登录服务。
	service, err := NewQRLoginService(repository, lifecycle)
	if err != nil {
		t.Fatalf("构造 Token 清理失败服务失败: %v", err)
	}
	// result、persistErr 保存凭证成功与否以及应用错误。
	result, persistErr := service.PersistSuccess(context.Background(), QRLoginInput{UserID: 7, ScannedAccountID: "unb-7", Cookies: "secret"})
	if persistErr != nil || result.AccountID != "unb-7" || lifecycle.calls != 1 {
		t.Fatalf("Token 清理失败不应阻断登录: result=%+v err=%v lifecycle=%d", result, persistErr, lifecycle.calls)
	}
}
