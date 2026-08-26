package automation

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"xianyu-go/internal/db"
)

// accountTaskBoundaryRepository 覆盖账号任务状态和设置读取错误，不实现本测试未调用的持久化端口。
type accountTaskBoundaryRepository struct {
	// AccountTaskRepository 嵌入未涉及方法的默认接口实现。
	AccountTaskRepository
	// paused、pausedErr 保存账号暂停查询结果和错误。
	paused    bool
	pausedErr error
	// enabled、statusErr 保存账号启用查询结果和错误。
	enabled   bool
	statusErr error
	// settings、settingsErr 保存账号任务配置结果和错误。
	settings    db.AccountTaskSettings
	settingsErr error
	// runtimeData、runtimeDataErr 保存凭证指纹查询结果和错误。
	runtimeData    db.CookieRuntimeData
	runtimeDataErr error
}

// IsPaused 返回测试预置的账号暂停状态。
func (repository *accountTaskBoundaryRepository) IsPaused(context.Context, string) (bool, int64, error) {
	return repository.paused, 0, repository.pausedErr
}

// Status 返回测试预置的账号启用状态。
func (repository *accountTaskBoundaryRepository) Status(context.Context, string) (bool, error) {
	return repository.enabled, repository.statusErr
}

// Get 返回测试预置的账号任务配置。
func (repository *accountTaskBoundaryRepository) Get(context.Context, string) (db.AccountTaskSettings, error) {
	return repository.settings, repository.settingsErr
}

// GetCookieRuntimeData 返回测试预置的凭证指纹输入。
func (repository *accountTaskBoundaryRepository) GetCookieRuntimeData(context.Context, string) (db.CookieRuntimeData, error) {
	return repository.runtimeData, repository.runtimeDataErr
}

// TestAccountTaskCoordinatorCoversStateAndTaskGuards 验证账号任务状态门禁、配置读取和任务类型保护。
func TestAccountTaskCoordinatorCoversStateAndTaskGuards(t *testing.T) {
	// nilCoordinator 保存空接收者状态协调器。
	var nilCoordinator *accountTaskCoordinator
	// nilAllowedErr 保存空协调器状态检查错误。
	if _, nilAllowedErr := nilCoordinator.accountAutomationAllowed(context.Background(), "account"); nilAllowedErr == nil {
		t.Fatal("空账号任务协调器不应通过状态检查")
	}
	// pausedErr 是暂停状态查询失败的底层错误。
	pausedErr := errors.New("pause lookup failed")
	// pausedRepository 保存暂停状态查询失败的仓储替身。
	pausedRepository := &accountTaskBoundaryRepository{pausedErr: pausedErr}
	// pausedCoordinator 保存暂停状态查询失败的协调器。
	pausedCoordinator := &accountTaskCoordinator{repository: pausedRepository, logger: slog.Default()}
	// pausedResultErr 保存暂停状态查询错误。
	if _, pausedResultErr := pausedCoordinator.accountAutomationAllowed(context.Background(), "account"); !errors.Is(pausedResultErr, pausedErr) {
		t.Fatalf("暂停状态查询错误=%v", pausedResultErr)
	}
	// statusErr 是账号启用状态查询失败的底层错误。
	statusErr := errors.New("status lookup failed")
	// statusRepository 保存启用状态查询失败的仓储替身。
	statusRepository := &accountTaskBoundaryRepository{statusErr: statusErr}
	// statusCoordinator 保存启用状态查询失败的协调器。
	statusCoordinator := &accountTaskCoordinator{repository: statusRepository, logger: slog.Default()}
	// statusResultErr 保存启用状态查询错误。
	if _, statusResultErr := statusCoordinator.accountAutomationAllowed(context.Background(), "account"); !errors.Is(statusResultErr, statusErr) {
		t.Fatalf("启用状态查询错误=%v", statusResultErr)
	}
	// blockedRepository 保存暂停账号的状态结果。
	blockedRepository := &accountTaskBoundaryRepository{paused: true, enabled: true}
	// blockedCoordinator 保存暂停账号的协调器。
	blockedCoordinator := &accountTaskCoordinator{repository: blockedRepository, logger: slog.Default()}
	// blockedTaskErr 保存暂停账号任务拒绝错误。
	if _, blockedTaskErr := blockedCoordinator.runAccountTask(context.Background(), "account", TaskAutoRate); blockedTaskErr == nil {
		t.Fatal("暂停账号不应执行任务")
	}
	// settingsErr 是账号任务配置读取失败的底层错误。
	settingsErr := errors.New("settings lookup failed")
	// settingsRepository 保存配置读取失败仓储。
	settingsRepository := &accountTaskBoundaryRepository{enabled: true, settingsErr: settingsErr}
	// settingsCoordinator 保存配置读取失败协调器。
	settingsCoordinator := &accountTaskCoordinator{repository: settingsRepository, logger: slog.Default()}
	// settingsResultErr 保存配置读取错误。
	if _, settingsResultErr := settingsCoordinator.runAccountTask(context.Background(), "account", TaskAutoRate); !errors.Is(settingsResultErr, settingsErr) {
		t.Fatalf("任务配置读取错误=%v", settingsResultErr)
	}
	// unsupportedCoordinator 保存用于未知任务类型检查的协调器。
	unsupportedCoordinator := &accountTaskCoordinator{repository: &accountTaskBoundaryRepository{}, logger: slog.Default()}
	// unsupportedErr 保存未知任务类型错误。
	if _, unsupportedErr := unsupportedCoordinator.runConfiguredAccountTask(context.Background(), db.AccountTaskSettings{CookieID: "account"}, "unknown"); unsupportedErr == nil {
		t.Fatal("未知账号任务类型不应成功")
	}
}

// TestAccountTaskCoordinatorCoversSessionFingerprintBranches 验证会话指纹错误、变化和恢复器结果分支。
func TestAccountTaskCoordinatorCoversSessionFingerprintBranches(t *testing.T) {
	// fingerprintErr 是凭证指纹读取失败的底层错误。
	fingerprintErr := errors.New("credential lookup failed")
	// repository 保存凭证指纹读取错误。
	repository := &accountTaskBoundaryRepository{runtimeDataErr: fingerprintErr}
	// coordinator 保存会话阻断测试协调器。
	coordinator := &accountTaskCoordinator{repository: repository, logger: slog.Default(), recoverer: func() CredentialRecoverer { return nil }}
	// fingerprintResultErr 保存指纹读取失败结果。
	coordinator.sessionExpired.Store("account", "old")
	// fingerprintResultErr 保存指纹读取失败结果。
	if _, fingerprintResultErr := coordinator.accountTaskSessionBlocked(context.Background(), "account"); !errors.Is(fingerprintResultErr, fingerprintErr) {
		t.Fatalf("指纹读取错误=%v", fingerprintResultErr)
	}
	// changedRepository 保存新凭证指纹数据。
	changedRepository := &accountTaskBoundaryRepository{runtimeData: db.CookieRuntimeData{Value: "new-cookie", MetadataJSON: "{}"}}
	// changedCoordinator 保存凭证变化测试协调器。
	changedCoordinator := &accountTaskCoordinator{repository: changedRepository, logger: slog.Default()}
	changedCoordinator.sessionExpired.Store("account", "old")
	// changedBlocked、changedErr 保存凭证变化后的阻断结果。
	changedBlocked, changedErr := changedCoordinator.accountTaskSessionBlocked(context.Background(), "account")
	if changedErr != nil || changedBlocked {
		t.Fatalf("凭证变化阻断结果=%v err=%v", changedBlocked, changedErr)
	}
	// recoverCoordinator 保存没有恢复器的会话恢复协调器。
	recoverCoordinator := &accountTaskCoordinator{repository: changedRepository, logger: slog.Default(), recoverer: func() CredentialRecoverer { return nil }}
	// sessionErr 是平台报告的 Session 失效错误。
	sessionErr := errors.New("session expired")
	// recoverErr 保存没有恢复器时的人工恢复错误。
	recoverErr := recoverCoordinator.recoverAccountTaskSession(context.Background(), "account", sessionErr)
	if !errors.Is(recoverErr, sessionErr) {
		t.Fatalf("无恢复器错误=%v", recoverErr)
	}
}
