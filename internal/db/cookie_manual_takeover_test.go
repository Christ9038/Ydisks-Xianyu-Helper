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
