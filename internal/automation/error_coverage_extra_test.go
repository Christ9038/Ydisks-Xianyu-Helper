package automation

import (
	"errors"
	"testing"

	"xianyu-go/internal/db"
)

// TestAutomationErrorWrappers 覆盖自动化准备错误、不确定动作错误和不可重试错误包装。
func TestAutomationErrorWrappers(t *testing.T) {
	// cause 保存自动化错误包装使用的底层原因。
	cause := errors.New("底层失败")
	// preparation 保存准备阶段错误包装结果。
	preparation := &preparationError{err: cause}
	if preparation.Error() != cause.Error() || !errors.Is(preparation, cause) || preparation.Unwrap() != cause {
		t.Fatal("preparation error chain incorrect")
	}
	// uncertain 保存不确定动作错误包装结果。
	uncertain := &uncertainActionError{err: cause}
	if uncertain.Error() != cause.Error() || !errors.Is(uncertain, cause) || uncertain.Unwrap() != cause {
		t.Fatal("uncertain error chain incorrect")
	}
	// wrapped 保存通用不确定错误包装结果。
	wrapped := uncertainAction(cause)
	if !errors.Is(wrapped, cause) || uncertainAction(wrapped) != wrapped || uncertainAction(nil) != nil {
		t.Fatal("uncertainAction semantics incorrect")
	}
	// noRetry 保存外部动作确定未执行的包装错误。
	noRetry := noRetryAction(cause)
	if !errors.Is(noRetry, cause) || noRetryAction(nil) != nil {
		t.Fatal("noRetryAction wrapping incorrect")
	}
	// existing 保存已经带不可重试前缀的错误，验证不会重复包装。
	existing := errors.New(db.NoRetryErrorPrefix + ": 已确定未执行")
	if noRetryAction(existing) != existing {
		t.Fatal("existing no-retry error should remain unchanged")
	}
}
