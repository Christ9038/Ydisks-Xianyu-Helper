package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// AllForCookie 返回一个账号下的全部商品持久化记录，不包含账号凭证明文。
func (i *Items) AllForCookie(ctx context.Context, cookieID string) ([]ItemInfoRow, error) {
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := i.DB.QueryContext(ctx,
		`SELECT id, cookie_id, item_id, COALESCE(item_title,''), COALESCE(item_description,''),
		        COALESCE(item_category,''), COALESCE(item_price,''), COALESCE(item_detail,''),
		        is_multi_spec, COALESCE(multi_quantity_delivery,0)
		 FROM item_info WHERE cookie_id=? AND deleted_at IS NULL ORDER BY id DESC`, cookieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItemInfoRows(rows)
}

// GetByCookieItem 读取账号下指定商品的详情文本，供 API 发货模板使用；不存在时返回空记录而非猜测商品内容。
func (i *Items) GetByCookieItem(ctx context.Context, cookieID, itemID string) (ItemInfoRow, error) {
	// row 保存当前账号与商品标识匹配到的本地商品详情。
	var row ItemInfoRow
	// isMulti、multiQty 保存数据库整数标记的布尔转换中间值。
	var isMulti, multiQty int
	// err 表示商品详情查询或扫描错误。
	err := i.DB.QueryRowContext(ctx,
		`SELECT id, cookie_id, item_id, COALESCE(item_title,''), COALESCE(item_description,''),
		        COALESCE(item_category,''), COALESCE(item_price,''), COALESCE(item_detail,''),
		        is_multi_spec, COALESCE(multi_quantity_delivery,0)
		 FROM item_info WHERE cookie_id=? AND item_id=? AND deleted_at IS NULL`, cookieID, itemID).Scan(
		&row.ID, &row.CookieID, &row.ItemID, &row.ItemTitle, &row.ItemDescription,
		&row.ItemCategory, &row.ItemPrice, &row.ItemDetail, &isMulti, &multiQty)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ItemInfoRow{}, ErrNotFound
		}
		return ItemInfoRow{}, err
	}
	row.IsMultiSpec, row.MultiQuantityDelivery = isMulti != 0, multiQty != 0
	return row, nil
}

// ListForUser 一次查询用户范围内的全部商品，可选按账号 ID 过滤。
func (i *Items) ListForUser(ctx context.Context, userID int64, cookieID string) ([]ItemInfoRow, error) {
	// rows、err 保存用户范围商品查询结果及错误。
	rows, err := i.DB.QueryContext(ctx,
		`SELECT i.id, i.cookie_id, i.item_id, COALESCE(i.item_title,''), COALESCE(i.item_description,''),
		        COALESCE(i.item_category,''), COALESCE(i.item_price,''), COALESCE(i.item_detail,''),
		        i.is_multi_spec, COALESCE(i.multi_quantity_delivery,0)
		 FROM item_info i JOIN cookies c ON c.id=i.cookie_id
		 WHERE c.user_id=? AND (?='' OR i.cookie_id=?) AND i.deleted_at IS NULL
		 ORDER BY i.id DESC`, userID, cookieID, cookieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItemInfoRows(rows)
}

// scanItemInfoRows 将商品查询游标转换为统一的商品行模型。
func scanItemInfoRows(rows *sql.Rows) ([]ItemInfoRow, error) {
	// out 用于本次流程后续判断的out
	var out []ItemInfoRow
	for rows.Next() {
		// r 用于本次流程后续判断的r
		var r ItemInfoRow
		// isMulti、multiQty 用于本次流程后续判断的isMulti、multiQty
		var isMulti, multiQty int
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&r.ID, &r.CookieID, &r.ItemID, &r.ItemTitle, &r.ItemDescription,
			&r.ItemCategory, &r.ItemPrice, &r.ItemDetail, &isMulti, &multiQty); err != nil {
			return nil, err
		}
		r.IsMultiSpec = isMulti != 0
		r.MultiQuantityDelivery = multiQty != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// Upsert 插入或更新商品信息。
func (i *Items) Upsert(ctx context.Context, r *ItemInfoRow) error {
	// err 用于本次流程后续判断的err
	_, err := i.DB.ExecContext(ctx,
		`INSERT INTO item_info (cookie_id, item_id, item_title, item_description,
		    item_category, item_price, item_detail, is_multi_spec, multi_quantity_delivery, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`+
			dialectUpsert(i.Dialect, []string{"cookie_id", "item_id"}, map[string]string{
				"item_title":              "EXCLUDED.item_title",
				"item_description":        "EXCLUDED.item_description",
				"item_category":           "EXCLUDED.item_category",
				"item_price":              "EXCLUDED.item_price",
				"item_detail":             "EXCLUDED.item_detail",
				"is_multi_spec":           "EXCLUDED.is_multi_spec",
				"multi_quantity_delivery": "EXCLUDED.multi_quantity_delivery",
				"deleted_at":              "NULL",
				"updated_at":              "CURRENT_TIMESTAMP",
			}),
		r.CookieID, r.ItemID, nullable(r.ItemTitle), nullable(r.ItemDescription),
		nullable(r.ItemCategory), nullable(r.ItemPrice), nullable(r.ItemDetail),
		boolToInt(r.IsMultiSpec), boolToInt(r.MultiQuantityDelivery))
	return err
}

// UpsertBasic 插入或补全商品基础信息，不覆盖已有的多规格/多数量发货设置。
func (i *Items) UpsertBasic(ctx context.Context, r *ItemInfoRow) error {
	return i.upsertBasic(ctx, i.DB, r)
}

// UpsertBasicTx 封装UpsertBasicTx业务协调。
func (i *Items) UpsertBasicTx(ctx context.Context, tx *sql.Tx, r *ItemInfoRow) error {
	return i.upsertBasic(ctx, tx, r)
}

// SyncFromRemote 将远端商品全集同步到本地。
//
// 远端列表只提供商品基础信息，因此保留本地的描述和发货配置；基础字段
// （标题、分类、价格、详情）由远端非空值覆盖。整个 reconcile 在一个事务内
// 完成，只有在全部远端商品写入成功后，才会逻辑删除本次全集中不存在的本地商品及其商品级自动化规则。
// SyncFromRemote 同步FromRemote。
func (i *Items) SyncFromRemote(ctx context.Context, cookieID string, rows []ItemInfoRow) (ItemSyncResult, error) {
	cookieID = strings.TrimSpace(cookieID)
	if cookieID == "" {
		return ItemSyncResult{}, errors.New("cookie_id 不能为空")
	}

	// remoteIDs 用于本次流程后续判断的remoteIDs
	remoteIDs := make(map[string]struct{}, len(rows))
	// validRows 用于本次流程后续判断的有效Rows
	validRows := make([]ItemInfoRow, 0, len(rows))
	// row 表示当前遍历过程中的row
	for _, row := range rows {
		row.CookieID = cookieID
		row.ItemID = strings.TrimSpace(row.ItemID)
		if row.ItemID == "" {
			continue
		}
		if // exists 用于本次流程后续判断的exists
		_, exists := remoteIDs[row.ItemID]; exists {
			continue
		}
		remoteIDs[row.ItemID] = struct{}{}
		validRows = append(validRows, row)
	}

	// tx、err 用于本次流程后续判断的tx、err
	tx, err := i.DB.BeginTx(ctx, nil)
	if err != nil {
		return ItemSyncResult{}, err
	}
	// rollback 用于本次流程后续判断的rollback
	rollback := func(err error) (ItemSyncResult, error) {
		_ = tx.Rollback()
		return ItemSyncResult{}, err
	}
	// index 表示当前遍历过程中的index
	for index := range validRows {
		if // err 用于本次流程后续判断的err
		err := i.UpsertBasicTx(ctx, tx, &validRows[index]); err != nil {
			return rollback(err)
		}
		if validRows[index].IsMultiSpec {
			if // err 用于本次流程后续判断的err
			_, err := tx.ExecContext(ctx,
				`UPDATE item_info SET is_multi_spec=? WHERE cookie_id=? AND item_id=?`,
				boolToInt(true), cookieID, validRows[index].ItemID); err != nil {
				return rollback(err)
			}
		}
	}

	// args 用于本次流程后续判断的args
	args := make([]any, 0, len(remoteIDs)+1)
	args = append(args, cookieID)
	// deleteQuery 用于本次流程后续判断的delete查询
	deleteQuery := `UPDATE item_info SET deleted_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND deleted_at IS NULL`
	if len(remoteIDs) > 0 {
		// placeholders 用于本次流程后续判断的placeholders
		placeholders := make([]string, 0, len(remoteIDs))
		// itemID 表示当前遍历过程中的商品ID
		for itemID := range remoteIDs {
			placeholders = append(placeholders, "?")
			args = append(args, itemID)
		}
		deleteQuery += ` AND item_id NOT IN (` + strings.Join(placeholders, ",") + ")"
	}
	// deletedResult、err 用于本次流程后续判断的deletedResult、err
	deletedResult, err := tx.ExecContext(ctx, deleteQuery, args...)
	if err != nil {
		return rollback(err)
	}
	// deleted、err 用于本次流程后续判断的deleted、err
	deleted, err := deletedResult.RowsAffected()
	if err != nil {
		return rollback(err)
	}

	// ruleArgs 用于本次流程后续判断的规则Args
	ruleArgs := make([]any, 0, len(remoteIDs)+1)
	ruleArgs = append(ruleArgs, cookieID)
	// ruleQuery 用于本次流程后续判断的规则查询
	ruleQuery := `UPDATE automation_rules
		SET deleted_at=CURRENT_TIMESTAMP, enabled=0, updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND item_id<>'' AND deleted_at IS NULL`
	if len(remoteIDs) > 0 {
		// placeholders 用于本次流程后续判断的placeholders
		placeholders := make([]string, 0, len(remoteIDs))
		// itemID 表示当前遍历过程中的商品ID
		for itemID := range remoteIDs {
			placeholders = append(placeholders, "?")
			ruleArgs = append(ruleArgs, itemID)
		}
		ruleQuery += ` AND item_id NOT IN (` + strings.Join(placeholders, ",") + ")"
	}
	if // err 用于本次流程后续判断的err
	_, err := tx.ExecContext(ctx, ruleQuery, ruleArgs...); err != nil {
		return rollback(err)
	}
	if // err 用于本次流程后续判断的err
	err := tx.Commit(); err != nil {
		return ItemSyncResult{}, err
	}
	return ItemSyncResult{Saved: len(validRows), Deleted: int(deleted)}, nil
}

// upsertBasic 封装upsertBasic业务协调。
func (i *Items) upsertBasic(ctx context.Context, execer sqlExecer, r *ItemInfoRow) error {
	// 三种数据库的条件 upsert：非空才覆盖，空值保留旧值。
	// SQLite/Postgres 用 EXCLUDED.col 引用插入值；MySQL 用 VALUES(col)。
	// conflictClause 用于本次流程后续判断的conflictClause
	var conflictClause string
	switch i.Dialect {
	case DialectMySQL:
		conflictClause = ` ON DUPLICATE KEY UPDATE
		   item_title=CASE WHEN VALUES(item_title) IS NOT NULL AND VALUES(item_title) != '' THEN VALUES(item_title) ELSE item_info.item_title END,
		   item_description=CASE WHEN VALUES(item_description) IS NOT NULL AND VALUES(item_description) != '' THEN VALUES(item_description) ELSE item_info.item_description END,
		   item_category=CASE WHEN VALUES(item_category) IS NOT NULL AND VALUES(item_category) != '' THEN VALUES(item_category) ELSE item_info.item_category END,
		   item_price=CASE WHEN VALUES(item_price) IS NOT NULL AND VALUES(item_price) != '' THEN VALUES(item_price) ELSE item_info.item_price END,
		   item_detail=CASE WHEN VALUES(item_detail) IS NOT NULL AND VALUES(item_detail) != '' THEN VALUES(item_detail) ELSE item_info.item_detail END,
		   deleted_at=NULL,
		   updated_at=CURRENT_TIMESTAMP`
	default:
		conflictClause = ` ON CONFLICT(cookie_id, item_id) DO UPDATE SET
		   item_title=CASE WHEN EXCLUDED.item_title IS NOT NULL AND EXCLUDED.item_title != '' THEN EXCLUDED.item_title ELSE item_info.item_title END,
		   item_description=CASE WHEN EXCLUDED.item_description IS NOT NULL AND EXCLUDED.item_description != '' THEN EXCLUDED.item_description ELSE item_info.item_description END,
		   item_category=CASE WHEN EXCLUDED.item_category IS NOT NULL AND EXCLUDED.item_category != '' THEN EXCLUDED.item_category ELSE item_info.item_category END,
		   item_price=CASE WHEN EXCLUDED.item_price IS NOT NULL AND EXCLUDED.item_price != '' THEN EXCLUDED.item_price ELSE item_info.item_price END,
		   item_detail=CASE WHEN EXCLUDED.item_detail IS NOT NULL AND EXCLUDED.item_detail != '' THEN EXCLUDED.item_detail ELSE item_info.item_detail END,
		   deleted_at=NULL,
		   updated_at=CURRENT_TIMESTAMP`
	}
	// err 用于本次流程后续判断的err
	_, err := execer.ExecContext(ctx,
		`INSERT INTO item_info (cookie_id, item_id, item_title, item_description,
		    item_category, item_price, item_detail, updated_at)
		 VALUES (?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`+conflictClause,
		r.CookieID, r.ItemID, nullable(r.ItemTitle), nullable(r.ItemDescription),
		nullable(r.ItemCategory), nullable(r.ItemPrice), nullable(r.ItemDetail))
	return err
}

// Delete 逻辑删除商品及其商品级自动化规则，保留历史数据以便审计和恢复。
func (i *Items) Delete(ctx context.Context, cookieID, itemID string) error {
	// tx、err 用于本次流程后续判断的tx、err
	tx, err := i.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if // err 用于本次流程后续判断的err
	_, err := tx.ExecContext(ctx, `UPDATE item_info
		SET deleted_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND item_id=? AND deleted_at IS NULL`, cookieID, itemID); err != nil {
		return err
	}
	if // err 用于本次流程后续判断的err
	_, err := tx.ExecContext(ctx, `UPDATE automation_rules
		SET deleted_at=CURRENT_TIMESTAMP, enabled=0, updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND item_id=? AND deleted_at IS NULL`, cookieID, itemID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetMultiSpec 设置多规格开关。
func (i *Items) SetMultiSpec(ctx context.Context, cookieID, itemID string, on bool) error {
	// err 用于本次流程后续判断的err
	_, err := i.DB.ExecContext(ctx,
		`UPDATE item_info SET is_multi_spec=?, updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND item_id=? AND deleted_at IS NULL`,
		boolToInt(on), cookieID, itemID)
	return err
}

// SetMultiQuantity 设置多数量发货开关。
func (i *Items) SetMultiQuantity(ctx context.Context, cookieID, itemID string, on bool) error {
	// err 用于本次流程后续判断的err
	_, err := i.DB.ExecContext(ctx,
		`UPDATE item_info SET multi_quantity_delivery=?, updated_at=CURRENT_TIMESTAMP WHERE cookie_id=? AND item_id=? AND deleted_at IS NULL`,
		boolToInt(on), cookieID, itemID)
	return err
}
