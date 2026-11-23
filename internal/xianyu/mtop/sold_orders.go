// Package mtop: 卖家订单列表域 — 从卖家工作台发现并解析订单。
package mtop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/protocol"
)

// soldOrdersReferer 用于本次流程后续判断的sold订单列表Referer
const soldOrdersReferer = "https://seller.goofish.com/?site=COMMONPRO#/seller-trade/order-manage"

// SoldOrderFetcher 是订单同步使用的可选能力，不扩展基础 Client 接口，避免影响其他调用方的 mock。
type SoldOrderFetcher interface {
	FetchSoldOrdersPage(ctx context.Context, cookies string, pageNumber, pageSize int) (*SoldOrdersPage, error)
}

// SoldOrdersPage 是一页卖家订单列表。
type SoldOrdersPage struct {
	Items      []SoldOrder
	NextPage   bool
	TotalCount int
}

// SoldOrder 是订单列表可直接落库的字段。
type SoldOrder struct {
	OrderID string
	ItemID  string
	BuyerID string
	// CreatedAt 是平台记录的买家下单时间，缺失时保持空值让本地数据库沿用默认时间。
	CreatedAt      string
	OrderStatus    string
	Quantity       string
	Amount         string
	ReceiverName   string
	ReceiverPhone  string
	ReceiverAddr   string
	ReceiverCity   string
	IsBargain      bool
	PlatformStatus string
}

var _ SoldOrderFetcher = (*ClientImpl)(nil)

// FetchSoldOrdersPage 使用 c 的平台端点读取 pageNumber 页（从 1 开始），pageSize 为每页条数。
// ctx 控制取消并可携带实际 Cookie 会话；cookies 是仅用于请求的明文兼容凭证，禁止输出。
// 仅 HTTP 与平台均成功、必要结构合法且无订单丢失时返回页面和 nil；错误包含安全的平台错误码和原因。
// 只有 MTOP 签名 Token 过期会刷新后重试，Session、风控和业务错误立即返回。
func (c *ClientImpl) FetchSoldOrdersPage(ctx context.Context, cookies string, pageNumber, pageSize int) (*SoldOrdersPage, error) {
	// currentCookies 保存本轮订单列表请求实际使用的 Cookie，不向日志或错误输出。
	currentCookies := cookies
	if // session 用于吸收没有显式会话调用方收到的 Set-Cookie。
	session := cookieSessionFromContext(ctx); session != nil {
		currentCookies, _, _ = session.State()
	} else {
		ctx, _ = WithFlatCookieSession(ctx, currentCookies)
	}
	// lastFailure 保存 Token 重试耗尽时最后一次平台失败。
	var lastFailure error
	for // attempt 表示含首次请求在内的重试序号。
	attempt := 0; attempt < 4; attempt++ {
		// page、err 保存一次订单列表请求的结果。
		page, err := c.fetchSoldOrdersPageOnce(ctx, currentCookies, pageNumber, pageSize)
		if err == nil {
			return page, nil
		}
		if !IsMTopTokenExpiredErr(err) {
			return nil, err
		}
		lastFailure = err
		if attempt == 3 {
			// 最后一次尝试已失败，循环即将结束；此时再刷新 Token 只会多发一次无人使用的平台请求。
			break
		}
		// previousCookies 用于判断当前会话是否已经吸收响应下发的新签名 Cookie。
		previousCookies := currentCookies
		if // session 用于读取响应刚吸收的最新 Cookie。
		session := cookieSessionFromContext(ctx); session != nil {
			currentCookies, _, _ = session.State()
		}
		if !mtopTokenCookieChanged(previousCookies, currentCookies) {
			// refreshed、refreshErr 保存主动刷新 MTOP 签名 Token 的结果及错误。
			refreshed, refreshErr := c.RefreshTokenContext(ctx, currentCookies)
			if refreshErr != nil {
				return nil, fmt.Errorf("订单列表 Token 过期且刷新失败: %w", refreshErr)
			}
			currentCookies = refreshed.UpdatedCookies
		}
		if // sleepErr 表示两次平台请求之间等待期间的取消错误。
		sleepErr := sleepCtx(ctx, MTopRetryGap); sleepErr != nil {
			return nil, sleepErr
		}
	}
	return nil, fmt.Errorf("订单列表接口 Token 重试失败: %w", lastFailure)
}

// fetchSoldOrdersPageOnce 执行一次订单列表请求；调用方负责 Token 过期后的恢复重试。
func (c *ClientImpl) fetchSoldOrdersPageOnce(ctx context.Context, cookies string, pageNumber, pageSize int) (*SoldOrdersPage, error) {
	if pageNumber < 1 {
		pageNumber = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 30
	}
	// endpoint 是客户端配置的已售接口地址，未配置时使用平台默认端点。
	endpoint := c.SoldOrdersURL
	if endpoint == "" {
		endpoint = SoldOrdersAPI
	}
	// signingCookies 用于签名，requestCookies 是按实际请求 URL 筛选的明文凭证；两者均禁止输出。
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookies, soldOrdersReferer, endpoint)
	// token 是仅在当前请求内参与签名的明文令牌，禁止记录。
	token := protocol.SignToken(signingCookies)
	if token == "" {
		// 缺少签名令牌属于可由协议续期恢复的凭证状态；保留原诊断文本供上层展示。
		return nil, &MTopResponseError{Kind: MTopErrorTokenExpired, API: "订单列表接口", Detail: "cookie 缺少 _m_h5_tk，无法获取订单列表"}
	}
	// payload 保留当前卖家工作台查询全部订单的唯一请求格式。
	payload := map[string]any{
		"pageNumber":       pageNumber,
		"rowsPerPage":      pageSize,
		"orderIds":         "",
		"queryCode":        "ALL",
		"orderSearchParam": "{}",
	}
	// rawPayload、err 是分页参数序列化结果及错误，不包含凭证。
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	// dataVal 是参与签名并提交的平台查询参数文本。
	dataVal := string(rawPayload)
	// timestamp 是当前请求签名使用的 Unix 毫秒时间。
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// sign 是请求签名，禁止在错误中回显请求 URL。
	sign := protocol.GenerateSign(timestamp, token, dataVal)
	// req、err 是带取消上下文的请求及构造错误；错误只暴露固定诊断。
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+buildSoldOrdersQuery(timestamp, sign), strings.NewReader("data="+url.QueryEscape(dataVal)))
	if err != nil {
		return nil, errors.New("订单列表请求地址无效")
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://seller.goofish.com")
	req.Header.Set("Referer", soldOrdersReferer)
	req.Header.Set("idle_site_biz_code", "COMMONPRO")

	// hc 复用客户端已配置的超时与传输行为。
	hc := c.httpClient()
	// resp、err 保存平台 HTTP 响应及传输错误，不向上层泄漏含签名的 URL。
	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("订单列表请求失败")
	}
	defer resp.Body.Close()
	absorbMTopResponseCookies(ctx, cookies, resp)
	// raw、err 保存受大小上限保护的响应体和读取错误；正文仅用于解析，禁止回显。
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, c.mtopResponseFailureWithCause("订单列表接口", resp.StatusCode, nil, "读取响应失败", err)
	}
	// decoded 仅接收现有 data.module 结构，不推测其他平台格式。
	var decoded struct {
		// Ret 用于平台成功或会话失效分类，原始文本禁止传播。
		Ret []string `json:"ret"`
		// Data 承载卖家工作台的模块响应。
		Data struct {
			// Module 包含订单条目和分页信息，缺失或 null 不代表合法空列表。
			Module map[string]any `json:"module"`
		} `json:"data"`
	}
	// decodeErr 保存 JSON 语法或结构错误，错误值可能含平台输入，不能直接传播。
	if decodeErr := json.Unmarshal(raw, &decoded); decodeErr != nil {
		return nil, c.mtopResponseFailureWithCause("订单列表接口", resp.StatusCode, nil, "JSON 解析失败", decodeErr)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, c.mtopResponseFailure("订单列表接口", resp.StatusCode, decoded.Ret, "HTTP 状态异常")
	}
	if !hasMTopSuccess(decoded.Ret) {
		return nil, c.mtopResponseFailure("订单列表接口", resp.StatusCode, decoded.Ret, "平台 ret 未包含 SUCCESS")
	}
	// module 保存已售列表唯一受支持的响应容器。
	module := decoded.Data.Module
	if module == nil {
		return nil, fmt.Errorf("订单列表响应 module 缺失或为空: %w", c.mtopResponseFailure("订单列表接口", resp.StatusCode, decoded.Ret, "响应 data.module 缺失或为空"))
	}
	// itemsValue、itemsPresent 区分明确的 null 空列表与缺少必需字段。
	itemsValue, itemsPresent := module["items"]
	// rawItems、itemsValid 保存订单数组和类型校验结果；显式 null 兼容合法空列表。
	rawItems, itemsValid := itemsValue.([]any)
	if !itemsPresent || (itemsValue != nil && !itemsValid) {
		return nil, fmt.Errorf("订单列表响应 items 缺失或类型非法: %w", c.mtopResponseFailure("订单列表接口", resp.StatusCode, decoded.Ret, "响应 module.items 缺失或类型非法"))
	}
	// nextPage、nextPageValid 保留平台布尔兼容值，但不将未知形状静默当作最终页。
	nextPage, nextPageValid := soldOrdersNextPage(module["nextPage"])
	if !nextPageValid {
		return nil, fmt.Errorf("订单列表响应 nextPage 缺失或类型非法: %w", c.mtopResponseFailure("订单列表接口", resp.StatusCode, decoded.Ret, "响应 module.nextPage 缺失或类型非法"))
	}
	if nextPage && len(rawItems) == 0 {
		return nil, fmt.Errorf("订单列表第 %d 页为空页但 nextPage 为真，分页不完整: %w", pageNumber, c.mtopResponseFailure("订单列表接口", resp.StatusCode, decoded.Ret, "空页仍要求继续分页"))
	}
	// items 保存本页全部可解析订单，任何条目丢失都使整页失败。
	items := make([]SoldOrder, 0, len(rawItems))
	// itemIndex 是从零开始的条目位置，rawItem 是当前待解析订单，错误仅报告位置。
	for itemIndex, rawItem := range rawItems {
		// item、ok 保存订单解析结果及必要身份是否存在。
		item, ok := parseSoldOrder(rawItem)
		if !ok {
			return nil, fmt.Errorf("订单列表第 %d 页第 %d 条订单解析失败，分页不完整: %w", pageNumber, itemIndex+1, c.mtopResponseFailure("订单列表接口", resp.StatusCode, decoded.Ret, fmt.Sprintf("第 %d 条订单结构不符合接口约定", itemIndex+1)))
		}
		items = append(items, item)
	}
	return &SoldOrdersPage{
		Items:      items,
		NextPage:   nextPage,
		TotalCount: mtopInt(module["totalCount"]),
	}, nil
}

// soldOrdersNextPage 将 value 的已知平台布尔形状转换为是否继续分页和合法性；未知值不能证明分页结束。
func soldOrdersNextPage(value any) (bool, bool) {
	// typed 是 JSON 解码后的分页标志，只允许无歧义的真、假表示。
	switch typed := value.(type) {
	case bool:
		return typed, true
	case float64:
		return typed == 1, typed == 0 || typed == 1
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes":
			return true, true
		case "false", "0", "no":
			return false, true
		}
	}
	return false, false
}

// buildSoldOrdersQuery 封装buildSold订单列表查询业务协调。
func buildSoldOrdersQuery(timestamp, sign string) string {
	// values 用于本次流程后续判断的values
	values := url.Values{
		"jsv":           {"2.7.2"},
		"appKey":        {protocol.SignAppKey},
		"t":             {timestamp},
		"sign":          {sign},
		"v":             {"1.0"},
		"type":          {"json"},
		"accountSite":   {"xianyu"},
		"dataType":      {"json"},
		"timeout":       {"20000"},
		"api":           {"mtop.taobao.idle.trade.merchant.sold.get"},
		"valueType":     {"string"},
		"sessionOption": {"AutoLoginOnly"},
		"spm_cnt":       {"a21107h.42831410.0.0"},
	}
	return values.Encode()
}

// parseSoldOrder 封装parseSold订单业务协调。
func parseSoldOrder(raw any) (SoldOrder, bool) {
	// item、ok 用于本次流程后续判断的item、ok
	item, ok := raw.(map[string]any)
	if !ok {
		return SoldOrder{}, false
	}
	// common 用于本次流程后续判断的common
	common, _ := item["commonData"].(map[string]any)
	// buyer 用于本次流程后续判断的买家
	buyer, _ := item["buyerInfoVO"].(map[string]any)
	// price 用于本次流程后续判断的price
	price, _ := item["priceVO"].(map[string]any)
	// rights 用于本次流程后续判断的rights
	rights, _ := item["rightVO"].(map[string]any)
	// orderID 用于本次流程后续判断的订单ID
	orderID := strings.TrimSpace(mtopString(common["orderId"]))
	if orderID == "" {
		return SoldOrder{}, false
	}
	// rawStatus 用于本次流程后续判断的原始状态
	rawStatus := strings.TrimSpace(mtopString(common["orderStatus"]))
	// status 用于本次流程后续判断的状态
	status := normalizeSoldOrderStatus(rawStatus, mtopBool(common["inRefund"]))
	// amount 用于本次流程后续判断的amount
	amount := firstMTopString(price, "totalPrice", "confirmFee", "auctionPrice")
	// createdAt 保存平台订单创建时间，优先兼容卖家订单响应中常见的时间字段命名。
	createdAt := soldOrderCreatedAt(item, common)
	// quantity 用于本次流程后续判断的quantity
	quantity := firstMTopString(price, "buyNum", "quantity")
	if quantity == "" || quantity == "0" {
		quantity = "1"
	}
	// isBargain 用于本次流程后续判断的isBargain
	isBargain := false
	// buttons 用于本次流程后续判断的buttons
	buttons, _ := rights["btnList"].([]any)
	// rawButton 表示当前遍历过程中的原始Button
	for _, rawButton := range buttons {
		// button 用于本次流程后续判断的button
		button, _ := rawButton.(map[string]any)
		if strings.EqualFold(mtopString(button["tradeAction"]), "SKIP_PIN") {
			isBargain = true
			break
		}
	}
	return SoldOrder{
		OrderID:        orderID,
		ItemID:         strings.TrimSpace(mtopString(common["itemId"])),
		BuyerID:        strings.TrimSpace(mtopString(buyer["buyerId"])),
		CreatedAt:      createdAt,
		OrderStatus:    status,
		Quantity:       quantity,
		Amount:         amount,
		ReceiverName:   firstMTopString(buyer, "name", "receiverName"),
		ReceiverPhone:  firstMTopString(buyer, "phone", "receiverPhone"),
		ReceiverAddr:   firstMTopString(buyer, "address", "receiverAddress"),
		ReceiverCity:   firstMTopString(buyer, "city", "receiverCity"),
		IsBargain:      isBargain,
		PlatformStatus: rawStatus,
	}, true
}

// soldOrderCreatedAt 从平台订单列表响应提取并规范化买家下单时间。
// 平台不同版本可能把时间放在 commonData、订单根对象或嵌套订单对象中，所有成功解析值统一保存为 UTC 数据库文本。
func soldOrderCreatedAt(item, common map[string]any) string {
	// timeKeys 保存卖家订单接口可能返回的创建时间字段名，顺序体现优先级。
	timeKeys := []string{"orderCreateTime", "order_create_time", "createTime", "create_time", "gmtCreate", "gmt_create", "orderTime", "order_time"}
	// candidates 保存按平台响应层级排列的候选对象。
	candidates := []map[string]any{common, item}
	// container 表示当前遍历的订单响应容器。
	for _, container := range []map[string]any{common, item} {
		// nestedKey 表示可能承载订单时间的嵌套对象字段名。
		for _, nestedKey := range []string{"orderInfo", "orderVO", "tradeInfo"} {
			// nested 保存可能承载订单时间的嵌套对象。
			nested, _ := container[nestedKey].(map[string]any)
			if nested != nil {
				candidates = append(candidates, nested)
			}
		}
	}
	// candidate 表示当前尝试读取订单创建时间的响应对象。
	for _, candidate := range candidates {
		// key 表示当前尝试读取的订单创建时间字段名。
		for _, key := range timeKeys {
			// value 保存当前候选字段规范化后的订单创建时间。
			value := normalizeSoldOrderTime(mtopString(candidate[key]))
			if value != "" {
				return value
			}
		}
	}
	return ""
}

// normalizeSoldOrderTime 将平台订单时间转换为 UTC 数据库文本。
// 数字时间按秒或毫秒解释；带时区文本转换为 UTC；无时区平台文本固定按 UTC+8 解释。
func normalizeSoldOrderTime(raw string) string {
	// value 保存去除空白后的平台时间值。
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	// timestamp、parseErr 保存数字时间解析结果及错误；解析成功后按秒或毫秒解释。
	timestamp, parseErr := strconv.ParseInt(value, 10, 64)
	if parseErr == nil {
		// instant 保存按平台约定解析出的绝对时间点。
		instant := time.Unix(timestamp, 0)
		if len(strings.TrimLeft(value, "-")) >= 13 {
			instant = time.UnixMilli(timestamp)
		}
		return instant.UTC().Format("2006-01-02 15:04:05")
	}
	// layouts 保存卖家接口可能返回的标准文本时间格式。
	layouts := []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", "2006/01/02 15:04:05"}
	// layout 表示当前尝试解析的平台时间格式。
	for _, layout := range layouts {
		// parsed、parseErr 保存当前格式解析出的时间点及错误。
		location := time.UTC
		// timeSeparator 保存文本日期与时间部分的分隔位置。
		timeSeparator := strings.IndexByte(value, 'T')
		// hasExplicitZone 表示平台文本是否显式给出了时区偏移。
		hasExplicitZone := timeSeparator >= 0 && (strings.HasSuffix(value, "Z") || strings.LastIndexAny(value[timeSeparator+1:], "+-") >= 0)
		if !hasExplicitZone {
			location = time.FixedZone("Xianyu+08", 8*60*60)
		}
		// parsed、parseErr 保存当前布局解释出的时间点及错误。
		parsed, parseErr := time.ParseInLocation(layout, value, location)
		if parseErr == nil {
			return parsed.UTC().Format("2006-01-02 15:04:05")
		}
	}
	return ""
}

// normalizeSoldOrderStatus 封装normalizeSold订单状态业务协调。
func normalizeSoldOrderStatus(raw string, inRefund bool) string {
	if inRefund {
		return "refunding"
	}
	switch strings.TrimSpace(raw) {
	case "待付款", "处理中":
		return "processing"
	case "待发货", "已付款":
		return "pending_ship"
	case "已发货":
		return "shipped"
	case "交易成功", "已完成":
		return "completed"
	case "交易关闭", "已关闭", "退款关闭", "退款成功", "已退款":
		return "cancelled"
	case "退款中":
		return "refunding"
	default:
		return "unknown"
	}
}

// firstMTopString 封装firstMTopString业务协调。
func firstMTopString(values map[string]any, keys ...string) string {
	// key 表示当前遍历过程中的key
	for _, key := range keys {
		if // value 用于本次流程后续判断的值
		value := strings.TrimSpace(mtopString(values[key])); value != "" {
			return value
		}
	}
	return ""
}

// mtopBool 封装mtopBool业务协调。
func mtopBool(value any) bool {
	switch // typed 用于本次流程后续判断的typed
	typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case string:
		// normalized 用于本次流程后续判断的normalized
		normalized := strings.ToLower(strings.TrimSpace(typed))
		return normalized == "true" || normalized == "1" || normalized == "yes"
	default:
		return false
	}
}
