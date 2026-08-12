//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func acquireTrayInstance() (release func(), acquired bool, err error) {
	cacheDirectory, err := os.UserCacheDir()
	if err != nil {
		return nil, false, fmt.Errorf("获取用户缓存目录失败: %w", err)
	}
	lockDirectory := filepath.Join(cacheDirectory, "YdisksXianyuHelper")
	if err := os.MkdirAll(lockDirectory, 0o755); err != nil {
		return nil, false, fmt.Errorf("创建托盘锁目录失败: %w", err)
	}

	return acquireTrayFileLock(filepath.Join(lockDirectory, "tray.lock"))
}

func acquireTrayFileLock(lockPath string) (release func(), acquired bool, err error) {
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("打开托盘锁文件失败: %w", err)
	}
	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lockFile.Close()
		if err == unix.EWOULDBLOCK || err == unix.EAGAIN {
			return func() {}, false, nil
		}
		return nil, false, fmt.Errorf("锁定托盘实例失败: %w", err)
	}

	return func() {
		_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
		_ = lockFile.Close()
	}, true, nil
}
