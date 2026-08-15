// Package browser 用 playwright-go 在进程内驱动 Chromium。
// 安装包提供预置 runtime；开发环境没有预置 runtime 时才自动下载。
package browser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"
	"golang.org/x/sync/singleflight"

	"xianyu-go/internal/xianyu"
)

// 默认 UA、语言、时区与视口。
// defaultW 保存defaultW，供当前处理流程使用
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

// chromiumExecutablePath 负责chromiumExecutable路径相关处理。
func chromiumExecutablePath() *string {
	if // path 保存路径，供当前处理流程使用
	path := strings.TrimSpace(os.Getenv("PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH")); path != "" {
		return playwright.String(path)
	}
	return nil
}

// skipPlaywrightBrowserDownload 负责skipPlaywright浏览器Download相关处理。
func skipPlaywrightBrowserDownload() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// packagedPlaywrightRuntimeReady 负责packagedPlaywrightRuntimeReady相关处理。
func packagedPlaywrightRuntimeReady() bool {
	// driverDir 保存driverDir，供当前处理流程使用
	driverDir := strings.TrimSpace(os.Getenv("PLAYWRIGHT_DRIVER_PATH"))
	if driverDir == "" {
		return false
	}
	// nodeReady 保存nodeReady，供当前处理流程使用
	nodeReady := false
	if // nodePath 保存node路径，供当前处理流程使用
	nodePath := strings.TrimSpace(os.Getenv("PLAYWRIGHT_NODEJS_PATH")); nodePath != "" {
		// err 保存err，供当前处理流程使用
		_, err := os.Stat(nodePath)
		nodeReady = err == nil
	} else {
		// nodeName 保存node名称，供当前处理流程使用
		nodeName := "node"
		if runtime.GOOS == "windows" {
			nodeName = "node.exe"
		}
		// err 保存err，供当前处理流程使用
		_, err := os.Stat(filepath.Join(driverDir, nodeName))
		nodeReady = err == nil
	}
	if !nodeReady {
		return false
	}
	if // err 保存err，供当前处理流程使用
	_, err := os.Stat(filepath.Join(driverDir, "package", "cli.js")); err != nil {
		return false
	}
	if strings.TrimSpace(os.Getenv("PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH")) != "" {
		return true
	}
	// browserDir 保存浏览器Dir，供当前处理流程使用
	browserDir := strings.TrimSpace(os.Getenv("PLAYWRIGHT_BROWSERS_PATH"))
	if browserDir == "" {
		return false
	}
	// matches、err 保存matches、err，供当前处理流程使用
	matches, err := filepath.Glob(filepath.Join(browserDir, "chromium-*"))
	if err != nil {
		return false
	}
	// match 表示当前遍历过程中的match
	for _, match := range matches {
		if // info、statErr 保存info、statErr，供当前处理流程使用
		info, statErr := os.Stat(match); statErr == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// ErrManagerClosed 表示浏览器管理器已经进入关闭流程，不能再创建新的浏览器实例。
var ErrManagerClosed = errors.New("浏览器管理器已关闭")

// Manager 管理浏览器生命周期与按账号复用的上下文池。
type Manager struct {
	// lifecycleMu 保护关闭状态和活动浏览器调用计数；关闭时不持有它执行 Playwright I/O。
	lifecycleMu sync.Mutex
	// lifecycleCond 在活动调用归零时唤醒等待关闭的调用方。
	lifecycleCond *sync.Cond
	// closing 表示管理器已经拒绝新的浏览器调用但仍在等待已有调用退出。
	closing bool
	// closed 表示所有池实例和 Playwright 进程均已同步释放。
	closed bool
	// inFlight 统计从浏览器实例创建到对应 release 执行完毕的活动调用。
	inFlight int
	// closeMu 串行化多个 CloseContext 调用，避免重复停止同一个 Playwright 进程。
	closeMu sync.Mutex

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

// poolEntry 保存poolEntry，供当前处理流程使用
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
	// manager 保存已配置生命周期条件变量和浏览器池的管理器实例。
	manager := &Manager{
		logger:     logger,
		pool:       make(map[string]*poolEntry),
		maxSize:    3,
		idleTTL:    5 * time.Minute,
		renewLocks: make(map[string]*sync.Mutex),
		renewSlots: make(chan struct{}, 3),
		installFn: func() error {
			if packagedPlaywrightRuntimeReady() {
				return nil
			}
			// opts 保存opts，供当前处理流程使用
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
	manager.lifecycleCond = sync.NewCond(&manager.lifecycleMu)
	return manager
}

// beginOperation 登记一个可能持有 Chromium 实例的调用；关闭开始后拒绝新调用。
// ctx 仅用于在进入状态机前传播调用方取消语义，不会启动无法回收的等待 goroutine。
func (m *Manager) beginOperation(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// err 表示调用方 Context 已取消，管理器不会为已取消调用登记活动任务。
	if err := ctx.Err(); err != nil {
		return err
	}
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.closing || m.closed {
		return ErrManagerClosed
	}
	m.inFlight++
	return nil
}

// endOperation 释放活动调用登记，并唤醒等待 Manager 关闭的调用方。
func (m *Manager) endOperation() {
	m.lifecycleMu.Lock()
	if m.inFlight > 0 {
		m.inFlight--
	}
	if m.lifecycleCond != nil && m.inFlight == 0 {
		m.lifecycleCond.Broadcast()
	}
	m.lifecycleMu.Unlock()
}

// ensureLifecycleCond 为测试构造的零值 Manager 补齐关闭等待条件变量。
func (m *Manager) ensureLifecycleCond() {
	m.lifecycleMu.Lock()
	if m.lifecycleCond == nil {
		m.lifecycleCond = sync.NewCond(&m.lifecycleMu)
	}
	m.lifecycleMu.Unlock()
}

// accountRenewLock 负责账号Renew锁相关处理。
func (m *Manager) accountRenewLock(cookieID string) *sync.Mutex {
	m.renewMu.Lock()
	defer m.renewMu.Unlock()
	if m.renewLocks == nil {
		m.renewLocks = make(map[string]*sync.Mutex)
	}
	// lock 保存锁，供当前处理流程使用
	lock := m.renewLocks[cookieID]
	if lock == nil {
		lock = &sync.Mutex{}
		m.renewLocks[cookieID] = lock
	}
	return lock
}

// acquireRenewSlot 负责acquireRenewSlot相关处理。
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
		if // err 保存err，供当前处理流程使用
		err := m.installFn(); err != nil {
			m.initErr = fmt.Errorf("安装 playwright/chromium 失败（缺系统依赖时需手动执行 playwright install --with-deps）: %w", err)
			return
		}
		// pw、err 保存pw、err，供当前处理流程使用
		pw, err := m.runFn()
		if err != nil {
			m.initErr = fmt.Errorf("启动 playwright 失败: %w", err)
			return
		}
		m.pw = pw
		if // err 保存err，供当前处理流程使用
		err := m.detectBrowserFingerprint(); err != nil {
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
// Initialize 负责Initialize相关处理。
func (m *Manager) Initialize() error {
	// err 表示管理器已进入关闭流程，不能继续初始化 Playwright。
	if err := m.beginOperation(context.Background()); err != nil {
		return err
	}
	defer m.endOperation()
	return m.init()
}

// detectBrowserFingerprint 负责detect浏览器Fingerprint相关处理。
func (m *Manager) detectBrowserFingerprint() error {
	// observed 保存observed，供当前处理流程使用
	observed := make(chan http.Header, 1)
	// server 保存server，供当前处理流程使用
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- r.Header.Clone()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><title>fingerprint</title>"))
	}))
	defer server.Close()

	// b、err 保存b、err，供当前处理流程使用
	b, err := m.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless:       playwright.Bool(true),
		Args:           chromiumLaunchArgs(),
		ExecutablePath: chromiumExecutablePath(),
	})
	if err != nil {
		return err
	}
	defer func() { _ = b.Close() }()
	// ctx、err 保存ctx、err，供当前处理流程使用
	ctx, err := b.NewContext()
	if err != nil {
		return err
	}
	defer func() { _ = ctx.Close() }()
	// page、err 保存page、err，供当前处理流程使用
	page, err := ctx.NewPage()
	if err != nil {
		return err
	}
	if // err 保存err，供当前处理流程使用
	_, err := page.Goto(server.URL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); err != nil {
		return err
	}
	// headers 保存headers，供当前处理流程使用
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

// Close 释放所有浏览器与 Playwright；它会等待活动实例退出且不会遗留关闭 goroutine。
func (m *Manager) Close() error {
	return m.CloseContext(context.Background())
}

// CloseContext 拒绝新调用并等待已有浏览器调用结束后同步释放资源。
// ctx 到期时返回 ctx.Err，管理器保持 closing 状态，调用方可稍后用更长的 Context 重试；
// 实现不通过后台 goroutine 包装 Close，因此超时不会留下无法观察的关闭任务。
func (m *Manager) CloseContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
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
	// entries 保存entries，供当前处理流程使用
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
// newPage 负责new页码相关处理。
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
	if // err 保存err，供当前处理流程使用
	err := m.init(); err != nil {
		finishOperation()
		return nil, nil, err
	}
	// entry、err 保存entry、err，供当前处理流程使用
	entry, err := m.acquireEntry(cookieID, cookieStr, headless)
	if err != nil {
		finishOperation()
		return nil, nil, err
	}
	// page、err 保存page、err，供当前处理流程使用
	page, err := entry.context.NewPage()
	if err != nil {
		// context 损坏，丢弃重建一次。
		m.releaseEntry(cookieID, entry)
		m.evict(cookieID)
		entry, err = m.acquireEntry(cookieID, cookieStr, headless)
		if err != nil {
			finishOperation()
			return nil, nil, err
		}
		page, err = entry.context.NewPage()
		if err != nil {
			m.releaseEntry(cookieID, entry)
			m.evict(cookieID)
			finishOperation()
			return nil, nil, fmt.Errorf("新建 page 失败: %w", err)
		}
	}
	// release 保存release，供当前处理流程使用
	release := func() {
		_ = page.Close()
		m.releaseEntry(cookieID, entry)
		finishOperation()
	}
	return page, release, nil
}

// acquireEntry 负责acquireEntry相关处理。
func (m *Manager) acquireEntry(cookieID, cookieStr string, headless bool) (*poolEntry, error) {
	m.mu.Lock()
	if // e、ok 保存e、ok，供当前处理流程使用
	e, ok := m.pool[cookieID]; ok && e.browser != nil && e.browser.IsConnected() {
		m.claimEntryLocked(e)
		m.mu.Unlock()
		return e, nil
	}
	m.mu.Unlock()

	// created、err 保存created、err，供当前处理流程使用
	created, err, _ := m.creates.Do(cookieID, func() (any, error) {
		m.mu.Lock()
		if // e、ok 保存e、ok，供当前处理流程使用
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
	// entry、ok 保存entry、ok，供当前处理流程使用
	entry, ok := created.(*poolEntry)
	if !ok || entry == nil {
		return nil, fmt.Errorf("浏览器池创建返回异常")
	}
	m.mu.Lock()
	if // current 保存current，供当前处理流程使用
	current := m.pool[cookieID]; current == entry {
		m.claimEntryLocked(entry)
		m.mu.Unlock()
	} else {
		m.mu.Unlock()
		return nil, fmt.Errorf("浏览器池条目在获取期间已失效")
	}
	return entry, nil
}

// claimEntryLocked 负责claimEntryLocked相关处理。
func (m *Manager) claimEntryLocked(entry *poolEntry) {
	entry.lastUsed = time.Now()
	if entry.initialLeaseAvailable {
		entry.initialLeaseAvailable = false
		return
	}
	entry.active++
}

// releaseEntry 负责releaseEntry相关处理。
func (m *Manager) releaseEntry(cookieID string, entry *poolEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if // current 保存current，供当前处理流程使用
	current := m.pool[cookieID]; current != entry {
		return
	}
	if entry.active > 0 {
		entry.active--
	}
	entry.lastUsed = time.Now()
}

// createEntry 负责createEntry相关处理。
func (m *Manager) createEntry(cookieID, cookieStr string, headless bool) (*poolEntry, error) {
	// browser、err 保存browser、err，供当前处理流程使用
	browser, err := m.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless:       playwright.Bool(headless),
		Args:           chromiumLaunchArgs(),
		ExecutablePath: chromiumExecutablePath(),
	})
	if err != nil {
		return nil, fmt.Errorf("启动 chromium 失败: %w", err)
	}
	// context、err 保存context、err，供当前处理流程使用
	context, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport:   &playwright.Size{Width: defaultW, Height: defaultH},
		Locale:     playwright.String(defaultLang),
		TimezoneId: playwright.String(defaultTZ),
	})
	if err != nil {
		_ = browser.Close()
		return nil, fmt.Errorf("创建 context 失败: %w", err)
	}
	if // err 保存err，供当前处理流程使用
	err := context.AddInitScript(playwright.Script{Content: playwright.String(stealthScript())}); err != nil {
		m.logger.Warn("注入 stealth 脚本失败", "err", err)
	}
	if cookieStr != "" {
		if // err 保存err，供当前处理流程使用
		err := addCookieStr(context, cookieStr); err != nil {
			_ = context.Close()
			_ = browser.Close()
			return nil, fmt.Errorf("注入 cookie 失败: %w", err)
		}
	}
	// entry 保存entry，供当前处理流程使用
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

// touch 负责touch相关处理。
func (m *Manager) touch(cookieID string) {
	m.mu.Lock()
	if // e、ok 保存e、ok，供当前处理流程使用
	e, ok := m.pool[cookieID]; ok {
		e.lastUsed = time.Now()
	}
	m.mu.Unlock()
}

// evict 负责evict相关处理。
func (m *Manager) evict(cookieID string) {
	m.mu.Lock()
	// e、ok 保存e、ok，供当前处理流程使用
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

// evictIfNeeded 负责evictIfNeeded相关处理。
func (m *Manager) evictIfNeeded() {
	m.mu.Lock()
	if len(m.pool) < m.maxSize {
		m.mu.Unlock()
		return
	}
	// oldest 保存oldest，供当前处理流程使用
	var oldest *poolEntry
	// oldestID 保存oldestID，供当前处理流程使用
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
	// now 保存now，供当前处理流程使用
	now := time.Now()
	m.mu.Lock()
	// toClose 保存toClose，供当前处理流程使用
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

// closeEntry 负责closeEntry相关处理。
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
	// cookies 保存cookies，供当前处理流程使用
	cookies := parseCookieStrToPlaywright(cookieStr)
	if len(cookies) == 0 {
		return errors.New("cookie 为空或格式错误")
	}
	if // err 保存err，供当前处理流程使用
	err := ctx.ClearCookies(); err != nil {
		return fmt.Errorf("清理浏览器旧 cookie: %w", err)
	}
	return ctx.AddCookies(cookies)
}
