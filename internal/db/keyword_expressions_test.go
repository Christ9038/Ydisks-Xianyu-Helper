package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestKeywordsSQLiteExpressionsCRUD 验证 SQLite 关键词表达式的兼容写入、JSON 往返、更新、替换、删除和排序语义。
func TestKeywordsSQLiteExpressionsCRUD(t *testing.T) {
	// store、cleanup 保存隔离 SQLite 存储及其释放责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// userID、cookieID 保存测试账号归属信息；表达式仓储不读取账号凭证。
	userID, cookieID := seedAccount(t, store)
	exerciseKeywordExpressionCRUD(t, store, userID, cookieID)
}

// exerciseKeywordExpressionCRUD 在指定方言存储上验证关键词表达式完整 CRUD 契约。
// 测试同时覆盖旧 Keyword-only 调用、缺省新列、空 JSON 回退、首表达式同步和历史排序。
func exerciseKeywordExpressionCRUD(t *testing.T, store *Store, userID int64, cookieID string) {
	t.Helper()
	// ctx 是本测试所有数据库操作共用的非取消上下文。
	ctx := context.Background()
	// addErr 保存旧 Add API 写入普通包含规则时的数据库错误。
	if _, addErr := store.Keywords.Add(ctx, cookieID, "旧接口关键词", "旧回复", "", "", ""); addErr != nil {
		t.Fatalf("legacy Add: %v", addErr)
	}
	// storedKeyword、storedJSON、storedMatch 保存旧 Add API 实际写入的兼容字段。
	var storedKeyword, storedJSON, storedMatch string
	// scanErr 保存读取旧 Add API 持久化字段时的数据库错误。
	if scanErr := store.DB.QueryRowContext(ctx,
		`SELECT keyword, keyword_expressions, match_type FROM keywords WHERE cookie_id=? AND keyword=?`,
		cookieID, "旧接口关键词").Scan(&storedKeyword, &storedJSON, &storedMatch); scanErr != nil {
		t.Fatalf("read legacy Add storage: %v", scanErr)
	}
	if storedKeyword != "旧接口关键词" || storedMatch != "contains" {
		t.Fatalf("legacy Add storage keyword=%q match=%q", storedKeyword, storedMatch)
	}
	// storedExpressions 保存 JSON 文本反序列化后的旧 Add 表达式数组。
	var storedExpressions []string
	// decodeErr 保存旧 Add 表达式 JSON 解析错误。
	if decodeErr := json.Unmarshal([]byte(storedJSON), &storedExpressions); decodeErr != nil {
		t.Fatalf("decode legacy Add expressions: %v", decodeErr)
	}
	if !reflect.DeepEqual(storedExpressions, []string{"旧接口关键词"}) {
		t.Fatalf("legacy Add expressions=%v", storedExpressions)
	}

	// multiID、multiErr 保存多表达式规则的主键及写入错误。
	multiID, multiErr := store.Keywords.AddWithExpressions(ctx, cookieID,
		[]string{" 首表达式 ", "次表达式", "次表达式"}, "多表达式回复", "", "text", "REGEXP", "")
	if multiErr != nil {
		t.Fatalf("AddWithExpressions: %v", multiErr)
	}
	// rows、rowsErr 保存当前账号的全部关键词行。
	rows, rowsErr := store.Keywords.AllRows(ctx, cookieID)
	if rowsErr != nil {
		t.Fatalf("AllRows after AddWithExpressions: %v", rowsErr)
	}
	// multiRow 保存多表达式规则的数据库行。
	var multiRow KeywordRow
	// multiFound 表示是否找到刚写入的多表达式规则。
	var multiFound bool
	// row 表示当前遍历到的关键词持久化行。
	for _, row := range rows {
		if row.ID == multiID {
			multiRow = row
			multiFound = true
			break
		}
	}
	if !multiFound || multiRow.Keyword != "首表达式" || multiRow.MatchType != "regexp" || !reflect.DeepEqual(multiRow.Expressions, []string{"首表达式", "次表达式"}) {
		t.Fatalf("multi-expression row=%+v found=%v", multiRow, multiFound)
	}

	// insertErr 保存模拟旧数据库行省略新列时的插入错误。
	if _, insertErr := store.DB.ExecContext(ctx,
		`INSERT INTO keywords (cookie_id, keyword, reply, type) VALUES (?,?,?,?)`,
		cookieID, "历史缺列", "历史回复", "text"); insertErr != nil {
		t.Fatalf("insert legacy row without new columns: %v", insertErr)
	}
	// emptyJSONErr 保存模拟新列存在但 JSON 为空、匹配模式为空时的插入错误。
	if _, emptyJSONErr := store.DB.ExecContext(ctx,
		`INSERT INTO keywords (cookie_id, keyword, reply, type, keyword_expressions, match_type) VALUES (?,?,?,?,?,?)`,
		cookieID, "空 JSON", "空 JSON 回复", "text", "[]", ""); emptyJSONErr != nil {
		t.Fatalf("insert empty JSON row: %v", emptyJSONErr)
	}
	// legacyRows、legacyRowsErr 保存兼容行读取结果及错误。
	legacyRows, legacyRowsErr := store.Keywords.AllRows(ctx, cookieID)
	if legacyRowsErr != nil {
		t.Fatalf("AllRows legacy fallback: %v", legacyRowsErr)
	}
	// legacyFallback、emptyJSONFallback 保存两类历史行。
	var legacyFallback, emptyJSONFallback KeywordRow
	// legacyFallbackFound、emptyJSONFallbackFound 表示两类历史行是否已找到。
	var legacyFallbackFound, emptyJSONFallbackFound bool
	// row 表示当前遍历到的历史兼容关键词行。
	for _, row := range legacyRows {
		switch row.Keyword {
		case "历史缺列":
			legacyFallback = row
			legacyFallbackFound = true
		case "空 JSON":
			emptyJSONFallback = row
			emptyJSONFallbackFound = true
		}
	}
	if !legacyFallbackFound || legacyFallback.MatchType != "contains" || !reflect.DeepEqual(legacyFallback.Expressions, []string{"历史缺列"}) {
		t.Fatalf("legacy missing-column fallback=%+v found=%v", legacyFallback, legacyFallbackFound)
	}
	if !emptyJSONFallbackFound || emptyJSONFallback.MatchType != "contains" || !reflect.DeepEqual(emptyJSONFallback.Expressions, []string{"空 JSON"}) {
		t.Fatalf("empty JSON fallback=%+v found=%v", emptyJSONFallback, emptyJSONFallbackFound)
	}

	// updateErr 保存把多表达式规则更新为新首表达式和正则模式的结果。
	if updateErr := store.Keywords.UpdateByID(ctx, KeywordRow{
		ID: multiID, CookieID: cookieID, Keyword: "旧首值", Expressions: []string{"更新首", "更新次"}, MatchType: "regexp",
		Reply: "更新回复", Type: "text",
	}); updateErr != nil {
		t.Fatalf("UpdateByID expressions: %v", updateErr)
	}
	// updatedKeyword、updatedJSON、updatedMatch 保存更新后的原始数据库字段。
	var updatedKeyword, updatedJSON, updatedMatch string
	// updateScanErr 保存更新后字段读取错误。
	if updateScanErr := store.DB.QueryRowContext(ctx,
		`SELECT keyword, keyword_expressions, match_type FROM keywords WHERE id=?`, multiID).
		Scan(&updatedKeyword, &updatedJSON, &updatedMatch); updateScanErr != nil {
		t.Fatalf("read updated keyword storage: %v", updateScanErr)
	}
	if updatedKeyword != "更新首" || updatedMatch != "regexp" {
		t.Fatalf("updated storage keyword=%q match=%q", updatedKeyword, updatedMatch)
	}
	// updatedExpressions 保存更新后 JSON 文本反序列化出的表达式集合。
	var updatedExpressions []string
	// updatedDecodeErr 保存更新后 JSON 解析错误。
	if updatedDecodeErr := json.Unmarshal([]byte(updatedJSON), &updatedExpressions); updatedDecodeErr != nil {
		t.Fatalf("decode updated expressions: %v", updatedDecodeErr)
	}
	if !reflect.DeepEqual(updatedExpressions, []string{"更新首", "更新次"}) {
		t.Fatalf("updated expressions=%v", updatedExpressions)
	}

	// replacementRows 保存用于原子覆盖的两条规则，第二条刻意只填写旧 Keyword 字段。
	replacementRows := []KeywordRow{
		{CookieID: cookieID, Keyword: "替换首", Expressions: []string{"替换首", "替换次"}, MatchType: "regexp", Reply: "替换正则", Type: "text"},
		{CookieID: cookieID, Keyword: "兼容替换", Reply: "替换普通", Type: "text"},
	}
	// replaceErr 保存原子替换关键词规则的事务错误。
	if replaceErr := store.Keywords.ReplaceForCookie(ctx, cookieID, replacementRows); replaceErr != nil {
		t.Fatalf("ReplaceForCookie: %v", replaceErr)
	}
	// replacedRows、replacedRowsErr 保存替换后的全部规则。
	replacedRows, replacedRowsErr := store.Keywords.AllRows(ctx, cookieID)
	if replacedRowsErr != nil || len(replacedRows) != 2 {
		t.Fatalf("replaced rows=%+v err=%v", replacedRows, replacedRowsErr)
	}
	if replacedRows[0].Keyword != "替换首" || !reflect.DeepEqual(replacedRows[0].Expressions, []string{"替换首", "替换次"}) || replacedRows[0].MatchType != "regexp" {
		t.Fatalf("replaced expression row=%+v", replacedRows[0])
	}
	if replacedRows[1].Keyword != "兼容替换" || !reflect.DeepEqual(replacedRows[1].Expressions, []string{"兼容替换"}) || replacedRows[1].MatchType != "contains" {
		t.Fatalf("replaced legacy row=%+v", replacedRows[1])
	}
	// deleteErr 保存按主键删除替换规则的结果。
	if deleteErr := store.Keywords.DeleteByID(ctx, cookieID, replacedRows[0].ID); deleteErr != nil {
		t.Fatalf("DeleteByID: %v", deleteErr)
	}
	// deleteIndexErr 保存按稳定 ID 顺序删除剩余规则的结果。
	if deleteIndexErr := store.Keywords.DeleteByIndex(ctx, cookieID, 0); deleteIndexErr != nil {
		t.Fatalf("DeleteByIndex: %v", deleteIndexErr)
	}
	// missingUpdateErr 保存更新不存在规则时的兼容资源缺失错误。
	if missingUpdateErr := store.Keywords.UpdateByID(ctx, KeywordRow{ID: 999999, CookieID: cookieID, Keyword: "不存在", Reply: "不存在"}); !errors.Is(missingUpdateErr, ErrNotFound) {
		t.Fatalf("missing UpdateByID error=%v", missingUpdateErr)
	}
	// remainingRows、remainingErr 保存删除后的规则列表。
	remainingRows, remainingErr := store.Keywords.AllRows(ctx, cookieID)
	if remainingErr != nil || len(remainingRows) != 0 {
		t.Fatalf("remaining rows=%+v err=%v", remainingRows, remainingErr)
	}

	// sortCookieID 保存专门验证跨规则排序的独立账号标识。
	sortCookieID := cookieID + "-sort"
	// saveErr 保存排序测试账号的本地账号记录写入错误。
	if saveErr := store.Cookies.Save(ctx, sortCookieID, "cv", userID); saveErr != nil {
		t.Fatalf("save sort cookie: %v", saveErr)
	}
	// addSortErr 保存首表达式很短但后续表达式更长的规则写入错误。
	if _, addSortErr := store.Keywords.AddWithExpressions(ctx, sortCookieID, []string{"短", "后续表达式很长"}, "短规则", "", "text", "contains", ""); addSortErr != nil {
		t.Fatalf("add short sort rule: %v", addSortErr)
	}
	// addLongErr 保存首表达式较长规则的写入错误。
	if _, addLongErr := store.Keywords.Add(ctx, sortCookieID, "较长关键词", "长规则", "", "text", ""); addLongErr != nil {
		t.Fatalf("add long sort rule: %v", addLongErr)
	}
	// addTieFirstErr、addTieSecondErr 保存同长度规则的写入错误。
	if _, addTieFirstErr := store.Keywords.Add(ctx, sortCookieID, "甲乙", "同长一", "", "text", ""); addTieFirstErr != nil {
		t.Fatalf("add first tie rule: %v", addTieFirstErr)
	}
	// addTieSecondErr 保存第二条同长度规则的写入错误。
	if _, addTieSecondErr := store.Keywords.Add(ctx, sortCookieID, "丙丁", "同长二", "", "text", ""); addTieSecondErr != nil {
		t.Fatalf("add second tie rule: %v", addTieSecondErr)
	}
	// sortedKeywords、sortedErr 保存按历史优先级读取的关键词列表。
	sortedKeywords, sortedErr := store.Keywords.AllWithType(ctx, sortCookieID)
	if sortedErr != nil || len(sortedKeywords) != 4 {
		t.Fatalf("sorted keywords=%+v err=%v", sortedKeywords, sortedErr)
	}
	// sortedNames 保存排序结果中的兼容首表达式顺序。
	sortedNames := []string{sortedKeywords[0].Keyword, sortedKeywords[1].Keyword, sortedKeywords[2].Keyword, sortedKeywords[3].Keyword}
	if !reflect.DeepEqual(sortedNames, []string{"较长关键词", "甲乙", "丙丁", "短"}) {
		t.Fatalf("keyword order changed: %v", sortedNames)
	}
}

// TestMultiDBKeywordExpressionsCRUD 在 SQLite、MySQL 和 PostgreSQL 可用目标上复用关键词 CRUD 矩阵。
func TestMultiDBKeywordExpressionsCRUD(t *testing.T) {
	// target 表示当前可用的数据库方言测试目标。
	for _, target := range allTestTargets(t) {
		// target 保存当前子测试闭包独占的目标副本，避免循环变量复用。
		target := target
		// t 表示当前数据库方言的子测试上下文。
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// ctx 是当前方言测试共用的数据库上下文。
			ctx := context.Background()
			// suffix 保存当前方言测试使用的唯一后缀。
			suffix := fmt.Sprintf("%d", nextMultiDBTestID())
			// username 保存当前方言测试用户的非敏感标识。
			username := target.name + "_keyword_" + suffix
			// created、createErr 保存测试用户创建结果。
			if created, createErr := target.store.Users.Create(ctx, username, username+"@example.invalid", "pw"); createErr != nil || !created {
				t.Fatalf("create user: created=%v err=%v", created, createErr)
			}
			// user、userErr 保存测试用户摘要及查询错误。
			user, userErr := target.store.Users.GetByUsername(ctx, username)
			if userErr != nil {
				t.Fatalf("get user: %v", userErr)
			}
			// cookieID 保存当前方言测试账号的标识，不包含凭证内容。
			cookieID := target.name + "_keyword_cookie_" + suffix
			// saveErr 保存测试账号归属记录写入错误。
			if saveErr := target.store.Cookies.Save(ctx, cookieID, "cv", user.ID); saveErr != nil {
				t.Fatalf("save cookie: %v", saveErr)
			}
			exerciseKeywordExpressionCRUD(t, target.store, user.ID, cookieID)
		})
	}
}

// TestMultiDBKeywordExpressionsMigration 验证三方言 00052 迁移可回退到 00051 后重新升级到最新版本。
func TestMultiDBKeywordExpressionsMigration(t *testing.T) {
	// target 表示当前可用的数据库方言迁移目标。
	for _, target := range allTestTargets(t) {
		// target 保存当前迁移子测试闭包独占的目标副本。
		target := target
		// t 表示当前数据库方言的迁移子测试上下文。
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// subdir、gooseDialect 保存当前方言的嵌入迁移目录和 Goose 方言名。
			subdir, gooseDialect := migrationTestSubdir(t, target.dialect)
			// dialectErr 保存 Goose 方言设置错误。
			if dialectErr := goose.SetDialect(gooseDialect); dialectErr != nil {
				t.Fatalf("set goose dialect: %v", dialectErr)
			}
			goose.SetBaseFS(migrationsFS)
			// version、versionErr 保存回退前的迁移版本及读取错误。
			version, versionErr := goose.GetDBVersion(target.store.DB)
			if versionErr != nil || version != 52 {
				t.Fatalf("initial migration version=%d err=%v", version, versionErr)
			}
			// downErr 保存回退关键词表达式迁移的错误。
			if downErr := goose.DownTo(target.store.DB, "migrations/"+subdir, 51); downErr != nil {
				t.Fatalf("down keyword expression migration: %v", downErr)
			}
			if columnExistsForDialect(t, target.store.DB, target.dialect, "keywords", "keyword_expressions") || columnExistsForDialect(t, target.store.DB, target.dialect, "keywords", "match_type") {
				t.Fatal("keyword expression columns should be absent after down to 00051")
			}
			// upErr 保存重新应用关键词表达式迁移的错误。
			if upErr := goose.UpTo(target.store.DB, "migrations/"+subdir, 52); upErr != nil {
				t.Fatalf("up keyword expression migration: %v", upErr)
			}
			if !columnExistsForDialect(t, target.store.DB, target.dialect, "keywords", "keyword_expressions") || !columnExistsForDialect(t, target.store.DB, target.dialect, "keywords", "match_type") {
				t.Fatal("keyword expression columns should exist after up to 00052")
			}
			// finalVersion、finalVersionErr 保存重新升级后的最新迁移版本及读取错误。
			finalVersion, finalVersionErr := goose.GetDBVersion(target.store.DB)
			if finalVersionErr != nil || finalVersion != 52 {
				t.Fatalf("final migration version=%d err=%v", finalVersion, finalVersionErr)
			}
		})
	}
}

// nextMultiDBTestID 生成多数据库测试使用的单调后缀，复用既有矩阵计数器避免名称冲突。
func nextMultiDBTestID() uint64 {
	return atomic.AddUint64(&multidbCounter, 1)
}
