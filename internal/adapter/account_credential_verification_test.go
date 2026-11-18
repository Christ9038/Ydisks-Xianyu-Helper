package adapter

import (
	"context"
	"errors"
	"testing"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/xianyu/cookierefresh"
)

// credentialRepositoryMinimalFake 只实现验证适配器用到的两个方法；嵌入的接口为空，
// 调用其他方法会直接 panic，从而保证适配器不会越权读取别的凭证能力。
type credentialRepositoryMinimalFake struct {
	accountapp.CredentialRepository
	// detail 是 LoadPlatformDetail 返回的凭证窄视图。
	detail *accountapp.CredentialDetail
	// loadErr 是 LoadPlatformDetail 返回的错误。
	loadErr error
	// locks、unlocks 记录凭证锁的获取与释放次数。
	locks, unlocks int
}

// LockCredentials 记录加锁并返回记录解锁的函数。
func (f *credentialRepositoryMinimalFake) LockCredentials(string) func() {
	f.locks++
	return func() { f.unlocks++ }
}

// LoadPlatformDetail 返回预设的凭证窄视图或错误。
func (f *credentialRepositoryMinimalFake) LoadPlatformDetail(context.Context, string) (*accountapp.CredentialDetail, error) {
	return f.detail, f.loadErr
}

// verificationStarterFake 记录收到的参数并返回预设结果；同时校验调用发生时凭证锁已释放。
type verificationStarterFake struct {
	// repository 用于在调用发生时检查凭证锁是否已经释放。
	repository *credentialRepositoryMinimalFake
	// sessionID、err 是预设返回值。
	sessionID string
	err       error
	// gotCookie、gotURL、gotSnapshot 记录最近一次调用参数。
	gotCookie, gotURL string
	gotSnapshot       []cookierefresh.BrowserCookie
	// lockHeldDuringCall 表示调用发生时凭证锁仍未释放（不允许）。
	lockHeldDuringCall bool
	// calls 记录调用次数。
	calls int
}

// StartCredentialVerification 记录参数并返回预设结果。
func (f *verificationStarterFake) StartCredentialVerification(_ context.Context, cookieHeader string, snapshot []cookierefresh.BrowserCookie, verificationURL string) (string, error) {
	f.calls++
	f.gotCookie, f.gotURL, f.gotSnapshot = cookieHeader, verificationURL, snapshot
	f.lockHeldDuringCall = f.repository != nil && f.repository.locks != f.repository.unlocks
	return f.sessionID, f.err
}

// TestNewCredentialVerificationAdapterValidatesDependencies 验证构造期校验必需依赖；t 管理断言。
func TestNewCredentialVerificationAdapterValidatesDependencies(t *testing.T) {
	// err 是 NewCredentialVerificationAdapter 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, err := NewCredentialVerificationAdapter(nil, &verificationStarterFake{}); err == nil {
		t.Fatal("缺少 repository 应报错")
	}
	// err 是 NewCredentialVerificationAdapter 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, err := NewCredentialVerificationAdapter(&credentialRepositoryMinimalFake{}, nil); err == nil {
		t.Fatal("缺少 starter 应报错")
	}
	// nilAdapter 是未初始化的适配器，调用应返回错误。
	var nilAdapter *CredentialVerificationAdapter
	// err 是 nilAdapter.StartCredentialVerification 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, err := nilAdapter.StartCredentialVerification(context.Background(), "a", "u"); err == nil {
		t.Fatal("空适配器应返回错误")
	}
}

// TestCredentialVerificationAdapterPassesLatestCredentialsOutsideLock 验证适配器读取最新凭证、
// 有完整快照时传递快照、在锁外创建会话；t 管理断言。
func TestCredentialVerificationAdapterPassesLatestCredentialsOutsideLock(t *testing.T) {
	// snapshot 是账号完整 Cookie 快照的伪造样本，并编码进加密 metadata 的明文形态。
	snapshot := []cookierefresh.BrowserCookie{{Name: "unb", Value: "fixture", Domain: ".goofish.com", Path: "/"}}
	// repository 保存 该表达式 的结果，供后续分支和断言使用。
	repository := &credentialRepositoryMinimalFake{detail: &accountapp.CredentialDetail{
		ID: "a1", Value: "unb=fixture; _m_h5_tk=rotated_1", MetadataJSON: cookierefresh.MetadataWithSnapshot("", snapshot),
	}}
	// starter 保存 该表达式 的结果，供后续分支和断言使用。
	starter := &verificationStarterFake{repository: repository, sessionID: "session-1"}
	// adapter 保存 NewCredentialVerificationAdapter 的结果，供后续分支和断言使用；err 是 NewCredentialVerificationAdapter 返回的失败原因，用于断言成功或被拒绝的路径。
	adapter, err := NewCredentialVerificationAdapter(repository, starter)
	if err != nil {
		t.Fatal(err)
	}
	// sessionID 保存 adapter.StartCredentialVerification 的结果，供后续分支和断言使用；startErr 是 adapter.StartCredentialVerification 返回的失败原因，用于断言成功或被拒绝的路径。
	sessionID, startErr := adapter.StartCredentialVerification(context.Background(), "a1", "https://x.invalid/iv/1")
	if startErr != nil || sessionID != "session-1" {
		t.Fatalf("session=%q err=%v", sessionID, startErr)
	}
	if starter.gotCookie != "unb=fixture; _m_h5_tk=rotated_1" || starter.gotURL != "https://x.invalid/iv/1" {
		t.Fatalf("应传递账号最新凭证与验证地址: cookie=%q url=%q", starter.gotCookie, starter.gotURL)
	}
	if len(starter.gotSnapshot) != 1 || starter.gotSnapshot[0].Name != "unb" {
		t.Fatalf("存在完整快照时应传递快照: %+v", starter.gotSnapshot)
	}
	if starter.lockHeldDuringCall || repository.locks != 1 || repository.unlocks != 1 {
		t.Fatalf("会话创建必须在凭证锁释放之后: held=%v locks=%d unlocks=%d", starter.lockHeldDuringCall, repository.locks, repository.unlocks)
	}
}

// TestCredentialVerificationAdapterFallsBackToFlatCookies 验证没有完整快照时传 nil，由会话改用扁平 Cookie；t 管理断言。
func TestCredentialVerificationAdapterFallsBackToFlatCookies(t *testing.T) {
	// repository 保存 该表达式 的结果，供后续分支和断言使用。
	repository := &credentialRepositoryMinimalFake{detail: &accountapp.CredentialDetail{ID: "a1", Value: "unb=1", MetadataJSON: ""}}
	// starter 保存 该表达式 的结果，供后续分支和断言使用。
	starter := &verificationStarterFake{repository: repository, sessionID: "s"}
	// adapter 保存 NewCredentialVerificationAdapter 的结果，供后续分支和断言使用。
	adapter, _ := NewCredentialVerificationAdapter(repository, starter)
	// err 是 adapter.StartCredentialVerification 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, err := adapter.StartCredentialVerification(context.Background(), "a1", "https://x.invalid/iv/1"); err != nil {
		t.Fatal(err)
	}
	if starter.gotSnapshot != nil {
		t.Fatalf("没有完整快照时必须传 nil: %+v", starter.gotSnapshot)
	}
}

// TestCredentialVerificationAdapterErrors 验证读取失败、账号缺失与会话创建失败都被正确透传，且读取失败不创建会话；t 管理断言。
func TestCredentialVerificationAdapterErrors(t *testing.T) {
	// loadErr 是凭证读取失败的哨兵错误。
	loadErr := errors.New("读取失败")
	// repository 保存 该表达式 的结果，供后续分支和断言使用。
	repository := &credentialRepositoryMinimalFake{loadErr: loadErr}
	// starter 保存 该表达式 的结果，供后续分支和断言使用。
	starter := &verificationStarterFake{repository: repository}
	// adapter 保存 NewCredentialVerificationAdapter 的结果，供后续分支和断言使用。
	adapter, _ := NewCredentialVerificationAdapter(repository, starter)
	// err 是 adapter.StartCredentialVerification 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, err := adapter.StartCredentialVerification(context.Background(), "a1", "u"); !errors.Is(err, loadErr) || starter.calls != 0 || repository.locks != repository.unlocks {
		t.Fatalf("读取失败应透传、不创建会话且释放锁: err=%v calls=%d", err, starter.calls)
	}
	// 账号不存在：详情为空。
	repository.loadErr, repository.detail = nil, nil
	// err 是 adapter.StartCredentialVerification 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, err := adapter.StartCredentialVerification(context.Background(), "a1", "u"); !errors.Is(err, accountapp.ErrNotFound) {
		t.Fatalf("详情为空应返回 ErrNotFound: %v", err)
	}
	// 会话创建失败：错误透传。
	startErr := errors.New("创建会话失败")
	repository.detail = &accountapp.CredentialDetail{Value: "unb=1"}
	starter.err = startErr
	// err 是 adapter.StartCredentialVerification 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, err := adapter.StartCredentialVerification(context.Background(), "a1", "u"); !errors.Is(err, startErr) {
		t.Fatalf("创建失败应透传: %v", err)
	}
}
