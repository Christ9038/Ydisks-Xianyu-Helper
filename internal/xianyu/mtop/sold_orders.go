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
func (c *ClientImpl) FetchSoldOrdersPage(ctx context.Context, cookies string, pageNumber, pageSize int) (*SoldOrdersPage, error) {
	if pageNumber < 1 {
		pageNumber = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 30
	}
	endpoint := c.SoldOrdersURL
	if endpoint == "" {
		endpoint = SoldOrdersAPI
	}
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookies, soldOrdersReferer, endpoint)
	token := protocol.SignToken(signingCookies)
	if token == "" {
		return nil, fmt.Errorf("cookie 缺少 _m_h5_tk，无法获取订单列表")
	}
	payload := map[string]any{
		"pageNumber":       pageNumber,
		"rowsPerPage":      pageSize,
		"orderIds":         "",
		"queryCode":        "ALL",
		"orderSearchParam": "{}",
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	dataVal := string(rawPayload)
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sign := protocol.GenerateSign(timestamp, token, dataVal)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+buildSoldOrdersQuery(timestamp, sign), strings.NewReader("data="+url.QueryEscape(dataVal)))
	if err != nil {
		return nil, err
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://seller.goofish.com")
	req.Header.Set("Referer", soldOrdersReferer)
	req.Header.Set("idle_site_biz_code", "COMMONPRO")

	hc := c.httpClient()
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("订单列表请求失败: %w", err)
	}
	defer resp.Body.Close()
	absorbMTopResponseCookies(ctx, cookies, resp)
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Ret  []string `json:"ret"`
		Data struct {
			Module map[string]any `json:"module"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("解析订单列表响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	if !hasMTopSuccess(decoded.Ret) {
		return nil, fmt.Errorf("订单列表接口返回非成功: ret=%v", decoded.Ret)
	}
	module := decoded.Data.Module
	rawItems, _ := module["items"].([]any)
	items := make([]SoldOrder, 0, len(rawItems))
	for _, rawItem := range rawItems {
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

func buildSoldOrdersQuery(timestamp, sign string) string {
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

func parseSoldOrder(raw any) (SoldOrder, bool) {
	item, ok := raw.(map[string]any)
	if !ok {
		return SoldOrder{}, false
	}
	common, _ := item["commonData"].(map[string]any)
	buyer, _ := item["buyerInfoVO"].(map[string]any)
	price, _ := item["priceVO"].(map[string]any)
	rights, _ := item["rightVO"].(map[string]any)
	orderID := strings.TrimSpace(mtopString(common["orderId"]))
	if orderID == "" {
		return SoldOrder{}, false
	}
	rawStatus := strings.TrimSpace(mtopString(common["orderStatus"]))
	status := normalizeSoldOrderStatus(rawStatus, mtopBool(common["inRefund"]))
	amount := firstMTopString(price, "totalPrice", "confirmFee", "auctionPrice")
	quantity := firstMTopString(price, "buyNum", "quantity")
	if quantity == "" || quantity == "0" {
		quantity = "1"
	}
	isBargain := false
	buttons, _ := rights["btnList"].([]any)
	for _, rawButton := range buttons {
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

func firstMTopString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(mtopString(values[key])); value != "" {
			return value
		}
	}
	return ""
}

func mtopBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		return normalized == "true" || normalized == "1" || normalized == "yes"
	default:
		return false
	}
}
