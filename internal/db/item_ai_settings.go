package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"unicode/utf8"
)

const (
	// ItemAIOverrideInherit 表示商品沿用账号级 AI 启停状态。
	ItemAIOverrideInherit = "inherit"
	// ItemAIOverrideEnabled 表示商品强制启用 AI 回复。
	ItemAIOverrideEnabled = "enabled"
	// ItemAIOverrideDisabled 表示商品强制停用 AI 回复。
	ItemAIOverrideDisabled = "disabled"
	// ItemAIContextMaxRunes 是商品专属资料允许保存的最大 Unicode 字符数。
	ItemAIContextMaxRunes = 12000
)

// ItemAISettingsRow 保存单个账号商品的 AI 启停覆盖和专属资料。
type ItemAISettingsRow struct {
	// CookieID 是商品所属账号标识。
	CookieID string
	// ItemID 是账号范围内的平台商品标识。
	ItemID string
	// Override 是 inherit、enabled 或 disabled 三态覆盖值。
	Override string
	// Context 是注入 AI 提示词的商品专属资料，不包含账号凭证。
	Context string
}

// ItemAISettings 提供商品级 AI 配置的独立持久化能力。
type ItemAISettings struct {
	// DB 是仓储使用的数据库连接池。
	DB *sql.DB
	// Dialect 决定三方言 UPSERT 语法。
	Dialect Dialect
}

// Get 读取账号商品的 AI 配置；没有独立记录时返回 inherit 和空资料。
func (settings *ItemAISettings) Get(ctx context.Context, cookieID, itemID string) (ItemAISettingsRow, error) {
	// row 保存数据库返回值，并预置无记录时的继承语义。
	row := ItemAISettingsRow{CookieID: cookieID, ItemID: itemID, Override: ItemAIOverrideInherit}
	// queryErr 保存配置查询或扫描错误。
	queryErr := settings.DB.QueryRowContext(ctx,
		`SELECT cookie_id, item_id, ai_override, item_context
		 FROM item_ai_settings WHERE cookie_id=? AND item_id=?`, cookieID, itemID).Scan(
		&row.CookieID, &row.ItemID, &row.Override, &row.Context)
	if errors.Is(queryErr, sql.ErrNoRows) {
		return row, nil
	}
	if queryErr != nil {
		return ItemAISettingsRow{}, queryErr
	}
	return row, nil
}

// Upsert 创建或更新账号商品的 AI 配置，并在进入数据库前复核三态值和资料长度。
func (settings *ItemAISettings) Upsert(ctx context.Context, row ItemAISettingsRow) error {
	if !validItemAIOverride(row.Override) {
		return fmt.Errorf("无效的商品 AI 覆盖值 %q", row.Override)
	}
	if !utf8.ValidString(row.Context) {
		return errors.New("商品 AI 资料必须是有效 Unicode 文本")
	}
	if utf8.RuneCountInString(row.Context) > ItemAIContextMaxRunes {
		return fmt.Errorf("商品 AI 资料不能超过 %d 个 Unicode 字符", ItemAIContextMaxRunes)
	}
	// statement 使用统一占位符，并按数据库方言生成冲突更新子句。
	statement := `INSERT INTO item_ai_settings (cookie_id, item_id, ai_override, item_context, updated_at)
		VALUES (?,?,?,?,CURRENT_TIMESTAMP)` + dialectUpsert(settings.Dialect, []string{"cookie_id", "item_id"}, map[string]string{
		"ai_override":  "EXCLUDED.ai_override",
		"item_context": "EXCLUDED.item_context",
		"updated_at":   "CURRENT_TIMESTAMP",
	})
	// execErr 保存插入或更新配置时的数据库错误。
	_, execErr := settings.DB.ExecContext(ctx, statement, row.CookieID, row.ItemID, row.Override, row.Context)
	return execErr
}

// Delete 删除账号商品的独立 AI 配置，使后续读取恢复默认继承语义。
func (settings *ItemAISettings) Delete(ctx context.Context, cookieID, itemID string) error {
	// execErr 保存删除配置时的数据库错误；不存在记录按幂等成功处理。
	_, execErr := settings.DB.ExecContext(ctx, `DELETE FROM item_ai_settings WHERE cookie_id=? AND item_id=?`, cookieID, itemID)
	return execErr
}

// validItemAIOverride 判断覆盖值是否属于持久化契约允许的三态集合。
func validItemAIOverride(override string) bool {
	return override == ItemAIOverrideInherit || override == ItemAIOverrideEnabled || override == ItemAIOverrideDisabled
}
