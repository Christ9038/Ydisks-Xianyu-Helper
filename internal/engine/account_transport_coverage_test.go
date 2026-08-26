package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// TestTokenFailureClassification 覆盖连接令牌失败分类和不计数状态判定。
func TestTokenFailureClassification(t *testing.T) {
	// sessionError 保存平台明确返回的会话失效错误。
	sessionError := &mtop.SessionExpiredError{API: "test"}
	// cases 保存错误输入及其稳定状态分类。
	cases := []struct {
		err  error
		want string
	}{
		{nil, tokenRefreshFailedAPI},
		{sessionError, tokenRefreshFailedSession},
		{context.DeadlineExceeded, tokenRefreshFailedTimeout},
		{errors.New("network unavailable"), tokenRefreshFailedNetwork},
		{errors.New("请求失败"), tokenRefreshFailedNetwork},
		{errors.New("业务拒绝"), tokenRefreshFailedAPI},
	}
	// item 表示当前待分类的令牌错误样例。
	for _, item := range cases {
		// got 保存错误分类函数返回的稳定状态。
		if got := classifyTokenFailure(item.err); got != item.want {
			t.Errorf("classifyTokenFailure(%v)=%q want %q", item.err, got, item.want)
		}
	}
	// statuses 保存不应增加连续失败计数的特殊状态。
	statuses := []string{tokenRefreshFailedCaptcha, tokenRefreshFailedCaptchaError, tokenRefreshSkippedCooldown}
	// status 表示当前待判断的令牌状态。
	for _, status := range statuses {
		if !tokenFailureIsNonCounted(status) {
			t.Errorf("status %q should be non-counted", status)
		}
	}
	if tokenFailureIsNonCounted(tokenRefreshFailedAPI) {
		t.Fatal("API failure should be counted")
	}
}

// TestTokenRetryDelayCoversFailureAndExpiryRules 验证令牌重试延迟对失败次数和即将过期凭证的优先级。
func TestTokenRetryDelayCoversFailureAndExpiryRules(t *testing.T) {
	// account 保存令牌延迟计算使用的账号运行时。
	account := New(Config{CookieStr: "unb=token-delay"})
	// firstDelay 保存首次获取令牌失败后的默认延迟。
	firstDelay := account.tokenRetryDelay()
	if firstDelay != time.Minute {
		t.Fatalf("首次令牌延迟=%v", firstDelay)
	}
	account.mu.Lock()
	account.tokenFetchFailures = 2
	account.mu.Unlock()
	// repeatedDelay 保存连续失败后的退避延迟。
	repeatedDelay := account.tokenRetryDelay()
	if repeatedDelay != 2*time.Minute {
		t.Fatalf("连续失败令牌延迟=%v", repeatedDelay)
	}
	account.mu.Lock()
	account.tokenExpiresAt = time.Now().Add(time.Minute)
	account.mu.Unlock()
	// expiringDelay 保存凭证即将过期时覆盖普通退避的短延迟。
	expiringDelay := account.tokenRetryDelay()
	if expiringDelay != 30*time.Second {
		t.Fatalf("即将过期令牌延迟=%v", expiringDelay)
	}
	// expiredAccount 保存令牌已过期且尚未建立聊天连接的账号运行时。
	expiredAccount := New(Config{CookieStr: "unb=expired-token"})
	expiredAccount.mu.Lock()
	expiredAccount.tokenExpiresAt = time.Now().Add(-time.Minute)
	expiredAccount.mu.Unlock()
	// expiredStatus 保存过期令牌的运行状态快照。
	expiredStatus := expiredAccount.RuntimeStatus()
	if expiredStatus.TokenRemainingSeconds != 0 {
		t.Fatalf("过期令牌剩余时间=%d", expiredStatus.TokenRemainingSeconds)
	}
	// readErr 保存未建立 WebSocket 时已读上报的生命周期错误。
	if readErr := expiredAccount.MarkChatRead(context.Background(), "chat", nil); readErr == nil {
		t.Fatal("未连接账号的已读上报应返回错误")
	}
}

// TestCookieSnapshotMatchesDBCoversCredentialBoundaries 验证 WebSocket 注册前凭证快照的缺失、空值、匹配和变化路径。
func TestCookieSnapshotMatchesDBCoversCredentialBoundaries(t *testing.T) {
	// noStoreAccount 是无数据库兼容模式下的账号。
	noStoreAccount := New(Config{CookieID: "no-store-snapshot", CookieStr: "unb=1"})
	if !noStoreAccount.cookieSnapshotMatchesDB(context.Background(), "") {
		t.Fatal("无数据库账号应跳过快照校验")
	}
	// account、handler、store、cleanup 保存带数据库账号及其测试资源。
	account, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// runtimeData 保存数据库中的当前凭证运行时快照。
	runtimeData, readErr := store.Cookies.GetCookieRuntimeData(context.Background(), account.CookieID)
	if readErr != nil {
		t.Fatalf("读取凭证快照失败：%v", readErr)
	}
	// matchingFingerprint 保存与当前数据库凭证一致的状态指纹。
	matchingFingerprint := credentialStateFingerprint(runtimeData.Value, runtimeData.MetadataJSON)
	if !account.cookieSnapshotMatchesDB(context.Background(), matchingFingerprint) {
		t.Fatal("一致凭证快照应通过校验")
	}
	if account.cookieSnapshotMatchesDB(context.Background(), "different-fingerprint") {
		t.Fatal("不同凭证指纹不应通过校验")
	}
	if account.cookieSnapshotMatchesDB(context.Background(), "") {
		t.Fatal("缺少 Token 绑定指纹不应通过校验")
	}

	// missingAccount 是数据库中不存在的账号，用于覆盖窄查询失败路径。
	missingAccount := New(Config{CookieID: "missing-snapshot", Store: store, CookieStr: "unb=missing"})
	if missingAccount.cookieSnapshotMatchesDB(context.Background(), matchingFingerprint) {
		t.Fatal("不存在账号不应通过凭证快照校验")
	}

	// emptyMetadataJSON 是没有权威 Jar 的空 Cookie 元数据文本。
	emptyMetadataJSON := `{}`
	// emptyPersistErr 表示保存空 Cookie 运行时状态的错误。
	emptyPersistErr := store.Cookies.UpdateRenewalCookie(context.Background(), account.CookieID, "", emptyMetadataJSON, time.Now().Unix())
	if emptyPersistErr != nil {
		t.Fatalf("保存空 Cookie 失败：%v", emptyPersistErr)
	}
	if account.cookieSnapshotMatchesDB(context.Background(), credentialStateFingerprint("", emptyMetadataJSON)) {
		t.Fatal("空 Cookie 且没有权威 Jar 不应通过校验")
	}
	// authoritativeSnapshot 保存允许空扁平 Cookie 的权威浏览器快照。
	authoritativeSnapshot := []cookierefresh.BrowserCookie{{Name: "unb", Value: "1", Domain: ".goofish.com", Path: "/"}}
	// authoritativeMetadata 保存带完整 Jar 的凭证元数据。
	authoritativeMetadata := cookierefresh.MetadataWithSnapshot(`{}`, authoritativeSnapshot)
	// authoritativePersistErr 表示保存权威 Jar 的错误。
	authoritativePersistErr := store.Cookies.UpdateRenewalCookie(context.Background(), account.CookieID, "", authoritativeMetadata, time.Now().Unix())
	if authoritativePersistErr != nil {
		t.Fatalf("保存权威 Jar 失败：%v", authoritativePersistErr)
	}
	if !account.cookieSnapshotMatchesDB(context.Background(), credentialStateFingerprint("", authoritativeMetadata)) {
		t.Fatal("空扁平 Cookie 但有权威 Jar 应通过校验")
	}
}

// TestCredentialFingerprintAndReloadCookieBoundaries 验证 Token 凭证指纹读取与数据库 Cookie 同步的边界路径。
func TestCredentialFingerprintAndReloadCookieBoundaries(t *testing.T) {
	// noStoreAccount 是无数据库模式下用于计算本地指纹的账号。
	noStoreAccount := New(Config{CookieID: "no-store-fingerprint", CookieStr: "unb=local"})
	// localFingerprint 保存无数据库模式直接计算出的凭证指纹。
	localFingerprint, localErr := noStoreAccount.databaseCredentialFingerprint(context.Background(), "unb=local")
	if localErr != nil || localFingerprint == "" {
		t.Fatalf("无数据库指纹结果异常：fingerprint=%q err=%v", localFingerprint, localErr)
	}

	// account、handler、store、cleanup 保存带数据库账号及其测试资源。
	account, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// runtimeData 保存数据库中的当前凭证运行时数据。
	runtimeData, readErr := store.Cookies.GetCookieRuntimeData(context.Background(), account.CookieID)
	if readErr != nil {
		t.Fatalf("读取凭证运行时数据失败：%v", readErr)
	}
	// databaseFingerprint 保存数据库当前 Cookie 状态的预期指纹。
	databaseFingerprint := credentialStateFingerprint(runtimeData.Value, runtimeData.MetadataJSON)
	// fingerprint、fingerprintErr 保存数据库指纹读取结果。
	fingerprint, fingerprintErr := account.databaseCredentialFingerprint(context.Background(), runtimeData.Value)
	if fingerprintErr != nil || fingerprint != databaseFingerprint {
		t.Fatalf("数据库指纹结果异常：got=%q want=%q err=%v", fingerprint, databaseFingerprint, fingerprintErr)
	}
	// missingAccount 是数据库中不存在的账号，用于覆盖查询失败。
	missingAccount := New(Config{CookieID: "missing-fingerprint", Store: store, CookieStr: "unb=missing"})
	// missingFingerprint、missingErr 保存不存在账号的指纹查询结果。
	missingFingerprint, missingErr := missingAccount.databaseCredentialFingerprint(context.Background(), "unb=missing")
	if missingFingerprint != "" || missingErr == nil {
		t.Fatal("不存在账号应返回指纹查询错误")
	}
	// changedFingerprint、changedErr 保存请求 Cookie 已被并发替换时的结果。
	changedFingerprint, changedErr := account.databaseCredentialFingerprint(context.Background(), "unb=changed")
	if changedFingerprint != "" || changedErr == nil {
		t.Fatalf("Cookie 变化应返回错误：fingerprint=%q err=%v", changedFingerprint, changedErr)
	}

	// emptyMetadataJSON 保存没有权威 Jar 的空 Cookie 元数据。
	emptyMetadataJSON := `{}`
	// emptyPersistErr 表示保存空 Cookie 状态的错误。
	emptyPersistErr := store.Cookies.UpdateRenewalCookie(context.Background(), account.CookieID, "", emptyMetadataJSON, time.Now().Unix())
	if emptyPersistErr != nil {
		t.Fatalf("保存空 Cookie 失败：%v", emptyPersistErr)
	}
	// emptyFingerprint、emptyErr 保存空 Cookie 且无权威 Jar 的指纹结果。
	emptyFingerprint, emptyErr := account.databaseCredentialFingerprint(context.Background(), "")
	if emptyFingerprint != "" || emptyErr == nil {
		t.Fatal("空 Cookie 且无权威 Jar 应返回错误")
	}
	// authoritativeSnapshot 保存允许空平面 Cookie 的完整 Jar。
	authoritativeSnapshot := []cookierefresh.BrowserCookie{{Name: "unb", Value: "1", Domain: ".goofish.com", Path: "/"}}
	// authoritativeMetadata 保存完整 Jar 元数据。
	authoritativeMetadata := cookierefresh.MetadataWithSnapshot(emptyMetadataJSON, authoritativeSnapshot)
	// authoritativePersistErr 表示保存完整 Jar 的错误。
	authoritativePersistErr := store.Cookies.UpdateRenewalCookie(context.Background(), account.CookieID, "", authoritativeMetadata, time.Now().Unix())
	if authoritativePersistErr != nil {
		t.Fatalf("保存权威 Jar 失败：%v", authoritativePersistErr)
	}
	// authoritativeFingerprint、authoritativeErr 保存完整 Jar 的指纹结果。
	authoritativeFingerprint, authoritativeErr := account.databaseCredentialFingerprint(context.Background(), "")
	if authoritativeFingerprint == "" || authoritativeErr != nil {
		t.Fatalf("权威 Jar 应允许空平面 Cookie：%v", authoritativeErr)
	}

	// noStoreReloadAccount 是无数据库模式下不会同步 Cookie 的账号。
	noStoreReloadAccount := New(Config{CookieID: "no-store-reload", CookieStr: "unb=1"})
	if noStoreReloadAccount.reloadCookieFromDB(context.Background()) {
		t.Fatal("无数据库账号不应报告 Cookie 已同步")
	}
	// missingReloadAccount 是数据库中不存在的同步目标账号。
	missingReloadAccount := New(Config{CookieID: "missing-reload", Store: store, CookieStr: "unb=missing"})
	if missingReloadAccount.reloadCookieFromDB(context.Background()) {
		t.Fatal("不存在账号不应报告 Cookie 已同步")
	}
	// account.reloadCookieFromDB 首次采纳数据库中的新权威 Jar。
	if !account.reloadCookieFromDB(context.Background()) {
		t.Fatal("数据库权威凭证变化后应同步到内存")
	}
	// account.reloadCookieFromDB 再次读取相同权威 Jar 时应视为无需同步。
	if account.reloadCookieFromDB(context.Background()) {
		t.Fatal("相同权威凭证不应重复同步")
	}
}
