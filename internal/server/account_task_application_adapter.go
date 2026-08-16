package server

import automationapp "xianyu-go/internal/application/automation"

// newApplicationAccountTaskSettingsResponse 将应用层账号任务设置转换为 HTTP DTO。
func newApplicationAccountTaskSettingsResponse(settings automationapp.AccountTaskSettings) accountTaskSettingsResponse {
	return accountTaskSettingsResponse{
		AccountID: settings.CookieID, AutoRateEnabled: settings.AutoRateEnabled, RateContent: settings.RateContent,
		AutoPolishEnabled: settings.AutoPolishEnabled, PolishTime: settings.PolishTime,
		LastRateScanAt: settings.LastRateScanAt, LastPolishDate: settings.LastPolishDate, LastPolishAt: settings.LastPolishAt,
	}
}

// newApplicationAccountTaskRunResponses 批量转换应用层账号任务运行记录。
func newApplicationAccountTaskRunResponses(runs []automationapp.AccountTaskRun) []accountTaskRunResponse {
	// result 保存待写入响应的账号任务运行 DTO。
	result := make([]accountTaskRunResponse, 0, len(runs))
	// run 是当前待转换的应用层运行记录。
	for _, run := range runs {
		result = append(result, accountTaskRunResponse{
			ID: run.ID, RunKey: run.RunKey, AccountID: run.CookieID, TaskType: run.TaskType,
			TargetID: run.TargetID, RunDate: run.RunDate, Status: run.Status, SuccessCount: run.SuccessCount,
			FailedCount: run.FailedCount, ErrorMessage: run.ErrorMessage, NextRetryAt: run.NextRetryAt,
			StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
		})
	}
	return result
}

// newApplicationAccountTaskSummaryResponse 将应用层账号任务摘要转换为 HTTP DTO。
func newApplicationAccountTaskSummaryResponse(summary automationapp.TaskSummary) accountTaskSummaryResponse {
	return accountTaskSummaryResponse{
		TaskType: summary.TaskType, Found: summary.Found, Success: summary.Success,
		Failed: summary.Failed, Skipped: summary.Skipped, Message: summary.Message,
	}
}
