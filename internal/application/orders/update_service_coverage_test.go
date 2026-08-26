package orders

import "testing"

// TestValidationErrorNilReceiver 覆盖订单校验错误的空接收者和自定义消息语义。
func TestValidationErrorNilReceiver(t *testing.T) {
	// nilError 保存空校验错误接收者的稳定错误文本。
	var nilError *ValidationError
	// got 保存空校验错误的 Error 方法返回值。
	if got := nilError.Error(); got != "订单字段校验失败" {
		t.Fatalf("nil error=%q", got)
	}
	// customError 保存带业务提示的校验错误文本。
	customError := (&ValidationError{Message: "数量必须为正数"}).Error()
	if customError != "数量必须为正数" {
		t.Fatalf("custom error=%q", customError)
	}
}
