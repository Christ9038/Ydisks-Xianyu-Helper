// Package mtop: 卖家订单列表域 — 从卖家工作台发现并解析订单。
package mtop

import (
	"context"
	"encoding/json"
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
	OrderID        string
	ItemID         string
	BuyerID        string
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

// FetchSoldOrdersPage 获取卖家已售订单。该调用不主动刷新 token；当 ctx
// 携带 CookieSession 时会像浏览器一样吸收响应 Cookie。
// FetchSoldOrdersPage 封装FetchSold订单列表页码业务协调。
func (c *ClientImpl) FetchSoldOrdersPage(ctx context.Context, cookies string, pageNumber, pageSize int) (*SoldOrdersPage, error) {
	if pageNumber < 1 {
		pageNumber = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 30
	}
	// endpoint 用于本次流程后续判断的endpoint
	endpoint := c.SoldOrdersURL
	if endpoint == "" {
		endpoint = SoldOrdersAPI
	}
	// signingCookies、requestCookies 用于本次流程后续判断的signingCookies、requestCookies
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookies, soldOrdersReferer, endpoint)
	// token 用于本次流程后续判断的令牌
	token := protocol.SignToken(signingCookies)
	if token == "" {
		return nil, fmt.Errorf("cookie 缺少 _m_h5_tk，无法获取订单列表")
	}
	// payload 用于本次流程后续判断的请求载荷
	payload := map[string]any{
		"pageNumber":       pageNumber,
		"rowsPerPage":      pageSize,
		"orderIds":         "",
		"queryCode":        "ALL",
		"orderSearchParam": "{}",
	}
	// rawPayload、err 用于本次流程后续判断的原始Payload、err
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	// dataVal 用于本次流程后续判断的数据Val
	dataVal := string(rawPayload)
	// timestamp 用于本次流程后续判断的timestamp
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// sign 用于本次流程后续判断的sign
	sign := protocol.GenerateSign(timestamp, token, dataVal)
	// req、err 用于本次流程后续判断的req、err
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+buildSoldOrdersQuery(timestamp, sign), strings.NewReader("data="+url.QueryEscape(dataVal)))
	if err != nil {
		return nil, err
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://seller.goofish.com")
	req.Header.Set("Referer", soldOrdersReferer)
	req.Header.Set("idle_site_biz_code", "COMMONPRO")

	// hc 用于本次流程后续判断的hc
	hc := c.httpClient()
	// resp、err 用于本次流程后续判断的resp、err
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("订单列表请求失败: %w", err)
	}
	defer resp.Body.Close()
	absorbMTopResponseCookies(ctx, cookies, resp)
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, err
	}
	// decoded 用于本次流程后续判断的decoded
	var decoded struct {
		Ret  []string `json:"ret"`
		Data struct {
			Module map[string]any `json:"module"`
		} `json:"data"`
	}
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("解析订单列表响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	if isSessionExpiredRet(decoded.Ret) {
		return nil, sessionExpiredError("订单列表接口", decoded.Ret)
	}
	if !hasMTopSuccess(decoded.Ret) {
		return nil, fmt.Errorf("订单列表接口返回非成功: ret=%v", decoded.Ret)
	}
	// module 用于本次流程后续判断的module
	module := decoded.Data.Module
	// rawItems 用于本次流程后续判断的原始商品列表
	rawItems, _ := module["items"].([]any)
	// items 用于本次流程后续判断的商品列表
	items := make([]SoldOrder, 0, len(rawItems))
	// rawItem 表示当前遍历过程中的原始商品
	for _, rawItem := range rawItems {
		// item、ok 用于本次流程后续判断的item、ok
		item, ok := parseSoldOrder(rawItem)
		if ok {
			items = append(items, item)
		}
	}
	return &SoldOrdersPage{
		Items:      items,
		NextPage:   mtopBool(module["nextPage"]),
		TotalCount: mtopInt(module["totalCount"]),
	}, nil
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
