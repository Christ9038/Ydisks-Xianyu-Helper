package automation

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

// TestCenter_ReviewRolesKeepRuleExecution 使用 t 验证 seller 与无角色评价仍匹配规则，buyer 副本与重复重放不会产生额外消息。
func TestCenter_ReviewRolesKeepRuleExecution(t *testing.T) {
	// role 是当前合法卖家副本的角色字段；空值保护旧版评价兼容。
	for _, role := range []string{"seller", ""} {
		// t 为两种合法协议分别建立隔离数据库和消息发送记录。
		t.Run(firstNonEmpty(role, "no-role"), func(t *testing.T) {
			// store、cleanup 提供当前测试的 SQLite 仓储和关闭责任。
			store, cleanup := newAutomationTestStore(t)
			defer cleanup()
			// ctx 限定本测试内的同步规则创建与任务处理。
			ctx := context.Background()
			// admin、adminErr 提供测试规则及买家账号的管理用户归属。
			admin, adminErr := store.Users.GetByUsername(ctx, "admin")
			if adminErr != nil || admin == nil {
				t.Fatalf("读取测试管理员失败: %v", adminErr)
			}
			// saveErr 保存买家接收账号创建结果；账号键不等于平台 UID。
			if saveErr := store.Cookies.Save(ctx, "buyer-account", "", admin.ID); saveErr != nil {
				t.Fatal(saveErr)
			}
			// accountID 是需要启用并配置相同评价规则的接收账号，防止未配规则掩盖买家误触发。
			for _, accountID := range []string{"cid", "buyer-account"} {
				// statusErr 保存测试账号启用结果。
				if statusErr := store.Cookies.SetStatus(ctx, accountID, true); statusErr != nil {
					t.Fatal(statusErr)
				}
				// ruleErr 保存评价规则写入结果，发送动作只调用本地消息记录器。
				if _, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{
					UserID: admin.ID, CookieID: accountID, Name: "评价角色回归", TriggerType: TriggerBuyerReviewed, Enabled: true,
					Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "评价回赠", Enabled: true}},
				}); ruleErr != nil {
					t.Fatal(ruleErr)
				}
			}
			// sender 是本地假发送器，不访问平台或发送真实消息。
			sender := &testSender{}
			// center 使用真实事实记录、规则匹配和运行幂等流程。
			center := New(store, testSenderProvider{sender: sender}, nil)
			// copyRole 按买家先到、卖家到达、双方重放的顺序生成同一订单副本。
			for _, copyRole := range []string{"buyer", role, "buyer", role} {
				// accountID 根据副本角色选择实际接收账号，不能由评价文案推断。
				accountID := "cid"
				if copyRole == "buyer" {
					accountID = "buyer-account"
				}
				// task 是真实 WS 入口解析结果；明确买家副本被入口拒绝后不进入事实记录。
				task := ExtractTaskFromWS(accountID, "", roleEventFixture(t, TriggerBuyerReviewed, "nestedURL", copyRole))
				if task != nil {
					// handleErr 保存规则执行或事实写入错误，禁止静默吞掉执行失败。
					if handleErr := center.HandleTask(ctx, *task); handleErr != nil {
						t.Fatal(handleErr)
					}
				}
			}
			if len(sender.texts) != 1 || sender.texts[0] != "评价回赠" {
				t.Fatalf("合法评价应恰好发送一次，实际消息数=%d", len(sender.texts))
			}
			// totalRuns、sellerRuns 分别是全部运行数和成功卖家运行数，二者都必须恰好为一。
			var totalRuns, sellerRuns int
			// queryErr 保存运行归属和幂等性的联合查询结果，不读取敏感运行凭证。
			if queryErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN cookie_id='cid' AND status='success' THEN 1 ELSE 0 END),0) FROM automation_runs`).Scan(&totalRuns, &sellerRuns); queryErr != nil {
				t.Fatal(queryErr)
			}
			if totalRuns != 1 || sellerRuns != 1 {
				t.Fatalf("评价运行归属或重放防重失败: 全部=%d 成功卖家=%d", totalRuns, sellerRuns)
			}
		})
	}
}
