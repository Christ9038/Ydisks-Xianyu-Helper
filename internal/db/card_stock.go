package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// AvailableDataStock 统计用户启用的数据卡密组中非空卡密行的数量。
// ctx 用于取消查询；结果只返回数量，绝不将卡密正文传出 db 层。
func (cards *Cards) AvailableDataStock(ctx context.Context, userID int64) (int64, error) {
	if cards == nil || cards.DB == nil {
		return 0, errors.New("卡密库存仓储未初始化")
	}
	// rows、queryErr 保存卡密组最小字段查询结果；正文仅在本函数内逐行计数。
	rows, queryErr := cards.DB.QueryContext(ctx, `SELECT enabled, type, data_content FROM cards WHERE user_id=?`, userID)
	if queryErr != nil {
		return 0, queryErr
	}
	defer rows.Close()
	// stock 保存符合启用状态和数据类型约束的非空卡密行总数。
	var stock int64
	for rows.Next() {
		// enabled 保存当前卡密组是否启用的可空数据库字段。
		var enabled sql.NullInt64
		// cardType 保存当前卡密组的业务类型，用于排除非数据卡密组。
		var cardType sql.NullString
		// dataContent 保存当前卡密组的原始正文，只在 db 层计数且不返回给调用方。
		var dataContent sql.NullString
		// scanErr 保存扫描当前卡密组最小字段时的数据库错误。
		scanErr := rows.Scan(&enabled, &cardType, &dataContent)
		if scanErr != nil {
			return 0, scanErr
		}
		if !enabled.Valid || enabled.Int64 == 0 || !cardType.Valid || cardType.String != "data" {
			continue
		}
		// line 保存拆分后的当前卡密文本行，只用于判断是否为空，不记录或传播其内容。
		for _, line := range strings.Split(strings.ReplaceAll(dataContent.String, "\r\n", "\n"), "\n") {
			if strings.TrimSpace(line) != "" {
				stock++
			}
		}
	}
	// iterationErr 保存驱动在遍历结果集时报告的延迟错误。
	iterationErr := rows.Err()
	if iterationErr != nil {
		return 0, iterationErr
	}
	return stock, nil
}
