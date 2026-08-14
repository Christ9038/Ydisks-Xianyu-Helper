package db

import (
	"context"
	"errors"
	"testing"
)

// TestCookieScopedQueriesExcludeSecrets 验证摘要和所有权查询不会解密敏感字段。
func TestCookieScopedQueriesExcludeSecrets(t *testing.T) {
	// store 是当前测试使用的 SQLite repository 聚合器。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是测试数据库操作共用的上下文。
	ctx := context.Background()
	// ownerID 和 otherID 是两个互不相同的账号所有者。
	var ownerID, otherID int64
	// ownerCreateErr 表示创建 owner 测试用户失败的原因。
	if ownerCreateErr := store.DB.QueryRowContext(ctx,
		`INSERT INTO users (username,email,password_hash) VALUES (?,?,?) RETURNING id`,
		"scope-owner", "scope-owner@example.com", "test-hash").Scan(&ownerID); ownerCreateErr != nil {
		t.Fatalf("创建 owner: %v", ownerCreateErr)
	}
	// otherCreateErr 表示创建 other 测试用户失败的原因。
	if otherCreateErr := store.DB.QueryRowContext(ctx,
		`INSERT INTO users (username,email,password_hash) VALUES (?,?,?) RETURNING id`,
		"scope-other", "scope-other@example.com", "test-hash").Scan(&otherID); otherCreateErr != nil {
		t.Fatalf("创建 other: %v", otherCreateErr)
	}
	// insertErr 表示写入带有故意无效密文的测试账号失败原因。
	if _, insertErr := store.DB.ExecContext(ctx, `
		INSERT INTO cookies
			(id,value,user_id,auto_confirm,remark,pause_duration,username,password,show_browser,
			 nickname,avatar_url,metadata_json,last_refresh_at,login_method,last_login_at)
		VALUES ('scope-owned','not-a-ciphertext',?,1,'主账号',15,'login-user','not-a-password',1,
				'昵称','https://avatar.invalid/a', 'not-a-metadata',123,'password',456)`, ownerID); insertErr != nil {
		t.Fatalf("创建 owner cookie: %v", insertErr)
	}
	// otherInsertErr 表示写入 other 测试账号失败的原因。
	if _, otherInsertErr := store.DB.ExecContext(ctx,
		`INSERT INTO cookies (id,value,user_id) VALUES ('scope-other','other-cookie',?)`, otherID); otherInsertErr != nil {
		t.Fatalf("创建 other cookie: %v", otherInsertErr)
	}
	// summaries 是 owner 的非敏感摘要，即使 value/password/metadata 不是合法密文也应可读取。
	summaries, summaryErr := store.Cookies.ListSummaries(ctx, ownerID)
	if summaryErr != nil {
		t.Fatalf("ListSummaries: %v", summaryErr)
	}
	if len(summaries) != 1 || summaries[0].ID != "scope-owned" || summaries[0].UserID != ownerID {
		t.Fatalf("摘要范围错误: %#v", summaries)
	}
	// summary 是按账号和用户联合过滤得到的单账号非敏感摘要。
	summary, summaryLookupErr := store.Cookies.GetSummaryOwned(ctx, ownerID, "scope-owned")
	if summaryLookupErr != nil || summary.ID != "scope-owned" || summary.Remark != "主账号" {
		t.Fatalf("GetSummaryOwned: summary=%#v err=%v", summary, summaryLookupErr)
	}
	// crossSummaryErr 表示其他用户不能通过摘要查询读取该账号。
	if _, crossSummaryErr := store.Cookies.GetSummaryOwned(ctx, otherID, "scope-owned"); !errors.Is(crossSummaryErr, ErrNotFound) {
		t.Fatalf("GetSummaryOwned cross-owner 应 ErrNotFound, err=%v", crossSummaryErr)
	}
	if !summaries[0].AutoConfirm || summaries[0].PauseDuration != 15 || summaries[0].Username != "login-user" {
		t.Fatalf("摘要字段错误: %#v", summaries[0])
	}
	// cookieIDs 是 owner 的所有权 ID 列表，不应包含 other 的账号。
	cookieIDs, idsErr := store.Cookies.ListOwnedIDs(ctx, ownerID)
	if idsErr != nil || len(cookieIDs) != 1 || cookieIDs[0] != "scope-owned" {
		t.Fatalf("ListOwnedIDs: ids=%v err=%v", cookieIDs, idsErr)
	}
	// owned 表示 owner 对自己的账号拥有权限；otherOwned 应明确为 false。
	owned, ownedErr := store.Cookies.ExistsOwned(ctx, ownerID, "scope-owned")
	if ownedErr != nil || !owned {
		t.Fatalf("ExistsOwned owner: owned=%v err=%v", owned, ownedErr)
	}
	// otherOwned 和 otherErr 表示 owner 对 other 账号的所有权结果及查询错误。
	otherOwned, otherErr := store.Cookies.ExistsOwned(ctx, ownerID, "scope-other")
	if otherErr != nil || otherOwned {
		t.Fatalf("ExistsOwned cross-owner: owned=%v err=%v", otherOwned, otherErr)
	}
	// ownerOfCookie 是不读取凭证字段即可确认账号归属的结果。
	ownerOfCookie, ownerLookupErr := store.Cookies.GetOwnerID(ctx, "scope-owned")
	if ownerLookupErr != nil || ownerOfCookie != ownerID {
		t.Fatalf("GetOwnerID: owner=%d err=%v", ownerOfCookie, ownerLookupErr)
	}
	// readableCookieID 是通过正常加密流程创建的账号，用于验证单值凭证读取。
	const readableCookieID = "scope-readable"
	// saveErr 表示创建可解密测试账号失败的原因。
	if saveErr := store.Cookies.Save(ctx, readableCookieID, "readable-cookie", ownerID); saveErr != nil {
		t.Fatalf("创建可读 cookie: %v", saveErr)
	}
	// readableValue 是 owner 读取自己账号时得到的 Cookie 明文。
	readableValue, readableErr := store.Cookies.GetValueOwned(ctx, ownerID, readableCookieID)
	if readableErr != nil || readableValue != "readable-cookie" {
		t.Fatalf("GetValueOwned owner: value=%q err=%v", readableValue, readableErr)
	}
	// wrongOwnerErr 表示其他用户不能读取该账号的 Cookie 密文。
	if _, wrongOwnerErr := store.Cookies.GetValueOwned(ctx, otherID, readableCookieID); !errors.Is(wrongOwnerErr, ErrNotFound) {
		t.Fatalf("GetValueOwned cross-owner 应 ErrNotFound, err=%v", wrongOwnerErr)
	}
	// missingOwnerErr 表示不存在账号的所有者查询应返回统一的未找到错误。
	if _, missingOwnerErr := store.Cookies.GetOwnerID(ctx, "scope-missing"); !errors.Is(missingOwnerErr, ErrNotFound) {
		t.Fatalf("GetOwnerID missing 应 ErrNotFound, err=%v", missingOwnerErr)
	}
	// invalidListErr 表示 userID=0 被所有权列表查询拒绝的错误。
	if _, invalidListErr := store.Cookies.ListOwnedIDs(ctx, 0); invalidListErr != ErrInvalidUserID {
		t.Fatalf("userID=0 应拒绝隐式管理员查询，err=%v", invalidListErr)
	}
	// invalidExistsErr 表示 userID=0 被所有权存在性查询拒绝的错误。
	if _, invalidExistsErr := store.Cookies.ExistsOwned(ctx, 0, "scope-owned"); invalidExistsErr != ErrInvalidUserID {
		t.Fatalf("ExistsOwned userID=0 应拒绝，err=%v", invalidExistsErr)
	}
	// invalidValueErr 表示 userID=0 被单值凭证查询拒绝的错误。
	if _, invalidValueErr := store.Cookies.GetValueOwned(ctx, 0, "scope-owned"); invalidValueErr != ErrInvalidUserID {
		t.Fatalf("GetValueOwned userID=0 应拒绝，err=%v", invalidValueErr)
	}
}

// TestListEnabledRuntimeCredentials 只返回启用账号的 Cookie，并验证不会因其他敏感字段损坏而扩大读取范围。
func TestListEnabledRuntimeCredentials(t *testing.T) {
	// store 是当前测试使用的 SQLite repository 聚合器。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是测试数据库操作共用的上下文。
	ctx := context.Background()
	// ownerID 是测试账号所属用户，用于创建两条账号记录。
	var ownerID int64
	// ownerCreateErr 表示创建测试用户失败的原因。
	if ownerCreateErr := store.DB.QueryRowContext(ctx,
		`INSERT INTO users (username,email,password_hash) VALUES (?,?,?) RETURNING id`,
		"runtime-owner", "runtime-owner@example.com", "test-hash").Scan(&ownerID); ownerCreateErr != nil {
		t.Fatalf("创建测试用户: %v", ownerCreateErr)
	}
	// enabledSaveErr 表示创建启用账号失败的原因。
	if enabledSaveErr := store.Cookies.Save(ctx, "runtime-enabled", "runtime-cookie", ownerID); enabledSaveErr != nil {
		t.Fatalf("创建启用账号: %v", enabledSaveErr)
	}
	// disabledSaveErr 表示创建禁用账号失败的原因。
	if disabledSaveErr := store.Cookies.Save(ctx, "runtime-disabled", "disabled-cookie", ownerID); disabledSaveErr != nil {
		t.Fatalf("创建禁用账号: %v", disabledSaveErr)
	}
	// statusErr 表示将测试账号标记为禁用失败的原因。
	if statusErr := store.Cookies.SetStatus(ctx, "runtime-disabled", false); statusErr != nil {
		t.Fatalf("禁用账号: %v", statusErr)
	}
	// corruptErr 表示写入故意损坏的密码和 metadata 测试值失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET password=?, metadata_json=? WHERE id=?`,
		"not-a-password-ciphertext", "not-a-metadata-ciphertext", "runtime-enabled"); corruptErr != nil {
		t.Fatalf("损坏非运行时字段: %v", corruptErr)
	}
	// credentials 是只应包含启用账号的最小运行时凭证集合。
	credentials, listErr := store.Cookies.ListEnabledRuntimeCredentials(ctx)
	if listErr != nil {
		t.Fatalf("ListEnabledRuntimeCredentials: %v", listErr)
	}
	if len(credentials) != 1 || credentials[0].ID != "runtime-enabled" || credentials[0].Value != "runtime-cookie" {
		t.Fatalf("运行时凭证范围或值错误: %#v", credentials)
	}
}

// TestGetCookieRuntimeDataExcludesLoginSecrets 验证运行时窄查询只解密 Cookie 与 metadata，不读取损坏的登录密码。
func TestGetCookieRuntimeDataExcludesLoginSecrets(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "fingerprint-query-key")
	// store 是当前测试使用的 SQLite repository 聚合器。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是测试数据库操作共用的上下文。
	ctx := context.Background()
	// createErr 表示创建测试用户失败的原因。
	if ok, createErr := store.Users.Create(ctx, "fingerprint-owner", "fingerprint-owner@example.com", "pw"); createErr != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, createErr)
	}
	// owner 是测试账号的所有者。
	owner, ownerErr := store.Users.GetByUsername(ctx, "fingerprint-owner")
	if ownerErr != nil {
		t.Fatalf("get owner: %v", ownerErr)
	}
	// saveErr 表示创建测试账号失败的原因。
	if saveErr := store.Cookies.CreateOwned(ctx, "fingerprint-cookie", "sid=fingerprint", owner.ID); saveErr != nil {
		t.Fatalf("create cookie: %v", saveErr)
	}
	// metadata 是指纹查询应读取的合法运行 metadata。
	metadata := `{"jar":"fingerprint"}`
	// updateErr 表示写入测试 Cookie 与 metadata 失败的原因。
	if updateErr := store.Cookies.UpdateRenewalCookie(ctx, "fingerprint-cookie", "sid=fingerprint", metadata, 1); updateErr != nil {
		t.Fatalf("update cookie: %v", updateErr)
	}
	// corruptErr 表示写入故意损坏的登录密码失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET username=?,password=? WHERE id=?`,
		"fingerprint-user", "not-a-password-ciphertext", "fingerprint-cookie"); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}
	// data 是不受登录密码损坏影响的最小运行时输入。
	data, dataErr := store.Cookies.GetCookieRuntimeData(ctx, "fingerprint-cookie")
	if dataErr != nil || data.Value != "sid=fingerprint" || data.MetadataJSON != metadata {
		t.Fatalf("fingerprint data=%+v err=%v", data, dataErr)
	}
	// corruptValueErr 表示将 Cookie 明文密文损坏，用于证明 metadata 单值查询不会读取 Cookie。
	if _, corruptValueErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET value=?,password=? WHERE id=?`,
		"not-a-cookie-ciphertext", "not-a-password-ciphertext", "fingerprint-cookie"); corruptValueErr != nil {
		t.Fatalf("corrupt cookie: %v", corruptValueErr)
	}
	// metadataOnly 是只解密 metadata 的窄查询结果。
	metadataOnly, metadataOnlyErr := store.Cookies.GetCookieMetadata(ctx, "fingerprint-cookie")
	if metadataOnlyErr != nil || metadataOnly != metadata {
		t.Fatalf("metadata-only data=%q err=%v", metadataOnly, metadataOnlyErr)
	}
	// missingErr 表示不存在账号应返回统一的未找到错误。
	if _, missingErr := store.Cookies.GetCookieRuntimeData(ctx, "fingerprint-missing"); !errors.Is(missingErr, ErrNotFound) {
		t.Fatalf("missing fingerprint data err=%v", missingErr)
	}
	// missingMetadataErr 表示不存在账号的 metadata 查询应返回统一的未找到错误。
	if _, missingMetadataErr := store.Cookies.GetCookieMetadata(ctx, "fingerprint-missing"); !errors.Is(missingMetadataErr, ErrNotFound) {
		t.Fatalf("missing metadata err=%v", missingMetadataErr)
	}
}
