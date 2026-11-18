package renewal

import (
	"fmt"

	apirenew "xianyu-go/internal/xianyu/renew"
)

// securityVerificationEvent 是“闲鱼要求安全验证”通知类别，与账号运行时、Token 风控共用同一订阅项。
const securityVerificationEvent = "security_verification"

// VerificationRegistry 定义调度器登记“续期被要求安全验证”的账号所需的最小能力，由消费方定义。
// 实现方必须并发安全；返回 true 表示这是一次新的验证要求（此前没有未过期登记），应当通知用户，
// 返回 false 表示同一要求仍在有效期内，不应重复通知。登记内容含一次性验证地址，只能存放在内存中。
type VerificationRegistry interface {
	// Register 登记账号需要人工验证；verificationURL 可为空，仅用于通知去重。
	Register(accountID, verificationURL string) bool
}

// SetVerificationRegistry 设置可选的待验证登记表，必须在 Run 之前由组合根调用一次。
// 这是可选运行配置而不是必需依赖：未设置时续期遇到安全验证仍会通知，只是提示用户重新扫码且无法去重。
func (s *Scheduler) SetVerificationRegistry(registry VerificationRegistry) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.verifications = registry
}

// verificationRegistry 返回当前登记表；mu 只保护该字段的读写，不跨越任何外部 I/O。
func (s *Scheduler) verificationRegistry() VerificationRegistry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.verifications
}

// noteVerificationRequired 在续期结果为 iframeRedirect 中间状态时登记并通知用户。
// 响应 Cookie 已由调用方按原有流程保存，本函数不再改动凭证；通知正文不含平台验证地址，
// 因为通知会经过第三方渠道而地址带一次性令牌。同一账号在登记有效期内只通知一次。
// 本函数不发起任何 MTOP 或平台请求。
func (s *Scheduler) noteVerificationRequired(accountID string, result *apirenew.Result) {
	if s == nil || result == nil || !result.VerificationRequired {
		return
	}
	// registry 是可选的待验证登记表；hasChannel 表示用户是否可以在管理页直接完成验证。
	registry := s.verificationRegistry()
	// hasChannel 保存 result.VerificationURL 的结果，供后续分支和断言使用。
	hasChannel := registry != nil && result.VerificationURL() != ""
	// isNew 表示这是否为一次新的验证要求；没有登记表时无法去重，按新要求处理。
	isNew := true
	if registry != nil {
		isNew = registry.Register(accountID, result.VerificationURL())
	}
	if !isNew {
		s.logger.Debug("续期安全验证仍在有效期内，不重复通知", "account", accountID)
		return
	}
	s.logger.Warn("定时续期被闲鱼要求安全验证，需要用户人工完成", "account", accountID, "channel_available", hasChannel)
	if s.notifier == nil {
		return
	}
	// action 是提示用户的下一步操作；没有可用验证通道时回退为重新扫码授权。
	action := "请尽快在管理页重新扫码授权该账号"
	if hasChannel {
		action = "请尽快在管理页该账号卡片点击「完成安全验证」"
	}
	s.notifier.NotifyAccountEvent(accountID, securityVerificationEvent, "warn", "闲鱼要求安全验证，自动续期未完成",
		fmt.Sprintf("账号 %s 的 API 自动续期被闲鱼要求完成安全验证（人脸或扫码）。%s，否则订单、发货等接口可能在数分钟后失效。", accountID, action))
}
