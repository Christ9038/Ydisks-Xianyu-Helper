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

// ChatItem 是聊天商品选择器可展示和发送的非敏感商品快照。
type ChatItem struct {
	// ItemID 是闲鱼商品的稳定标识。
	ItemID string
	// Title 是当前商品标题。
	Title string
	// ImageURL 是商品主图的公开地址。
	ImageURL string
	// Price 是平台 reservePrice 字符串，不包含货币符号。
	Price string
	// Description 是列表中可选的商品摘要，不参与卡片发送。
	Description string
}

// ChatItemPage 是个人会话商品查询的单页结果。
type ChatItemPage struct {
	// Items 保存当前页归一化后的商品快照。
	Items []ChatItem
	// Page 是本次请求的从 1 开始页码。
	Page int
	// HasMore 表示平台是否返回 nextPage=true。
	HasMore bool
	// UpdatedCookies 保存合并 Set-Cookie 后的凭证，只能由适配器持久化。
	UpdatedCookies string
}

// SearchChatItems 查询个人聊天会话中对方或当前账号的在售商品。
// role 只允许 other/own；page 从 1 开始；query 传递给平台的 queryWord。
func (c *ClientImpl) SearchChatItems(ctx context.Context, cookiesStr, sessionID, role, query string, page int) (*ChatItemPage, error) {
	// normalizedSessionID、normalizedRole 和 normalizedQuery 是发起请求前去除首尾空白的平台参数。
	normalizedSessionID, normalizedRole, normalizedQuery := strings.TrimSpace(sessionID), strings.TrimSpace(role), strings.TrimSpace(query)
	if normalizedSessionID == "" || (normalizedRole != "other" && normalizedRole != "own") || page < 1 {
		return nil, fmt.Errorf("聊天商品查询参数无效")
	}
	// currentCookies 是当前重试轮次使用的凭证快照。
	currentCookies := cookiesStr
	// session 是同一业务请求内共享 Cookie 更新的可选会话。
	if session := cookieSessionFromContext(ctx); session != nil {
		currentCookies, _, _ = session.State()
	}
	// lastRet 保存最后一次平台返回码，供重试耗尽时分类。
	var lastRet []string
	// attempt 是与现有 MTOP 接口一致的最多四次 Token 恢复轮次。
	for attempt := 0; attempt < 4; attempt++ {
		// result、ret、updatedCookies 和 requestErr 保存单次请求的解析结果、返回码、最新凭证和错误。
		result, ret, updatedCookies, requestErr := c.searchChatItemsOnce(ctx, currentCookies, normalizedSessionID, normalizedRole, normalizedQuery, page)
		lastRet = ret
		if requestErr == nil && result != nil {
			return result, nil
		}
		// failure 是 HTTP 成功但 MTOP ret 失败时的统一错误分类。
		failure := requestErr
		if failure == nil {
			failure = c.mtopResponseFailure("聊天商品查询接口", http.StatusOK, ret, "")
		}
		if !IsMTopTokenExpiredErr(failure) {
			return nil, failure
		}
		if attempt == 3 {
			// 最后一次尝试已失败，循环即将结束；此时再等待或刷新 Token 只会多发一次无人使用的平台请求。
			break
		}
		if updatedCookies != "" {
			// tokenChanged 表示响应 Cookie 是否真的轮换了签名令牌；普通 Cookie 变化仍需调用官方刷新接口。
			tokenChanged := mtopTokenCookieChanged(currentCookies, updatedCookies)
			currentCookies = updatedCookies
			if tokenChanged {
				// sleepErr 表示 Token 新 Cookie 重试间隔是否被上下文取消。
				if sleepErr := sleepCtx(ctx, MTopRetryGap); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
		}
		// refreshed 是官方 Token 端点恢复后的凭证结果。
		refreshed, refreshErr := c.RefreshTokenContext(ctx, currentCookies)
		if refreshErr != nil {
			return nil, fmt.Errorf("刷新聊天商品查询 token 失败: %w", refreshErr)
		}
		currentCookies = refreshed.UpdatedCookies
		// sleepErr 表示 Token 重试间隔是否被上下文取消。
		if sleepErr := sleepCtx(ctx, MTopRetryGap); sleepErr != nil {
			return nil, sleepErr
		}
	}
	return nil, fmt.Errorf("聊天商品查询 token 重试失败: %w", c.mtopResponseFailure("聊天商品查询接口", http.StatusOK, lastRet, "重试次数已耗尽"))
}

// searchChatItemsOnce 执行一次已签名的聊天商品 MTOP 请求并归一化响应。
func (c *ClientImpl) searchChatItemsOnce(ctx context.Context, cookiesStr, sessionID, role, queryWord string, page int) (*ChatItemPage, []string, string, error) {
	// endpoint 是生产官方地址或测试注入地址。
	endpoint := strings.TrimSpace(c.ChatItemSearchURL)
	if endpoint == "" {
		endpoint = ChatItemSearchAPI
	}
	// signingCookies 和 requestCookies 分别用于生成签名和构建完整 Cookie 请求头。
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, "https://www.goofish.com/im", endpoint)
	// data 精确对应官网个人会话商品查询载荷，页大小固定为 20。
	data := map[string]any{"sessionId": sessionID, "pageSize": 20, "pageNumber": page, "searchItemRole": role, "queryWord": queryWord}
	// rawData 和 marshalErr 是参与签名的稳定 JSON 正文及序列化错误。
	rawData, marshalErr := json.Marshal(data)
	if marshalErr != nil {
		return nil, nil, cookiesStr, marshalErr
	}
	// dataValue 是参与签名并提交到 MTOP 表单的稳定 JSON 正文。
	dataValue := string(rawData)
	// timestamp 是平台签名使用的毫秒时间戳文本。
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// signature 是根据账号签名 Token、时间戳和正文生成的请求签名。
	signature := protocol.GenerateSign(timestamp, protocol.SignToken(signingCookies), dataValue)
	// query 是 MTOP H5 网关要求的查询参数。
	requestQuery := url.Values{}
	requestQuery.Set("jsv", "2.7.2")
	requestQuery.Set("appKey", protocol.SignAppKey)
	requestQuery.Set("t", timestamp)
	requestQuery.Set("sign", signature)
	requestQuery.Set("v", "1.0")
	requestQuery.Set("type", "originaljson")
	requestQuery.Set("accountSite", "xianyu")
	requestQuery.Set("dataType", "json")
	requestQuery.Set("timeout", "20000")
	requestQuery.Set("api", "mtop.taobao.idlemessage.pc.tool.item.search")
	requestQuery.Set("sessionOption", "AutoLoginOnly")
	// request 是带取消语义的表单 POST 请求。
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+requestQuery.Encode(), strings.NewReader("data="+url.QueryEscape(dataValue)))
	if requestErr != nil {
		return nil, nil, cookiesStr, requestErr
	}
	setCommonHeaders(request, requestCookies)
	request.Header.Set("Referer", "https://www.goofish.com/im")
	// response 和 doErr 保存平台 HTTP 响应及传输错误。
	response, doErr := c.httpClient().Do(request)
	if doErr != nil {
		return nil, nil, cookiesStr, fmt.Errorf("聊天商品查询请求失败: %w", doErr)
	}
	defer response.Body.Close()
	// updatedCookies 是吸收平台 Set-Cookie 后的最新凭证。
	updatedCookies := absorbMTopResponseCookies(ctx, cookiesStr, response)
	// body 和 readErr 是受限长度的响应正文及读取错误。
	body, readErr := readMTopBody(response)
	if readErr != nil {
		return nil, nil, updatedCookies, c.mtopResponseFailureWithCause("聊天商品查询接口", response.StatusCode, nil, "读取响应失败", readErr)
	}
	// decoded 是聊天商品查询的最小 MTOP 响应结构。
	var decoded struct {
		Ret  []string       `json:"ret"`
		Data map[string]any `json:"data"`
	}
	// decodeErr 表示平台响应是否为预期 JSON 结构。
	if decodeErr := json.Unmarshal(body, &decoded); decodeErr != nil {
		return nil, nil, updatedCookies, c.mtopResponseFailureWithCause("聊天商品查询接口", response.StatusCode, nil, "JSON 解析失败", decodeErr)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, decoded.Ret, updatedCookies, c.mtopResponseFailure("聊天商品查询接口", response.StatusCode, decoded.Ret, "HTTP 状态异常")
	}
	if !hasMTopSuccess(decoded.Ret) {
		return nil, decoded.Ret, updatedCookies, nil
	}
	// rawItems 是官网响应 data.items 中未归一化的商品集合。
	rawItems, _ := decoded.Data["items"].([]any)
	// items 仅保留 HTTP 和 UI 需要的非敏感商品字段。
	items := make([]ChatItem, 0, len(rawItems))
	for _ /* rawItem 是 data.items 中当前尚未归一化的平台元素。 */, rawItem := range rawItems {
		// itemMap 是当前平台商品对象；非对象元素被忽略。
		itemMap, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		// item 是归一化后的非敏感商品快照。
		item := ChatItem{ItemID: strings.TrimSpace(mtopString(itemMap["itemId"])), Title: strings.TrimSpace(mtopString(itemMap["title"])), ImageURL: normalizeChatItemImageURL(mtopString(itemMap["absoluteMajorPicture"])), Price: strings.TrimSpace(mtopString(itemMap["reservePrice"])), Description: strings.TrimSpace(mtopString(itemMap["desc"]))}
		if item.ItemID != "" && item.Title != "" && item.ImageURL != "" && item.Price != "" {
			items = append(items, item)
		}
	}
	return &ChatItemPage{Items: items, Page: page, HasMore: mtopBool(decoded.Data["nextPage"]), UpdatedCookies: updatedCookies}, decoded.Ret, updatedCookies, nil
}

// normalizeChatItemImageURL 将官网可直接渲染的协议相对主图地址转换为明确 HTTPS 地址。
func normalizeChatItemImageURL(raw string) string {
	// trimmed 是去除首尾空白的平台主图地址。
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "//") {
		return "https:" + trimmed
	}
	return trimmed
}
