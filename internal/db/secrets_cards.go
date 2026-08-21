package db

import (
	"context"
	"database/sql"
	"fmt"
)

// legacyCardAPIConfig 保存待迁移 API 卡券的所属用户和完整请求模板。
type legacyCardAPIConfig struct {
	// id 是 API 卡券数据库主键。
	id int64
	// userID 是卡券所属用户，用于绑定加密附加数据。
	userID int64
	// config 是尚未迁移或待校验的完整配置文本。
	config string
}

// migrateLegacyCardAPIConfigs 在启动事务中迁移并校验历史 API 卡券配置。
func migrateLegacyCardAPIConfigs(ctx context.Context, tx *sql.Tx, codec *secretCodec) error {
	// rows、err 保存 API 卡券迁移查询游标及查询错误。
	rows, err := tx.QueryContext(ctx, `SELECT id,user_id,COALESCE(api_config,'') FROM cards WHERE type='api' AND api_config IS NOT NULL AND api_config<>''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	// cards 保存需要迁移或校验的 API 卡券配置。
	var cards []legacyCardAPIConfig
	for rows.Next() {
		// row 保存当前 API 卡券的持久化敏感字段。
		var row legacyCardAPIConfig
		// err 表示当前 API 卡券敏感字段扫描错误。
		if err := rows.Scan(&row.id, &row.userID, &row.config); err != nil {
			return err
		}
		cards = append(cards, row)
	}
	// err 表示查询游标遍历过程中的错误。
	if err := rows.Err(); err != nil {
		return err
	}
	// row 表示当前需要加密或校验的 API 卡券配置。
	for _, row := range cards {
		// encrypted、err 保存 API 配置密文及加密错误。
		encrypted, err := codec.encrypt(cardAPIConfigScope, fmt.Sprint(row.userID), row.config)
		if err != nil {
			return fmt.Errorf("校验卡券 %d API 配置: %w", row.id, err)
		}
		if encrypted != row.config {
			// err 表示回写 API 卡券密文时的数据库错误。
			if _, err := tx.ExecContext(ctx, `UPDATE cards SET api_config=? WHERE id=?`, encrypted, row.id); err != nil {
				return err
			}
		}
	}
	return nil
}
