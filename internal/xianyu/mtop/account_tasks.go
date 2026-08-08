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

const (
	RateCreateAPI       = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.rate.create/4.0/"
	PendingRateListAPI  = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.merchant.rate.list/1.0/"
	PolishItemAPI       = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.item.polish/1.0/"
	PolishItemBackupAPI = "https://h5api.m.goofish.com/h5/mtop.idle.item.polish/1.0/"
)

type PendingRateOrder struct {
	TradeID string `json:"trade_id"`
	ItemID  string `json:"item_id"`
}

type PendingRateResult struct {
	Orders         []PendingRateOrder
	UpdatedCookies string
}

type AccountTaskResult struct {
	Success        bool
	Message        string
	UpdatedCookies string
}

func (c *ClientImpl) FetchPendingRateOrders(ctx context.Context, cookiesStr string, page, pageSize int) (*PendingRateResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	data := map[string]any{"pageNumber": page, "rowsPerPage": pageSize, "queryType": "ORDER",
		"rateSearchParam": map[string]any{"sellerRateStatus": "5"}}
	decoded, updated, err := c.accountTaskRequest(ctx, cookiesStr, firstNonEmptyURL(c.RateListURL, PendingRateListAPI),
		"mtop.taobao.idle.merchant.rate.list", "1.0", data, "https://seller.goofish.com/")
	if err != nil {
		return nil, err
	}
	module, _ := decoded.Data["module"].(map[string]any)
	items, _ := module["items"].([]any)
	orders := make([]PendingRateOrder, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		tradeID := findStringField(item, "tradeId", "trade_id", "orderId", "orderNo", "order_no")
		if tradeID == "" {
			continue
		}
		if _, ok := seen[tradeID]; ok {
			continue
		}
		seen[tradeID] = struct{}{}
		orders = append(orders, PendingRateOrder{TradeID: tradeID,
			ItemID: findStringField(item, "itemId", "item_id")})
	}
	return &PendingRateResult{Orders: orders, UpdatedCookies: updated}, nil
}

func (c *ClientImpl) RateBuyer(ctx context.Context, cookiesStr, tradeID, feedback string) (*AccountTaskResult, error) {
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		feedback = "不错的买家，交易愉快"
	}
	decoded, updated, err := c.accountTaskRequest(ctx, cookiesStr, firstNonEmptyURL(c.RateCreateURL, RateCreateAPI),
		"mtop.taobao.idle.rate.create", "4.0", map[string]any{
			"tradeId": tradeID, "rate": 1, "feedback": feedback, "createOrAppend": 0,
		}, "https://www.goofish.com/")
	if err != nil {
		return nil, err
	}
	return &AccountTaskResult{Success: true, Message: firstRet(decoded.Ret), UpdatedCookies: updated}, nil
}

func (c *ClientImpl) PolishItem(ctx context.Context, cookiesStr, itemID string) (*AccountTaskResult, error) {
	decoded, updated, err := c.accountTaskRequest(ctx, cookiesStr, firstNonEmptyURL(c.PolishItemURL, PolishItemAPI),
		"mtop.taobao.idle.item.polish", "2.0", map[string]any{"itemId": itemID}, "https://www.goofish.com/")
	if err == nil {
		return &AccountTaskResult{Success: true, Message: firstRet(decoded.Ret), UpdatedCookies: updated}, nil
	}
	if duplicatePolishError(err) {
		return &AccountTaskResult{Success: true, Message: "商品今天已经擦亮", UpdatedCookies: updated}, nil
	}
	primaryErr := err
	if strings.TrimSpace(updated) == "" {
		updated = cookiesStr
	}
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

func duplicatePolishError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "IDLEITEM_POLISH_AGAIN") || strings.Contains(msg, "已经擦亮") ||
		strings.Contains(msg, "POLISH_DUPLICATE") || strings.Contains(msg, "一天只能擦亮一次")
}

type accountTaskResponse struct {
	Ret  []string       `json:"ret"`
	Data map[string]any `json:"data"`
}

func (c *ClientImpl) accountTaskRequest(ctx context.Context, cookiesStr, endpoint, api, version string, data map[string]any, referer string) (*accountTaskResponse, string, error) {
	current := cookiesStr
	if session := cookieSessionFromContext(ctx); session != nil {
		current, _, _ = session.State()
	}
	var lastRet []string
	for attempt := 0; attempt < 3; attempt++ {
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
		if !isTokenExpiredRet(decoded.Ret) {
			return nil, updated, fmt.Errorf("%s 返回失败: %s", api, firstRet(decoded.Ret))
		}
		current = updated
		if current == cookiesStr {
			refreshed, refreshErr := c.RefreshTokenContext(ctx, current)
			if refreshErr != nil {
				return nil, current, fmt.Errorf("刷新 mtop token: %w", refreshErr)
			}
			current = refreshed.UpdatedCookies
		}
		if err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return nil, current, err
		}
	}
	return nil, current, fmt.Errorf("%s token 重试失败: %s", api, firstRet(lastRet))
}

func (c *ClientImpl) accountTaskRequestOnce(ctx context.Context, cookiesStr, endpoint, api, version string, data map[string]any, referer string) (*accountTaskResponse, string, error) {
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, referer, endpoint)
	token := protocol.SignToken(signingCookies)
	if token == "" {
		return nil, cookiesStr, fmt.Errorf("cookie 缺少 _m_h5_tk，无法调用 %s", api)
	}
	rawData, err := json.Marshal(data)
	if err != nil {
		return nil, cookiesStr, err
	}
	dataVal := string(rawData)
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sign := protocol.GenerateSign(t, token, dataVal)
	query := url.Values{}
	query.Set("jsv", "2.7.2")
	query.Set("appKey", protocol.SignAppKey)
	query.Set("t", t)
	query.Set("sign", sign)
	query.Set("v", version)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+query.Encode(),
		strings.NewReader("data="+url.QueryEscape(dataVal)))
	if err != nil {
		return nil, cookiesStr, err
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Referer", referer)
	if parsedReferer, parseErr := url.Parse(referer); parseErr == nil && parsedReferer.Scheme != "" && parsedReferer.Host != "" {
		req.Header.Set("Origin", parsedReferer.Scheme+"://"+parsedReferer.Host)
	}
	resp, err := c.httpClientWithTimeout(25 * time.Second).Do(req)
	if err != nil {
		return nil, cookiesStr, err
	}
	defer resp.Body.Close()
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, updated, err
	}
	var decoded accountTaskResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, updated, fmt.Errorf("解析 %s 响应: %w", api, err)
	}
	return &decoded, updated, nil
}

func firstRet(ret []string) string {
	if len(ret) == 0 {
		return "未知响应"
	}
	return ret[0]
}

func firstNonEmptyURL(configured, fallback string) string {
	if strings.TrimSpace(configured) != "" {
		return configured
	}
	return fallback
}

func findStringField(value any, keys ...string) string {
	wanted := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		wanted[key] = struct{}{}
	}
	var walk func(any) string
	walk = func(v any) string {
		switch x := v.(type) {
		case map[string]any:
			for key, child := range x {
				if _, ok := wanted[key]; ok {
					if text := mtopString(child); text != "" {
						return text
					}
				}
			}
			for _, child := range x {
				if text := walk(child); text != "" {
					return text
				}
			}
		case []any:
			for _, child := range x {
				if text := walk(child); text != "" {
					return text
				}
			}
		}
		return ""
	}
	return walk(value)
}
