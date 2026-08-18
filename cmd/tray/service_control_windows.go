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

// windowsServiceAccess 用于本次流程后续判断的windowsServiceAccess
const windowsServiceAccess = windows.SERVICE_QUERY_STATUS | windows.SERVICE_START | windows.SERVICE_STOP

// windowsServiceController 用于本次流程后续判断的windowsService请求取消控制器
type windowsServiceController interface {
	state() (uint32, error)
	start() error
	stop() error
	close()
}

// nativeWindowsServiceController 用于本次流程后续判断的nativeWindowsService请求取消控制器
type nativeWindowsServiceController struct {
	handle windows.Handle
}

// serviceAction 封装service动作业务协调。
func serviceAction(action string) error {
	if action != "start" && action != "stop" && action != "restart" {
		return fmt.Errorf("未知服务操作: %s", action)
	}

	// name 用于本次流程后续判断的名称
	name := envOr("XIANYU_SERVICE_NAME", "YdisksXianyuHelper")
	// controller、err 用于本次流程后续判断的controller、err
	controller, err := openWindowsServiceController(name)
	if err != nil {
		if action == "stop" && errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil
		}
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return fmt.Errorf("Windows 服务 %s 尚未安装，请重新运行安装器", name)
		}
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return fmt.Errorf("没有控制 Windows 服务的权限，请重新安装当前版本以更新服务权限")
		}
		return fmt.Errorf("打开 Windows 服务 %s 失败: %w", name, err)
	}
	defer controller.close()

	return controlWindowsService(controller, action, 30*time.Second, 250*time.Millisecond)
}

// openWindowsServiceController 封装openWindowsService请求取消控制器业务协调。
func openWindowsServiceController(name string) (windowsServiceController, error) {
	// manager、err 用于本次流程后续判断的manager、err
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return nil, err
	}
	defer windows.CloseServiceHandle(manager) //nolint:errcheck

	// namePointer、err 用于本次流程后续判断的名称Pointer、err
	namePointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	// handle、err 用于本次流程后续判断的handle、err
	handle, err := windows.OpenService(manager, namePointer, windowsServiceAccess)
	if err != nil {
		return nil, err
	}
	return &nativeWindowsServiceController{handle: handle}, nil
}

// controlWindowsService 封装controlWindowsService业务协调。
func controlWindowsService(controller windowsServiceController, action string, timeout, pollInterval time.Duration) error {
	switch action {
	case "start":
		return ensureWindowsServiceRunning(controller, timeout, pollInterval)
	case "stop":
		return ensureWindowsServiceStopped(controller, timeout, pollInterval)
	case "restart":
		if // err 用于本次流程后续判断的err
		err := ensureWindowsServiceStopped(controller, timeout, pollInterval); err != nil {
			return err
		}
		return ensureWindowsServiceRunning(controller, timeout, pollInterval)
	default:
		return fmt.Errorf("未知服务操作: %s", action)
	}
}

// ensureWindowsServiceRunning 封装ensureWindowsServiceRunning业务协调。
func ensureWindowsServiceRunning(controller windowsServiceController, timeout, pollInterval time.Duration) error {
	// state、err 用于本次流程后续判断的state、err
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
		if // err 用于本次流程后续判断的err
		err := waitForWindowsServiceState(controller, windows.SERVICE_STOPPED, timeout, pollInterval); err != nil {
			return err
		}
	}

	if // err 用于本次流程后续判断的err
	err := controller.start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return fmt.Errorf("启动 Windows 服务失败: %w", err)
	}
	return waitForWindowsServiceState(controller, windows.SERVICE_RUNNING, timeout, pollInterval)
}

// ensureWindowsServiceStopped 封装ensureWindowsServiceStopped业务协调。
func ensureWindowsServiceStopped(controller windowsServiceController, timeout, pollInterval time.Duration) error {
	// state、err 用于本次流程后续判断的state、err
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
		if // err 用于本次流程后续判断的err
		err := waitForWindowsServiceState(controller, windows.SERVICE_RUNNING, timeout, pollInterval); err != nil {
			return err
		}
	}

	if // err 用于本次流程后续判断的err
	err := controller.stop(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return fmt.Errorf("停止 Windows 服务失败: %w", err)
	}
	return waitForWindowsServiceState(controller, windows.SERVICE_STOPPED, timeout, pollInterval)
}

// waitForWindowsServiceState 封装waitForWindowsService状态业务协调。
func waitForWindowsServiceState(controller windowsServiceController, expected uint32, timeout, pollInterval time.Duration) error {
	// deadline 用于本次流程后续判断的deadline
	deadline := time.Now().Add(timeout)
	for {
		// state、err 用于本次流程后续判断的state、err
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

// state 封装状态业务协调。
func (controller *nativeWindowsServiceController) state() (uint32, error) {
	// status 用于本次流程后续判断的状态
	var status windows.SERVICE_STATUS
	if // err 用于本次流程后续判断的err
	err := windows.QueryServiceStatus(controller.handle, &status); err != nil {
		return 0, err
	}
	return status.CurrentState, nil
}

// start 封装开始业务协调。
func (controller *nativeWindowsServiceController) start() error {
	return windows.StartService(controller.handle, 0, nil)
}

// stop 封装stop业务协调。
func (controller *nativeWindowsServiceController) stop() error {
	// status 用于本次流程后续判断的状态
	var status windows.SERVICE_STATUS
	return windows.ControlService(controller.handle, windows.SERVICE_CONTROL_STOP, &status)
}

// close 封装close业务协调。
func (controller *nativeWindowsServiceController) close() {
	_ = windows.CloseServiceHandle(controller.handle)
}

// quitTray 封装quitTray业务协调。
func quitTray() error {
	if // err 用于本次流程后续判断的err
	err := serviceAction("stop"); err != nil {
		return fmt.Errorf("停止后台服务失败: %w", err)
	}
	return nil
}

// logDirectoryPath 封装logDirectory路径业务协调。
func logDirectoryPath() (string, error) {
	// base 用于本次流程后续判断的base
	base := strings.TrimSpace(os.Getenv("PROGRAMDATA"))
	if base == "" {
		// err 用于本次流程后续判断的err
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("获取 Windows 数据目录失败: %w", err)
		}
	}
	return filepath.Join(base, "YdisksXianyuHelper", "logs"), nil
}
