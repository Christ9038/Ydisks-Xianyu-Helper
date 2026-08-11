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
	systray.SetTitle("闲鱼管家")
	systray.SetIcon(trayIconBytes())
	systray.SetTooltip("闲鱼管家服务")

	statusItem := systray.AddMenuItem("服务状态：检查中", "读取后台服务状态")
	openItem := systray.AddMenuItem("打开管理页面", "在默认浏览器打开管理页面")
	systray.AddSeparator()
	startItem := systray.AddMenuItem("启动服务", "启动后台服务")
	stopItem := systray.AddMenuItem("停止服务", "停止后台服务")
	restartItem := systray.AddMenuItem("重启服务", "重启后台服务")
	browserItem := systray.AddMenuItem("安装/修复 Chromium", "准备 Playwright Chromium")
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
			_ = serviceAction("start")
		}
	}()
	go func() {
		for range stopItem.ClickedCh {
			_ = serviceAction("stop")
		}
	}()
	go func() {
		for range restartItem.ClickedCh {
			_ = serviceAction("restart")
		}
	}()
	go func() {
		for range browserItem.ClickedCh {
			_ = installBrowser()
		}
	}()
	go func() {
		for range quitItem.ClickedCh {
			systray.Quit()
		}
	}()
}

func installBrowser() error {
	path := strings.TrimSpace(os.Getenv("XIANYU_BROWSER_INSTALL"))
	if path == "" {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		path = executable
		if runtime.GOOS == "darwin" {
			path = strings.Replace(path, "/Contents/MacOS/xianyu-tray", "/Contents/Helpers/browser-install", 1)
		} else {
			path = strings.Replace(path, "xianyu-tray.exe", "browser-install.exe", 1)
		}
	}
	return exec.Command(path).Run()
}

func refreshStatus(item *systray.MenuItem) {
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		status, err := readHealth(client)
		if err != nil {
			item.SetTitle("服务状态：未运行")
			systray.SetTooltip("闲鱼管家：后台服务未运行")
		} else if status.Status == "ok" && status.Database == "ok" {
			item.SetTitle("服务状态：运行正常")
			systray.SetTooltip("闲鱼管家：运行正常")
		} else {
			item.SetTitle("服务状态：异常")
			systray.SetTooltip("闲鱼管家：数据库或服务异常")
		}
		time.Sleep(5 * time.Second)
	}
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
