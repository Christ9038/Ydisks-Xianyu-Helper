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
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"
	"golang.org/x/sync/singleflight"

	"xianyu-go/internal/xianyu"
)

// 默认 UA、语言、时区与视口。
// defaultW 用于本次流程后续判断的defaultW
const (
	defaultW       = 1920
	defaultH       = 1080
	defaultLang    = "zh-CN"
	defaultTZ      = "Asia/Shanghai"
	goofishDot     = ".goofish.com"
	goofishHomeURL = "https://www.goofish.com/"
	goofishIMURL   = "https://www.goofish.com/im"
	// legacyLifecycleOperationTimeout 为旧的无 Context 浏览器入口提供有界初始化与关闭预算。
	legacyLifecycleOperationTimeout = 45 * time.Second
)

// chromiumLaunchArgs 统一 Chromium 启动参数。
func chromiumLaunchArgs() []string {
	// args 是所有 Chromium 启动路径共享的参数；证书绕过仅在环境明确授权时追加。
	args := []string{
		"--no-sandbox",
		"--disable-setuid-sandbox",
		"--disable-dev-shm-usage",
		"--disable-blink-features=AutomationControlled",
		"--lang=zh-CN",
	}
	if captchaIgnoreCertificateErrors() {
		args = append(args, "--ignore-certificate-errors")
	}
	// proxy 是通过受限解析验证后的验证码浏览器代理，仅在部署显式配置时覆盖 Chromium 的系统代理选择。
	if proxy := captchaBrowserProxy(); proxy != "" {
		args = append(args, "--proxy-server="+proxy)
	}
	return args
}

// captchaIgnoreCertificateErrors 仅为 TLS 检查代理替换平台证书链的受控环境提供浏览器证书校验绕过；默认关闭，且不会影响 HTTP 客户端或已保存凭证的校验。
func captchaIgnoreCertificateErrors() bool {
	// value 是环境变量的去空白值，空值按安全默认值禁用证书绕过。
	value := strings.TrimSpace(os.Getenv("CAPTCHA_IGNORE_CERT_ERRORS"))
	if value == "" {
		return false
	}
	// parsed 是环境开关的布尔解释；err 表示值不属于 Go 支持的布尔文本。
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

// captchaBrowserProxy 返回仅供 token CAPTCHA Chromium 使用的显式代理地址；默认保留系统代理，非法、带凭证或带查询的值一律忽略，避免把敏感代理信息写入启动参数或日志。
func captchaBrowserProxy() string {
	// value 是环境变量中的代理地址，空值表示让 Chromium 沿用操作系统的正常网络配置。
	value := strings.TrimSpace(os.Getenv("CAPTCHA_BROWSER_PROXY"))
	if value == "" {
		return ""
	}
	// parsed、parseErr 是受限代理地址的结构化解析结果；仅接受 Chromium 支持的无凭证 HTTP(S)/SOCKS 代理。
	parsed, parseErr := url.Parse(value)
	if parseErr != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	// scheme 是统一小写后的代理协议，限制集合避免把任意 Chromium flag 拼入 Args。
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "http", "https", "socks4", "socks5":
		return value
	default:
		return ""
	}
}

// chromiumExecutablePath 封装chromiumExecutable路径业务协调。
func chromiumExecutablePath() *string {
	if // path 用于本次流程后续判断的路径
	path := strings.TrimSpace(os.Getenv("PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH")); path != "" {
		return playwright.String(path)
	}
	return nil
}

// skipPlaywrightBrowserDownload 封装skipPlaywright浏览器Download业务协调。
func skipPlaywrightBrowserDownload() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// packagedPlaywrightRuntimeReady 封装packagedPlaywrightRuntimeReady业务协调。
func packagedPlaywrightRuntimeReady() bool {
	// driverDir 用于本次流程后续判断的driverDir
	driverDir := strings.TrimSpace(os.Getenv("PLAYWRIGHT_DRIVER_PATH"))
	if driverDir == "" {
		return false
	}
	// nodeReady 用于本次流程后续判断的nodeReady
	nodeReady := false
	if // nodePath 用于本次流程后续判断的node路径
	nodePath := strings.TrimSpace(os.Getenv("PLAYWRIGHT_NODEJS_PATH")); nodePath != "" {
		// err 用于本次流程后续判断的err
		_, err := os.Stat(nodePath)
		nodeReady = err == nil
	} else {
		// nodeName 用于本次流程后续判断的node名称
		nodeName := "node"
		if runtime.GOOS == "windows" {
			nodeName = "node.exe"
		}
		// err 用于本次流程后续判断的err
		_, err := os.Stat(filepath.Join(driverDir, nodeName))
		nodeReady = err == nil
	}
	if !nodeReady {
		return false
	}
	if // err 用于本次流程后续判断的err
	_, err := os.Stat(filepath.Join(driverDir, "package", "cli.js")); err != nil {
		return false
	}
	if strings.TrimSpace(os.Getenv("PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH")) != "" {
		return true
	}
	// browserDir 用于本次流程后续判断的浏览器Dir
	browserDir := strings.TrimSpace(os.Getenv("PLAYWRIGHT_BROWSERS_PATH"))
	if browserDir == "" {
		return false
	}
	// matches、err 用于本次流程后续判断的matches、err
	matches, err := filepath.Glob(filepath.Join(browserDir, "chromium-*"))
	if err != nil {
		return false
	}
	// match 表示当前遍历过程中的match
	for _, match := range matches {
		if // info、statErr 用于本次流程后续判断的info、statErr
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

	// browserFingerprint is observed from the bundled Chromium once during
	// initialization.  Headless contexts use the same identity with only the
	// HeadlessChrome product token removed; headed contexts keep Chromium's
	// native identity.
	browserFingerprint xianyu.BrowserFingerprint
	userAgentMetadata  map[string]any

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

// poolEntry 用于本次流程后续判断的poolEntry
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
			// opts 用于本次流程后续判断的opts
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
		return errors.New("浏览器操作需要调用方 Context")
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

// accountRenewLock 封装账号Renew锁业务协调。
func (m *Manager) accountRenewLock(cookieID string) *sync.Mutex {
	m.renewMu.Lock()
	defer m.renewMu.Unlock()
	if m.renewLocks == nil {
		m.renewLocks = make(map[string]*sync.Mutex)
	}
	// lock 用于本次流程后续判断的锁
	lock := m.renewLocks[cookieID]
	if lock == nil {
		lock = &sync.Mutex{}
		m.renewLocks[cookieID] = lock
	}
	return lock
}

// acquireRenewSlot 封装acquireRenewSlot业务协调。
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
		if // err 用于本次流程后续判断的err
		err := m.installFn(); err != nil {
			m.initErr = fmt.Errorf("安装 playwright/chromium 失败（缺系统依赖时需手动执行 playwright install --with-deps）: %w", err)
			return
		}
		// pw、err 用于本次流程后续判断的pw、err
		pw, err := m.runFn()
		if err != nil {
			m.initErr = fmt.Errorf("启动 playwright 失败: %w", err)
			return
		}
		m.pw = pw
		if // err 用于本次流程后续判断的err
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

// Initialize 为兼容旧调用方在受限 Context 内启动 Playwright。
func (m *Manager) Initialize() error {
	// initializeCtx、initializeCancel 为旧入口提供有限初始化预算，避免浏览器初始化脱离进程关闭链。
	initializeCtx, initializeCancel := context.WithTimeout(context.Background(), legacyLifecycleOperationTimeout)
	defer initializeCancel()
	return m.InitializeContext(initializeCtx)
}

// InitializeContext 在调用方生命周期 Context 内启动 Playwright 并发布浏览器运行时指纹。
func (m *Manager) InitializeContext(ctx context.Context) error {
	// err 表示管理器已进入关闭流程、Context 无效或不能继续初始化 Playwright。
	if err := m.beginOperation(ctx); err != nil {
		return err
	}
	defer m.endOperation()
	return m.init()
}

// detectBrowserFingerprint 封装detect浏览器Fingerprint业务协调。
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
