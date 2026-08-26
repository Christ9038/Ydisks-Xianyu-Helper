package adapter

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	accountapp "xianyu-go/internal/application/account"
)

// TestAccountCookieWriterRejectsMissingPorts 验证请求范围凭证 writer 缺少仓储时不会伪装成功。
func TestAccountCookieWriterRejectsMissingPorts(t *testing.T) {
	// writer 模拟尚未完成基础设施装配但已经携带明文 Cookie 的请求实例。
	writer := NewAccountCookieWriter(nil, nil, "sid=short-lived", nil)
	// createErr 保存新增凭证写入的装配错误。
	createErr := writer.CreateOwnedCookie(context.Background(), "cid", 1)
	if createErr == nil {
		t.Fatal("缺少凭证仓储时新增 Cookie 不应伪装成功")
	}
	// updateErr 保存既有凭证更新的装配错误。
	updateErr := writer.UpdateOwnedCookie(context.Background(), "cid", 1, 0)
	if updateErr == nil {
		t.Fatal("缺少凭证端口时更新 Cookie 不应伪装成功")
	}
}

// TestAccountCookieWriterUpdatesOwnedCookie 验证更新成功、跨用户拒绝和版本冲突均发生在适配器内。
func TestAccountCookieWriterUpdatesOwnedCookie(t *testing.T) {
	// store、cleanup 保存包含测试账号凭证的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定测试数据库的账号凭证仓储。
	repository := NewAccountLoginRepository(store)
	// platformCredentials 是只暴露平台窄视图的读取服务。
	platformCredentials, serviceErr := accountapp.NewPlatformCredentialService(repository)
	if serviceErr != nil {
		t.Fatalf("构造平台凭证服务失败: %v", serviceErr)
	}
	// detail、detailErr 保存更新前的凭证版本与归属信息。
	detail, detailErr := repository.LoadPlatformDetail(context.Background(), "cid")
	if detailErr != nil {
		t.Fatalf("读取测试凭证失败: %v", detailErr)
	}
	// writer 是只在本次更新请求中持有明文 Cookie 的适配器。
	writer := NewAccountCookieWriter(repository, platformCredentials, "sid=updated", nil)
	// updateErr 保存归属正确且版本匹配时的更新结果。
	updateErr := writer.UpdateOwnedCookie(context.Background(), "cid", detail.UserID, detail.LastRefreshAt)
	if updateErr != nil {
		t.Fatalf("更新账号 Cookie 失败: %v", updateErr)
	}
	// updated、updatedErr 保存数据库中更新后的平台凭证视图。
	updated, updatedErr := repository.LoadPlatformDetail(context.Background(), "cid")
	if updatedErr != nil || updated.Value != "sid=updated" {
		t.Fatalf("更新后的 Cookie 不符合预期: detail=%+v err=%v", updated, updatedErr)
	}
	// forbiddenErr 保存跨用户更新的稳定应用错误。
	forbiddenErr := writer.UpdateOwnedCookie(context.Background(), "cid", detail.UserID+1, 0)
	if !errors.Is(forbiddenErr, accountapp.ErrForbidden) {
		t.Fatalf("跨用户更新应拒绝，got %v", forbiddenErr)
	}
	// conflictErr 保存客户端版本落后时的并发冲突错误。
	conflictErr := writer.UpdateOwnedCookie(context.Background(), "cid", detail.UserID, detail.LastRefreshAt-1)
	if !errors.Is(conflictErr, accountapp.ErrCredentialConflict) {
		t.Fatalf("过期版本应返回冲突，got %v", conflictErr)
	}
}

// TestAccountCookieWriterHonorsContext 验证平台视图查询收到取消信号后不会继续写入 Cookie。
func TestAccountCookieWriterHonorsContext(t *testing.T) {
	// store、cleanup 保存用于取消路径验证的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定测试数据库的账号凭证仓储。
	repository := NewAccountLoginRepository(store)
	// platformCredentials 是更新流程使用的窄凭证视图服务。
	platformCredentials, serviceErr := accountapp.NewPlatformCredentialService(repository)
	if serviceErr != nil {
		t.Fatalf("构造平台凭证服务失败: %v", serviceErr)
	}
	// writer 是待接收取消上下文的凭证更新适配器。
	writer := NewAccountCookieWriter(repository, platformCredentials, "sid=cancelled", nil)
	// ctx、cancel 保存已取消的请求上下文及其取消函数。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// updateErr 保存取消后的数据库查询结果。
	updateErr := writer.UpdateOwnedCookie(ctx, "cid", 1, 0)
	if updateErr == nil {
		t.Fatal("取消上下文不应继续完成凭证更新")
	}
}

// cookieWriterRepositoryFake 是凭证 writer 测试使用的最小仓储替身，可注入清理失败和写入失败。
type cookieWriterRepositoryFake struct {
	// detail 是平台窄视图查询返回的账号归属与版本。
	detail *accountapp.CredentialDetail
	// createErr 是新增 Cookie 持久化时注入的错误。
	createErr error
	// updateErr 是更新 Cookie 持久化时注入的错误。
	updateErr error
	// clearErr 是旧 Token 清理时注入的错误。
	clearErr error
	// clearCalls 记录旧 Token 清理调用次数，用于确认主写入后仍执行补偿清理。
	clearCalls int
	// receivedCookies 保存适配器传入的明文 Cookie，仅用于当前测试断言，不输出到失败日志。
	receivedCookies string
}

// LockCredentials 返回测试用的无阻塞解锁函数。
func (f *cookieWriterRepositoryFake) LockCredentials(string) func() {
	return func() {}
}

// CreateCookieOwned 记录新增 Cookie 并返回注入的数据库结果。
func (f *cookieWriterRepositoryFake) CreateCookieOwned(_ context.Context, _ string, cookies string, _ int64) error {
	f.receivedCookies = cookies
	return f.createErr
}

// LoadPlatformDetail 返回测试用的平台窄凭证视图。
func (f *cookieWriterRepositoryFake) LoadPlatformDetail(ctx context.Context, _ string) (*accountapp.CredentialDetail, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return f.detail, nil
}

// UpdateFlatCookieOwned 记录更新 Cookie 并返回注入的数据库结果。
func (f *cookieWriterRepositoryFake) UpdateFlatCookieOwned(_ context.Context, _ *accountapp.CredentialDetail, cookies string) error {
	f.receivedCookies = cookies
	return f.updateErr
}

// UpdateRenewalCookie 满足凭证仓储接口的会话写回方法；本测试不使用该路径。
func (f *cookieWriterRepositoryFake) UpdateRenewalCookie(context.Context, string, string, string, int64) error {
	return nil
}

// ClearTokens 记录清理次数并返回注入的清理结果。
func (f *cookieWriterRepositoryFake) ClearTokens(context.Context, string) error {
	f.clearCalls++
	return f.clearErr
}

// GetStatus 满足凭证仓储接口的账号状态查询方法；本测试不使用该路径。
func (f *cookieWriterRepositoryFake) GetStatus(context.Context, string) bool {
	return true
}

// UpdateProfile 满足凭证仓储接口的资料更新方法；本测试不使用该路径。
func (f *cookieWriterRepositoryFake) UpdateProfile(context.Context, string, string, string) error {
	return nil
}

// TestAccountCookieWriterKeepsPrimaryWriteWhenTokenCleanupFails 验证旧 Token 清理失败不回滚已成功的 Cookie 写入。
func TestAccountCookieWriterKeepsPrimaryWriteWhenTokenCleanupFails(t *testing.T) {
	// cleanupErr 是模拟旧连接 Token 已不可清理的基础设施错误。
	cleanupErr := errors.New("token cleanup unavailable")
	// repository 是可注入清理失败的凭证仓储替身。
	repository := &cookieWriterRepositoryFake{
		detail:   &accountapp.CredentialDetail{ID: "cid", UserID: 1, LastRefreshAt: 7},
		clearErr: cleanupErr,
	}
	// logger 将脱敏清理告警写入丢弃流，避免测试输出任何凭证内容。
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// writer 是携带本次请求 Cookie 的适配器实例。
	writer := NewAccountCookieWriter(repository, repository, "sid=new", logger)
	// createErr 保存新增 Cookie 主写入结果。
	createErr := writer.CreateOwnedCookie(context.Background(), "cid", 1)
	if createErr != nil || repository.receivedCookies != "sid=new" || repository.clearCalls != 1 {
		t.Fatalf("清理失败不应回滚新增写入: err=%v cookies=%q clears=%d", createErr, repository.receivedCookies, repository.clearCalls)
	}
	// updateErr 保存既有 Cookie 主写入结果。
	updateErr := writer.UpdateOwnedCookie(context.Background(), "cid", 1, 7)
	if updateErr != nil || repository.receivedCookies != "sid=new" || repository.clearCalls != 2 {
		t.Fatalf("清理失败不应回滚更新写入: err=%v cookies=%q clears=%d", updateErr, repository.receivedCookies, repository.clearCalls)
	}
}

// TestAccountCookieWriterPropagatesPrimaryWriteErrors 验证凭证主写入失败时立即返回且不执行旧 Token 清理。
func TestAccountCookieWriterPropagatesPrimaryWriteErrors(t *testing.T) {
	// createErr 保存新增 Cookie 主写入失败原因。
	createErr := errors.New("新增 Cookie 写入失败")
	// createRepository 保存可注入新增写入错误的仓储替身。
	createRepository := &cookieWriterRepositoryFake{createErr: createErr}
	// createWriter 保存新增 Cookie 错误路径的请求范围适配器。
	createWriter := NewAccountCookieWriter(createRepository, createRepository, "sid=create", nil)
	// err 保存新增 Cookie 主写入错误路径的返回值。
	if err := createWriter.CreateOwnedCookie(context.Background(), "cid", 1); !errors.Is(err, createErr) || createRepository.clearCalls != 0 {
		t.Fatalf("新增主写入错误未及时返回: err=%v clears=%d", err, createRepository.clearCalls)
	}
	// updateErr 保存既有 Cookie 主写入失败原因。
	updateErr := errors.New("更新 Cookie 写入失败")
	// updateRepository 保存可注入更新写入错误的仓储替身。
	updateRepository := &cookieWriterRepositoryFake{detail: &accountapp.CredentialDetail{ID: "cid", UserID: 1, LastRefreshAt: 7}, updateErr: updateErr}
	// updateWriter 保存更新 Cookie 错误路径的请求范围适配器。
	updateWriter := NewAccountCookieWriter(updateRepository, updateRepository, "sid=update", nil)
	// err 保存既有 Cookie 主写入错误路径的返回值。
	if err := updateWriter.UpdateOwnedCookie(context.Background(), "cid", 1, 7); !errors.Is(err, updateErr) || updateRepository.clearCalls != 0 {
		t.Fatalf("更新主写入错误未及时返回: err=%v clears=%d", err, updateRepository.clearCalls)
	}
}
