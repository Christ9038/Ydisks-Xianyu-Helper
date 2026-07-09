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
// 浏览器自动化（扫码风控验证、密码登录、订单抓取）内置 playwright-go，
// 首次使用时自动下载 Chromium。加 -no-browser 可禁用。
package main

import (
	"context"
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
	noBrowser := flag.Bool("no-browser", false, "禁用内置浏览器自动化（扫码风控验证/密码登录/订单抓取将不可用）")
	verbose := flag.Bool("v", false, "调试日志")
	initAdmin := flag.Bool("init-admin", false, "初始化或重置 admin 管理员后退出")
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

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

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
	defer database.Close()

	if *initAdmin {
		if err := ensureAdmin(ctx, store, *adminEmail, *adminPassword); err != nil {
			logger.Error("初始化管理员失败", "err", err)
			os.Exit(1)
		}
		logger.Info("管理员初始化完成", "username", "admin")
		return
	}

	// 检查系统是否已初始化。
	if init, _ := store.Users.IsSystemInitialized(ctx); !init {
		logger.Warn("系统尚未初始化，请先运行本二进制的 -init-admin 初始化管理员")
	}

	// 2) 内置浏览器管理器（playwright-go，首运行自动下载 Chromium）。
	var bm *browser.Manager
	if !*noBrowser {
		bm = browser.NewManager(logger)
	}
	var mgr *account.Manager
	ap := adapter.New(store, bm, logger)
	mgr = account.NewManager(store, ap, logger)
	autoCenter := automation.New(store, mgr, logger)
	autoCenter.SetOrderDetailFetcher(ap)
	notifier := notify.New("", store, logger)
	autoCenter.SetNotifier(notifier)
	ap.SetAutomation(autoCenter)
	ap.SetNotifier(notifier)
	if err := mgr.StartAll(ctx); err != nil {
		logger.Error("启动账号引擎失败", "err", err)
	}
	go automation.NewScheduler(autoCenter).Run(ctx)
	go renewal.NewScheduler(store, mgr, bm, ap, logger).Run(ctx)

	// 3) HTTP 服务。
	srv := server.New(store, mgr, bm, *secure, *webDir, *addr, logger, autoCenter, notifier)
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
