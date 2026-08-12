//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

const windowsServiceAccess = windows.SERVICE_QUERY_STATUS | windows.SERVICE_START | windows.SERVICE_STOP

type windowsServiceController interface {
	state() (uint32, error)
	start() error
	stop() error
	close()
}

type nativeWindowsServiceController struct {
	handle windows.Handle
}

func serviceAction(action string) error {
	if action != "start" && action != "stop" && action != "restart" {
		return fmt.Errorf("未知服务操作: %s", action)
	}

	name := envOr("XIANYU_SERVICE_NAME", "YdisksXianyuHelper")
	controller, err := openWindowsServiceController(name)
	if err != nil {
		if action == "stop" && errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil
		}
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return fmt.Errorf("没有控制 Windows 服务的权限，请重新安装当前版本以更新服务权限")
		}
		return fmt.Errorf("打开 Windows 服务 %s 失败: %w", name, err)
	}
	defer controller.close()

	return controlWindowsService(controller, action, 30*time.Second, 250*time.Millisecond)
}

func openWindowsServiceController(name string) (windowsServiceController, error) {
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return nil, err
	}
	defer windows.CloseServiceHandle(manager) //nolint:errcheck

	namePointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	handle, err := windows.OpenService(manager, namePointer, windowsServiceAccess)
	if err != nil {
		return nil, err
	}
	return &nativeWindowsServiceController{handle: handle}, nil
}

func controlWindowsService(controller windowsServiceController, action string, timeout, pollInterval time.Duration) error {
	switch action {
	case "start":
		return ensureWindowsServiceRunning(controller, timeout, pollInterval)
	case "stop":
		return ensureWindowsServiceStopped(controller, timeout, pollInterval)
	case "restart":
		if err := ensureWindowsServiceStopped(controller, timeout, pollInterval); err != nil {
			return err
		}
		return ensureWindowsServiceRunning(controller, timeout, pollInterval)
	default:
		return fmt.Errorf("未知服务操作: %s", action)
	}
}

func ensureWindowsServiceRunning(controller windowsServiceController, timeout, pollInterval time.Duration) error {
	state, err := controller.state()
	if err != nil {
		return fmt.Errorf("查询 Windows 服务状态失败: %w", err)
	}
	switch state {
	case windows.SERVICE_RUNNING:
		return nil
	case windows.SERVICE_START_PENDING:
		return waitForWindowsServiceState(controller, windows.SERVICE_RUNNING, timeout, pollInterval)
	case windows.SERVICE_STOP_PENDING:
		if err := waitForWindowsServiceState(controller, windows.SERVICE_STOPPED, timeout, pollInterval); err != nil {
			return err
		}
	}

	if err := controller.start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return fmt.Errorf("启动 Windows 服务失败: %w", err)
	}
	return waitForWindowsServiceState(controller, windows.SERVICE_RUNNING, timeout, pollInterval)
}

func ensureWindowsServiceStopped(controller windowsServiceController, timeout, pollInterval time.Duration) error {
	state, err := controller.state()
	if err != nil {
		return fmt.Errorf("查询 Windows 服务状态失败: %w", err)
	}
	switch state {
	case windows.SERVICE_STOPPED:
		return nil
	case windows.SERVICE_STOP_PENDING:
		return waitForWindowsServiceState(controller, windows.SERVICE_STOPPED, timeout, pollInterval)
	case windows.SERVICE_START_PENDING:
		if err := waitForWindowsServiceState(controller, windows.SERVICE_RUNNING, timeout, pollInterval); err != nil {
			return err
		}
	}

	if err := controller.stop(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return fmt.Errorf("停止 Windows 服务失败: %w", err)
	}
	return waitForWindowsServiceState(controller, windows.SERVICE_STOPPED, timeout, pollInterval)
}

func waitForWindowsServiceState(controller windowsServiceController, expected uint32, timeout, pollInterval time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		state, err := controller.state()
		if err != nil {
			return fmt.Errorf("查询 Windows 服务状态失败: %w", err)
		}
		if state == expected {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待 Windows 服务状态 %d 超时，当前状态 %d", expected, state)
		}
		time.Sleep(pollInterval)
	}
}

func (controller *nativeWindowsServiceController) state() (uint32, error) {
	var status windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(controller.handle, &status); err != nil {
		return 0, err
	}
	return status.CurrentState, nil
}

func (controller *nativeWindowsServiceController) start() error {
	return windows.StartService(controller.handle, 0, nil)
}

func (controller *nativeWindowsServiceController) stop() error {
	var status windows.SERVICE_STATUS
	return windows.ControlService(controller.handle, windows.SERVICE_CONTROL_STOP, &status)
}

func (controller *nativeWindowsServiceController) close() {
	_ = windows.CloseServiceHandle(controller.handle)
}

func quitTray() error {
	if err := serviceAction("stop"); err != nil {
		return fmt.Errorf("停止后台服务失败: %w", err)
	}
	return nil
}

func logDirectoryPath() (string, error) {
	base := strings.TrimSpace(os.Getenv("PROGRAMDATA"))
	if base == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("获取 Windows 数据目录失败: %w", err)
		}
	}
	return filepath.Join(base, "YdisksXianyuHelper", "logs"), nil
}
