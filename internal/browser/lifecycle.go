// Package browser 用 playwright-go 在进程内驱动 Chromium，替换原 Python sidecar。
// 首次使用时自动下载 Chromium（playwright.Install），无需手动启动任何外部服务。
//
// 移植自原项目：
//   - utils/browser_pool.py           → Manager 上下文池
//   - utils/xianyu_slider_stealth.py  → stealth.go + slider.go
//   - utils/item_search.py            → search.go
//   - utils/order_fetcher_optimized.py → orders.go
//   - XianyuAutoAsync.refresh_cookies_from_qr_login → qrrefresh.go
package browser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"

	"xianyu-go/internal/xianyu"
)

// 默认 UA / 视口，与 Python 端一致。
const (
	defaultUA    = xianyu.BrowserUA
	defaultW     = 1920
	defaultH     = 1080
	defaultLang  = "zh-CN"
	defaultTZ    = "Asia/Shanghai"
	goofishDot   = ".goofish.com"
	goofishIMURL = "https://www.goofish.com/im"
)

// chromiumLaunchArgs 统一 Chromium 启动参数（取自 browser_pool._create_browser）。
func chromiumLaunchArgs() []string {
	return []string{
		"--no-sandbox",
		"--disable-setuid-sandbox",
		"--disable-dev-shm-usage",
		"--disable-gpu",
		"--disable-extensions",
		"--disable-blink-features=AutomationControlled",
		"--disable-background-timer-throttling",
		"--disable-backgrounding-occluded-windows",
		"--disable-renderer-backgrounding",
		"--disable-features=TranslateUI",
		"--disable-ipc-flooding-protection",
		"--disable-default-apps",
		"--disable-sync",
		"--disable-translate",
		"--hide-scrollbars",
		"--mute-audio",
		"--no-default-browser-check",
		"--no-pings",
		"--lang=zh-CN",
	}
}

// Manager 管理浏览器生命周期与按账号复用的上下文池。
type Manager struct {
	pw     *playwright.Playwright
	logger *slog.Logger

	once      sync.Once
	initErr   error
	installed bool

	mu      sync.Mutex
	pool    map[string]*poolEntry
	maxSize int
	idleTTL time.Duration

	// 允许测试注入自定义 playwright / 安装函数。
	installFn func() error
	runFn     func() (*playwright.Playwright, error)
}

type poolEntry struct {
	cookieID  string
	browser   playwright.Browser
	context   playwright.BrowserContext
	lastUsed  time.Time
	userData  string // 持久化目录（密码登录用），空表示非持久化
	persistent bool
}

// NewManager 构造。logger 为 nil 用默认。
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		logger:  logger,
		pool:    make(map[string]*poolEntry),
		maxSize: 3,
		idleTTL: 5 * time.Minute,
		installFn: func() error {
			return playwright.Install(&playwright.RunOptions{
				Browsers: []string{"chromium"},
				Verbose:  false,
			})
		},
		runFn: func() (*playwright.Playwright, error) {
			return playwright.Run()
		},
	}
}

// init 懒加载 playwright（首次调用时下载 driver + chromium）。
func (m *Manager) init() error {
	m.once.Do(func() {
		if err := m.installFn(); err != nil {
			m.initErr = fmt.Errorf("安装 playwright/chromium 失败（缺系统依赖时需手动执行 playwright install --with-deps）: %w", err)
			return
		}
		pw, err := m.runFn()
		if err != nil {
			m.initErr = fmt.Errorf("启动 playwright 失败: %w", err)
			return
		}
		m.pw = pw
		m.installed = true
		m.logger.Info("playwright chromium 就绪")
	})
	return m.initErr
}

// Close 释放所有浏览器与 playwright。
func (m *Manager) Close() error {
	m.mu.Lock()
	entries := make([]*poolEntry, 0, len(m.pool))
	for _, e := range m.pool {
		entries = append(entries, e)
	}
	m.pool = make(map[string]*poolEntry)
	m.mu.Unlock()

	for _, e := range entries {
		closeEntry(e, m.logger)
	}
	if m.pw != nil {
		return m.pw.Stop()
	}
	return nil
}

// ping 暴露给 health 检查：playwright 是否就绪。
func (m *Manager) ping(ctx context.Context) error {
	if err := m.init(); err != nil {
		return err
	}
	return nil
}

// newPage 从池中取（或创建）一个 context，返回新 page + 释放函数。
// 每次请求新建 page，避免并发导航冲突（与 browser_pool.get_browser 一致）。
func (m *Manager) newPage(ctx context.Context, cookieID, cookieStr string, headless bool) (playwright.Page, func(), error) {
	if err := m.init(); err != nil {
		return nil, nil, err
	}
	entry, err := m.acquireEntry(cookieID, cookieStr, headless)
	if err != nil {
		return nil, nil, err
	}
	page, err := entry.context.NewPage()
	if err != nil {
		// context 损坏，丢弃重建一次。
		m.evict(cookieID)
		entry, err = m.acquireEntry(cookieID, cookieStr, headless)
		if err != nil {
			return nil, nil, err
		}
		page, err = entry.context.NewPage()
		if err != nil {
			return nil, nil, fmt.Errorf("新建 page 失败: %w", err)
		}
	}
	release := func() {
		_ = page.Close()
		m.touch(cookieID)
	}
	return page, release, nil
}

func (m *Manager) acquireEntry(cookieID, cookieStr string, headless bool) (*poolEntry, error) {
	m.mu.Lock()
	if e, ok := m.pool[cookieID]; ok && e.browser != nil && e.browser.IsConnected() {
		e.lastUsed = time.Now()
		m.mu.Unlock()
		return e, nil
	}
	m.mu.Unlock()

	// 池满，淘汰最久未用。
	m.evictIfNeeded()
	return m.createEntry(cookieID, cookieStr, headless)
}

func (m *Manager) createEntry(cookieID, cookieStr string, headless bool) (*poolEntry, error) {
	browser, err := m.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(headless),
		Args:     chromiumLaunchArgs(),
	})
	if err != nil {
		return nil, fmt.Errorf("启动 chromium 失败: %w", err)
	}
	context, err := browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent:  playwright.String(defaultUA),
		Viewport:  &playwright.Size{Width: defaultW, Height: defaultH},
		Locale:    playwright.String(defaultLang),
		TimezoneId: playwright.String(defaultTZ),
	})
	if err != nil {
		_ = browser.Close()
		return nil, fmt.Errorf("创建 context 失败: %w", err)
	}
	if err := context.AddInitScript(playwright.Script{Content: playwright.String(stealthScript())}); err != nil {
		m.logger.Warn("注入 stealth 脚本失败", "err", err)
	}
	if cookieStr != "" {
		if err := addCookieStr(context, cookieStr); err != nil {
			m.logger.Warn("注入 cookie 失败", "err", err)
		}
	}
	entry := &poolEntry{
		cookieID: cookieID,
		browser:  browser,
		context:  context,
		lastUsed: time.Now(),
	}
	m.mu.Lock()
	m.pool[cookieID] = entry
	m.mu.Unlock()
	return entry, nil
}

func (m *Manager) touch(cookieID string) {
	m.mu.Lock()
	if e, ok := m.pool[cookieID]; ok {
		e.lastUsed = time.Now()
	}
	m.mu.Unlock()
}

func (m *Manager) evict(cookieID string) {
	m.mu.Lock()
	e, ok := m.pool[cookieID]
	delete(m.pool, cookieID)
	m.mu.Unlock()
	if ok {
		closeEntry(e, m.logger)
	}
}

func (m *Manager) evictIfNeeded() {
	m.mu.Lock()
	if len(m.pool) < m.maxSize {
		m.mu.Unlock()
		return
	}
	var oldest *poolEntry
	var oldestID string
	for id, e := range m.pool {
		if oldest == nil || e.lastUsed.Before(oldest.lastUsed) {
			oldest = e
			oldestID = id
		}
	}
	delete(m.pool, oldestID)
	m.mu.Unlock()
	if oldest != nil {
		closeEntry(oldest, m.logger)
	}
}

// CleanupIdle 清理超过 idleTTL 未用的浏览器。
func (m *Manager) CleanupIdle() {
	now := time.Now()
	m.mu.Lock()
	var toClose []*poolEntry
	for id, e := range m.pool {
		if now.Sub(e.lastUsed) > m.idleTTL {
			toClose = append(toClose, e)
			delete(m.pool, id)
		}
	}
	m.mu.Unlock()
	for _, e := range toClose {
		closeEntry(e, m.logger)
	}
}

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
	cookies := parseCookieStrToPlaywright(cookieStr)
	if len(cookies) == 0 {
		return errors.New("cookie 为空或格式错误")
	}
	return ctx.AddCookies(cookies)
}
