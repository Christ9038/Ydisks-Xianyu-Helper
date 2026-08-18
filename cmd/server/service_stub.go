//go:build !windows

package main

import (
	"context"
	"fmt"
)

// runPlatformService 封装运行PlatformService业务协调。
func runPlatformService(_ string, _ func(context.Context) error) error {
	return fmt.Errorf("当前平台不支持 -service；请使用系统服务管理器启动普通进程")
}
