package db

import (
	"errors"
	"testing"
)

// intPtr 返回整数指针，供可选设置字段使用。
func intPtr(value int) *int { return &value }

// TestManualTakeoverSettingDefaultsAndUpdate 覆盖迁移默认值（关闭、仅 AI）、保存读回、摘要同步、nil 字段保持原值与缺失账号。
func TestManualTakeoverSettingDefaultsAndUpdate(t *testing.T) {
	// ctx、store、userID、accountID、cleanup 是复用确认发货凭证测试的账号夹具。
	ctx, store, userID, accountID, cleanup := newConsignProofTestAccount(t)
	defer cleanup()
	// setting、err 是迁移默认配置读取结果。
	setting, err := store.Cookies.GetManualTakeoverSetting(ctx, accountID)
	if err != nil || setting.Minutes != 0 || setting.Scope != ManualTakeoverScopeAI {
		t.Fatalf("默认人工介入配置错误: %#v err=%v", setting, err)
	}
	// updateErr 保存同时写入分钟数与范围的结果。
	if _, updateErr := store.Cookies.UpdateSettings(ctx, accountID, AccountSettingsUpdate{UserID: userID, ManualTakeoverMinutes: intPtr(15), ManualTakeoverScope: strPtr(ManualTakeoverScopeAll)}); updateErr != nil {
		t.Fatalf("保存人工介入配置失败: %v", updateErr)
	}
	if setting, _ = store.Cookies.GetManualTakeoverSetting(ctx, accountID); setting.Minutes != 15 || setting.Scope != ManualTakeoverScopeAll {
		t.Fatalf("人工介入配置读回错误: %#v", setting)
	}
	// summary、summaryErr 验证账号摘要同步返回新配置。
	summary, summaryErr := store.Cookies.GetSummaryOwned(ctx, userID, accountID)
	if summaryErr != nil || summary.ManualTakeoverMinutes != 15 || summary.ManualTakeoverScope != ManualTakeoverScopeAll {
		t.Fatalf("摘要人工介入配置错误: %#v err=%v", summary, summaryErr)
	}
	// summaries、listErr 验证列表摘要同样返回新配置。
	summaries, listErr := store.Cookies.ListSummaries(ctx, userID)
	if listErr != nil || len(summaries) != 1 || summaries[0].ManualTakeoverMinutes != 15 || summaries[0].ManualTakeoverScope != ManualTakeoverScopeAll {
		t.Fatalf("列表摘要人工介入配置错误: %#v err=%v", summaries, listErr)
	}
	// remarkErr 只修改备注，人工介入配置必须保持原值。
	if _, remarkErr := store.Cookies.UpdateSettings(ctx, accountID, AccountSettingsUpdate{UserID: userID, Remark: strPtr("备注")}); remarkErr != nil {
		t.Fatalf("保存备注失败: %v", remarkErr)
	}
	if setting, _ = store.Cookies.GetManualTakeoverSetting(ctx, accountID); setting.Minutes != 15 || setting.Scope != ManualTakeoverScopeAll {
		t.Fatalf("无关字段保存不应改动人工介入配置: %#v", setting)
	}
	// missingErr 验证不存在的账号映射为 ErrNotFound。
	if _, missingErr := store.Cookies.GetManualTakeoverSetting(ctx, "missing"); !errors.Is(missingErr, ErrNotFound) {
		t.Fatalf("缺失账号应返回 ErrNotFound: %v", missingErr)
	}
}

// TestManualTakeoverSettingNormalizesOutOfRangeValues 验证仓储直接写入越界分钟数与未知范围时被归一，历史脏数据读取同样归一。
func TestManualTakeoverSettingNormalizesOutOfRangeValues(t *testing.T) {
	// ctx、store、userID、accountID、cleanup 是账号夹具。
	ctx, store, userID, accountID, cleanup := newConsignProofTestAccount(t)
	defer cleanup()
	// overErr 保存写入超上限分钟数与未知范围的结果。
	if _, overErr := store.Cookies.UpdateSettings(ctx, accountID, AccountSettingsUpdate{UserID: userID, ManualTakeoverMinutes: intPtr(99999), ManualTakeoverScope: strPtr("unknown")}); overErr != nil {
		t.Fatalf("保存越界配置失败: %v", overErr)
	}
	// setting 是越界写入后读回的配置。
	if setting, _ := store.Cookies.GetManualTakeoverSetting(ctx, accountID); setting.Minutes != MaxManualTakeoverMinutes || setting.Scope != ManualTakeoverScopeAI {
		t.Fatalf("越界配置应被归一: %#v", setting)
	}
	// rawErr 模拟手工改库写入负数分钟与空范围。
	if _, rawErr := store.DB.ExecContext(ctx, `UPDATE cookies SET manual_takeover_minutes=-5, manual_takeover_scope='' WHERE id=?`, accountID); rawErr != nil {
		t.Fatalf("写入脏数据失败: %v", rawErr)
	}
	// setting 是越界或脏数据写入后读回并归一的人工介入配置。
	if setting, _ := store.Cookies.GetManualTakeoverSetting(ctx, accountID); setting.Minutes != 0 || setting.Scope != ManualTakeoverScopeAI {
		t.Fatalf("脏数据读取应被归一: %#v", setting)
	}
}

// TestKeywordNotifyManualPersistence 覆盖关键词“通知人工”标记在新增、更新、批量替换与运行时读取四条路径上的持久化。
func TestKeywordNotifyManualPersistence(t *testing.T) {
	// ctx、store、accountID、cleanup 是账号夹具；关键词归属账号即可，不需要用户 ID。
	ctx, store, _, accountID, cleanup := newConsignProofTestAccount(t)
	defer cleanup()
	// id、addErr 保存新增带通知标记规则的主键与错误。
	id, addErr := store.Keywords.AddRow(ctx, KeywordRow{CookieID: accountID, Expressions: []string{"转人工"}, Reply: "已为您转接人工", NotifyManual: true})
	if addErr != nil {
		t.Fatalf("新增关键词失败: %v", addErr)
	}
	// rows、rowsErr 验证管理端读取返回通知标记。
	rows, rowsErr := store.Keywords.AllRows(ctx, accountID)
	if rowsErr != nil || len(rows) != 1 || !rows[0].NotifyManual {
		t.Fatalf("管理端读取通知标记错误: %#v err=%v", rows, rowsErr)
	}
	// runtime、runtimeErr 验证回复运行时读取返回通知标记。
	runtime, runtimeErr := store.Keywords.AllWithType(ctx, accountID)
	if runtimeErr != nil || len(runtime) != 1 || !runtime[0].NotifyManual {
		t.Fatalf("运行时读取通知标记错误: %#v err=%v", runtime, runtimeErr)
	}
	// updated 关闭通知标记，其余字段保持。
	updated := rows[0]
	updated.ID = id
	updated.NotifyManual = false
	// updateErr 保存只改备注时的账号设置保存错误。
	if updateErr := store.Keywords.UpdateByID(ctx, updated); updateErr != nil {
		t.Fatalf("更新关键词失败: %v", updateErr)
	}
	if rows, _ = store.Keywords.AllRows(ctx, accountID); len(rows) != 1 || rows[0].NotifyManual {
		t.Fatalf("更新后通知标记应关闭: %#v", rows)
	}
	// replaceErr 保存批量替换带通知标记规则的结果。
	if replaceErr := store.Keywords.ReplaceForCookie(ctx, accountID, []KeywordRow{{Keyword: "人工客服", Reply: "", NotifyManual: true}, {Keyword: "价格", Reply: "九块九"}}); replaceErr != nil {
		t.Fatalf("批量替换关键词失败: %v", replaceErr)
	}
	if rows, _ = store.Keywords.AllRows(ctx, accountID); len(rows) != 2 || !rows[0].NotifyManual || rows[1].NotifyManual {
		t.Fatalf("批量替换后通知标记错误: %#v", rows)
	}
}

// TestReplyMergeSecondsDefaultUpdateAndNormalize 覆盖合并等待秒数的迁移默认值（3）、保存读回、摘要同步、无关字段保持原值、越界与脏数据归一、缺失账号。
func TestReplyMergeSecondsDefaultUpdateAndNormalize(t *testing.T) {
	// ctx、store、userID、accountID、cleanup 是账号夹具。
	ctx, store, userID, accountID, cleanup := newConsignProofTestAccount(t)
	defer cleanup()
	// seconds、err 是迁移默认值读取结果。
	seconds, err := store.Cookies.GetReplyMergeSeconds(ctx, accountID)
	if err != nil || seconds != DefaultReplyMergeSeconds {
		t.Fatalf("默认合并等待应为 %d: %d err=%v", DefaultReplyMergeSeconds, seconds, err)
	}
	// updateErr 保存写入 7 秒的结果。
	if _, updateErr := store.Cookies.UpdateSettings(ctx, accountID, AccountSettingsUpdate{UserID: userID, ReplyMergeSeconds: intPtr(7)}); updateErr != nil {
		t.Fatalf("保存合并等待失败: %v", updateErr)
	}
	if seconds, _ = store.Cookies.GetReplyMergeSeconds(ctx, accountID); seconds != 7 {
		t.Fatalf("合并等待读回错误: %d", seconds)
	}
	// summary、summaryErr 与 summaries、listErr 验证单账号摘要和列表摘要同步返回新值。
	summary, summaryErr := store.Cookies.GetSummaryOwned(ctx, userID, accountID)
	if summaryErr != nil || summary.ReplyMergeSeconds != 7 {
		t.Fatalf("摘要合并等待错误: %#v err=%v", summary, summaryErr)
	}
	// summaries、listErr 是账号摘要列表及其查询错误，用于核对摘要里的合并等待秒数。
	summaries, listErr := store.Cookies.ListSummaries(ctx, userID)
	if listErr != nil || len(summaries) != 1 || summaries[0].ReplyMergeSeconds != 7 {
		t.Fatalf("列表摘要合并等待错误: %#v err=%v", summaries, listErr)
	}
	// remarkErr 只修改备注，合并等待必须保持原值。
	if _, remarkErr := store.Cookies.UpdateSettings(ctx, accountID, AccountSettingsUpdate{UserID: userID, Remark: strPtr("备注")}); remarkErr != nil {
		t.Fatalf("保存备注失败: %v", remarkErr)
	}
	if seconds, _ = store.Cookies.GetReplyMergeSeconds(ctx, accountID); seconds != 7 {
		t.Fatalf("无关字段保存不应改动合并等待: %d", seconds)
	}
	// overErr 直接写入越界值，仓储应夹取到上限。
	if _, overErr := store.Cookies.UpdateSettings(ctx, accountID, AccountSettingsUpdate{UserID: userID, ReplyMergeSeconds: intPtr(999)}); overErr != nil {
		t.Fatalf("保存越界合并等待失败: %v", overErr)
	}
	if seconds, _ = store.Cookies.GetReplyMergeSeconds(ctx, accountID); seconds != MaxReplyMergeSeconds {
		t.Fatalf("越界值应夹取到上限: %d", seconds)
	}
	// rawErr 模拟手工改库写入 0 与负数，读取应回到默认值。
	for _, raw := range []int{0, -4} {
		// rawErr 是直接写入非法原始秒数的 SQL 错误，用于模拟手工改库。
		if _, rawErr := store.DB.ExecContext(ctx, `UPDATE cookies SET reply_merge_seconds=? WHERE id=?`, raw, accountID); rawErr != nil {
			t.Fatalf("写入脏数据失败: %v", rawErr)
		}
		if seconds, _ = store.Cookies.GetReplyMergeSeconds(ctx, accountID); seconds != DefaultReplyMergeSeconds {
			t.Fatalf("脏数据 %d 应回到默认值: %d", raw, seconds)
		}
	}
	// missingErr 验证不存在的账号映射为 ErrNotFound。
	if _, missingErr := store.Cookies.GetReplyMergeSeconds(ctx, "missing"); !errors.Is(missingErr, ErrNotFound) {
		t.Fatalf("缺失账号应返回 ErrNotFound: %v", missingErr)
	}
}
