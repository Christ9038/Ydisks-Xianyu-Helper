// Package mtop: 商品列表域 — mtop.idle.web.xyh.item.list 调用、分页与解析。
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

// FetchItemsPage 获取指定页卖家在售商品列表。
func (c *ClientImpl) FetchItemsPage(ctx context.Context, cookiesStr string, pageNumber, pageSize int) (*ItemListResult, error) {
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

func (c *ClientImpl) fetchItemsPageOnce(ctx context.Context, cookiesStr string, pageNumber, pageSize int) (*ItemListResult, []string, string, error) {
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
	raw, err := readMTopBody(resp)
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
func (c *ClientImpl) FetchAllItems(ctx context.Context, cookiesStr string, pageSize, maxPages int) (*ItemListResult, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	currentCookies := cookiesStr
	page := 1
	var all []ItemListItem
	for maxPages <= 0 || page <= maxPages {
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
