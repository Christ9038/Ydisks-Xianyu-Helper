package browser

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"

	"xianyu-go/internal/logsafe"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// mtopOrderDetailURL 用于本次流程后续判断的mtop订单DetailURL
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
// FetchOrderDetail 封装Fetch订单Detail业务协调。
func (m *Manager) FetchOrderDetail(ctx context.Context, orderID, cookieID, cookieValue string, requireSpec ...bool) (*OrderDetail, error) {
	// needsSpec 用于本次流程后续判断的needsSpec
	needsSpec := len(requireSpec) > 0 && requireSpec[0]
	// 优先调用稳定的 MTop 详情接口；页面自动化只作为规格字段的兼容兜底。
	if direct, directErr := mtop.NewClient().FetchOrderDetail(ctx, cookieValue, orderID); directErr == nil && direct != nil && direct.Amount != "" &&
		(!needsSpec || (direct.SpecName != "" && direct.SpecValue != "")) {
		return &OrderDetail{
			OrderID: orderID, Quantity: direct.Quantity, SpecName: direct.SpecName, SpecValue: direct.SpecValue,
			OrderStatus: direct.OrderStatus, Amount: direct.Amount, UpdatedCookies: direct.UpdatedCookies,
		}, nil
	} else if directErr != nil {
		m.logger.Warn("MTop 订单详情获取失败，回退浏览器", "order_id", orderID, "err", logsafe.Error(directErr))
	}
	// A shared Chromium context owns one mutable Cookie Jar. Serialize browser
	// fallback refreshes for the account so concurrent batch rows cannot clear
	// and re-inject different snapshots underneath each other.
	// browserCredentialLock 用于本次流程后续判断的浏览器Credential锁
	browserCredentialLock := m.accountRenewLock("order_credential_" + cookieID)
	browserCredentialLock.Lock()
	defer browserCredentialLock.Unlock()
	if // err 用于本次流程后续判断的err
	err := m.init(); err != nil {
		return nil, err
	}
	// page、release、err 用于本次流程后续判断的page、release、err
	page, release, err := m.newPage(ctx, cookieID, cookieValue, true)
	if err != nil {
		return nil, err
	}
	defer release()
	if // session 用于本次流程后续判断的会话
	session := mtop.CookieSessionFromContext(ctx); session != nil {
		if // err 用于本次流程后续判断的err
		err := syncCredentialCookies(page.Context(), cookieValue, session.Snapshot()); err != nil {
			return nil, fmt.Errorf("订单详情同步完整 Cookie Jar: %w", err)
		}
	}

	// mu 用于本次流程后续判断的mu
	var mu sync.Mutex
	// apiBody 用于本次流程后续判断的api请求体
	var apiBody map[string]any
	// observedAPIs 用于本次流程后续判断的observedAPIs
	var observedAPIs []string

	// 路由拦截：fetch mtop 订单详情 API 响应体。
	_ = page.Route("**/*", func(route playwright.Route) {
		// requestURL 用于本次流程后续判断的请求URL
		requestURL := route.Request().URL()
		if // parsed、parseErr 用于本次流程后续判断的parsed、parseErr
		parsed, parseErr := url.Parse(requestURL); parseErr == nil && strings.Contains(parsed.Host, "goofish.com") {
			// apiName 用于本次流程后续判断的api名称
			apiName := parsed.Query().Get("api")
			if apiName == "" && strings.Contains(parsed.Path, "/h5/") {
				// parts 用于本次流程后续判断的parts
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
			// resp、err 用于本次流程后续判断的resp、err
			resp, err := route.Fetch()
			if err == nil {
				// body 用于本次流程后续判断的请求体
				var body map[string]any
				if // err 用于本次流程后续判断的err
				err := resp.JSON(&body); err == nil {
					mu.Lock()
					apiBody = body
					mu.Unlock()
				}
			}
		}
		_ = route.Continue()
	})

	// url 用于本次流程后续判断的地址
	url := fmt.Sprintf("https://www.goofish.com/order-detail?orderId=%s&role=seller", orderID)
	if // err 用于本次流程后续判断的err
	_, err := page.Goto(url, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
		Timeout:   playwright.Float(30000),
	}); err != nil {
		m.logger.Warn("访问订单详情页超时", "err", logsafe.Error(err))
	}
	time.Sleep(2 * time.Second)
	_, _ = page.Evaluate("() => window.scrollTo(0, document.body.scrollHeight)")
	time.Sleep(500 * time.Millisecond)
	_, _ = page.Evaluate("() => window.scrollTo(0, 0)")
	time.Sleep(1 * time.Second)

	mu.Lock()
	// body 用于本次流程后续判断的请求体
	body := apiBody
	mu.Unlock()

	// od 用于本次流程后续判断的od
	od := &OrderDetail{OrderID: orderID}

	if body != nil {
		if // d、ok 用于本次流程后续判断的d、ok
		d, ok := body["data"].(map[string]any); ok {
			parseAPIResponse(od, d)
		}
	}

	// DOM 补充（金额 / SKU）。
	parseDOMContent(page, od)
	// allCookies、cookieErr 用于本次流程后续判断的allCookies、cookieErr
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
	if // session 用于本次流程后续判断的会话
	session := mtop.CookieSessionFromContext(ctx); session != nil {
		session.ReplaceSnapshot(od.CookieSnapshot)
	}
	if od.Amount == "" {
		// title 用于本次流程后续判断的标题
		title, _ := page.Title()
		m.logger.Warn("订单详情未解析到实付金额", "order_id", orderID, "page_title", title, "page_url", logsafe.URL(page.URL()), "api_captured", body != nil, "observed_apis", observedAPIs)
	}
	return od, nil
}

// parseAPIResponse 封装parseAPI响应业务协调。
func parseAPIResponse(od *OrderDetail, data map[string]any) {
	// 订单状态。
	if ut, ok := data["utArgs"].(map[string]any); ok {
		if // code、ok 用于本次流程后续判断的code、ok
		code, ok := ut["orderStatus"].(float64); ok {
			if // s、ok 用于本次流程后续判断的s、ok
			s, ok := orderStatusMap[int(code)]; ok {
				od.OrderStatus = s
			}
		}
	}
	// 组件中找金额和规格。
	comps, _ := data["components"].([]any)
	// c 表示当前遍历过程中的c
	for _, c := range comps {
		// cm、ok 用于本次流程后续判断的cm、ok
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if cm["render"] == "orderInfoVO" {
			// 当前接口把 priceInfo 放在 component.data 下；兼容旧结构的顶层字段。
			componentData, _ := cm["data"].(map[string]any)
			// priceInfo 用于本次流程后续判断的priceInfo
			priceInfo, _ := componentData["priceInfo"].(map[string]any)
			if priceInfo == nil {
				priceInfo, _ = cm["priceInfo"].(map[string]any)
			}
			if priceInfo != nil {
				// amount 用于本次流程后续判断的amount
				amount, _ := priceInfo["amount"].(map[string]any)
				if // v、ok 用于本次流程后续判断的v、ok
				v, ok := amount["value"].(string); ok {
					od.Amount = v
				}
			}
		}
	}
}

// currencyAmountRE 用于本次流程后续判断的currencyAmountRE
var currencyAmountRE = regexp.MustCompile(`[¥￥]\s*([0-9]+(?:\.[0-9]{1,2})?)`)

// plainAmountRE 用于本次流程后续判断的plainAmountRE
var plainAmountRE = regexp.MustCompile(`^\s*([0-9]+(?:\.[0-9]{1,2})?)\s*$`)

// parseDOMContent 封装parseDOM内容业务协调。
func parseDOMContent(page playwright.Page, od *OrderDetail) {
	// 金额。
	if od.Amount == "" {
		if // el、err 用于本次流程后续判断的el、err
		el, err := page.QuerySelector(`.boldNum--JgEOXfA3`); err == nil && el != nil {
			if // t、err 用于本次流程后续判断的t、err
			t, err := el.TextContent(); err == nil {
				od.Amount = strings.TrimSpace(t)
			}
		}
	}
	if od.Amount == "" {
		if // raw、err 用于本次流程后续判断的raw、err
		raw, err := page.Evaluate(`() => document.body?.innerText || ''`); err == nil {
			if // pageText、ok 用于本次流程后续判断的页码Text、ok
			pageText, ok := raw.(string); ok {
				od.Amount = extractPaidAmountFromText(pageText)
			}
		}
	}

	// SKU（规格+数量）。移植自 _parse_dom_content。
	if skuEls, err := page.QuerySelectorAll(`.sku--u_ddZval`); err == nil && len(skuEls) >= 1 {
		// t 用于本次流程后续判断的t
		t, _ := skuEls[0].TextContent()
		// parts 用于本次流程后续判断的parts
		parts := strings.SplitN(t, ":", 2)
		if len(parts) == 2 {
			od.SpecName = strings.TrimSpace(parts[0])
			od.SpecValue = strings.TrimSpace(parts[1])
		}
		if len(skuEls) >= 2 {
			// q 用于本次流程后续判断的q
			q, _ := skuEls[1].TextContent()
			od.Quantity = strings.TrimPrefix(strings.TrimSpace(q), "x")
		}
	}
	if od.Quantity == "" {
		od.Quantity = "1"
	}
}

// extractPaidAmountFromText 封装extractPaidAmountFrom文本业务协调。
func extractPaidAmountFromText(pageText string) string {
	// labels 用于本次流程后续判断的labels
	labels := []string{"实付款", "实付金额", "订单金额", "合计"}
	// rawLines 用于本次流程后续判断的原始Lines
	rawLines := strings.Split(pageText, "\n")
	// lines 用于本次流程后续判断的lines
	lines := make([]string, 0, len(rawLines))
	// line 表示当前遍历过程中的line
	for _, line := range rawLines {
		if // trimmed 用于本次流程后续判断的trimmed
		trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	// i、line 表示当前遍历过程中的i、line
	for i, line := range lines {
		// matchedLabel 用于本次流程后续判断的matchedLabel
		matchedLabel := false
		// label 表示当前遍历过程中的label
		for _, label := range labels {
			if strings.Contains(line, label) {
				matchedLabel = true
				break
			}
		}
		if !matchedLabel {
			continue
		}
		// end 用于本次流程后续判断的结束
		end := i + 3
		if end > len(lines) {
			end = len(lines)
		}
		// candidate 表示当前遍历过程中的candidate
		for _, candidate := range lines[i:end] {
			if // match 用于本次流程后续判断的match
			match := currencyAmountRE.FindStringSubmatch(candidate); len(match) == 2 {
				return match[1]
			}
			if candidate != line {
				if // match 用于本次流程后续判断的match
				match := plainAmountRE.FindStringSubmatch(candidate); len(match) == 2 {
					return match[1]
				}
			}
		}
	}
	return ""
}

// BatchRefreshOrders 并发抓取多个订单详情（semaphore=5）。
func (m *Manager) BatchRefreshOrders(ctx context.Context, orderIDs []string, cookieID, cookieValue string) (map[string]any, error) {
	// sem 用于本次流程后续判断的sem
	sem := make(chan struct{}, 5)
	// result 用于本次流程后续判断的结果
	type result struct {
		id  string
		od  *OrderDetail
		err error
	}
	// results 用于本次流程后续判断的results
	results := make([]result, len(orderIDs))
	// wg 用于本次流程后续判断的wg
	var wg sync.WaitGroup
	// i、id 表示当前遍历过程中的i、id
	for i, id := range orderIDs {
		wg.Add(1)
		go func(idx int, oid string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// od、err 用于本次流程后续判断的od、err
			od, err := m.FetchOrderDetail(ctx, oid, cookieID, cookieValue)
			results[idx] = result{id: oid, od: od, err: err}
		}(i, id)
	}
	wg.Wait()

	// orders 用于本次流程后续判断的订单列表
	orders := make([]map[string]any, 0, len(orderIDs))
	// r 表示当前遍历过程中的r
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
