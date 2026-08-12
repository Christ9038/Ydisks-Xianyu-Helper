//go:build windows

package main

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func acquireTrayInstance() (release func(), acquired bool, err error) {
	mutexName, err := windows.UTF16PtrFromString(`Local\YdisksXianyuHelperTray`)
	if err != nil {
		return nil, false, fmt.Errorf("生成托盘互斥锁名称失败: %w", err)
	}
	handle, err := windows.CreateMutex(nil, false, mutexName)
	if err == windows.ERROR_ALREADY_EXISTS {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		return func() {}, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("创建托盘互斥锁失败: %w", err)
	}
	return func() { _ = windows.CloseHandle(handle) }, true, nil
}
