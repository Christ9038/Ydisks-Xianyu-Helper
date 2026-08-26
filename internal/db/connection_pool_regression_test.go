package db

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestTemplateAndRuleLoadsWithSingleSQLiteConnection 验证嵌套加载不会占满 SQLite 单连接池。
func TestTemplateAndRuleLoadsWithSingleSQLiteConnection(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 数据库及释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	store.DB.SetMaxOpenConns(1)
	// ctx 保存限制连接池回归测试最长等待时间的上下文。
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// userID、cookieID 保存规则和模板的归属初始化结果。
	userID, cookieID := seedAccount(t, store)
	// templateID、templateErr 保存无变量模板的创建结果。
	templateID, templateErr := store.DeliveryTemplates.Create(ctx, DeliveryTemplateInput{UserID: userID, Name: "单连接模板", Enabled: true, Messages: []string{"正文"}})
	if templateErr != nil {
		t.Fatal(templateErr)
	}
	// ruleID、ruleErr 保存引用模板的规则创建结果。
	ruleID, ruleErr := store.Automation.Create(ctx, AutomationRuleInput{UserID: userID, CookieID: cookieID, ItemID: "single-connection", Name: "单连接规则", TriggerType: "paid", Enabled: true, Actions: []AutomationActionInput{{ActionType: "send_template", DeliveryTemplateID: templateID, ConfigJSON: `{}`, Enabled: true, SortOrder: 1}}})
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// actions、actionsErr 保存单连接下的动作加载结果。
	actions, actionsErr := store.Automation.Actions(ctx, ruleID)
	if actionsErr != nil || len(actions) != 1 || len(actions[0].TemplateMessages) != 1 {
		t.Fatalf("Actions 单连接加载失败：actions=%+v err=%v", actions, actionsErr)
	}
	// matched、matchErr 保存单连接下规则匹配结果。
	matched, matchErr := store.Automation.Match(ctx, cookieID, "single-connection", "paid")
	if matchErr != nil || len(matched) != 1 || len(matched[0].Actions) != 1 {
		t.Fatalf("Match 单连接加载失败：rules=%+v err=%v", matched, matchErr)
	}
	// templates、listErr 保存单连接下模板列表和消息加载结果。
	templates, listErr := store.DeliveryTemplates.ListForUser(ctx, userID)
	if listErr != nil || len(templates) != 1 || len(templates[0].Messages) != 1 {
		t.Fatalf("模板列表单连接加载失败：templates=%+v err=%v", templates, listErr)
	}
}

// TestConcurrentRuleMatchesWithSingleSQLiteConnection 验证多个匹配请求不会因子查询互相等待。
func TestConcurrentRuleMatchesWithSingleSQLiteConnection(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 数据库及释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	store.DB.SetMaxOpenConns(1)
	// setupCtx 保存初始化规则的上下文。
	setupCtx := context.Background()
	// userID、cookieID 保存规则归属初始化结果。
	userID, cookieID := seedAccount(t, store)
	// templateID、templateErr 保存并发匹配使用的模板。
	templateID, templateErr := store.DeliveryTemplates.Create(setupCtx, DeliveryTemplateInput{UserID: userID, Name: "并发模板", Enabled: true, Messages: []string{"正文"}})
	if templateErr != nil {
		t.Fatal(templateErr)
	}
	// err 保存并发规则创建错误。
	if _, err := store.Automation.Create(setupCtx, AutomationRuleInput{UserID: userID, CookieID: cookieID, ItemID: "concurrent", Name: "并发规则", TriggerType: "paid", Enabled: true, Actions: []AutomationActionInput{{ActionType: "send_template", DeliveryTemplateID: templateID, ConfigJSON: `{}`, Enabled: true, SortOrder: 1}}}); err != nil {
		t.Fatal(err)
	}
	// ctx 保存每个并发匹配请求共享的截止时间。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// waitGroup 等待全部匹配请求完成。
	var waitGroup sync.WaitGroup
	// errorsFound 保存并发匹配期间收集到的错误数量。
	var errorsFound int
	// errorLock 保护并发测试错误计数。
	var errorLock sync.Mutex
	for /* index 表示并发匹配请求的序号。 */ index := 0; index < 8; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			// matched、err 保存当前并发匹配结果及错误。
			matched, err := store.Automation.Match(ctx, cookieID, "concurrent", "paid")
			if err != nil || len(matched) != 1 || len(matched[0].Actions) != 1 {
				errorLock.Lock()
				errorsFound++
				errorLock.Unlock()
			}
		}()
	}
	waitGroup.Wait()
	if errorsFound != 0 {
		t.Fatalf("并发匹配失败次数=%d", errorsFound)
	}
}
