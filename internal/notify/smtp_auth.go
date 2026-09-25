package notify

import (
	"fmt"
	"net/smtp"
	"strings"
)

// compatibleSMTPAuth 在单次加密 SMTP 会话内协商 PLAIN 或 LOGIN，不可并发复用。
type compatibleSMTPAuth struct {
	// plain 保留标准库的服务器身份校验及原有 PLAIN 兼容行为。
	plain smtp.Auth
	// username、password 仅用于本次认证，明文凭据不得写入日志或响应。
	username, password string
	// login 表示服务器只提供 LOGIN；step 记录已响应的挑战次数。
	login bool
	step  int
}

// newSMTPAuth 用 username、password 和预期 host 创建会话认证器；凭据仅留在内存。
func newSMTPAuth(username, password, host string) smtp.Auth {
	return &compatibleSMTPAuth{plain: smtp.PlainAuth("", username, password, host), username: username, password: password}
}

// Start 根据 server 的认证声明选择协议，返回机制与初始响应；沿用标准库的 TLS 和主机校验。
func (a *compatibleSMTPAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	a.login, a.step = false, 0
	// mechanism、response 是原有 PLAIN 初始帧；err 表示连接身份或加密条件不满足。
	mechanism, response, err := a.plain.Start(server)
	if err != nil {
		return "", nil, err
	}
	// hasPlain、hasLogin 反映 EHLO 声明，优先保留原有 PLAIN 路径。
	hasPlain, hasLogin := false, false
	// advertised 是服务器声明的一种认证机制。
	for _, advertised := range server.Auth {
		hasPlain = hasPlain || strings.EqualFold(advertised, "PLAIN")
		hasLogin = hasLogin || strings.EqualFold(advertised, "LOGIN")
	}
	if hasLogin && !hasPlain {
		a.login = true
		return "LOGIN", nil, nil
	}
	return mechanism, response, nil
}

// Next 响应服务器 challenge；more 为 false 表示认证结束，多余挑战返回不含凭据的错误。
func (a *compatibleSMTPAuth) Next(challenge []byte, more bool) ([]byte, error) {
	if !a.login {
		return a.plain.Next(challenge, more)
	}
	if !more {
		return nil, nil
	}
	a.step++
	switch a.step {
	case 1:
		return []byte(a.username), nil
	case 2:
		return []byte(a.password), nil
	default:
		return nil, fmt.Errorf("SMTP LOGIN 返回了意外的认证挑战")
	}
}
