package engine

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/protocol"
)

// refreshToken 按账号凭证协调器的最小间隔规则刷新平台连接 Token，并返回 Token 与 Cookie 快照。
func (c *credentialCoordinator) refreshToken(ctx context.Context) (string, string, error) {
	// a 是本凭证协调器绑定的账号 facade，保留连接流程使用的返回契约。
	a := c.account
	return a.refreshTokenWithMinGap(ctx, false)
}

// refreshTokenWithMinGap 保留旧签名以避免影响调用方；参考实现没有额外的一分钟
// Token 防抖，因此 enforceMinGap 不参与行为。
// refreshTokenWithMinGap 封装refresh令牌WithMinGap业务协调。
func (c *credentialCoordinator) refreshTokenWithMinGap(ctx context.Context, _ bool) (string, string, error) {
	// a 是本凭证协调器绑定的账号 facade，集中访问 MTOP、Cookie repository 和状态。
	a := c.account
	// releaseRefreshGate 释放 Token 刷新占用的通道令牌。
	releaseRefreshGate, gateErr := a.acquireRefreshGate(ctx)
	if gateErr != nil {
		return "", "", gateErr
	}
	defer releaseRefreshGate()
	// credentialUnlock 用于本次流程后续判断的credentialUnlock
	credentialUnlock := func() {}
	// credentialLocked 标识当前调用是否持有账号凭证锁。
	credentialLocked := false
	if a.store != nil {
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
		credentialLocked = true
	}
	defer func() {
		if credentialLocked {
			credentialUnlock()
		}
	}()

	// refreshGate 串行化完整 Token/Cookie 更新事务；风控失败冷却仍由调用方状态控制。
	if // remaining 用于本次流程后续判断的remaining
	remaining := a.tokenCaptchaCooldownRemaining(); remaining > 0 {
		a.setLastTokenStatus(tokenRefreshSkippedCooldown)
		return "", "", fmt.Errorf("%w，剩余 %s", errTokenCaptchaCooldown, remaining.Round(time.Second))
	}

	a.reloadCookieFromDB(ctx)

	a.mu.Lock()
	// cookieStr 用于本次流程后续判断的登录凭证Str
	cookieStr := a.CookieStr
	// metadataJSON 保存当前凭证快照对应的 metadata。
	metadataJSON := ""
	a.lastTokenRefresh = time.Now()
	a.lastTokenStatus = tokenRefreshStarted
	a.mu.Unlock()

	// deviceID 用于本次流程后续判断的deviceID
	deviceID := strings.TrimSpace(a.deviceID)
	if deviceID == "" {
		if // unb 用于本次流程后续判断的unb
		unb := protocol.TransCookies(cookieStr)["unb"]; unb != "" {
			deviceID = protocol.GenerateDeviceID(unb)
			a.mu.Lock()
			a.deviceID = deviceID
			a.mu.Unlock()
		}
	}
	if a.store != nil && a.store.Cookies != nil {
		// runtimeData 保存 Token 请求开始前的最小凭证快照。
		runtimeData, detailErr := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID)
		if detailErr != nil {
			return "", "", detailErr
		}
		cookieStr = runtimeData.Value
		metadataJSON = runtimeData.MetadataJSON
	}
	// Token 网络请求和风控恢复都必须在共享凭证锁外执行。
	if credentialLocked {
		credentialUnlock()
		credentialLocked = false
	}
	for // captchaRetry 用于本次流程后续判断的captcha重试
	captchaRetry := 0; captchaRetry < 3; captchaRetry++ {
		// res、err 保存本轮平台 Token 请求结果及其传输或业务失败原因。
		res, err := c.requestPlatformToken(ctx, cookieStr, deviceID)
		if a.store != nil && a.store.Cookies != nil {
			// credentialUnlock 保存 Token 响应提交临界区的释放函数。
			credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
			credentialLocked = true
			// latestRuntimeData 和 reloadErr 保存网络请求完成后的最新凭证视图及重读错误。
			latestRuntimeData, reloadErr := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID)
			if reloadErr != nil {
				a.setLastTokenStatus(tokenRefreshFailedAPI)
				a.clearCurrentToken()
				return "", "", reloadErr
			}
			if latestRuntimeData.Value != cookieStr || latestRuntimeData.MetadataJSON != metadataJSON {
				// 并发流程已经更新凭证，丢弃旧请求的 Token 和 Cookie 响应，下一轮使用最新快照重试。
				cookieStr = latestRuntimeData.Value
				metadataJSON = latestRuntimeData.MetadataJSON
				credentialUnlock()
				credentialLocked = false
				continue
			}
		}
		// 参考实现无论业务结果为何，都先合并响应 Set-Cookie。本地还必须先把
		// 完整 Jar 持久化成功，避免当前 /reg 成功而下次重连回滚到旧凭证。
		// 响应处理会广播给 Handler，因此先释放凭证锁，成功 Token 绑定前会重新校验。
		if credentialLocked {
			credentialUnlock()
			credentialLocked = false
		}
		// persistErr 用于本次流程后续判断的persistErr
		var persistErr error
		cookieStr, persistErr = a.adoptTokenResponseCookies(ctx, cookieStr, res)
		if persistErr != nil {
			a.setLastTokenStatus(tokenRefreshFailedAPI)
			a.clearCurrentToken()
			return "", "", fmt.Errorf("保存 token 响应 Cookie: %w", persistErr)
		}
		if err != nil && mtop.IsRiskVerificationErr(err) {
			// 风控恢复是外部调用，不能在共享凭证锁内执行。
			if credentialLocked {
				credentialUnlock()
				credentialLocked = false
			}
			if // recovered、ok 用于本次流程后续判断的recovered、ok
			recovered, ok := a.tryTokenCaptchaRecovery(ctx, cookieStr, deviceID, err); ok {
				cookieStr = recovered.UpdatedCookies
				// 重取地址时即使拿到了 accessToken，参考实现也不会直接采用；
				// 它会清缓存后重新走一次标准 token 请求。
				continue
			}
			a.markTokenCaptchaFailure()
			a.setLastTokenStatus(tokenRefreshFailedCaptcha)
			a.clearCurrentToken()
			a.clearTokenCache(ctx)
			return "", "", err
		}
		if err != nil {
			// status 用于本次流程后续判断的状态
			status := classifyTokenFailure(err)
			a.setLastTokenStatus(status)
			a.clearCurrentToken()
			if status != tokenRefreshFailedNetwork && status != tokenRefreshFailedTimeout {
				a.clearTokenCache(ctx)
			}
			return "", "", err
		}
		if res == nil || strings.TrimSpace(res.AccessToken) == "" {
			a.setLastTokenStatus(tokenRefreshFailedAPI)
			a.clearCurrentToken()
			a.clearTokenCache(ctx)
			return "", "", fmt.Errorf("token API 未返回结果")
		}
		if a.store != nil {
			credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
			credentialLocked = true
		}
		// credentialFP、fingerprintErr 用于本次流程后续判断的credentialFP、fingerprintErr
		credentialFP, fingerprintErr := a.databaseCredentialFingerprint(ctx, cookieStr)
		if fingerprintErr != nil {
			a.setLastTokenStatus(tokenRefreshFailedAPI)
			a.clearCurrentToken()
			a.clearTokenCache(ctx)
			return "", "", fmt.Errorf("绑定 token 凭证状态: %w", fingerprintErr)
		}
		a.saveTokenCache(ctx, deviceID, res.AccessToken, res.AccessTokenExpireAt, credentialFP)
		a.mu.Lock()
		a.credentialFP = credentialFP
		a.tokenCredentialFP = credentialFP
		a.lastCaptchaFailure = time.Time{}
		a.tokenFetchFailures = 0
		a.lastTokenStatus = tokenRefreshSuccess
		a.mu.Unlock()
		a.runtimeMu.Lock()
		a.lastMsgReceived = time.Time{}
		a.runtimeMu.Unlock()
		return res.AccessToken, cookieStr, nil
	}

	a.setLastTokenStatus(tokenRefreshFailedCaptcha)
	a.clearCurrentToken()
	a.clearTokenCache(ctx)
	return "", "", fmt.Errorf("滑块验证重试次数已达上限")
}

// requestPlatformToken 使用具备凭证快照能力的客户端请求 Token；旧客户端保持扁平 Cookie 兼容路径。
func (c *credentialCoordinator) requestPlatformToken(ctx context.Context, cookieStr, deviceID string) (*mtop.RefreshResult, error) {
	// a 是本凭证协调器绑定的账号 facade，保存平台客户端与凭证快照仓储。
	a := c.account
	// scoped、ok 分别是支持权威 Cookie 快照的 MTOP 客户端及其类型断言结果。
	if scoped, ok := a.mtop.(scopedTokenClient); ok {
		// snapshot 是当前 Cookie metadata 的权威快照；读取失败时退回客户端既有扁平凭证请求语义。
		var snapshot []cookierefresh.BrowserCookie
		if a.store != nil && a.store.Cookies != nil {
			// metadata、metadataErr 分别是账号 Cookie 快照元数据及其读取失败原因；读取失败时保持兼容请求路径。
			if metadata, metadataErr := a.store.Cookies.GetCookieMetadata(ctx, a.CookieID); metadataErr == nil {
				snapshot = cookierefresh.SnapshotFromMetadata(metadata)
			}
		}
		return scoped.RefreshTokenWithCredentialContext(ctx, cookieStr, deviceID, snapshot)
	}
	return a.mtop.RefreshTokenWithDeviceIDContext(ctx, cookieStr, deviceID)
}

// clearCurrentToken 封装clearCurrent令牌业务协调。
func (c *credentialCoordinator) clearCurrentToken() {
	// a 是本凭证协调器绑定的账号 facade，持有 Token 状态锁。
	a := c.account
	a.mu.Lock()
	a.currentToken = ""
	a.tokenCredentialFP = ""
	a.mu.Unlock()
}

// adoptTokenResponseCookies 封装adopt令牌响应Cookies业务协调。
func (c *credentialCoordinator) adoptTokenResponseCookies(ctx context.Context, cookieStr string, res *mtop.RefreshResult) (string, error) {
	// a 是本凭证协调器绑定的账号 facade，提供 Cookie 持久化与回调端口。
	a := c.account
	if res == nil {
		return cookieStr, nil
	}
	if !res.CookieSnapshotComplete && !res.CookieStateChanged && strings.TrimSpace(res.UpdatedCookies) == "" {
		return cookieStr, nil
	}
	if !res.CookieSnapshotComplete && !res.CookieStateChanged && res.UpdatedCookies == cookieStr && len(res.CookieSnapshot) == 0 {
		return cookieStr, nil
	}
	if a.store != nil && a.store.Cookies != nil {
		metadata, detailErr := a.store.Cookies.GetCookieMetadata(ctx, a.CookieID) // metadata 只包含 token 响应 Cookie 合并所需的快照信息。
		if detailErr != nil {
			return cookieStr, detailErr
		}
		// metadata 已在 repository 层按账号作用域解密，不读取旧 Cookie 或登录秘密。
		// 下面继续根据响应类型合并已有快照，并由 UpdateRenewalCookie 统一持久化。
		// 错误返回和运行时状态更新顺序保持原有 token 响应语义。
		// 只有 token 响应本身发生变化时才进入后续快照合并逻辑。
		if res.CookieSnapshotComplete {
			// snapshot 用于本次流程后续判断的snapshot
			snapshot := cookierefresh.NormalizeSnapshot(res.CookieSnapshot)
			if snapshot == nil {
				snapshot = []cookierefresh.BrowserCookie{}
			}
			metadata = cookierefresh.MetadataWithSnapshot(metadata, snapshot)
		} else if // snapshot、snapshotOK 用于本次流程后续判断的snapshot、snapshotOK
		snapshot, snapshotOK := cookierefresh.SnapshotFromMetadataOK(metadata); snapshotOK {
			// 扁平结果不能凭空证明 Jar 完整；仅在已有权威 Jar 时按已知
			// Domain/Path 身份对值做兼容合并。
			snapshot = cookierefresh.ReconcileSnapshotWithCookieString(snapshot, res.UpdatedCookies)
			metadata = cookierefresh.MetadataWithSnapshot(metadata, snapshot)
		} else {
			metadata = cookierefresh.MetadataWithoutSnapshot(metadata)
		}
		if // err 用于本次流程后续判断的err
		err := a.store.Cookies.UpdateRenewalCookie(ctx, a.CookieID, res.UpdatedCookies, metadata, time.Now().Unix()); err != nil {
			return cookieStr, err
		}
		a.replaceCredentialState(res.UpdatedCookies, credentialStateFingerprint(res.UpdatedCookies, metadata))
		a.notifyCredentialUpdated(ctx)
		return res.UpdatedCookies, nil
	}
	if res.UpdatedCookies != cookieStr {
		a.replaceCookieStr(res.UpdatedCookies)
	}
	return res.UpdatedCookies, nil
}

// tryTokenCaptchaRecovery 封装try令牌CaptchaRecovery业务协调。
func (c *credentialCoordinator) tryTokenCaptchaRecovery(ctx context.Context, cookieStr, deviceID string, err error) (*mtop.RefreshResult, bool) {
	// a 是本凭证协调器绑定的账号 facade，提供风控 Handler 与运行时状态。
	a := c.account
	// h、ok 用于本次流程后续判断的h、ok
	h, ok := a.handler.(tokenCaptchaHandler)
	if !ok {
		return nil, false
	}
	// riskErr 用于本次流程后续判断的riskErr
	var riskErr *mtop.RiskVerificationError
	if !errors.As(err, &riskErr) || strings.TrimSpace(riskErr.VerificationURL) == "" {
		return nil, false
	}
	a.alertEvent(ctx, EventSecurityVerification, AlertLevelWarn, "闲鱼要求滑块验证",
		"token 刷新触发闲鱼风控验证，系统将尝试自动完成滑块并合并 x5sec。")
	// result、ok 用于本次流程后续判断的result、ok
	result, ok := h.OnTokenCaptchaVerification(ctx, a.CookieID, cookieStr, riskErr.VerificationURL, deviceID)
	if !ok || result == nil || strings.TrimSpace(result.UpdatedCookies) == "" {
		return nil, false
	}
	// updatedCookies、persistErr 用于本次流程后续判断的updatedCookies、persistErr
	updatedCookies, persistErr := a.adoptTokenResponseCookies(ctx, cookieStr, result)
	if persistErr != nil {
		a.logger.Error("滑块验证后保存 cookie 失败", "cookie_id", a.CookieID, "err", persistErr)
		return nil, false
	}
	result.UpdatedCookies = updatedCookies
	a.replaceCookieStr(updatedCookies)
	a.clearTokenCache(ctx)
	a.setRuntimeState(RuntimeConnecting, tokenRiskRecoveryMessage)
	return result, true
}

// markTokenCaptchaFailure 封装mark令牌CaptchaFailure业务协调。
func (c *credentialCoordinator) markTokenCaptchaFailure() {
	// a 是本凭证协调器绑定的账号 facade，持有风控冷却状态。
	a := c.account
	a.mu.Lock()
	a.lastCaptchaFailure = time.Now()
	a.mu.Unlock()
}

// tokenCaptchaCooldownRemaining 封装令牌CaptchaCooldownRemaining业务协调。
func (c *credentialCoordinator) tokenCaptchaCooldownRemaining() time.Duration {
	// a 是本凭证协调器绑定的账号 facade，持有风控冷却状态。
	a := c.account
	a.mu.Lock()
	// lastFailure 用于本次流程后续判断的lastFailure
	lastFailure := a.lastCaptchaFailure
	a.mu.Unlock()
	if lastFailure.IsZero() {
		return 0
	}
	// remaining 用于本次流程后续判断的remaining
	remaining := TokenCaptchaFailureCooldown - time.Since(lastFailure)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// saveTokenCache records the server expiry and current page-runtime identity.
// It is diagnostic state only: acquireToken never reads the accessToken back
// for a later WebSocket registration.
// saveTokenCache 封装save令牌Cache业务协调。
func (c *credentialCoordinator) saveTokenCache(ctx context.Context, deviceID, accessToken string, serverExpireAt int64, credentialFP string) {
	// a 是本凭证协调器绑定的账号 facade，提供 Token repository 与诊断日志。
	a := c.account
	if accessToken == "" {
		return
	}
	// now 用于本次流程后续判断的now
	now := time.Now()
	// expiresAt、refreshAt 用于本次流程后续判断的expiresAt、refreshAt
	expiresAt, refreshAt := tokenRotationSchedule(serverExpireAt, now)
	// tokenFP 用于本次流程后续判断的令牌FP
	tokenFP := tokenFingerprint(accessToken)
	a.mu.Lock()
	// previousTokenFP 用于本次流程后续判断的previous令牌FP
	previousTokenFP := a.tokenFingerprint
	a.tokenFingerprint = tokenFP
	a.tokenAcquiredAt = now
	a.tokenExpiresAt = expiresAt
	a.tokenRefreshAt = refreshAt
	a.mu.Unlock()
	a.logger.Info("WS Token 获取成功", "expires_at", expiresAt, "refresh_at", refreshAt, "ttl", time.Until(expiresAt).Round(time.Second), "token_fp", tokenFP, "previous_token_fp", previousTokenFP, "token_changed", previousTokenFP == "" || previousTokenFP != tokenFP)
	if a.store == nil || a.store.Tokens == nil {
		return
	}
	// expireAt 用于本次流程后续判断的expireAt
	expireAt := effectiveTokenExpireAt(serverExpireAt, now)
	if expireAt == 0 {
		// 服务端未给有效期时仍使用保守运行时轮换时间，但不把推测期限
		// 伪装成服务端缓存期限。
		a.logger.Warn("token API 未返回可用过期时间，使用保守轮换时间", "refresh_at", refreshAt)
		a.clearTokenCache(ctx)
		return
	}
	if // err 用于本次流程后续判断的err
	err := a.store.Tokens.SaveBound(ctx, a.CookieID, deviceID, accessToken, expireAt, credentialFP); err != nil {
		a.logger.Warn("缓存 accessToken 失败", "err", err)
	}
}

// tokenFingerprint 用不可逆摘要标识 Token，便于判断服务端是否轮换了 Token，
// 同时避免日志泄露可用于 WS 注册的凭证原文。
// tokenFingerprint 封装令牌Fingerprint业务协调。
func tokenFingerprint(token string) string {
	// sum 用于本次流程后续判断的sum
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", sum[:6])
}

// clearTokenCache 清除账号 token 缓存（session 失效 / 短连接可疑 / cookie 被外部更新时调用）。
func (c *credentialCoordinator) clearTokenCache(ctx context.Context) {
	// a 是本凭证协调器绑定的账号 facade，提供 Token repository 与内存状态。
	a := c.account
	a.mu.Lock()
	a.tokenFingerprint = ""
	a.mu.Unlock()
	if a.store == nil || a.store.Tokens == nil {
		return
	}
	if // err 用于本次流程后续判断的err
	err := a.store.Tokens.Clear(ctx, a.CookieID); err != nil {
		a.logger.Warn("清除 token 缓存失败", "err", err)
	}
}

// databaseCredentialFingerprint returns the complete DB credential state that
// produced cookieStr. It must be called while the account credential lock is
// held when a Store is present.
// databaseCredentialFingerprint 封装databaseCredentialFingerprint业务协调。
func (c *credentialCoordinator) databaseCredentialFingerprint(ctx context.Context, cookieStr string) (string, error) {
	// a 是本凭证协调器绑定的账号 facade，提供当前账号的窄凭证查询端口。
	a := c.account
	if a.store == nil || a.store.Cookies == nil {
		return credentialStateFingerprint(cookieStr, ""), nil
	}
	runtimeData, err := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID) // runtimeData 只包含 token 凭证一致性校验所需的 Cookie 与 metadata。
	if err != nil {
		return "", err
	}
	// runtimeData 已在调用方凭证锁内读取，避免校验期间混入另一笔 Cookie 更新。
	// Cookie 与 metadata 均由 repository 按账号作用域解密，登录密码不会进入此流程。
	// 后续空值判断、指纹比较和错误文案保持原有 token 绑定语义。
	// snapshotComplete 用于本次流程后续判断的snapshotComplete
	_, snapshotComplete := cookierefresh.SnapshotFromMetadataOK(runtimeData.MetadataJSON)
	if strings.TrimSpace(runtimeData.Value) == "" && !snapshotComplete {
		return "", fmt.Errorf("数据库 Cookie 为空且没有权威 Jar")
	}
	if credentialCookieFingerprint(runtimeData.Value) != credentialCookieFingerprint(cookieStr) {
		return "", fmt.Errorf("token 请求期间数据库 Cookie 已变化")
	}
	return credentialStateFingerprint(runtimeData.Value, runtimeData.MetadataJSON), nil
}

// reloadCookieFromDB 复读 DB cookie：与内存不同则采纳，并清 token 缓存。普通 Cookie
// 更新不轮换页面生命周期内的 device ID；显式登录由 Manager 重建 Account。
// reloadCookieFromDB 封装reload登录凭证FromDB业务协调。
func (c *credentialCoordinator) reloadCookieFromDB(ctx context.Context) bool {
	// a 是本凭证协调器绑定的账号 facade，提供运行时状态与 Cookie repository。
	a := c.account
	if a.store == nil || a.store.Cookies == nil {
		return false
	}
	runtimeData, err := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID) // runtimeData 只包含检测外部凭证更新所需的 Cookie 与 metadata。
	if err != nil {
		return false
	}
	if strings.TrimSpace(runtimeData.Value) == "" {
		if // complete 用于本次流程后续判断的complete
		_, complete := cookierefresh.SnapshotFromMetadataOK(runtimeData.MetadataJSON); !complete {
			return false
		}
	}
	// databaseFP 用于本次流程后续判断的databaseFP
	databaseFP := credentialStateFingerprint(runtimeData.Value, runtimeData.MetadataJSON)
	a.mu.Lock()
	// currentFP 用于本次流程后续判断的currentFP
	currentFP := a.credentialFP
	if currentFP == "" {
		currentFP = credentialStateFingerprint(a.CookieStr, "")
	}
	a.mu.Unlock()
	if databaseFP == currentFP {
		return false
	}
	a.logger.Info("检测到 DB cookie 已更新，重新加载", "account", a.CookieID)
	a.replaceCredentialState(runtimeData.Value, databaseFP)
	a.clearCurrentToken()
	a.clearTokenCache(ctx)
	a.mu.Lock()
	a.lastCaptchaFailure = time.Time{}
	a.mu.Unlock()
	return true
}

// cookieSnapshotMatchesDB 封装登录凭证SnapshotMatchesDB业务协调。
func (c *credentialCoordinator) cookieSnapshotMatchesDB(ctx context.Context, expectedFP string) bool {
	// a 是本凭证协调器绑定的账号 facade，提供 WS 注册前的窄凭证查询端口。
	a := c.account
	if a.store == nil || a.store.Cookies == nil {
		return true
	}
	runtimeData, err := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID) // runtimeData 只包含 WS 注册前凭证一致性校验所需的 Cookie 与 metadata。
	if err != nil {
		a.logger.Warn("WS 注册前读取最新 Cookie 失败，放弃本次连接", "err", err)
		return false
	}
	// snapshotComplete 用于本次流程后续判断的snapshotComplete
	_, snapshotComplete := cookierefresh.SnapshotFromMetadataOK(runtimeData.MetadataJSON)
	if strings.TrimSpace(runtimeData.Value) == "" && !snapshotComplete {
		a.logger.Warn("WS 注册前最新 Cookie 为空且没有权威 Jar，放弃本次连接")
		return false
	}
	if expectedFP == "" {
		a.logger.Warn("WS 注册 token 缺少绑定的凭证状态，放弃本次连接")
		return false
	}
	return credentialStateFingerprint(runtimeData.Value, runtimeData.MetadataJSON) == expectedFP
}

// replaceCookieStr 封装replace登录凭证Str业务协调。
func (c *credentialCoordinator) replaceCookieStr(cookieStr string) {
	// a 是本凭证协调器绑定的账号 facade，持有运行时 Cookie 状态。
	a := c.account
	a.replaceCredentialState(cookieStr, credentialStateFingerprint(cookieStr, ""))
}

// replaceCredentialState 封装replaceCredential状态业务协调。
func (c *credentialCoordinator) replaceCredentialState(cookieStr, credentialFP string) {
	// a 是本凭证协调器绑定的账号 facade，持有凭证状态锁与用户身份快照。
	a := c.account
	a.mu.Lock()
	defer a.mu.Unlock()
	a.CookieStr = cookieStr
	a.credentialFP = credentialFP
	if // unb 用于本次流程后续判断的unb
	unb := protocol.TransCookies(cookieStr)["unb"]; unb != "" {
		a.UserID = unb
	}
}

// updateCookie 用外部刷新得到的新 Cookie 更新运行时状态，并在调用方 Context 到期时停止等待。
func (c *credentialCoordinator) updateCookie(ctx context.Context, cookieStr string) error {
	// a 是本凭证协调器绑定的账号 facade，提供权威 Cookie repository 与刷新门。
	a := c.account
	if strings.TrimSpace(cookieStr) == "" && (a.store == nil || a.store.Cookies == nil) {
		return nil
	}
	// releaseRefreshGate 释放运行时 Cookie 同步占用的通道令牌。
	releaseRefreshGate, gateErr := a.acquireRefreshGate(ctx)
	if gateErr != nil {
		return fmt.Errorf("获取运行时 Cookie 同步门: %w", gateErr)
	}
	defer releaseRefreshGate()
	// credentialUnlock 用于本次流程后续判断的credentialUnlock
	credentialUnlock := func() {}
	if a.store != nil {
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
	}
	defer credentialUnlock()
	// 外部调用通常发生在一次网络请求写回之后。调用排队期间可能已有更新的
	// Cookie 落库，因此参数只作为无 Store 场景的兼容值；有 Store 时始终
	// 复读权威数据库，绝不把较旧的请求结果重新写回运行时。
	// metadataJSON 用于本次流程后续判断的metadataJSON
	metadataJSON := ""
	if a.store != nil && a.store.Cookies != nil {
		// runtimeData、err 分别是账号运行时的权威 Cookie 快照及读取失败；查询继承调用方取消。
		runtimeData, err := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID)
		if err != nil {
			return fmt.Errorf("读取运行时 Cookie: %w", err)
		}
		if strings.TrimSpace(runtimeData.Value) == "" {
			if // complete 用于本次流程后续判断的complete
			_, complete := cookierefresh.SnapshotFromMetadataOK(runtimeData.MetadataJSON); !complete {
				return errors.New("同步运行时 Cookie 时数据库值为空且无权威 Jar")
			}
		}
		cookieStr = runtimeData.Value
		metadataJSON = runtimeData.MetadataJSON
	}
	// credentialFP 用于本次流程后续判断的credentialFP
	credentialFP := credentialStateFingerprint(cookieStr, metadataJSON)
	a.mu.Lock()
	// changed 用于本次流程后续判断的changed
	changed := credentialFP != a.credentialFP
	a.mu.Unlock()
	if !changed {
		return nil
	}
	a.replaceCredentialState(cookieStr, credentialFP)
	// Cookie Jar 的普通更新不会打断已经认证的 IMPaaS 连接。新 Cookie
	// 会在下一次自然重连前被重新读取并用于获取新的 accessToken。
	a.clearTokenCache(ctx)
	return nil
}
