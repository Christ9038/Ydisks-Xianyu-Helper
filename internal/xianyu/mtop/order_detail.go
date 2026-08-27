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
	// bestName、bestValue 保存所有响应形状中维度最完整的规格结果。
	bestName, bestValue := "", ""
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
				bestName, bestValue = preferOrderSpecCandidate(bestName, bestValue, specName, specValue)
				continue
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
				bestName, bestValue = preferOrderSpecCandidate(bestName, bestValue, specName, specValue)
				continue
			}
			if partialName == "" && partialValue == "" {
				partialName, partialValue = specName, specValue
			}
		}
	}
	// key 表示可能承载结构化多 SKU 规格数组的平台字段名。
	for _, key := range []string{"skuProperties", "sku_properties", "skuProps", "sku_props", "specProperties", "spec_props"} {
		// specName、specValue、ok 保存结构化规格数组的归一结果及是否存在完整规格对。
		specName, specValue, ok := orderSpecsFromList(itemInfo[key])
		if ok {
			bestName, bestValue = preferOrderSpecCandidate(bestName, bestValue, specName, specValue)
		}
	}
	// key 表示可能嵌套规格对象的平台字段名。
	for _, key := range []string{"skuInfo", "sku_info", "specInfo", "spec_info", "sku"} {
		// specName、specValue、ok 保存嵌套数组规格的归一结果及是否存在完整规格对。
		specName, specValue, ok := orderSpecsFromList(itemInfo[key])
		if ok {
			bestName, bestValue = preferOrderSpecCandidate(bestName, bestValue, specName, specValue)
			continue
		}
		// nested、ok 保存当前嵌套对象及其类型断言结果。
		nested, ok := itemInfo[key].(map[string]any)
		if !ok {
			continue
		}
		// specName、specValue 保存嵌套对象解析出的规格名称和值。
		specName, specValue = orderSpecFromItemInfo(nested)
		if specName != "" || specValue != "" {
			if specName != "" && specValue != "" {
				bestName, bestValue = preferOrderSpecCandidate(bestName, bestValue, specName, specValue)
				continue
			}
			if partialName == "" && partialValue == "" {
				partialName, partialValue = specName, specValue
			}
		}
	}
	if bestName != "" && bestValue != "" {
		return bestName, bestValue
	}
	return partialName, partialValue
}

// preferOrderSpecCandidate 在多个平台字段同时存在时优先保留维度更多的完整规格结果。
func preferOrderSpecCandidate(currentName, currentValue, candidateName, candidateValue string) (string, string) {
	if currentName == "" || currentValue == "" {
		return candidateName, candidateValue
	}
	// currentScore、candidateScore 保存当前结果和候选结果的维度数量。
	currentScore := orderSpecDimensionCount(currentName, currentValue)
	// candidateScore 保存候选结果的规格维度数量。
	candidateScore := orderSpecDimensionCount(candidateName, candidateValue)
	if candidateScore > currentScore {
		return candidateName, candidateValue
	}
	return currentName, currentValue
}

// orderSpecDimensionCount 计算归一规格文本包含的维度数量，用于选择更完整的平台结果。
func orderSpecDimensionCount(name, value string) int {
	// nameCount、valueCount 保存名称和值文本中由稳定分隔符划分出的维度数量。
	nameCount := strings.Count(name, "；") + 1
	// valueCount 保存规格值文本中的维度数量。
	valueCount := strings.Count(value, "；") + 1
	if valueCount > nameCount {
		return valueCount
	}
	return nameCount
}

// orderSpecsFromList 从平台返回的结构化规格数组中提取全部名称和值。
func orderSpecsFromList(raw any) (string, string, bool) {
	// list、ok 保存规格数组及其类型断言结果。
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return "", "", false
	}
	// pairs 保存结构化数组中按平台顺序出现的完整规格对。
	pairs := make([]orderSpecPair, 0, len(list))
	for /* item 表示数组中的一条结构化规格记录。 */ _, item := range list {
		// record、ok 保存当前规格记录及其类型断言结果。
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		// name、value 保存当前记录的候选名称和值字段。
		name := firstOrderSpecMTopString(record, []string{"name", "specName", "spec_name", "propName", "propertyName"})
		// value 保存当前记录的候选规格值。
		value := firstOrderSpecMTopString(record, []string{"value", "specValue", "spec_value", "propValue", "propertyValue", "text"})
		if name != "" && value != "" {
			pairs = append(pairs, orderSpecPair{name: name, value: value})
		}
	}
	if len(pairs) == 0 {
		return "", "", false
	}
	// name、value 保存结构化规格数组归一后的名称和值文本。
	name, value := joinOrderSpecPairs(pairs)
	return name, value, true
}

// firstOrderSpecMTopString 按候选键顺序读取第一个非空规格文本字段。
func firstOrderSpecMTopString(record map[string]any, keys []string) string {
	for /* key 表示当前尝试读取的候选字段名。 */ _, key := range keys {
		if value := strings.TrimSpace(mtopString(record[key])); value != "" {
			return value
		}
	}
	return ""
}

// joinOrderSpecPairs 将规格对列表归一成规则配置使用的稳定名称和值文本。
func joinOrderSpecPairs(pairs []orderSpecPair) (string, string) {
	// names、values 保存按相同下标对齐的规格维度名称和值。
	names := make([]string, 0, len(pairs))
	// values 保存与名称列表位置一一对应的规格值。
	values := make([]string, 0, len(pairs))
	for /* pair 表示当前待归一化的规格名称和值。 */ _, pair := range pairs {
		names = append(names, pair.name)
		values = append(values, pair.value)
	}
	return strings.Join(names, "；"), strings.Join(values, "；")
}

// orderSpecPair 保存组合 SKU 中按平台顺序出现的一组规格名称和值。
type orderSpecPair struct {
	// name 是规格维度名称，例如“颜色”。
	name string
	// value 是该规格维度的实际成交值，例如“红色”。
	value string
}

// splitOrderSpecText 将平台返回的组合规格文本归一为完整的规格名称和值列表。
// 多维 SKU 使用中文分号连接各维度，旧版单维 SKU 的结果保持不变。
func splitOrderSpecText(raw string) (string, string) {
	// text 保存去除首尾空白后的组合规格文本。
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", ""
	}
	// pairs 保存按平台文本顺序解析出的全部规格对，避免只保留第一个维度造成规则串配。
	pairs := make([]orderSpecPair, 0, 2)
	for /* segment 表示多规格文本中的一个候选规格片段。 */ _, segment := range splitOrderSpecSegments(text) {
		// specName、specValue 保存当前片段两侧的完整规格名称和值。
		specName, specValue, ok := splitOrderSpecSegment(segment)
		if ok {
			pairs = append(pairs, orderSpecPair{name: specName, value: specValue})
		}
	}
	if len(pairs) > 0 {
		// names、values 使用稳定分隔符保留每一个 SKU 维度，供规则精确匹配。
		return joinOrderSpecPairs(pairs)
	}
	// fields 兼容平台以单个空格连接规格名和值的简化返回格式。
	fields := strings.Fields(text)
	if len(fields) == 2 && !containsOrderSpecListDelimiter(text) {
		return fields[0], fields[1]
	}
	return "", ""
}

// splitOrderSpecSegments 按“后方确实开始新规格对”的列表分隔符切分文本。
// 这样既支持逗号、分号等平台格式，也不会把规格值中的斜杠或普通逗号误当成维度边界。
func splitOrderSpecSegments(text string) []string {
	// segments 保存按完整规格对边界切出的候选片段。
	segments := make([]string, 0, 2)
	// start 保存当前候选片段的字节起点。
	start := 0
	for /* index、char 表示当前列表分隔符的字节位置和字符。 */ index, char := range text {
		if !isOrderSpecListDelimiter(char) {
			continue
		}
		// next 保存分隔符之后的下一个候选片段起点。
		next := index + len(string(char))
		if next >= len(text) || (!hasOrderSpecPairPrefix(text[next:]) &&
			(isOrderSpecCommaDelimiter(char) || !hasOrderSpecPairAnywhere(text[next:]))) {
			continue
		}
		segments = append(segments, text[start:index])
		start = next
	}
	segments = append(segments, text[start:])
	return segments
}

// isOrderSpecCommaDelimiter 标记逗号类分隔符；平台也可能把逗号放在一个规格值内部。
func isOrderSpecCommaDelimiter(char rune) bool {
	return char == '，' || char == ','
}

// isOrderSpecListDelimiter 判断字符是否是平台用于连接多个规格维度的分隔符。
func isOrderSpecListDelimiter(char rune) bool {
	return char == '；' || char == ';' || char == '\n' || char == '\r' || char == '，' || char == ',' || char == '|'
}

// containsOrderSpecListDelimiter 判断文本是否包含组合规格列表分隔符。
func containsOrderSpecListDelimiter(text string) bool {
	return strings.ContainsAny(text, "；;\n\r，,|")
}

// hasOrderSpecPairPrefix 判断文本开头直到下一个列表分隔符是否包含完整规格对。
func hasOrderSpecPairPrefix(text string) bool {
	// end 保存开头候选片段的字节结束位置。
	end := len(text)
	for /* index、char 表示当前扫描位置和列表分隔符。 */ index, char := range text {
		if isOrderSpecListDelimiter(char) {
			end = index
			break
		}
	}
	// ok 标记开头候选片段是否已经形成完整规格对。
	_, _, ok := splitOrderSpecSegment(text[:end])
	return ok
}

// hasOrderSpecPairAnywhere 判断剩余文本中是否存在后续完整规格对，用于跳过分隔符之间的脏片段。
func hasOrderSpecPairAnywhere(text string) bool {
	for /* segment 表示剩余文本中的一个候选规格片段。 */ _, segment := range strings.FieldsFunc(text, isOrderSpecListDelimiter) {
		// ok 标记当前候选片段是否可作为完整规格对。
		if _, _, ok := splitOrderSpecSegment(segment); ok {
			return true
		}
	}
	return false
}

// splitOrderSpecSegment 从单个候选片段中提取第一个名称和值分隔符。
func splitOrderSpecSegment(segment string) (string, string, bool) {
	segment = strings.TrimSpace(segment)
	// separatorIndex、separatorLength 保存第一个名称和值分隔符的位置及字节长度。
	separatorIndex, separatorLength := -1, 0
	for /* separator 表示当前尝试识别的名称和值分隔符。 */ _, separator := range []string{"：", ":", "="} {
		// index 保存当前分隔符在候选片段中的字节位置。
		if index := strings.Index(segment, separator); index >= 0 && (separatorIndex < 0 || index < separatorIndex) {
			separatorIndex, separatorLength = index, len(separator)
		}
	}
	if separatorIndex <= 0 || separatorIndex+separatorLength >= len(segment) {
		return "", "", false
	}
	// name、value 保存分隔符两侧清理空白后的规格名称和值。
	name := strings.TrimSpace(segment[:separatorIndex])
	// value 保存分隔符右侧清理空白后的规格值。
	value := strings.TrimSpace(segment[separatorIndex+separatorLength:])
	return name, value, name != "" && value != ""
}

// buildOrderDetailQuery 封装build订单Detail查询业务协调。
func buildOrderDetailQuery(t, sign string) string {
	return "jsv=2.7.2&appKey=" + protocol.SignAppKey +
		"&t=" + t + "&sign=" + sign +
		"&v=1.0&type=originaljson&accountSite=xianyu&dataType=json&timeout=20000" +
		"&api=mtop.idle.web.trade.order.detail&sessionOption=AutoLoginOnly&valueType=string"
}
