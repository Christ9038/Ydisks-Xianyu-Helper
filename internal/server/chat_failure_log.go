package server

import (
	"context"
	"errors"
	"log/slog"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/logging"
	"xianyu-go/internal/logsafe"
)

// logChatFailure 在 HTTP 边界记录一次聊天发送或商品请求失败的脱敏原因。
//
// HTTP 响应只携带稳定的固定文案，平台拒绝原因（例如内容审核拦截）、下载失败原因等只存在于应用层返回的错误链里，
// 不在这里记录就会彻底丢失。ctx 是请求上下文（自动带上 request_id、user_id）；operation 标识入口
// （text、image、library_image、item_card_send、item_card_list）；accountID、chatID 用于定位账号与会话，chatID 为空时省略该字段；
// err 是应用层返回的错误，写入前经 logsafe 去除 URL 查询与凭证键值，不含消息正文。
// 已知的可恢复失败（离线、参数无效、平台拒绝、结果待确认）记为 Warn；保存本地消息、状态收口失败或发送入口未映射的错误记为 Error；读取平台商品列表（item_card_list）的外部失败始终按 Warn 记录。
func logChatFailure(ctx context.Context, operation, accountID, chatID string, err error) {
	if err == nil {
		return
	}
	// level 是本次失败的日志等级，默认按可恢复失败记为 Warn。
	level := slog.LevelWarn
	if errors.Is(err, chatapp.ErrStatusSave) || errors.Is(err, chatapp.ErrChatItemCreate) || (operation != "item_card_list" && !isRecoverableChatSendError(err)) {
		level = slog.LevelError
	}
	// attrs 是固定键名的日志属性；err 已脱敏。
	attrs := []any{"operation", operation, "account", accountID}
	if chatID != "" {
		attrs = append(attrs, "chat_id", chatID)
	}
	attrs = append(attrs, "err", logsafe.Error(err))
	logging.LogAt(ctx, nil, 1, level, "聊天请求失败", attrs...)
}

// isRecoverableChatSendError 判断错误是否属于调用方可重试或可核对的已知发送失败，用于决定日志等级。
// err 是应用层返回的错误。
func isRecoverableChatSendError(err error) bool {
	return errors.Is(err, chatapp.ErrUnavailable) || errors.Is(err, chatapp.ErrOffline) ||
		errors.Is(err, chatapp.ErrSendInvalidInput) || errors.Is(err, chatapp.ErrSend) ||
		errors.Is(err, chatapp.ErrSendUncertain) || errors.Is(err, chatapp.ErrChatItemUnavailable) ||
		errors.Is(err, chatapp.ErrChatItemInvalid) || errors.Is(err, chatapp.ErrChatItemForbidden) ||
		errors.Is(err, chatapp.ErrChatSessionNotFound) || errors.Is(err, chatapp.ErrSessionUnavailable)
}
