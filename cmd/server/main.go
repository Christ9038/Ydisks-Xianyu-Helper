// Package main 是闲鱼管家 Go 主进程入口。
// 启动：DB 迁移 → 加载账号引擎 → HTTP API 服务。
//
// 用法：
//
//	go run ./cmd/server -db data/xianyu_data.db -addr :8080 [-web ./static] [-secure] [-no-browser]
//	go run ./cmd/server -db data/xianyu_data.db -init-admin -admin-password '...'
//
// 首次使用可直接用本二进制初始化管理员，不再需要单独 init-admin。
//
// Chromium 仅用于读取本机浏览器指纹和处理 token 滑块，首次使用时由
// playwright-go 准备运行环境。加 -no-browser 可禁用这两项能力。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"xianyu-go/internal/account"
	"xianyu-go/internal/adapter"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/browser"
	"xianyu-go/internal/db"
	"xianyu-go/internal/logging"
	"xianyu-go/internal/notify"
	"xianyu-go/internal/renewal"
	"xianyu-go/internal/server"
)

func main() {
	dbPath := flag.String("db", "data/xianyu_data.db", "SQLite 数据库路径（兼容旧用法）")
	dbURL := flag.String("db-url", "", "数据库连接 URL（sqlite:// mysql:// postgres://），优先级高于 -db；也可用 DATABASE_URL 环境变量")
	addr := flag.String("addr", ":8080", "HTTP 监听地址")
	webDir := flag.String("web", "", "前端静态资源目录（含 index.html）")
	secure := flag.Bool("secure", false, "HTTPS 模式（Cookie 加 Secure）")
	noBrowser := flag.Bool("no-browser", false, "禁用 Chromium（本机浏览器指纹读取和 token 滑块自动处理将不可用）")
	verbose := flag.Bool("v", false, "调试日志")
	logLevel := flag.String("log-level", "", "日志等级：debug/info/warn/error；默认读取 LOG_LEVEL 或系统设置")
	logFormat := flag.String("log-format", "", "日志格式：text/json；默认读取 LOG_FORMAT")
	initAdmin := flag.Bool("init-admin", false, "初始化或重置 admin 管理员后退出")
	ensureAdminOnStart := flag.Bool("ensure-admin", false, "仅在 admin 管理员不存在时初始化；已存在时不修改密码")
	adminEmail := flag.String("admin-email", "admin@example.com", "初始化 admin 的邮箱")
	adminPassword := flag.String("admin-password", "", "初始化/重置 admin 密码；也可用 XIANYU_ADMIN_PASSWORD 环境变量")
	flag.Parse()

	// 解析数据库连接：DATABASE_URL > -db-url > -db（SQLite 路径，向后兼容）。
	resolvedDBURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if resolvedDBURL == "" {
		resolvedDBURL = strings.TrimSpace(*dbURL)
	}
	if resolvedDBURL == "" {
		resolvedDBURL = *dbPath
	}

	resolvedLogLevel := strings.TrimSpace(os.Getenv("LOG_LEVEL"))
	explicitLogLevel := resolvedLogLevel != ""
	if strings.TrimSpace(*logLevel) != "" {
		resolvedLogLevel = strings.TrimSpace(*logLevel)
		explicitLogLevel = true
	}
	if resolvedLogLevel == "" && *verbose {
		resolvedLogLevel = "debug"
		explicitLogLevel = true
	}
	if err := logging.SetLevel(resolvedLogLevel); err != nil {
		fmt.Fprintf(os.Stderr, "日志等级无效: %v\n", err)
		os.Exit(2)
	}
	resolvedLogFormat := strings.TrimSpace(os.Getenv("LOG_FORMAT"))
	explicitLogFormat := resolvedLogFormat != ""
	if strings.TrimSpace(*logFormat) != "" {
		resolvedLogFormat = strings.TrimSpace(*logFormat)
		explicitLogFormat = true
	}
	logger := logging.NewLogger(os.Stdout, resolvedLogFormat)
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 1) DB。
	database, dialect, err := db.Open(ctx, resolvedDBURL)
	if err != nil {
		logger.Error("打开数据库失败", "err", err)
		os.Exit(1)
	}
	logger.Info("数据库已就绪", "dialect", dialect)
	store := db.NewStore(database, dialect)
	if err := store.EncryptLegacySecrets(ctx); err != nil {
		logger.Error("校验或升级数据库敏感字段失败", "err", err)
		os.Exit(1)
	}
	defer database.Close()
	if !explicitLogLevel {
		if lv, err := store.Settings.Get(ctx, "log_level"); err == nil && strings.TrimSpace(lv) != "" {
			if err := logging.SetLevel(lv); err != nil {
				logger.Warn("忽略无效的系统日志等级设置", "value", lv, "err", err)
			}
		}
	}
	if !explicitLogFormat {
		if format, err := store.Settings.Get(ctx, "log_format"); err == nil && strings.TrimSpace(format) != "" {
			logger = logging.NewLogger(os.Stdout, format)
			slog.SetDefault(logger)
		}
	}

	if *initAdmin {
		if err := ensureAdmin(ctx, store, *adminEmail, *adminPassword); err != nil {
			logger.Error("初始化管理员失败", "err", err)
			os.Exit(1)
		}
		logger.Info("管理员初始化完成", "username", "admin")
		return
	}
	if *ensureAdminOnStart {
		created, err := ensureAdminIfMissing(ctx, store, *adminEmail, *adminPassword)
		if err != nil {
			logger.Error("检查或初始化管理员失败", "err", err)
			os.Exit(1)
		}
		if created {
			logger.Info("管理员初始化完成", "username", "admin")
		}
	}

	// 检查系统是否已初始化。
	if init, _ := store.Users.IsSystemInitialized(ctx); !init {
		logger.Warn("系统尚未初始化，请先运行本二进制的 -init-admin 初始化管理员")
	}

	// 2) Chromium 只负责读取本机浏览器指纹和处理 token 滑块。
	// 扫码、人脸、续期、MTOP、Cookie 与 WebSocket 全部走 Go 协议实现。
	var bm *browser.Manager
	if !*noBrowser {
		bm = browser.NewManager(logger)
		if err := bm.Initialize(); err != nil {
			logger.Error("初始化 Playwright Chromium 指纹失败", "err", err)
			os.Exit(1)
		}
	}
	var mgr *account.Manager
	ap := adapter.New(store, bm, logger)
	mgr = account.NewManager(store, ap, logger)
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
	go automation.NewScheduler(autoCenter).Run(ctx)
	go renewal.NewScheduler(store, mgr, ap, logger).Run(ctx)

	// 3) HTTP 服务。
	srv := server.New(store, mgr, *secure, *webDir, *addr, logger, autoCenter, notifier)
	go srv.RunPublishBatchRecovery(ctx)
	if err := srv.Run(ctx); err != nil {
		logger.Error("HTTP 服务退出", "err", err)
		// 即便出错也尝试清理已启动的账号与浏览器。
		mgr.StopAll()
		if bm != nil {
			_ = bm.Close()
		}
		os.Exit(1)
	}
	// HTTP 服务正常退出（ctx 取消触发 Shutdown）：停账号引擎、关浏览器。
	mgr.StopAll()
	if bm != nil {
		_ = bm.Close()
	}
	notifier.Wait()
}

// handlerAdapter 已下沉到 internal/adapter 包；本文件只负责装配。

// ensureAdmin 解析密码后委托 auth.InitAdmin 完成创建或重置。
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

// ensureAdminIfMissing 只为首次部署创建管理员，避免容器重启重置既有管理员密码。
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
