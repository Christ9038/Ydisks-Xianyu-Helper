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

// soldOrdersReferer 保存sold订单列表Referer，供当前处理流程使用
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
// FetchSoldOrdersPage 负责FetchSold订单列表页码相关处理。
func (c *ClientImpl) FetchSoldOrdersPage(ctx context.Context, cookies string, pageNumber, pageSize int) (*SoldOrdersPage, error) {
	if pageNumber < 1 {
		pageNumber = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 30
	}
	// endpoint 保存endpoint，供当前处理流程使用
	endpoint := c.SoldOrdersURL
	if endpoint == "" {
		endpoint = SoldOrdersAPI
	}
	// signingCookies、requestCookies 保存signingCookies、requestCookies，供当前处理流程使用
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookies, soldOrdersReferer, endpoint)
	// token 保存令牌，供当前处理流程使用
	token := protocol.SignToken(signingCookies)
	if token == "" {
		return nil, fmt.Errorf("cookie 缺少 _m_h5_tk，无法获取订单列表")
	}
	// payload 保存请求载荷，供当前处理流程使用
	payload := map[string]any{
		"pageNumber":       pageNumber,
		"rowsPerPage":      pageSize,
		"orderIds":         "",
		"queryCode":        "ALL",
		"orderSearchParam": "{}",
	}
	// rawPayload、err 保存原始Payload、err，供当前处理流程使用
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	// dataVal 保存数据Val，供当前处理流程使用
	dataVal := string(rawPayload)
	// timestamp 保存timestamp，供当前处理流程使用
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// sign 保存sign，供当前处理流程使用
	sign := protocol.GenerateSign(timestamp, token, dataVal)
	// req、err 保存req、err，供当前处理流程使用
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+buildSoldOrdersQuery(timestamp, sign), strings.NewReader("data="+url.QueryEscape(dataVal)))
	if err != nil {
		return nil, err
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://seller.goofish.com")
	req.Header.Set("Referer", soldOrdersReferer)
	req.Header.Set("idle_site_biz_code", "COMMONPRO")

	// hc 保存hc，供当前处理流程使用
	hc := c.httpClient()
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("订单列表请求失败: %w", err)
	}
	defer resp.Body.Close()
	absorbMTopResponseCookies(ctx, cookies, resp)
	// raw、err 保存raw、err，供当前处理流程使用
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, err
	}
	// decoded 保存decoded，供当前处理流程使用
	var decoded struct {
		Ret  []string `json:"ret"`
		Data struct {
			Module map[string]any `json:"module"`
		} `json:"data"`
	}
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("解析订单列表响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	if isSessionExpiredRet(decoded.Ret) {
		return nil, sessionExpiredError("订单列表接口", decoded.Ret)
	}
	if !hasMTopSuccess(decoded.Ret) {
		return nil, fmt.Errorf("订单列表接口返回非成功: ret=%v", decoded.Ret)
	}
	// module 保存module，供当前处理流程使用
	module := decoded.Data.Module
	// rawItems 保存原始商品列表，供当前处理流程使用
	rawItems, _ := module["items"].([]any)
	// items 保存商品列表，供当前处理流程使用
	items := make([]SoldOrder, 0, len(rawItems))
	// rawItem 表示当前遍历过程中的原始商品
	for _, rawItem := range rawItems {
		// item、ok 保存item、ok，供当前处理流程使用
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

// buildSoldOrdersQuery 负责buildSold订单列表查询相关处理。
func buildSoldOrdersQuery(timestamp, sign string) string {
	// values 保存values，供当前处理流程使用
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

// parseSoldOrder 负责parseSold订单相关处理。
func parseSoldOrder(raw any) (SoldOrder, bool) {
	// item、ok 保存item、ok，供当前处理流程使用
	item, ok := raw.(map[string]any)
	if !ok {
		return SoldOrder{}, false
	}
	// common 保存common，供当前处理流程使用
	common, _ := item["commonData"].(map[string]any)
	// buyer 保存买家，供当前处理流程使用
	buyer, _ := item["buyerInfoVO"].(map[string]any)
	// price 保存price，供当前处理流程使用
	price, _ := item["priceVO"].(map[string]any)
	// rights 保存rights，供当前处理流程使用
	rights, _ := item["rightVO"].(map[string]any)
	// orderID 保存订单ID，供当前处理流程使用
	orderID := strings.TrimSpace(mtopString(common["orderId"]))
	if orderID == "" {
		return SoldOrder{}, false
	}
	// rawStatus 保存原始状态，供当前处理流程使用
	rawStatus := strings.TrimSpace(mtopString(common["orderStatus"]))
	// status 保存状态，供当前处理流程使用
	status := normalizeSoldOrderStatus(rawStatus, mtopBool(common["inRefund"]))
	// amount 保存amount，供当前处理流程使用
	amount := firstMTopString(price, "totalPrice", "confirmFee", "auctionPrice")
	// quantity 保存quantity，供当前处理流程使用
	quantity := firstMTopString(price, "buyNum", "quantity")
	if quantity == "" || quantity == "0" {
		quantity = "1"
	}
	// isBargain 保存isBargain，供当前处理流程使用
	isBargain := false
	// buttons 保存buttons，供当前处理流程使用
	buttons, _ := rights["btnList"].([]any)
	// rawButton 表示当前遍历过程中的原始Button
	for _, rawButton := range buttons {
		// button 保存button，供当前处理流程使用
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

// normalizeSoldOrderStatus 负责normalizeSold订单状态相关处理。
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

// firstMTopString 负责firstMTopString相关处理。
func firstMTopString(values map[string]any, keys ...string) string {
	// key 表示当前遍历过程中的key
	for _, key := range keys {
		if // value 保存值，供当前处理流程使用
		value := strings.TrimSpace(mtopString(values[key])); value != "" {
			return value
		}
	}
	return ""
}

// mtopBool 负责mtopBool相关处理。
func mtopBool(value any) bool {
	switch // typed 保存typed，供当前处理流程使用
	typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case string:
		// normalized 保存normalized，供当前处理流程使用
		normalized := strings.ToLower(strings.TrimSpace(typed))
		return normalized == "true" || normalized == "1" || normalized == "yes"
	default:
		return false
	}
}
