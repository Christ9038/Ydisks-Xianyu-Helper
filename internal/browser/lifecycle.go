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
	"os/exec"
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

	// initMu 串行化 Playwright 安装、启动和指纹探测；初始化阶段不与 Close 并发发布半成品资源。
	initMu sync.Mutex
	// initErr 保存不可恢复的安装或启动错误；调用方取消不会写入该字段，以便后续重试。
	initErr error
	// installed 表示 Playwright 与运行时指纹均已完整发布。
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
	// installFn 执行可观察的 runtime 安装阶段；实现必须在开始和返回前尊重 Context。
	installFn func(context.Context) error
	// runFn 执行可观察的 Playwright 启动阶段；实现必须在开始和返回前尊重 Context。
	runFn func(context.Context) (*playwright.Playwright, error)
	// fingerprintFn 读取已启动 Chromium 的指纹；测试可注入失败，验证半成品资源不会发布。
	fingerprintFn func() error

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
		installFn: func(ctx context.Context) error {
			// err 保存安装阶段开始前已取消的调用方 Context 错误。
			if err := ctx.Err(); err != nil {
				return err
			}
			if packagedPlaywrightRuntimeReady() {
				return nil
			}
			// opts 保存开发环境安装 driver 和 Chromium 所需的固定运行参数。
			opts := &playwright.RunOptions{
				Browsers: []string{"chromium"},
				Verbose:  false,
			}
			if skipPlaywrightBrowserDownload() {
				opts.SkipInstallBrowsers = true
			}
			// installErr 保存可取消安装流程的错误；浏览器 CLI 必须由 Context 控制，不能包装为不可观察后台 goroutine。
			installErr := installPlaywrightRuntime(ctx, opts)
			// contextErr 保存安装完成后才观察到的取消错误，禁止发布已无主的安装结果。
			if contextErr := ctx.Err(); contextErr != nil {
				return contextErr
			}
			return installErr
		},
		runFn: func(ctx context.Context) (*playwright.Playwright, error) {
			// err 保存启动阶段开始前已取消的调用方 Context 错误。
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			// pw、runErr 保存同步启动的 Playwright 进程及启动错误；已取消时调用方负责停止新进程。
			pw, runErr := playwright.Run()
			// contextErr 保存启动完成后才观察到的取消错误，必须同步停止半成品进程。
			if contextErr := ctx.Err(); contextErr != nil {
				if pw != nil {
					_ = pw.Stop()
				}
				return nil, contextErr
			}
			return pw, runErr
		},
		fingerprintFn: nil,
	}
	manager.fingerprintFn = manager.detectBrowserFingerprint
	manager.lifecycleCond = sync.NewCond(&manager.lifecycleMu)
	return manager
}

// installPlaywrightRuntime 下载缺失 driver，并用 Context 控制 Node CLI 安装 Chromium；包内 runtime 已就绪时调用方不会进入此路径。
func installPlaywrightRuntime(ctx context.Context, options *playwright.RunOptions) error {
	if ctx == nil {
		return errors.New("安装 Playwright runtime 需要 Context")
	}
	// err 保存安装前调用方已经取消的 Context 错误。
	if err := ctx.Err(); err != nil {
		return err
	}
	// installer、args 保存可被 Context 终止的 browser-install 子进程及其参数。
	installer, args, installerErr := browserInstallerCommand(options)
	if installerErr != nil {
		return installerErr
	}
	// controlledCommand 是由初始化 Context 取消的完整 runtime 安装子进程，覆盖 driver、Node 和 Chromium 下载。
	controlledCommand := exec.CommandContext(ctx, installer, args...)
	// installErr 保存独立安装器及其子进程树的退出错误；Context 取消时由 os/exec 终止安装器。
	installErr := controlledCommand.Run()
	// contextErr 保存 CLI 返回后观察到的取消错误，取消语义优先于子进程退出错误。
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	return installErr
}

// browserInstallerCommand 查找可取消的 browser-install 命令；优先使用部署提供的可执行文件和同目录打包助手，开发源码树最后通过 go run 调用同一入口。
func browserInstallerCommand(options *playwright.RunOptions) (string, []string, error) {
	if options == nil {
		return "", nil, errors.New("安装 Playwright runtime 缺少运行参数")
	}
	// driverOnly 控制是否只准备 driver；跳过浏览器下载时不能让子进程误下载 Chromium。
	args := []string{"-driver-dir", options.DriverDirectory}
	if options.SkipInstallBrowsers {
		args = append(args, "-driver-only")
	}
	// configured 是部署显式指定的独立安装器路径，必须是可被 Context 直接终止的单一进程入口。
	if configured := strings.TrimSpace(os.Getenv("PLAYWRIGHT_BROWSER_INSTALLER")); configured != "" {
		return configured, args, nil
	}
	// executable 保存当前服务可执行文件路径；打包目录约定 browser-install 与服务二进制同目录。
	if executable, err := os.Executable(); err == nil {
		// candidate 是与服务二进制同目录的打包安装器路径。
		candidate := filepath.Join(filepath.Dir(executable), "browser-install")
		if runtime.GOOS == "windows" {
			candidate += ".exe"
		}
		// statErr 表示候选安装器不存在或不可访问时的文件检查错误。
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, args, nil
		}
	}
	// sourceFile 保存当前编译单元的源码路径；开发模式依赖它定位模块内唯一的安装器入口。
	_, sourceFile, _, callerOK := runtime.Caller(0)
	if callerOK {
		// moduleRoot 是从 internal/browser/lifecycle.go 回退两级得到的 Go 模块根目录。
		moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
		// installerSource 是开发源码树中的安装器 main 包，必须与运行中库版本一致。
		installerSource := filepath.Join(moduleRoot, "cmd", "browser-install")
		// moduleFileErr、installerErr 分别验证模块声明和安装器源码存在，避免把任意工作目录交给 go run。
		_, moduleFileErr := os.Stat(filepath.Join(moduleRoot, "go.mod"))
		// installerErr 表示源码安装器入口是否存在；缺失时继续尝试其他部署方式并返回可诊断错误。
		_, installerErr := os.Stat(filepath.Join(installerSource, "main.go"))
		if moduleFileErr == nil && installerErr == nil {
			// goCommand 是当前开发工具链的绝对路径；找不到时保留清晰的部署错误，而不是启动无法管理的下载 goroutine。
			goCommand, goErr := exec.LookPath("go")
			if goErr == nil {
				// sourceArgs 强制 Go 工具链在模块根解析依赖，测试或调用方切换工作目录时仍把双横线后的参数交给安装器。
				sourceArgs := append([]string{"-C", moduleRoot, "run", installerSource, "--"}, args...)
				return goCommand, sourceArgs, nil
			}
		}
	}
	return "", nil, errors.New("找不到可取消的 browser-install 安装器，请配置 PLAYWRIGHT_BROWSER_INSTALLER、同目录安装器或在源码开发环境中提供 Go 工具链")
}

// beginOperation 登记一个可能持有 Chromium 实例的调用；关闭开始后拒绝新调用。
// ctx 仅用于在进入状态机前传播调用方取消语义，不会启动无法回收的等待 goroutine。
func (m *Manager) beginOperation(ctx context.Context) error {
	if ctx == nil {
		return errors.New("浏览器操作需要调用方 Context")
	}
	// err 表示调用方 Context 已取消，管理器不会为已取消调用登记活动任务。
	// err 保存进入初始化前已取消的 Context 错误。
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

// init 为未传递 Context 的既有浏览器调用提供有限预算；冻结验证码调用链继续使用该兼容入口。
func (m *Manager) init() error {
	// initCtx、initCancel 为兼容调用提供有限初始化预算；新生命周期入口必须使用 initContext。
	initCtx, initCancel := context.WithTimeout(context.Background(), legacyLifecycleOperationTimeout)
	defer initCancel()
	return m.initContext(initCtx)
}

// initContext 串行完成安装、启动和指纹探测；取消或探测失败会释放已经启动的 Playwright，允许后续重试。
func (m *Manager) initContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("初始化浏览器需要 Context")
	}
	// err 保存获得初始化互斥锁后观察到的取消错误。
	if err := ctx.Err(); err != nil {
		return err
	}
	m.initMu.Lock()
	defer m.initMu.Unlock()
	if m.installed {
		return nil
	}
	if m.initErr != nil {
		return m.initErr
	}
	// err 保存获得初始化互斥锁后观察到的取消错误。
	if err := ctx.Err(); err != nil {
		return err
	}
	// err 保存可观察安装阶段的错误。
	if err := m.installFn(ctx); err != nil {
		// contextErr 保存安装返回后观察到的取消错误，取消不应污染可重试初始化状态。
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		m.initErr = fmt.Errorf("安装 playwright/chromium 失败（缺系统依赖时需手动执行 playwright install --with-deps）: %w", err)
		return m.initErr
	}
	// err 保存安装完成后、启动 Playwright 前观察到的取消错误。
	if err := ctx.Err(); err != nil {
		return err
	}
	// pw、runErr 保存 Playwright 启动结果；任何后续失败都必须同步停止该进程。
	pw, runErr := m.runFn(ctx)
	if runErr != nil {
		// contextErr 保存启动阶段返回时的取消错误，优先于基础设施启动错误返回。
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		m.initErr = fmt.Errorf("启动 playwright 失败: %w", runErr)
		return m.initErr
	}
	// err 保存 Playwright 启动后、发布实例前观察到的取消错误。
	if err := ctx.Err(); err != nil {
		if pw != nil {
			_ = pw.Stop()
		}
		return err
	}
	m.pw = pw
	// err 保存 Chromium 指纹探测错误；失败时必须释放刚启动的 Playwright。
	// fingerprintFn 保存生产探测函数；零值测试 Manager 没有注入时回退到真实实现。
	fingerprintFn := m.fingerprintFn
	if fingerprintFn == nil {
		fingerprintFn = m.detectBrowserFingerprint
	}
	// err 保存 Chromium 指纹探测失败原因，失败时必须释放尚未发布的 Playwright。
	if err := fingerprintFn(); err != nil {
		if pw != nil {
			_ = pw.Stop()
		}
		m.pw = nil
		// contextErr 保存探测失败同时观察到的取消错误，取消语义优先于探测诊断。
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		m.initErr = fmt.Errorf("读取 Playwright Chromium 原生指纹失败: %w", err)
		return m.initErr
	}
	// err 保存指纹探测后、最终发布前观察到的取消错误。
	if err := ctx.Err(); err != nil {
		_ = pw.Stop()
		m.pw = nil
		return err
	}
	m.installed = true
	m.logger.Info("playwright chromium 就绪")
	return nil
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
	return m.initContext(ctx)
}

// detectBrowserFingerprint 封装detect浏览器Fingerprint业务协调。
