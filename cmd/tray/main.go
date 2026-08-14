// Command tray provides the small desktop controller for the background server.
// The server remains a separate process managed by the operating system.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"fyne.io/systray"
)

// healthResponse 保存health响应，供当前处理流程使用
type healthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
	Version  string `json:"version"`
	Commit   string `json:"commit"`
}

// serviceURL 保存serviceURL，供当前处理流程使用
var serviceURL = strings.TrimRight(envOr("XIANYU_SERVICE_URL", "http://127.0.0.1:59188"), "/")

// actionMu 保存动作Mu，供当前处理流程使用
var (
	actionMu      sync.Mutex
	transitioning atomic.Bool
	exitRequested atomic.Bool
)

// main 负责main相关处理。
func main() {
	// closeLog 保存closeLog，供当前处理流程使用
	closeLog := configureTrayLogger()
	defer closeLog()
	// releaseInstance、acquired、err 保存releaseInstance、acquired、err，供当前处理流程使用
	releaseInstance, acquired, err := acquireTrayInstance()
	if err != nil {
		log.Printf("获取托盘单实例锁失败: %v", err)
		return
	}
	if !acquired {
		log.Printf("托盘已经运行，本次启动直接退出")
		return
	}
	defer releaseInstance()
	log.Printf("托盘启动，PID=%d", os.Getpid())
	watchTerminationSignals()
	systray.Run(onReady, onTrayExit)

	// systray 的退出回调运行在 Cocoa/系统托盘事件循环中，不能在回调
	// 内同步执行 launchctl，否则 macOS 可能把 LaunchAgent 标记为已退出，
	// 但实际托盘进程仍然存活。等事件循环真正返回后再处理外部退出。
	if !exitRequested.Load() {
		log.Printf("托盘事件循环结束，清理后台服务")
		if // err 保存err，供当前处理流程使用
		err := quitTray(); err != nil {
			log.Printf("清理后台服务失败: %v", err)
		} else if // err 保存err，供当前处理流程使用
		err := waitForService(&http.Client{Timeout: 2 * time.Second}, false, 30*time.Second); err != nil {
			log.Printf("等待后台服务退出失败: %v", err)
		}
	}
	log.Printf("托盘退出")
}

// watchTerminationSignals 负责watchTerminationSignals相关处理。
func watchTerminationSignals() {
	// signals 保存signals，供当前处理流程使用
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		// reason 保存原因，供当前处理流程使用
		reason := <-signals
		log.Printf("收到进程终止信号: %s", reason)

		actionMu.Lock()
		// actionMu 保证不会与菜单动作并发；拿到锁时其它动作已经完成。
		transitioning.Store(true)
		if // err 保存err，供当前处理流程使用
		err := quitTray(); err != nil {
			log.Printf("终止前停止后台服务失败: %v", err)
		} else if // err 保存err，供当前处理流程使用
		err := waitForService(&http.Client{Timeout: 2 * time.Second}, false, 30*time.Second); err != nil {
			log.Printf("终止前等待后台服务退出失败: %v", err)
		}
		transitioning.Store(false)
		actionMu.Unlock()
		signal.Stop(signals)
		os.Exit(0)
	}()
}

// onTrayExit 负责onTrayExit相关处理。
func onTrayExit() {
	// 这里只记录事件。服务清理由 main 在 systray.Run 返回后执行，避免
	// 阻塞原生托盘事件循环。
	log.Printf("收到托盘退出事件")
}

// configureTrayLogger 负责configureTrayLogger相关处理。
func configureTrayLogger() func() {
	// directory、err 保存directory、err，供当前处理流程使用
	directory, err := logDirectoryPath()
	if err != nil {
		return func() {}
	}
	if // err 保存err，供当前处理流程使用
	err := os.MkdirAll(directory, 0o755); err != nil {
		return func() {}
	}
	// path 保存路径，供当前处理流程使用
	path := filepath.Join(directory, "tray.log")
	// file、err 保存file、err，供当前处理流程使用
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return func() {}
	}
	log.SetOutput(file)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	return func() { _ = file.Close() }
}

// onReady 负责onReady相关处理。
func onReady() {
	systray.SetTitle("")
	systray.SetIcon(trayIconBytes(false))
	systray.SetTooltip("Ydisks闲鱼助手服务")

	// statusItem 保存状态商品，供当前处理流程使用
	statusItem := systray.AddMenuItem("服务状态：检查中", "读取后台服务状态")
	// openItem 保存open商品，供当前处理流程使用
	openItem := systray.AddMenuItem("打开管理页面", "在默认浏览器打开管理页面")
	// logItem 保存log商品，供当前处理流程使用
	logItem := systray.AddMenuItem("打开日志目录", "打开后台服务和托盘日志所在目录")
	systray.AddSeparator()
	// startItem 保存开始商品，供当前处理流程使用
	startItem := systray.AddMenuItem("启动服务", "启动后台服务")
	// stopItem 保存stop商品，供当前处理流程使用
	stopItem := systray.AddMenuItem("停止服务", "停止后台服务")
	// restartItem 保存restart商品，供当前处理流程使用
	restartItem := systray.AddMenuItem("重启服务", "重启后台服务")
	systray.AddSeparator()
	// quitItem 保存quit商品，供当前处理流程使用
	quitItem := systray.AddMenuItem("退出托盘", "停止后台服务并退出菜单栏控制器")

	// 安装脚本只负责注册平台服务。用户再次打开托盘时，托盘本身必须确保
	// 后台服务已启动。首次启动动作完成后再进入轮询，避免“检查中”的旧请求
	// 覆盖“启动中”等中间状态。
	go func() {
		runServiceAction(statusItem, "启动服务", "启动中", "start", startItem, stopItem, restartItem, quitItem)
		refreshStatus(statusItem)
	}()
	go func() {
		// 局部数据 表示当前遍历过程中的局部数据
		for range openItem.ClickedCh {
			_ = openURL(serviceURL)
		}
	}()
	go func() {
		// 局部数据 表示当前遍历过程中的局部数据
		for range logItem.ClickedCh {
			if // err 保存err，供当前处理流程使用
			err := openLogDirectory(); err != nil {
				statusItem.SetTitle("日志目录：打开失败")
				systray.SetTooltip(fmt.Sprintf("Ydisks闲鱼助手：打开日志目录失败：%v", err))
			}
		}
	}()
	go func() {
		// 局部数据 表示当前遍历过程中的局部数据
		for range startItem.ClickedCh {
			runServiceAction(statusItem, "启动服务", "启动中", "start", startItem, stopItem, restartItem, quitItem)
		}
	}()
	go func() {
		// 局部数据 表示当前遍历过程中的局部数据
		for range stopItem.ClickedCh {
			runServiceAction(statusItem, "停止服务", "正在停止", "stop", startItem, stopItem, restartItem, quitItem)
		}
	}()
	go func() {
		// 局部数据 表示当前遍历过程中的局部数据
		for range restartItem.ClickedCh {
			runServiceAction(statusItem, "重启服务", "正在重启", "restart", startItem, stopItem, restartItem, quitItem)
		}
	}()
	go func() {
		// 局部数据 表示当前遍历过程中的局部数据
		for range quitItem.ClickedCh {
			exitTray(statusItem, startItem, stopItem, restartItem, quitItem)
		}
	}()
}

// refreshStatus 负责refresh状态相关处理。
func refreshStatus(item *systray.MenuItem) {
	// client 保存client，供当前处理流程使用
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		// 与菜单动作使用同一把锁，避免一次已经开始的健康检查在“启动中”
		// 或“正在停止”之后才返回，并把中间状态错误覆盖掉。
		actionMu.Lock()
		if !transitioning.Load() {
			refreshStatusOnce(item, client)
		}
		actionMu.Unlock()
		time.Sleep(5 * time.Second)
	}
}

// refreshStatusOnce 负责refresh状态Once相关处理。
func refreshStatusOnce(item *systray.MenuItem, client *http.Client) {
	// status、err 保存status、err，供当前处理流程使用
	status, err := readHealth(client)
	if err != nil {
		systray.SetIcon(trayIconBytes(false))
		item.SetTitle("服务状态：未运行")
		systray.SetTooltip("Ydisks闲鱼助手：后台服务未运行")
	} else if status.Status == "ok" && status.Database == "ok" {
		systray.SetIcon(trayIconBytes(true))
		item.SetTitle("服务状态：运行正常")
		systray.SetTooltip("Ydisks闲鱼助手：运行正常")
	} else {
		systray.SetIcon(trayIconBytes(false))
		item.SetTitle("服务状态：异常")
		systray.SetTooltip("Ydisks闲鱼助手：数据库或服务异常")
	}
}

// runServiceAction 负责运行Service动作相关处理。
func runServiceAction(statusItem *systray.MenuItem, actionName, transitionTitle, action string, actionItems ...*systray.MenuItem) {
	actionMu.Lock()
	defer actionMu.Unlock()
	if transitioning.Swap(true) {
		return
	}
	defer transitioning.Store(false)
	log.Printf("开始执行服务操作: %s", actionName)
	setMenuItemsDisabled(true, actionItems...)
	defer setMenuItemsDisabled(false, actionItems...)

	systray.SetIcon(trayIconBytes(false))
	statusItem.SetTitle("服务状态：" + transitionTitle)
	systray.SetTooltip("Ydisks闲鱼助手：" + transitionTitle)
	// client 保存client，供当前处理流程使用
	client := &http.Client{Timeout: 2 * time.Second}
	if action == "start" {
		if // health、err 保存health、err，供当前处理流程使用
		health, err := readHealth(client); err == nil && health.Status == "ok" && health.Database == "ok" {
			statusItem.SetTitle("服务状态：运行正常")
			systray.SetIcon(trayIconBytes(true))
			systray.SetTooltip("Ydisks闲鱼助手：运行正常")
			return
		}
	}
	if // err 保存err，供当前处理流程使用
	err := serviceAction(action); err != nil {
		log.Printf("服务操作失败: %s: %v", actionName, err)
		statusItem.SetTitle("服务状态：操作失败")
		systray.SetTooltip(fmt.Sprintf("Ydisks闲鱼助手：%s失败：%v", actionName, err))
		return
	}
	// wantRunning 保存wantRunning，供当前处理流程使用
	wantRunning := action != "stop"
	if // err 保存err，供当前处理流程使用
	err := waitForService(client, wantRunning, 30*time.Second); err != nil {
		log.Printf("服务操作状态确认失败: %s: %v", actionName, err)
		statusItem.SetTitle("服务状态：操作失败")
		systray.SetTooltip(fmt.Sprintf("Ydisks闲鱼助手：%s后状态确认失败：%v", actionName, err))
		return
	}
	if wantRunning {
		statusItem.SetTitle("服务状态：运行正常")
		systray.SetIcon(trayIconBytes(true))
		systray.SetTooltip("Ydisks闲鱼助手：运行正常")
	} else {
		statusItem.SetTitle("服务状态：未运行")
		systray.SetTooltip("Ydisks闲鱼助手：后台服务已停止")
	}
	log.Printf("服务操作完成: %s", actionName)
}

// exitTray 负责exitTray相关处理。
func exitTray(statusItem *systray.MenuItem, actionItems ...*systray.MenuItem) {
	actionMu.Lock()
	defer actionMu.Unlock()
	if transitioning.Swap(true) {
		return
	}
	defer transitioning.Store(false)
	log.Printf("开始退出托盘")
	setMenuItemsDisabled(true, actionItems...)

	systray.SetIcon(trayIconBytes(false))
	statusItem.SetTitle("托盘状态：正在退出")
	systray.SetTooltip("Ydisks闲鱼助手：正在停止后台服务")
	if // err 保存err，供当前处理流程使用
	err := quitTray(); err != nil {
		log.Printf("退出托盘停止服务失败: %v", err)
		statusItem.SetTitle("托盘状态：退出失败")
		systray.SetTooltip(fmt.Sprintf("Ydisks闲鱼助手：后台服务停止失败：%v", err))
		setMenuItemsDisabled(false, actionItems...)
		return
	}
	if // err 保存err，供当前处理流程使用
	err := waitForService(&http.Client{Timeout: 2 * time.Second}, false, 30*time.Second); err != nil {
		log.Printf("退出托盘等待服务停止失败: %v", err)
		statusItem.SetTitle("托盘状态：退出失败")
		systray.SetTooltip(fmt.Sprintf("Ydisks闲鱼助手：后台服务仍未退出：%v", err))
		setMenuItemsDisabled(false, actionItems...)
		return
	}
	systray.SetTooltip("Ydisks闲鱼助手：后台服务已退出")
	exitRequested.Store(true)
	systray.Quit()
}

// setMenuItemsDisabled 负责setMenu商品列表Disabled相关处理。
func setMenuItemsDisabled(disabled bool, items ...*systray.MenuItem) {
	// item 表示当前遍历过程中的商品
	for _, item := range items {
		if disabled {
			item.Disable()
		} else {
			item.Enable()
		}
	}
}

// waitForService 负责waitForService相关处理。
func waitForService(client *http.Client, wantRunning bool, timeout time.Duration) error {
	// deadline 保存deadline，供当前处理流程使用
	deadline := time.Now().Add(timeout)
	for {
		// health、reachable、err 保存health、reachable、err，供当前处理流程使用
		health, reachable, err := probeHealth(client)
		// running 保存running，供当前处理流程使用
		running := reachable && err == nil && health.Status == "ok" && health.Database == "ok"
		if wantRunning && running {
			return nil
		}
		// “已停止”要求监听端口确实不可达。服务仍能返回异常状态或非 2xx
		// 响应时，进程仍然存在，不能让托盘提前退出。
		if !wantRunning && !reachable {
			return nil
		}
		if time.Now().After(deadline) {
			if wantRunning {
				return fmt.Errorf("等待后台服务启动超时")
			}
			return fmt.Errorf("等待后台服务退出超时")
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// readHealth 负责readHealth相关处理。
func readHealth(client *http.Client) (healthResponse, error) {
	// health、err 保存health、err，供当前处理流程使用
	health, _, err := probeHealth(client)
	return health, err
}

// probeHealth 负责probeHealth相关处理。
func probeHealth(client *http.Client) (healthResponse, bool, error) {
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := client.Get(serviceURL + "/health")
	if err != nil {
		return healthResponse{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return healthResponse{}, true, fmt.Errorf("health status %d", resp.StatusCode)
	}
	// health 保存health，供当前处理流程使用
	var health healthResponse
	if // err 保存err，供当前处理流程使用
	err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return healthResponse{}, true, err
	}
	return health, true, nil
}

// openURL 负责openURL相关处理。
func openURL(url string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
	return exec.Command("open", url).Start()
}

// openLogDirectory 负责openLogDirectory相关处理。
func openLogDirectory() error {
	// directory、err 保存directory、err，供当前处理流程使用
	directory, err := logDirectoryPath()
	if err != nil {
		return err
	}
	if // err 保存err，供当前处理流程使用
	err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer.exe", directory).Start()
	case "darwin":
		return exec.Command("open", directory).Start()
	default:
		return exec.Command("xdg-open", directory).Start()
	}
}

// envOr 负责envOr相关处理。
func envOr(name, fallback string) string {
	if // value 保存值，供当前处理流程使用
	value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
