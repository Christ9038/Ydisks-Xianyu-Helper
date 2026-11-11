package adapter

import (
	"errors"
	"fmt"
	"testing"

	"xianyu-go/internal/xianyu/mtop"
)

// TestIsBatchAbortErrorCoversRiskSessionAndToken 验证批量发布整批中止判定覆盖风控、会话失效与 Token 耗尽，
// 且包装后的错误同样生效；普通业务失败与 nil 不中止，会话判定仍只认 Session 失效。
func TestIsBatchAbortErrorCoversRiskSessionAndToken(t *testing.T) {
	// aborting 是必须终止本批次的错误样例，均经过一层包装。
	aborting := []error{
		fmt.Errorf("发布失败: %w", &mtop.RiskVerificationError{API: "publish"}),
		fmt.Errorf("发布失败: %w", &mtop.SessionExpiredError{API: "publish"}),
		fmt.Errorf("发布失败: %w", &mtop.MTopResponseError{API: "publish", Kind: mtop.MTopErrorTokenExpired, HTTPStatus: 200}),
	}
	// err 是当前中止样例。
	for _, err := range aborting {
		if !IsBatchAbortError(err) {
			t.Fatalf("应中止本批次: %v", err)
		}
	}
	if IsBatchAbortError(nil) || IsBatchAbortError(errors.New("类目不存在")) {
		t.Fatal("普通失败不应中止本批次")
	}
	if IsCredentialExpiredError(aborting[0]) || IsCredentialExpiredError(aborting[2]) {
		t.Fatal("风控与 Token 耗尽不能升级为账号会话恢复")
	}
}
