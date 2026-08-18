package engine

import (
	"context"
	"errors"
	"strings"
	"time"

	"xianyu-go/internal/logsafe"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/renew"
)

// credentialCoordinator 统一编排单账号的 Cookie、登录态、API 续期与 Token 缓存。
// account 在 New 完成所有固定依赖和状态装配后写入；协调器不持有独立锁，凭证字段
// 始终由 credentialState.mu 保护，数据库凭证锁只覆盖快照读取和条件提交，绝不覆盖
// Handler、通知、MTOP 或浏览器等可扩展外部 I/O。
type credentialCoordinator struct {
	// account 是本协调器唯一服务的账号 facade，运行期间不会替换。
	account *Account
}

// tryLoginStatusCheck 保留 Account 的稳定内部入口，并委托给凭证协调器。
func (a *Account) tryLoginStatusCheck(ctx context.Context) loginStatusCheckResult {
	return a.credentials.tryLoginStatusCheck(ctx)
}

// tryAPIRenew 保留启动续期入口，并委托给凭证协调器。
func (a *Account) tryAPIRenew(ctx context.Context) bool {
	return a.credentials.tryAPIRenew(ctx)
}

// tryAPIRenewUsing 保留注入续期调用的测试兼容入口，并委托给凭证协调器。
func (a *Account) tryAPIRenewUsing(ctx context.Context, call func(context.Context, string, []cookierefresh.BrowserCookie) (*renew.Result, error)) (bool, error) {
	return a.credentials.tryAPIRenewUsing(ctx, call)
}

// persistRenewFlatCookie 保留扁平 Cookie 写回的测试兼容入口，并委托给凭证协调器。
func (a *Account) persistRenewFlatCookie(ctx context.Context, newCookies string) error {
	return a.credentials.persistRenewFlatCookie(ctx, newCookies)
}

// watchPendingAPIRenew 保留迟到续期任务登记入口，并委托给凭证协调器。
func (a *Account) watchPendingAPIRenew(parent context.Context, result *renew.Result) {
	a.credentials.watchPendingAPIRenew(parent, result)
}

// persistPendingRenewCookies 保留迟到响应持久化入口，并委托给凭证协调器。
func (a *Account) persistPendingRenewCookies(ctx context.Context, result *renew.Result) error {
	return a.credentials.persistPendingRenewCookies(ctx, result)
}

// adoptRecoveredCookie 保留轻量恢复 Cookie 的兼容入口，并委托给凭证协调器。
func (a *Account) adoptRecoveredCookie(ctx context.Context, newCookies, source string) bool {
	return a.credentials.adoptRecoveredCookie(ctx, newCookies, source)
}

// notifyCredentialUpdated 保留凭证变更通知入口，并委托给凭证协调器。
func (a *Account) notifyCredentialUpdated(ctx context.Context) {
	a.credentials.notifyCredentialUpdated(ctx)
}

// refreshToken 保留连接前新 Token 获取入口，并委托给凭证协调器。
func (a *Account) refreshToken(ctx context.Context) (string, string, error) {
	return a.credentials.refreshToken(ctx)
}

// refreshTokenWithMinGap 保留旧签名测试入口，并委托给凭证协调器。
func (a *Account) refreshTokenWithMinGap(ctx context.Context, enforceMinGap bool) (string, string, error) {
	return a.credentials.refreshTokenWithMinGap(ctx, enforceMinGap)
}

// clearCurrentToken 保留连接 Token 清理入口，并委托给凭证协调器。
func (a *Account) clearCurrentToken() {
	a.credentials.clearCurrentToken()
}

// adoptTokenResponseCookies 保留 Token 响应 Cookie 合并入口，并委托给凭证协调器。
func (a *Account) adoptTokenResponseCookies(ctx context.Context, cookieStr string, result *mtop.RefreshResult) (string, error) {
	return a.credentials.adoptTokenResponseCookies(ctx, cookieStr, result)
}

// tryTokenCaptchaRecovery 保留风控恢复入口，并委托给凭证协调器。
func (a *Account) tryTokenCaptchaRecovery(ctx context.Context, cookieStr, deviceID string, refreshErr error) (*mtop.RefreshResult, bool) {
	return a.credentials.tryTokenCaptchaRecovery(ctx, cookieStr, deviceID, refreshErr)
}

// markTokenCaptchaFailure 保留风控冷却记录入口，并委托给凭证协调器。
func (a *Account) markTokenCaptchaFailure() {
	a.credentials.markTokenCaptchaFailure()
}

// tokenCaptchaCooldownRemaining 保留风控冷却查询入口，并委托给凭证协调器。
func (a *Account) tokenCaptchaCooldownRemaining() time.Duration {
	return a.credentials.tokenCaptchaCooldownRemaining()
}

// setLastTokenStatus 保留 Token 刷新诊断状态入口，并委托给凭证协调器。
func (a *Account) setLastTokenStatus(status string) {
	a.credentials.setLastTokenStatus(status)
}

// saveTokenCache 保留 Token 缓存写入入口，并委托给凭证协调器。
func (a *Account) saveTokenCache(ctx context.Context, deviceID, accessToken string, serverExpireAt int64, credentialFP string) {
	a.credentials.saveTokenCache(ctx, deviceID, accessToken, serverExpireAt, credentialFP)
}

// clearTokenCache 保留 Token 缓存删除入口，并委托给凭证协调器。
func (a *Account) clearTokenCache(ctx context.Context) {
	a.credentials.clearTokenCache(ctx)
}

// databaseCredentialFingerprint 保留凭证指纹校验入口，并委托给凭证协调器。
func (a *Account) databaseCredentialFingerprint(ctx context.Context, cookieStr string) (string, error) {
	return a.credentials.databaseCredentialFingerprint(ctx, cookieStr)
}

// reloadCookieFromDB 保留数据库 Cookie 同步入口，并委托给凭证协调器。
func (a *Account) reloadCookieFromDB(ctx context.Context) bool {
	return a.credentials.reloadCookieFromDB(ctx)
}

// cookieSnapshotMatchesDB 保留 WS 注册前凭证复核入口，并委托给凭证协调器。
func (a *Account) cookieSnapshotMatchesDB(ctx context.Context, expectedFP string) bool {
	return a.credentials.cookieSnapshotMatchesDB(ctx, expectedFP)
}

// replaceCookieStr 保留内存 Cookie 替换入口，并委托给凭证协调器。
func (a *Account) replaceCookieStr(cookieStr string) {
	a.credentials.replaceCookieStr(cookieStr)
}

// replaceCredentialState 保留完整凭证状态替换入口，并委托给凭证协调器。
func (a *Account) replaceCredentialState(cookieStr, credentialFP string) {
	a.credentials.replaceCredentialState(cookieStr, credentialFP)
}

// runtimeCookieUpdateTimeout 是旧无 Context Cookie 同步入口的最长数据库与刷新门等待预算。
const runtimeCookieUpdateTimeout = 10 * time.Second

// UpdateCookie 是外部刷新 Cookie 同步的兼容入口。新调用方应使用 UpdateCookieContext
// 传入请求或应用任务的 Context；该入口只为尚未迁移的内部回调提供受限预算。
func (a *Account) UpdateCookie(cookieStr string) {
	// updateCtx、updateCancel 为历史无 Context 调用创建有限同步预算，避免等待刷新门时无限阻塞。
	updateCtx, updateCancel := context.WithTimeout(context.Background(), runtimeCookieUpdateTimeout)
	defer updateCancel()
	// updateErr 保存受限兼容同步在取消、数据库读取或刷新门等待时的失败原因。
	if updateErr := a.UpdateCookieContext(updateCtx, cookieStr); updateErr != nil {
		a.logger.Warn("同步运行时 Cookie 失败", "err", updateErr)
	}
}

// UpdateCookieContext 用调用方 Context 同步已持久化的 Cookie、内存凭证快照与 Token 缓存。
// 它不创建后台 worker；调用返回前完成全部状态收口，因此不受账号 Run 停止 fencing 影响。
func (a *Account) UpdateCookieContext(ctx context.Context, cookieStr string) error {
	if ctx == nil {
		return errors.New("同步运行时 Cookie 需要调用 Context")
	}
	return a.credentials.updateCookie(ctx, cookieStr)
}

// tryLoginStatusCheck 调用 mtop.taobao.idlemessage.pc.loginuser.get 做轻量登录态确认。
// 这个接口的成本低于完整 token 刷新，且可能顺手下发新的签名 Cookie；
// 因此在 session 失效后、接口续期前先跑一遍，避免已实现的登录态检查能力闲置。
// tryLoginStatusCheck 封装try登录状态Check业务协调。
func (c *credentialCoordinator) tryLoginStatusCheck(ctx context.Context) loginStatusCheckResult {
	// a 是本凭证协调器绑定的账号 facade，提供状态与固定依赖访问。
	a := c.account
	// checker、ok 用于本次流程后续判断的checker、ok
	checker, ok := a.mtop.(loginStatusChecker)
	if !ok {
		return loginStatusCheckResult{}
	}
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
	a.mu.Lock()
	// cookieStr 用于本次流程后续判断的登录凭证Str
	cookieStr := a.CookieStr
	a.mu.Unlock()
	// requestCtx 用于本次流程后续判断的请求Ctx
	requestCtx := ctx
	// cookieSession 用于本次流程后续判断的登录凭证会话
	var cookieSession *mtop.CookieSession
	// metadataJSON 用于本次流程后续判断的metadataJSON
	metadataJSON := ""
	if a.store != nil && a.store.Cookies != nil {
		runtimeData, detailErr := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID) // runtimeData 只包含登录态检查所需的 Cookie 与 metadata。
		if detailErr != nil {
			a.logger.Warn("登录态检查前读取最新 Cookie 失败", "err", detailErr)
			return loginStatusCheckResult{}
		}
		cookieStr = runtimeData.Value
		metadataJSON = runtimeData.MetadataJSON
		// runtimeData 已在凭证锁内读取，下面只负责根据 metadata 建立 Cookie 会话。
		// metadataJSON 保留完整 Jar 信息，不能退化为仅使用扁平 Cookie。
		// snapshot 分支继续沿用登录态检查原有的请求作用域和持久化顺序。
		if // snapshot、complete 用于本次流程后续判断的snapshot、complete
		snapshot, complete := cookierefresh.SnapshotFromMetadataOK(metadataJSON); complete {
			requestCtx, cookieSession = mtop.WithCookieSnapshot(ctx, snapshot)
		} else {
			requestCtx, cookieSession = mtop.WithFlatCookieSession(ctx, cookieStr)
		}
	}
	// 登录态检查只使用当前凭证快照；慢速外部调用不得持有共享账号锁。
	if credentialLocked {
		credentialUnlock()
		credentialLocked = false
	}
	// res、err 用于本次流程后续判断的res、err
	res, err := checker.CheckLoginStatusContext(requestCtx, cookieStr)
	if a.store != nil && a.store.Cookies != nil {
		// credentialUnlock 保存外部检查完成后重新进入提交临界区的释放函数。
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
		credentialLocked = true
		// latestRuntimeData 和 reloadErr 保存外部检查完成后的最新凭证视图及重读错误。
		latestRuntimeData, reloadErr := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID)
		if reloadErr != nil {
			a.logger.Warn("登录态检查完成后读取最新 Cookie 失败", "err", reloadErr)
			return loginStatusCheckResult{}
		}
		// credentialSnapshotChanged 表示外部检查期间已有其他流程更新 Cookie 或 metadata。
		credentialSnapshotChanged := latestRuntimeData.Value != cookieStr || latestRuntimeData.MetadataJSON != metadataJSON
		cookieStr = latestRuntimeData.Value
		metadataJSON = latestRuntimeData.MetadataJSON
		if credentialSnapshotChanged {
			// 外部响应基于旧快照，当前切片不具备可安全重放的 Cookie 集合，因此丢弃旧响应状态。
			cookieSession = nil
			if res != nil {
				res.UpdatedCookies = cookieStr
			}
		}
	}
	if cookieSession != nil {
		// value、snapshot、changed 用于本次流程后续判断的value、snapshot、changed
		value, snapshot, changed := cookieSession.State()
		if changed {
			// metadata 用于本次流程后续判断的metadata
			metadata := cookierefresh.MetadataWithoutSnapshot(metadataJSON)
			if snapshot != nil {
				metadata = cookierefresh.MetadataWithSnapshot(metadataJSON, snapshot)
			}
			if // persistErr 用于本次流程后续判断的persistErr
			persistErr := a.store.Cookies.UpdateRenewalCookie(ctx, a.CookieID, value, metadata, time.Now().Unix()); persistErr != nil {
				a.logger.Warn("登录态检查保存响应 Cookie Jar 失败", "err", persistErr)
				return loginStatusCheckResult{}
			}
			a.replaceCredentialState(value, credentialStateFingerprint(value, metadata))
			a.clearTokenCache(ctx)
			// Handler 实现可执行通知或其他外部 I/O，不能让其占用凭证提交锁。
			if credentialLocked {
				credentialUnlock()
				credentialLocked = false
			}
			a.notifyCredentialUpdated(ctx)
			if err != nil {
				a.logger.Warn("登录态检查失败，已保存响应 Cookie", "err", err)
			}
			return loginStatusCheckResult{recovered: res != nil && res.Status == mtop.LoginStatusTokenRefreshed}
		}
	}
	// 以下恢复路径会触发可扩展 Handler；提交阶段已经结束，必须先释放凭证锁。
	if credentialLocked {
		credentialUnlock()
		credentialLocked = false
	}
	if err != nil {
		a.logger.Warn("登录态检查失败", "err", err)
		return loginStatusCheckResult{}
	}
	if res == nil {
		return loginStatusCheckResult{}
	}
	if res.Status == mtop.LoginStatusRiskRequired {
		a.setRuntimeState(RuntimeVerificationRequired, "闲鱼要求安全验证")
		a.logger.Warn("登录态检查命中风控验证", "ret", strings.Join(res.Ret, ","), "verification_url", logsafe.URL(res.VerificationURL))
		return loginStatusCheckResult{riskRequired: true, verificationURL: res.VerificationURL}
	}
	if res.Status == mtop.LoginStatusTokenRefreshed && len(cookierefresh.ChangedCookieNames(cookieStr, res.UpdatedCookies)) > 0 && a.adoptRecoveredCookie(ctx, res.UpdatedCookies, "登录态检查") {
		a.logger.Info("登录态检查刷新了 Cookie", "status", res.Status, "message", res.Message)
		return loginStatusCheckResult{recovered: true}
	}
	a.logger.Info("登录态检查未产生可用 Cookie 更新", "status", res.Status, "message", res.Message)
	return loginStatusCheckResult{}
}

// tryAPIRenew 是密码登录前的轻量恢复层，只执行官网 auto-login plugin 的
// 单次 silentHasLogin 流程。如果只拿到部分 Cookie，仍先保存并清 token，
// 但继续按 Go 协议执行后续恢复；仍失败时由上层要求重新扫码登录。
// tryAPIRenew 封装tryAPIRenew业务协调。
func (c *credentialCoordinator) tryAPIRenew(ctx context.Context) bool {
	// a 是本凭证协调器绑定的账号 facade，提供可选续期端口。
	a := c.account
	if a.renewer == nil {
		return false
	}
	// renewed 用于本次流程后续判断的renewed
	renewed, _ := a.tryAPIRenewUsing(ctx, func(runCtx context.Context, cookieStr string, snapshot []cookierefresh.BrowserCookie) (*renew.Result, error) {
		return a.renewer.RenewAPIFirst(runCtx, cookieStr, snapshot)
	})
	return renewed
}

// tryAPIRenewUsing 封装tryAPIRenewUsing业务协调。
func (c *credentialCoordinator) tryAPIRenewUsing(ctx context.Context, call func(context.Context, string, []cookierefresh.BrowserCookie) (*renew.Result, error)) (bool, error) {
	// a 是本凭证协调器绑定的账号 facade，统一提供凭证状态与运行时状态。
	a := c.account
	// releaseRefreshGate 释放当前账号刷新流程的通道令牌。
	releaseRefreshGate, gateErr := a.acquireRefreshGate(ctx)
	if gateErr != nil {
		return false, gateErr
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
	if a.store != nil && a.store.Cookies != nil && !a.store.Cookies.GetStatus(ctx, a.CookieID) {
		return false, nil
	}
	a.mu.Lock()
	// cookieStr 用于本次流程后续判断的登录凭证Str
	cookieStr := a.CookieStr
	a.mu.Unlock()
	// snapshot 用于本次流程后续判断的snapshot
	var snapshot []cookierefresh.BrowserCookie
	// metadataJSON 保存用于续期请求和提交比较的 Cookie metadata。
	metadataJSON := ""
	if a.store != nil && a.store.Cookies != nil {
		runtimeData, detailErr := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID) // runtimeData 只包含接口续期所需的 Cookie 与 metadata。
		if detailErr != nil {
			a.logger.Warn("接口续期前读取最新 Cookie 失败", "err", detailErr)
			return false, detailErr
		}
		if runtimeData.Value != cookieStr {
			cookieStr = runtimeData.Value
			a.replaceCookieStr(cookieStr)
			a.clearCurrentToken()
			a.clearTokenCache(ctx)
		}
		metadataJSON = runtimeData.MetadataJSON
		snapshot = cookierefresh.SnapshotFromMetadata(runtimeData.MetadataJSON)
		// runtimeData 的 Cookie 用于续期请求，metadata 用于恢复浏览器 Cookie 快照。
		// 读取窄模型不会触碰登录用户名、密码等无关凭证字段。
		// API 续期仍沿用原有锁、Cookie 比较和 token 清理顺序。
	}
	// API 续期只使用当前凭证快照；慢速外部续期回调不得持有共享账号锁。
	if credentialLocked {
		credentialUnlock()
		credentialLocked = false
	}
	// res、err 用于本次流程后续判断的res、err
	res, err := call(ctx, cookieStr, snapshot)
	// responseMetadataOverride 保存并发更新期间重放响应 Cookie 后的 metadata。
	responseMetadataOverride := ""
	// responseMetadataOverridden 表示本次响应已经基于最新凭证快照重放。
	responseMetadataOverridden := false
	if a.store != nil && a.store.Cookies != nil {
		// credentialUnlock 保存外部续期完成后重新进入提交临界区的释放函数。
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
		credentialLocked = true
		// latestRuntimeData 和 reloadErr 保存外部续期完成后的最新凭证视图及重读错误。
		latestRuntimeData, reloadErr := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID)
		if reloadErr != nil {
			a.logger.Warn("接口续期完成后读取最新 Cookie 失败", "err", reloadErr)
			return false, reloadErr
		}
		// credentialSnapshotChanged 表示外部续期期间已有其他流程更新 Cookie 或 metadata。
		credentialSnapshotChanged := latestRuntimeData.Value != cookieStr || latestRuntimeData.MetadataJSON != metadataJSON
		cookieStr = latestRuntimeData.Value
		metadataJSON = latestRuntimeData.MetadataJSON
		if res != nil && credentialSnapshotChanged {
			if len(res.SetCookies) > 0 {
				// rebasedCookies、rebasedMetadata 保存基于最新快照重放 Set-Cookie 的结果。
				rebasedCookies, rebasedMetadata, _ := renew.RebaseResponseCookies(cookieStr, metadataJSON, res)
				res.NewCookies = rebasedCookies
				res.CookieSnapshot = nil
				res.CookieSnapshotComplete = false
				responseMetadataOverride = rebasedMetadata
				responseMetadataOverridden = true
			} else {
				// 没有可重放的 Set-Cookie 时，拒绝把旧请求计算出的状态写回。
				res.NewCookies = cookieStr
				res.CookieSnapshot = nil
				res.CookieSnapshotComplete = false
			}
		}
	}
	if res == nil {
		if err != nil {
			a.logger.Warn("接口续期失败", "err", err)
		}
		return false, err
	}
	// updated 用于本次流程后续判断的updated
	updated := false
	// persisted 用于本次流程后续判断的persisted
	persisted := false
	if responseMetadataOverridden && a.store != nil && a.store.Cookies != nil {
		// err 保存并发重放结果写回错误。
		if err := a.store.Cookies.UpdateRenewalCookie(ctx, a.CookieID, res.NewCookies, responseMetadataOverride, time.Now().Unix()); err != nil {
			a.logger.Warn("保存并发重放后的续期 Cookie 失败", "err", err)
			return false, err
		}
		persisted = true
	} else if res.CookieSnapshotComplete && a.store != nil && a.store.Cookies != nil {
		runtimeData, detailErr := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID) // runtimeData 只包含续期快照持久化所需字段。
		// 快照持久化只依赖 metadata，Cookie 明文由续期响应直接提供。
		// 保留统一运行时查询模型，避免为相同凭证路径恢复完整账号详情。
		// 下面的更新操作和错误处理保持原有续期语义不变。
		if detailErr != nil {
			a.logger.Warn("保存续期 Cookie 快照失败", "err", detailErr)
			return false, detailErr
		}
		// metadata 用于本次流程后续判断的metadata
		metadata := cookierefresh.MetadataWithSnapshot(runtimeData.MetadataJSON, res.CookieSnapshot)
		if // err 用于本次流程后续判断的err
		err := a.store.Cookies.UpdateRenewalCookie(ctx, a.CookieID, res.NewCookies, metadata, time.Now().Unix()); err != nil {
			a.logger.Warn("保存续期 Cookie 快照失败", "err", err)
			return false, err
		}
		persisted = true
	} else if len(res.SetCookies) > 0 && a.store != nil && a.store.Cookies != nil {
		if // err 用于本次流程后续判断的err
		err := a.persistRenewFlatCookie(ctx, res.NewCookies); err != nil {
			a.logger.Warn("保存接口续期扁平 Cookie 失败", "err", err)
			return false, err
		}
		persisted = true
	}
	// 续期写入已经提交，迟到续期 watcher 与 Handler 回调必须在凭证锁外启动。
	if credentialLocked {
		credentialUnlock()
		credentialLocked = false
	}
	if res.HasPending() {
		a.watchPendingAPIRenew(ctx, res)
	}
	// credentialChanged 用于本次流程后续判断的credentialChanged
	credentialChanged := res.NewCookies != cookieStr && (res.CookieSnapshotComplete || len(res.SetCookies) > 0 || res.NewCookies != "")
	if credentialChanged {
		if persisted || a.store == nil || a.store.Cookies == nil {
			a.replaceCookieStr(res.NewCookies)
			a.clearCurrentToken()
			a.clearTokenCache(ctx)
			a.setRuntimeState(RuntimeConnecting, "接口续期已更新登录凭证，正在重新连接")
			updated = true
		} else {
			updated = a.adoptRecoveredCookie(ctx, res.NewCookies, "接口续期")
		}
		if updated && persisted {
			a.notifyCredentialUpdated(ctx)
		}
	}
	if err != nil {
		a.logger.Warn("接口续期失败，已保存响应 Cookie", "err", err)
		return false, err
	}
	if res.Success {
		if !updated {
			a.setRuntimeState(RuntimeConnecting, "登录凭证已接口续期，正在重新连接")
		}
		a.logger.Info("接口续期成功", "method", res.RenewMethod, "updated", strings.Join(res.UpdatedCookieNames, ","))
		return true, nil
	}
	if updated {
		a.logger.Info("接口续期返回部分 Cookie 更新，继续降级恢复", "updated", strings.Join(res.UpdatedCookieNames, ","))
		return false, nil
	}
	a.logger.Info("接口续期未产生可用恢复", "success", res.Success, "message", res.Message)
	return false, nil
}

// persistRenewFlatCookie 封装persistRenewFlat登录凭证业务协调。
func (c *credentialCoordinator) persistRenewFlatCookie(ctx context.Context, newCookies string) error {
	// a 是本凭证协调器绑定的账号 facade，提供窄 Cookie repository。
	a := c.account
	if a.store == nil || a.store.Cookies == nil {
		return nil
	}
	metadata, err := a.store.Cookies.GetCookieMetadata(ctx, a.CookieID) // metadata 只包含扁平 Cookie 写回所需的快照信息。
	if err != nil {
		return err
	}
	// 该流程不需要读取现有 Cookie 明文或登录秘密。
	// metadata 已在 repository 层按账号作用域解密，下面只清理旧快照。
	// 更新操作继续由 UpdateRenewalCookie 负责加密和账号存在性校验。
	// 没有权威 Jar 时，接口 Set-Cookie 只能更新兼容扁平值。不能把
	// Domain/Path/HttpOnly/PartitionKey 均未知的 Cookie 伪造成完整快照。
	metadata = cookierefresh.MetadataWithoutSnapshot(metadata)
	return a.store.Cookies.UpdateRenewalCookie(ctx, a.CookieID, newCookies, metadata, time.Now().Unix())
}

// watchPendingAPIRenew 封装watchPendingAPIRenew业务协调。
func (c *credentialCoordinator) watchPendingAPIRenew(parent context.Context, result *renew.Result) {
	// a 是本凭证协调器绑定的账号 facade，提供生命周期任务登记。
	a := c.account
	a.pendingRenewal.watch(parent, a.beginTask, result, a.persistPendingRenewCookies, a.logger)
}

// persistPendingRenewCookies 封装persistPendingRenewCookies业务协调。
func (c *credentialCoordinator) persistPendingRenewCookies(ctx context.Context, result *renew.Result) error {
	// a 是本凭证协调器绑定的账号 facade，提供凭证存储与脱敏日志。
	a := c.account
	if result == nil || len(result.SetCookies) == 0 || a.store == nil || a.store.Cookies == nil {
		return nil
	}
	// releaseRefreshGate 释放迟到 Cookie 收口占用的刷新通道令牌。
	releaseRefreshGate, gateErr := a.acquireRefreshGate(ctx)
	if gateErr != nil {
		return gateErr
	}
	defer releaseRefreshGate()
	// credentialUnlock 用于本次流程后续判断的credentialUnlock
	credentialUnlock := a.store.LockAccountCredentials(a.CookieID)
	// credentialLocked 表示延迟清理是否仍拥有数据库凭证锁。
	credentialLocked := true
	defer func() {
		if credentialLocked {
			credentialUnlock()
		}
	}()
	runtimeData, err := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID) // runtimeData 只包含迟到续期合并所需的 Cookie 与 metadata。
	if err != nil {
		return err
	}
	// runtimeData 已在凭证锁内读取，避免迟到响应覆盖并发写入的最新凭证状态。
	// RebaseResponseCookies 继续根据当前 Cookie 与 metadata 重放 Set-Cookie。
	// 下面的 UpdateRenewalCookie、运行时状态和通知顺序保持原有行为。
	// newCookies、metadata、changed 用于本次流程后续判断的newCookies、metadata、changed
	newCookies, metadata, changed := renew.RebaseResponseCookies(runtimeData.Value, runtimeData.MetadataJSON, result)
	if !changed {
		return nil
	}
	if // err 用于本次流程后续判断的err
	err := a.store.Cookies.UpdateRenewalCookie(ctx, a.CookieID, newCookies, metadata, time.Now().Unix()); err != nil {
		return err
	}
	a.replaceCredentialState(newCookies, credentialStateFingerprint(newCookies, metadata))
	a.clearTokenCache(ctx)
	// Handler 回调可以执行网络通知，必须在释放数据库凭证锁后进行。
	credentialUnlock()
	credentialLocked = false
	a.notifyCredentialUpdated(ctx)
	a.logger.Info("已异步接收官网静默续期迟到 Cookie", "updated", strings.Join(result.UpdatedCookieNames, ","))
	return nil
}

// adoptRecoveredCookie 统一接收“轻量检查/接口续期”拿到的新 Cookie。
// 官网页面在普通 Set-Cookie 更新后保持当前 FishEngine/device ID 与健康 WS；
// 下一次重连才使用新 Cookie 获取新的连接级 accessToken。
// adoptRecoveredCookie 封装adoptRecovered登录凭证业务协调。
func (c *credentialCoordinator) adoptRecoveredCookie(ctx context.Context, newCookies, source string) bool {
	// a 是本凭证协调器绑定的账号 facade，提供当前凭证状态与状态通知。
	a := c.account
	if strings.TrimSpace(newCookies) == "" {
		return false
	}
	a.mu.Lock()
	// oldCookies 用于本次流程后续判断的oldCookies
	oldCookies := a.CookieStr
	a.mu.Unlock()
	if newCookies == oldCookies {
		return false
	}
	if a.store != nil && a.store.Cookies != nil {
		if // err 用于本次流程后续判断的err
		err := a.store.Cookies.UpdateValueExisting(ctx, a.CookieID, newCookies); err != nil {
			a.logger.Error(source+"后保存 cookie 失败", "cookie_id", a.CookieID, "err", err)
			return false
		}
	}
	a.replaceCookieStr(newCookies)
	a.clearCurrentToken()
	a.clearTokenCache(ctx)
	a.setRuntimeState(RuntimeConnecting, source+"已更新登录凭证，正在重新连接")
	a.notifyCredentialUpdated(ctx)
	return true
}

// notifyCredentialUpdated 封装notifyCredentialUpdated业务协调。
func (c *credentialCoordinator) notifyCredentialUpdated(ctx context.Context) {
	// a 是本凭证协调器绑定的账号 facade；调用点已经完成凭证锁释放。
	a := c.account
	if // handler、ok 用于本次流程后续判断的handler、ok
	handler, ok := a.handler.(credentialUpdateHandler); ok {
		handler.OnCredentialUpdated(ctx, a.CookieID)
	}
}

// refreshToken 调 mtop token API，返回 (accessToken, 更新后的 cookie)。
// 成功时记录 token 的服务端过期时间并保持 device_id 持久化，但连接流程
// 不会复用该 token；下一次 loginV2/reConnect 仍会重新调用本方法。
// refreshToken 封装refresh令牌业务协调。
