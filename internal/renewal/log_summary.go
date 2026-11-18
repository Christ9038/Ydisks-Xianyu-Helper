package renewal

import (
	"fmt"

	apirenew "xianyu-go/internal/xianyu/renew"
)

// renewStepDetails 把续期结果的每个步骤整理成不含敏感内容的日志摘要；需要人工验证时追加标记（验证地址已在响应文本中脱敏）。
func renewStepDetails(res *apirenew.Result) []string {
	// details 依次保存步骤摘要、总体结果和可选的验证标记。
	details := make([]string, 0, len(res.StepDetails)+2)
	// step 是当前遍历的续期步骤。
	for _, step := range res.StepDetails {
		details = append(details, fmt.Sprintf("%s: http=%d business_ok=%v set_cookie=%d", step.Name, step.HTTPStatus, step.BusinessOK, step.SetCookieCount))
	}
	details = append(details, fmt.Sprintf("result: success=%v skipped=%v reason=%s", res.Success, res.Skipped, res.SkipReason))
	if res.VerificationRequired {
		details = append(details, "verification_required=true")
	}
	return details
}

// renewLogStatus 由续期结果和“是否有 Cookie 字段变化”推导续期记录状态。
func renewLogStatus(res *apirenew.Result, cookieChanged bool) string {
	switch {
	case res.HasPending():
		return "pending"
	case res.Skipped:
		return "skipped"
	case res.Success && cookieChanged:
		return "cookie_updated"
	case res.Success:
		return "success"
	default:
		return "failed"
	}
}
