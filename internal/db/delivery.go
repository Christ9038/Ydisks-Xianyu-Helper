package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ItemInfo 对应 item_info 表。
type ItemInfo struct {
	ID                    int64
	CookieID              string
	ItemID                string
	ItemTitle             string
	ItemDescription       string
	ItemCategory          string
	ItemPrice             string
	ItemDetail            string
	IsMultiSpec           bool
	MultiQuantityDelivery bool
}

// Card 对应 cards 表（发货用字段）。
type Card struct {
	ID           int64
	Name         string
	Type         string // api/text/data/image
	APIConfig    string // JSON
	TextContent  string
	DataContent  string
	ImageURL     string
	Description  string
	Enabled      bool
	DelaySeconds int
	IsMultiSpec  bool
	SpecName     string
	SpecValue    string
	UserID       int64
}

// Items 商品信息操作。
type Items struct {
	DB      *sql.DB
	Dialect Dialect
}

// Get 取某账号下某商品信息。不存在返回 ErrNotFound。
func (i *Items) Get(ctx context.Context, cookieID, itemID string) (*ItemInfo, error) {
	// it 用于本次流程后续判断的it
	var it ItemInfo
	// isMulti、multiQty 用于本次流程后续判断的isMulti、multiQty
	var isMulti, multiQty int
	// title、desc、cat、price、detail 用于本次流程后续判断的title、desc、cat、price、detail
	var title, desc, cat, price, detail sql.NullString
	// err 用于本次流程后续判断的err
	err := i.DB.QueryRowContext(ctx,
		`SELECT id, cookie_id, item_id, item_title, item_description, item_category, item_price, item_detail,
		        is_multi_spec, multi_quantity_delivery
		 FROM item_info WHERE cookie_id=? AND item_id=? AND deleted_at IS NULL`, cookieID, itemID).Scan(
		&it.ID, &it.CookieID, &it.ItemID, &title, &desc, &cat,
		&price, &detail, &isMulti, &multiQty)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	it.ItemTitle = title.String
	it.ItemDescription = desc.String
	it.ItemCategory = cat.String
	it.ItemPrice = price.String
	it.ItemDetail = detail.String
	it.IsMultiSpec = isMulti != 0
	it.MultiQuantityDelivery = multiQty != 0
	return &it, nil
}

// IsMultiSpec 是否多规格商品。
func (i *Items) IsMultiSpec(ctx context.Context, cookieID, itemID string) bool {
	// v 用于本次流程后续判断的v
	var v int
	// err 用于本次流程后续判断的err
	err := i.DB.QueryRowContext(ctx, `SELECT is_multi_spec FROM item_info WHERE cookie_id=? AND item_id=? AND deleted_at IS NULL`, cookieID, itemID).Scan(&v)
	if err != nil {
		return false
	}
	return v != 0
}

// MultiSpecFlags 批量读取账号商品的多规格标记，避免同步时逐商品查询数据库。
func (i *Items) MultiSpecFlags(ctx context.Context, cookieID string, itemIDs []string) (map[string]bool, error) {
	// uniqueIDs 保存去重后的商品标识，避免重复占用 SQL 参数。
	uniqueIDs := make([]string, 0, len(itemIDs))
	// seenIDs 保存已经加入查询的商品标识。
	seenIDs := make(map[string]struct{}, len(itemIDs))
	// itemID 表示当前待查询的商品标识。
	for _, itemID := range itemIDs {
		itemID = strings.TrimSpace(itemID)
		if itemID == "" {
			continue
		}
		// exists 表示当前商品标识是否已经加入查询集合。
		if _, exists := seenIDs[itemID]; exists {
			continue
		}
		seenIDs[itemID] = struct{}{}
		uniqueIDs = append(uniqueIDs, itemID)
	}
	// flags 保存已存在商品的多规格标记；缺失商品不会出现在结果中。
	flags := make(map[string]bool, len(uniqueIDs))
	if len(uniqueIDs) == 0 {
		return flags, nil
	}
	// placeholders 保存 IN 子句的参数占位符。
	placeholders := make([]string, len(uniqueIDs))
	// args 保存账号和商品查询参数。
	args := make([]any, 0, len(uniqueIDs)+1)
	args = append(args, cookieID)
	// index、itemID 分别表示占位符下标和当前商品标识。
	for index, itemID := range uniqueIDs {
		placeholders[index] = "?"
		args = append(args, itemID)
	}
	// rows、err 保存批量标记查询结果及错误。
	rows, err := i.DB.QueryContext(ctx,
		`SELECT item_id, is_multi_spec FROM item_info WHERE cookie_id=? AND deleted_at IS NULL AND item_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		// itemID、isMultiSpec 保存当前商品标识及多规格值。
		var itemID string
		// isMultiSpec 保存数据库中的多规格整数标记。
		var isMultiSpec int
		// err 表示扫描当前商品标记时的数据库错误。
		if err := rows.Scan(&itemID, &isMultiSpec); err != nil {
			return nil, err
		}
		flags[itemID] = isMultiSpec != 0
	}
	// err 表示遍历批量标记结果时的数据库错误。
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return flags, nil
}

// MultiQuantityDelivery 是否开启多数量发货。
func (i *Items) MultiQuantityDelivery(ctx context.Context, cookieID, itemID string) bool {
	// v 用于本次流程后续判断的v
	var v int
	// err 用于本次流程后续判断的err
	err := i.DB.QueryRowContext(ctx, `SELECT multi_quantity_delivery FROM item_info WHERE cookie_id=? AND item_id=? AND deleted_at IS NULL`, cookieID, itemID).Scan(&v)
	if err != nil {
		return false
	}
	return v != 0
}

// Cards 卡券操作。
type Cards struct {
	DB      *sql.DB
	Dialect Dialect
	// codec 负责 API 卡请求模板的静态加密与解密；只有完整卡券读取和写入路径使用它。
	codec *secretCodec
}

// ConsumeBatchData 原子预留一条批量数据卡券（data 类型），返回内容。
// 通过快照条件更新处理并发追加/消费；调用方发送失败时应调用 RestoreBatchData。
// ConsumeBatchData 封装Consume批次数据业务协调。
func (c *Cards) ConsumeBatchData(ctx context.Context, cardID int64) (string, error) {
	for // attempts 用于本次流程后续判断的尝试次数
	attempts := 0; attempts < 20; attempts++ {
		// dataContent 用于本次流程后续判断的数据内容
		var dataContent sql.NullString
		// err 用于本次流程后续判断的err
		err := c.DB.QueryRowContext(ctx, `SELECT data_content FROM cards WHERE id=?`, cardID).Scan(&dataContent)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", ErrNotFound
			}
			return "", err
		}
		if !dataContent.Valid || dataContent.String == "" {
			return "", errors.New("卡券批量数据为空")
		}
		// lines 用于本次流程后续判断的lines
		lines := splitLines(dataContent.String)
		if len(lines) == 0 {
			return "", errors.New("卡券批量数据无有效行")
		}
		// remaining 用于本次流程后续判断的remaining
		remaining := ""
		if len(lines) > 1 {
			remaining = joinLines(lines[1:])
		}
		// res、err 用于本次流程后续判断的res、err
		res, err := c.DB.ExecContext(ctx,
			`UPDATE cards SET data_content=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND data_content=?`,
			remaining, cardID, dataContent.String)
		if err != nil {
			return "", err
		}
		if // affected、err 用于本次流程后续判断的affected、err
		affected, err := res.RowsAffected(); err == nil && affected == 1 {
			return lines[0], nil
		}
	}
	return "", errors.New("卡券库存并发修改过于频繁，请稍后重试")
}

// RestoreBatchData 把发送失败的预留卡密放回库存头部。恢复失败时宁可进入人工处理，
// 也不能把一个已成功发送但响应不确定的卡密自动重复发给下一位买家。
// RestoreBatchData 封装Restore批次数据业务协调。
func (c *Cards) RestoreBatchData(ctx context.Context, cardID int64, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return errors.New("恢复卡密内容为空")
	}
	for // attempts 用于本次流程后续判断的尝试次数
	attempts := 0; attempts < 20; attempts++ {
		// current 用于本次流程后续判断的current
		var current sql.NullString
		if // err 用于本次流程后续判断的err
		err := c.DB.QueryRowContext(ctx, `SELECT data_content FROM cards WHERE id=?`, cardID).Scan(&current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		// merged 用于本次流程后续判断的merged
		merged := content
		if current.Valid && strings.TrimSpace(current.String) != "" {
			merged += "\n" + current.String
		}
		// res、err 用于本次流程后续判断的res、err
		res, err := c.DB.ExecContext(ctx,
			`UPDATE cards SET data_content=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND COALESCE(data_content,'')=?`,
			merged, cardID, current.String)
		if err != nil {
			return err
		}
		if // affected、err 用于本次流程后续判断的affected、err
		affected, err := res.RowsAffected(); err == nil && affected == 1 {
			return nil
		}
	}
	return errors.New("恢复卡密时库存并发修改过于频繁")
}

// FirstBatchData 返回 data 类型卡券当前第一条有效内容和原始快照。
// 调用方发送成功后应调用 CommitFirstBatchData 删除同一快照中的第一条，避免发送失败丢卡。
// FirstBatchData 封装First批次数据业务协调。
func (c *Cards) FirstBatchData(ctx context.Context, cardID int64) (content, snapshot string, err error) {
	// dataContent 用于本次流程后续判断的数据内容
	var dataContent sql.NullString
	err = c.DB.QueryRowContext(ctx, `SELECT data_content FROM cards WHERE id=?`, cardID).Scan(&dataContent)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	if !dataContent.Valid || dataContent.String == "" {
		return "", "", errors.New("卡券批量数据为空")
	}
	// lines 用于本次流程后续判断的lines
	lines := splitLines(dataContent.String)
	if len(lines) == 0 {
		return "", "", errors.New("卡券批量数据无有效行")
	}
	return lines[0], dataContent.String, nil
}

// CommitFirstBatchData 在 data_content 仍等于 snapshot 时删除第一条有效内容。
// 条件更新失败表示库存被并发修改，调用方应停止本次发货，避免错删卡密。
// CommitFirstBatchData 封装CommitFirst批次数据业务协调。
func (c *Cards) CommitFirstBatchData(ctx context.Context, cardID int64, snapshot string) error {
	// lines 用于本次流程后续判断的lines
	lines := splitLines(snapshot)
	if len(lines) == 0 {
		return errors.New("卡券批量数据无有效行")
	}
	// remaining 用于本次流程后续判断的remaining
	remaining := ""
	if len(lines) > 1 {
		remaining = joinLines(lines[1:])
	}
	// res、err 用于本次流程后续判断的res、err
	res, err := c.DB.ExecContext(ctx, `UPDATE cards SET data_content=? WHERE id=? AND data_content=?`, remaining, cardID, snapshot)
	if err != nil {
		return err
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return errors.New("卡券库存已被并发修改，请重试")
	}
	return nil
}

// AppendBatchData 往 data 类型卡密组追加卡密号（按行）。返回新增的有效（非空）行数。
// 已有 data_content 非空时在末尾换行追加，否则直接写入。
// AppendBatchData 封装Append批次数据业务协调。
func (c *Cards) AppendBatchData(ctx context.Context, cardID int64, content string) (int, error) {
	// lines 用于本次流程后续判断的lines
	lines := splitLines(content)
	// valid 用于本次流程后续判断的有效
	valid := 0
	// l 表示当前遍历过程中的l
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			valid++
		}
	}
	if valid == 0 {
		return 0, errors.New("无有效卡密行")
	}
	for // attempts 用于本次流程后续判断的尝试次数
	attempts := 0; attempts < 20; attempts++ {
		// existing 用于本次流程后续判断的existing
		var existing sql.NullString
		// err 用于本次流程后续判断的err
		err := c.DB.QueryRowContext(ctx, `SELECT data_content FROM cards WHERE id=?`, cardID).Scan(&existing)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, ErrNotFound
			}
			return 0, err
		}
		// merged 用于本次流程后续判断的merged
		merged := content
		if existing.Valid && existing.String != "" {
			merged = existing.String + "\n" + content
		}
		// res、err 用于本次流程后续判断的res、err
		res, err := c.DB.ExecContext(ctx,
			`UPDATE cards SET data_content=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND COALESCE(data_content,'')=?`,
			merged, cardID, existing.String)
		if err != nil {
			return 0, err
		}
		if // affected、err 用于本次流程后续判断的affected、err
		affected, err := res.RowsAffected(); err == nil && affected == 1 {
			return valid, nil
		}
	}
	return 0, errors.New("追加卡密时库存并发修改过于频繁")
}

// splitLines / joinLines 统一处理多行库存内容。
func splitLines(s string) []string {
	// out 用于本次流程后续判断的out
	var out []string
	// cur 用于本次流程后续判断的cur
	cur := []rune{}
	// r 表示当前遍历过程中的r
	for _, r := range s {
		switch r {
		case '\n':
			out = append(out, string(cur))
			cur = cur[:0]
		case '\r':
			out = append(out, string(cur))
			cur = cur[:0]
			// 跳过 \r，处理 \r\n
		default:
			cur = append(cur, r)
		}
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	// 过滤空行。
	res := out[:0]
	// l 表示当前遍历过程中的l
	for _, l := range out {
		if strings.TrimSpace(l) != "" {
			res = append(res, l)
		}
	}
	return res
}

// joinLines 封装joinLines业务协调。
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	// out 用于本次流程后续判断的out
	out := lines[0]
	// l 表示当前遍历过程中的l
	for _, l := range lines[1:] {
		out += "\n" + l
	}
	return out
}
