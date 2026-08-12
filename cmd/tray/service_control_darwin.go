//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func serviceAction(action string) error {
	label := envOr("XIANYU_SERVICE_NAME", "com.ydisks.xianyu-helper.server")
	uid := fmt.Sprint(os.Getuid())
	domain := "gui/" + uid
	target := domain + "/" + label
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户目录失败: %w", err)
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	switch action {
	case "start":
		if err := launchctl("print", target); err == nil {
			if err := launchctl("kickstart", target); err == nil {
				return nil
			}
			// launchd 可能仍保留一个正在退出的旧 job；先完整卸载，
			// 等它消失后再 bootstrap，避免第二次启动报 Input/output error。
			_ = launchctl("bootout", target)
			if err := waitForLaunchctlGone(target, 10*time.Second); err != nil {
				return err
			}
		}
		if err := launchctl("bootstrap", domain, plistPath); err != nil {
			return err
		}
		return launchctl("kickstart", target)
	case "stop":
		if err := launchctl("print", target); err != nil {
			return nil
		}
		if err := launchctl("bootout", target); err != nil {
			return err
		}
		return waitForLaunchctlGone(target, 10*time.Second)
	case "restart":
		_ = launchctl("bootout", target)
		if err := waitForLaunchctlGone(target, 10*time.Second); err != nil {
			return err
		}
		if err := launchctl("bootstrap", domain, plistPath); err != nil {
			return err
		}
		return launchctl("kickstart", target)
	default:
		return fmt.Errorf("未知服务操作: %s", action)
	}
}

func waitForLaunchctlGone(target string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := launchctl("print", target); err != nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待 LaunchAgent 退出超时: %s", target)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func quitTray() error {
	if err := serviceAction("stop"); err != nil {
		return fmt.Errorf("停止后台服务失败: %w", err)
	}
	// 不要从托盘进程内部 bootout 自己的 LaunchAgent。launchctl 可能会先
	// 卸载 job 再留下当前进程，导致旧托盘残留而新托盘无法正确接管。
	// KeepAlive=false，随后由 systray.Quit() 正常退出进程即可。
	return nil
}

func logDirectoryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户目录失败: %w", err)
	}
	return filepath.Join(home, "Library", "Logs", "YdisksXianyuHelper"), nil
}

func launchctl(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("launchctl %s 失败: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("launchctl %s 失败: %s", strings.Join(args, " "), message)
	}
	return nil
}
