package ws

import "errors"

// SendErrorKind 标识一条聊天发送请求在平台侧的可证明结果。
type SendErrorKind string

const (
	// SendNotSent 表示请求尚未进入无法回收的远端执行阶段。
	SendNotSent SendErrorKind = "not_sent"
	// SendRejected 表示平台明确拒绝了这条请求。
	SendRejected SendErrorKind = "rejected"
	// SendUncertain 表示请求可能已经执行，但本地没有得到确定确认。
	SendUncertain SendErrorKind = "uncertain"
)

// SendError 表示聊天请求的安全结果分类，并保留底层错误链供调用方判断。
type SendError struct {
	// Kind 保存平台结果分类。
	Kind SendErrorKind
	// Code 保存平台返回的 HTTP/业务状态码；无法解析时为零。
	Code int
	// Reason 保存平台应答中已脱敏、截断的原因文本；平台未提供或无法获取时为空。
	Reason string
	// Err 保存底层传输或协议错误。
	Err error
}

// Error 返回不包含消息正文、Cookie 或 Token 的安全错误文本；Reason 已在写入前脱敏。
func (e *SendError) Error() string {
	if e == nil {
		return ""
	}
	// text 保存逐步拼接的安全错误文本。
	text := "闲鱼聊天发送结果: kind=" + string(e.Kind)
	if e.Code != 0 {
		text += " code=" + itoa(e.Code)
	}
	if e.Reason != "" {
		text += " reason=" + e.Reason
	}
	return text
}

// Unwrap 保留底层错误的 errors.Is/errors.As 能力。
func (e *SendError) Unwrap() error { return e.Err }

// SendResultKind 返回错误携带的发送结果分类；普通错误按未知结果处理。
func SendResultKind(err error) SendErrorKind {
	if err == nil {
		return ""
	}
	// sendErr 保存待解析的类型化发送错误。
	var sendErr *SendError
	if errors.As(err, &sendErr) {
		return sendErr.Kind
	}
	return SendUncertain
}

// itoa 避免为单个安全错误引入格式化输出中的敏感值。
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	// negative 表示状态码是否为负数。
	negative := value < 0
	if negative {
		value = -value
	}
	// buf 和 index 保存无格式化敏感文本的数字缓冲区及写入位置。
	buf := [20]byte{}
	// index 是数字从缓冲区末尾向前写入的位置。
	index := len(buf)
	for value > 0 {
		index--
		buf[index] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		index--
		buf[index] = '-'
	}
	return string(buf[index:])
}
