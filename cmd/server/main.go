// Package main 是闲鱼管家 Go 主进程入口。
// 启动：DB 迁移 → 加载账号引擎 → HTTP API 服务。
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"xianyu-go/internal/auth"
	compositionruntime "xianyu-go/internal/composition/runtime"
	"xianyu-go/internal/db"
	"xianyu-go/internal/logging"
	"xianyu-go/internal/logsafe"
	appversion "xianyu-go/internal/version"
)

// serverOptions 保存命令行和环境变量解析后的进程启动选项。
type serverOptions struct {
	dbPath                string
	dbURL                 string
	addr                  string
	webDir                string
	workDir               string
	playwrightRuntimeRoot string
	playwrightDriverDir   string
	playwrightBrowserDir  string
	dataKeyFile           string
	secure                bool
	noBrowser             bool
	verbose               bool
	logLevel              string
	logFormat             string
	initAdmin             bool
	ensureAdmin           bool
	adminEmail            string
	adminPassword         string
	service               bool
	showVersion           bool
}

// defaultDBPath 保存默认 SQLite 数据库的相对路径；桌面运行时会根据 dataDir 重定位。
const (
	defaultDBPath      = "data/xianyu_data.db"
	userDataDirName    = "YdisksXianyuHelper"
	defaultDataKeyName = "data-key"
	// httpShutdownTimeout 限制 HTTP 请求排空和 Server 自有后台任务等待时长。
	httpShutdownTimeout = 10 * time.Second
	// applicationShutdownTimeout 限制应用 worker 收到取消后的独立收束和 Join 时长。
	applicationShutdownTimeout = 10 * time.Second
)

// errDataKeyEmpty 表示已打开的数据密钥文件尚未包含有效内容；并发读取者可以有限等待写入者完成。
var errDataKeyEmpty = errors.New("data key 文件为空")

// serverStartupConfig 保存目录、数据库和日志策略准备阶段的派生配置。
type serverStartupConfig struct {
	// dataDir 是桌面服务或显式 workdir 使用的应用数据目录，空值表示沿用当前目录布局。
	dataDir string
	// resolvedDBURL 是按环境变量、命令行和默认路径优先级解析出的数据库地址。
	resolvedDBURL string
	// explicitLogLevel 表示日志等级是否由环境变量或命令行显式指定，显式值优先于数据库设置。
	explicitLogLevel bool
	// explicitLogFormat 表示日志格式是否由环境变量或命令行显式指定，显式值优先于数据库设置。
	explicitLogFormat bool
	// resolvedLogFormat 是启动时选择的初始日志输出格式。
	resolvedLogFormat string
}

// serverInfrastructure 保存数据库、日志和加密设置已就绪的基础设施，并集中承担关闭责任。
type serverInfrastructure struct {
	// database 是已完成迁移并由本进程独占关闭的数据库连接池。
	database *sql.DB
	// store 提供应用服务使用的数据库仓储集合。
	store *db.Store
	// logger 是同时写入标准输出或服务日志文件的结构化日志器。
	logger *slog.Logger
	// logWriter 是 logger 使用的输出目标，关闭由 closeLog 负责。
	logWriter io.Writer
	// closeLog 释放服务日志文件；标准输出场景下该函数为空操作。
	closeLog func()
	// initializationOnly 表示本次调用仅完成 -init-admin 管理员初始化，调用方不得继续启动运行时。
	initializationOnly bool
}

// close 逆序释放日志文件和数据库连接，允许基础设施初始化失败路径统一调用。
func (i serverInfrastructure) close() {
	if i.database != nil {
		_ = i.database.Close()
	}
	if i.closeLog != nil {
		i.closeLog()
	}
}

// serverRuntime 保存 HTTP 服务及应用生命周期协调器，二者必须由同一 Context 启动和关闭。
type serverRuntime struct {
	// httpServer 是已完成依赖注入、尚未启动监听的 HTTP 服务。
	httpServer httpRuntime
	// lifecycleCoordinator 按登记顺序启动组件并按逆序关闭后台 worker。
	lifecycleCoordinator applicationRuntimeCoordinator
}

// httpRuntimeStopper 定义进程关闭阶段需要的最小 HTTP 停止能力。
// Stop 必须只收束 HTTP 请求和 Server 自有任务，不能等待应用 worker。
type httpRuntimeStopper interface {
	// Stop 在调用方提供的独立 HTTP 关闭 Context 内完成请求排空。
	Stop(context.Context) error
}

// httpRuntime 定义正常进程启动、等待和关闭 HTTP 服务所需的完整能力。
type httpRuntime interface {
	// Bind 在启动后台组件前同步占用 HTTP 监听端口。
	Bind() error
	// Start 开始接收 HTTP 请求。
	Start(context.Context) error
	// Wait 等待 HTTP 监听退出。
	Wait() error
	Stop(context.Context) error
}

// applicationRuntimeCloser 定义进程关闭阶段需要的最小应用组件收束能力。
// Close 必须取消并等待全部已登记应用 worker，返回值保留未完成组件诊断。
type applicationRuntimeCloser interface {
	// Close 在调用方提供的独立应用关闭 Context 内取消并 Join worker。
	Close(context.Context) error
}

// applicationRuntimeCoordinator 定义 cmd 对组合根生命周期对象执行启动和关闭所需的能力。
type applicationRuntimeCoordinator interface {
	Start(context.Context) error
	Close(context.Context) error
}

// main 封装main业务协调。
func main() {
	// opts 是命令行解析出的服务启动选项。
	opts := parseOptions()
	if opts.showVersion {
		fmt.Printf("Ydisks Xianyu Helper %s (commit %s, built %s)\n", appversion.Version, appversion.ShortCommit(), appversion.BuildTime)
		return
	}
	if opts.workDir != "" {
		// err 表示切换到桌面服务指定工作目录失败。
		if err := os.Chdir(opts.workDir); err != nil {
			fmt.Fprintf(os.Stderr, "切换工作目录失败: %s\n", logsafe.Error(err))
			os.Exit(2)
		}
	}

	// run 统一封装平台服务和前台进程都会调用的启动函数。
	run := func(ctx context.Context) error { return runServer(ctx, opts) }
	if opts.service {
		// err 表示平台注册或运行服务失败。
		if err := runPlatformService("YdisksXianyuHelper", run); err != nil {
			fmt.Fprintf(os.Stderr, "服务运行失败: %s\n", logsafe.Error(err))
			os.Exit(1)
		}
		return
	}

	// ctx 接收终端信号取消；cancel 在主进程退出时解除信号订阅。
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	// err 表示前台服务的启动、监听或关闭失败。
	if err := run(ctx); err != nil {
		slog.Error("服务退出", "err", logsafe.Error(err))
		os.Exit(1)
	}
}

// parseOptions 封装parseOptions业务协调。
func parseOptions() serverOptions {
	// opts 收集所有命令行标志的目标字段，并在 flag.Parse 后返回。
	var opts serverOptions
	flag.StringVar(&opts.dbPath, "db", defaultDBPath, "SQLite 数据库路径（兼容旧用法）")
	flag.StringVar(&opts.dbURL, "db-url", "", "数据库连接 URL（sqlite:// mysql:// postgres://），优先级高于 -db；也可用 DATABASE_URL 环境变量")
	flag.StringVar(&opts.addr, "addr", ":59188", "HTTP 监听地址")
	flag.StringVar(&opts.webDir, "web", "", "前端静态资源目录（含 index.html）")
	flag.StringVar(&opts.workDir, "workdir", "", "服务工作目录；用于桌面服务固定数据和浏览器目录")
	flag.StringVar(&opts.playwrightRuntimeRoot, "playwright-runtime-root", "", "随安装包分发的 Playwright runtime 根目录")
	flag.StringVar(&opts.playwrightDriverDir, "playwright-driver-dir", "", "Playwright driver 目录")
	flag.StringVar(&opts.playwrightBrowserDir, "playwright-browser-dir", "", "Playwright 浏览器缓存目录")
	flag.StringVar(&opts.dataKeyFile, "data-key-file", "", "XIANYU_DATA_KEY 持久化文件；不存在时自动生成")
	flag.BoolVar(&opts.secure, "secure", false, "HTTPS 模式（Cookie 加 Secure）")
	flag.BoolVar(&opts.noBrowser, "no-browser", false, "禁用 Chromium（本机浏览器指纹读取和 token 滑块自动处理将不可用）")
	flag.BoolVar(&opts.verbose, "v", false, "调试日志")
	flag.StringVar(&opts.logLevel, "log-level", "", "日志等级：debug/info/warn/error；默认读取 LOG_LEVEL 或系统设置")
	flag.StringVar(&opts.logFormat, "log-format", "", "日志格式：text/json；默认读取 LOG_FORMAT")
	flag.BoolVar(&opts.initAdmin, "init-admin", false, "初始化或重置 admin 管理员后退出")
	flag.BoolVar(&opts.ensureAdmin, "ensure-admin", false, "仅在 admin 管理员不存在时初始化；已存在时不修改密码")
	flag.StringVar(&opts.adminEmail, "admin-email", "admin@example.com", "初始化 admin 的邮箱")
	flag.StringVar(&opts.adminPassword, "admin-password", "", "初始化/重置 admin 密码；也可用 XIANYU_ADMIN_PASSWORD 环境变量")
	flag.BoolVar(&opts.service, "service", false, "以 Windows Service 模式运行")
	flag.BoolVar(&opts.showVersion, "version", false, "显示版本和构建信息后退出")
	flag.Parse()
	return opts
}

// runServer 按准备、基础设施、运行时装配和生命周期四个阶段启动服务，并在退出时逆序释放资源。
func runServer(parent context.Context, opts serverOptions) error {
	// ctx 是本次服务实例的取消上下文；cancel 在所有启动路径结束时释放派生资源。
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	// startup 保存目录、数据库和日志策略的派生配置。
	startup, err := prepareServerStartup(&opts)
	if err != nil {
		return err
	}
	// infrastructure 负责数据库、日志和加密设置资源的生命周期。
	infrastructure, err := openServerInfrastructure(ctx, startup, opts)
	if err != nil {
		return err
	}
	defer infrastructure.close()
	if infrastructure.initializationOnly {
		return nil
	}
	// runtime 保存已完成依赖注入但尚未启动的 HTTP 服务和生命周期协调器。
	runtime, err := buildServerRuntime(opts, infrastructure)
	if err != nil {
		return err
	}
	return runServerLifecycle(ctx, runtime, infrastructure.logger)
}

// prepareServerStartup 解析数据目录、Playwright 路径、数据密钥、数据库地址和日志启动策略，并把必需环境变量写入当前进程。
func prepareServerStartup(opts *serverOptions) (serverStartupConfig, error) {
	// dataDir 是显式 workdir 或桌面平台默认用户数据目录。
	dataDir, err := resolveDataDir(opts.workDir)
	if err != nil {
		return serverStartupConfig{}, err
	}
	// packagedPlaywrightRuntime 表示安装包是否显式提供浏览器 runtime；本地开发不应把空目录当作缓存。
	packagedPlaywrightRuntime := strings.TrimSpace(opts.playwrightRuntimeRoot) != ""
	applyPlaywrightRuntimeRoot(opts)
	if dataDir != "" {
		// err 表示创建受限权限应用数据目录时的文件系统错误。
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			return serverStartupConfig{}, fmt.Errorf("创建应用数据目录失败: %w", err)
		}
		if opts.dataKeyFile == "" {
			opts.dataKeyFile = filepath.Join(dataDir, defaultDataKeyName)
		}
		if packagedPlaywrightRuntime && opts.playwrightDriverDir == "" {
			opts.playwrightDriverDir = filepath.Join(dataDir, "playwright-driver")
		}
		if packagedPlaywrightRuntime && opts.playwrightBrowserDir == "" {
			opts.playwrightBrowserDir = filepath.Join(dataDir, "playwright-browsers")
		}
	}
	if opts.playwrightDriverDir != "" {
		// err 表示写入 Playwright driver 路径环境变量失败。
		if err := os.Setenv("PLAYWRIGHT_DRIVER_PATH", opts.playwrightDriverDir); err != nil {
			return serverStartupConfig{}, fmt.Errorf("设置 Playwright driver 目录失败: %w", err)
		}
	}
	if opts.playwrightBrowserDir != "" {
		// err 表示写入 Playwright browser 缓存路径环境变量失败。
		if err := os.Setenv("PLAYWRIGHT_BROWSERS_PATH", opts.playwrightBrowserDir); err != nil {
			return serverStartupConfig{}, fmt.Errorf("设置 Playwright 浏览器目录失败: %w", err)
		}
	}
	if strings.TrimSpace(os.Getenv("XIANYU_DATA_KEY")) == "" && opts.dataKeyFile != "" {
		// key 是从磁盘读取或新生成的加密主密钥；不得写入日志。
		key, keyErr := loadOrCreateDataKey(opts.dataKeyFile)
		if keyErr != nil {
			return serverStartupConfig{}, keyErr
		}
		// err 表示把数据加密主密钥写入当前进程环境失败。
		if err := os.Setenv("XIANYU_DATA_KEY", key); err != nil {
			return serverStartupConfig{}, fmt.Errorf("设置 XIANYU_DATA_KEY 失败: %w", err)
		}
	}
	// resolvedDBURL 按 DATABASE_URL、-db-url、-db 默认优先级选择实际数据库地址。
	resolvedDBURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if resolvedDBURL == "" {
		resolvedDBURL = strings.TrimSpace(opts.dbURL)
	}
	if resolvedDBURL == "" {
		resolvedDBURL = resolveDBPath(dataDir, opts.dbPath)
	}
	if dataDir != "" && resolvedDBURL == resolveDBPath(dataDir, defaultDBPath) {
		// err 表示创建默认 SQLite 数据库父目录失败。
		if err := os.MkdirAll(filepath.Dir(resolvedDBURL), 0o700); err != nil {
			return serverStartupConfig{}, fmt.Errorf("创建数据库目录失败: %w", err)
		}
	}
	// resolvedLogLevel 是环境变量、命令行和 verbose 共同决定的日志等级；explicit 标记是否禁止数据库覆盖。
	resolvedLogLevel := strings.TrimSpace(os.Getenv("LOG_LEVEL"))
	// explicitLogLevel 表示日志等级来自进程配置，因此不允许数据库设置覆盖它。
	explicitLogLevel := resolvedLogLevel != ""
	if strings.TrimSpace(opts.logLevel) != "" {
		resolvedLogLevel = strings.TrimSpace(opts.logLevel)
		explicitLogLevel = true
	}
	if resolvedLogLevel == "" && opts.verbose {
		resolvedLogLevel = "debug"
		explicitLogLevel = true
	}
	// err 表示初始日志等级不被 logging 包支持。
	if err := logging.SetLevel(resolvedLogLevel); err != nil {
		return serverStartupConfig{}, fmt.Errorf("日志等级无效: %w", err)
	}
	// resolvedLogFormat 是环境变量或命令行选择的初始日志格式；explicitLogFormat 控制数据库设置是否可覆盖。
	resolvedLogFormat := strings.TrimSpace(os.Getenv("LOG_FORMAT"))
	// explicitLogFormat 表示日志格式来自进程配置，因此不允许数据库设置覆盖它。
	explicitLogFormat := resolvedLogFormat != ""
	if strings.TrimSpace(opts.logFormat) != "" {
		resolvedLogFormat = strings.TrimSpace(opts.logFormat)
		explicitLogFormat = true
	}
	return serverStartupConfig{dataDir: dataDir, resolvedDBURL: resolvedDBURL, explicitLogLevel: explicitLogLevel, explicitLogFormat: explicitLogFormat, resolvedLogFormat: resolvedLogFormat}, nil
}

// openServerInfrastructure 打开日志和数据库、升级敏感字段、应用数据库日志设置并处理管理员初始化选项。
func openServerInfrastructure(ctx context.Context, startup serverStartupConfig, opts serverOptions) (serverInfrastructure, error) {
	// logWriter 和 closeLog 共同管理服务日志输出目标。
	logWriter, closeLog, err := openServerLogWriter(startup.dataDir)
	if err != nil {
		return serverInfrastructure{}, err
	}
	// logger 是当前进程的初始结构化日志器；后续数据库日志格式变更会替换默认 logger。
	logger := logging.NewLogger(logWriter, startup.resolvedLogFormat)
	slog.SetDefault(logger)
	// database 和 dialect 表示已打开数据库及其 SQL 方言；database 的关闭责任转移给返回值。
	database, dialect, err := db.Open(ctx, startup.resolvedDBURL)
	if err != nil {
		closeLog()
		return serverInfrastructure{}, fmt.Errorf("打开数据库失败: %w", err)
	}
	logger.Info("数据库已就绪", "dialect", dialect)
	// store 是绑定数据库方言的仓储集合。
	store := db.NewStore(database, dialect)
	// err 表示历史敏感字段加密校验或升级失败，失败时不能继续运行。
	if err := store.EncryptLegacySecrets(ctx); err != nil {
		_ = database.Close()
		closeLog()
		return serverInfrastructure{}, fmt.Errorf("校验或升级数据库敏感字段失败: %w", err)
	}
	if !startup.explicitLogLevel {
		// level、levelErr 分别是数据库保存的日志等级及其读取错误；读取失败时保留进程默认值。
		if level, levelErr := store.Settings.Get(ctx, "log_level"); levelErr == nil && strings.TrimSpace(level) != "" {
			// setErr 表示数据库日志等级不合法，系统会记录警告并继续使用现有等级。
			if setErr := logging.SetLevel(level); setErr != nil {
				logger.Warn("忽略无效的系统日志设置", "value", level, "err", setErr)
			}
		}
	}
	if !startup.explicitLogFormat {
		// format、formatErr 分别是数据库保存的日志格式及其读取错误；读取失败时保留进程默认格式。
		if format, formatErr := store.Settings.Get(ctx, "log_format"); formatErr == nil && strings.TrimSpace(format) != "" {
			logger = logging.NewLogger(logWriter, format)
			slog.SetDefault(logger)
		}
	}
	if opts.initAdmin {
		// err 表示管理员密码初始化或重置失败，失败时不进入后续运行时装配。
		if err := ensureAdmin(ctx, store, opts.adminEmail, opts.adminPassword); err != nil {
			_ = database.Close()
			closeLog()
			return serverInfrastructure{}, fmt.Errorf("初始化管理员失败: %w", err)
		}
		logger.Info("管理员初始化完成", "username", "admin")
		return serverInfrastructure{database: database, store: store, logger: logger, logWriter: logWriter, closeLog: closeLog, initializationOnly: true}, nil
	}
	if opts.ensureAdmin {
		// created、ensureErr 分别标记是否新建管理员以及查询或创建管理员时的失败。
		created, ensureErr := ensureAdminIfMissing(ctx, store, opts.adminEmail, opts.adminPassword)
		if ensureErr != nil {
			_ = database.Close()
			closeLog()
			return serverInfrastructure{}, fmt.Errorf("检查或初始化管理员失败: %w", ensureErr)
		}
		if created {
			logger.Info("管理员初始化完成", "username", "admin")
		}
	}
	// initialized 表示系统是否已有管理员；查询失败按未初始化提示而不阻断启动。
	if initialized, _ := store.Users.IsSystemInitialized(ctx); !initialized {
		if isNonLoopbackListenAddress(opts.addr) {
			logger.Warn("系统尚未初始化且 HTTP 监听可被远程访问；首个访问初始化页面的客户端可创建管理员，请由部署者通过网络隔离或 -init-admin 承担该风险", "addr", opts.addr)
		} else {
			logger.Warn("系统尚未初始化，请先运行本二进制的 -init-admin 初始化管理员")
		}
	}
	return serverInfrastructure{database: database, store: store, logger: logger, logWriter: logWriter, closeLog: closeLog}, nil
}

// buildServerRuntime 构造浏览器、账号、自动化、通知、应用服务和 HTTP 服务依赖，并登记全部生命周期组件但不启动它们。
func buildServerRuntime(opts serverOptions, infrastructure serverInfrastructure) (serverRuntime, error) {
	// runtime、buildErr 分别是组合层返回的完整运行时快照及其装配失败原因。
	runtime, buildErr := compositionruntime.BuildRuntime(compositionruntime.RuntimeOptions{
		NoBrowser: opts.noBrowser, SecureCookie: opts.secure, WebDir: opts.webDir, Addr: opts.addr,
	}, compositionruntime.RuntimeInfrastructure{Store: infrastructure.store, Logger: infrastructure.logger})
	if buildErr != nil {
		return serverRuntime{}, buildErr
	}
	return serverRuntime{httpServer: runtime.HTTPServer, lifecycleCoordinator: runtime.Lifecycle}, nil
}

// runServerLifecycle 启动应用组件和 HTTP 监听，等待退出后执行有限时长的 HTTP 与组件逆序关闭。
func runServerLifecycle(ctx context.Context, runtime serverRuntime, logger *slog.Logger) error {
	// bindErr 是应用 worker 启动前同步占用 HTTP 端口的错误，端口冲突不得触发任何业务副作用。
	if bindErr := runtime.httpServer.Bind(); bindErr != nil {
		return fmt.Errorf("绑定 HTTP 服务失败: %w", bindErr)
	}
	// err 表示应用组件按依赖顺序启动失败。
	if err := runtime.lifecycleCoordinator.Start(ctx); err != nil {
		// stopCtx、stopCancel 关闭已绑定但尚未 Serve 的监听器，避免启动失败后端口泄漏。
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		return errors.Join(fmt.Errorf("启动应用生命周期失败: %w", err), runtime.httpServer.Stop(stopCtx))
	}
	// err 表示 HTTP 服务监听启动失败；失败后必须关闭已启动的应用组件。
	if err := runtime.httpServer.Start(ctx); err != nil {
		return errors.Join(fmt.Errorf("启动 HTTP 服务失败: %w", err), rollbackApplicationRuntime(runtime.lifecycleCoordinator, 10*time.Second))
	}
	// runErr 是 HTTP 监听退出时返回的错误；退出后仍需关闭所有应用组件。
	runErr := runtime.httpServer.Wait()
	if runErr != nil {
		logger.Error("HTTP 服务退出", "err", runErr)
	}
	// shutdownErr 保存 HTTP 与应用组件关闭阶段的聚合结果；任一阶段超时都不能伪装为成功退出。
	shutdownErr := closeServerRuntime(runtime.httpServer, runtime.lifecycleCoordinator, logger, httpShutdownTimeout, applicationShutdownTimeout)
	return errors.Join(runErr, shutdownErr)
}

// rollbackApplicationRuntime 在 HTTP 启动失败后以独立预算回滚应用组件，并保留未完成 worker 的诊断。
func rollbackApplicationRuntime(lifecycleCoordinator applicationRuntimeCloser, timeout time.Duration) error {
	if lifecycleCoordinator == nil {
		return nil
	}
	// rollbackCtx 限制 HTTP 启动失败后的组件回滚时间，避免后台 worker 泄漏。
	rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), timeout)
	defer rollbackCancel()
	// rollbackErr 保存应用组件逆序回滚的底层错误。
	rollbackErr := lifecycleCoordinator.Close(rollbackCtx)
	return wrapShutdownError("HTTP 启动失败后的应用回滚", rollbackErr)
}

// closeServerRuntime 依次关闭 HTTP transport 和应用 worker，并为两者分配互不共享的关闭预算。
// HTTP 关闭失败不会阻止应用 Context 取消和 worker Join；返回值保留每个失败阶段及其未完成组件诊断。
func closeServerRuntime(httpServer httpRuntimeStopper, lifecycleCoordinator applicationRuntimeCloser, logger *slog.Logger, httpTimeout, applicationTimeout time.Duration) error {
	// shutdownStartedAt 记录 HTTP 监听退出后开始收束各生命周期组件的时间，用于排查关闭预算耗尽。
	shutdownStartedAt := time.Now()
	// httpStopCtx、httpStopCancel 为 HTTP 请求排空和 Server 自有后台任务分配独立关闭预算。
	httpStopCtx, httpStopCancel := context.WithTimeout(context.Background(), httpTimeout)
	defer httpStopCancel()
	// httpErr 保存 HTTP 优雅关闭错误；该错误不会阻断应用 worker 收束。
	var httpErr error
	if httpServer != nil {
		httpErr = httpServer.Stop(httpStopCtx)
	}
	if httpErr != nil && logger != nil {
		logger.Warn("HTTP 服务关闭未完成", "err", httpErr)
	}
	// applicationStopCtx、applicationStopCancel 为已取消的应用 worker 分配独立 Join 预算，避免 HTTP 超时吞掉 worker 收束机会。
	applicationStopCtx, applicationStopCancel := context.WithTimeout(context.Background(), applicationTimeout)
	defer applicationStopCancel()
	// applicationErr 保存应用组件逆序关闭的聚合错误，其中包含未完成组件名称。
	var applicationErr error
	if lifecycleCoordinator != nil {
		applicationErr = lifecycleCoordinator.Close(applicationStopCtx)
	}
	if applicationErr != nil && logger != nil {
		logger.Warn("应用组件关闭未完成", "err", applicationErr)
	}
	// shutdownErr 为调用方保留阶段名称和底层错误，便于区分 HTTP 排空与 worker Join 失败。
	shutdownErr := errors.Join(
		wrapShutdownError("HTTP 服务关闭", httpErr),
		wrapShutdownError("应用组件关闭", applicationErr),
	)
	if logger != nil {
		logger.Info("服务生命周期关闭收束完成", "duration", time.Since(shutdownStartedAt), "http_completed", httpErr == nil, "application_completed", applicationErr == nil)
	}
	return shutdownErr
}

// wrapShutdownError 为非空关闭错误附加稳定阶段名称；空错误保持为 nil 以支持 errors.Join。
func wrapShutdownError(stage string, shutdownErr error) error {
	if shutdownErr == nil {
		return nil
	}
	return fmt.Errorf("%s失败: %w", stage, shutdownErr)
}

// openServerLogWriter keeps container and interactive runs on stdout while
// desktop/system-service installations persist logs in their platform log
// directory. Windows services do not have a useful console, so they get a
// default log file beside the service data directory.
// openServerLogWriter 封装openServerLogWriter业务协调。
func openServerLogWriter(dataDir string) (io.Writer, func(), error) {
	// logDir 是环境变量或 Windows 服务数据目录指定的持久化日志目录。
	logDir := strings.TrimSpace(os.Getenv("XIANYU_LOG_DIR"))
	if logDir == "" && runtime.GOOS == "windows" && dataDir != "" {
		logDir = filepath.Join(dataDir, "logs")
	}
	if logDir == "" {
		return os.Stdout, func() {}, nil
	}
	// err 表示创建受限权限日志目录失败。
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("创建日志目录失败: %w", err)
	}
	// logPath 是服务进程追加写入的单一日志文件路径。
	logPath := filepath.Join(logDir, "server.log")
	// file、err 分别是已打开的日志文件与打开失败原因。
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("打开日志文件失败: %w", err)
	}
	return file, func() { _ = file.Close() }, nil
}

// applyPlaywrightRuntimeRoot 封装applyPlaywrightRuntimeRoot业务协调。
func applyPlaywrightRuntimeRoot(opts *serverOptions) {
	if opts == nil {
		return
	}
	// root 是安装包提供的 Playwright runtime 根目录。
	root := strings.TrimSpace(opts.playwrightRuntimeRoot)
	if root == "" {
		return
	}
	// archRoot 是当前 Go 架构对应的驱动和浏览器资源目录。
	archRoot := filepath.Join(root, runtime.GOARCH)
	if opts.playwrightDriverDir == "" {
		opts.playwrightDriverDir = filepath.Join(archRoot, "playwright-driver")
	}
	if opts.playwrightBrowserDir == "" {
		opts.playwrightBrowserDir = filepath.Join(archRoot, "playwright-browsers")
	}
}

// resolveDataDir 返回桌面端的标准用户数据目录。
// Linux/Docker 保留原有相对路径行为；macOS 和 Windows 在没有显式 -workdir
// 时使用当前用户的系统配置目录，避免把具体用户路径写进安装包或代码。
// resolveDataDir 封装resolve数据Dir业务协调。
func resolveDataDir(workDir string) (string, error) {
	if strings.TrimSpace(workDir) != "" {
		return filepath.Clean(workDir), nil
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return "", nil
	}
	// configDir、err 分别是操作系统用户配置根目录及其读取失败原因。
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("读取用户配置目录失败: %w", err)
	}
	return filepath.Join(configDir, userDataDirName), nil
}

// resolveDBPath 封装resolveDB路径业务协调。
func resolveDBPath(dataDir, configuredPath string) string {
	if dataDir != "" && configuredPath == defaultDBPath {
		return filepath.Join(dataDir, "data", "xianyu_data.db")
	}
	return configuredPath
}

// loadOrCreateDataKey 封装loadOrCreate数据Key业务协调。
func loadOrCreateDataKey(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("data key 文件路径不能为空")
	}
	// key、readErr 分别是已存在密钥文件的内容及读取错误；已有文件永远优先于本次生成。
	if key, readErr := readDataKey(path); readErr == nil {
		return key, nil
	} else if errors.Is(readErr, errDataKeyEmpty) {
		return waitForDataKey(path)
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return "", readErr
	}

	// err 表示创建数据加密主密钥父目录失败。
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("创建 data key 目录失败: %w", err)
	}
	// raw 保存高熵随机字节，编码后成为新的数据加密主密钥且不得日志输出。
	raw := make([]byte, 48)
	// err 表示系统随机源无法生成新的数据加密主密钥。
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("生成 data key 失败: %w", err)
	}
	// key 是待写入权限为 0600 文件的数据加密主密钥，禁止记录到日志。
	key := base64.RawStdEncoding.EncodeToString(raw)
	// file、createErr 分别是以排他方式创建的密钥文件及创建错误；同一时刻只能有一个进程成为写入者。
	file, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(createErr, os.ErrExist) {
		return waitForDataKey(path)
	}
	if createErr != nil {
		return "", fmt.Errorf("创建 data key 文件失败: %w", createErr)
	}
	// writeErr 表示新密钥写入错误；失败时删除当前进程独占创建的文件，避免遗留不可恢复的空文件。
	if _, writeErr := io.WriteString(file, key+"\n"); writeErr != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("写入 data key 文件失败: %w", writeErr)
	}
	// syncErr 表示密钥内容持久化到文件系统前发生的同步错误；不能在未落盘时继续启动。
	if syncErr := file.Sync(); syncErr != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("同步 data key 文件失败: %w", syncErr)
	}
	// closeErr 表示关闭新建密钥文件时产生的错误；关闭失败同样视为密钥未可靠创建。
	if closeErr := file.Close(); closeErr != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("关闭 data key 文件失败: %w", closeErr)
	}
	return key, nil
}

// readDataKey 读取并校验已有的数据加密主密钥；返回错误时绝不输出密钥内容。
func readDataKey(path string) (string, error) {
	// raw、readErr 分别是文件原始内容及读取失败原因。
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return "", os.ErrNotExist
		}
		return "", fmt.Errorf("读取 data key 文件失败: %w", readErr)
	}
	// key 是去除文件末尾换行后的数据加密主密钥，禁止记录到日志。
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return "", fmt.Errorf("%w: %s", errDataKeyEmpty, path)
	}
	return key, nil
}

// waitForDataKey 等待并读取由并发启动进程创建的密钥文件，不会生成或覆盖另一把密钥。
func waitForDataKey(path string) (string, error) {
	// lastReadErr 保存最后一次未得到可用密钥的读取错误，用于保留遗留空文件诊断。
	var lastReadErr error
	// attempt 是读取竞争方写入结果的有限尝试次数，避免空文件永久阻塞启动。
	for attempt := 0; attempt < 20; attempt++ {
		// key、readErr 分别是当前尝试读到的密钥及读取错误。
		key, readErr := readDataKey(path)
		if readErr == nil {
			return key, nil
		}
		lastReadErr = readErr
		if !errors.Is(readErr, errDataKeyEmpty) && !errors.Is(readErr, os.ErrNotExist) {
			return "", readErr
		}
		time.Sleep(10 * time.Millisecond)
	}
	if errors.Is(lastReadErr, errDataKeyEmpty) {
		return "", lastReadErr
	}
	return "", fmt.Errorf("等待并发创建 data key 文件超时: %s", path)
}

// isNonLoopbackListenAddress 判断监听地址是否可能接收本机以外的连接；空 host 与通配地址均视为远程可达。
func isNonLoopbackListenAddress(address string) bool {
	// host、port、splitErr 分别是监听地址的主机部分、端口部分和解析错误。
	host, _, splitErr := net.SplitHostPort(strings.TrimSpace(address))
	if splitErr != nil {
		return true
	}
	host = strings.TrimSpace(host)
	if host == "" || strings.EqualFold(host, "localhost") {
		return host == ""
	}
	// ip 是可直接判断 loopback 属性的 IP 地址；主机名按远程可达处理，避免漏报部署风险。
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

// ensureAdmin 封装ensureAdmin业务协调。
func ensureAdmin(ctx context.Context, store *db.Store, email, password string) error {
	if password == "" {
		password = os.Getenv("XIANYU_ADMIN_PASSWORD")
	}
	if password == "" {
		return fmt.Errorf("admin 密码不能为空，请传 -admin-password 或设置 XIANYU_ADMIN_PASSWORD")
	}
	// err 表示管理员初始化或密码重置失败。
	_, err := auth.InitAdmin(ctx, store, email, password)
	return err
}

// ensureAdminIfMissing 封装ensureAdminIfMissing业务协调。
func ensureAdminIfMissing(ctx context.Context, store *db.Store, email, password string) (bool, error) {
	// admin、err 分别是现有管理员记录及其查询失败原因。
	admin, err := store.Users.GetAdmin(ctx)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return false, fmt.Errorf("查询 admin 失败: %w", err)
	}
	if admin != nil {
		return false, nil
	}
	// err 表示首次创建管理员失败。
	if err := ensureAdmin(ctx, store, email, password); err != nil {
		return false, err
	}
	return true, nil
}
