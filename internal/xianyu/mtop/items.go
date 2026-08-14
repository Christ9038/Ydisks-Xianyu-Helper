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
		// res、ret、updatedCookies、err 保存res、ret、updatedCookies、err，供当前处理流程使用
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
			if // err 保存err，供当前处理流程使用
			err := sleepCtx(ctx, MTopRetryGap); err != nil {
				return nil, err
			}
			continue
		}
		if // err 保存err，供当前处理流程使用
		err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return nil, err
		}
		// refreshed、err 保存refreshed、err，供当前处理流程使用
		refreshed, err := c.RefreshTokenContext(ctx, currentCookies)
		if err != nil {
			return nil, fmt.Errorf("刷新 mtop token 失败: %w", err)
		}
		currentCookies = refreshed.UpdatedCookies
	}
	return nil, fmt.Errorf("商品列表接口 token 重试失败: ret=%v", lastRet)
}

// fetchItemsPageOnce 负责fetch商品列表页码Once相关处理。
func (c *ClientImpl) fetchItemsPageOnce(ctx context.Context, cookiesStr string, pageNumber, pageSize int) (*ItemListResult, []string, string, error) {
	// hc 保存hc，供当前处理流程使用
	hc := c.httpClient()
	// signingCookies、requestCookies 保存signingCookies、requestCookies，供当前处理流程使用
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, "https://www.goofish.com/", ItemListAPI)
	// cookies 保存cookies，供当前处理流程使用
	cookies := protocol.TransCookies(signingCookies)
	// userID 保存用户ID，供当前处理流程使用
	userID := cookies["unb"]
	if userID == "" {
		return nil, nil, cookiesStr, fmt.Errorf("cookie 缺少 unb 字段，无法获取商品列表")
	}

	// data 保存数据，供当前处理流程使用
	data := map[string]any{
		"needGroupInfo": false,
		"pageNumber":    pageNumber,
		"pageSize":      pageSize,
		"groupName":     "在售",
		"groupId":       "58877261",
		"defaultGroup":  true,
		"userId":        userID,
	}
	// rawData 保存原始数据，供当前处理流程使用
	rawData, _ := json.Marshal(data)
	// dataVal 保存数据Val，供当前处理流程使用
	dataVal := string(rawData)
	// t 保存t，供当前处理流程使用
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// sign 保存sign，供当前处理流程使用
	sign := protocol.GenerateSign(t, protocol.SignToken(signingCookies), dataVal)
	// query 保存查询，供当前处理流程使用
	query := buildItemListQuery(t, sign)
	// body 保存请求体，供当前处理流程使用
	body := "data=" + url.QueryEscape(dataVal)

	// req、err 保存req、err，供当前处理流程使用
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ItemListAPI+"?"+query, strings.NewReader(body))
	if err != nil {
		return nil, nil, cookiesStr, err
	}
	setCommonHeaders(req, requestCookies)
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, cookiesStr, fmt.Errorf("商品列表请求失败: %w", err)
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
		return nil, nil, updated, fmt.Errorf("解析商品列表响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	if !hasMTopSuccess(decoded.Ret) {
		return nil, decoded.Ret, updated, nil
	}

	// items 保存商品列表，供当前处理流程使用
	items := parseItemList(decoded.Data)
	// totalCount、totalPages 保存总数Count、totalPages，供当前处理流程使用
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
	// currentCookies 保存currentCookies，供当前处理流程使用
	currentCookies := cookiesStr
	if // session 保存会话，供当前处理流程使用
	session := cookieSessionFromContext(ctx); session != nil {
		currentCookies, _, _ = session.State()
	}
	// page 保存页码，供当前处理流程使用
	page := 1
	// fetchedPages 保存fetchedPages，供当前处理流程使用
	fetchedPages := 0
	// all 保存all，供当前处理流程使用
	var all []ItemListItem
	for maxPages <= 0 || page <= maxPages {
		// res、err 保存res、err，供当前处理流程使用
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
		if // err 保存err，供当前处理流程使用
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

// itemListPagination 负责商品ListPagination相关处理。
func itemListPagination(data map[string]any, pageNumber, pageSize int) (totalCount, totalPages int) {
	// key 表示当前遍历过程中的key
	for _, key := range []string{"totalCount", "total_count", "total"} {
		if // value 保存值，供当前处理流程使用
		value := mtopInt(data[key]); value > 0 {
			totalCount = value
			break
		}
	}
	// key 表示当前遍历过程中的key
	for _, key := range []string{"pageCount", "page_count", "totalPages", "total_pages"} {
		if // value 保存值，供当前处理流程使用
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

// buildItemListQuery 负责build商品List查询相关处理。
func buildItemListQuery(t, sign string) string {
	// parts 保存parts，供当前处理流程使用
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
	// b 保存b，供当前处理流程使用
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

// parseItemList 负责parse商品List相关处理。
func parseItemList(data map[string]any) []ItemListItem {
	// cardList 保存卡密List，供当前处理流程使用
	cardList, _ := data["cardList"].([]any)
	// items 保存商品列表，供当前处理流程使用
	items := make([]ItemListItem, 0, len(cardList))
	// rawCard 表示当前遍历过程中的原始卡密
	for _, rawCard := range cardList {
		// card、ok 保存card、ok，供当前处理流程使用
		card, ok := rawCard.(map[string]any)
		if !ok {
			continue
		}
		// cardData 保存卡密数据，供当前处理流程使用
		cardData, _ := card["cardData"].(map[string]any)
		if cardData == nil {
			continue
		}
		// detailParams 保存detailParams，供当前处理流程使用
		detailParams, _ := cardData["detailParams"].(map[string]any)
		// itemID 保存商品ID，供当前处理流程使用
		itemID := mtopString(detailParams["itemId"])
		if itemID == "" {
			itemID = mtopString(cardData["id"])
		}
		if itemID == "" || strings.HasPrefix(itemID, "auto_") {
			continue
		}
		// priceInfo 保存priceInfo，供当前处理流程使用
		priceInfo, _ := cardData["priceInfo"].(map[string]any)
		// price 保存price，供当前处理流程使用
		price := mtopString(priceInfo["price"])
		// priceText 保存price文本，供当前处理流程使用
		priceText := mtopString(priceInfo["preText"]) + price
		// picInfo 保存picInfo，供当前处理流程使用
		picInfo, _ := cardData["picInfo"].(map[string]any)
		// picURL 保存picURL，供当前处理流程使用
		picURL := mtopString(picInfo["picUrl"])
		// detailURL 保存detailURL，供当前处理流程使用
		detailURL := mtopString(cardData["detailUrl"])
		// detail 保存detail，供当前处理流程使用
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
		// detailJSON 保存detailJSON，供当前处理流程使用
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
