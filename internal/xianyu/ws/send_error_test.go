package ws

import (
	"errors"
	"fmt"
	"testing"
)

// TestSendErrorTextUnwrapAndKind 覆盖发送错误文本（含/不含状态码、nil）、错误链保留与结果分类。
func TestSendErrorTextUnwrapAndKind(t *testing.T) {
	// cause 是被包装的底层传输错误。
	cause := errors.New("closed")
	// withCode 带有平台状态码；withoutCode 不带。
	withCode := &SendError{Kind: SendRejected, Code: 403, Err: cause}
	// withoutCode 保存该步骤产生的中间结果，供后续断言使用。
	withoutCode := &SendError{Kind: SendNotSent, Err: cause}
	if withCode.Error() != "闲鱼聊天发送结果: kind=rejected code=403" {
		t.Fatalf("text=%q", withCode.Error())
	}
	if withoutCode.Error() != "闲鱼聊天发送结果: kind=not_sent" {
		t.Fatalf("text=%q", withoutCode.Error())
	}
	if (*SendError)(nil).Error() != "" {
		t.Fatal("nil 接收者应返回空文本")
	}
	if !errors.Is(withCode, cause) || withCode.Unwrap() != cause {
		t.Fatal("应保留底层错误链")
	}
	if SendResultKind(nil) != "" {
		t.Fatal("nil 错误没有分类")
	}
	if SendResultKind(fmt.Errorf("wrap: %w", withCode)) != SendRejected {
		t.Fatal("包装后的类型化错误应保留分类")
	}
	if SendResultKind(errors.New("plain")) != SendUncertain {
		t.Fatal("普通错误应按未知结果处理")
	}
}

// TestItoaHandlesZeroNegativeAndPositive 覆盖状态码数字转换的零、负数和多位数分支。
func TestItoaHandlesZeroNegativeAndPositive(t *testing.T) {
	// cases 保存输入数字到期望文本的映射。
	cases := map[int]string{0: "0", 7: "7", 4040: "4040", -12: "-12"}
	// input、want 分别是待转换数字和期望文本。
	for input, want := range cases {
		// got 保存本次调用的返回值，用于与期望结果比较。
		if got := itoa(input); got != want {
			t.Fatalf("itoa(%d)=%q want %q", input, got, want)
		}
	}
}

// TestSendErrorTextIncludesReason 覆盖带平台原因的错误文本，以及无状态码仅有原因的组合。
func TestSendErrorTextIncludesReason(t *testing.T) {
	// got 保存带状态码和原因的错误文本。
	if got := (&SendError{Kind: SendRejected, Code: 400, Reason: "reason=会话受限"}).Error(); got != "闲鱼聊天发送结果: kind=rejected code=400 reason=reason=会话受限" {
		t.Fatalf("text=%q", got)
	}
	// got 保存无状态码、仅有原因的错误文本。
	if got := (&SendError{Kind: SendUncertain, Reason: "x"}).Error(); got != "闲鱼聊天发送结果: kind=uncertain reason=x" {
		t.Fatalf("text=%q", got)
	}
}
