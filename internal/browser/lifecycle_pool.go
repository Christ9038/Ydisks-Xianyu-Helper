package browser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"

	"xianyu-go/internal/xianyu"
)

// detectBrowserFingerprint 从受控 Chromium 读取运行时指纹，供浏览器与非浏览器协议请求统一使用。
func (m *Manager) detectBrowserFingerprint() error {
	// observed 用于本次流程后续判断的observed
	observed := make(chan http.Header, 1)
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- r.Header.Clone()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><title>fingerprint</title>"))
	}))
	defer server.Close()

	// b、err 用于本次流程后续判断的b、err
	b, err := m.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless:       playwright.Bool(true),
		Args:           chromiumLaunchArgs(),
		ExecutablePath: chromiumExecutablePath(),
	})
	if err != nil {
		return err
	}
	defer func() { _ = b.Close() }()
	// ctx、err 用于本次流程后续判断的ctx、err
	ctx, err := b.NewContext()
	if err != nil {
		return err
	}
	defer func() { _ = ctx.Close() }()
	// page、err 用于本次流程后续判断的page、err
	page, err := ctx.NewPage()
	if err != nil {
		return err
	}
	if // err 用于本次流程后续判断的err
	_, err := page.Goto(server.URL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
		return err
	}
	// headers 用于本次流程后续判断的headers
	var headers http.Header
	select {
	case headers = <-observed:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("等待 Chromium 指纹请求超时")
	}
	if strings.TrimSpace(headers.Get("User-Agent")) == "" {
		return fmt.Errorf("Chromium 返回空 userAgent")
	}
	// metadata 是本次实测 Chromium 的高熵 Client Hints；err 表示页面未暴露或无法读取该浏览器信息。
	metadata, err := readUserAgentMetadata(page)
	if err != nil {
		return fmt.Errorf("读取 Chromium User-Agent Client Hints 失败: %w", err)
	}
	// fingerprint 是去除无头产品标记后的运行时身份，供后续无头页面和协议请求一致使用。
	fingerprint := normalizeBrowserFingerprint(xianyu.BrowserFingerprint{
		UserAgent: headers.Get("User-Agent"),
		SecChUA:   headers.Get("sec-ch-ua"),
		Platform:  strings.Trim(headers.Get("sec-ch-ua-platform"), `"`),
		Mobile:    headers.Get("sec-ch-ua-mobile"),
	})
	m.browserFingerprint = fingerprint
	m.userAgentMetadata = normalizeUserAgentMetadata(metadata)
	xianyu.SetBrowserFingerprint(fingerprint)
	m.logger.Info("已读取 Playwright Chromium 浏览器指纹", "browser_version", b.Version(), "user_agent", fingerprint.UserAgent, "headless_token_removed", fingerprint.UserAgent != headers.Get("User-Agent"))
	return nil
}

// normalizeBrowserFingerprint 移除 Chromium 无头模式附加的产品标记，同时保留同一运行时实测的版本和 Client Hints。
func normalizeBrowserFingerprint(fingerprint xianyu.BrowserFingerprint) xianyu.BrowserFingerprint {
	fingerprint.UserAgent = normalizeHeadlessUserAgent(fingerprint.UserAgent)
	fingerprint.SecChUA = normalizeSecChUA(fingerprint.SecChUA)
	return fingerprint
}

// normalizeHeadlessUserAgent 只替换 UA 中的 HeadlessChrome 产品名，避免伪造浏览器版本、平台或其他身份字段。
func normalizeHeadlessUserAgent(userAgent string) string {
	return strings.ReplaceAll(strings.TrimSpace(userAgent), "HeadlessChrome/", "Chrome/")
}

// normalizeSecChUA 规范化 Sec-CH-UA 品牌并按品牌去重，保证无头标记不会通过 Client Hints 泄露。
func normalizeSecChUA(value string) string {
	// parts 是按逗号拆分的原始品牌条目，保留同一 Chromium 实测版本字段。
	parts := strings.Split(value, ",")
	// result 以原始顺序收集保留后的品牌条目；seen 防止替换 HeadlessChrome 后的重复品牌。
	result := make([]string, 0, len(parts))
	// seen 按规范化品牌名去重，避免同一产品经替换后重复出现在请求头。
	seen := make(map[string]struct{}, len(parts))
	// part 是当前待规范化的 Sec-CH-UA 品牌条目。
	for _, part := range parts {
		part = strings.TrimSpace(strings.ReplaceAll(part, "HeadlessChrome", "Chromium"))
		if part == "" {
			continue
		}
		// brand 是用于去重的品牌名，不含版本参数。
		brand := part
		// index 是品牌名与版本参数分隔分号的位置，负值表示条目没有参数。
		if index := strings.IndexByte(brand, ';'); index >= 0 {
			brand = strings.TrimSpace(brand[:index])
		}
		// exists 表示同名品牌已被保留，避免 HeadlessChrome 归一化后出现重复条目。
		if _, exists := seen[brand]; exists {
			continue
		}
		seen[brand] = struct{}{}
		result = append(result, part)
	}
	return strings.Join(result, ", ")
}

// readUserAgentMetadata 从页面读取 Chromium 公开的低、高熵 Client Hints，返回可传给 CDP 的对象或读取失败原因。
func readUserAgentMetadata(page playwright.Page) (map[string]any, error) {
	// value 是页面脚本返回的任意值；err 表示浏览器脚本执行或高熵字段读取失败。
	value, err := page.Evaluate(`async () => {
		const data = navigator.userAgentData;
		if (!data) return null;
		const high = await data.getHighEntropyValues([
			'architecture', 'bitness', 'fullVersionList', 'model', 'platformVersion', 'wow64'
		]);
		return {
			brands: data.brands,
			fullVersionList: high.fullVersionList,
			platform: data.platform,
			platformVersion: high.platformVersion,
			architecture: high.architecture,
			model: high.model,
			mobile: data.mobile,
			bitness: high.bitness,
			wow64: high.wow64
		};
	}`)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, fmt.Errorf("navigator.userAgentData 不可用")
	}
	// metadata 是可作为 CDP userAgentMetadata 使用的对象；ok 证明脚本结果的结构正确。
	metadata, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("navigator.userAgentData 类型异常: %T", value)
	}
	return metadata, nil
}

// normalizeUserAgentMetadata 复制 Client Hints 并仅规范化品牌列表，避免修改 Manager 持有的实测元数据。
func normalizeUserAgentMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	// result 是供当前页面 CDP 覆盖使用的独立元数据副本。
	result := make(map[string]any, len(metadata))
	// key 是当前 Client Hints 字段名；value 是其待复制或规范化的值。
	for key, value := range metadata {
		switch key {
		case "brands", "fullVersionList":
			result[key] = normalizeUserAgentBrands(value)
		default:
			result[key] = value
		}
	}
	return result
}

// normalizeUserAgentBrands 将品牌列表中的无头标记替换为 Chromium，并按品牌保留首个实测版本条目。
func normalizeUserAgentBrands(value any) []any {
	// brands 是页面返回的品牌数组；ok 表示该字段可以安全按数组处理。
	brands, ok := value.([]any)
	if !ok {
		return nil
	}
	// result 收集归一化后的品牌对象；seen 防止别名替换后的品牌重复。
	result := make([]any, 0, len(brands))
	// seen 记录已经保留的规范化品牌名，保证 CDP metadata 与 Sec-CH-UA 一致。
	seen := make(map[string]struct{}, len(brands))
	// item 是当前页面返回的候选品牌对象。
	for _, item := range brands {
		// brandEntry 是转换后的品牌对象；ok 表示候选条目有可读取字段。
		brandEntry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		// brand 是去重和覆盖写回使用的产品名，非字符串字段按空值丢弃。
		brand, _ := brandEntry["brand"].(string)
		brand = strings.ReplaceAll(brand, "HeadlessChrome", "Chromium")
		if brand == "" {
			continue
		}
		// exists 表示相同规范化品牌已经收录。
		if _, exists := seen[brand]; exists {
			continue
		}
		seen[brand] = struct{}{}
		// entry 是独立拷贝，防止更改本次页面覆盖时改写原始实测对象。
		entry := make(map[string]any, len(brandEntry))
		// key 是当前品牌字段名；itemValue 是需原样保留的版本等字段。
		for key, itemValue := range brandEntry {
			entry[key] = itemValue
		}
		entry["brand"] = brand
		result = append(result, entry)
	}
	return result
}

// headlessUserAgent 返回当前实测 Chromium 的规范化无头 UA；未完成指纹探测时回退全局实测值，仍不可用则返回 nil。
func (m *Manager) headlessUserAgent() *string {
	// userAgent 是移除 HeadlessChrome 标记后的 runtime UA，保持版本与协议侧指纹一致。
	userAgent := normalizeHeadlessUserAgent(m.browserFingerprint.UserAgent)
	if userAgent == "" {
		userAgent = normalizeHeadlessUserAgent(xianyu.CurrentBrowserFingerprint().UserAgent)
	}
	if userAgent == "" {
		return nil
	}
	return playwright.String(userAgent)
}

// newBrowserPage 从 bctx 创建页面；无头模式会在首次导航前应用 UA/Client Hints 覆盖，失败时关闭半成品页面并返回错误。
func (m *Manager) newBrowserPage(bctx playwright.BrowserContext, headless bool) (playwright.Page, error) {
	// page 是新建的浏览器页面；err 表示 context 不可用或页面创建失败。
	page, err := bctx.NewPage()
	if err != nil {
		return nil, err
	}
	if !headless {
		return page, nil
	}
	// err 表示页面级 CDP 指纹覆盖失败，不能继续导航以免暴露无头身份。
	if err := m.applyHeadlessFingerprint(page); err != nil {
		_ = page.Close()
		return nil, err
	}
	return page, nil
}

// applyHeadlessFingerprint 通过页面级 CDP 在首次导航前覆盖 UA 与 Client Hints；成功后故意保持会话附着以维持页面身份。
func (m *Manager) applyHeadlessFingerprint(page playwright.Page) error {
	// userAgent 是当前运行时的规范化 UA，nil 表示未完成必须的初始化探测。
	userAgent := m.headlessUserAgent()
	if userAgent == nil {
		return fmt.Errorf("无头 Chromium User-Agent 未初始化")
	}
	// metadata 是可安全传给当前页面的 Client Hints 拷贝，不能含 HeadlessChrome 品牌。
	metadata := normalizeUserAgentMetadata(m.userAgentMetadata)
	if len(metadata) == 0 {
		return fmt.Errorf("无头 Chromium User-Agent Client Hints 未初始化")
	}
	// session 是页面生命周期内保持附着的 CDP 会话；err 表示 Chromium 拒绝创建目标会话。
	session, err := page.Context().NewCDPSession(page)
	if err != nil {
		return fmt.Errorf("创建 Chromium 指纹 CDP 会话: %w", err)
	}
	// err 表示 Chromium 未接受身份覆盖；此时立即分离 session 防止资源泄漏。
	if _, err := session.Send("Emulation.setUserAgentOverride", map[string]any{
		"userAgent":         *userAgent,
		"userAgentMetadata": metadata,
	}); err != nil {
		_ = session.Detach()
		return fmt.Errorf("应用 Chromium 无头指纹: %w", err)
	}
	// session 在 page 生命周期内必须保持附着；目标级仿真会话分离后 Chromium 会还原 navigator.userAgentData。
	return nil
}

// Close 释放所有浏览器与 Playwright；它会等待活动实例退出且不会遗留关闭 goroutine。
func (m *Manager) Close() error {
	// closeCtx、closeCancel 为兼容入口提供有限关闭预算，避免同步 Playwright 释放永久阻塞调用方。
	closeCtx, closeCancel := context.WithTimeout(context.Background(), legacyLifecycleOperationTimeout)
	defer closeCancel()
	return m.CloseContext(closeCtx)
}

// CloseContext 拒绝新调用并等待已有浏览器调用结束后同步释放资源。
// ctx 到期时返回 ctx.Err，管理器保持 closing 状态，调用方可稍后用更长的 Context 重试；
// 实现不通过后台 goroutine 包装 Close，因此超时不会留下无法观察的关闭任务。
func (m *Manager) CloseContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("关闭浏览器需要调用方 Context")
	}
	// err 表示调用方在关闭开始前已经取消等待，管理器保持可重试状态。
	if err := ctx.Err(); err != nil {
		return err
	}
	m.closeMu.Lock()
	defer m.closeMu.Unlock()
	m.ensureLifecycleCond()
	m.lifecycleMu.Lock()
	if m.closed {
		m.lifecycleMu.Unlock()
		return nil
	}
	m.closing = true
	m.lifecycleMu.Unlock()

	// 轮询等待而不是把条件等待放进 goroutine，以便 Context 取消时没有游离任务。
	for {
		m.lifecycleMu.Lock()
		// remaining 表示仍持有浏览器实例或上下文的活动调用数量。
		remaining := m.inFlight
		m.lifecycleMu.Unlock()
		if remaining == 0 {
			break
		}
		// waitTimer 限制单次轮询间隔，避免为 Context 等待启动后台 goroutine。
		waitTimer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !waitTimer.Stop() {
				<-waitTimer.C
			}
			return ctx.Err()
		case <-waitTimer.C:
		}
	}

	m.mu.Lock()
	// entries 用于本次流程后续判断的entries
	entries := make([]*poolEntry, 0, len(m.pool))
	// e 表示当前遍历过程中的e
	for _, e := range m.pool {
		entries = append(entries, e)
	}
	m.pool = make(map[string]*poolEntry)
	m.mu.Unlock()

	// e 表示当前遍历过程中的e
	for _, e := range entries {
		closeEntry(e, m.logger)
	}
	// stopErr 保存 Playwright 进程同步停止时返回的错误。
	var stopErr error
	if m.pw != nil {
		stopErr = m.pw.Stop()
	}
	m.lifecycleMu.Lock()
	m.closed = true
	m.lifecycleMu.Unlock()
	return stopErr
}

// newPage 从池中取（或创建）一个 context，返回新 page + 释放函数。
// 每次请求新建 page，避免并发导航冲突（与 browser_pool.get_browser 一致）。
// newPage 封装new页码业务协调。
func (m *Manager) newPage(ctx context.Context, cookieID, cookieStr string, headless bool) (playwright.Page, func(), error) {
	// err 表示管理器已关闭或调用方已取消，不能继续申请浏览器页。
	if err := m.beginOperation(ctx); err != nil {
		return nil, nil, err
	}
	// operationOnce 保证 page release 重复调用时只结束一次活动登记。
	var operationOnce sync.Once
	// finishOperation 在 page 释放或创建失败时结束生命周期登记。
	finishOperation := func() {
		operationOnce.Do(m.endOperation)
	}
	if // err 用于本次流程后续判断的err
	err := m.init(); err != nil {
		finishOperation()
		return nil, nil, err
	}
	// entry、err 用于本次流程后续判断的entry、err
	entry, err := m.acquireEntry(cookieID, cookieStr, headless)
	if err != nil {
		finishOperation()
		return nil, nil, err
	}
	// page 在无头模式下会在首次导航前收到运行时 UA 与 Client Hints 覆盖。
	page, err := m.newBrowserPage(entry.context, headless)
	if err != nil {
		// context 损坏，丢弃重建一次。
		m.releaseEntry(cookieID, entry)
		m.evict(cookieID)
		entry, err = m.acquireEntry(cookieID, cookieStr, headless)
		if err != nil {
			finishOperation()
			return nil, nil, err
		}
		page, err = m.newBrowserPage(entry.context, headless)
		if err != nil {
			m.releaseEntry(cookieID, entry)
			m.evict(cookieID)
			finishOperation()
			return nil, nil, fmt.Errorf("新建 page 失败: %w", err)
		}
	}
	// release 用于本次流程后续判断的release
	release := func() {
		_ = page.Close()
		m.releaseEntry(cookieID, entry)
		finishOperation()
	}
	return page, release, nil
}

// acquireEntry 封装acquireEntry业务协调。
func (m *Manager) acquireEntry(cookieID, cookieStr string, headless bool) (*poolEntry, error) {
	m.mu.Lock()
	if // e、ok 用于本次流程后续判断的e、ok
	e, ok := m.pool[cookieID]; ok && e.browser != nil && e.browser.IsConnected() {
		m.claimEntryLocked(e)
		m.mu.Unlock()
		return e, nil
	}
	m.mu.Unlock()

	// created、err 用于本次流程后续判断的created、err
	created, err, _ := m.creates.Do(cookieID, func() (any, error) {
		m.mu.Lock()
		if // e、ok 用于本次流程后续判断的e、ok
		e, ok := m.pool[cookieID]; ok && e.browser != nil && e.browser.IsConnected() {
			m.mu.Unlock()
			return e, nil
		}
		m.mu.Unlock()
		// 池满，淘汰最久未用。
		m.evictIfNeeded()
		return m.createEntry(cookieID, cookieStr, headless)
	})
	if err != nil {
		return nil, err
	}
	// entry、ok 用于本次流程后续判断的entry、ok
	entry, ok := created.(*poolEntry)
	if !ok || entry == nil {
		return nil, fmt.Errorf("浏览器池创建返回异常")
	}
	m.mu.Lock()
	if // current 用于本次流程后续判断的current
	current := m.pool[cookieID]; current == entry {
		m.claimEntryLocked(entry)
		m.mu.Unlock()
	} else {
		m.mu.Unlock()
		return nil, fmt.Errorf("浏览器池条目在获取期间已失效")
	}
	return entry, nil
}

// claimEntryLocked 封装claimEntryLocked业务协调。
func (m *Manager) claimEntryLocked(entry *poolEntry) {
	entry.lastUsed = time.Now()
	if entry.initialLeaseAvailable {
		entry.initialLeaseAvailable = false
		return
	}
	entry.active++
}

// releaseEntry 封装releaseEntry业务协调。
func (m *Manager) releaseEntry(cookieID string, entry *poolEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if // current 用于本次流程后续判断的current
	current := m.pool[cookieID]; current != entry {
		return
	}
	if entry.active > 0 {
		entry.active--
	}
	entry.lastUsed = time.Now()
}

// createEntry 封装createEntry业务协调。
func (m *Manager) createEntry(cookieID, cookieStr string, headless bool) (*poolEntry, error) {
	// browser、err 用于本次流程后续判断的browser、err
	browser, err := m.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless:       playwright.Bool(headless),
		Args:           chromiumLaunchArgs(),
		ExecutablePath: chromiumExecutablePath(),
	})
	if err != nil {
		return nil, fmt.Errorf("启动 chromium 失败: %w", err)
	}
	// contextOptions 保留 Chromium 原生上下文配置；无头 UA 仅使用本次实测 runtime 的规范化版本。
	contextOptions := playwright.BrowserNewContextOptions{
		Viewport:   &playwright.Size{Width: defaultW, Height: defaultH},
		Locale:     playwright.String(defaultLang),
		TimezoneId: playwright.String(defaultTZ),
	}
	if headless {
		contextOptions.UserAgent = m.headlessUserAgent()
	}
	// context 是 browser 的新隔离上下文；err 表示 context 创建失败并触发已启动浏览器回收。
	context, err := browser.NewContext(contextOptions)
	if err != nil {
		_ = browser.Close()
		return nil, fmt.Errorf("创建 context 失败: %w", err)
	}
	if // err 用于本次流程后续判断的err
	err := context.AddInitScript(playwright.Script{Content: playwright.String(stealthScript())}); err != nil {
		m.logger.Warn("注入 stealth 脚本失败", "err", err)
	}
	if cookieStr != "" {
		if // err 用于本次流程后续判断的err
		err := addCookieStr(context, cookieStr); err != nil {
			_ = context.Close()
			_ = browser.Close()
			return nil, fmt.Errorf("注入 cookie 失败: %w", err)
		}
	}
	// entry 用于本次流程后续判断的entry
	entry := &poolEntry{
		cookieID:              cookieID,
		browser:               browser,
		context:               context,
		lastUsed:              time.Now(),
		active:                1,
		initialLeaseAvailable: true,
	}
	m.mu.Lock()
	m.pool[cookieID] = entry
	m.mu.Unlock()
	return entry, nil
}

// touch 封装touch业务协调。
func (m *Manager) touch(cookieID string) {
	m.mu.Lock()
	if // e、ok 用于本次流程后续判断的e、ok
	e, ok := m.pool[cookieID]; ok {
		e.lastUsed = time.Now()
	}
	m.mu.Unlock()
}

// evict 封装evict业务协调。
func (m *Manager) evict(cookieID string) {
	m.mu.Lock()
	// e、ok 用于本次流程后续判断的e、ok
	e, ok := m.pool[cookieID]
	if ok && e.active > 0 {
		m.mu.Unlock()
		return
	}
	delete(m.pool, cookieID)
	m.mu.Unlock()
	if ok {
		closeEntry(e, m.logger)
	}
}

// evictIfNeeded 封装evictIfNeeded业务协调。
func (m *Manager) evictIfNeeded() {
	m.mu.Lock()
	if len(m.pool) < m.maxSize {
		m.mu.Unlock()
		return
	}
	// oldest 用于本次流程后续判断的oldest
	var oldest *poolEntry
	// oldestID 用于本次流程后续判断的oldestID
	var oldestID string
	// id、e 表示当前遍历过程中的id、e
	for id, e := range m.pool {
		if e.active > 0 {
			continue
		}
		if oldest == nil || e.lastUsed.Before(oldest.lastUsed) {
			oldest = e
			oldestID = id
		}
	}
	if oldest != nil {
		delete(m.pool, oldestID)
	}
	m.mu.Unlock()
	if oldest != nil {
		closeEntry(oldest, m.logger)
	}
}

// CleanupIdle 清理超过 idleTTL 未用的浏览器。
func (m *Manager) CleanupIdle() {
	// now 用于本次流程后续判断的now
	now := time.Now()
	m.mu.Lock()
	// toClose 用于本次流程后续判断的toClose
	var toClose []*poolEntry
	// id、e 表示当前遍历过程中的id、e
	for id, e := range m.pool {
		if e.active == 0 && now.Sub(e.lastUsed) > m.idleTTL {
			toClose = append(toClose, e)
			delete(m.pool, id)
		}
	}
	m.mu.Unlock()
	// e 表示当前遍历过程中的e
	for _, e := range toClose {
		closeEntry(e, m.logger)
	}
}

// closeEntry 封装closeEntry业务协调。
func closeEntry(e *poolEntry, logger *slog.Logger) {
	if e == nil {
		return
	}
	if e.context != nil {
		_ = e.context.Close()
	}
	if e.browser != nil {
		_ = e.browser.Close()
	}
}

// addCookieStr 把 "k=v; k2=v2" 注入 context（domain .goofish.com）。
func addCookieStr(ctx playwright.BrowserContext, cookieStr string) error {
	// cookies 用于本次流程后续判断的cookies
	cookies := parseCookieStrToPlaywright(cookieStr)
	if len(cookies) == 0 {
		return errors.New("cookie 为空或格式错误")
	}
	if // err 用于本次流程后续判断的err
	err := ctx.ClearCookies(); err != nil {
		return fmt.Errorf("清理浏览器旧 cookie: %w", err)
	}
	return ctx.AddCookies(cookies)
}
