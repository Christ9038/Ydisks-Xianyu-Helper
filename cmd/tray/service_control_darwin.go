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

// serviceAction 负责service动作相关处理。
func serviceAction(action string) error {
	// label 保存label，供当前处理流程使用
	label := envOr("XIANYU_SERVICE_NAME", "com.ydisks.xianyu-helper.server")
	// uid 保存uid，供当前处理流程使用
	uid := fmt.Sprint(os.Getuid())
	// domain 保存domain，供当前处理流程使用
	domain := "gui/" + uid
	// target 保存target，供当前处理流程使用
	target := domain + "/" + label
	// home、err 保存home、err，供当前处理流程使用
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户目录失败: %w", err)
	}
	// plistPath 保存plist路径，供当前处理流程使用
	plistPath := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	switch action {
	case "start":
		if // err 保存err，供当前处理流程使用
		err := launchctl("print", target); err == nil {
			if // err 保存err，供当前处理流程使用
			err := launchctl("kickstart", target); err == nil {
				return nil
			}
			// launchd 可能仍保留一个正在退出的旧 job；先完整卸载，
			// 等它消失后再 bootstrap，避免第二次启动报 Input/output error。
			_ = launchctl("bootout", target)
			if // err 保存err，供当前处理流程使用
			err := waitForLaunchctlGone(target, 10*time.Second); err != nil {
				return err
			}
		}
		if // err 保存err，供当前处理流程使用
		err := launchctl("bootstrap", domain, plistPath); err != nil {
			return err
		}
		return launchctl("kickstart", target)
	case "stop":
		if // err 保存err，供当前处理流程使用
		err := launchctl("print", target); err != nil {
			return nil
		}
		if // err 保存err，供当前处理流程使用
		err := launchctl("bootout", target); err != nil {
			return err
		}
		return waitForLaunchctlGone(target, 10*time.Second)
	case "restart":
		_ = launchctl("bootout", target)
		if // err 保存err，供当前处理流程使用
		err := waitForLaunchctlGone(target, 10*time.Second); err != nil {
			return err
		}
		if // err 保存err，供当前处理流程使用
		err := launchctl("bootstrap", domain, plistPath); err != nil {
			return err
		}
		return launchctl("kickstart", target)
	default:
		return fmt.Errorf("未知服务操作: %s", action)
	}
}

// waitForLaunchctlGone 负责waitForLaunchctlGone相关处理。
func waitForLaunchctlGone(target string, timeout time.Duration) error {
	// deadline 保存deadline，供当前处理流程使用
	deadline := time.Now().Add(timeout)
	for {
		if // err 保存err，供当前处理流程使用
		err := launchctl("print", target); err != nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待 LaunchAgent 退出超时: %s", target)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// quitTray 负责quitTray相关处理。
func quitTray() error {
	if // err 保存err，供当前处理流程使用
	err := serviceAction("stop"); err != nil {
		return fmt.Errorf("停止后台服务失败: %w", err)
	}
	// 不要从托盘进程内部 bootout 自己的 LaunchAgent。launchctl 可能会先
	// 卸载 job 再留下当前进程，导致旧托盘残留而新托盘无法正确接管。
	// KeepAlive=false，随后由 systray.Quit() 正常退出进程即可。
	return nil
}

// logDirectoryPath 负责logDirectory路径相关处理。
func logDirectoryPath() (string, error) {
	// home、err 保存home、err，供当前处理流程使用
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户目录失败: %w", err)
	}
	return filepath.Join(home, "Library", "Logs", "YdisksXianyuHelper"), nil
}

// launchctl 负责launchctl相关处理。
func launchctl(args ...string) error {
	// cmd 保存cmd，供当前处理流程使用
	cmd := exec.Command("launchctl", args...)
	// output、err 保存output、err，供当前处理流程使用
	output, err := cmd.CombinedOutput()
	if err != nil {
		// message 保存消息，供当前处理流程使用
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("launchctl %s 失败: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("launchctl %s 失败: %s", strings.Join(args, " "), message)
	}
	return nil
}
