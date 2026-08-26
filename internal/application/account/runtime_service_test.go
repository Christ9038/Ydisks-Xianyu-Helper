package account

import (
	"context"
	"errors"
	"testing"
	"time"
)

// runtimePortFake 记录账号运行时端口调用并返回预置结果。
type runtimePortFake struct {
	// updateErr 是 Cookie 同步操作的预置错误。
	updateErr error
	// restartErr 是账号重启操作的预置错误。
	restartErr error
	// statuses 是运行状态快照。
	statuses map[string]RuntimeStatus
	// statusesErr 是读取运行状态快照时的预置错误。
	statusesErr error
	// updates 记录已同步的账号 Cookie。
	updates []string
	// restarts 记录已请求重启的账号。
	restarts []string
}

// UpdateCookie 记录运行时 Cookie 同步请求。
func (f *runtimePortFake) UpdateCookie(_ context.Context, accountID, value string) error {
	f.updates = append(f.updates, accountID+":"+value)
	return f.updateErr
}

// RuntimeStatuses 返回预置状态快照。
func (f *runtimePortFake) RuntimeStatuses(_ context.Context) (map[string]RuntimeStatus, error) {
	return f.statuses, f.statusesErr
}

// RecoverExpiredCredential 记录测试中的会话恢复请求并返回预设结果。
func (f *runtimePortFake) RecoverExpiredCredential(context.Context, string) bool { return true }

// Restart 记录账号重启请求。
func (f *runtimePortFake) Restart(_ context.Context, accountID string) error {
	f.restarts = append(f.restarts, accountID)
	return f.restartErr
}

// wakePortFake 记录自动化任务唤醒请求。
type wakePortFake struct {
	// err 是唤醒操作的预置错误。
	err error
	// accounts 记录已唤醒的账号。
	accounts []string
}

// WakeCredentialBlocked 记录凭证阻塞任务唤醒请求。
func (f *wakePortFake) WakeCredentialBlocked(_ context.Context, accountID string) error {
	f.accounts = append(f.accounts, accountID)
	return f.err
}

// TestRuntimeServiceUpdateCookieSuccess 验证 Cookie 同步和任务唤醒的成功路径。
func TestRuntimeServiceUpdateCookieSuccess(t *testing.T) {
	// runtime 保存运行时端口替身。
	runtime := &runtimePortFake{}
	// wake 保存自动化唤醒端口替身。
	wake := &wakePortFake{}
	// service 保存待测试的账号运行时应用服务。
	service := NewRuntimeService(runtime, wake)
	// updateErr 保存 Cookie 同步结果。
	if updateErr := service.UpdateCookie(context.Background(), "acc1", "cookie"); updateErr != nil {
		t.Fatalf("UpdateCookie returned error: %v", updateErr)
	}
	if len(runtime.updates) != 1 || runtime.updates[0] != "acc1:cookie" || len(wake.accounts) != 1 {
		t.Fatalf("unexpected side effects: runtime=%+v wake=%+v", runtime, wake)
	}
}

// TestRuntimeServiceUpdateCookieMissingRuntime 验证未装配运行时仍保持凭证写回幂等。
func TestRuntimeServiceUpdateCookieMissingRuntime(t *testing.T) {
	// service 保存未装配运行时但已装配唤醒端口的应用服务。
	service := NewRuntimeService(nil, &wakePortFake{})
	// updateErr 保存未装配运行时的同步结果。
	if updateErr := service.UpdateCookie(context.Background(), "acc1", "cookie"); updateErr != nil {
		t.Fatalf("missing runtime should be idempotent: %v", updateErr)
	}
}

// TestRuntimeServiceCancellationAndErrors 验证取消、唤醒错误和运行时错误不会伪造成功。
func TestRuntimeServiceCancellationAndErrors(t *testing.T) {
	// canceled 保存已取消的调用上下文。
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	// service 保存可执行错误路径的应用服务。
	service := NewRuntimeService(&runtimePortFake{updateErr: errors.New("runtime failed")}, &wakePortFake{})
	// canceledErr 保存取消上下文的同步错误。
	if canceledErr := service.UpdateCookie(canceled, "acc1", "cookie"); !errors.Is(canceledErr, context.Canceled) {
		t.Fatalf("canceled context error=%v", canceledErr)
	}
	// wakeErrService 保存唤醒失败的应用服务。
	wakeErrService := NewRuntimeService(&runtimePortFake{}, &wakePortFake{err: errors.New("wake failed")})
	// wakeErr 保存唤醒失败但 Cookie 同步仍完成后的返回错误。
	if wakeErr := wakeErrService.UpdateCookie(context.Background(), "acc1", "cookie"); wakeErr == nil || wakeErr.Error() != "wake failed" {
		t.Fatalf("wake error=%v", wakeErr)
	}
}

// TestRuntimeServiceRuntimeStatusesSnapshot 验证状态快照字段按应用 DTO 原样返回。
func TestRuntimeServiceRuntimeStatusesSnapshot(t *testing.T) {
	// updatedAt 保存状态更新时间，确认时间字段不被 transport 层丢失。
	updatedAt := time.Now().UTC()
	// statuses 保存运行时端口提供的非敏感状态快照。
	statuses := map[string]RuntimeStatus{"acc1": {State: "online", Connected: true, UpdatedAt: updatedAt}}
	// service 保存状态读取应用服务。
	service := NewRuntimeService(&runtimePortFake{statuses: statuses}, nil)
	// got、statusErr 保存读取到的快照和错误。
	got, statusErr := service.RuntimeStatuses(context.Background())
	if statusErr != nil || got["acc1"].State != "online" || !got["acc1"].Connected || !got["acc1"].UpdatedAt.Equal(updatedAt) {
		t.Fatalf("snapshot=%+v err=%v", got, statusErr)
	}
}

// TestRuntimeServiceRecoversExpiredCredential 验证会话失效恢复由运行时端口接管并尊重取消上下文。
func TestRuntimeServiceRecoversExpiredCredential(t *testing.T) {
	// service 保存绑定运行时端口的账号应用服务。
	service := NewRuntimeService(&runtimePortFake{}, nil)
	if !service.RecoverExpiredCredential(context.Background(), "acc1") {
		t.Fatal("expected runtime recovery to be accepted")
	}
	// canceled 保存已取消的请求上下文。
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if service.RecoverExpiredCredential(canceled, "acc1") {
		t.Fatal("canceled recovery must not reach runtime port")
	}
}
