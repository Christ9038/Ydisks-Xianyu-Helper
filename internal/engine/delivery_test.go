package engine

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

func newDeliveryStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s := db.NewStore(d)
	// 初始化 admin + 一个账号。
	s.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	admin, _ := s.Users.GetByUsername(context.Background(), "admin")
	s.Cookies.Save(context.Background(), "cid", "unb=123; _m_h5_tk=tk_1;", admin.ID)
	return s, func() { d.Close() }
}

// TestIsAutoDeliveryTrigger 触发关键字检测。
func TestIsAutoDeliveryTrigger(t *testing.T) {
	cases := map[string]bool{
		"[我已付款，等待你发货]":        true,
		"买家说：[我已付款，等待你发货] 谢谢": true,
		"[已付款，待发货]":           true,
		"[记得及时发货]":            true,
		"我已付款，等待你发货":          true,
		"你好，这个还在吗":            false,
		"已付款，待发货":             false, // 无括号变体不在触发列表中
		"":                    false,
	}
	for in, want := range cases {
		if got := IsAutoDeliveryTrigger(in); got != want {
			t.Errorf("IsAutoDeliveryTrigger(%q)=%v want %v", in, got, want)
		}
	}
}

// TestExtractOrderID 从真实样本结构提取订单 ID。
func TestExtractOrderID(t *testing.T) {
	// dxCard 的 button.targetUrl 含 orderId。
	msg := map[string]any{
		"1": map[string]any{
			"6": map[string]any{
				"3": map[string]any{
					"5": `{"dxCard":{"item":{"main":{"exContent":{"button":{"targetUrl":"fleamarket://adjust_price?bizOrderId=2503688126356636370"}}}}}}`,
				},
			},
		},
	}
	if id := extractOrderID(msg); id != "2503688126356636370" {
		t.Errorf("extractOrderID=%q want 2503688126356636370", id)
	}
}

// TestExtractOrderID_FallbackString 整消息字符串兜底。
func TestExtractOrderID_FallbackString(t *testing.T) {
	msg := map[string]any{
		"raw": "some text with bizOrderId=98765432109987654 inside",
	}
	if id := extractOrderID(msg); id != "98765432109987654" {
		t.Errorf("fallback extractOrderID=%q", id)
	}
}

// TestDeliveryRuleMatching 验证双向模糊匹配 SQL 与多规格过滤。
func TestDeliveryRuleMatching(t *testing.T) {
	s, cleanup := newDeliveryStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := s.Users.GetByUsername(ctx, "admin")

	// 一个 text 卡券 + 一个 data 卡券（非多规格）。
	s.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,text_content,data_content,enabled,user_id) VALUES
		(1,'VIP卡','text','您的卡密:ABC123',NULL,1,?),
		(2,'数据卡','data',NULL,'line1\nline2\nline3',1,?)`, admin.ID, admin.ID)
	// 多规格卡券。
	s.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,text_content,enabled,is_multi_spec,spec_name,spec_value,user_id) VALUES
		(3,'红色版','text','红色发货码',1,1,'颜色','红',?)`, admin.ID)
	// 发货规则：关键字"VIP" → 卡券1；"商品" → 卡券2；"红色" → 卡券3。
	s.DB.ExecContext(ctx, `INSERT INTO delivery_rules (id,keyword,card_id,enabled,user_id) VALUES
		(1,'VIP',1,1,?),(2,'商品',2,1,?),(3,'红色',3,1,?)`, admin.ID, admin.ID, admin.ID)

	// 非多规格匹配：商品文本"我的VIP商品"应命中规则1（VIP 含于文本，长度优先）+规则2（"商品"含于文本）。
	rules, err := s.DeliveryRules.MatchByKeyword(ctx, "我的VIP商品")
	if err != nil {
		t.Fatalf("MatchByKeyword: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("应匹配2条非多规格规则，got %d", len(rules))
	}
	// 长度降序：VIP(3) > 商品(2)，规则1应在前。
	if rules[0].ID != 1 {
		t.Errorf("应按关键字长度降序，首条=%d want 1", rules[0].ID)
	}

	// 多规格匹配。
	msRules, err := s.DeliveryRules.MatchByKeywordAndSpec(ctx, "红色商品", "颜色", "红")
	if err != nil {
		t.Fatalf("MatchByKeywordAndSpec: %v", err)
	}
	if len(msRules) != 1 || msRules[0].ID != 3 {
		t.Fatalf("多规格应只匹配规则3，got %+v", msRules)
	}
}

func TestDeliveryRuleMatchingByItemAndVariant(t *testing.T) {
	s, cleanup := newDeliveryStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := s.Users.GetByUsername(ctx, "admin")

	_, _ = s.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES
		(11,'30天库存','text','CODE30',1,?),
		(12,'90天库存','text','CODE90',1,?)`, admin.ID, admin.ID)
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO delivery_rules
		(id,keyword,card_id,delivery_count,enabled,user_id,cookie_id,item_id)
		VALUES (21,'会员',11,1,1,?,'cid','item-vip')`, admin.ID)
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO delivery_rule_variants
		(rule_id,spec_name,spec_value,card_id,delivery_count,enabled) VALUES
		(21,'套餐','30天',11,1,1),(21,'套餐','90天',12,2,1)`)

	rules, err := s.DeliveryRules.MatchForOrder(ctx, "cid", "item-vip", "任意标题", "套餐", "90天")
	if err != nil {
		t.Fatalf("MatchForOrder: %v", err)
	}
	if len(rules) != 1 || rules[0].CardID != 12 || rules[0].DeliveryCount != 2 {
		t.Fatalf("规格映射错误: %+v", rules)
	}
	other, err := s.DeliveryRules.MatchForOrder(ctx, "other", "item-vip", "任意标题", "套餐", "90天")
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("其他账号不应命中规则: %+v", other)
	}
}

// TestConsumeBatchData data 卡券逐行消费。
func TestConsumeBatchData(t *testing.T) {
	s, cleanup := newDeliveryStore(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := s.Users.GetByUsername(ctx, "admin")
	// 创建一个 data 卡券（id=2）。
	s.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,data_content,enabled,user_id) VALUES (2,'数据卡','data','line1'||char(10)||'line2'||char(10)||'line3',1,?)`, admin.ID)

	c1, err := s.Cards.ConsumeBatchData(ctx, 2)
	if err != nil {
		t.Fatalf("ConsumeBatchData: %v", err)
	}
	if c1 != "line1" {
		t.Errorf("首次消费=%q want line1", c1)
	}
	c2, _ := s.Cards.ConsumeBatchData(ctx, 2)
	if c2 != "line2" {
		t.Errorf("二次消费=%q want line2", c2)
	}
	c3, _ := s.Cards.ConsumeBatchData(ctx, 2)
	if c3 != "line3" {
		t.Errorf("三次消费=%q want line3", c3)
	}
	// 第四次应失败（已空）。
	if _, err := s.Cards.ConsumeBatchData(ctx, 2); err == nil {
		t.Fatal("数据耗尽应报错")
	}
}

// TestDedupMachine 四重防重：冷却 + 延迟锁。
func TestDedupMachine(t *testing.T) {
	s, cleanup := newDeliveryStore(t)
	defer cleanup()
	d := NewDeliveryService("cid", s, nil, nil, nil, nil)
	d.SetCookieSource(func() string { return "" }, func(string) {})

	// 冷却：无发货记录时允许。
	if !d.canAutoDelivery("order1") {
		t.Fatal("首次应允许发货")
	}
	// 标记已发货 → 10 分钟冷却。
	d.markDeliverySent("order1")
	if d.canAutoDelivery("order1") {
		t.Fatal("发货后应进入冷却期")
	}
	if !d.isLockHeld("order1") {
		t.Fatal("发货后延迟锁应持有")
	}
	// 不同订单不受影响。
	if !d.canAutoDelivery("order2") {
		t.Fatal("order2 不应受 order1 冷却影响")
	}
}

func TestShouldAutoConfirmRespectsAccountSetting(t *testing.T) {
	s, cleanup := newDeliveryStore(t)
	defer cleanup()
	ctx := context.Background()
	d := NewDeliveryService("cid", s, nil, nil, nil, nil)

	if !d.shouldAutoConfirm(ctx, "order1") {
		t.Fatal("默认开启时应允许自动确认发货")
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE cookies SET auto_confirm=0 WHERE id=?`, "cid"); err != nil {
		t.Fatalf("关闭 auto_confirm: %v", err)
	}
	if d.shouldAutoConfirm(ctx, "order2") {
		t.Fatal("关闭设置后不应自动确认发货")
	}
	if d.shouldAutoConfirm(ctx, "") {
		t.Fatal("缺少订单 ID 时不应自动确认发货")
	}
}

// TestProcessDeliveryContentWithDescription 备注/变量替换。
func TestProcessDeliveryContentWithDescription(t *testing.T) {
	// 有 {DELIVERY_CONTENT} 变量。
	got := processDeliveryContentWithDescription("ABC123", "您的卡密是 {DELIVERY_CONTENT}，请妥善保管")
	want := "您的卡密是 ABC123，请妥善保管"
	if got != want {
		t.Errorf("变量替换: got %q want %q", got, want)
	}
	// 无变量：备注 + 内容。
	got = processDeliveryContentWithDescription("ABC123", "温馨提示")
	if got != "温馨提示\n\nABC123" {
		t.Errorf("无变量组合: got %q", got)
	}
	// 图片标记不处理。
	got = processDeliveryContentWithDescription("__IMAGE_SEND__1|http://x", "备注")
	if got != "__IMAGE_SEND__1|http://x" {
		t.Errorf("图片标记不应处理: got %q", got)
	}
	// 无备注直接返回。
	got = processDeliveryContentWithDescription("ABC123", "")
	if got != "ABC123" {
		t.Errorf("无备注: got %q", got)
	}
}

// TestAcquireOrderLockSerializes 同一 order_id 串行化。
func TestAcquireOrderLockSerializes(t *testing.T) {
	s, cleanup := newDeliveryStore(t)
	defer cleanup()
	d := NewDeliveryService("cid", s, nil, nil, nil, nil)

	done := make(chan struct{})
	// 先获取锁。
	unlock := d.acquireOrderLock("orderX")
	go func() {
		defer close(done)
		d.acquireOrderLock("orderX") // 应阻塞
	}()
	select {
	case <-done:
		t.Fatal("锁未释放前第二个获取应阻塞")
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("释放后第二个应能获取")
	}
}
