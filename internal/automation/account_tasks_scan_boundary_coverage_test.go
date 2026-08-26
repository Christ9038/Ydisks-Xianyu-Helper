package automation

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// TestScanAccountTasksCoversStatusPauseSessionAndPolishBranches 验证账号任务扫描器的状态、暂停、会话阻断和擦亮分支。
func TestScanAccountTasksCoversStatusPauseSessionAndPolishBranches(t *testing.T) {
	// store、cleanup 保存扫描测试使用的 SQLite 存储及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是所有本地扫描和任务仓储调用共用的上下文。
	ctx := context.Background()
	// client 记录扫描器是否触发平台任务。
	client := &fakeAccountTaskClient{items: []mtop.ItemListItem{{ID: "scan-polish-item"}}}
	// center 是注入本地平台替身后的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{AccountTaskClient: client})
	// settings 保存同时启用评价和擦亮的任务设置。
	settings := db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true, AutoPolishEnabled: true, RateContent: "交易愉快", PolishTime: beijingNow().Format("15:04")}
	// saveErr 表示写入扫描设置时的数据库错误。
	if saveErr := store.AccountTasks.Upsert(ctx, settings); saveErr != nil {
		t.Fatal(saveErr)
	}
	// statusErr 表示停用账号时的状态更新错误。
	if statusErr := store.Cookies.SetStatus(ctx, "cid", false); statusErr != nil {
		t.Fatal(statusErr)
	}
	center.scanAccountTasks(ctx)
	if client.pendingCalls != 0 || client.polishCalls != 0 {
		t.Fatalf("停用账号不应执行任务 pending=%d polish=%d", client.pendingCalls, client.polishCalls)
	}
	// resumeErr 表示恢复账号启用状态时的状态更新错误。
	if resumeErr := store.Cookies.SetStatus(ctx, "cid", true); resumeErr != nil {
		t.Fatal(resumeErr)
	}
	// pauseErr 表示设置临时暂停时的数据库错误。
	if _, pauseErr := store.Cookies.SetPause(ctx, "cid", 1); pauseErr != nil {
		t.Fatal(pauseErr)
	}
	center.scanAccountTasks(ctx)
	if client.pendingCalls != 0 || client.polishCalls != 0 {
		t.Fatalf("暂停账号不应执行任务 pending=%d polish=%d", client.pendingCalls, client.polishCalls)
	}
	// clearPauseErr 表示取消账号暂停时的数据库错误。
	if _, clearPauseErr := store.Cookies.SetPause(ctx, "cid", 0); clearPauseErr != nil {
		t.Fatal(clearPauseErr)
	}
	// fingerprint、fingerprintErr 保存当前凭证阻断指纹及读取错误。
	fingerprint, fingerprintErr := center.taskRunner.accountCredentialFingerprint(ctx, "cid")
	if fingerprintErr != nil {
		t.Fatal(fingerprintErr)
	}
	center.taskRunner.sessionExpired.Store("cid", fingerprint)
	center.scanAccountTasks(ctx)
	if client.pendingCalls != 0 || client.polishCalls != 0 {
		t.Fatalf("会话阻断账号不应执行任务 pending=%d polish=%d", client.pendingCalls, client.polishCalls)
	}
	center.taskRunner.sessionExpired.Delete("cid")
	// updateErr 表示清除旧运行状态并重新启用设置时的数据库错误。
	if updateErr := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoPolishEnabled: true, RateContent: "交易愉快", PolishTime: beijingNow().Format("15:04")}); updateErr != nil {
		t.Fatal(updateErr)
	}
	center.scanAccountTasks(ctx)
	if client.polishCalls != 1 {
		t.Fatalf("到时擦亮应执行一次，polish=%d", client.polishCalls)
	}
}
