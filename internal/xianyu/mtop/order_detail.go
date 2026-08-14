// Package mtop: 订单详情域 — mtop.idle.web.trade.order.detail 调用与重试。
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

// OrderDetailResult 是订单详情接口中自动发货需要的字段。
type OrderDetailResult struct {
	Quantity       string
	SpecName       string
	SpecValue      string
	OrderStatus    string
	Amount         string
	UpdatedCookies string
}

// FetchOrderDetail 获取订单真实成交价、数量、状态和规格；token 过期时自动重签重试。
func (c *ClientImpl) FetchOrderDetail(ctx context.Context, cookiesStr, orderID string) (*OrderDetailResult, error) {
	// currentCookies 保存currentCookies，供当前处理流程使用
	currentCookies := cookiesStr
	if // session 保存会话，供当前处理流程使用
	session := cookieSessionFromContext(ctx); session != nil {
		currentCookies, _, _ = session.State()
	}
	// lastRet 保存lastRet，供当前处理流程使用
	var lastRet []string
	for // attempt 保存尝试次数，供当前处理流程使用
	attempt := 0; attempt < 4; attempt++ {
		// previousCookies 保存previousCookies，供当前处理流程使用
		previousCookies := currentCookies
		// result、ret、updated、err 保存result、ret、updated、err，供当前处理流程使用
		result, ret, updated, err := c.fetchOrderDetailOnce(ctx, currentCookies, orderID)
		if err != nil {
			return nil, err
		}
		lastRet = ret
		if updated != "" {
			currentCookies = updated
		}
		if result != nil {
			result.UpdatedCookies = currentCookies
			return result, nil
		}
		if isSessionExpiredRet(ret) {
			return nil, sessionExpiredError("订单详情接口", ret)
		}
		if !isTokenExpiredRet(ret) {
			return nil, fmt.Errorf("订单详情接口返回非成功: ret=%v", ret)
		}
		if attempt == 3 {
			break
		}
		if currentCookies == previousCookies {
			// refreshed、refreshErr 保存refreshed、refreshErr，供当前处理流程使用
			refreshed, refreshErr := c.RefreshTokenContext(ctx, currentCookies)
			if refreshErr != nil {
				return nil, fmt.Errorf("订单详情 token 刷新失败: %w", refreshErr)
			}
			currentCookies = refreshed.UpdatedCookies
		}
		if // err 保存err，供当前处理流程使用
		err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("订单详情 token 重试失败: ret=%v", lastRet)
}

// fetchOrderDetailOnce 负责fetch订单DetailOnce相关处理。
func (c *ClientImpl) fetchOrderDetailOnce(ctx context.Context, cookiesStr, orderID string) (*OrderDetailResult, []string, string, error) {
	// hc 保存hc，供当前处理流程使用
	hc := c.httpClient()
	// endpoint 保存endpoint，供当前处理流程使用
	endpoint := c.OrderDetailURL
	if endpoint == "" {
		endpoint = OrderDetailAPI
	}
	// documentURL 保存documentURL，供当前处理流程使用
	documentURL := "https://www.goofish.com/order-detail?orderId=" + url.QueryEscape(orderID) + "&role=seller"
	// signingCookies、requestCookies 保存signingCookies、requestCookies，供当前处理流程使用
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, documentURL, endpoint)
	// t 保存t，供当前处理流程使用
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// dataVal 保存数据Val，供当前处理流程使用
	dataVal := `{"tid":"` + orderID + `"}`
	// sign 保存sign，供当前处理流程使用
	sign := protocol.GenerateSign(t, protocol.SignToken(signingCookies), dataVal)
	// req、err 保存req、err，供当前处理流程使用
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+buildOrderDetailQuery(t, sign), strings.NewReader("data="+url.QueryEscape(dataVal)))
	if err != nil {
		return nil, nil, cookiesStr, err
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Referer", documentURL)
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, cookiesStr, fmt.Errorf("订单详情请求失败: %w", err)
	}
	defer resp.Body.Close()
	// updated 保存updated，供当前处理流程使用
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	// raw、err 保存raw、err，供当前处理流程使用
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, nil, updated, err
	}
	// decoded 保存decoded，供当前处理流程使用
	var decoded struct {
		Ret  []string       `json:"ret"`
		Data map[string]any `json:"data"`
	}
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, nil, updated, fmt.Errorf("解析订单详情响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	if !hasMTopSuccess(decoded.Ret) {
		return nil, decoded.Ret, updated, nil
	}
	// result 保存结果，供当前处理流程使用
	result := &OrderDetailResult{Quantity: "1"}
	if // utArgs、ok 保存utArgs、ok，供当前处理流程使用
	utArgs, ok := decoded.Data["utArgs"].(map[string]any); ok {
		result.OrderStatus = mtopString(utArgs["orderStatus"])
	}
	// components 保存components，供当前处理流程使用
	components, _ := decoded.Data["components"].([]any)
	// component 表示当前遍历过程中的component
	for _, component := range components {
		// cm 保存cm，供当前处理流程使用
		cm, _ := component.(map[string]any)
		if cm["render"] != "orderInfoVO" {
			continue
		}
		// componentData 保存component数据，供当前处理流程使用
		componentData, _ := cm["data"].(map[string]any)
		if // itemInfo、ok 保存商品Info、ok，供当前处理流程使用
		itemInfo, ok := componentData["itemInfo"].(map[string]any); ok {
			if // value 保存值，供当前处理流程使用
			value := mtopString(itemInfo["buyAmount"]); value != "" {
				result.Quantity = value
			}
			result.SpecName = mtopString(itemInfo["specName"])
			result.SpecValue = mtopString(itemInfo["specValue"])
		}
		if // priceInfo、ok 保存priceInfo、ok，供当前处理流程使用
		priceInfo, ok := componentData["priceInfo"].(map[string]any); ok {
			if // amount、ok 保存amount、ok，供当前处理流程使用
			amount, ok := priceInfo["amount"].(map[string]any); ok {
				result.Amount = mtopString(amount["value"])
			}
		}
	}
	return result, decoded.Ret, updated, nil
}

// buildOrderDetailQuery 负责build订单Detail查询相关处理。
func buildOrderDetailQuery(t, sign string) string {
	return "jsv=2.7.2&appKey=" + protocol.SignAppKey +
		"&t=" + t + "&sign=" + sign +
		"&v=1.0&type=originaljson&accountSite=xianyu&dataType=json&timeout=20000" +
		"&api=mtop.idle.web.trade.order.detail&sessionOption=AutoLoginOnly&valueType=string"
}
