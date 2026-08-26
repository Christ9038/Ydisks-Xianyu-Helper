package engine

import (
	"context"
	"testing"
)

// TestAIReplierItemInfoUsesFieldFallbacks 验证商品标题、价格和详情为空时的业务兜底规则。
func TestAIReplierItemInfoUsesFieldFallbacks(t *testing.T) {
	// store、cleanup 保存隔离的 AI 测试数据库和关闭责任。
	store, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是商品信息查询使用的非取消上下文。
	ctx := context.Background()
	// insertErr 表示写入字段不完整测试商品的数据库错误。
	if _, insertErr := store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,item_price,item_description,item_detail) VALUES ('cid','fallback-item','','not-a-number','','备用详情')`); insertErr != nil {
		t.Fatal(insertErr)
	}
	// replier 是绑定测试账号的 AI 回复适配器。
	replier := NewAIReplier("cid", store, nil)
	// title、price、description 保存商品字段兜底后的查询结果。
	title, price, description := replier.itemInfo(ctx, "fallback-item")
	if title != "未知商品" || price != 0 || description != "备用详情" {
		t.Fatalf("商品字段兜底异常 title=%q price=%v description=%q", title, price, description)
	}
}
