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

// TestMultiDBKeywordExpressionsMigration 验证三方言 00054 迁移可回退到 00053 后重新升级到最新版本。
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
			if versionErr != nil || version != 55 {
				t.Fatalf("initial migration version=%d err=%v", version, versionErr)
			}
			// downErr 保存回退关键词表达式迁移的错误。
			if downErr := goose.DownTo(target.store.DB, "migrations/"+subdir, 53); downErr != nil {
				t.Fatalf("down keyword expression migration: %v", downErr)
			}
			if columnExistsForDialect(t, target.store.DB, target.dialect, "keywords", "keyword_expressions") || columnExistsForDialect(t, target.store.DB, target.dialect, "keywords", "match_type") {
				t.Fatal("keyword expression columns should be absent after down to 00053")
			}
			// upErr 保存重新应用关键词表达式迁移的错误。
			if upErr := goose.UpTo(target.store.DB, "migrations/"+subdir, 54); upErr != nil {
				t.Fatalf("up keyword expression migration: %v", upErr)
			}
			if !columnExistsForDialect(t, target.store.DB, target.dialect, "keywords", "keyword_expressions") || !columnExistsForDialect(t, target.store.DB, target.dialect, "keywords", "match_type") {
				t.Fatal("keyword expression columns should exist after up to 00054")
			}
			// finalVersion、finalVersionErr 保存重新升级后的最新迁移版本及读取错误。
			finalVersion, finalVersionErr := goose.GetDBVersion(target.store.DB)
			if finalVersionErr != nil || finalVersion != 54 {
				t.Fatalf("final migration version=%d err=%v", finalVersion, finalVersionErr)
			}
		})
	}
}

// nextMultiDBTestID 生成多数据库测试使用的单调后缀，复用既有矩阵计数器避免名称冲突。
func nextMultiDBTestID() uint64 {
	return atomic.AddUint64(&multidbCounter, 1)
}

// TestKeywordExpressionCodecRules 验证表达式 JSON 编解码规则：
// 解码时非法或空数据回退历史单关键词，去空白并按首次出现顺序去重；编码时丢弃空白项、无有效项时回退 fallback；
// 持久化字段以第一条表达式同步兼容的 keyword 列，匹配模式统一小写且空值为 contains。
func TestKeywordExpressionCodecRules(t *testing.T) {
	// decodeCases 描述解码输入及期望结果。
	decodeCases := []struct {
		// name 是子用例说明。
		name string
		// raw、fallback 是 JSON 文本与历史关键词。
		raw, fallback string
		// want 是期望的表达式集合。
		want []string
	}{
		{"空 JSON 回退历史关键词", "", " 旧词 ", []string{"旧词"}},
		{"纯空白 JSON 回退历史关键词", "   ", "旧词", []string{"旧词"}},
		{"非法 JSON 回退历史关键词", "{坏", "旧词", []string{"旧词"}},
		{"JSON 为字符串而非数组回退", `"字符串"`, "旧词", []string{"旧词"}},
		{"去空白并去重保持顺序", `[" 乙 ","甲","乙",""]`, "旧词", []string{"乙", "甲"}},
		{"数组全为空白回退历史关键词", `["  ",""]`, "旧词", []string{"旧词"}},
		{"没有任何可用内容", "[]", "  ", nil},
	}
	// tc 是当前解码场景。
	for _, tc := range decodeCases {
		// got 保存 decodeKeywordExpressions 调用的返回结果，供随后的断言使用。
		if got := decodeKeywordExpressions(tc.raw, tc.fallback); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("decode %s: got=%v want=%v", tc.name, got, tc.want)
		}
	}
	// encodeCases 描述编码输入及期望的 JSON 文本。
	encodeCases := []struct {
		// name 是子用例说明。
		name string
		// expressions、fallback 是待编码集合与回退关键词。
		expressions []string
		fallback    string
		// want 是期望的 JSON 文本。
		want string
	}{
		{"去空白去重", []string{" 甲 ", "甲", "乙"}, "", `["甲","乙"]`},
		{"全部空白时回退 fallback", []string{" ", ""}, "旧词", `["旧词"]`},
		{"没有任何内容编码为空数组", nil, "", `[]`},
		{"保留特殊字符不转义中文", []string{`a"b`, "中文"}, "", `["a\"b","中文"]`},
	}
	// tc 是当前编码场景。
	for _, tc := range encodeCases {
		// got 保存 encodeKeywordExpressions 调用的返回结果，供随后的断言使用。
		if got := encodeKeywordExpressions(tc.expressions, tc.fallback); got != tc.want {
			t.Errorf("encode %s: got=%s want=%s", tc.name, got, tc.want)
		}
	}
	// keyword、encoded、matchType 是持久化字段：keyword 取第一条规范化表达式，模式统一小写。
	keyword, encoded, matchType := keywordPersistenceFields("旧", []string{" 首 ", "次"}, " REGEXP ")
	if keyword != "首" || encoded != `["首","次"]` || matchType != "regexp" {
		t.Fatalf("持久化字段异常: keyword=%q encoded=%s match=%q", keyword, encoded, matchType)
	}
	// 没有表达式集合时沿用历史 keyword，空模式规整为 contains。
	keyword, encoded, matchType = keywordPersistenceFields("只有旧词", nil, "")
	if keyword != "只有旧词" || encoded != `["只有旧词"]` || matchType != "contains" {
		t.Fatalf("历史单值写入异常: keyword=%q encoded=%s match=%q", keyword, encoded, matchType)
	}
}

// TestKeywordRowsIsolateAccountsAndReportMissingRules 验证按主键更新、删除和按序号删除只作用于所属账号：
// 其他账号的同主键请求以 ErrNotFound 失败且不改动数据；无变化的更新不会被误判为不存在。
func TestKeywordRowsIsolateAccountsAndReportMissingRules(t *testing.T) {
	// store、cleanup 是隔离 SQLite 存储及其释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// userID、cookieID 是测试账号归属。
	userID, cookieID := seedAccount(t, store)
	_ = userID
	// ctx 是本测试共用的上下文。
	ctx := context.Background()
	// ruleID、addErr 是新增规则的主键与错误。
	ruleID, addErr := store.Keywords.AddWithExpressions(ctx, cookieID, []string{"甲", "乙"}, "回复", "", "text", "contains", "")
	if addErr != nil {
		t.Fatal(addErr)
	}
	// original 是更新前读取的规则行。
	rows, rowsErr := store.Keywords.AllRows(ctx, cookieID)
	if rowsErr != nil || len(rows) != 1 {
		t.Fatalf("AllRows: rows=%v err=%v", rows, rowsErr)
	}
	// 其他账号更新同主键必须 ErrNotFound，原数据不变。
	foreign := rows[0]
	foreign.CookieID = "another-account"
	foreign.Reply = "被篡改"
	// err 保存 UpdateByID 调用返回的错误，随后断言它是否符合该场景的预期。
	if err := store.Keywords.UpdateByID(ctx, foreign); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨账号更新应 ErrNotFound，got %v", err)
	}
	// 不存在的主键同样 ErrNotFound。
	missing := rows[0]
	missing.ID = ruleID + 1000
	// err 保存 UpdateByID 调用返回的错误，随后断言它是否符合该场景的预期。
	if err := store.Keywords.UpdateByID(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的规则更新应 ErrNotFound，got %v", err)
	}
	// 使用完全相同的内容再次更新（无变化）必须成功。
	if err := store.Keywords.UpdateByID(ctx, rows[0]); err != nil {
		t.Fatalf("无变化更新不应失败: %v", err)
	}
	// 跨账号删除与不存在的删除、越界序号删除都返回 ErrNotFound。
	if err := store.Keywords.DeleteByID(ctx, "another-account", ruleID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨账号删除应 ErrNotFound，got %v", err)
	}
	// err 保存 DeleteByID 调用返回的错误，随后断言它是否符合该场景的预期。
	if err := store.Keywords.DeleteByID(ctx, cookieID, ruleID+1000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的规则删除应 ErrNotFound，got %v", err)
	}
	// err 保存 DeleteByIndex 调用返回的错误，随后断言它是否符合该场景的预期。
	if err := store.Keywords.DeleteByIndex(ctx, cookieID, 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("越界序号删除应 ErrNotFound，got %v", err)
	}
	// err 保存 DeleteByIndex 调用返回的错误，随后断言它是否符合该场景的预期。
	if err := store.Keywords.DeleteByIndex(ctx, cookieID, -1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("负序号删除应 ErrNotFound，got %v", err)
	}
	// 以上失败操作后规则内容保持不变。
	after, afterErr := store.Keywords.AllRows(ctx, cookieID)
	if afterErr != nil || len(after) != 1 || after[0].Reply != "回复" || !reflect.DeepEqual(after[0].Expressions, []string{"甲", "乙"}) {
		t.Fatalf("失败操作后数据被改动: %+v err=%v", after, afterErr)
	}
	// ReplaceForCookie 以空集合清空账号规则，且不影响其他账号。
	if err := store.Keywords.ReplaceForCookie(ctx, cookieID, nil); err != nil {
		t.Fatal(err)
	}
	// cleared、err 保存 AllRows 调用的返回值及其错误，随后断言是否符合该场景的预期。
	if cleared, err := store.Keywords.AllRows(ctx, cookieID); err != nil || len(cleared) != 0 {
		t.Fatalf("空集合替换应清空规则: %+v err=%v", cleared, err)
	}
}

// TestKeywordRowsReportDatabaseErrors 验证数据库不可用时关键词仓储的每个入口都返回错误，不会返回部分结果或伪装成功。
func TestKeywordRowsReportDatabaseErrors(t *testing.T) {
	// store、cleanup 是隔离存储；seedAccount 之后主动关闭连接池模拟故障。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// cookieID 保存 seedAccount 调用的返回结果，供随后的断言使用。
	_, cookieID := seedAccount(t, store)
	// ctx 是本测试共用的上下文。
	ctx := context.Background()
	// err 保存 Close 调用返回的错误，随后断言它是否符合该场景的预期。
	if err := store.DB.Close(); err != nil {
		t.Fatal(err)
	}
	// err 保存 UpdateByID 调用返回的错误，随后断言它是否符合该场景的预期。
	if err := store.Keywords.UpdateByID(ctx, KeywordRow{ID: 1, CookieID: cookieID, Keyword: "甲", Reply: "回复"}); err == nil {
		t.Error("UpdateByID 数据库故障必须返回错误")
	}
	// err 保存 DeleteByID 调用返回的错误，随后断言它是否符合该场景的预期。
	if err := store.Keywords.DeleteByID(ctx, cookieID, 1); err == nil {
		t.Error("DeleteByID 数据库故障必须返回错误")
	}
	// err、rows 保存 AllRows 调用的返回值及其错误，随后断言是否符合该场景的预期。
	if rows, err := store.Keywords.AllRows(ctx, cookieID); err == nil || rows != nil {
		t.Errorf("AllRows 数据库故障必须返回错误且无结果: rows=%v err=%v", rows, err)
	}
	// err 保存 ReplaceForCookie 调用返回的错误，随后断言它是否符合该场景的预期。
	if err := store.Keywords.ReplaceForCookie(ctx, cookieID, []KeywordRow{{Keyword: "甲", Reply: "回复"}}); err == nil {
		t.Error("ReplaceForCookie 数据库故障必须返回错误")
	}
	// err 保存 DeleteByIndex 调用返回的错误，随后断言它是否符合该场景的预期。
	if err := store.Keywords.DeleteByIndex(ctx, cookieID, 0); err == nil {
		t.Error("DeleteByIndex 数据库故障必须返回错误")
	}
	// err、rows 保存 AllWithType 调用的返回值及其错误，随后断言是否符合该场景的预期。
	if rows, err := store.Keywords.AllWithType(ctx, cookieID); err == nil || rows != nil {
		t.Errorf("AllWithType 数据库故障必须返回错误且无结果: rows=%v err=%v", rows, err)
	}
}
