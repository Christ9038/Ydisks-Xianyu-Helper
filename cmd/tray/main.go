// Command tray provides the small desktop controller for the background server.
// The server remains a separate process managed by the operating system.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"fyne.io/systray"
)

type healthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
}

var serviceURL = strings.TrimRight(envOr("XIANYU_SERVICE_URL", "http://127.0.0.1:8080"), "/")

func main() {
	systray.Run(onReady, func() {})
}

func onReady() {
	systray.SetTitle("")
	systray.SetIcon(trayIconBytes(false))
	systray.SetTooltip("Ydisks闲鱼助手服务")

	statusItem := systray.AddMenuItem("服务状态：检查中", "读取后台服务状态")
	openItem := systray.AddMenuItem("打开管理页面", "在默认浏览器打开管理页面")
	systray.AddSeparator()
	startItem := systray.AddMenuItem("启动服务", "启动后台服务")
	stopItem := systray.AddMenuItem("停止服务", "停止后台服务")
	restartItem := systray.AddMenuItem("重启服务", "重启后台服务")
	systray.AddSeparator()
	quitItem := systray.AddMenuItem("退出托盘", "只退出菜单栏控制器")

	go refreshStatus(statusItem)
	go func() {
		for range openItem.ClickedCh {
			_ = openURL(serviceURL)
		}
	}()
	go func() {
		for range startItem.ClickedCh {
			runServiceAction(statusItem, "启动服务", "start")
		}
	}()
	go func() {
		for range stopItem.ClickedCh {
			runServiceAction(statusItem, "停止服务", "stop")
		}
	}()
	go func() {
		for range restartItem.ClickedCh {
			runServiceAction(statusItem, "重启服务", "restart")
		}
	}()
	go func() {
		for range quitItem.ClickedCh {
			if err := quitTray(); err != nil {
				statusItem.SetTitle("托盘状态：退出失败")
				systray.SetTooltip(fmt.Sprintf("Ydisks闲鱼助手：托盘退出失败：%v", err))
				continue
			}
			systray.Quit()
		}
	}()
}

func refreshStatus(item *systray.MenuItem) {
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		refreshStatusOnce(item, client)
		time.Sleep(5 * time.Second)
	}
}

func refreshStatusOnce(item *systray.MenuItem, client *http.Client) {
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

func runServiceAction(statusItem *systray.MenuItem, actionName, action string) {
	systray.SetIcon(trayIconBytes(false))
	statusItem.SetTitle("服务状态：" + actionName + "中")
	if err := serviceAction(action); err != nil {
		statusItem.SetTitle("服务状态：操作失败")
		systray.SetTooltip(fmt.Sprintf("Ydisks闲鱼助手：%s失败：%v", actionName, err))
		return
	}

	statusItem.SetTitle("服务状态：检查中")
}

func readHealth(client *http.Client) (healthResponse, error) {
	resp, err := client.Get(serviceURL + "/health")
	if err != nil {
		return healthResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return healthResponse{}, fmt.Errorf("health status %d", resp.StatusCode)
	}
	var health healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return healthResponse{}, err
	}
	return health, nil
}

func openURL(url string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
	return exec.Command("open", url).Start()
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
