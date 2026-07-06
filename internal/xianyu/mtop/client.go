// Package mtop 实现闲鱼 mtop H5 API 客户端。
// 关键：签名只覆盖 (t, token, data_val)，与 URL query 参数无关；token 取自 cookie _m_h5_tk 前半段。
package mtop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/xianyu"
	"xianyu-go/internal/xianyu/protocol"
)

// RegAppKey 是 WS 注册用的 appKey（与签名用的 protocol.SignAppKey 不同）。
const RegAppKey = "444e9908a51d1cb236a27862abc769c9"

// TokenAPI 取 access token 的端点。
const TokenAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idlemessage.pc.login.token/1.0/"

// ConsignAPI 是虚拟商品确认发货端点。
const ConsignAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.logistic.consign.dummy/1.0/"

// OrderDetailAPI 是卖家订单详情端点。
const OrderDetailAPI = "https://h5api.m.goofish.com/h5/mtop.idle.web.trade.order.detail/1.0/"

// UserPageNavAPI 是 PC 站当前登录账号资料端点。
const UserPageNavAPI = "https://h5api.m.goofish.com/h5/mtop.idle.web.user.page.nav/1.0/"

// ItemListAPI 是卖家商品列表端点。
const ItemListAPI = "https://h5api.m.goofish.com/h5/mtop.idle.web.xyh.item.list/1.0/"

const (
	MTopRetryGap = time.Second
	ItemPageGap  = time.Second
)

// Client 是 mtop API 客户端。零值可用；HTTP 超时默认 30s。
type Client struct {
	HTTPClient     *http.Client
	TokenURL       string
	ConsignURL     string
	OrderDetailURL string
}

// RefreshResult 是刷新 token 的结果。
type RefreshResult struct {
	AccessToken    string // 用于 WS /reg 注册
	UpdatedCookies string // 合并 Set-Cookie 后的新 cookie 字符串（无变化则与入参相同）
}

// UserProfileResult 是 mtop.idle.web.user.page.nav 返回的当前账号资料。
type UserProfileResult struct {
	Nickname       string
	DisplayNick    string
	AvatarURL      string
	UpdatedCookies string
}

// ItemListResult 是卖家商品列表结果。
type ItemListResult struct {
	Items          []ItemListItem
	PageNumber     int
	PageSize       int
	CurrentCount   int
	TotalCount     int
	TotalPages     int
	SavedCountHint int
	UpdatedCookies string
}

// ItemListItem 是 mtop.idle.web.xyh.item.list 的核心商品信息。
type ItemListItem struct {
	ID          string
	Title       string
	Price       string
	PriceText   string
	CategoryID  string
	DetailURL   string
	WebURL      string
	PicURL      string
	ItemDetail  string
	AuctionType string
	ItemStatus  int
}

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
func (c *Client) FetchOrderDetail(ctx context.Context, cookiesStr, orderID string) (*OrderDetailResult, error) {
	currentCookies := cookiesStr
	var lastRet []string
	for attempt := 0; attempt < 4; attempt++ {
		previousCookies := currentCookies
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
		if !isTokenExpiredRet(ret) {
			return nil, fmt.Errorf("订单详情接口返回非成功: ret=%v", ret)
		}
		if attempt == 3 {
			break
		}
		if currentCookies == previousCookies {
			refreshed, refreshErr := c.RefreshTokenContext(ctx, currentCookies)
			if refreshErr != nil {
				return nil, fmt.Errorf("订单详情 token 刷新失败: %w", refreshErr)
			}
			currentCookies = refreshed.UpdatedCookies
		}
		if err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("订单详情 token 重试失败: ret=%v", lastRet)
}

func (c *Client) fetchOrderDetailOnce(ctx context.Context, cookiesStr, orderID string) (*OrderDetailResult, []string, string, error) {
	hc := c.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	cookies := protocol.TransCookies(cookiesStr)
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	dataVal := `{"tid":"` + orderID + `"}`
	sign := protocol.GenerateSign(t, protocol.SignToken(cookiesStr), dataVal)
	endpoint := c.OrderDetailURL
	if endpoint == "" {
		endpoint = OrderDetailAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+buildOrderDetailQuery(t, sign), strings.NewReader("data="+url.QueryEscape(dataVal)))
	if err != nil {
		return nil, nil, cookiesStr, err
	}
	setCommonHeaders(req, cookiesStr)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, cookiesStr, fmt.Errorf("订单详情请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, cookiesStr, err
	}
	updated := mergeSetCookie(cookiesStr, cookies, resp)
	var decoded struct {
		Ret  []string       `json:"ret"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, nil, updated, fmt.Errorf("解析订单详情响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	if !hasMTopSuccess(decoded.Ret) {
		return nil, decoded.Ret, updated, nil
	}
	result := &OrderDetailResult{Quantity: "1"}
	if utArgs, ok := decoded.Data["utArgs"].(map[string]any); ok {
		result.OrderStatus = mtopString(utArgs["orderStatus"])
	}
	components, _ := decoded.Data["components"].([]any)
	for _, component := range components {
		cm, _ := component.(map[string]any)
		if cm["render"] != "orderInfoVO" {
			continue
		}
		componentData, _ := cm["data"].(map[string]any)
		if itemInfo, ok := componentData["itemInfo"].(map[string]any); ok {
			if value := mtopString(itemInfo["buyAmount"]); value != "" {
				result.Quantity = value
			}
			result.SpecName = mtopString(itemInfo["specName"])
			result.SpecValue = mtopString(itemInfo["specValue"])
		}
		if priceInfo, ok := componentData["priceInfo"].(map[string]any); ok {
			if amount, ok := priceInfo["amount"].(map[string]any); ok {
				result.Amount = mtopString(amount["value"])
			}
		}
	}
	return result, decoded.Ret, updated, nil
}

// FetchUserProfile 获取当前 cookie 对应账号的实时昵称和头像。
func (c *Client) FetchUserProfile(ctx context.Context, cookiesStr string) (*UserProfileResult, error) {
	currentCookies := cookiesStr
	var lastRet []string
	for attempt := 0; attempt < 4; attempt++ {
		res, ret, updatedCookies, err := c.fetchUserProfileOnce(ctx, currentCookies)
		if err != nil {
			return nil, err
		}
		lastRet = ret
		if res != nil {
			return res, nil
		}
		if !isTokenExpiredRet(ret) {
			return nil, fmt.Errorf("账号资料接口返回非成功: ret=%v", ret)
		}
		if updatedCookies != "" && updatedCookies != currentCookies {
			currentCookies = updatedCookies
			if err := sleepCtx(ctx, MTopRetryGap); err != nil {
				return nil, err
			}
			continue
		}
		if err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return nil, err
		}
		refreshed, err := c.RefreshToken(currentCookies)
		if err != nil {
			return nil, fmt.Errorf("刷新 mtop token 失败: %w", err)
		}
		currentCookies = refreshed.UpdatedCookies
	}
	return nil, fmt.Errorf("账号资料接口 token 重试失败: ret=%v", lastRet)
}

func (c *Client) fetchUserProfileOnce(ctx context.Context, cookiesStr string) (*UserProfileResult, []string, string, error) {
	hc := c.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}

	cookies := protocol.TransCookies(cookiesStr)
	dataVal := "{}"
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sign := protocol.GenerateSign(t, protocol.SignToken(cookiesStr), dataVal)
	query := buildUserPageNavQuery(t, sign)
	body := "data=" + url.QueryEscape(dataVal)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, UserPageNavAPI+"?"+query, strings.NewReader(body))
	if err != nil {
		return nil, nil, cookiesStr, err
	}
	setCommonHeaders(req, cookiesStr)

	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, cookiesStr, fmt.Errorf("账号资料请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, cookiesStr, err
	}
	updated := mergeSetCookie(cookiesStr, cookies, resp)

	var decoded struct {
		Ret  []string       `json:"ret"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, nil, updated, fmt.Errorf("解析账号资料响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	if !hasMTopSuccess(decoded.Ret) {
		return nil, decoded.Ret, updated, nil
	}

	profile := parseUserProfile(decoded.Data)
	profile.UpdatedCookies = updated
	return profile, decoded.Ret, updated, nil
}

// Consign 调用 mtop.taobao.idle.logistic.consign.dummy 确认发货（虚拟发货）。
// data_val 形如 {"orderId":"...","tradeText":"","picList":[],"newUnconsign":true}。
// 返回成功标志、响应 ret 列表、可能更新后的 cookie。
// 移植自 secure_confirm_decrypted.auto_confirm。
func (c *Client) Consign(cookiesStr, orderID string) (ok bool, ret []string, updatedCookies string, err error) {
	return c.ConsignContext(context.Background(), cookiesStr, orderID)
}

// ConsignContext 确认发货；签名 token 过期时使用响应下发的新 Cookie 重签并重试。
func (c *Client) ConsignContext(ctx context.Context, cookiesStr, orderID string) (ok bool, ret []string, updatedCookies string, err error) {
	currentCookies := cookiesStr
	var lastRet []string
	for attempt := 0; attempt < 4; attempt++ {
		previousCookies := currentCookies
		ok, ret, updated, requestErr := c.consignOnce(ctx, currentCookies, orderID)
		if requestErr != nil {
			return false, ret, currentCookies, requestErr
		}
		lastRet = ret
		if updated != "" {
			currentCookies = updated
		}
		if ok {
			return true, ret, currentCookies, nil
		}
		if !isTokenExpiredRet(ret) {
			return false, ret, currentCookies, nil
		}
		if attempt == 3 {
			break
		}

		// MTop 通常会在 token 过期响应中通过 Set-Cookie 下发新签名 token。
		// 若没有下发，则主动调用 token API 尝试刷新一次。
		if currentCookies == previousCookies {
			refreshed, refreshErr := c.RefreshTokenContext(ctx, currentCookies)
			if refreshErr != nil {
				return false, ret, currentCookies, fmt.Errorf("consign token 过期且刷新失败: %w", refreshErr)
			}
			if refreshed.UpdatedCookies != "" {
				currentCookies = refreshed.UpdatedCookies
			}
		}
		if err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return false, ret, currentCookies, err
		}
	}
	return false, lastRet, currentCookies, nil
}

func (c *Client) consignOnce(ctx context.Context, cookiesStr, orderID string) (ok bool, ret []string, updatedCookies string, err error) {
	hc := c.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	cookies := protocol.TransCookies(cookiesStr)
	token := protocol.SignToken(cookiesStr)
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	dataVal := `{"orderId":"` + orderID + `", "tradeText":"","picList":[],"newUnconsign":true}`
	sign := protocol.GenerateSign(t, token, dataVal)

	query := buildConsignQuery(t, sign)
	body := "data=" + url.QueryEscape(dataVal)
	consignURL := c.ConsignURL
	if consignURL == "" {
		consignURL = ConsignAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		consignURL+"?"+query,
		strings.NewReader(body))
	if err != nil {
		return false, nil, cookiesStr, err
	}
	setCommonHeaders(req, cookiesStr)
	resp, err := hc.Do(req)
	if err != nil {
		return false, nil, cookiesStr, fmt.Errorf("consign 请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, nil, cookiesStr, err
	}
	updated := mergeSetCookie(cookiesStr, cookies, resp)
	var res struct {
		Ret []string `json:"ret"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return false, nil, updated, fmt.Errorf("解析 consign 响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	for _, r := range res.Ret {
		if strings.Contains(r, "SUCCESS::调用成功") {
			return true, res.Ret, updated, nil
		}
	}
	return false, res.Ret, updated, nil
}

// FetchItemsPage 获取指定页卖家在售商品列表。
func (c *Client) FetchItemsPage(ctx context.Context, cookiesStr string, pageNumber, pageSize int) (*ItemListResult, error) {
	if pageNumber < 1 {
		pageNumber = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	currentCookies := cookiesStr
	var lastRet []string
	for attempt := 0; attempt < 4; attempt++ {
		res, ret, updatedCookies, err := c.fetchItemsPageOnce(ctx, currentCookies, pageNumber, pageSize)
		if err != nil {
			return nil, err
		}
		lastRet = ret
		if res != nil {
			return res, nil
		}
		if !isTokenExpiredRet(ret) {
			return nil, fmt.Errorf("商品列表接口返回非成功: ret=%v", ret)
		}
		if updatedCookies != "" && updatedCookies != currentCookies {
			currentCookies = updatedCookies
			if err := sleepCtx(ctx, MTopRetryGap); err != nil {
				return nil, err
			}
			continue
		}
		if err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return nil, err
		}
		refreshed, err := c.RefreshToken(currentCookies)
		if err != nil {
			return nil, fmt.Errorf("刷新 mtop token 失败: %w", err)
		}
		currentCookies = refreshed.UpdatedCookies
	}
	return nil, fmt.Errorf("商品列表接口 token 重试失败: ret=%v", lastRet)
}

func (c *Client) fetchItemsPageOnce(ctx context.Context, cookiesStr string, pageNumber, pageSize int) (*ItemListResult, []string, string, error) {
	hc := c.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	cookies := protocol.TransCookies(cookiesStr)
	userID := cookies["unb"]
	if userID == "" {
		return nil, nil, cookiesStr, fmt.Errorf("cookie 缺少 unb 字段，无法获取商品列表")
	}

	data := map[string]any{
		"needGroupInfo": false,
		"pageNumber":    pageNumber,
		"pageSize":      pageSize,
		"groupName":     "在售",
		"groupId":       "58877261",
		"defaultGroup":  true,
		"userId":        userID,
	}
	rawData, _ := json.Marshal(data)
	dataVal := string(rawData)
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sign := protocol.GenerateSign(t, protocol.SignToken(cookiesStr), dataVal)
	query := buildItemListQuery(t, sign)
	body := "data=" + url.QueryEscape(dataVal)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ItemListAPI+"?"+query, strings.NewReader(body))
	if err != nil {
		return nil, nil, cookiesStr, err
	}
	setCommonHeaders(req, cookiesStr)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, cookiesStr, fmt.Errorf("商品列表请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, cookiesStr, err
	}
	updated := mergeSetCookie(cookiesStr, cookies, resp)

	var decoded struct {
		Ret  []string       `json:"ret"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, nil, updated, fmt.Errorf("解析商品列表响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	if !hasMTopSuccess(decoded.Ret) {
		return nil, decoded.Ret, updated, nil
	}

	items := parseItemList(decoded.Data)
	return &ItemListResult{
		Items:          items,
		PageNumber:     pageNumber,
		PageSize:       pageSize,
		CurrentCount:   len(items),
		SavedCountHint: len(items),
		UpdatedCookies: updated,
	}, decoded.Ret, updated, nil
}

// FetchAllItems 自动分页获取卖家全部在售商品。maxPages <= 0 表示不限页。
func (c *Client) FetchAllItems(ctx context.Context, cookiesStr string, pageSize, maxPages int) (*ItemListResult, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	currentCookies := cookiesStr
	page := 1
	var all []ItemListItem
	for {
		if maxPages > 0 && page > maxPages {
			break
		}
		res, err := c.FetchItemsPage(ctx, currentCookies, page, pageSize)
		if err != nil {
			return nil, err
		}
		currentCookies = res.UpdatedCookies
		all = append(all, res.Items...)
		if len(res.Items) < pageSize {
			break
		}
		page++
		if err := sleepCtx(ctx, ItemPageGap); err != nil {
			return nil, err
		}
	}
	return &ItemListResult{
		Items:          all,
		PageNumber:     1,
		PageSize:       pageSize,
		CurrentCount:   len(all),
		TotalCount:     len(all),
		TotalPages:     page,
		SavedCountHint: len(all),
		UpdatedCookies: currentCookies,
	}, nil
}

func buildItemListQuery(t, sign string) string {
	parts := [][2]string{
		{"jsv", "2.7.2"},
		{"appKey", protocol.SignAppKey},
		{"t", t},
		{"sign", sign},
		{"v", "1.0"},
		{"type", "originaljson"},
		{"accountSite", "xianyu"},
		{"dataType", "json"},
		{"timeout", "20000"},
		{"api", "mtop.idle.web.xyh.item.list"},
		{"sessionOption", "AutoLoginOnly"},
		{"spm_cnt", "a21ybx.im.0.0"},
		{"spm_pre", "a21ybx.collection.menu.1.272b5141NafCNK"},
	}
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(p[0])
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(p[1]))
	}
	return b.String()
}

func buildUserPageNavQuery(t, sign string) string {
	parts := [][2]string{
		{"jsv", "2.7.2"},
		{"appKey", protocol.SignAppKey},
		{"t", t},
		{"sign", sign},
		{"v", "1.0"},
		{"type", "originaljson"},
		{"accountSite", "xianyu"},
		{"dataType", "json"},
		{"timeout", "20000"},
		{"api", "mtop.idle.web.user.page.nav"},
		{"sessionOption", "AutoLoginOnly"},
		{"ecode", "0"},
		{"spm_cnt", "a21ybx.home.0.0"},
	}
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(p[0])
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(p[1]))
	}
	return b.String()
}

func buildOrderDetailQuery(t, sign string) string {
	return "jsv=2.7.2&appKey=" + protocol.SignAppKey +
		"&t=" + t + "&sign=" + sign +
		"&v=1.0&type=originaljson&accountSite=xianyu&dataType=json&timeout=20000" +
		"&api=mtop.idle.web.trade.order.detail&sessionOption=AutoLoginOnly&valueType=string"
}

func parseUserProfile(data map[string]any) *UserProfileResult {
	module, _ := data["module"].(map[string]any)
	base, _ := module["base"].(map[string]any)
	if base == nil {
		return &UserProfileResult{}
	}
	nickname := strings.TrimSpace(mtopString(base["displayName"]))
	displayNick := strings.TrimSpace(mtopString(base["displayNick"]))
	if nickname == "" {
		nickname = displayNick
	}
	return &UserProfileResult{
		Nickname:    nickname,
		DisplayNick: displayNick,
		AvatarURL:   strings.TrimSpace(mtopString(base["avatar"])),
	}
}

func parseItemList(data map[string]any) []ItemListItem {
	cardList, _ := data["cardList"].([]any)
	items := make([]ItemListItem, 0, len(cardList))
	for _, rawCard := range cardList {
		card, ok := rawCard.(map[string]any)
		if !ok {
			continue
		}
		cardData, _ := card["cardData"].(map[string]any)
		if cardData == nil {
			continue
		}
		detailParams, _ := cardData["detailParams"].(map[string]any)
		itemID := mtopString(detailParams["itemId"])
		if itemID == "" {
			itemID = mtopString(cardData["id"])
		}
		if itemID == "" || strings.HasPrefix(itemID, "auto_") {
			continue
		}
		priceInfo, _ := cardData["priceInfo"].(map[string]any)
		price := mtopString(priceInfo["price"])
		priceText := mtopString(priceInfo["preText"]) + price
		picInfo, _ := cardData["picInfo"].(map[string]any)
		picURL := mtopString(picInfo["picUrl"])
		detailURL := mtopString(cardData["detailUrl"])
		detail := map[string]any{
			"title":           mtopString(cardData["title"]),
			"price":           price,
			"price_text":      priceText,
			"category_id":     mtopString(cardData["categoryId"]),
			"auction_type":    mtopString(cardData["auctionType"]),
			"item_status":     mtopInt(cardData["itemStatus"]),
			"detail_url":      detailURL,
			"web_url":         "https://www.goofish.com/item?id=" + itemID,
			"pic_info":        picInfo,
			"detail_params":   detailParams,
			"track_params":    cardData["trackParams"],
			"item_label_data": cardData["itemLabelDataVO"],
			"card_type":       mtopInt(card["cardType"]),
		}
		detailJSON, _ := json.Marshal(detail)
		items = append(items, ItemListItem{
			ID:          itemID,
			Title:       mtopString(cardData["title"]),
			Price:       price,
			PriceText:   priceText,
			CategoryID:  mtopString(cardData["categoryId"]),
			DetailURL:   detailURL,
			WebURL:      "https://www.goofish.com/item?id=" + itemID,
			PicURL:      picURL,
			ItemDetail:  string(detailJSON),
			AuctionType: mtopString(cardData["auctionType"]),
			ItemStatus:  mtopInt(cardData["itemStatus"]),
		})
	}
	return items
}

func hasMTopSuccess(ret []string) bool {
	for _, r := range ret {
		if strings.Contains(r, "SUCCESS::调用成功") {
			return true
		}
	}
	return false
}

func isTokenExpiredRet(ret []string) bool {
	for _, r := range ret {
		lower := strings.ToLower(r)
		if strings.Contains(lower, "token") ||
			strings.Contains(r, "FAIL_SYS_TOKEN_EXOIRED") ||
			strings.Contains(r, "FAIL_SYS_TOKEN_EXPIRED") ||
			strings.Contains(r, "FAIL_SYS_SESSION_EXPIRED") {
			return true
		}
	}
	return false
}

// IsSessionExpiredErr 判断错误是否表示 cookie/session 已彻底失效（需密码登录刷新）。
func IsSessionExpiredErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "fail_sys_session_expired") ||
		strings.Contains(msg, "session过期") ||
		strings.Contains(msg, "登录凭证已失效")
}

func mtopString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case int:
		return strconv.Itoa(x)
	case json.Number:
		return x.String()
	default:
		return ""
	}
}

func mtopInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		n, _ := strconv.Atoi(x)
		return n
	case json.Number:
		n, _ := strconv.Atoi(x.String())
		return n
	default:
		return 0
	}
}

func buildConsignQuery(t, sign string) string {
	parts := [][2]string{
		{"jsv", "2.7.2"},
		{"appKey", protocol.SignAppKey},
		{"t", t},
		{"sign", sign},
		{"v", "1.0"},
		{"type", "originaljson"},
		{"accountSite", "xianyu"},
		{"dataType", "json"},
		{"timeout", "20000"},
		{"api", "mtop.taobao.idle.logistic.consign.dummy"},
		{"sessionOption", "AutoLoginOnly"},
	}
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(p[0])
		b.WriteByte('=')
		b.WriteString(p[1])
	}
	return b.String()
}

// RefreshToken 调用 mtop.taobao.idlemessage.pc.login.token 获取 accessToken。
// 遇到 mtop 签名 token 过期时，仅在响应下发了新 Cookie 后低频重试一次。
func (c *Client) RefreshToken(cookiesStr string) (*RefreshResult, error) {
	return c.RefreshTokenContext(context.Background(), cookiesStr)
}

// RefreshTokenContext 是支持取消的 RefreshToken 版本。
func (c *Client) RefreshTokenContext(ctx context.Context, cookiesStr string) (*RefreshResult, error) {
	return c.RefreshTokenWithDeviceIDContext(ctx, cookiesStr, "")
}

// RefreshTokenWithDeviceIDContext 使用指定 deviceId 获取 accessToken。
// 闲鱼 IM token 和 WS /reg 的 did 是绑定校验关系：token 请求里的 deviceId
// 必须与 /reg.headers.did 完全一致，否则 /reg 会返回
// "device id or appkey is not equal"。
func (c *Client) RefreshTokenWithDeviceIDContext(ctx context.Context, cookiesStr, deviceID string) (*RefreshResult, error) {
	currentCookies := cookiesStr
	for attempt := 0; attempt < 2; attempt++ {
		accessToken, ret, updatedCookies, status, err := c.refreshTokenOnce(ctx, currentCookies, deviceID)
		if err != nil {
			return &RefreshResult{UpdatedCookies: currentCookies}, err
		}
		if accessToken != "" {
			return &RefreshResult{AccessToken: accessToken, UpdatedCookies: updatedCookies}, nil
		}
		if !isTokenExpiredRet(ret) {
			return &RefreshResult{UpdatedCookies: updatedCookies}, fmt.Errorf("token API 返回非成功: ret=%v (status=%d)", ret, status)
		}
		// 没有新 Cookie 时继续请求只会重复失败，也会增加风控风险。
		if updatedCookies == "" || updatedCookies == currentCookies || attempt == 1 {
			return &RefreshResult{UpdatedCookies: updatedCookies}, fmt.Errorf("token API 登录凭证已失效: ret=%v (status=%d)", ret, status)
		}
		currentCookies = updatedCookies
		if err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return &RefreshResult{UpdatedCookies: currentCookies}, err
		}
	}
	return &RefreshResult{UpdatedCookies: currentCookies}, fmt.Errorf("token API 登录凭证已失效")
}

func (c *Client) refreshTokenOnce(ctx context.Context, cookiesStr, deviceID string) (string, []string, string, int, error) {
	hc := c.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}

	cookies := protocol.TransCookies(cookiesStr)
	myid := cookies["unb"]
	if myid == "" {
		return "", nil, cookiesStr, 0, fmt.Errorf("cookie 缺少 unb 字段，无法生成 deviceId")
	}
	if strings.TrimSpace(deviceID) == "" {
		deviceID = protocol.GenerateDeviceID(myid)
	}
	token := protocol.SignToken(cookiesStr)

	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	dataVal := `{"appKey":"` + RegAppKey + `","deviceId":"` + deviceID + `"}`

	// 签名不覆盖 query，因此 query 的编码细节不影响验签。
	query := buildTokenQuery(t, protocol.GenerateSign(t, token, dataVal))

	body := "data=" + url.QueryEscape(dataVal)

	tokenURL := c.TokenURL
	if tokenURL == "" {
		tokenURL = TokenAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL+"?"+query, strings.NewReader(body))
	if err != nil {
		return "", nil, cookiesStr, 0, err
	}
	setCommonHeaders(req, cookiesStr)

	resp, err := hc.Do(req)
	if err != nil {
		return "", nil, cookiesStr, 0, fmt.Errorf("token API 请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, cookiesStr, resp.StatusCode, err
	}
	// 即使业务返回 token 过期，也要保留响应下发的新签名 Cookie。
	updated := mergeSetCookie(cookiesStr, cookies, resp)

	var res struct {
		Ret  []string `json:"ret"`
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", nil, updated, resp.StatusCode, fmt.Errorf("解析 token 响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}

	ok := false
	for _, r := range res.Ret {
		if strings.Contains(r, "SUCCESS::调用成功") {
			ok = true
			break
		}
	}
	if !ok {
		return "", res.Ret, updated, resp.StatusCode, nil
	}
	if res.Data.AccessToken == "" {
		return "", res.Ret, updated, resp.StatusCode, fmt.Errorf("token API 成功但 accessToken 为空 (body=%s)", truncate(string(raw), 300))
	}
	return res.Data.AccessToken, res.Ret, updated, resp.StatusCode, nil
}

// buildTokenQuery 构造 token API 的 query string。
// 值按原样拼接（dangerouslySetWindvaneParams 已是单次编码），不做二次编码。
func buildTokenQuery(t, sign string) string {
	parts := [][2]string{
		{"jsv", "2.7.2"},
		{"appKey", protocol.SignAppKey},
		{"t", t},
		{"sign", sign},
		{"v", "1.0"},
		{"type", "originaljson"},
		{"accountSite", "xianyu"},
		{"dataType", "json"},
		{"timeout", "20000"},
		{"api", "mtop.taobao.idlemessage.pc.login.token"},
		{"sessionOption", "AutoLoginOnly"},
		{"dangerouslySetWindvaneParams", "%5Bobject%20Object%5D"},
		{"smToken", "token"},
		{"queryToken", "sm"},
		{"sm", "sm"},
		{"spm_cnt", "a21ybx.im.0.0"},
		{"spm_pre", "a21ybx.home.sidebar.1.4c053da6vYwnmf"},
		{"log_id", "4c053da6vYwnmf"},
	}
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(p[0])
		b.WriteByte('=')
		b.WriteString(p[1])
	}
	return b.String()
}

func setCommonHeaders(req *http.Request, cookiesStr string) {
	h := req.Header
	h.Set("accept", "application/json")
	h.Set("accept-language", "zh-CN,zh;q=0.9,en;q=0.8")
	h.Set("cache-control", "no-cache")
	h.Set("content-type", "application/x-www-form-urlencoded")
	h.Set("pragma", "no-cache")
	h.Set("priority", "u=1, i")
	h.Set("sec-ch-ua", xianyu.SecChUA)
	h.Set("sec-ch-ua-mobile", "?0")
	h.Set("sec-ch-ua-platform", `"Windows"`)
	h.Set("sec-fetch-dest", "empty")
	h.Set("sec-fetch-mode", "cors")
	h.Set("sec-fetch-site", "same-site")
	h.Set("user-agent", xianyu.BrowserUA)
	h.Set("referer", "https://www.goofish.com/")
	h.Set("origin", "https://www.goofish.com")
	h.Set("cookie", cookiesStr)
}

// mergeSetCookie 把响应的 Set-Cookie 合并回 cookie 字符串。
func mergeSetCookie(orig string, current map[string]string, resp *http.Response) string {
	setCookies := resp.Header["Set-Cookie"]
	if len(setCookies) == 0 {
		return orig
	}
	changed := false
	for _, sc := range setCookies {
		// Set-Cookie: name=value; Path=/; ...
		pair := sc
		if i := strings.Index(pair, ";"); i >= 0 {
			pair = pair[:i]
		}
		if eq := strings.Index(pair, "="); eq >= 0 {
			name := strings.TrimSpace(pair[:eq])
			val := strings.TrimSpace(pair[eq+1:])
			if name != "" {
				current[name] = val
				changed = true
			}
		}
	}
	if !changed {
		return orig
	}
	var b strings.Builder
	first := true
	for k, v := range current {
		if !first {
			b.WriteString("; ")
		}
		first = false
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v)
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
