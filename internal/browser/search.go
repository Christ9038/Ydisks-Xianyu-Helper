package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

const mtopSearchURL = "h5api.m.goofish.com/h5/mtop.taobao.idlemtopsearch.pc.search"

// SearchItem 商品搜索结果。
type SearchItem struct {
	Title     string `json:"title"`
	Price     string `json:"price"`
	WantCount int    `json:"want_count"`
	Area      string `json:"area"`
	UserNick  string `json:"user_nick"`
	ItemURL   string `json:"item_url"`
	PicURL    string `json:"pic_url"`
	ItemID    string `json:"item_id"`
}

// SearchItems 搜索闲鱼商品，返回解析结果。
// 移植自 utils/item_search.py search_items。
func (m *Manager) SearchItems(ctx context.Context, keyword, cookieID, cookieValue string, page, pageSize int) (map[string]any, error) {
	if err := m.init(); err != nil {
		return nil, err
	}

	pw, release, err := m.newPage(ctx, cookieID, cookieValue, true)
	if err != nil {
		return nil, err
	}
	defer release()

	var mu sync.Mutex
	var responses []map[string]any

	pw.OnResponse(func(resp playwright.Response) {
		if !strings.Contains(resp.URL(), mtopSearchURL) {
			return
		}
		if resp.Status() != 200 {
			return
		}
		var body map[string]any
		if err := resp.JSON(&body); err == nil {
			mu.Lock()
			responses = append(responses, body)
			mu.Unlock()
		}
	})

	// 访问首页、注入 cookie、刷新。
	if _, err := pw.Goto("https://www.goofish.com", playwright.PageGotoOptions{Timeout: playwright.Float(30000)}); err != nil {
		return nil, fmt.Errorf("访问 goofish.com 失败: %w", err)
	}
	if cookieValue != "" {
		_ = addCookieStr(pw.Context(), cookieValue)
		_, _ = pw.Reload()
		time.Sleep(2 * time.Second)
	}

	// 填搜索框并提交。
	_, _ = pw.WaitForSelector(`input[class*="search-input"]`, playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(10000)})
	_ = pw.Fill(`input[class*="search-input"]`, keyword)
	_ = pw.Click(`button[type="submit"]`)

	// 等待 mtop 响应。
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := len(responses)
		mu.Unlock()
		if got > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// 可能出现滑块。
	content, _ := pw.Content()
	if strings.Contains(content, "nc-container") || strings.Contains(content, "scratch-captcha") {
		if err := solveSlider(pw, isScratchCaptcha(content), m.logger); err != nil {
			m.logger.Warn("搜索滑块处理失败", "err", err)
		}
		// 重试搜索。
		_ = pw.Fill(`input[class*="search-input"]`, keyword)
		_ = pw.Click(`button[type="submit"]`)
		time.Sleep(3 * time.Second)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(responses) == 0 {
		return map[string]any{"items": []any{}, "total": 0, "is_real_data": false}, nil
	}

	items := parseSearchResponse(responses[0])
	return map[string]any{"items": items, "total": len(items), "is_real_data": true, "source": "browser"}, nil
}

// parseSearchResponse 解析 mtop 搜索响应（移植自 _parse_real_item）。
func parseSearchResponse(body map[string]any) []SearchItem {
	data, _ := body["data"].(map[string]any)
	list, _ := data["resultList"].([]any)
	var items []SearchItem
	for _, raw := range list {
		item := parseSearchItem(raw)
		if item != nil {
			items = append(items, *item)
		}
	}
	return items
}

var wantRE = regexp.MustCompile(`(\d+(?:\.\d+)?(?:万)?)\s*人想要`)

func parseSearchItem(raw any) *SearchItem {
	r, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	d, _ := r["data"].(map[string]any)
	if d == nil {
		return nil
	}
	itemM, _ := d["item"].(map[string]any)
	if itemM == nil {
		return nil
	}
	main, _ := itemM["main"].(map[string]any)
	if main == nil {
		return nil
	}
	exContent, _ := main["exContent"].(map[string]any)
	clickParam, _ := main["clickParam"].(map[string]any)
	args, _ := clickParam["args"].(map[string]any)

	title, _ := exContent["title"].(string)
	area, _ := exContent["area"].(string)
	userNick, _ := exContent["userNickName"].(string)
	picURL, _ := exContent["picUrl"].(string)
	targetURL, _ := exContent["targetUrl"].(string)
	itemID, _ := args["content"].(string)

	// 价格：取 priceList 中的文字拼接。
	price := ""
	if pl, ok := exContent["priceList"].([]any); ok {
		var parts []string
		for _, p := range pl {
			if pm, ok := p.(map[string]any); ok {
				if t, ok := pm["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		price = strings.Join(parts, "")
		price = strings.TrimPrefix(price, "当前价")
	}

	// 想要数。
	wantCount := 0
	if tags, ok := exContent["fishTags"].([]any); ok {
		for _, t := range tags {
			if s, ok := t.(string); ok {
				if m := wantRE.FindStringSubmatch(s); len(m) > 1 {
					wantCount = parseWantCount(m[1])
				}
			}
		}
	}

	itemURL := strings.ReplaceAll(targetURL, "fleamarket://", "https://www.goofish.com/")

	return &SearchItem{
		Title:     title,
		Price:     price,
		WantCount: wantCount,
		Area:      area,
		UserNick:  userNick,
		PicURL:    picURL,
		ItemURL:   itemURL,
		ItemID:    itemID,
	}
}

func parseWantCount(s string) int {
	if strings.HasSuffix(s, "万") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(s, "万"), 64)
		if err == nil {
			return int(f * 10000)
		}
	}
	n, _ := strconv.Atoi(s)
	return n
}

// marshalSearchItems 供 HTTP handler 序列化。
func marshalSearchItems(items []SearchItem) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, it := range items {
		b, _ := json.Marshal(it)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		result = append(result, m)
	}
	return result
}
