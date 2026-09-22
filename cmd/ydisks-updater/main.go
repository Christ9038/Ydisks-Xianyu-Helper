// Package main 启动闲鱼助手宿主机受限 Docker 更新 daemon。
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"xianyu-go/internal/updater"
)

// main 只启动固定生产目录的 updater，不接受外部命令参数或路径参数。
func main() {
	// ctx 在 systemd 停止服务或宿主机发送终止信号时结束 daemon 生命周期。
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	// daemon 保存固定 Compose、数据库和官方 Release 规则。
	daemon := updater.NewDaemon()
	if /* serveErr 是 updater daemon 监听或收束失败原因。 */ serveErr := daemon.Run(ctx); serveErr != nil {
		slog.Error("更新 daemon 退出", "err", serveErr)
		os.Exit(1)
	}
}
