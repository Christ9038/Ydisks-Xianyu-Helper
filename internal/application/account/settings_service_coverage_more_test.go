package account

import (
	"context"
	"errors"
	"testing"
)

// TestSettingsServiceCoversUpdateAndStatusReadFailures 验证账号设置写入、状态查询及暂停查询的错误边界。
func TestSettingsServiceCoversUpdateAndStatusReadFailures(t *testing.T) {
	// ctx 是设置服务调用使用的基础上下文。
	ctx := context.Background()
	// updateErr 是账号设置事务需要返回的稳定错误。
	updateErr := errors.New("settings update failed")
	// updateService 保存设置事务失败场景的账号设置服务。
	updateService, updateServiceErr := NewSettingsService(&fakeSettingsRepository{updateErr: updateErr}, nil)
	if updateServiceErr != nil {
		t.Fatalf("构造设置事务错误服务失败: %v", updateServiceErr)
	}
	// actualUpdateErr 保存设置事务错误透传结果。
	if _, actualUpdateErr := updateService.UpdateSettings(ctx, SettingsUpdateInput{UserID: 7, AccountID: "account"}); !errors.Is(actualUpdateErr, updateErr) {
		t.Fatalf("设置事务错误未透传: %v", actualUpdateErr)
	}
	// statusReadErr 是 Cookie 更新后启用状态查询需要返回的错误。
	statusReadErr := errors.New("status lookup failed")
	// cookie 保存本次触发状态查询的 Cookie 输入。
	cookie := "cookie"
	// statusService 保存状态查询失败场景的账号设置服务。
	statusService, statusServiceErr := NewSettingsService(&fakeSettingsRepository{statusErr: statusReadErr}, &fakeSettingsRuntime{})
	if statusServiceErr != nil {
		t.Fatalf("构造状态查询错误服务失败: %v", statusServiceErr)
	}
	// statusResult 保存状态查询失败但设置写入成功后的结果。
	statusResult, actualStatusErr := statusService.UpdateSettings(ctx, SettingsUpdateInput{UserID: 7, AccountID: "account", Cookie: &cookie})
	if actualStatusErr != nil || statusResult.RuntimeError != nil {
		t.Fatalf("状态查询失败不应伪造主写入失败: result=%+v err=%v", statusResult, actualStatusErr)
	}
	// disabledStatusRepository 保存状态查询为未启用且无错误的设置仓储替身。
	disabledStatusRepository := &fakeSettingsRepository{status: false}
	// disabledStatusService 保存不会触发运行时重启的 Cookie 设置服务。
	disabledStatusService, disabledStatusServiceErr := NewSettingsService(disabledStatusRepository, &fakeSettingsRuntime{})
	if disabledStatusServiceErr != nil {
		t.Fatalf("构造未启用服务失败: %v", disabledStatusServiceErr)
	}
	// disabledResult 保存未启用账号 Cookie 设置后的结果。
	if disabledResult, disabledCallErr := disabledStatusService.UpdateSettings(ctx, SettingsUpdateInput{UserID: 7, AccountID: "account", Cookie: &cookie}); disabledCallErr != nil || disabledResult.RuntimeError != nil {
		t.Fatalf("未启用账号设置结果=%+v err=%v", disabledResult, disabledCallErr)
	}
	// pauseReadErr 是暂停状态读取需要返回的稳定错误。
	pauseReadErr := errors.New("pause lookup failed")
	// pauseService 保存暂停查询错误场景的账号设置服务。
	pauseService, pauseServiceErr := NewSettingsService(&fakeSettingsRepository{getPauseErr: pauseReadErr}, nil)
	if pauseServiceErr != nil {
		t.Fatalf("构造暂停查询错误服务失败: %v", pauseServiceErr)
	}
	// actualPauseErr 保存暂停查询错误透传结果。
	if _, actualPauseErr := pauseService.GetPause(ctx, 7, "account"); !errors.Is(actualPauseErr, pauseReadErr) {
		t.Fatalf("暂停查询错误未透传: %v", actualPauseErr)
	}
	// emptyRepositoryService 保存服务实例存在但仓储为空的暂停查询场景。
	emptyRepositoryService := &SettingsService{}
	// emptyRepositoryErr 保存空仓储暂停查询时的初始化错误。
	if _, emptyRepositoryErr := emptyRepositoryService.GetPause(ctx, 7, "account"); emptyRepositoryErr == nil {
		t.Fatal("空仓储查询暂停应返回错误")
	}
	// invalidService 表示未初始化的账号设置服务。
	var invalidService *SettingsService
	// invalidStatusErr 保存空服务设置状态时的初始化错误。
	if _, invalidStatusErr := invalidService.SetStatus(ctx, 7, "account", true); invalidStatusErr == nil {
		t.Fatal("空服务设置状态应返回错误")
	}
	// emptyStatusErr 保存空账号设置状态时的输入错误。
	if _, emptyStatusErr := statusService.SetStatus(ctx, 7, "", true); emptyStatusErr == nil {
		t.Fatal("空账号设置状态应返回错误")
	}
	// noRuntimeService 保存不装配运行时的账号设置服务。
	noRuntimeService, noRuntimeServiceErr := NewSettingsService(&fakeSettingsRepository{}, nil)
	if noRuntimeServiceErr != nil {
		t.Fatalf("构造无运行时服务失败: %v", noRuntimeServiceErr)
	}
	// noRuntimeCallErr 保存无运行时启用状态写入的调用错误。
	if _, noRuntimeCallErr := noRuntimeService.SetStatus(ctx, 7, "account", true); noRuntimeCallErr != nil {
		t.Fatalf("无运行时启用状态失败: %v", noRuntimeCallErr)
	}
	// repositoryOnlyService 保存停用状态写入错误的无运行时服务。
	repositoryOnlyService, repositoryOnlyServiceErr := NewSettingsService(&fakeSettingsRepository{statusWriteErr: errors.New("disable write failed")}, nil)
	if repositoryOnlyServiceErr != nil {
		t.Fatalf("构造仓储错误服务失败: %v", repositoryOnlyServiceErr)
	}
	// repositoryOnlyCallErr 保存无运行时停用状态写入错误。
	if _, repositoryOnlyCallErr := repositoryOnlyService.SetStatus(ctx, 7, "account", false); repositoryOnlyCallErr == nil {
		t.Fatal("停用状态写入错误应返回")
	}
	// emptyLoginErr 保存空账号更新登录信息时的输入错误。
	if emptyLoginErr := statusService.UpdateLoginInfo(ctx, LoginInfoUpdateInput{UserID: 7}); emptyLoginErr == nil {
		t.Fatal("空账号更新登录信息应返回错误")
	}
}
