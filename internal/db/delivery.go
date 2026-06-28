package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

// DeliveryRule 对应 delivery_rules JOIN cards 的发货匹配结果。
type DeliveryRule struct {
	ID               int64
	Keyword          string
	CardID           int64
	DeliveryCount    int
	Enabled          bool
	Description      string
	DeliveryTimes    int
	CardName         string
	CardType         string
	APIConfig        string
	TextContent      string
	DataContent      string
	ImageURL         string
	CardDescription  string
	CardDelaySeconds int
	IsMultiSpec      bool
	SpecName         string
	SpecValue        string
}

// Items 商品信息操作。
type Items struct{ DB *sql.DB }

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
type Cards struct{ DB *sql.DB }

// ConsumeBatchData 消费一条批量数据卡券（data 类型），返回内容。
// 对应 Python consume_batch_data：取 data_content 第一行，删除已消费行，发完置空。
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

// IncrementDeliveryTimes 发货次数 +1。
func (c *Cards) IncrementDeliveryTimes(ctx context.Context, ruleID int64) error {
	_, err := c.DB.ExecContext(ctx,
		`UPDATE delivery_rules SET delivery_times=delivery_times+1, updated_at=CURRENT_TIMESTAMP WHERE id=?`, ruleID)
	return err
}

// DeliveryRules 发货规则匹配。
type DeliveryRules struct{ DB *sql.DB }

// MatchByKeyword 非多规格双向模糊匹配（? LIKE %keyword% OR keyword LIKE %?%）。
// 按关键字长度降序（商品包含关键字时按全长，否则半长）+ id 升序。
// 移植自 get_delivery_rules_by_keyword。
func (d *DeliveryRules) MatchByKeyword(ctx context.Context, searchText string) ([]DeliveryRule, error) {
	q := `
SELECT dr.id, dr.keyword, dr.card_id, dr.delivery_count, dr.enabled, dr.description, dr.delivery_times,
       c.name, c.type, c.api_config, c.text_content, c.data_content, c.image_url, c.description,
       c.delay_seconds, c.is_multi_spec, c.spec_name, c.spec_value
FROM delivery_rules dr
LEFT JOIN cards c ON dr.card_id = c.id
WHERE dr.enabled=1 AND c.enabled=1
  AND (? LIKE '%' || dr.keyword || '%' OR dr.keyword LIKE '%' || ? || '%')
ORDER BY
  CASE WHEN ? LIKE '%' || dr.keyword || '%' THEN LENGTH(dr.keyword) ELSE LENGTH(dr.keyword)/2 END DESC,
  dr.id ASC`
	return d.scanRules(ctx, q, searchText, searchText, searchText)
}

// MatchByKeywordAndSpec 多规格匹配：双向模糊 + spec_name/spec_value 精确匹配。
// 移植自 get_delivery_rules_by_keyword_and_spec。
func (d *DeliveryRules) MatchByKeywordAndSpec(ctx context.Context, searchText, specName, specValue string) ([]DeliveryRule, error) {
	q := `
SELECT dr.id, dr.keyword, dr.card_id, dr.delivery_count, dr.enabled, dr.description, dr.delivery_times,
       c.name, c.type, c.api_config, c.text_content, c.data_content, c.image_url, c.description,
       c.delay_seconds, c.is_multi_spec, c.spec_name, c.spec_value
FROM delivery_rules dr
LEFT JOIN cards c ON dr.card_id = c.id
WHERE dr.enabled=1 AND c.enabled=1
  AND (? LIKE '%' || dr.keyword || '%' OR dr.keyword LIKE '%' || ? || '%')
  AND c.is_multi_spec=1 AND c.spec_name=? AND c.spec_value=?
ORDER BY
  CASE WHEN ? LIKE '%' || dr.keyword || '%' THEN LENGTH(dr.keyword) ELSE LENGTH(dr.keyword)/2 END DESC,
  dr.delivery_times ASC`
	return d.scanRules(ctx, q, searchText, searchText, specName, specValue, searchText)
}

// MatchForOrder 按账号、真实商品 ID 和订单规格匹配卡券。
// 新规则优先精确 item_id；旧规则（item_id 为空）保留关键词回退。
func (d *DeliveryRules) MatchForOrder(ctx context.Context, cookieID, itemID, searchText, specName, specValue string) ([]DeliveryRule, error) {
	q := `
SELECT dr.id, dr.keyword, v.card_id, v.delivery_count, dr.enabled, dr.description, dr.delivery_times,
       c.name, c.type, c.api_config, c.text_content, c.data_content, c.image_url, c.description,
       c.delay_seconds,
       CASE WHEN v.spec_name <> '' OR v.spec_value <> '' THEN 1 ELSE 0 END,
       v.spec_name, v.spec_value
FROM delivery_rules dr
JOIN delivery_rule_variants v ON v.rule_id = dr.id
JOIN cards c ON c.id = v.card_id
WHERE dr.enabled=1 AND v.enabled=1 AND c.enabled=1
  AND (dr.cookie_id='' OR dr.cookie_id=?)
  AND ((dr.item_id<>'' AND dr.item_id=?) OR
       (dr.item_id='' AND (? LIKE '%' || dr.keyword || '%' OR dr.keyword LIKE '%' || ? || '%')))
  AND v.spec_name=? AND v.spec_value=?
ORDER BY
  CASE WHEN dr.item_id=? THEN 1 ELSE 0 END DESC,
  CASE WHEN ? LIKE '%' || dr.keyword || '%' THEN LENGTH(dr.keyword) ELSE LENGTH(dr.keyword)/2 END DESC,
  dr.delivery_times ASC`
	return d.scanRules(ctx, q, cookieID, itemID, searchText, searchText, specName, specValue, itemID, searchText)
}

func (d *DeliveryRules) scanRules(ctx context.Context, query string, args ...any) ([]DeliveryRule, error) {
	rows, err := d.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []DeliveryRule
	for rows.Next() {
		var r DeliveryRule
		var enabled, isMultiSpec int
		var apiConfig, textContent, dataContent, imageURL, specName, specValue, desc, cardDesc sql.NullString
		var delaySeconds sql.NullInt64
		if err := rows.Scan(
			&r.ID, &r.Keyword, &r.CardID, &r.DeliveryCount, &enabled, &desc, &r.DeliveryTimes,
			&r.CardName, &r.CardType, &apiConfig, &textContent, &dataContent, &imageURL, &cardDesc,
			&delaySeconds, &isMultiSpec, &specName, &specValue); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		r.Description = desc.String
		r.APIConfig = apiConfig.String
		r.TextContent = textContent.String
		r.DataContent = dataContent.String
		r.ImageURL = imageURL.String
		r.CardDescription = cardDesc.String
		if delaySeconds.Valid {
			r.CardDelaySeconds = int(delaySeconds.Int64)
		}
		r.IsMultiSpec = isMultiSpec != 0
		r.SpecName = specName.String
		r.SpecValue = specValue.String
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// APIConfig 解析卡券的 api_config JSON。
func (r *DeliveryRule) APIConfigMap() map[string]any {
	if r.APIConfig == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(r.APIConfig), &m); err != nil {
		return nil
	}
	return m
}

// splitLines / joinLines 复刻 Python str.splitlines / "\n".join。
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
	// 过滤空行（Python consume_batch_data 行为）。
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

// 防止 fmt 未用（保留供未来错误格式化）。
var _ = fmt.Sprintf
