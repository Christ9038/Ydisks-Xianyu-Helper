// Package main 是闲鱼管家 Go 主进程入口。
// 启动：DB 迁移 → 加载账号引擎 → HTTP API 服务。
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"xianyu-go/internal/account"
	"xianyu-go/internal/adapter"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/browser"
	"xianyu-go/internal/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/logging"
	"xianyu-go/internal/logsafe"
	"xianyu-go/internal/notify"
	"xianyu-go/internal/renewal"
	"xianyu-go/internal/server"
	appversion "xianyu-go/internal/version"
)

// serverOptions 保存serverOptions，供当前处理流程使用
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

// defaultDBPath 保存defaultDB路径，供当前处理流程使用
const (
	defaultDBPath      = "data/xianyu_data.db"
	userDataDirName    = "YdisksXianyuHelper"
	defaultDataKeyName = "data-key"
)

// main 负责main相关处理。
func main() {
	// opts 保存opts，供当前处理流程使用
	opts := parseOptions()
	if opts.showVersion {
		fmt.Printf("Ydisks Xianyu Helper %s (commit %s, built %s)\n", appversion.Version, appversion.ShortCommit(), appversion.BuildTime)
		return
	}
	if opts.workDir != "" {
		if // err 保存err，供当前处理流程使用
		err := os.Chdir(opts.workDir); err != nil {
			fmt.Fprintf(os.Stderr, "切换工作目录失败: %s\n", logsafe.Error(err))
			os.Exit(2)
		}
	}

	// run 保存运行，供当前处理流程使用
	run := func(ctx context.Context) error { return runServer(ctx, opts) }
	if opts.service {
		if // err 保存err，供当前处理流程使用
		err := runPlatformService("YdisksXianyuHelper", run); err != nil {
			fmt.Fprintf(os.Stderr, "服务运行失败: %s\n", logsafe.Error(err))
			os.Exit(1)
		}
		return
	}

	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if // err 保存err，供当前处理流程使用
	err := run(ctx); err != nil {
		slog.Error("服务退出", "err", logsafe.Error(err))
		os.Exit(1)
	}
}

// parseOptions 负责parseOptions相关处理。
func parseOptions() serverOptions {
	// opts 保存opts，供当前处理流程使用
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

// runServer 负责运行Server相关处理。
func runServer(parent context.Context, opts serverOptions) error {
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	// dataDir、err 保存数据Dir、err，供当前处理流程使用
	dataDir, err := resolveDataDir(opts.workDir)
	if err != nil {
		return err
	}
	// packagedPlaywrightRuntime 保存packagedPlaywrightRuntime，供当前处理流程使用
	packagedPlaywrightRuntime := strings.TrimSpace(opts.playwrightRuntimeRoot) != ""
	applyPlaywrightRuntimeRoot(&opts)
	if dataDir != "" {
		if // err 保存err，供当前处理流程使用
		err := os.MkdirAll(dataDir, 0o700); err != nil {
			return fmt.Errorf("创建应用数据目录失败: %w", err)
		}
		if opts.dataKeyFile == "" {
			opts.dataKeyFile = filepath.Join(dataDir, defaultDataKeyName)
		}
		// 只有安装包显式提供 runtime root 时，才把 Playwright runtime
		// 放进应用数据目录。普通本地开发应沿用 Playwright 的用户缓存；
		// 空的应用目录会让 playwright-go 误以为 driver 已配置，随后尝试
		// 执行不存在的 <dataDir>/playwright-driver/node。
		if packagedPlaywrightRuntime && opts.playwrightDriverDir == "" {
			opts.playwrightDriverDir = filepath.Join(dataDir, "playwright-driver")
		}
		if packagedPlaywrightRuntime && opts.playwrightBrowserDir == "" {
			opts.playwrightBrowserDir = filepath.Join(dataDir, "playwright-browsers")
		}
	}

	if opts.playwrightDriverDir != "" {
		if // err 保存err，供当前处理流程使用
		err := os.Setenv("PLAYWRIGHT_DRIVER_PATH", opts.playwrightDriverDir); err != nil {
			return fmt.Errorf("设置 Playwright driver 目录失败: %w", err)
		}
	}
	if opts.playwrightBrowserDir != "" {
		if // err 保存err，供当前处理流程使用
		err := os.Setenv("PLAYWRIGHT_BROWSERS_PATH", opts.playwrightBrowserDir); err != nil {
			return fmt.Errorf("设置 Playwright 浏览器目录失败: %w", err)
		}
	}
	if strings.TrimSpace(os.Getenv("XIANYU_DATA_KEY")) == "" && opts.dataKeyFile != "" {
		// key、err 保存key、err，供当前处理流程使用
		key, err := loadOrCreateDataKey(opts.dataKeyFile)
		if err != nil {
			return err
		}
		if // err 保存err，供当前处理流程使用
		err := os.Setenv("XIANYU_DATA_KEY", key); err != nil {
			return fmt.Errorf("设置 XIANYU_DATA_KEY 失败: %w", err)
		}
	}

	// resolvedDBURL 保存resolvedDBURL，供当前处理流程使用
	resolvedDBURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if resolvedDBURL == "" {
		resolvedDBURL = strings.TrimSpace(opts.dbURL)
	}
	if resolvedDBURL == "" {
		resolvedDBURL = resolveDBPath(dataDir, opts.dbPath)
	}
	if dataDir != "" && resolvedDBURL == resolveDBPath(dataDir, defaultDBPath) {
		if // err 保存err，供当前处理流程使用
		err := os.MkdirAll(filepath.Dir(resolvedDBURL), 0o700); err != nil {
			return fmt.Errorf("创建数据库目录失败: %w", err)
		}
	}

	// resolvedLogLevel 保存resolvedLogLevel，供当前处理流程使用
	resolvedLogLevel := strings.TrimSpace(os.Getenv("LOG_LEVEL"))
	// explicitLogLevel 保存explicitLogLevel，供当前处理流程使用
	explicitLogLevel := resolvedLogLevel != ""
	if strings.TrimSpace(opts.logLevel) != "" {
		resolvedLogLevel = strings.TrimSpace(opts.logLevel)
		explicitLogLevel = true
	}
	if resolvedLogLevel == "" && opts.verbose {
		resolvedLogLevel = "debug"
		explicitLogLevel = true
	}
	if // err 保存err，供当前处理流程使用
	err := logging.SetLevel(resolvedLogLevel); err != nil {
		return fmt.Errorf("日志等级无效: %w", err)
	}
	// resolvedLogFormat 保存resolvedLogFormat，供当前处理流程使用
	resolvedLogFormat := strings.TrimSpace(os.Getenv("LOG_FORMAT"))
	// explicitLogFormat 保存explicitLogFormat，供当前处理流程使用
	explicitLogFormat := resolvedLogFormat != ""
	if strings.TrimSpace(opts.logFormat) != "" {
		resolvedLogFormat = strings.TrimSpace(opts.logFormat)
		explicitLogFormat = true
	}
	// logWriter、closeLog、err 保存logWriter、closeLog、err，供当前处理流程使用
	logWriter, closeLog, err := openServerLogWriter(dataDir)
	if err != nil {
		return err
	}
	defer closeLog()
	// logger 保存logger，供当前处理流程使用
	logger := logging.NewLogger(logWriter, resolvedLogFormat)
	slog.SetDefault(logger)

	// database、dialect、err 保存database、dialect、err，供当前处理流程使用
	database, dialect, err := db.Open(ctx, resolvedDBURL)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	defer database.Close()
	logger.Info("数据库已就绪", "dialect", dialect)
	// store 保存store，供当前处理流程使用
	store := db.NewStore(database, dialect)
	if // err 保存err，供当前处理流程使用
	err := store.EncryptLegacySecrets(ctx); err != nil {
		return fmt.Errorf("校验或升级数据库敏感字段失败: %w", err)
	}
	if !explicitLogLevel {
		if // lv、err 保存lv、err，供当前处理流程使用
		lv, err := store.Settings.Get(ctx, "log_level"); err == nil && strings.TrimSpace(lv) != "" {
			if // err 保存err，供当前处理流程使用
			err := logging.SetLevel(lv); err != nil {
				logger.Warn("忽略无效的系统日志设置", "value", lv, "err", err)
			}
		}
	}
	if !explicitLogFormat {
		if // format、err 保存format、err，供当前处理流程使用
		format, err := store.Settings.Get(ctx, "log_format"); err == nil && strings.TrimSpace(format) != "" {
			logger = logging.NewLogger(logWriter, format)
			slog.SetDefault(logger)
		}
	}

	if opts.initAdmin {
		if // err 保存err，供当前处理流程使用
		err := ensureAdmin(ctx, store, opts.adminEmail, opts.adminPassword); err != nil {
			return fmt.Errorf("初始化管理员失败: %w", err)
		}
		logger.Info("管理员初始化完成", "username", "admin")
		return nil
	}
	if opts.ensureAdmin {
		// created、err 保存created、err，供当前处理流程使用
		created, err := ensureAdminIfMissing(ctx, store, opts.adminEmail, opts.adminPassword)
		if err != nil {
			return fmt.Errorf("检查或初始化管理员失败: %w", err)
		}
		if created {
			logger.Info("管理员初始化完成", "username", "admin")
		}
	}

	if // init 保存init，供当前处理流程使用
	init, _ := store.Users.IsSystemInitialized(ctx); !init {
		logger.Warn("系统尚未初始化，请先运行本二进制的 -init-admin 初始化管理员")
	}

	// bm 保存bm，供当前处理流程使用
	var bm *browser.Manager
	if !opts.noBrowser {
		bm = browser.NewManager(logger)
		if // err 保存err，供当前处理流程使用
		err := bm.Initialize(); err != nil {
			return fmt.Errorf("初始化 Playwright Chromium 指纹失败: %w", err)
		}
	}

	// ap 保存ap，供当前处理流程使用
	ap := adapter.New(store, bm, logger)
	// chatService 保存聊天Service，供当前处理流程使用
	chatService := chat.New(store)
	ap.SetChatService(chatService)
	// mgr 保存mgr，供当前处理流程使用
	mgr := account.NewManager(store, ap, logger)
	// autoCenter 保存autoCenter，供当前处理流程使用
	autoCenter := automation.New(store, mgr, logger)
	autoCenter.SetOrderDetailFetcher(ap)
	// notifier 保存notifier，供当前处理流程使用
	notifier := notify.New("", store, logger)
	notifier.Start(ctx)
	autoCenter.SetNotifier(notifier)
	ap.SetAutomation(autoCenter)
	ap.SetNotifier(notifier)
	if // err 保存err，供当前处理流程使用
	err := mgr.StartAll(ctx); err != nil {
		logger.Error("启动账号引擎失败", "err", err)
	}
	// automationScheduler 保存自动化Scheduler，供当前处理流程使用
	automationScheduler := automation.NewScheduler(autoCenter)
	go automationScheduler.Run(ctx)
	// renewalScheduler 保存renewalScheduler，供当前处理流程使用
	renewalScheduler := renewal.NewScheduler(store, mgr, ap, logger, notifier)
	go renewalScheduler.Run(ctx)

	// srv 是完成依赖校验并注入聊天服务后的 HTTP 应用实例。
	srv, err := server.New(store, mgr, opts.secure, opts.webDir, opts.addr, logger, autoCenter, notifier, server.WithChatService(chatService))
	if err != nil {
		return fmt.Errorf("构造 HTTP 服务失败: %w", err)
	}
	srv.StartPublishBatchRecovery(ctx)
	srv.StartOrderReconciliationRecovery(ctx)
	srv.StartOrderRefreshRecovery(ctx)
	// err 是 HTTP 服务显式启动失败的原因。
	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("启动 HTTP 服务失败: %w", err)
	}
	// runErr 是 HTTP 监听退出时返回的错误。
	runErr := srv.Wait()
	if runErr != nil {
		logger.Error("HTTP 服务退出", "err", runErr)
	}
	cancel()
	// stopCtx 是关闭 HTTP 服务及其后台 worker 的有限等待上下文。
	// stopCancel 释放 stopCtx 的定时器资源。
	// stopCtx、stopCancel 保存stopCtx、stop取消，供当前处理流程使用
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	_ = srv.Stop(stopCtx)
	// err 表示自动化调度器在关闭上下文内未完成退出的错误。
	if err := automationScheduler.WaitContext(stopCtx); err != nil {
		logger.Warn("自动化调度器关闭未完成", "err", err)
	}
	// err 表示续期调度器在关闭上下文内未完成退出的错误。
	if err := renewalScheduler.WaitContext(stopCtx); err != nil {
		logger.Warn("续期调度器关闭未完成", "err", err)
	}
	// err 表示账号运行时未能在关闭上下文内完整停止的错误。
	if err := mgr.StopAllContext(stopCtx); err != nil {
		logger.Warn("账号运行时关闭未完成", "err", err)
	}
	if bm != nil {
		_ = bm.Close()
	}
	// err 表示通知 worker 在关闭上下文内未完成退出的错误。
	if err := notifier.WaitContext(stopCtx); err != nil {
		logger.Warn("通知 worker 关闭未完成", "err", err)
	}
	stopCancel()
	return runErr
}

// openServerLogWriter keeps container and interactive runs on stdout while
// desktop/system-service installations persist logs in their platform log
// directory. Windows services do not have a useful console, so they get a
// default log file beside the service data directory.
// openServerLogWriter 负责openServerLogWriter相关处理。
func openServerLogWriter(dataDir string) (io.Writer, func(), error) {
	// logDir 保存logDir，供当前处理流程使用
	logDir := strings.TrimSpace(os.Getenv("XIANYU_LOG_DIR"))
	if logDir == "" && runtime.GOOS == "windows" && dataDir != "" {
		logDir = filepath.Join(dataDir, "logs")
	}
	if logDir == "" {
		return os.Stdout, func() {}, nil
	}
	if // err 保存err，供当前处理流程使用
	err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("创建日志目录失败: %w", err)
	}
	// logPath 保存log路径，供当前处理流程使用
	logPath := filepath.Join(logDir, "server.log")
	// file、err 保存file、err，供当前处理流程使用
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("打开日志文件失败: %w", err)
	}
	return file, func() { _ = file.Close() }, nil
}

// applyPlaywrightRuntimeRoot 负责applyPlaywrightRuntimeRoot相关处理。
func applyPlaywrightRuntimeRoot(opts *serverOptions) {
	if opts == nil {
		return
	}
	// root 保存root，供当前处理流程使用
	root := strings.TrimSpace(opts.playwrightRuntimeRoot)
	if root == "" {
		return
	}
	// archRoot 保存archRoot，供当前处理流程使用
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
// resolveDataDir 负责resolve数据Dir相关处理。
func resolveDataDir(workDir string) (string, error) {
	if strings.TrimSpace(workDir) != "" {
		return filepath.Clean(workDir), nil
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return "", nil
	}
	// configDir、err 保存配置Dir、err，供当前处理流程使用
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("读取用户配置目录失败: %w", err)
	}
	return filepath.Join(configDir, userDataDirName), nil
}

// resolveDBPath 负责resolveDB路径相关处理。
func resolveDBPath(dataDir, configuredPath string) string {
	if dataDir != "" && configuredPath == defaultDBPath {
		return filepath.Join(dataDir, "data", "xianyu_data.db")
	}
	return configuredPath
}

// loadOrCreateDataKey 负责loadOrCreate数据Key相关处理。
func loadOrCreateDataKey(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("data key 文件路径不能为空")
	}
	if // raw、err 保存raw、err，供当前处理流程使用
	raw, err := os.ReadFile(path); err == nil {
		// key 保存key，供当前处理流程使用
		key := strings.TrimSpace(string(raw))
		if key == "" {
			return "", fmt.Errorf("data key 文件为空: %s", path)
		}
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("读取 data key 文件失败: %w", err)
	}

	if // err 保存err，供当前处理流程使用
	err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("创建 data key 目录失败: %w", err)
	}
	// raw 保存原始，供当前处理流程使用
	raw := make([]byte, 48)
	if // err 保存err，供当前处理流程使用
	_, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("生成 data key 失败: %w", err)
	}
	// key 保存key，供当前处理流程使用
	key := base64.RawStdEncoding.EncodeToString(raw)
	if // err 保存err，供当前处理流程使用
	err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("写入 data key 文件失败: %w", err)
	}
	return key, nil
}

// ensureAdmin 负责ensureAdmin相关处理。
func ensureAdmin(ctx context.Context, store *db.Store, email, password string) error {
	if password == "" {
		password = os.Getenv("XIANYU_ADMIN_PASSWORD")
	}
	if password == "" {
		return fmt.Errorf("admin 密码不能为空，请传 -admin-password 或设置 XIANYU_ADMIN_PASSWORD")
	}
	// err 保存err，供当前处理流程使用
	_, err := auth.InitAdmin(ctx, store, email, password)
	return err
}

// ensureAdminIfMissing 负责ensureAdminIfMissing相关处理。
func ensureAdminIfMissing(ctx context.Context, store *db.Store, email, password string) (bool, error) {
	// admin、err 保存admin、err，供当前处理流程使用
	admin, err := store.Users.GetAdmin(ctx)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return false, fmt.Errorf("查询 admin 失败: %w", err)
	}
	if admin != nil {
		return false, nil
	}
	if // err 保存err，供当前处理流程使用
	err := ensureAdmin(ctx, store, email, password); err != nil {
		return false, err
	}
	return true, nil
}
