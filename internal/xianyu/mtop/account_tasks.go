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

// RateCreateAPI 用于本次流程后续判断的RateCreateAPI
const (
	RateCreateAPI       = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.rate.create/4.0/"
	PendingRateListAPI  = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.merchant.rate.list/1.0/"
	PolishItemAPI       = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.item.polish/2.0/"
	PolishItemBackupAPI = "https://h5api.m.goofish.com/h5/mtop.idle.item.polish/1.0/"
)

// PendingRateOrder 用于本次流程后续判断的PendingRate订单
type PendingRateOrder struct {
	TradeID string `json:"trade_id"`
	ItemID  string `json:"item_id"`
}

// PendingRateResult 用于本次流程后续判断的PendingRate结果
type PendingRateResult struct {
	Orders         []PendingRateOrder
	UpdatedCookies string
}

// AccountTaskResult 用于本次流程后续判断的账号任务结果
type AccountTaskResult struct {
	Success        bool
	Message        string
	UpdatedCookies string
}

// FetchPendingRateOrders 封装FetchPendingRate订单列表业务协调。
func (c *ClientImpl) FetchPendingRateOrders(ctx context.Context, cookiesStr string, page, pageSize int) (*PendingRateResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	// data 用于本次流程后续判断的数据
	data := map[string]any{"pageNumber": page, "rowsPerPage": pageSize, "queryType": "ORDER",
		"rateSearchParam": map[string]any{"sellerRateStatus": "5"}}
	// decoded、updated、err 用于本次流程后续判断的decoded、updated、err
	decoded, updated, err := c.accountTaskRequest(ctx, cookiesStr, firstNonEmptyURL(c.RateListURL, PendingRateListAPI),
		"mtop.taobao.idle.merchant.rate.list", "1.0", data, "https://seller.goofish.com/")
	if err != nil {
		return nil, err
	}
	// module 用于本次流程后续判断的module
	module, _ := decoded.Data["module"].(map[string]any)
	// items 用于本次流程后续判断的商品列表
	items, _ := module["items"].([]any)
	// orders 用于本次流程后续判断的订单列表
	orders := make([]PendingRateOrder, 0, len(items))
	// seen 用于本次流程后续判断的seen
	seen := make(map[string]struct{}, len(items))
	// item 表示当前遍历过程中的商品
	for _, item := range items {
		// tradeID 用于本次流程后续判断的tradeID
		tradeID := findStringField(item, "tradeId", "trade_id", "orderId", "orderNo", "order_no")
		if tradeID == "" {
			continue
		}
		if // ok 用于本次流程后续判断的ok
		_, ok := seen[tradeID]; ok {
			continue
		}
		seen[tradeID] = struct{}{}
		orders = append(orders, PendingRateOrder{TradeID: tradeID,
			ItemID: findStringField(item, "itemId", "item_id")})
	}
	return &PendingRateResult{Orders: orders, UpdatedCookies: updated}, nil
}

// RateBuyer 封装Rate买家业务协调。
func (c *ClientImpl) RateBuyer(ctx context.Context, cookiesStr, tradeID, feedback string) (*AccountTaskResult, error) {
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		feedback = "不错的买家，交易愉快"
	}
	// decoded、updated、err 用于本次流程后续判断的decoded、updated、err
	decoded, updated, err := c.accountTaskRequest(ctx, cookiesStr, firstNonEmptyURL(c.RateCreateURL, RateCreateAPI),
		"mtop.taobao.idle.rate.create", "4.0", map[string]any{
			"tradeId": tradeID, "rate": 1, "feedback": feedback, "createOrAppend": 0,
		}, "https://www.goofish.com/")
	if err != nil {
		return nil, err
	}
	return &AccountTaskResult{Success: true, Message: firstRet(decoded.Ret), UpdatedCookies: updated}, nil
}

// PolishItem 封装Polish商品业务协调。
func (c *ClientImpl) PolishItem(ctx context.Context, cookiesStr, itemID string) (*AccountTaskResult, error) {
	// decoded、updated、err 用于本次流程后续判断的decoded、updated、err
	decoded, updated, err := c.accountTaskRequest(ctx, cookiesStr, firstNonEmptyURL(c.PolishItemURL, PolishItemAPI),
		"mtop.taobao.idle.item.polish", "2.0", map[string]any{"itemId": itemID}, "https://www.goofish.com/")
	if err == nil {
		return &AccountTaskResult{Success: true, Message: firstRet(decoded.Ret), UpdatedCookies: updated}, nil
	}
	if duplicatePolishError(err) {
		return &AccountTaskResult{Success: true, Message: "商品今天已经擦亮", UpdatedCookies: updated}, nil
	}
	if IsSessionExpiredErr(err) || IsRiskVerificationErr(err) {
		return nil, err
	}
	// primaryErr 用于本次流程后续判断的primaryErr
	primaryErr := err
	if strings.TrimSpace(updated) == "" {
		updated = cookiesStr
	}
	// decoded、backupUpdated、backupErr 用于本次流程后续判断的decoded、backupUpdated、backupErr
	decoded, backupUpdated, backupErr := c.accountTaskRequest(ctx, updated,
		firstNonEmptyURL(c.PolishItemBackupURL, PolishItemBackupAPI), "mtop.idle.item.polish", "1.0",
		map[string]any{"itemId": itemID}, "https://www.goofish.com/")
	if backupErr == nil {
		return &AccountTaskResult{Success: true, Message: firstRet(decoded.Ret), UpdatedCookies: backupUpdated}, nil
	}
	if duplicatePolishError(backupErr) {
		return &AccountTaskResult{Success: true, Message: "商品今天已经擦亮", UpdatedCookies: backupUpdated}, nil
	}
	return nil, fmt.Errorf("擦亮主接口失败: %v；备用接口失败: %w", primaryErr, backupErr)
}

// duplicatePolishError 封装duplicatePolish错误业务协调。
func duplicatePolishError(err error) bool {
	if err == nil {
		return false
	}
	// msg 用于本次流程后续判断的msg
	msg := err.Error()
	return strings.Contains(msg, "IDLEITEM_POLISH_AGAIN") || strings.Contains(msg, "已经擦亮") ||
		strings.Contains(msg, "POLISH_DUPLICATE") || strings.Contains(msg, "一天只能擦亮一次")
}

// accountTaskResponse 用于本次流程后续判断的账号任务响应
type accountTaskResponse struct {
	Ret  []string       `json:"ret"`
	Data map[string]any `json:"data"`
}

// accountTaskRequest 封装账号任务请求业务协调。
func (c *ClientImpl) accountTaskRequest(ctx context.Context, cookiesStr, endpoint, api, version string, data map[string]any, referer string) (*accountTaskResponse, string, error) {
	// current 用于本次流程后续判断的current
	current := cookiesStr
	if // session 用于本次流程后续判断的会话
	session := cookieSessionFromContext(ctx); session != nil {
		current, _, _ = session.State()
	}
	// lastRet 用于本次流程后续判断的lastRet
	var lastRet []string
	for // attempt 用于本次流程后续判断的尝试次数
	attempt := 0; attempt < 3; attempt++ {
		// decoded、updated、err 用于本次流程后续判断的decoded、updated、err
		decoded, updated, err := c.accountTaskRequestOnce(ctx, current, endpoint, api, version, data, referer)
		if err != nil {
			return nil, current, err
		}
		lastRet = decoded.Ret
		if hasMTopSuccess(decoded.Ret) {
			return decoded, updated, nil
		}
		if isRiskVerificationRet(decoded.Ret) {
			return nil, updated, &RiskVerificationError{Ret: decoded.Ret}
		}
		if isSessionExpiredRet(decoded.Ret) {
			return nil, updated, sessionExpiredError(api, decoded.Ret)
		}
		if !isTokenExpiredRet(decoded.Ret) {
			return nil, updated, fmt.Errorf("%s 返回失败: %s", api, firstRet(decoded.Ret))
		}
		current = updated
		if current == cookiesStr {
			// refreshed、refreshErr 用于本次流程后续判断的refreshed、refreshErr
			refreshed, refreshErr := c.RefreshTokenContext(ctx, current)
			if refreshErr != nil {
				return nil, current, fmt.Errorf("刷新 mtop token: %w", refreshErr)
			}
			current = refreshed.UpdatedCookies
		}
		if // err 用于本次流程后续判断的err
		err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return nil, current, err
		}
	}
	return nil, current, fmt.Errorf("%s token 重试失败: %s", api, firstRet(lastRet))
}

// accountTaskRequestOnce 封装账号任务请求Once业务协调。
func (c *ClientImpl) accountTaskRequestOnce(ctx context.Context, cookiesStr, endpoint, api, version string, data map[string]any, referer string) (*accountTaskResponse, string, error) {
	// signingCookies、requestCookies 用于本次流程后续判断的signingCookies、requestCookies
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, referer, endpoint)
	// token 用于本次流程后续判断的令牌
	token := protocol.SignToken(signingCookies)
	if token == "" {
		return nil, cookiesStr, fmt.Errorf("cookie 缺少 _m_h5_tk，无法调用 %s", api)
	}
	// rawData、err 用于本次流程后续判断的原始Data、err
	rawData, err := json.Marshal(data)
	if err != nil {
		return nil, cookiesStr, err
	}
	// dataVal 用于本次流程后续判断的数据Val
	dataVal := string(rawData)
	// t 用于本次流程后续判断的t
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// sign 用于本次流程后续判断的sign
	sign := protocol.GenerateSign(t, token, dataVal)
	// query 用于本次流程后续判断的查询
	query := url.Values{}
	query.Set("jsv", "2.7.2")
	query.Set("appKey", protocol.SignAppKey)
	query.Set("t", t)
	query.Set("sign", sign)
	query.Set("v", version)
	// responseType 用于本次流程后续判断的响应类型
	responseType := "originaljson"
	if api == "mtop.taobao.idle.merchant.rate.list" {
		responseType = "json"
		query.Set("valueType", "string")
	}
	query.Set("type", responseType)
	query.Set("accountSite", "xianyu")
	query.Set("dataType", "json")
	query.Set("timeout", "20000")
	query.Set("api", api)
	query.Set("sessionOption", "AutoLoginOnly")
	if api == "mtop.taobao.idlemessage.pc.user.query" {
		query.Set("spm_cnt", "a21ybx.im.0.0")
		query.Set("spm_pre", "a21ybx.home.sidebar.2.4c053da6MpVe1m")
		query.Set("log_id", "4c053da6MpVe1m")
	}
	if api == "mtop.taobao.idle.item.polish" || api == "mtop.idle.item.polish" {
		query.Set("spm_cnt", "a21ybx.item.0.0")
		query.Set("spm_pre", "a21ybx.personal.feeds.1.42f86ac21eZ9zd")
		query.Set("log_id", "42f86ac21eZ9zd")
	}
	// req、err 用于本次流程后续判断的req、err
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+query.Encode(),
		strings.NewReader("data="+url.QueryEscape(dataVal)))
	if err != nil {
		return nil, cookiesStr, err
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Referer", referer)
	if // parsedReferer、parseErr 用于本次流程后续判断的解析结果Referer、parseErr
	parsedReferer, parseErr := url.Parse(referer); parseErr == nil && parsedReferer.Scheme != "" && parsedReferer.Host != "" {
		req.Header.Set("Origin", parsedReferer.Scheme+"://"+parsedReferer.Host)
	}
	// resp、err 用于本次流程后续判断的resp、err
	resp, err := c.httpClientWithTimeout(25 * time.Second).Do(req)
	if err != nil {
		return nil, cookiesStr, err
	}
	defer resp.Body.Close()
	// updated 用于本次流程后续判断的updated
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, updated, err
	}
	// decoded 用于本次流程后续判断的decoded
	var decoded accountTaskResponse
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, updated, fmt.Errorf("解析 %s 响应: %w", api, err)
	}
	return &decoded, updated, nil
}

// firstRet 封装firstRet业务协调。
func firstRet(ret []string) string {
	if len(ret) == 0 {
		return "未知响应"
	}
	return ret[0]
}

// firstNonEmptyURL 封装firstNonEmptyURL业务协调。
func firstNonEmptyURL(configured, fallback string) string {
	if strings.TrimSpace(configured) != "" {
		return configured
	}
	return fallback
}

// findStringField 封装findString字段业务协调。
func findStringField(value any, keys ...string) string {
	// wanted 用于本次流程后续判断的wanted
	wanted := make(map[string]struct{}, len(keys))
	// key 表示当前遍历过程中的key
	for _, key := range keys {
		wanted[key] = struct{}{}
	}
	// walk 用于本次流程后续判断的walk
	var walk func(any) string
	walk = func(v any) string {
		switch // x 用于本次流程后续判断的x
		x := v.(type) {
		case map[string]any:
			// key、child 表示当前遍历过程中的key、child
			for key, child := range x {
				if // ok 用于本次流程后续判断的ok
				_, ok := wanted[key]; ok {
					if // text 用于本次流程后续判断的文本
					text := mtopString(child); text != "" {
						return text
					}
				}
			}
			// child 表示当前遍历过程中的child
			for _, child := range x {
				if // text 用于本次流程后续判断的文本
				text := walk(child); text != "" {
					return text
				}
			}
		case []any:
			// child 表示当前遍历过程中的child
			for _, child := range x {
				if // text 用于本次流程后续判断的文本
				text := walk(child); text != "" {
					return text
				}
			}
		}
		return ""
	}
	return walk(value)
}
