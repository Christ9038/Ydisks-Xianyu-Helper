// cmd/server 是闲鱼管家 Go 主进程入口。
// 启动：DB 迁移 → 加载账号引擎 → HTTP API 服务。
//
// 用法：
//
//	go run ./cmd/server -db data/xianyu_data.db -addr :8080 [-web ./static] [-secure] [-no-browser]
//	go run ./cmd/server -db data/xianyu_data.db -init-admin -admin-password '...'
//
// 首次使用可直接用本二进制初始化管理员，不再需要单独 init-admin。
//
// 浏览器自动化（扫码风控验证、滑块、密码登录、搜索、订单抓取）内置 playwright-go，
// 首次使用时自动下载 Chromium，无需手动启动 sidecar。加 -no-browser 可禁用。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"xianyu-go/internal/account"
	"xianyu-go/internal/browser"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/server"
)

func main() {
	dbPath := flag.String("db", "data/xianyu_data.db", "SQLite 数据库路径")
	addr := flag.String("addr", ":8080", "HTTP 监听地址")
	webDir := flag.String("web", "", "前端静态资源目录（含 index.html）")
	secure := flag.Bool("secure", false, "HTTPS 模式（Cookie 加 Secure）")
	noBrowser := flag.Bool("no-browser", false, "禁用内置浏览器自动化（扫码风控验证/滑块/搜索/订单抓取将不可用）")
	verbose := flag.Bool("v", false, "调试日志")
	initAdmin := flag.Bool("init-admin", false, "初始化或重置 admin 管理员后退出")
	adminEmail := flag.String("admin-email", "admin@example.com", "初始化 admin 的邮箱")
	adminPassword := flag.String("admin-password", "", "初始化/重置 admin 密码；也可用 XIANYU_ADMIN_PASSWORD 环境变量")
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 1) DB。
	database, err := db.Open(ctx, *dbPath)
	if err != nil {
		logger.Error("打开数据库失败", "err", err)
		os.Exit(1)
	}
	defer database.Close()
	store := db.NewStore(database)

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
	mgr := account.NewManager(store, &handlerAdapter{store: store, browser: bm, logger: logger}, logger)
	if err := mgr.StartAll(ctx); err != nil {
		logger.Error("启动账号引擎失败", "err", err)
	}

	// 3) HTTP 服务。
	srv := server.New(store, mgr, bm, *secure, *webDir, *addr, logger)
	if err := srv.Run(ctx); err != nil {
		logger.Error("HTTP 服务退出", "err", err)
		os.Exit(1)
	}
}

// handlerAdapter 实现 engine.Handler，把系统消息/密码登录刷新接到浏览器。
// 聊天消息的发货/回复由 Account 内部 DeliveryService/ReplyService 处理。
type handlerAdapter struct {
	store   *db.Store
	browser *browser.Manager
	logger  *slog.Logger

	mu             sync.Mutex
	lastLoginByCID map[string]time.Time
	orderFetchMu   sync.Mutex
	lastOrderFetch time.Time
}

func (h *handlerAdapter) HandleChatMessage(ctx context.Context, m engine.ChatMessage) error {
	return nil
}

func (h *handlerAdapter) HandleSystemMessage(ctx context.Context, m engine.SystemMessage) error {
	h.logger.Info("系统消息", "account", m.AccountID, "reminder", m.RedReminder)
	return nil
}

// Fetch 实现 engine.OrderDetailFetcher。只在本地订单缺少规格时启动浏览器，
// 并将所有账号的详情请求串行化、至少间隔 3 秒，避免短时间高频访问闲鱼。
func (h *handlerAdapter) Fetch(ctx context.Context, cookieID, orderID, itemID, buyerID, cookieStr string) (*engine.OrderDetail, error) {
	if order, err := h.store.Orders.Get(ctx, orderID); err == nil && order != nil && order.Amount != "" && order.Quantity != "" &&
		(!h.store.Items.IsMultiSpec(ctx, cookieID, itemID) || (order.SpecName != "" && order.SpecValue != "")) {
		return &engine.OrderDetail{Quantity: order.Quantity, SpecName: order.SpecName, SpecValue: order.SpecValue, Amount: order.Amount, OrderStatus: order.OrderStatus}, nil
	}
	if h.browser == nil {
		return nil, fmt.Errorf("订单缺少规格信息且浏览器自动化未启用")
	}

	h.orderFetchMu.Lock()
	defer h.orderFetchMu.Unlock()
	// 等锁期间其他流程可能已经补齐订单，再检查一次。
	if order, err := h.store.Orders.Get(ctx, orderID); err == nil && order != nil && order.Amount != "" && order.Quantity != "" &&
		(!h.store.Items.IsMultiSpec(ctx, cookieID, itemID) || (order.SpecName != "" && order.SpecValue != "")) {
		return &engine.OrderDetail{Quantity: order.Quantity, SpecName: order.SpecName, SpecValue: order.SpecValue, Amount: order.Amount, OrderStatus: order.OrderStatus}, nil
	}
	if remain := 3*time.Second - time.Since(h.lastOrderFetch); !h.lastOrderFetch.IsZero() && remain > 0 {
		timer := time.NewTimer(remain)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	h.lastOrderFetch = time.Now()
	detail, err := h.browser.FetchOrderDetail(ctx, orderID, cookieID, cookieStr, h.store.Items.IsMultiSpec(ctx, cookieID, itemID))
	if err != nil {
		return nil, err
	}
	if detail.UpdatedCookies != "" && detail.UpdatedCookies != cookieStr {
		_ = h.store.Cookies.Save(ctx, cookieID, detail.UpdatedCookies, 0)
	}
	return &engine.OrderDetail{
		Quantity: detail.Quantity, SpecName: detail.SpecName, SpecValue: detail.SpecValue,
		Amount: detail.Amount, OrderStatus: detail.OrderStatus,
	}, nil
}

// OnPasswordLoginRefresh 连续失败时调用浏览器密码登录刷新 cookie。
func (h *handlerAdapter) OnPasswordLoginRefresh(ctx context.Context, cookieID string) bool {
	if h.browser == nil {
		h.logger.Warn("密码登录刷新失败：浏览器自动化已禁用")
		return false
	}
	h.mu.Lock()
	if h.lastLoginByCID == nil {
		h.lastLoginByCID = make(map[string]time.Time)
	}
	last := h.lastLoginByCID[cookieID]
	if !last.IsZero() && time.Since(last) < engine.PasswordLoginMinGap {
		remain := engine.PasswordLoginMinGap - time.Since(last)
		h.mu.Unlock()
		h.logger.Warn("密码登录刷新冷却中", "account", cookieID, "remain", remain.Round(time.Second))
		return false
	}
	h.mu.Unlock()

	d, err := h.store.Cookies.GetDetails(ctx, cookieID)
	if err != nil {
		h.logger.Warn("密码登录刷新失败：读取账号详情失败", "account", cookieID, "err", err)
		return false
	}
	if strings.TrimSpace(d.Username) == "" || strings.TrimSpace(d.Password) == "" {
		h.logger.Warn("密码登录刷新失败：账号未配置登录用户名或密码", "account", cookieID)
		return false
	}
	h.mu.Lock()
	h.lastLoginByCID[cookieID] = time.Now()
	h.mu.Unlock()
	cookies, err := h.browser.PasswordLogin(ctx, d.Username, d.Password, cookieID, "", !d.ShowBrowser)
	if err != nil {
		h.logger.Warn("密码登录刷新失败", "account", cookieID, "err", err)
		return false
	}
	cookieStr := browser.MarshalCookies(cookies)
	if strings.TrimSpace(cookieStr) == "" {
		h.logger.Warn("密码登录刷新失败：浏览器未返回 cookie", "account", cookieID)
		return false
	}
	if err := h.store.Cookies.Save(ctx, cookieID, cookieStr, d.UserID); err != nil {
		h.logger.Warn("密码登录刷新失败：保存 cookie 失败", "account", cookieID, "err", err)
		return false
	}
	h.logger.Info("密码登录刷新 cookie 成功", "account", cookieID)
	return true
}

func ensureAdmin(ctx context.Context, store *db.Store, email, password string) error {
	if password == "" {
		password = os.Getenv("XIANYU_ADMIN_PASSWORD")
	}
	if password == "" {
		return fmt.Errorf("admin 密码不能为空，请传 -admin-password 或设置 XIANYU_ADMIN_PASSWORD")
	}
	existing, err := store.Users.GetAdmin(ctx)
	if err != nil && err != db.ErrNotFound {
		return err
	}
	if existing != nil {
		ok, err := store.Users.UpdatePassword(ctx, existing.Username, password)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("admin 用户存在但密码未更新")
		}
		return store.Users.SetAdmin(ctx, existing.Username)
	}
	ok, err := store.Users.Create(ctx, "admin", email, password)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("创建 admin 用户失败：用户名或邮箱可能已存在")
	}
	return store.Users.SetAdmin(ctx, "admin")
}
