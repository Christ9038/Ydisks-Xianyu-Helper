package server

import "context"

// CredentialVerificationPort 定义“定时续期被平台要求安全验证”后的人工验证 HTTP 用例端口。
// 实现方负责账号归属校验；Server 不接触验证地址、Cookie 或平台会话。
type CredentialVerificationPort interface {
	// Start 为用户拥有的账号启动人工验证会话并返回会话标识；没有待验证登记时返回应用层 ErrVerificationNotPending。
	Start(ctx context.Context, userID int64, accountID string) (string, error)
	// PendingAmong 返回 accountIDs 中当前有待完成安全验证的账号集合；调用方只能传入已按用户归属过滤的标识。
	PendingAmong(accountIDs []string) map[string]bool
}
