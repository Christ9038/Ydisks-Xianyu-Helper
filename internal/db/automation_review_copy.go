package db

import "context"

// CountRunsForRule 返回指定规则已经创建的自动化运行总数，供“好评文案模板池按顺序循环”推算下一条位置。
// ruleID 是自动化规则标识；ctx 控制查询取消。该方法只读取计数，不接触任何订单、凭证或消息正文。
func (a *AutomationRules) CountRunsForRule(ctx context.Context, ruleID int64) (int, error) {
	// total 保存该规则的运行数量，失败的运行也计入，保证轮换位置只增不减。
	var total int
	// err 保存计数查询错误。
	err := a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_runs WHERE rule_id=?`, ruleID).Scan(&total)
	return total, err
}
