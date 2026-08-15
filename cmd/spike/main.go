//go:build debug || tools

// spike 是 Phase 0 协议可行性验证程序（go/no-go 闸门）。
//
// 串起单账号完整链路：
//
//	cookie → mtop token API（签名）→ accessToken → WS 连接 → /reg 注册 → 心跳 → 收消息 → ACK → 解密
//
// 用法：
//
//	export XIANYU_COOKIE='unb=...; _m_h5_tk=...; cookie2=...; ...'
//	go run -tags debug ./cmd/spike
//
// 或从文件读取（避免 cookie 出现在进程列表）：
//
//	go run -tags debug ./cmd/spike -cookie-file /path/to/cookie.txt
//
// 真机验证成功（能收到并解密一条闲鱼消息）即视为 Phase 0 通过。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"xianyu-go/internal/logsafe"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/protocol"
	"xianyu-go/internal/xianyu/ws"
)

// main 负责main相关处理。
func main() {
	// cookieFile 保存登录凭证文件，供当前处理流程使用
	cookieFile := flag.String("cookie-file", "", "从文件读取 cookie（首行）")
	// verbose 保存verbose，供当前处理流程使用
	verbose := flag.Bool("v", false, "调试日志")
	flag.Parse()

	// logger 保存logger，供当前处理流程使用
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if *verbose {
		logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}

	// cookieStr 保存登录凭证Str，供当前处理流程使用
	cookieStr := strings.TrimSpace(os.Getenv("XIANYU_COOKIE"))
	if *cookieFile != "" {
		// b、err 保存b、err，供当前处理流程使用
		b, err := os.ReadFile(*cookieFile)
		if err != nil {
			logger.Error("读取 cookie 文件失败", "err", logsafe.Error(err))
			os.Exit(1)
		}
		cookieStr = strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
	}
	if cookieStr == "" {
		fmt.Fprintln(os.Stderr, "缺少 cookie：请设置 XIANYU_COOKIE 环境变量或用 -cookie-file 指定")
		os.Exit(2)
	}

	// 基本校验：必须有 unb 和 _m_h5_tk。
	c := protocol.TransCookies(cookieStr)
	if c["unb"] == "" || c["_m_h5_tk"] == "" {
		fmt.Fprintln(os.Stderr, "cookie 缺少 unb 或 _m_h5_tk 字段")
		os.Exit(2)
	}
	logger.Info("cookie 已加载", "account_hash", logsafe.ID(c["unb"]), "fields", len(c))

	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 1) mtop token API → accessToken。
	// mc 保存mc，供当前处理流程使用
	mc := mtop.NewClient()
	logger.Info("步骤 1/3：刷新 token")
	// res、err 保存res、err，供当前处理流程使用
	res, err := mc.RefreshToken(cookieStr)
	if err != nil {
		logger.Error("刷新 token 失败", "err", logsafe.Error(err))
		os.Exit(1)
	}
	logger.Info("token 刷新成功", "accessToken_len", len(res.AccessToken))

	// 2) WS 连接 + 注册。
	deviceID := protocol.GenerateDeviceID(c["unb"])
	// cfg 保存cfg，供当前处理流程使用
	cfg := ws.Config{
		CookieStr:   res.UpdatedCookies,
		DeviceID:    deviceID,
		AccessToken: res.AccessToken,
	}
	logger.Info("步骤 2/3：连接 WebSocket", "device_id", deviceID)
	// conn、err 保存conn、err，供当前处理流程使用
	conn, err := ws.Dial(ctx, cfg, logger)
	if err != nil {
		logger.Error("WS 连接/注册失败", "err", logsafe.Error(err))
		os.Exit(1)
	}
	defer conn.Close()

	// 3) 心跳 + 收消息。
	logger.Info("步骤 3/3：心跳与消息接收（等待闲鱼消息…）")
	go func() {
		if // err 保存err，供当前处理流程使用
		err := conn.HeartbeatLoop(ctx, 15*time.Second); err != nil {
			logger.Error("心跳循环退出", "err", logsafe.Error(err))
			cancel()
		}
	}()

	// gotMessage 保存got消息，供当前处理流程使用
	gotMessage := false
	err = conn.ReceiveLoop(ctx, func(decrypted map[string]any) {
		gotMessage = true
		// fieldNames 保存消息顶层字段名；只输出结构摘要，避免把消息正文或凭证带入终端。
		fieldNames := diagnosticFieldNames(decrypted)
		logger.Info("✅ 成功收到并解密消息，Phase 0 闸门通过（正文已省略）", "field_count", len(fieldNames), "fields", fieldNames)
		cancel()
	})
	if !gotMessage {
		logger.Error("未收到消息即退出", "err", logsafe.Error(err))
		os.Exit(1)
	}
}

// diagnosticFieldNames 返回消息顶层字段的排序列表，用于安全诊断而不暴露消息值。
func diagnosticFieldNames(message map[string]any) []string {
	// fields 保存消息字段名；字段值不会被复制到诊断输出。
	fields := make([]string, 0, len(message))
	// fieldName 表示当前消息的顶层字段名。
	for fieldName := range message {
		fields = append(fields, fieldName)
	}
	sort.Strings(fields)
	return fields
}
