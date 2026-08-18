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
		// res、ret、updatedCookies、err 用于本次流程后续判断的res、ret、updatedCookies、err
		res, ret, updatedCookies, err := c.fetchItemsPageOnce(ctx, currentCookies, pageNumber, pageSize)
		if err != nil {
			return nil, err
		}
		lastRet = ret
		if res != nil {
			return res, nil
		}
		if isSessionExpiredRet(ret) {
			return nil, sessionExpiredError("商品列表接口", ret)
		}
		if !isTokenExpiredRet(ret) {
			return nil, fmt.Errorf("商品列表接口返回非成功: ret=%v", ret)
		}
		if updatedCookies != "" && updatedCookies != currentCookies {
			currentCookies = updatedCookies
			if // err 用于本次流程后续判断的err
			err := sleepCtx(ctx, MTopRetryGap); err != nil {
				return nil, err
			}
			continue
		}
		if // err 用于本次流程后续判断的err
		err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return nil, err
		}
		// refreshed、err 用于本次流程后续判断的refreshed、err
		refreshed, err := c.RefreshTokenContext(ctx, currentCookies)
		if err != nil {
			return nil, fmt.Errorf("刷新 mtop token 失败: %w", err)
		}
		currentCookies = refreshed.UpdatedCookies
	}
	return nil, fmt.Errorf("商品列表接口 token 重试失败: ret=%v", lastRet)
}

// fetchItemsPageOnce 封装fetch商品列表页码Once业务协调。
func (c *ClientImpl) fetchItemsPageOnce(ctx context.Context, cookiesStr string, pageNumber, pageSize int) (*ItemListResult, []string, string, error) {
	// hc 用于本次流程后续判断的hc
	hc := c.httpClient()
	// signingCookies、requestCookies 用于本次流程后续判断的signingCookies、requestCookies
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, "https://www.goofish.com/", ItemListAPI)
	// cookies 用于本次流程后续判断的cookies
	cookies := protocol.TransCookies(signingCookies)
	// userID 用于本次流程后续判断的用户ID
	userID := cookies["unb"]
	if userID == "" {
		return nil, nil, cookiesStr, fmt.Errorf("cookie 缺少 unb 字段，无法获取商品列表")
	}

	// data 用于本次流程后续判断的数据
	data := map[string]any{
		"needGroupInfo": false,
		"pageNumber":    pageNumber,
		"pageSize":      pageSize,
		"groupName":     "在售",
		"groupId":       "58877261",
		"defaultGroup":  true,
		"userId":        userID,
	}
	// rawData 用于本次流程后续判断的原始数据
	rawData, _ := json.Marshal(data)
	// dataVal 用于本次流程后续判断的数据Val
	dataVal := string(rawData)
	// t 用于本次流程后续判断的t
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// sign 用于本次流程后续判断的sign
	sign := protocol.GenerateSign(t, protocol.SignToken(signingCookies), dataVal)
	// query 用于本次流程后续判断的查询
	query := buildItemListQuery(t, sign)
	// body 用于本次流程后续判断的请求体
	body := "data=" + url.QueryEscape(dataVal)

	// req、err 用于本次流程后续判断的req、err
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ItemListAPI+"?"+query, strings.NewReader(body))
	if err != nil {
		return nil, nil, cookiesStr, err
	}
	setCommonHeaders(req, requestCookies)
	// resp、err 用于本次流程后续判断的resp、err
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, cookiesStr, fmt.Errorf("商品列表请求失败: %w", err)
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
		return nil, nil, updated, fmt.Errorf("解析商品列表响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	if !hasMTopSuccess(decoded.Ret) {
		return nil, decoded.Ret, updated, nil
	}

	// items 用于本次流程后续判断的商品列表
	items := parseItemList(decoded.Data)
	// totalCount、totalPages 用于本次流程后续判断的总数Count、totalPages
	totalCount, totalPages := itemListPagination(decoded.Data, pageNumber, pageSize)
	return &ItemListResult{
		Items:          items,
		PageNumber:     pageNumber,
		PageSize:       pageSize,
		CurrentCount:   len(items),
		TotalCount:     totalCount,
		TotalPages:     totalPages,
		SavedCountHint: len(items),
		UpdatedCookies: updated,
	}, decoded.Ret, updated, nil
}

// FetchAllItems 自动分页获取卖家全部在售商品。maxPages <= 0 表示不限页。
func (c *ClientImpl) FetchAllItems(ctx context.Context, cookiesStr string, pageSize, maxPages int) (*ItemListResult, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	// currentCookies 用于本次流程后续判断的currentCookies
	currentCookies := cookiesStr
	if // session 用于本次流程后续判断的会话
	session := cookieSessionFromContext(ctx); session != nil {
		currentCookies, _, _ = session.State()
	}
	// page 用于本次流程后续判断的页码
	page := 1
	// fetchedPages 用于本次流程后续判断的fetchedPages
	fetchedPages := 0
	// all 用于本次流程后续判断的all
	var all []ItemListItem
	for maxPages <= 0 || page <= maxPages {
		// res、err 用于本次流程后续判断的res、err
		res, err := c.FetchItemsPage(ctx, currentCookies, page, pageSize)
		if err != nil {
			return nil, err
		}
		currentCookies = res.UpdatedCookies
		all = append(all, res.Items...)
		fetchedPages = page
		if res.TotalPages > 0 && page >= res.TotalPages {
			break
		}
		if res.TotalPages <= 0 && len(res.Items) < pageSize {
			break
		}
		page++
		if // err 用于本次流程后续判断的err
		err := sleepCtx(ctx, ItemPageGap); err != nil {
			return nil, err
		}
	}
	return &ItemListResult{
		Items:          all,
		PageNumber:     1,
		PageSize:       pageSize,
		CurrentCount:   len(all),
		TotalCount:     len(all),
		TotalPages:     fetchedPages,
		SavedCountHint: len(all),
		UpdatedCookies: currentCookies,
	}, nil
}

// itemListPagination 封装商品ListPagination业务协调。
func itemListPagination(data map[string]any, pageNumber, pageSize int) (totalCount, totalPages int) {
	// key 表示当前遍历过程中的key
	for _, key := range []string{"totalCount", "total_count", "total"} {
		if // value 用于本次流程后续判断的值
		value := mtopInt(data[key]); value > 0 {
			totalCount = value
			break
		}
	}
	// key 表示当前遍历过程中的key
	for _, key := range []string{"pageCount", "page_count", "totalPages", "total_pages"} {
		if // value 用于本次流程后续判断的值
		value := mtopInt(data[key]); value > 0 {
			totalPages = value
			break
		}
	}
	if totalPages == 0 && totalCount > 0 && pageSize > 0 {
		totalPages = (totalCount + pageSize - 1) / pageSize
	}
	if totalCount == 0 && totalPages > 0 && pageSize > 0 {
		totalCount = totalPages * pageSize
	}
	if totalPages > 0 && pageNumber > totalPages {
		totalPages = pageNumber
	}
	return totalCount, totalPages
}

// buildItemListQuery 封装build商品List查询业务协调。
func buildItemListQuery(t, sign string) string {
	// parts 用于本次流程后续判断的parts
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
	// b 用于本次流程后续判断的b
	var b strings.Builder
	// i、p 表示当前遍历过程中的i、p
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

// parseItemList 封装parse商品List业务协调。
func parseItemList(data map[string]any) []ItemListItem {
	// cardList 用于本次流程后续判断的卡密List
	cardList, _ := data["cardList"].([]any)
	// items 用于本次流程后续判断的商品列表
	items := make([]ItemListItem, 0, len(cardList))
	// rawCard 表示当前遍历过程中的原始卡密
	for _, rawCard := range cardList {
		// card、ok 用于本次流程后续判断的card、ok
		card, ok := rawCard.(map[string]any)
		if !ok {
			continue
		}
		// cardData 用于本次流程后续判断的卡密数据
		cardData, _ := card["cardData"].(map[string]any)
		if cardData == nil {
			continue
		}
		// detailParams 用于本次流程后续判断的detailParams
		detailParams, _ := cardData["detailParams"].(map[string]any)
		// itemID 用于本次流程后续判断的商品ID
		itemID := mtopString(detailParams["itemId"])
		if itemID == "" {
			itemID = mtopString(cardData["id"])
		}
		if itemID == "" || strings.HasPrefix(itemID, "auto_") {
			continue
		}
		// priceInfo 用于本次流程后续判断的priceInfo
		priceInfo, _ := cardData["priceInfo"].(map[string]any)
		// price 用于本次流程后续判断的price
		price := mtopString(priceInfo["price"])
		// priceText 用于本次流程后续判断的price文本
		priceText := mtopString(priceInfo["preText"]) + price
		// picInfo 用于本次流程后续判断的picInfo
		picInfo, _ := cardData["picInfo"].(map[string]any)
		// picURL 用于本次流程后续判断的picURL
		picURL := mtopString(picInfo["picUrl"])
		// detailURL 用于本次流程后续判断的detailURL
		detailURL := mtopString(cardData["detailUrl"])
		// detail 用于本次流程后续判断的detail
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
		// detailJSON 用于本次流程后续判断的detailJSON
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
			IsMultiSpec: detectItemMultiSpec(cardData),
		})
	}
	return items
}
