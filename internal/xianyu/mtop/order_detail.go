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
	// currentCookies 用于本次流程后续判断的currentCookies
	currentCookies := cookiesStr
	if // session 用于本次流程后续判断的会话
	session := cookieSessionFromContext(ctx); session != nil {
		currentCookies, _, _ = session.State()
	}
	// lastRet 用于本次流程后续判断的lastRet
	var lastRet []string
	for // attempt 用于本次流程后续判断的尝试次数
	attempt := 0; attempt < 4; attempt++ {
		// previousCookies 用于本次流程后续判断的previousCookies
		previousCookies := currentCookies
		// result、ret、updated、err 用于本次流程后续判断的result、ret、updated、err
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
			// refreshed、refreshErr 用于本次流程后续判断的refreshed、refreshErr
			refreshed, refreshErr := c.RefreshTokenContext(ctx, currentCookies)
			if refreshErr != nil {
				return nil, fmt.Errorf("订单详情 token 刷新失败: %w", refreshErr)
			}
			currentCookies = refreshed.UpdatedCookies
		}
		if // err 用于本次流程后续判断的err
		err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("订单详情 token 重试失败: ret=%v", lastRet)
}

// fetchOrderDetailOnce 封装fetch订单DetailOnce业务协调。
func (c *ClientImpl) fetchOrderDetailOnce(ctx context.Context, cookiesStr, orderID string) (*OrderDetailResult, []string, string, error) {
	// hc 用于本次流程后续判断的hc
	hc := c.httpClient()
	// endpoint 用于本次流程后续判断的endpoint
	endpoint := c.OrderDetailURL
	if endpoint == "" {
		endpoint = OrderDetailAPI
	}
	// documentURL 用于本次流程后续判断的documentURL
	documentURL := "https://www.goofish.com/order-detail?orderId=" + url.QueryEscape(orderID) + "&role=seller"
	// signingCookies、requestCookies 用于本次流程后续判断的signingCookies、requestCookies
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, documentURL, endpoint)
	// t 用于本次流程后续判断的t
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// dataVal 用于本次流程后续判断的数据Val
	dataVal := `{"tid":"` + orderID + `"}`
	// sign 用于本次流程后续判断的sign
	sign := protocol.GenerateSign(t, protocol.SignToken(signingCookies), dataVal)
	// req、err 用于本次流程后续判断的req、err
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+buildOrderDetailQuery(t, sign), strings.NewReader("data="+url.QueryEscape(dataVal)))
	if err != nil {
		return nil, nil, cookiesStr, err
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Referer", documentURL)
	// resp、err 用于本次流程后续判断的resp、err
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, cookiesStr, fmt.Errorf("订单详情请求失败: %w", err)
	}
	defer resp.Body.Close()
	// updated 用于本次流程后续判断的updated
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, nil, updated, err
	}
	// decoded 用于本次流程后续判断的decoded
	var decoded struct {
		Ret  []string       `json:"ret"`
		Data map[string]any `json:"data"`
	}
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, nil, updated, fmt.Errorf("解析订单详情响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	if !hasMTopSuccess(decoded.Ret) {
		return nil, decoded.Ret, updated, nil
	}
	// result 用于本次流程后续判断的结果
	result := &OrderDetailResult{Quantity: "1"}
	if // utArgs、ok 用于本次流程后续判断的utArgs、ok
	utArgs, ok := decoded.Data["utArgs"].(map[string]any); ok {
		result.OrderStatus = mtopString(utArgs["orderStatus"])
	}
	// components 用于本次流程后续判断的components
	components, _ := decoded.Data["components"].([]any)
	// component 表示当前遍历过程中的component
	for _, component := range components {
		// cm 用于本次流程后续判断的cm
		cm, _ := component.(map[string]any)
		if cm["render"] != "orderInfoVO" {
			continue
		}
		// componentData 用于本次流程后续判断的component数据
		componentData, _ := cm["data"].(map[string]any)
		if // itemInfo、ok 用于本次流程后续判断的商品Info、ok
		itemInfo, ok := componentData["itemInfo"].(map[string]any); ok {
			if // value 用于本次流程后续判断的值
			value := mtopString(itemInfo["buyAmount"]); value != "" {
				result.Quantity = value
			}
			// specName、specValue 兼容订单详情接口对单独规格字段和组合 SKU 文本的不同返回形状。
			result.SpecName, result.SpecValue = orderSpecFromItemInfo(itemInfo)
		}
		if // priceInfo、ok 用于本次流程后续判断的priceInfo、ok
		priceInfo, ok := componentData["priceInfo"].(map[string]any); ok {
			if // amount、ok 用于本次流程后续判断的amount、ok
			amount, ok := priceInfo["amount"].(map[string]any); ok {
				result.Amount = mtopString(amount["value"])
			}
		}
	}
	return result, decoded.Ret, updated, nil
}

// orderSpecFromItemInfo 从订单商品信息中提取自动发货需要的规格名称和值。
// 闲鱼不同商品类型可能返回 specName/specValue，也可能把规格放在 skuInfo 或 skuText 中；
// 统一在 MTOP 边界归一，避免自动化层被平台响应形状耦合。
func orderSpecFromItemInfo(itemInfo map[string]any) (string, string) {
	if itemInfo == nil {
		return "", ""
	}
	// pair 表示平台返回的规格名称字段与规格值字段候选组合。
	partialName, partialValue := "", ""
	for /* pair 表示平台规格名称和值字段的一组候选键。 */ _, pair := range [][2]string{
		{"specName", "specValue"},
		{"spec_name", "spec_value"},
		{"skuName", "skuValue"},
		{"sku_name", "sku_value"},
		{"propName", "propValue"},
		{"propertyName", "propertyValue"},
	} {
		// specName、specValue 保存当前候选组合解析出的规格字段。
		specName := strings.TrimSpace(mtopString(itemInfo[pair[0]]))
		// specValue 保存当前候选组合解析出的规格值。
		specValue := strings.TrimSpace(mtopString(itemInfo[pair[1]]))
		if specName != "" || specValue != "" {
			if specName != "" && specValue != "" {
				return specName, specValue
			}
			if partialName == "" && partialValue == "" {
				partialName, partialValue = specName, specValue
			}
		}
	}
	// key 表示可能承载“规格名:规格值”组合文本的平台字段名。
	for _, key := range []string{"skuText", "sku_text", "specText", "spec_text", "skuDesc", "skuDescText"} {
		// specName、specValue 保存组合规格文本拆分后的名称和值。
		specName, specValue := splitOrderSpecText(mtopString(itemInfo[key]))
		if specName != "" || specValue != "" {
			if specName != "" && specValue != "" {
				return specName, specValue
			}
			if partialName == "" && partialValue == "" {
				partialName, partialValue = specName, specValue
			}
		}
	}
	// key 表示可能嵌套规格对象的平台字段名。
	for _, key := range []string{"skuInfo", "sku_info", "specInfo", "spec_info", "sku"} {
		// nested、ok 保存当前嵌套对象及其类型断言结果。
		nested, ok := itemInfo[key].(map[string]any)
		if !ok {
			continue
		}
		// specName、specValue 保存嵌套对象解析出的规格名称和值。
		specName, specValue := orderSpecFromItemInfo(nested)
		if specName != "" || specValue != "" {
			if specName != "" && specValue != "" {
				return specName, specValue
			}
			if partialName == "" && partialValue == "" {
				partialName, partialValue = specName, specValue
			}
		}
	}
	return partialName, partialValue
}

// splitOrderSpecText 将平台返回的组合规格文本拆为名称和值。
// 目前自动化规则以一组名称和值描述规格，因此多字段 SKU 只取第一个带分隔符的规格对。
func splitOrderSpecText(raw string) (string, string) {
	// text 保存去除首尾空白后的组合规格文本。
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", ""
	}
	// segments 保存多规格文本的候选片段；斜杠不作为分隔符，避免破坏合法规格值。
	segments := strings.FieldsFunc(text, func(r rune) bool {
		return r == '；' || r == ';' || r == '\n' || r == '，' || r == ',' || r == '|'
	})
	if len(segments) == 0 {
		segments = []string{text}
	}
	for /* segment 表示多规格文本中的一个候选规格片段。 */ _, segment := range segments {
		segment = strings.TrimSpace(segment)
		for /* separator 表示当前片段使用的名称和值分隔符。 */ _, separator := range []string{"：", ":", "="} {
			// separatorIndex 表示当前片段中名称和值分隔符的位置。
			separatorIndex := strings.Index(segment, separator)
			if separatorIndex <= 0 || separatorIndex >= len(segment)-len(separator) {
				continue
			}
			// specName、specValue 保存当前片段两侧的完整规格名称和值。
			specName := strings.TrimSpace(segment[:separatorIndex])
			// specValue 保存当前片段右侧的完整规格值。
			specValue := strings.TrimSpace(segment[separatorIndex+len(separator):])
			if specName != "" && specValue != "" {
				return specName, specValue
			}
		}
	}
	// fields 兼容平台以单个空格连接规格名和值的简化返回格式。
	fields := strings.Fields(text)
	if len(segments) == 1 && len(fields) == 2 {
		return fields[0], fields[1]
	}
	return "", ""
}

// buildOrderDetailQuery 封装build订单Detail查询业务协调。
func buildOrderDetailQuery(t, sign string) string {
	return "jsv=2.7.2&appKey=" + protocol.SignAppKey +
		"&t=" + t + "&sign=" + sign +
		"&v=1.0&type=originaljson&accountSite=xianyu&dataType=json&timeout=20000" +
		"&api=mtop.idle.web.trade.order.detail&sessionOption=AutoLoginOnly&valueType=string"
}
