package notify

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

// TestSMTPAuthLOGIN 验证仅支持 LOGIN 的服务可完成挑战，拒绝多余帧且重用时重置状态；t 为测试上下文。
func TestSMTPAuthLOGIN(t *testing.T) {
	// auth 使用无真实账号含义的凭据构造单次测试认证器。
	auth := newSMTPAuth("fixture-user", "fixture-password", "mail.example")
	// attempt 覆盖首次协商和状态重置后的第二次协商。
	for attempt := 0; attempt < 2; attempt++ {
		// mechanism、initial、err 分别记录选中机制、初始响应和协商错误。
		mechanism, initial, err := auth.Start(&smtp.ServerInfo{Name: "mail.example", TLS: true, Auth: []string{"login"}})
		if err != nil || mechanism != "LOGIN" || len(initial) != 0 {
			t.Fatal("LOGIN 协商失败")
		}
		// expected 按协议顺序验证用户名和密码挑战，断言失败不输出凭据。
		for _, expected := range []string{"fixture-user", "fixture-password"} {
			// response、nextErr 是本次挑战响应和状态机错误。
			response, nextErr := auth.Next([]byte("challenge"), true)
			if nextErr != nil || string(response) != expected {
				t.Fatal("LOGIN 挑战响应不符合预期")
			}
		}
		// response、nextErr 验证协议结束后不再发送任何凭据。
		response, nextErr := auth.Next(nil, false)
		if nextErr != nil || response != nil {
			t.Fatal("LOGIN 结束响应异常")
		}
		if response, nextErr = auth.Next([]byte("extra"), true); nextErr == nil || response != nil {
			t.Fatal("LOGIN 未拒绝额外挑战")
		}
	}
}

// TestSendEmailLOGINOnly 用本地 SMTP 会话验证生产邮件路径的机制选择及正文投递；t 为测试上下文。
func TestSendEmailLOGINOnly(t *testing.T) {
	// client、server 为无外部网络访问的管道，测试清理关闭连接以释放服务端协程。
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	// done 由服务端唯一写入，测试在发送完成后等待协议校验结果。
	done := make(chan error, 1)
	go func() { done <- serveLOGINFixture(server) }()
	// original 保存原拨号器，当前测试结束时恢复，避免影响其他通知测试。
	original := dialPublicSMTP
	dialPublicSMTP = func(context.Context, string, string, time.Duration) (net.Conn, error) { return client, nil }
	t.Cleanup(func() { dialPublicSMTP = original })
	// cfg 使用虚构账号和独立发件地址；仅在本地管道中关闭 TLS，公网拒绝由安全回归覆盖。
	cfg := map[string]any{
		"smtp_server": "localhost", "smtp_port": "25", "smtp_use_tls": false,
		"smtp_user": "fixture-user", "smtp_password": "fixture-password",
		"smtp_from_address": "sender@example.test", "to_email": "recipient@example.test",
	}
	// err 为完整邮件路径结果，不输出协议中的任何认证数据。
	err := New("fixture", nil, nil).sendEmail(cfg, "LOGIN fixture body")
	if err != nil {
		t.Fatalf("LOGIN 邮件发送失败: %v", err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

// serveLOGINFixture 在 conn 上校验完整 LOGIN/投递对话并返回脱敏错误；截止时间限制协程生命周期。
func serveLOGINFixture(conn net.Conn) error {
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil { // err 为管道截止时间设置错误。
		return err
	}
	// wire 处理 SMTP 行协议与 DATA 点转义，不与其他协程共享。
	wire := textproto.NewConn(conn)
	if err := wire.PrintfLine("220 localhost ESMTP"); err != nil { // err 为模拟服务器欢迎帧写入错误。
		return err
	}
	// step 是当前预期客户端命令和服务器回复；凭据仅使用测试常量。
	for _, step := range [][2]string{
		{"EHLO localhost", "250-localhost\r\n250 AUTH LOGIN"},
		{"AUTH LOGIN", "334 VXNlcm5hbWU6"},
		{base64.StdEncoding.EncodeToString([]byte("fixture-user")), "334 UGFzc3dvcmQ6"},
		{base64.StdEncoding.EncodeToString([]byte("fixture-password")), "235 Authentication successful"},
		{"MAIL FROM:<sender@example.test>", "250 OK"},
		{"RCPT TO:<recipient@example.test>", "250 OK"},
		{"DATA", "354 Send body"},
	} {
		// line、err 是下一条客户端命令及读取错误，错误文本不包含认证帧。
		line, err := wire.ReadLine()
		if err != nil {
			return err
		}
		if line != step[0] {
			return fmt.Errorf("SMTP LOGIN 命令顺序不符合预期")
		}
		if err = wire.PrintfLine("%s", step[1]); err != nil {
			return err
		}
	}
	// body、err 验证 DATA 正文已经实际通过认证后的连接发送。
	body, err := wire.ReadDotBytes()
	if err != nil {
		return err
	}
	if !strings.Contains(string(body), "LOGIN fixture body") {
		return fmt.Errorf("SMTP LOGIN 邮件正文缺失")
	}
	if err = wire.PrintfLine("250 Queued"); err != nil {
		return err
	}
	// quit、err 验证客户端等待接收确认后正常结束会话。
	quit, err := wire.ReadLine()
	if err != nil {
		return err
	}
	if quit != "QUIT" {
		return fmt.Errorf("SMTP LOGIN 会话未正常退出")
	}
	return wire.PrintfLine("221 Bye")
}

// TestSMTPAuthPLAIN 验证已有 PLAIN 服务和无机制声明服务保持原有行为；t 为测试上下文。
func TestSMTPAuthPLAIN(t *testing.T) {
	// advertised 遍历显式 PLAIN、双机制与旧服务器无声明场景。
	for _, advertised := range [][]string{{"PLAIN"}, {"LOGIN", "PLAIN"}, nil} {
		// auth 是此场景独立的认证器。
		auth := newSMTPAuth("fixture-user", "fixture-password", "mail.example")
		// mechanism、initial、err 保存协商结果，不在断言输出敏感帧。
		mechanism, initial, err := auth.Start(&smtp.ServerInfo{Name: "mail.example", TLS: true, Auth: advertised})
		if err != nil || mechanism != "PLAIN" || string(initial) != "\x00fixture-user\x00fixture-password" {
			t.Fatal("PLAIN 兼容行为改变")
		}
		// response、nextErr 验证 PLAIN 正常结束及意外挑战路径。
		response, nextErr := auth.Next(nil, false)
		if nextErr != nil || response != nil {
			t.Fatal("PLAIN 结束异常")
		}
		if response, nextErr = auth.Next([]byte("extra"), true); nextErr == nil || response != nil {
			t.Fatal("PLAIN 未拒绝额外挑战")
		}
	}
}

// TestSMTPAuthRejectsUnsafeConnection 确认新增 LOGIN 支持不削弱 TLS 与目标身份要求；t 为测试上下文。
func TestSMTPAuthRejectsUnsafeConnection(t *testing.T) {
	// server 分别模拟明文远端连接和服务器身份不匹配。
	for _, server := range []*smtp.ServerInfo{
		{Name: "mail.example", TLS: false, Auth: []string{"LOGIN"}},
		{Name: "other.example", TLS: true, Auth: []string{"LOGIN"}},
	} {
		// auth 是隔离的测试认证器；response、err 确认失败不会返回凭据。
		auth := newSMTPAuth("fixture-user", "fixture-password", "mail.example")
		// response、err 保存拒绝不安全连接的结果，禁止返回任何认证明文。
		_, response, err := auth.Start(server)
		if err == nil || len(response) != 0 {
			t.Fatal("不安全连接未被拒绝")
		}
	}
}
