// Package browser 用 playwright-go 在进程内驱动 Chromium。
// 首次使用时自动下载 Chromium（playwright.Install），无需手动启动外部服务。
package browser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
	"golang.org/x/sync/singleflight"

	"xianyu-go/internal/xianyu"
)

// 默认 UA、语言、时区与视口。
const (
	defaultW       = 1920
	defaultH       = 1080
	defaultLang    = "zh-CN"
	defaultTZ      = "Asia/Shanghai"
	goofishDot     = ".goofish.com"
	goofishHomeURL = "https://www.goofish.com/"
	goofishIMURL   = "https://www.goofish.com/im"
)

// chromiumLaunchArgs 统一 Chromium 启动参数。
func chromiumLaunchArgs() []string {
	return []string{
		"--no-sandbox",
		"--disable-setuid-sandbox",
		"--disable-dev-shm-usage",
		"--disable-blink-features=AutomationControlled",
		"--lang=zh-CN",
	}
}

func chromiumExecutablePath() *string {
	if path := strings.TrimSpace(os.Getenv("PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH")); path != "" {
		return playwright.String(path)
	}
	return nil
}

func skipPlaywrightBrowserDownload() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
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
	creates singleflight.Group

	renewMu    sync.Mutex
	renewLocks map[string]*sync.Mutex
	renewSlots chan struct{}

	// 允许测试注入自定义 playwright / 安装函数。
	installFn func() error
	runFn     func() (*playwright.Playwright, error)

	// 仅用于隔离 token 风控引擎编排测试；生产环境为 nil，调用真实实现。
	tokenCaptchaPrimaryFn  tokenCaptchaEngineFunc
	tokenCaptchaFallbackFn tokenCaptchaEngineFunc
}

type poolEntry struct {
	cookieID              string
	browser               playwright.Browser
	context               playwright.BrowserContext
	lastUsed              time.Time
	active                int
	initialLeaseAvailable bool
}

// NewManager 构造。logger 为 nil 用默认。
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		logger:     logger,
		pool:       make(map[string]*poolEntry),
		maxSize:    3,
		idleTTL:    5 * time.Minute,
		renewLocks: make(map[string]*sync.Mutex),
		renewSlots: make(chan struct{}, 3),
		installFn: func() error {
			opts := &playwright.RunOptions{
				Browsers: []string{"chromium"},
				Verbose:  false,
			}
			if skipPlaywrightBrowserDownload() {
				opts.SkipInstallBrowsers = true
			}
			return playwright.Install(opts)
		},
		runFn: func() (*playwright.Playwright, error) {
			return playwright.Run()
		},
	}
}

func (m *Manager) accountRenewLock(cookieID string) *sync.Mutex {
	m.renewMu.Lock()
	defer m.renewMu.Unlock()
	if m.renewLocks == nil {
		m.renewLocks = make(map[string]*sync.Mutex)
	}
	lock := m.renewLocks[cookieID]
	if lock == nil {
		lock = &sync.Mutex{}
		m.renewLocks[cookieID] = lock
	}
	return lock
}

func (m *Manager) acquireRenewSlot(ctx context.Context) (func(), error) {
	if m.renewSlots == nil {
		m.renewSlots = make(chan struct{}, 3)
	}
	select {
	case m.renewSlots <- struct{}{}:
		return func() { <-m.renewSlots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
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
		if err := m.detectBrowserFingerprint(); err != nil {
			m.initErr = fmt.Errorf("读取 Playwright Chromium 原生指纹失败: %w", err)
			_ = pw.Stop()
			m.pw = nil
			return
		}
		m.installed = true
		m.logger.Info("playwright chromium 就绪")
	})
	return m.initErr
}

// Initialize starts Playwright and publishes the bundled Chromium's native
// browser identity before any non-browser client sends requests.
func (m *Manager) Initialize() error { return m.init() }

func (m *Manager) detectBrowserFingerprint() error {
	observed := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- r.Header.Clone()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><title>fingerprint</title>"))
	}))
	defer server.Close()

	b, err := m.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless:       playwright.Bool(true),
		Args:           chromiumLaunchArgs(),
		ExecutablePath: chromiumExecutablePath(),
	})
	if err != nil {
		return err
	}
	defer func() { _ = b.Close() }()
	ctx, err := b.NewContext()
	if err != nil {
		return err
	}
	defer func() { _ = ctx.Close() }()
	page, err := ctx.NewPage()
	if err != nil {
		return err
	}
	if _, err := page.Goto(server.URL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
		return err
	}
	var headers http.Header
	select {
	case headers = <-observed:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("等待 Chromium 指纹请求超时")
	}
	if strings.TrimSpace(headers.Get("User-Agent")) == "" {
		return fmt.Errorf("Chromium 返回空 userAgent")
	}
	xianyu.SetBrowserFingerprint(xianyu.BrowserFingerprint{
		UserAgent: headers.Get("User-Agent"),
		SecChUA:   headers.Get("sec-ch-ua"),
		Platform:  strings.Trim(headers.Get("sec-ch-ua-platform"), `"`),
		Mobile:    headers.Get("sec-ch-ua-mobile"),
	})
	m.logger.Info("已读取 Playwright Chromium 原生指纹", "browser_version", b.Version(), "user_agent", headers.Get("User-Agent"))
	return nil
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
		m.releaseEntry(cookieID, entry)
		m.evict(cookieID)
		entry, err = m.acquireEntry(cookieID, cookieStr, headless)
		if err != nil {
			return nil, nil, err
		}
		page, err = entry.context.NewPage()
		if err != nil {
			m.releaseEntry(cookieID, entry)
			m.evict(cookieID)
			return nil, nil, fmt.Errorf("新建 page 失败: %w", err)
		}
	}
	release := func() {
		_ = page.Close()
		m.releaseEntry(cookieID, entry)
	}
	return page, release, nil
}

func (m *Manager) acquireEntry(cookieID, cookieStr string, headless bool) (*poolEntry, error) {
	m.mu.Lock()
	if e, ok := m.pool[cookieID]; ok && e.browser != nil && e.browser.IsConnected() {
		m.claimEntryLocked(e)
		m.mu.Unlock()
		return e, nil
	}
	m.mu.Unlock()

	created, err, _ := m.creates.Do(cookieID, func() (any, error) {
		m.mu.Lock()
		if e, ok := m.pool[cookieID]; ok && e.browser != nil && e.browser.IsConnected() {
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
	entry, ok := created.(*poolEntry)
	if !ok || entry == nil {
		return nil, fmt.Errorf("浏览器池创建返回异常")
	}
	m.mu.Lock()
	if current := m.pool[cookieID]; current == entry {
		m.claimEntryLocked(entry)
		m.mu.Unlock()
	} else {
		m.mu.Unlock()
		return nil, fmt.Errorf("浏览器池条目在获取期间已失效")
	}
	return entry, nil
}

func (m *Manager) claimEntryLocked(entry *poolEntry) {
	entry.lastUsed = time.Now()
	if entry.initialLeaseAvailable {
		entry.initialLeaseAvailable = false
		return
	}
	entry.active++
}

func (m *Manager) releaseEntry(cookieID string, entry *poolEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current := m.pool[cookieID]; current != entry {
		return
	}
	if entry.active > 0 {
		entry.active--
	}
	entry.lastUsed = time.Now()
}

func (m *Manager) createEntry(cookieID, cookieStr string, headless bool) (*poolEntry, error) {
	browser, err := m.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless:       playwright.Bool(headless),
		Args:           chromiumLaunchArgs(),
		ExecutablePath: chromiumExecutablePath(),
	})
	if err != nil {
		return nil, fmt.Errorf("启动 chromium 失败: %w", err)
	}
	context, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport:   &playwright.Size{Width: defaultW, Height: defaultH},
		Locale:     playwright.String(defaultLang),
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
			_ = context.Close()
			_ = browser.Close()
			return nil, fmt.Errorf("注入 cookie 失败: %w", err)
		}
	}
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

func (m *Manager) evictIfNeeded() {
	m.mu.Lock()
	if len(m.pool) < m.maxSize {
		m.mu.Unlock()
		return
	}
	var oldest *poolEntry
	var oldestID string
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
	now := time.Now()
	m.mu.Lock()
	var toClose []*poolEntry
	for id, e := range m.pool {
		if e.active == 0 && now.Sub(e.lastUsed) > m.idleTTL {
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
	if err := ctx.ClearCookies(); err != nil {
		return fmt.Errorf("清理浏览器旧 cookie: %w", err)
	}
	return ctx.AddCookies(cookies)
}
