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
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"xianyu-go/internal/account"
	"xianyu-go/internal/adapter"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/browser"
	"xianyu-go/internal/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/logging"
	"xianyu-go/internal/notify"
	"xianyu-go/internal/renewal"
	"xianyu-go/internal/server"
)

type serverOptions struct {
	dbPath               string
	dbURL                string
	addr                 string
	webDir               string
	workDir              string
	playwrightDriverDir  string
	playwrightBrowserDir string
	dataKeyFile          string
	secure               bool
	noBrowser            bool
	verbose              bool
	logLevel             string
	logFormat            string
	initAdmin            bool
	ensureAdmin          bool
	adminEmail           string
	adminPassword        string
	service              bool
}

func main() {
	opts := parseOptions()
	if opts.workDir != "" {
		if err := os.Chdir(opts.workDir); err != nil {
			fmt.Fprintf(os.Stderr, "切换工作目录失败: %v\n", err)
			os.Exit(2)
		}
	}

	run := func(ctx context.Context) error { return runServer(ctx, opts) }
	if opts.service {
		if err := runPlatformService("YdisksXianyuHelper", run); err != nil {
			fmt.Fprintf(os.Stderr, "服务运行失败: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx); err != nil {
		slog.Error("服务退出", "err", err)
		os.Exit(1)
	}
}

func parseOptions() serverOptions {
	var opts serverOptions
	flag.StringVar(&opts.dbPath, "db", "data/xianyu_data.db", "SQLite 数据库路径（兼容旧用法）")
	flag.StringVar(&opts.dbURL, "db-url", "", "数据库连接 URL（sqlite:// mysql:// postgres://），优先级高于 -db；也可用 DATABASE_URL 环境变量")
	flag.StringVar(&opts.addr, "addr", ":8080", "HTTP 监听地址")
	flag.StringVar(&opts.webDir, "web", "", "前端静态资源目录（含 index.html）")
	flag.StringVar(&opts.workDir, "workdir", "", "服务工作目录；用于桌面服务固定数据和浏览器目录")
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
	flag.Parse()
	return opts
}

func runServer(parent context.Context, opts serverOptions) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	if opts.playwrightDriverDir != "" {
		if err := os.Setenv("PLAYWRIGHT_DRIVER_PATH", opts.playwrightDriverDir); err != nil {
			return fmt.Errorf("设置 Playwright driver 目录失败: %w", err)
		}
	}
	if opts.playwrightBrowserDir != "" {
		if err := os.Setenv("PLAYWRIGHT_BROWSERS_PATH", opts.playwrightBrowserDir); err != nil {
			return fmt.Errorf("设置 Playwright 浏览器目录失败: %w", err)
		}
	}
	if strings.TrimSpace(os.Getenv("XIANYU_DATA_KEY")) == "" && opts.dataKeyFile != "" {
		key, err := loadOrCreateDataKey(opts.dataKeyFile)
		if err != nil {
			return err
		}
		if err := os.Setenv("XIANYU_DATA_KEY", key); err != nil {
			return fmt.Errorf("设置 XIANYU_DATA_KEY 失败: %w", err)
		}
	}

	resolvedDBURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if resolvedDBURL == "" {
		resolvedDBURL = strings.TrimSpace(opts.dbURL)
	}
	if resolvedDBURL == "" {
		resolvedDBURL = opts.dbPath
	}

	resolvedLogLevel := strings.TrimSpace(os.Getenv("LOG_LEVEL"))
	explicitLogLevel := resolvedLogLevel != ""
	if strings.TrimSpace(opts.logLevel) != "" {
		resolvedLogLevel = strings.TrimSpace(opts.logLevel)
		explicitLogLevel = true
	}
	if resolvedLogLevel == "" && opts.verbose {
		resolvedLogLevel = "debug"
		explicitLogLevel = true
	}
	if err := logging.SetLevel(resolvedLogLevel); err != nil {
		return fmt.Errorf("日志等级无效: %w", err)
	}
	resolvedLogFormat := strings.TrimSpace(os.Getenv("LOG_FORMAT"))
	explicitLogFormat := resolvedLogFormat != ""
	if strings.TrimSpace(opts.logFormat) != "" {
		resolvedLogFormat = strings.TrimSpace(opts.logFormat)
		explicitLogFormat = true
	}
	logger := logging.NewLogger(os.Stdout, resolvedLogFormat)
	slog.SetDefault(logger)

	database, dialect, err := db.Open(ctx, resolvedDBURL)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	defer database.Close()
	logger.Info("数据库已就绪", "dialect", dialect)
	store := db.NewStore(database, dialect)
	if err := store.EncryptLegacySecrets(ctx); err != nil {
		return fmt.Errorf("校验或升级数据库敏感字段失败: %w", err)
	}
	if !explicitLogLevel {
		if lv, err := store.Settings.Get(ctx, "log_level"); err == nil && strings.TrimSpace(lv) != "" {
			if err := logging.SetLevel(lv); err != nil {
				logger.Warn("忽略无效的系统日志设置", "value", lv, "err", err)
			}
		}
	}
	if !explicitLogFormat {
		if format, err := store.Settings.Get(ctx, "log_format"); err == nil && strings.TrimSpace(format) != "" {
			logger = logging.NewLogger(os.Stdout, format)
			slog.SetDefault(logger)
		}
	}

	if opts.initAdmin {
		if err := ensureAdmin(ctx, store, opts.adminEmail, opts.adminPassword); err != nil {
			return fmt.Errorf("初始化管理员失败: %w", err)
		}
		logger.Info("管理员初始化完成", "username", "admin")
		return nil
	}
	if opts.ensureAdmin {
		created, err := ensureAdminIfMissing(ctx, store, opts.adminEmail, opts.adminPassword)
		if err != nil {
			return fmt.Errorf("检查或初始化管理员失败: %w", err)
		}
		if created {
			logger.Info("管理员初始化完成", "username", "admin")
		}
	}

	if init, _ := store.Users.IsSystemInitialized(ctx); !init {
		logger.Warn("系统尚未初始化，请先运行本二进制的 -init-admin 初始化管理员")
	}

	var bm *browser.Manager
	if !opts.noBrowser {
		bm = browser.NewManager(logger)
		if err := bm.Initialize(); err != nil {
			return fmt.Errorf("初始化 Playwright Chromium 指纹失败: %w", err)
		}
	}

	ap := adapter.New(store, bm, logger)
	chatService := chat.New(store)
	ap.SetChatService(chatService)
	mgr := account.NewManager(store, ap, logger)
	autoCenter := automation.New(store, mgr, logger)
	autoCenter.SetOrderDetailFetcher(ap)
	notifier := notify.New("", store, logger)
	notifier.Start(ctx)
	autoCenter.SetNotifier(notifier)
	ap.SetAutomation(autoCenter)
	ap.SetNotifier(notifier)
	if err := mgr.StartAll(ctx); err != nil {
		logger.Error("启动账号引擎失败", "err", err)
	}
	automationScheduler := automation.NewScheduler(autoCenter)
	go automationScheduler.Run(ctx)
	renewalScheduler := renewal.NewScheduler(store, mgr, ap, logger, notifier)
	go renewalScheduler.Run(ctx)

	srv := server.New(store, mgr, opts.secure, opts.webDir, opts.addr, logger, autoCenter, notifier)
	srv.SetChatService(chatService)
	srv.StartPublishBatchRecovery(ctx)
	runErr := srv.Run(ctx)
	if runErr != nil {
		logger.Error("HTTP 服务退出", "err", runErr)
	}
	cancel()
	srv.WaitForBackground()
	automationScheduler.Wait()
	renewalScheduler.Wait()
	mgr.StopAll()
	if bm != nil {
		_ = bm.Close()
	}
	notifier.Wait()
	return runErr
}

func loadOrCreateDataKey(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("data key 文件路径不能为空")
	}
	if raw, err := os.ReadFile(path); err == nil {
		key := strings.TrimSpace(string(raw))
		if key == "" {
			return "", fmt.Errorf("data key 文件为空: %s", path)
		}
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("读取 data key 文件失败: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("创建 data key 目录失败: %w", err)
	}
	raw := make([]byte, 48)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("生成 data key 失败: %w", err)
	}
	key := base64.RawStdEncoding.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("写入 data key 文件失败: %w", err)
	}
	return key, nil
}

func ensureAdmin(ctx context.Context, store *db.Store, email, password string) error {
	if password == "" {
		password = os.Getenv("XIANYU_ADMIN_PASSWORD")
	}
	if password == "" {
		return fmt.Errorf("admin 密码不能为空，请传 -admin-password 或设置 XIANYU_ADMIN_PASSWORD")
	}
	_, err := auth.InitAdmin(ctx, store, email, password)
	return err
}

func ensureAdminIfMissing(ctx context.Context, store *db.Store, email, password string) (bool, error) {
	admin, err := store.Users.GetAdmin(ctx)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return false, fmt.Errorf("查询 admin 失败: %w", err)
	}
	if admin != nil {
		return false, nil
	}
	if err := ensureAdmin(ctx, store, email, password); err != nil {
		return false, err
	}
	return true, nil
}
