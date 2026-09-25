package adapter

import (
	"context"
	"testing"

	keywordsapp "xianyu-go/internal/application/keywords"
)

// TestKeywordRepositoryExpressionFields 验证适配器在旧 Keyword-only 调用和多表达式字段之间保持一致映射。
func TestKeywordRepositoryExpressionFields(t *testing.T) {
	// store、cleanup 保存临时 SQLite 存储及其释放责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试数据库操作共用的非取消上下文。
	ctx := context.Background()
	// owner、ownerErr 保存测试账号所属用户摘要及读取错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// repository 是待验证的关键词数据库适配器。
	repository := NewKeywordRepository(store)
	// legacyID、legacyErr 保存只提供旧 Keyword 字段的创建结果。
	legacyID, legacyErr := repository.Add(ctx, owner.ID, "cid", keywordsapp.Draft{
		Keyword: "旧适配器关键词", Reply: "旧回复", Type: "text",
	})
	if legacyErr != nil || legacyID <= 0 {
		t.Fatalf("legacy adapter Add: id=%d err=%v", legacyID, legacyErr)
	}
	// multiID、multiErr 保存多表达式创建结果。
	multiID, multiErr := repository.Add(ctx, owner.ID, "cid", keywordsapp.Draft{
		Expressions: []string{"首表达式", "次表达式"}, MatchType: "regexp", Reply: "多表达式回复", Type: "text",
	})
	if multiErr != nil || multiID <= 0 {
		t.Fatalf("multi-expression adapter Add: id=%d err=%v", multiID, multiErr)
	}
	// initialRows、initialErr 保存创建后的应用层关键词模型。
	initialRows, initialErr := repository.List(ctx, owner.ID, "cid")
	if initialErr != nil || len(initialRows) != 2 {
		t.Fatalf("initial rows=%+v err=%v", initialRows, initialErr)
	}
	// legacyRowFound、multiRowFound 保存两类创建结果是否正确映射。
	var legacyRowFound, multiRowFound bool
	// row 表示当前遍历到的应用层关键词模型。
	for _, row := range initialRows {
		switch row.ID {
		case legacyID:
			legacyRowFound = row.Keyword == "旧适配器关键词" && row.MatchType == "contains" && len(row.Expressions) == 1 && row.Expressions[0] == "旧适配器关键词"
		case multiID:
			multiRowFound = row.Keyword == "首表达式" && row.MatchType == "regexp" && len(row.Expressions) == 2 && row.Expressions[1] == "次表达式"
		}
	}
	if !legacyRowFound || !multiRowFound {
		t.Fatalf("adapter mapping rows=%+v", initialRows)
	}

	// updateErr 保存多表达式规则更新为新表达式集合的结果。
	if updateErr := repository.Update(ctx, owner.ID, "cid", multiID, keywordsapp.Draft{
		Keyword: "旧首值", Expressions: []string{"更新首", "更新次"}, MatchType: "regexp", Reply: "更新回复", Type: "text",
	}); updateErr != nil {
		t.Fatalf("adapter Update: %v", updateErr)
	}
	// updatedRows、updatedErr 保存更新后的应用层关键词模型。
	updatedRows, updatedErr := repository.List(ctx, owner.ID, "cid")
	if updatedErr != nil {
		t.Fatalf("updated rows: %v", updatedErr)
	}
	// updatedRowFound 保存更新后的规则是否同步了首表达式和完整集合。
	var updatedRowFound bool
	// row 表示当前遍历到的更新后规则。
	for _, row := range updatedRows {
		if row.ID == multiID {
			updatedRowFound = row.Keyword == "更新首" && row.MatchType == "regexp" && len(row.Expressions) == 2 && row.Expressions[0] == "更新首" && row.Expressions[1] == "更新次"
		}
	}
	if !updatedRowFound {
		t.Fatalf("updated adapter rows=%+v", updatedRows)
	}

	// replaceErr 保存替换旧 Keyword-only 规则和多表达式规则的结果。
	if replaceErr := repository.Replace(ctx, owner.ID, "cid", []keywordsapp.Draft{
		{Keyword: "替换兼容", Reply: "兼容回复", Type: "text"},
		{Expressions: []string{"替换首", "替换次"}, MatchType: "regexp", Reply: "替换回复", Type: "text"},
	}); replaceErr != nil {
		t.Fatalf("adapter Replace: %v", replaceErr)
	}
	// replacedRows、replacedErr 保存替换后的应用层规则。
	replacedRows, replacedErr := repository.List(ctx, owner.ID, "cid")
	if replacedErr != nil || len(replacedRows) != 2 {
		t.Fatalf("replaced rows=%+v err=%v", replacedRows, replacedErr)
	}
	// replacementCompatibility、replacementExpressions 保存两条替换规则的字段核对结果。
	var replacementCompatibility, replacementExpressions bool
	// row 表示当前遍历到的替换后应用规则。
	for _, row := range replacedRows {
		switch row.Keyword {
		case "替换兼容":
			replacementCompatibility = row.MatchType == "contains" && len(row.Expressions) == 1 && row.Expressions[0] == "替换兼容"
		case "替换首":
			replacementExpressions = row.MatchType == "regexp" && len(row.Expressions) == 2 && row.Expressions[1] == "替换次"
		}
	}
	if !replacementCompatibility || !replacementExpressions {
		t.Fatalf("replacement mapping rows=%+v", replacedRows)
	}
}
