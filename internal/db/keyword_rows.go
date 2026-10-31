package db

import (
	"context"
	"database/sql"
	"errors"
)

// ---- 关键字 CRUD ----

// KeywordRow keywords 表完整行（含自增 id，用于按索引删除）。
type KeywordRow struct {
	// ID 是关键词规则主键。
	ID int64 `json:"id"`
	// CookieID 是规则所属账号标识。
	CookieID string
	// Keyword 是第一条表达式的兼容单值字段。
	Keyword string
	// Expressions 是规则保存的完整表达式集合。
	Expressions []string
	// MatchType 是 contains 或 regexp 匹配模式。
	MatchType string
	// Reply 是文字回复内容。
	Reply string
	// ItemID 是逗号分隔的商品范围字段。
	ItemID string
	// Type 是 text 或 image 回复类型。
	Type string
	// ImageURL 是图片回复地址。
	ImageURL string
	// LibraryImageID 是图片库图片主键；与 ImageURL 互斥，0 表示未使用图片库。
	LibraryImageID int64
}

// UpdateByID 更新指定账号的一条关键词规则。
func (k *Keywords) UpdateByID(ctx context.Context, row KeywordRow) error {
	// keyword、expressionsJSON、matchType 保存统一后的关键词持久化字段。
	keyword, expressionsJSON, matchType := keywordPersistenceFields(row.Keyword, row.Expressions, row.MatchType)
	// kwType 保存缺省回复类型的兼容值。
	kwType := row.Type
	if kwType == "" {
		kwType = "text"
	}
	// res、err 保存数据库更新结果。
	res, err := k.DB.ExecContext(ctx, `UPDATE keywords
		SET keyword=?,reply=?,item_id=?,type=?,image_url=?,library_image_id=?,keyword_expressions=?,match_type=?
		WHERE id=? AND cookie_id=?`,
		keyword, row.Reply, nullable(row.ItemID), kwType, nullable(row.ImageURL), row.LibraryImageID, expressionsJSON, matchType, row.ID, row.CookieID)
	if err != nil {
		return err
	}
	// affected、rowsErr 表示实际更新行数及读取行数错误。
	affected, rowsErr := res.RowsAffected()
	if rowsErr == nil && affected == 0 {
		// exists 保存目标规则是否仍存在，用于兼容 MySQL 无变化更新返回零行的驱动语义。
		var exists int
		// existsErr 保存目标规则存在性查询错误。
		existsErr := k.DB.QueryRowContext(ctx, `SELECT 1 FROM keywords WHERE id=? AND cookie_id=?`, row.ID, row.CookieID).Scan(&exists)
		if errors.Is(existsErr, sql.ErrNoRows) {
			return ErrNotFound
		}
		if existsErr != nil {
			return existsErr
		}
	}
	return nil
}

// DeleteByID 删除ByID。
func (k *Keywords) DeleteByID(ctx context.Context, cookieID string, id int64) error {
	// res、err 用于本次流程后续判断的res、err
	res, err := k.DB.ExecContext(ctx, `DELETE FROM keywords WHERE id=? AND cookie_id=?`, id, cookieID)
	if err != nil {
		return err
	}
	if // affected、err 用于本次流程后续判断的affected、err
	affected, err := res.RowsAffected(); err == nil && affected == 0 {
		return ErrNotFound
	}
	return nil
}

// AllRows 取某账号所有关键词规则（含 id 和表达式集合）。
func (k *Keywords) AllRows(ctx context.Context, cookieID string) ([]KeywordRow, error) {
	// rows、err 保存数据库查询结果。
	rows, err := k.DB.QueryContext(ctx,
		`SELECT id, keyword, reply, COALESCE(item_id,''), COALESCE(type,'text'), COALESCE(image_url,''),
				COALESCE(keyword_expressions,''), COALESCE(match_type,'contains'), library_image_id
		 FROM keywords WHERE cookie_id=? ORDER BY id`, cookieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 保存当前账号的关键词规则。
	var out []KeywordRow
	for rows.Next() {
		// r 表示当前读取的关键词数据库行。
		var r KeywordRow
		// rawExpressions 保存当前行的表达式 JSON 文本。
		var rawExpressions string
		if // err 表示当前行扫描错误。
		err := rows.Scan(&r.ID, &r.Keyword, &r.Reply, &r.ItemID, &r.Type, &r.ImageURL, &rawExpressions, &r.MatchType, &r.LibraryImageID); err != nil {
			return nil, err
		}
		r.Expressions = decodeKeywordExpressions(rawExpressions, r.Keyword)
		if len(r.Expressions) > 0 {
			r.Keyword = r.Expressions[0]
		}
		r.MatchType = normalizeKeywordMatchType(r.MatchType)
		r.CookieID = cookieID
		out = append(out, r)
	}
	return out, rows.Err()
}

// Add 添加一条历史兼容的普通包含关键词规则。
func (k *Keywords) Add(ctx context.Context, cookieID, keyword, reply, itemID, kwType, imageURL string) (int64, error) {
	return k.AddWithExpressions(ctx, cookieID, []string{keyword}, reply, itemID, kwType, "contains", imageURL)
}

// AddWithExpressions 添加一条包含多个表达式和匹配模式的关键词规则。
func (k *Keywords) AddWithExpressions(ctx context.Context, cookieID string, expressions []string, reply, itemID, kwType, matchType, imageURL string) (int64, error) {
	return k.AddRow(ctx, KeywordRow{CookieID: cookieID, Expressions: expressions, Reply: reply, ItemID: itemID, Type: kwType, MatchType: matchType, ImageURL: imageURL})
}

// AddRow 添加一条关键词规则，支持图片库图片；row.ID 被忽略，返回新规则主键。
func (k *Keywords) AddRow(ctx context.Context, row KeywordRow) (int64, error) {
	// cookieID、expressions、reply、itemID、kwType、matchType、imageURL 取自待写入行。
	cookieID, expressions, reply, itemID, kwType, matchType, imageURL := row.CookieID, row.Expressions, row.Reply, row.ItemID, row.Type, row.MatchType, row.ImageURL
	// firstKeyword 保存兼容旧字段的首表达式。
	firstKeyword := ""
	if len(expressions) > 0 {
		firstKeyword = expressions[0]
	}
	// keyword、expressionsJSON、normalizedMatchType 保存数据库写入字段。
	keyword, expressionsJSON, normalizedMatchType := keywordPersistenceFields(firstKeyword, expressions, matchType)
	if kwType == "" {
		kwType = "text"
	}
	return insertReturningID(ctx, k.DB, k.Dialect,
		`INSERT INTO keywords (cookie_id, keyword, reply, item_id, type, image_url, library_image_id, keyword_expressions, match_type) VALUES (?,?,?,?,?,?,?,?,?)`,
		cookieID, keyword, reply, nullable(itemID), kwType, nullable(imageURL), row.LibraryImageID, expressionsJSON, normalizedMatchType)
}

// ReplaceForCookie 原子覆盖某账号的全部关键词规则。
func (k *Keywords) ReplaceForCookie(ctx context.Context, cookieID string, rows []KeywordRow) error {
	// tx、err 保存本次批量替换使用的事务及开启错误。
	tx, err := k.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if // err 表示清理旧规则的事务错误。
	_, err := tx.ExecContext(ctx, `DELETE FROM keywords WHERE cookie_id=?`, cookieID); err != nil {
		return err
	}
	// row 表示当前遍历的关键词规则行。
	for _, row := range rows {
		// keyword、expressionsJSON、matchType 保存当前规则的表达式持久化字段。
		keyword, expressionsJSON, matchType := keywordPersistenceFields(row.Keyword, row.Expressions, row.MatchType)
		// kwType 保存缺省回复类型的兼容值。
		kwType := row.Type
		if kwType == "" {
			kwType = "text"
		}
		if // err 表示写入当前规则的事务错误。
		_, err := tx.ExecContext(ctx,
			`INSERT INTO keywords (cookie_id, keyword, reply, item_id, type, image_url, library_image_id, keyword_expressions, match_type) VALUES (?,?,?,?,?,?,?,?,?)`,
			cookieID, keyword, row.Reply, nullable(row.ItemID), kwType, nullable(row.ImageURL), row.LibraryImageID, expressionsJSON, matchType); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteByIndex 按索引（0-based，按 id 顺序）删除某账号的一个关键字。
func (k *Keywords) DeleteByIndex(ctx context.Context, cookieID string, index int) error {
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := k.DB.QueryContext(ctx,
		`SELECT id FROM keywords WHERE cookie_id=? ORDER BY id`, cookieID)
	if err != nil {
		return err
	}
	defer rows.Close()
	// ids 用于本次流程后续判断的ids
	var ids []int64
	for rows.Next() {
		// id 用于本次流程后续判断的标识
		var id int64
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	// iterErr 是逐行读取中途的数据库错误；截断的标识列表会让下标对应到错误的关键字或误报不存在。
	if iterErr := rows.Err(); iterErr != nil {
		return iterErr
	}
	if index < 0 || index >= len(ids) {
		return ErrNotFound
	}
	_, err = k.DB.ExecContext(ctx, `DELETE FROM keywords WHERE id=? AND cookie_id=?`, ids[index], cookieID)
	return err
}
