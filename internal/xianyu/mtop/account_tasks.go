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

// RateCreateAPI 保存RateCreateAPI，供当前处理流程使用
const (
	RateCreateAPI       = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.rate.create/4.0/"
	PendingRateListAPI  = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.merchant.rate.list/1.0/"
	PolishItemAPI       = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.item.polish/1.0/"
	PolishItemBackupAPI = "https://h5api.m.goofish.com/h5/mtop.idle.item.polish/1.0/"
)

// PendingRateOrder 保存PendingRate订单，供当前处理流程使用
type PendingRateOrder struct {
	TradeID string `json:"trade_id"`
	ItemID  string `json:"item_id"`
}

// PendingRateResult 保存PendingRate结果，供当前处理流程使用
type PendingRateResult struct {
	Orders         []PendingRateOrder
	UpdatedCookies string
}

// AccountTaskResult 保存账号任务结果，供当前处理流程使用
type AccountTaskResult struct {
	Success        bool
	Message        string
	UpdatedCookies string
}

// FetchPendingRateOrders 负责FetchPendingRate订单列表相关处理。
func (c *ClientImpl) FetchPendingRateOrders(ctx context.Context, cookiesStr string, page, pageSize int) (*PendingRateResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	// data 保存数据，供当前处理流程使用
	data := map[string]any{"pageNumber": page, "rowsPerPage": pageSize, "queryType": "ORDER",
		"rateSearchParam": map[string]any{"sellerRateStatus": "5"}}
	// decoded、updated、err 保存decoded、updated、err，供当前处理流程使用
	decoded, updated, err := c.accountTaskRequest(ctx, cookiesStr, firstNonEmptyURL(c.RateListURL, PendingRateListAPI),
		"mtop.taobao.idle.merchant.rate.list", "1.0", data, "https://seller.goofish.com/")
	if err != nil {
		return nil, err
	}
	// module 保存module，供当前处理流程使用
	module, _ := decoded.Data["module"].(map[string]any)
	// items 保存商品列表，供当前处理流程使用
	items, _ := module["items"].([]any)
	// orders 保存订单列表，供当前处理流程使用
	orders := make([]PendingRateOrder, 0, len(items))
	// seen 保存seen，供当前处理流程使用
	seen := make(map[string]struct{}, len(items))
	// item 表示当前遍历过程中的商品
	for _, item := range items {
		// tradeID 保存tradeID，供当前处理流程使用
		tradeID := findStringField(item, "tradeId", "trade_id", "orderId", "orderNo", "order_no")
		if tradeID == "" {
			continue
		}
		if // ok 保存ok，供当前处理流程使用
		_, ok := seen[tradeID]; ok {
			continue
		}
		seen[tradeID] = struct{}{}
		orders = append(orders, PendingRateOrder{TradeID: tradeID,
			ItemID: findStringField(item, "itemId", "item_id")})
	}
	return &PendingRateResult{Orders: orders, UpdatedCookies: updated}, nil
}

// RateBuyer 负责Rate买家相关处理。
func (c *ClientImpl) RateBuyer(ctx context.Context, cookiesStr, tradeID, feedback string) (*AccountTaskResult, error) {
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		feedback = "不错的买家，交易愉快"
	}
	// decoded、updated、err 保存decoded、updated、err，供当前处理流程使用
	decoded, updated, err := c.accountTaskRequest(ctx, cookiesStr, firstNonEmptyURL(c.RateCreateURL, RateCreateAPI),
		"mtop.taobao.idle.rate.create", "4.0", map[string]any{
			"tradeId": tradeID, "rate": 1, "feedback": feedback, "createOrAppend": 0,
		}, "https://www.goofish.com/")
	if err != nil {
		return nil, err
	}
	return &AccountTaskResult{Success: true, Message: firstRet(decoded.Ret), UpdatedCookies: updated}, nil
}

// PolishItem 负责Polish商品相关处理。
func (c *ClientImpl) PolishItem(ctx context.Context, cookiesStr, itemID string) (*AccountTaskResult, error) {
	// decoded、updated、err 保存decoded、updated、err，供当前处理流程使用
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
	// primaryErr 保存primaryErr，供当前处理流程使用
	primaryErr := err
	if strings.TrimSpace(updated) == "" {
		updated = cookiesStr
	}
	// decoded、backupUpdated、backupErr 保存decoded、backupUpdated、backupErr，供当前处理流程使用
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

// duplicatePolishError 负责duplicatePolish错误相关处理。
func duplicatePolishError(err error) bool {
	if err == nil {
		return false
	}
	// msg 保存msg，供当前处理流程使用
	msg := err.Error()
	return strings.Contains(msg, "IDLEITEM_POLISH_AGAIN") || strings.Contains(msg, "已经擦亮") ||
		strings.Contains(msg, "POLISH_DUPLICATE") || strings.Contains(msg, "一天只能擦亮一次")
}

// accountTaskResponse 保存账号任务响应，供当前处理流程使用
type accountTaskResponse struct {
	Ret  []string       `json:"ret"`
	Data map[string]any `json:"data"`
}

// accountTaskRequest 负责账号任务请求相关处理。
func (c *ClientImpl) accountTaskRequest(ctx context.Context, cookiesStr, endpoint, api, version string, data map[string]any, referer string) (*accountTaskResponse, string, error) {
	// current 保存current，供当前处理流程使用
	current := cookiesStr
	if // session 保存会话，供当前处理流程使用
	session := cookieSessionFromContext(ctx); session != nil {
		current, _, _ = session.State()
	}
	// lastRet 保存lastRet，供当前处理流程使用
	var lastRet []string
	for // attempt 保存尝试次数，供当前处理流程使用
	attempt := 0; attempt < 3; attempt++ {
		// decoded、updated、err 保存decoded、updated、err，供当前处理流程使用
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
			// refreshed、refreshErr 保存refreshed、refreshErr，供当前处理流程使用
			refreshed, refreshErr := c.RefreshTokenContext(ctx, current)
			if refreshErr != nil {
				return nil, current, fmt.Errorf("刷新 mtop token: %w", refreshErr)
			}
			current = refreshed.UpdatedCookies
		}
		if // err 保存err，供当前处理流程使用
		err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return nil, current, err
		}
	}
	return nil, current, fmt.Errorf("%s token 重试失败: %s", api, firstRet(lastRet))
}

// accountTaskRequestOnce 负责账号任务请求Once相关处理。
func (c *ClientImpl) accountTaskRequestOnce(ctx context.Context, cookiesStr, endpoint, api, version string, data map[string]any, referer string) (*accountTaskResponse, string, error) {
	// signingCookies、requestCookies 保存signingCookies、requestCookies，供当前处理流程使用
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, referer, endpoint)
	// token 保存令牌，供当前处理流程使用
	token := protocol.SignToken(signingCookies)
	if token == "" {
		return nil, cookiesStr, fmt.Errorf("cookie 缺少 _m_h5_tk，无法调用 %s", api)
	}
	// rawData、err 保存原始Data、err，供当前处理流程使用
	rawData, err := json.Marshal(data)
	if err != nil {
		return nil, cookiesStr, err
	}
	// dataVal 保存数据Val，供当前处理流程使用
	dataVal := string(rawData)
	// t 保存t，供当前处理流程使用
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// sign 保存sign，供当前处理流程使用
	sign := protocol.GenerateSign(t, token, dataVal)
	// query 保存查询，供当前处理流程使用
	query := url.Values{}
	query.Set("jsv", "2.7.2")
	query.Set("appKey", protocol.SignAppKey)
	query.Set("t", t)
	query.Set("sign", sign)
	query.Set("v", version)
	// responseType 保存响应类型，供当前处理流程使用
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
	// req、err 保存req、err，供当前处理流程使用
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+query.Encode(),
		strings.NewReader("data="+url.QueryEscape(dataVal)))
	if err != nil {
		return nil, cookiesStr, err
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Referer", referer)
	if // parsedReferer、parseErr 保存解析结果Referer、parseErr，供当前处理流程使用
	parsedReferer, parseErr := url.Parse(referer); parseErr == nil && parsedReferer.Scheme != "" && parsedReferer.Host != "" {
		req.Header.Set("Origin", parsedReferer.Scheme+"://"+parsedReferer.Host)
	}
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := c.httpClientWithTimeout(25 * time.Second).Do(req)
	if err != nil {
		return nil, cookiesStr, err
	}
	defer resp.Body.Close()
	// updated 保存updated，供当前处理流程使用
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	// raw、err 保存raw、err，供当前处理流程使用
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, updated, err
	}
	// decoded 保存decoded，供当前处理流程使用
	var decoded accountTaskResponse
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, updated, fmt.Errorf("解析 %s 响应: %w", api, err)
	}
	return &decoded, updated, nil
}

// firstRet 负责firstRet相关处理。
func firstRet(ret []string) string {
	if len(ret) == 0 {
		return "未知响应"
	}
	return ret[0]
}

// firstNonEmptyURL 负责firstNonEmptyURL相关处理。
func firstNonEmptyURL(configured, fallback string) string {
	if strings.TrimSpace(configured) != "" {
		return configured
	}
	return fallback
}

// findStringField 负责findString字段相关处理。
func findStringField(value any, keys ...string) string {
	// wanted 保存wanted，供当前处理流程使用
	wanted := make(map[string]struct{}, len(keys))
	// key 表示当前遍历过程中的key
	for _, key := range keys {
		wanted[key] = struct{}{}
	}
	// walk 保存walk，供当前处理流程使用
	var walk func(any) string
	walk = func(v any) string {
		switch // x 保存x，供当前处理流程使用
		x := v.(type) {
		case map[string]any:
			// key、child 表示当前遍历过程中的key、child
			for key, child := range x {
				if // ok 保存ok，供当前处理流程使用
				_, ok := wanted[key]; ok {
					if // text 保存文本，供当前处理流程使用
					text := mtopString(child); text != "" {
						return text
					}
				}
			}
			// child 表示当前遍历过程中的child
			for _, child := range x {
				if // text 保存文本，供当前处理流程使用
				text := walk(child); text != "" {
					return text
				}
			}
		case []any:
			// child 表示当前遍历过程中的child
			for _, child := range x {
				if // text 保存文本，供当前处理流程使用
				text := walk(child); text != "" {
					return text
				}
			}
		}
		return ""
	}
	return walk(value)
}
