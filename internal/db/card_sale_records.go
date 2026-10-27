package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	// SaleStatusReserved 表示卡密已从库存扣除但买家是否收到尚未确认，需要人工核对或等待运行收口。
	SaleStatusReserved = "reserved"
	// SaleStatusSold 表示卡密已确认发给买家。
	SaleStatusSold = "sold"
)

// CardSaleMeta 描述一次自动发货消费卡密时的订单上下文，随台账记录一起保存，便于按订单核对。
type CardSaleMeta struct {
	// RunID 是触发本次消费的自动化运行标识；独立动作测试或人工路径可以为零。
	RunID int64
	// CookieID 是订单所属闲鱼账号标识，与订单归属一起用于隔离不同账号的同号订单。
	CookieID string
	// OrderID 是平台订单号，订单详情用它查询本单已发出的卡密。
	OrderID string
	// ItemID 是订单商品标识，仅用于导出核对。
	ItemID string
	// BuyerID 是买家标识，仅用于导出核对。
	BuyerID string
}

// CardSaleRecord 是售出台账中的一条记录，Content 为已发出的卡密明文，只允许所有者经 HTTP 读取。
type CardSaleRecord struct {
	// ID 是台账记录主键。
	ID int64
	// UserID 是卡密组所有者，所有查询都必须按它隔离。
	UserID int64
	// CardID 是消费时的卡密组标识；卡密组被删除后记录仍保留该值。
	CardID int64
	// CardName 与 CardType 是消费时的卡密组名称和类型快照，卡密组改名或删除后历史仍可读。
	CardName string
	CardType string
	// Content 是实际发给买家的卡密正文或 API 返回内容。
	Content string
	// Status 取 SaleStatusReserved 或 SaleStatusSold。
	Status string
	// RunID、CookieID、OrderID、ItemID、BuyerID 是消费时的订单上下文。
	RunID    int64
	CookieID string
	OrderID  string
	ItemID   string
	BuyerID  string
	// CreatedAt 是扣减库存时刻（Unix 秒）；SoldAt 是确认售出时刻（Unix 秒），未确认时为零。
	CreatedAt int64
	SoldAt    int64
}

// CardSaleQuery 描述售出台账的分页查询条件。
type CardSaleQuery struct {
	// UserID 是必填的所有者隔离条件。
	UserID int64
	// CardID 为零时不限卡密组。
	CardID int64
	// Status 为空时不限状态，否则必须是台账状态之一。
	Status string
	// Search 按订单号精确匹配或卡密内容子串匹配；为空不过滤。
	Search string
	// Limit 与 Offset 控制分页，Limit 小于等于零时取 50 且最大 200。
	Limit  int
	Offset int
}

// CardSalePage 是一页台账记录及符合条件的总数。
type CardSalePage struct {
	// Records 是当前页记录，按 id 倒序。
	Records []CardSaleRecord
	// Total 是符合条件的记录总数。
	Total int64
}

// saleRecordColumns 是读取台账记录时统一使用的列顺序，必须与 scanSaleRecord 保持一致。
const saleRecordColumns = `id,user_id,card_id,card_name,card_type,content,status,run_id,cookie_id,order_id,item_id,buyer_id,created_at,sold_at`

// scanSaleRecord 按 saleRecordColumns 的顺序读取一行台账；scan 是 Rows.Scan 或 Row.Scan。
func scanSaleRecord(scan func(dest ...any) error) (CardSaleRecord, error) {
	// record 保存读取到的台账记录。
	var record CardSaleRecord
	// scanErr 保存读取行的数据库错误。
	scanErr := scan(&record.ID, &record.UserID, &record.CardID, &record.CardName, &record.CardType, &record.Content,
		&record.Status, &record.RunID, &record.CookieID, &record.OrderID, &record.ItemID, &record.BuyerID,
		&record.CreatedAt, &record.SoldAt)
	return record, scanErr
}

// insertSaleRecord 在 exec 所在连接或事务内写入一条台账记录并返回主键；status 必须是合法状态。
func insertSaleRecord(ctx context.Context, exec DBTX, dialect Dialect, record CardSaleRecord) (int64, error) {
	// soldAt 只有已售记录才有确认时间。
	soldAt := int64(0)
	if record.Status == SaleStatusSold {
		soldAt = record.CreatedAt
	}
	return insertReturningID(ctx, exec, dialect,
		`INSERT INTO card_sale_records (user_id,card_id,card_name,card_type,content,status,run_id,cookie_id,order_id,item_id,buyer_id,created_at,sold_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		record.UserID, record.CardID, record.CardName, record.CardType, record.Content, record.Status,
		record.RunID, record.CookieID, record.OrderID, record.ItemID, record.BuyerID, record.CreatedAt, soldAt)
}

// ReserveBatchData 原子扣减 data 卡密组的第一条卡密，并在同一事务中写入 reserved 台账。
// meta 是订单上下文；返回扣出的卡密内容和台账记录标识。发送确定失败时必须调用 ReleaseBatchData，
// 发送成功后调用 MarkSaleRecordsSold；结果不确定时保持 reserved，由运行收口或人工核对处理。
// 并发追加或消费造成的 CAS 冲突最多重试 20 次。
func (c *Cards) ReserveBatchData(ctx context.Context, cardID int64, meta CardSaleMeta) (string, int64, error) {
	for /* attempts 是并发冲突重试次数 */ attempts := 0; attempts < 20; attempts++ {
		// content、recordID、retry、err 分别是扣出的卡密、台账标识、是否因并发冲突需重试以及错误。
		content, recordID, retry, err := c.tryReserveBatchData(ctx, cardID, meta)
		if err != nil || !retry {
			return content, recordID, err
		}
	}
	return "", 0, errors.New("卡券库存并发修改过于频繁，请稍后重试")
}

// tryReserveBatchData 执行一次事务内的扣库存加记账；retry 为 true 表示 CAS 未命中，调用方应重试。
func (c *Cards) tryReserveBatchData(ctx context.Context, cardID int64, meta CardSaleMeta) (content string, recordID int64, retry bool, err error) {
	// tx、beginErr 保存扣库存与记账共用的事务及开启错误。
	tx, beginErr := c.DB.BeginTx(ctx, nil)
	if beginErr != nil {
		return "", 0, false, beginErr
	}
	defer func() { _ = tx.Rollback() }()
	// dataContent、name、cardType、userID 保存卡密组当前库存快照及台账需要的快照字段。
	var dataContent sql.NullString
	// name、cardType 保存卡密组名称与类型快照。
	var name, cardType string
	// userID 保存卡密组所有者，台账按所有者隔离。
	var userID int64
	// scanErr 保存读取库存快照的错误，无行时映射为 ErrNotFound。
	scanErr := tx.QueryRowContext(ctx, `SELECT data_content,name,type,user_id FROM cards WHERE id=?`, cardID).Scan(&dataContent, &name, &cardType, &userID)
	if scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return "", 0, false, ErrNotFound
		}
		return "", 0, false, scanErr
	}
	if !dataContent.Valid || dataContent.String == "" {
		return "", 0, false, errors.New("卡券批量数据为空")
	}
	// lines 是库存按行拆分后的有效卡密。
	lines := splitLines(dataContent.String)
	if len(lines) == 0 {
		return "", 0, false, errors.New("卡券批量数据无有效行")
	}
	// remaining 是扣除第一条后的库存正文。
	remaining := ""
	if len(lines) > 1 {
		remaining = joinLines(lines[1:])
	}
	// res、execErr 保存带库存快照守卫的更新结果。
	res, execErr := tx.ExecContext(ctx,
		`UPDATE cards SET data_content=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND data_content=?`,
		remaining, cardID, dataContent.String)
	if execErr != nil {
		return "", 0, false, execErr
	}
	// affected、affectedErr 保存库存更新命中行数及读取错误；未命中说明库存已被并发修改。
	if affected, affectedErr := res.RowsAffected(); affectedErr != nil || affected != 1 {
		return "", 0, affectedErr == nil, affectedErr
	}
	// id、insertErr 保存 reserved 台账主键及写入错误；失败时整个事务回滚，库存不被扣减。
	id, insertErr := insertSaleRecord(ctx, tx, c.Dialect, CardSaleRecord{
		UserID: userID, CardID: cardID, CardName: name, CardType: cardType, Content: lines[0], Status: SaleStatusReserved,
		RunID: meta.RunID, CookieID: meta.CookieID, OrderID: meta.OrderID, ItemID: meta.ItemID, BuyerID: meta.BuyerID,
		CreatedAt: time.Now().UTC().Unix(),
	})
	if insertErr != nil {
		return "", 0, false, insertErr
	}
	// commitErr 保存事务提交错误，提交失败时库存与台账一并回滚。
	if commitErr := tx.Commit(); commitErr != nil {
		return "", 0, false, commitErr
	}
	return lines[0], id, false, nil
}

// ReleaseBatchData 把确定未发送的卡密放回库存头部并删除对应 reserved 台账，两者在同一事务内完成。
// recordID 为零时只恢复库存（历史调用方没有台账）。恢复失败时调用方必须转人工核对，不能重复取卡。
func (c *Cards) ReleaseBatchData(ctx context.Context, cardID, recordID int64, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return errors.New("恢复卡密内容为空")
	}
	for /* attempts 是并发冲突重试次数 */ attempts := 0; attempts < 20; attempts++ {
		// retry、err 表示库存快照是否冲突以及事务错误。
		retry, err := c.tryReleaseBatchData(ctx, cardID, recordID, content)
		if err != nil || !retry {
			return err
		}
	}
	return errors.New("恢复卡密时库存并发修改过于频繁")
}

// tryReleaseBatchData 执行一次事务内的库存恢复与台账删除；retry 为 true 表示库存快照冲突。
func (c *Cards) tryReleaseBatchData(ctx context.Context, cardID, recordID int64, content string) (retry bool, err error) {
	// tx、beginErr 保存恢复库存与删除台账共用的事务及开启错误。
	tx, beginErr := c.DB.BeginTx(ctx, nil)
	if beginErr != nil {
		return false, beginErr
	}
	defer func() { _ = tx.Rollback() }()
	// current 保存卡密组当前库存快照。
	var current sql.NullString
	// scanErr 保存读取当前库存的错误，无行时映射为 ErrNotFound。
	if scanErr := tx.QueryRowContext(ctx, `SELECT data_content FROM cards WHERE id=?`, cardID).Scan(&current); scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, scanErr
	}
	// merged 是恢复后的库存正文，被恢复的卡密位于头部。
	merged := content
	if current.Valid && strings.TrimSpace(current.String) != "" {
		merged += "\n" + current.String
	}
	// res、execErr 保存带库存快照守卫的恢复更新结果。
	res, execErr := tx.ExecContext(ctx,
		`UPDATE cards SET data_content=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND COALESCE(data_content,'')=?`,
		merged, cardID, current.String)
	if execErr != nil {
		return false, execErr
	}
	// affected、affectedErr 保存恢复更新命中行数及读取错误；未命中说明库存快照冲突需重试。
	if affected, affectedErr := res.RowsAffected(); affectedErr != nil || affected != 1 {
		return affectedErr == nil, affectedErr
	}
	if recordID > 0 {
		// deleteErr 保存删除 reserved 台账的错误，失败时库存恢复一并回滚。
		if _, deleteErr := tx.ExecContext(ctx, `DELETE FROM card_sale_records WHERE id=? AND status=?`, recordID, SaleStatusReserved); deleteErr != nil {
			return false, deleteErr
		}
	}
	return false, tx.Commit()
}

// RecordAPISale 为 API 卡写入一条台账：status 为 SaleStatusSold 表示已发送成功，SaleStatusReserved 表示结果不确定。
// API 卡没有本地库存，台账写入失败由调用方记录日志但不得影响已完成的发货；content 不得写入日志。
func (c *Cards) RecordAPISale(ctx context.Context, cardID int64, meta CardSaleMeta, content, status string) (int64, error) {
	if status != SaleStatusSold && status != SaleStatusReserved {
		return 0, errors.New("售出状态无效")
	}
	// name、cardType、userID 保存卡密组当前快照。
	var name, cardType string
	// userID 保存卡密组所有者。
	var userID int64
	// scanErr 保存读取卡密组快照的错误，无行时映射为 ErrNotFound。
	if scanErr := c.DB.QueryRowContext(ctx, `SELECT name,type,user_id FROM cards WHERE id=?`, cardID).Scan(&name, &cardType, &userID); scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, scanErr
	}
	return insertSaleRecord(ctx, c.DB, c.Dialect, CardSaleRecord{
		UserID: userID, CardID: cardID, CardName: name, CardType: cardType, Content: content, Status: status,
		RunID: meta.RunID, CookieID: meta.CookieID, OrderID: meta.OrderID, ItemID: meta.ItemID, BuyerID: meta.BuyerID,
		CreatedAt: time.Now().UTC().Unix(),
	})
}

// MarkSaleRecordsSold 把指定的 reserved 台账确认为已售；已是 sold 或不存在的标识被忽略，因此可安全重复调用。
func (c *Cards) MarkSaleRecordsSold(ctx context.Context, ids ...int64) error {
	// valid 保存大于零的有效台账标识。
	valid := make([]any, 0, len(ids)+2)
	valid = append(valid, SaleStatusSold, time.Now().UTC().Unix())
	// placeholders 保存 IN 子句占位符。
	placeholders := make([]string, 0, len(ids))
	for /* id 是调用方传入的台账标识 */ _, id := range ids {
		if id > 0 {
			valid = append(valid, id)
			placeholders = append(placeholders, "?")
		}
	}
	if len(placeholders) == 0 {
		return nil
	}
	valid = append(valid, SaleStatusReserved)
	// err 保存批量确认售出的更新错误。
	_, err := c.DB.ExecContext(ctx,
		`UPDATE card_sale_records SET status=?,sold_at=? WHERE id IN (`+strings.Join(placeholders, ",")+`) AND status=?`, valid...)
	return err
}

// ListSaleRecords 按 query 分页读取售出台账，所有查询必须带 UserID。
func (c *Cards) ListSaleRecords(ctx context.Context, query CardSaleQuery) (CardSalePage, error) {
	// where、args 是动态拼接的过滤条件及其参数，值一律走占位符。
	where, args := saleRecordFilter(query)
	// page 保存读取结果。
	var page CardSalePage
	// scanErr 保存统计符合条件记录总数的错误。
	if scanErr := c.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM card_sale_records WHERE `+where, args...).Scan(&page.Total); scanErr != nil {
		return CardSalePage{}, scanErr
	}
	// limit 把分页大小限制在 1 到 200，默认 50。
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	// offset 是分页偏移，负数按 0 处理。
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	// rows、queryErr 保存当前页查询结果。
	rows, queryErr := c.DB.QueryContext(ctx,
		`SELECT `+saleRecordColumns+` FROM card_sale_records WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if queryErr != nil {
		return CardSalePage{}, queryErr
	}
	defer rows.Close()
	for rows.Next() {
		// record、scanErr 保存当前行解析出的台账记录及解析错误。
		record, scanErr := scanSaleRecord(rows.Scan)
		if scanErr != nil {
			return CardSalePage{}, scanErr
		}
		page.Records = append(page.Records, record)
	}
	return page, rows.Err()
}

// saleRecordFilter 把查询条件转换为 WHERE 片段和参数；搜索词中的 LIKE 通配符会被转义。
func saleRecordFilter(query CardSaleQuery) (string, []any) {
	// clauses、args 依次保存过滤片段和对应参数。
	clauses := []string{"user_id=?"}
	// args 保存与 clauses 一一对应的占位符参数，首项为所有者标识。
	args := []any{query.UserID}
	if query.CardID > 0 {
		clauses = append(clauses, "card_id=?")
		args = append(args, query.CardID)
	}
	if query.Status != "" {
		clauses = append(clauses, "status=?")
		args = append(args, query.Status)
	}
	// search 是去掉首尾空白后的搜索词，为空则不加搜索条件。
	if search := strings.TrimSpace(query.Search); search != "" {
		// pattern 是转义后的 LIKE 子串模式。
		pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(search) + "%"
		clauses = append(clauses, `(order_id=? OR content LIKE ? ESCAPE '!')`)
		args = append(args, search, pattern)
	}
	return strings.Join(clauses, " AND "), args
}

// ListSaleRecordsByOrder 返回某账号某订单已发出的全部台账，按发出顺序排列；userID 防止跨用户读取。
func (c *Cards) ListSaleRecordsByOrder(ctx context.Context, userID int64, cookieID, orderID string) ([]CardSaleRecord, error) {
	// rows、queryErr 保存按订单读取台账的结果。
	rows, queryErr := c.DB.QueryContext(ctx,
		`SELECT `+saleRecordColumns+` FROM card_sale_records WHERE user_id=? AND cookie_id=? AND order_id=? ORDER BY id`,
		userID, cookieID, orderID)
	if queryErr != nil {
		return nil, queryErr
	}
	defer rows.Close()
	// out 保存读取到的台账记录。
	var out []CardSaleRecord
	for rows.Next() {
		// record、scanErr 保存当前行解析出的台账记录及解析错误。
		record, scanErr := scanSaleRecord(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

// SaleCounts 返回用户每个卡密组已确认售出的记录数，键为卡密组标识；只统计 sold，不含待核对。
func (c *Cards) SaleCounts(ctx context.Context, userID int64) (map[int64]int64, error) {
	// rows、queryErr 保存分组统计结果。
	rows, queryErr := c.DB.QueryContext(ctx,
		`SELECT card_id,COUNT(*) FROM card_sale_records WHERE user_id=? AND status=? GROUP BY card_id`, userID, SaleStatusSold)
	if queryErr != nil {
		return nil, queryErr
	}
	defer rows.Close()
	// counts 保存卡密组到已售数量的映射。
	counts := make(map[int64]int64)
	for rows.Next() {
		// cardID、count 是当前分组的卡密组标识和已售数量。
		var cardID, count int64
		// scanErr 保存读取分组统计行的错误。
		if scanErr := rows.Scan(&cardID, &count); scanErr != nil {
			return nil, scanErr
		}
		counts[cardID] = count
	}
	return counts, rows.Err()
}

// AllSaleRecords 按售出先后（id 升序）返回卡密组的全部台账，供导出使用；userID 与 cardID 都必须匹配。
func (c *Cards) AllSaleRecords(ctx context.Context, userID, cardID int64) ([]CardSaleRecord, error) {
	// rows、queryErr 保存导出查询结果。
	rows, queryErr := c.DB.QueryContext(ctx,
		`SELECT `+saleRecordColumns+` FROM card_sale_records WHERE user_id=? AND card_id=? ORDER BY id`, userID, cardID)
	if queryErr != nil {
		return nil, queryErr
	}
	defer rows.Close()
	// out 保存读取到的台账记录。
	var out []CardSaleRecord
	for rows.Next() {
		// record、scanErr 保存当前行解析出的台账记录及解析错误。
		record, scanErr := scanSaleRecord(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

// finishRunTail 完成运行终态事务：先核对归属（已被其他持有者接管则回滚），成功运行再确认本次遗留的 reserved 台账，最后提交。
// res 是运行终态更新结果，status 是终态，runID 是运行标识；成功意味着买家已收到全部内容。
func finishRunTail(ctx context.Context, tx *sql.Tx, res sql.Result, status string, runID int64) error {
	// ownerErr 保存运行归属校验错误，运行被其他持有者接管时回滚终态更新。
	if ownerErr := requireAutomationRunOwner(res); ownerErr != nil {
		return ownerErr
	}
	if status == "success" {
		// settleErr 保存确认遗留 reserved 台账的错误，失败时整个终态事务回滚。
		if _, settleErr := tx.ExecContext(ctx, `UPDATE card_sale_records SET status=?,sold_at=? WHERE run_id=? AND status=?`,
			SaleStatusSold, time.Now().UTC().Unix(), runID, SaleStatusReserved); settleErr != nil {
			return settleErr
		}
	}
	return tx.Commit()
}
