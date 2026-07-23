package browser

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"

	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

const mtopOrderDetailURL = "mtop.idle.web.trade.order.detail"

// OrderDetail 订单详情（对齐 engine.OrderDetail）。
type OrderDetail struct {
	OrderID                string                        `json:"order_id"`
	Quantity               string                        `json:"quantity"`
	SpecName               string                        `json:"spec_name"`
	SpecValue              string                        `json:"spec_value"`
	OrderStatus            string                        `json:"order_status"`
	Amount                 string                        `json:"amount"`
	UpdatedCookies         string                        `json:"-"`
	CookieSnapshot         []cookierefresh.BrowserCookie `json:"-"`
	CookieSnapshotComplete bool                          `json:"-"`
}

// orderStatusMap 移植自 order_fetcher_optimized._parse_api_response。纯映射，可单测。
var orderStatusMap = map[int]string{
	1: "processing", 2: "pending_ship", 3: "shipped",
	4: "completed", 7: "refunding", 8: "cancelled",
	9: "refunding", 10: "cancelled", 11: "completed", 12: "cancelled",
}

// FetchOrderDetail 用浏览器抓取单个订单详情。
// 移植自 utils/order_fetcher_optimized.py fetch_order_complete。
func (m *Manager) FetchOrderDetail(ctx context.Context, orderID, cookieID, cookieValue string, requireSpec ...bool) (*OrderDetail, error) {
	needsSpec := len(requireSpec) > 0 && requireSpec[0]
	// 优先调用稳定的 MTop 详情接口；页面自动化只作为规格字段的兼容兜底。
	if direct, directErr := mtop.NewClient().FetchOrderDetail(ctx, cookieValue, orderID); directErr == nil && direct != nil && direct.Amount != "" &&
		(!needsSpec || (direct.SpecName != "" && direct.SpecValue != "")) {
		return &OrderDetail{
			OrderID: orderID, Quantity: direct.Quantity, SpecName: direct.SpecName, SpecValue: direct.SpecValue,
			OrderStatus: direct.OrderStatus, Amount: direct.Amount, UpdatedCookies: direct.UpdatedCookies,
		}, nil
	} else if directErr != nil {
		m.logger.Warn("MTop 订单详情获取失败，回退浏览器", "order_id", orderID, "err", directErr)
	}
	// A shared Chromium context owns one mutable Cookie Jar. Serialize browser
	// fallback refreshes for the account so concurrent batch rows cannot clear
	// and re-inject different snapshots underneath each other.
	browserCredentialLock := m.accountRenewLock("order_credential_" + cookieID)
	browserCredentialLock.Lock()
	defer browserCredentialLock.Unlock()
	if err := m.init(); err != nil {
		return nil, err
	}
	page, release, err := m.newPage(ctx, cookieID, cookieValue, true)
	if err != nil {
		return nil, err
	}
	defer release()
	if session := mtop.CookieSessionFromContext(ctx); session != nil {
		if err := syncCredentialCookies(page.Context(), cookieValue, session.Snapshot()); err != nil {
			return nil, fmt.Errorf("订单详情同步完整 Cookie Jar: %w", err)
		}
	}

	var mu sync.Mutex
	var apiBody map[string]any
	var observedAPIs []string

	// 路由拦截：fetch mtop 订单详情 API 响应体。
	_ = page.Route("**/*", func(route playwright.Route) {
		requestURL := route.Request().URL()
		if parsed, parseErr := url.Parse(requestURL); parseErr == nil && strings.Contains(parsed.Host, "goofish.com") {
			apiName := parsed.Query().Get("api")
			if apiName == "" && strings.Contains(parsed.Path, "/h5/") {
				parts := strings.Split(parsed.Path, "/")
				if len(parts) > 2 {
					apiName = parts[2]
				}
			}
			if apiName != "" {
				mu.Lock()
				if len(observedAPIs) < 30 {
					observedAPIs = append(observedAPIs, apiName)
				}
				mu.Unlock()
			}
		}
		if strings.Contains(requestURL, mtopOrderDetailURL) {
			resp, err := route.Fetch()
			if err == nil {
				var body map[string]any
				if err := resp.JSON(&body); err == nil {
					mu.Lock()
					apiBody = body
					mu.Unlock()
				}
			}
		}
		_ = route.Continue()
	})

	url := fmt.Sprintf("https://www.goofish.com/order-detail?orderId=%s&role=seller", orderID)
	if _, err := page.Goto(url, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
		Timeout:   playwright.Float(30000),
	}); err != nil {
		m.logger.Warn("访问订单详情页超时", "err", err)
	}
	time.Sleep(2 * time.Second)
	_, _ = page.Evaluate("() => window.scrollTo(0, document.body.scrollHeight)")
	time.Sleep(500 * time.Millisecond)
	_, _ = page.Evaluate("() => window.scrollTo(0, 0)")
	time.Sleep(1 * time.Second)

	mu.Lock()
	body := apiBody
	mu.Unlock()

	od := &OrderDetail{OrderID: orderID}

	if body != nil {
		if d, ok := body["data"].(map[string]any); ok {
			parseAPIResponse(od, d)
		}
	}

	// DOM 补充（金额 / SKU）。
	parseDOMContent(page, od)
	allCookies, cookieErr := page.Context().Cookies()
	if cookieErr != nil {
		return nil, fmt.Errorf("读取订单详情浏览器 Cookie Jar: %w", cookieErr)
	}
	od.CookieSnapshot = cookieSnapshotFromPlaywright(allCookies)
	if od.CookieSnapshot == nil {
		od.CookieSnapshot = []cookierefresh.BrowserCookie{}
	}
	od.CookieSnapshotComplete = true
	od.UpdatedCookies = currentCookieHeader(od.CookieSnapshot, goofishIMURL)
	if session := mtop.CookieSessionFromContext(ctx); session != nil {
		session.ReplaceSnapshot(od.CookieSnapshot)
	}
	if od.Amount == "" {
		title, _ := page.Title()
		m.logger.Warn("订单详情未解析到实付金额", "order_id", orderID, "page_title", title, "page_url", page.URL(), "api_captured", body != nil, "observed_apis", observedAPIs)
	}
	return od, nil
}

func parseAPIResponse(od *OrderDetail, data map[string]any) {
	// 订单状态。
	if ut, ok := data["utArgs"].(map[string]any); ok {
		if code, ok := ut["orderStatus"].(float64); ok {
			if s, ok := orderStatusMap[int(code)]; ok {
				od.OrderStatus = s
			}
		}
	}
	// 组件中找金额和规格。
	comps, _ := data["components"].([]any)
	for _, c := range comps {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if cm["render"] == "orderInfoVO" {
			// 当前接口把 priceInfo 放在 component.data 下；兼容旧结构的顶层字段。
			componentData, _ := cm["data"].(map[string]any)
			priceInfo, _ := componentData["priceInfo"].(map[string]any)
			if priceInfo == nil {
				priceInfo, _ = cm["priceInfo"].(map[string]any)
			}
			if priceInfo != nil {
				amount, _ := priceInfo["amount"].(map[string]any)
				if v, ok := amount["value"].(string); ok {
					od.Amount = v
				}
			}
		}
	}
}

var currencyAmountRE = regexp.MustCompile(`[¥￥]\s*([0-9]+(?:\.[0-9]{1,2})?)`)
var plainAmountRE = regexp.MustCompile(`^\s*([0-9]+(?:\.[0-9]{1,2})?)\s*$`)

func parseDOMContent(page playwright.Page, od *OrderDetail) {
	// 金额。
	if od.Amount == "" {
		if el, err := page.QuerySelector(`.boldNum--JgEOXfA3`); err == nil && el != nil {
			if t, err := el.TextContent(); err == nil {
				od.Amount = strings.TrimSpace(t)
			}
		}
	}
	if od.Amount == "" {
		if raw, err := page.Evaluate(`() => document.body?.innerText || ''`); err == nil {
			if pageText, ok := raw.(string); ok {
				od.Amount = extractPaidAmountFromText(pageText)
			}
		}
	}

	// SKU（规格+数量）。移植自 _parse_dom_content。
	if skuEls, err := page.QuerySelectorAll(`.sku--u_ddZval`); err == nil && len(skuEls) >= 1 {
		t, _ := skuEls[0].TextContent()
		parts := strings.SplitN(t, ":", 2)
		if len(parts) == 2 {
			od.SpecName = strings.TrimSpace(parts[0])
			od.SpecValue = strings.TrimSpace(parts[1])
		}
		if len(skuEls) >= 2 {
			q, _ := skuEls[1].TextContent()
			od.Quantity = strings.TrimPrefix(strings.TrimSpace(q), "x")
		}
	}
	if od.Quantity == "" {
		od.Quantity = "1"
	}
}

func extractPaidAmountFromText(pageText string) string {
	labels := []string{"实付款", "实付金额", "订单金额", "合计"}
	rawLines := strings.Split(pageText, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	for i, line := range lines {
		matchedLabel := false
		for _, label := range labels {
			if strings.Contains(line, label) {
				matchedLabel = true
				break
			}
		}
		if !matchedLabel {
			continue
		}
		end := i + 3
		if end > len(lines) {
			end = len(lines)
		}
		for _, candidate := range lines[i:end] {
			if match := currencyAmountRE.FindStringSubmatch(candidate); len(match) == 2 {
				return match[1]
			}
			if candidate != line {
				if match := plainAmountRE.FindStringSubmatch(candidate); len(match) == 2 {
					return match[1]
				}
			}
		}
	}
	return ""
}

// BatchRefreshOrders 并发抓取多个订单详情（semaphore=5）。
func (m *Manager) BatchRefreshOrders(ctx context.Context, orderIDs []string, cookieID, cookieValue string) (map[string]any, error) {
	sem := make(chan struct{}, 5)
	type result struct {
		id  string
		od  *OrderDetail
		err error
	}
	results := make([]result, len(orderIDs))
	var wg sync.WaitGroup
	for i, id := range orderIDs {
		wg.Add(1)
		go func(idx int, oid string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			od, err := m.FetchOrderDetail(ctx, oid, cookieID, cookieValue)
			results[idx] = result{id: oid, od: od, err: err}
		}(i, id)
	}
	wg.Wait()

	orders := make([]map[string]any, 0, len(orderIDs))
	for _, r := range results {
		if r.err != nil {
			orders = append(orders, map[string]any{"order_id": r.id, "success": false, "error": r.err.Error()})
			continue
		}
		orders = append(orders, map[string]any{
			"order_id": r.od.OrderID, "success": true,
			"quantity": r.od.Quantity, "spec_name": r.od.SpecName, "spec_value": r.od.SpecValue,
			"order_status": r.od.OrderStatus, "amount": r.od.Amount,
		})
	}
	return map[string]any{"orders": orders}, nil
}
