package account

import (
	"context"
	"errors"
	"testing"
)

// fakeLoginAuditRepository 记录登录审计应用服务调用并注入各阶段错误。
type fakeLoginAuditRepository struct {
	// markErr、statusErr、logErr 分别模拟登录方式、账号状态和审计日志写入失败。
	markErr   error
	statusErr error
	logErr    error
	// markCalls、statusCalls、logs 保存各持久化阶段的调用结果。
	markCalls   int
	statusCalls int
	logs        []LoginAuditLog
}

// MarkLogin 记录最近一次成功登录方式写入请求。
func (r *fakeLoginAuditRepository) MarkLogin(_ context.Context, _ string, _ string, _ int64) error {
	r.markCalls++
	return r.markErr
}

// SetStatusWithReason 记录登录成功后的账号启用请求。
func (r *fakeLoginAuditRepository) SetStatusWithReason(_ context.Context, _ string, _ bool, _ string) error {
	r.statusCalls++
	return r.statusErr
}

// AddLoginLog 保存应用层登录审计模型，供测试核对敏感字段未被引入。
func (r *fakeLoginAuditRepository) AddLoginLog(_ context.Context, log LoginAuditLog) error {
	r.logs = append(r.logs, log)
	return r.logErr
}

// TestLoginAuditServiceRequiresRepository 验证缺少持久化端口时服务返回明确错误。
func TestLoginAuditServiceRequiresRepository(t *testing.T) {
	// service 是没有仓储端口的零值审计服务。
	service := NewLoginAuditService(nil)
	// recordErr 保存缺少依赖时的执行错误。
	recordErr := service.RecordSuccessfulLogin(context.Background(), SuccessfulLoginInput{AccountID: "acc1", Method: LoginMethodManual})
	if !errors.Is(recordErr, ErrLoginAuditUnavailable) {
		t.Fatalf("缺少审计仓储时错误=%v", recordErr)
	}
}

// TestLoginAuditServiceManualSuccessWritesLog 验证手动登录只记录方式和审计日志，不重复启用账号。
func TestLoginAuditServiceManualSuccessWritesLog(t *testing.T) {
	// repository 是记录持久化调用的审计仓储替身。
	repository := &fakeLoginAuditRepository{}
	// service 是使用测试仓储构造的应用服务。
	service := NewLoginAuditService(repository)
	// recordErr 保存固定时间的手动登录审计结果。
	recordErr := service.RecordSuccessfulLogin(context.Background(), SuccessfulLoginInput{AccountID: "acc1", UserID: 7, Method: "cookie", Message: "登录成功", OccurredAt: 42})
	if recordErr != nil {
		t.Fatalf("记录手动登录失败: %v", recordErr)
	}
	if repository.markCalls != 1 || repository.statusCalls != 0 || len(repository.logs) != 1 {
		t.Fatalf("持久化阶段调用异常: %+v", repository)
	}
	if repository.logs[0].Method != LoginMethodManual || repository.logs[0].TriggerReason != "手动Cookie录入" || repository.logs[0].OccurredAt != 42 {
		t.Fatalf("登录审计模型异常: %+v", repository.logs[0])
	}
}

// TestLoginAuditServicePasswordEnablesAccount 验证密码和扫码登录成功后会启用账号并记录日志。
func TestLoginAuditServicePasswordEnablesAccount(t *testing.T) {
	// repository 是记录账号状态与日志写入的审计仓储替身。
	repository := &fakeLoginAuditRepository{}
	// service 是使用测试仓储构造的应用服务。
	service := NewLoginAuditService(repository)
	// recordErr 保存密码登录成功的审计结果。
	recordErr := service.RecordSuccessfulLogin(context.Background(), SuccessfulLoginInput{AccountID: "acc1", UserID: 7, Method: "password_login"})
	if recordErr != nil {
		t.Fatalf("记录密码登录失败: %v", recordErr)
	}
	if repository.statusCalls != 1 || len(repository.logs) != 1 || repository.logs[0].Method != LoginMethodPassword {
		t.Fatalf("密码登录启用与日志调用异常: %+v", repository)
	}
}

// TestLoginAuditServiceJoinsIndependentWriteFailures 验证状态与日志独立失败时保留两类错误。
func TestLoginAuditServiceJoinsIndependentWriteFailures(t *testing.T) {
	// statusErr、logErr 是两个相互独立的持久化故障。
	statusErr := errors.New("启用账号失败")
	// logErr 表示登录审计日志独立写入失败。
	logErr := errors.New("写入审计失败")
	// repository 注入两个独立故障，确认服务仍尝试写日志。
	repository := &fakeLoginAuditRepository{statusErr: statusErr, logErr: logErr}
	// service 是使用故障仓储构造的应用服务。
	service := NewLoginAuditService(repository)
	// recordErr 保存合并后的审计错误。
	recordErr := service.RecordSuccessfulLogin(context.Background(), SuccessfulLoginInput{AccountID: "acc1", Method: LoginMethodQRScan})
	if !errors.Is(recordErr, statusErr) || !errors.Is(recordErr, logErr) {
		t.Fatalf("应保留状态和日志双重错误: %v", recordErr)
	}
	if len(repository.logs) != 1 {
		t.Fatal("状态写入失败后仍应尝试记录审计日志")
	}
}

// TestLoginAuditServiceStopsAfterMarkFailure 验证最近登录方式写入失败时不会伪造后续成功记录。
func TestLoginAuditServiceStopsAfterMarkFailure(t *testing.T) {
	// markErr 是最近登录方式写入失败。
	markErr := errors.New("记录登录方式失败")
	// repository 注入方式写入故障。
	repository := &fakeLoginAuditRepository{markErr: markErr}
	// service 是使用故障仓储构造的应用服务。
	service := NewLoginAuditService(repository)
	// recordErr 保存方式写入失败结果。
	recordErr := service.RecordSuccessfulLogin(context.Background(), SuccessfulLoginInput{AccountID: "acc1", Method: LoginMethodManual})
	if !errors.Is(recordErr, markErr) {
		t.Fatalf("应返回方式写入错误: %v", recordErr)
	}
	if repository.statusCalls != 0 || len(repository.logs) != 0 {
		t.Fatal("方式写入失败时不应继续伪造成功状态或日志")
	}
}
