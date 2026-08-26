package account

import (
	"context"
	"errors"
	"testing"
)

// loginSuccessSummaryFake 保存登录成功后归属查询结果。
type loginSuccessSummaryFake struct {
	// summary 是归属查询返回的非敏感账号摘要。
	summary Summary
	// err 是归属查询返回的底层错误。
	err error
}

// GetOwnedSummary 返回测试账号摘要或预设错误。
func (f loginSuccessSummaryFake) GetOwnedSummary(context.Context, int64, string) (Summary, error) {
	return f.summary, f.err
}

// loginSuccessProfilePortFake 保存平台资料刷新结果。
type loginSuccessProfilePortFake struct {
	// result 是平台资料刷新返回的可展示结果。
	result ProfileResult
	// err 是平台资料刷新返回的底层错误。
	err error
}

// RefreshProfile 返回测试资料结果或预设错误。
func (f loginSuccessProfilePortFake) RefreshProfile(context.Context, ProfileInput) (ProfileResult, error) {
	return f.result, f.err
}

// loginSuccessRuntimeErrorFake 保存登录成功后重启错误。
type loginSuccessRuntimeErrorFake struct {
	// err 是运行时重启返回的底层错误。
	err error
}

// Restart 返回预设的运行时重启错误。
func (f loginSuccessRuntimeErrorFake) Restart(context.Context, string) error { return f.err }

// TestLoginSuccessServiceCoversProfileRefreshBranches 验证登录成功后的资料刷新成功、业务失败和基础设施失败分支。
func TestLoginSuccessServiceCoversProfileRefreshBranches(t *testing.T) {
	// reports 保存后续动作的脱敏诊断文本。
	reports := make([]string, 0, 4)
	// report 记录服务发出的诊断信息。
	report := func(message string, _ error) { reports = append(reports, message) }
	// summary 是资料刷新使用的本地账号摘要。
	summary := loginSuccessSummaryFake{summary: Summary{ID: "account"}}
	// profileError 是平台资料刷新返回的基础设施错误。
	profileError := errors.New("profile unavailable")
	// profileErrorService 是资料刷新失败场景使用的服务。
	profileErrorProfile, err := NewProfileService(summary, loginSuccessProfilePortFake{err: profileError})
	if err != nil {
		t.Fatal(err)
	}
	// profileErrorService 是资料刷新基础设施失败场景使用的服务。
	profileErrorService := NewLoginSuccessService(&summary, profileErrorProfile, nil, nil, report)
	profileErrorService.AfterSuccessfulLogin(context.Background(), 1, "account")
	// businessErrorProfile 是返回平台业务错误文本的资料服务。
	businessErrorProfile, err := NewProfileService(summary, loginSuccessProfilePortFake{result: ProfileResult{ErrorMessage: "platform rejected"}})
	if err != nil {
		t.Fatal(err)
	}
	// businessErrorService 是平台返回业务错误文本场景使用的服务。
	businessErrorService := NewLoginSuccessService(&summary, businessErrorProfile, nil, nil, report)
	businessErrorService.AfterSuccessfulLogin(context.Background(), 1, "account")
	// successProfile 是返回成功资料的资料服务。
	successProfile, err := NewProfileService(summary, loginSuccessProfilePortFake{result: ProfileResult{Nickname: "seller"}})
	if err != nil {
		t.Fatal(err)
	}
	// successService 是平台资料刷新成功场景使用的服务。
	successService := NewLoginSuccessService(&summary, successProfile, nil, nil, report)
	successService.AfterSuccessfulLogin(context.Background(), 1, "account")
	if len(reports) != 2 {
		t.Fatalf("资料刷新诊断数量错误: %v", reports)
	}

	// summaryErrorService 是账号归属查询失败场景使用的服务。
	summaryErrorService := NewLoginSuccessService(&loginSuccessSummaryFake{err: errors.New("summary unavailable")}, successProfile, nil, nil, report)
	summaryErrorService.AfterSuccessfulLogin(context.Background(), 1, "account")
}

// TestLoginSuccessServiceCoversRestartErrorBranch 验证启用账号登录成功后重启失败会被安全记录。
func TestLoginSuccessServiceCoversRestartErrorBranch(t *testing.T) {
	// reports 保存重启失败诊断消息。
	reports := make([]string, 0, 1)
	// restartError 是运行时重启返回的底层错误。
	restartError := errors.New("runtime unavailable")
	// service 是绑定启用状态和失败运行时的登录成功服务。
	service := NewLoginSuccessService(nil, nil, loginSuccessStatusFake{enabled: true}, loginSuccessRuntimeErrorFake{err: restartError}, func(message string, _ error) {
		reports = append(reports, message)
	})
	service.AfterSuccessfulLogin(context.Background(), 1, "account")
	if len(reports) != 1 || reports[0] == "" {
		t.Fatalf("重启失败诊断错误: %v", reports)
	}
}
