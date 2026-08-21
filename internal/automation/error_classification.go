package automation

import (
	"fmt"
	"strings"

	"xianyu-go/internal/db"
)

// noRetryAction 标记外部动作确定未执行且不应被通用恢复队列再次调用的错误。
func noRetryAction(err error) error {
	if err == nil {
		return nil
	}
	if strings.HasPrefix(err.Error(), db.NoRetryErrorPrefix) {
		return err
	}
	return fmt.Errorf("%s: %w", db.NoRetryErrorPrefix, err)
}
