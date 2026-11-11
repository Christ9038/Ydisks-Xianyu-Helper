package db

import (
	"context"
	"errors"
	"testing"
)

// newConsignProofTestAccount 创建一个用户及其明文 Cookie 账号，返回上下文、仓储、用户 ID、账号 ID 和清理函数。
func newConsignProofTestAccount(t *testing.T) (context.Context, *Store, int64, string, func()) {
	t.Helper()
	// store、cleanup 是本测试独占的 SQLite 仓储与关闭函数。
	store, cleanup := newTestDB(t)
	// ctx 是本测试数据库操作共用的上下文。
	ctx := context.Background()
	// userID 是账号所有者的本地用户 ID。
	var userID int64
	// userErr 保存创建测试用户的错误。
	if userErr := store.DB.QueryRowContext(ctx,
		`INSERT INTO users (username,email,password_hash) VALUES (?,?,?) RETURNING id`,
		"proof-owner", "proof-owner@example.com", "test-hash").Scan(&userID); userErr != nil {
		cleanup()
		t.Fatalf("创建用户: %v", userErr)
	}
	// insertErr 保存写入测试账号的错误；不显式写凭证列，验证迁移默认值。
	if _, insertErr := store.DB.ExecContext(ctx, `INSERT INTO cookies (id,value,user_id) VALUES ('proof-account','cookie',?)`, userID); insertErr != nil {
		cleanup()
		t.Fatalf("创建账号: %v", insertErr)
	}
	return ctx, store, userID, "proof-account", cleanup
}

// strPtr 返回字符串指针，供可选设置字段使用。
func strPtr(value string) *string { return &value }

// TestConsignProofSettingDefaultsToDelivery 验证迁移后的新旧账号默认携带卡密/模板全文，保持历史确认发货行为。
func TestConsignProofSettingDefaultsToDelivery(t *testing.T) {
	// ctx、store、userID、accountID、cleanup 是测试账号夹具。
	ctx, store, userID, accountID, cleanup := newConsignProofTestAccount(t)
	defer cleanup()
	// setting、err 是默认配置读取结果。
	setting, err := store.Cookies.GetConsignProofSetting(ctx, accountID)
	if err != nil || setting.Mode != ConsignProofModeDelivery || setting.FixedText != "" {
		t.Fatalf("默认凭证配置错误: %#v err=%v", setting, err)
	}
	// summary、summaryErr 验证账号摘要同步返回默认模式。
	summary, summaryErr := store.Cookies.GetSummaryOwned(ctx, userID, accountID)
	if summaryErr != nil || summary.ConsignProofMode != ConsignProofModeDelivery || summary.ConsignProofText != "" {
		t.Fatalf("摘要默认凭证配置错误: %#v err=%v", summary, summaryErr)
	}
	// missingErr 验证不存在的账号映射为 ErrNotFound。
	if _, missingErr := store.Cookies.GetConsignProofSetting(ctx, "missing"); !errors.Is(missingErr, ErrNotFound) {
		t.Fatalf("缺失账号应返回 ErrNotFound: %v", missingErr)
	}
}

// TestConsignProofSettingUpdateAndFinalStateValidation 覆盖固定文字保存、切换模式保留文字、最终状态校验失败整笔回滚以及无关字段不受历史数据阻塞。
func TestConsignProofSettingUpdateAndFinalStateValidation(t *testing.T) {
	// ctx、store、userID、accountID、cleanup 是测试账号夹具。
	ctx, store, userID, accountID, cleanup := newConsignProofTestAccount(t)
	defer cleanup()

	// missingTextErr 验证切换到固定文字但从未填写文字时被拒绝，且同一请求里的备注也不落库。
	_, missingTextErr := store.Cookies.UpdateSettings(ctx, accountID, AccountSettingsUpdate{UserID: userID, ConsignProofMode: strPtr(ConsignProofModeFixed), Remark: strPtr("不应保存")})
	if !errors.Is(missingTextErr, ErrConsignProofTextRequired) {
		t.Fatalf("固定文字为空应被拒绝: %v", missingTextErr)
	}
	// rolledBack 是拒绝后重新读取的摘要，备注与模式都必须保持原值。
	rolledBack, _ := store.Cookies.GetSummaryOwned(ctx, userID, accountID)
	if rolledBack.Remark != "" || rolledBack.ConsignProofMode != ConsignProofModeDelivery {
		t.Fatalf("校验失败后应整笔回滚: %#v", rolledBack)
	}

	// fixedErr 保存同时写入固定文字模式与文字的结果。
	if _, fixedErr := store.Cookies.UpdateSettings(ctx, accountID, AccountSettingsUpdate{UserID: userID, ConsignProofMode: strPtr(ConsignProofModeFixed), ConsignProofText: strPtr("已通过聊天发送")}); fixedErr != nil {
		t.Fatalf("保存固定文字失败: %v", fixedErr)
	}
	// setting 是保存固定文字后读回的凭证配置。
	if setting, _ := store.Cookies.GetConsignProofSetting(ctx, accountID); setting.Mode != ConsignProofModeFixed || setting.FixedText != "已通过聊天发送" {
		t.Fatalf("固定文字读取错误: %#v", setting)
	}

	// blankErr 验证固定文字模式下单独把文字清成空白也会被拒绝。
	if _, blankErr := store.Cookies.UpdateSettings(ctx, accountID, AccountSettingsUpdate{UserID: userID, ConsignProofText: strPtr("   ")}); !errors.Is(blankErr, ErrConsignProofTextRequired) {
		t.Fatalf("固定文字模式下清空文字应被拒绝: %v", blankErr)
	}

	// noneErr 验证切换到不携带时保留已填写的固定文字，便于用户切回。
	if _, noneErr := store.Cookies.UpdateSettings(ctx, accountID, AccountSettingsUpdate{UserID: userID, ConsignProofMode: strPtr(ConsignProofModeNone)}); noneErr != nil {
		t.Fatalf("切换不携带失败: %v", noneErr)
	}
	// setting 是切换到不携带后读回的凭证配置，固定文字应保留。
	if setting, _ := store.Cookies.GetConsignProofSetting(ctx, accountID); setting.Mode != ConsignProofModeNone || setting.FixedText != "已通过聊天发送" {
		t.Fatalf("切换模式后应保留固定文字: %#v", setting)
	}

	// legacyErr 模拟手工改库留下的“固定文字模式 + 空文字”历史数据。
	if _, legacyErr := store.DB.ExecContext(ctx, `UPDATE cookies SET consign_proof_mode='fixed',consign_proof_text='' WHERE id=?`, accountID); legacyErr != nil {
		t.Fatal(legacyErr)
	}
	// remarkErr 验证未触碰凭证字段的保存不会被历史数据阻塞。
	if _, remarkErr := store.Cookies.UpdateSettings(ctx, accountID, AccountSettingsUpdate{UserID: userID, Remark: strPtr("新备注")}); remarkErr != nil {
		t.Fatalf("无关字段保存不应校验凭证: %v", remarkErr)
	}
}

// TestNormalizeConsignProofModeFallsBackToDelivery 验证空值与未知值按历史行为归一为 delivery。
func TestNormalizeConsignProofModeFallsBackToDelivery(t *testing.T) {
	// mode、want 是输入模式与期望归一结果。
	for mode, want := range map[string]string{"": ConsignProofModeDelivery, "unknown": ConsignProofModeDelivery, ConsignProofModeDelivery: ConsignProofModeDelivery, ConsignProofModeFixed: ConsignProofModeFixed, ConsignProofModeNone: ConsignProofModeNone} {
		// got 是当前输入的归一结果。
		if got := NormalizeConsignProofMode(mode); got != want {
			t.Fatalf("NormalizeConsignProofMode(%q)=%q want %q", mode, got, want)
		}
	}
	// ctx、store、accountID、cleanup 是测试账号夹具，用于验证读取路径同样归一。
	ctx, store, _, accountID, cleanup := newConsignProofTestAccount(t)
	defer cleanup()
	// err 保存写入未知模式历史数据的错误。
	if _, err := store.DB.ExecContext(ctx, `UPDATE cookies SET consign_proof_mode='garbage' WHERE id=?`, accountID); err != nil {
		t.Fatal(err)
	}
	// setting 是读取未知模式时的归一结果。
	if setting, _ := store.Cookies.GetConsignProofSetting(ctx, accountID); setting.Mode != ConsignProofModeDelivery {
		t.Fatalf("未知模式应按 delivery 读取: %#v", setting)
	}
}
