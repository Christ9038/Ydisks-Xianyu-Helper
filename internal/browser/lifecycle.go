// Package browser 用 playwright-go 在进程内驱动 Chromium。
// 安装包提供预置 runtime；开发环境没有预置 runtime 时才自动下载。
package browser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
