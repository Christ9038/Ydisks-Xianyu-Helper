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
	var it ItemInfo
	var isMulti, multiQty int
	var title, desc, cat, price, detail sql.NullString
	err := i.DB.QueryRowContext(ctx,
		`SELECT id, cookie_id, item_id, item_title, item_description, item_category, item_price, item_detail,
		        is_multi_spec, multi_quantity_delivery
		 FROM item_info WHERE cookie_id=? AND item_id=?`, cookieID, itemID).Scan(
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
	var v int
	err := i.DB.QueryRowContext(ctx, `SELECT is_multi_spec FROM item_info WHERE cookie_id=? AND item_id=?`, cookieID, itemID).Scan(&v)
	if err != nil {
		return false
	}
	return v != 0
}

// MultiQuantityDelivery 是否开启多数量发货。
func (i *Items) MultiQuantityDelivery(ctx context.Context, cookieID, itemID string) bool {
	var v int
	err := i.DB.QueryRowContext(ctx, `SELECT multi_quantity_delivery FROM item_info WHERE cookie_id=? AND item_id=?`, cookieID, itemID).Scan(&v)
	if err != nil {
		return false
	}
	return v != 0
}

// Cards 卡券操作。
type Cards struct {
	DB      *sql.DB
	Dialect Dialect
}

// ConsumeBatchData 消费一条批量数据卡券（data 类型），返回内容。
// ConsumeBatchData 取 data_content 第一行，删除已消费行，发完置空。
func (c *Cards) ConsumeBatchData(ctx context.Context, cardID int64) (string, error) {
	tx, err := c.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var dataContent sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT data_content FROM cards WHERE id=?`, cardID).Scan(&dataContent)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	if !dataContent.Valid || dataContent.String == "" {
		return "", errors.New("卡券批量数据为空")
	}

	// 按行分割，取第一行非空作为发货内容，其余回写。
	lines := splitLines(dataContent.String)
	if len(lines) == 0 {
		return "", errors.New("卡券批量数据无有效行")
	}
	content := lines[0]
	remaining := ""
	if len(lines) > 1 {
		remaining = joinLines(lines[1:])
	}
	if _, err := tx.ExecContext(ctx, `UPDATE cards SET data_content=? WHERE id=?`, remaining, cardID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return content, nil
}

// FirstBatchData 返回 data 类型卡券当前第一条有效内容和原始快照。
// 调用方发送成功后应调用 CommitFirstBatchData 删除同一快照中的第一条，避免发送失败丢卡。
func (c *Cards) FirstBatchData(ctx context.Context, cardID int64) (content, snapshot string, err error) {
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
	lines := splitLines(dataContent.String)
	if len(lines) == 0 {
		return "", "", errors.New("卡券批量数据无有效行")
	}
	return lines[0], dataContent.String, nil
}

// CommitFirstBatchData 在 data_content 仍等于 snapshot 时删除第一条有效内容。
// 条件更新失败表示库存被并发修改，调用方应停止本次发货，避免错删卡密。
func (c *Cards) CommitFirstBatchData(ctx context.Context, cardID int64, snapshot string) error {
	lines := splitLines(snapshot)
	if len(lines) == 0 {
		return errors.New("卡券批量数据无有效行")
	}
	remaining := ""
	if len(lines) > 1 {
		remaining = joinLines(lines[1:])
	}
	res, err := c.DB.ExecContext(ctx, `UPDATE cards SET data_content=? WHERE id=? AND data_content=?`, remaining, cardID, snapshot)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return errors.New("卡券库存已被并发修改，请重试")
	}
	return nil
}

// AppendBatchData 往 data 类型卡密组追加卡密号（按行）。返回新增的有效（非空）行数。
// 已有 data_content 非空时在末尾换行追加，否则直接写入。
func (c *Cards) AppendBatchData(ctx context.Context, cardID int64, content string) (int, error) {
	lines := splitLines(content)
	valid := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			valid++
		}
	}
	if valid == 0 {
		return 0, errors.New("无有效卡密行")
	}
	var existing sql.NullString
	err := c.DB.QueryRowContext(ctx, `SELECT data_content FROM cards WHERE id=?`, cardID).Scan(&existing)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	merged := content
	if existing.Valid && existing.String != "" {
		merged = existing.String + "\n" + content
	}
	if _, err := c.DB.ExecContext(ctx,
		`UPDATE cards SET data_content=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, merged, cardID); err != nil {
		return 0, err
	}
	return valid, nil
}

// splitLines / joinLines 统一处理多行库存内容。
func splitLines(s string) []string {
	var out []string
	cur := []rune{}
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
	for _, l := range out {
		if l != "" {
			res = append(res, l)
		}
	}
	return res
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	out := lines[0]
	for _, l := range lines[1:] {
		out += "\n" + l
	}
	return out
}
