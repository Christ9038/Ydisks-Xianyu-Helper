package renewal

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	apirenew "xianyu-go/internal/xianyu/renew"
)

// loginRenewInterval 保存登录RenewInterval，供当前处理流程使用
const (
	loginRenewInterval     = 600 * time.Second
	cookiesRefreshInterval = 600 * time.Second
	apiCookieRenewInterval = 4 * time.Hour
	accountRequestInterval = time.Minute
	sessionExpiredCooldown = 300 * time.Second
	passwordLoginCooldown  = 300 * time.Second
	passwordErrorCooldown  = 5 * time.Hour
)

// loginRenewEnabledSetting 保存登录Renew启用状态设置，供当前处理流程使用
const (
	loginRenewEnabledSetting      = "renewal.login_renew.enabled"
	loginRenewIntervalSetting     = "renewal.login_renew.interval_seconds"
	apiCookieRenewEnabledSetting  = "renewal.api_cookie_renew.enabled"
	apiCookieRenewIntervalSetting = "renewal.api_cookie_renew.interval_seconds"
	cookiesRefreshEnabledSetting  = "renewal.cookies_refresh.enabled"
	cookiesRefreshIntervalSetting = "renewal.cookies_refresh.interval_seconds"
)

// AccountStarter 保存账号Starter，供当前处理流程使用
type AccountStarter interface {
	Start(ctx context.Context, cookieID, cookieValue string) error
}

// accountRestarter 保存账号Restarter，供当前处理流程使用
type accountRestarter interface {
	Restart(ctx context.Context, cookieID string) error
}

// PasswordRefresher 保存密码Refresher，供当前处理流程使用
type PasswordRefresher interface {
	OnPasswordLoginRefresh(ctx context.Context, cookieID string) bool
}

// Scheduler 保存Scheduler，供当前处理流程使用
type Scheduler struct {
	mu        sync.Mutex
	store     *db.Store
	starter   AccountStarter
	refresher PasswordRefresher
	logger    *slog.Logger
	mtop      *mtop.ClientImpl
	api       apirenew.Service
	cooldown  *CooldownManager
	notifier  RenewalNotifier
	runOnce   sync.Once
	done      chan struct{}
	workers   sync.WaitGroup
	watchers  sync.WaitGroup
	// runCancel 取消当前调度器派生的运行上下文；只有 Run 成功登记后才非空。
	runCancel context.CancelFunc
}

// RenewalNotifier 保存RenewalNotifier，供当前处理流程使用
type RenewalNotifier interface {
	NotifyAccountEvent(cookieID, eventType, level, title, body string)
}

// NewScheduler 的最后一个参数可选，用于发送连续续期失败告警，保持旧调用方兼容。
func NewScheduler(store *db.Store, starter AccountStarter, refresher PasswordRefresher, logger *slog.Logger, notifiers ...RenewalNotifier) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	// notifier 保存notifier，供当前处理流程使用
	var notifier RenewalNotifier
	if len(notifiers) > 0 {
		notifier = notifiers[0]
	}
	return &Scheduler{
		store:     store,
		starter:   starter,
		refresher: refresher,
		logger:    logger,
		mtop:      mtop.NewClient(),
		api:       apirenew.Service{},
		cooldown:  GlobalCooldown,
		notifier:  notifier,
		done:      make(chan struct{}),
	}
}

// Run 执行当前值。
func (s *Scheduler) Run(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.runOnce.Do(func() {
		// runCtx、cancel 将父生命周期转换为调度器私有的可主动停止上下文。
		runCtx, cancel := context.WithCancel(ctx)
		s.mu.Lock()
		s.runCancel = cancel
		s.mu.Unlock()
		go func() {
			defer close(s.done)
			defer cancel()
			s.workers.Add(2)
			go func() {
				defer s.workers.Done()
				s.runFixed(runCtx, "login_renew", loginRenewEnabledSetting, loginRenewIntervalSetting, false, loginRenewInterval, s.executeLoginRenew)
			}()
			// 官网静默插件在账号启动时执行，并由此任务按保守频率持续检查；闲鱼
			// 下发的 sdkSilent 疲劳窗口仍会在请求前阻止重复续期。
			// cookies_refresh 仅作为旧配置名的兼容别名。两套配置必须汇聚到同一个
			// goroutine，否则同时开启时会重复续期并连续重启同一账号。
			go func() {
				defer s.workers.Done()
				s.runAPICookieRenewFixed(runCtx)
			}()
			s.workers.Wait()
			s.watchers.Wait()
		}()
	})
}

// StopContext 主动取消续期调度器并在 ctx 允许的时间内等待所有任务和迟到响应 watcher 收束。
// 调用可重复执行；已经停止或尚未启动的调度器视为成功完成。
func (s *Scheduler) StopContext(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	// cancel 是调度器运行上下文的取消函数快照，避免持锁执行取消回调。
	cancel := s.runCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return s.WaitContext(ctx)
}

// Stop 主动停止调度器并无限期等待，兼容旧调用方的无错误返回签名。
func (s *Scheduler) Stop() {
	_ = s.StopContext(context.Background())
}

// Wait 等待定时循环和迟到响应 watcher 完成。
func (s *Scheduler) Wait() {
	_ = s.WaitContext(context.Background())
}

// WaitContext 在 ctx 约束内等待定时循环和 watcher 完成。
func (s *Scheduler) WaitContext(ctx context.Context) error {
	if s == nil || s.done == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runAPICookieRenewFixed 负责运行API登录凭证RenewFixed相关处理。
func (s *Scheduler) runAPICookieRenewFixed(ctx context.Context) {
	if s.apiRenewEnabled(ctx) {
		s.executeAPICookieRenew(ctx)
	}
	for {
		// timer 保存定时器，供当前处理流程使用
		timer := time.NewTimer(s.apiRenewInterval(ctx))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if !s.apiRenewEnabled(ctx) {
			continue
		}
		s.logger.Info("执行续期任务", "task", "api_cookie_renew")
		s.executeAPICookieRenew(ctx)
	}
}

// runFixed 负责运行Fixed相关处理。
func (s *Scheduler) runFixed(ctx context.Context, name, settingKey, intervalKey string, defaultEnabled bool, defaultInterval time.Duration, fn func(context.Context)) {
	if s.settingEnabled(ctx, settingKey, defaultEnabled) {
		fn(ctx)
	}
	for {
		// interval 保存interval，供当前处理流程使用
		interval := s.settingInterval(ctx, intervalKey, defaultInterval)
		// timer 保存定时器，供当前处理流程使用
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if !s.settingEnabled(ctx, settingKey, defaultEnabled) {
			continue
		}
		s.logger.Info("执行续期任务", "task", name)
		fn(ctx)
	}
}

// executeLoginRenew 负责execute登录Renew相关处理。
func (s *Scheduler) executeLoginRenew(ctx context.Context) {
	s.cleanupExpiredLogs(ctx)
	// batchID 保存批次ID，供当前处理流程使用
	batchID := newBatchID()
	// accounts、err 保存accounts、err，供当前处理流程使用
	accounts, err := s.store.Cookies.ActiveRenewalRuntimeAccounts(ctx)
	if err != nil {
		s.logger.Warn("login_renew 加载账号失败", "err", err)
		return
	}
	// i、account 表示当前遍历过程中的i、account
	for i, account := range accounts {
		if s.isSessionCooled(account.ID) {
			s.logger.Info("login_renew session 冷却中，跳过", "account", account.ID)
			continue
		}
		s.loginRenewOne(ctx, batchID, account)
		if i < len(accounts)-1 {
			_ = sleepCtx(ctx, accountRequestInterval)
		}
	}
}

// loginRenewOne 负责登录RenewOne相关处理。
func (s *Scheduler) loginRenewOne(ctx context.Context, batchID string, account db.RenewalRuntimeAccount) {
	// credentialUnlock 保存credentialUnlock，供当前处理流程使用
	credentialUnlock := s.store.LockAccountCredentials(account.ID)
	// credentialLocked 标识当前调用是否持有账号凭证锁。
	credentialLocked := true
	// credentialUpdated 保存credentialUpdated，供当前处理流程使用
	credentialUpdated := false
	// sessionExpired 保存会话Expired，供当前处理流程使用
	sessionExpired := false
	defer func() {
		if credentialLocked {
			credentialUnlock()
		}
		if sessionExpired {
			s.logger.Warn("loginuser.get 检测到 Session 过期，开始即时续期", "account", account.ID)
			if s.refresher != nil && s.refresher.OnPasswordLoginRefresh(ctx, account.ID) {
				s.wakeCredentialBlockedAutomation(ctx, account.ID)
				s.restartAfterCredentialUpdate(ctx, account.ID, account.Enabled, "Session 即时续期")
			}
			return
		}
		if credentialUpdated {
			s.wakeCredentialBlockedAutomation(ctx, account.ID)
			s.restartAfterCredentialUpdate(ctx, account.ID, account.Enabled, "登录态续期")
		}
	}()
	// started 保存started，供当前处理流程使用
	started := time.Now()
	// latest、err 保存latest、err，供当前处理流程使用
	latest, err := s.reloadRenewalAccount(ctx, account)
	if err != nil {
		s.addLoginLog(ctx, batchID, account.ID, "failed", "重读账号凭证失败: "+err.Error(), nil, time.Since(started))
		return
	}
	account = latest
	if !account.Enabled {
		return
	}
	// runCtx、cancel 保存运行Ctx、cancel，供当前处理流程使用
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// mtopCtx 保存mtopCtx，供当前处理流程使用
	var mtopCtx context.Context
	// cookieSession 保存登录凭证会话，供当前处理流程使用
	var cookieSession *mtop.CookieSession
	if // snapshot、ok 保存snapshot、ok，供当前处理流程使用
	snapshot, ok := cookierefresh.SnapshotFromMetadataOK(account.MetadataJSON); ok {
		mtopCtx, cookieSession = mtop.WithCookieSnapshot(runCtx, snapshot)
	} else {
		mtopCtx, cookieSession = mtop.WithFlatCookieSession(runCtx, account.Value)
	}
	// 登录态检查只使用当前快照；慢速 loginuser.get 不得持有共享凭证锁。
	credentialUnlock()
	credentialLocked = false
	// res、callErr 保存res、callErr，供当前处理流程使用
	res, callErr := s.mtop.CheckLoginStatusContext(mtopCtx, account.Value)
	// credentialUnlock 保存外部检查完成后重新进入提交临界区的释放函数。
	credentialUnlock = s.store.LockAccountCredentials(account.ID)
	credentialLocked = true
	// latestAfterCheck 和 reloadErr 保存外部检查完成后的最新账号快照及重读错误。
	latestAfterCheck, reloadErr := s.reloadRenewalAccount(ctx, account)
	if reloadErr != nil {
		s.addLoginLog(ctx, batchID, account.ID, "failed", "外部登录态检查后重读账号凭证失败: "+reloadErr.Error(), nil, time.Since(started))
		s.logger.Warn("login_renew 检查完成后重读账号凭证失败", "account", account.ID, "err", reloadErr)
		return
	}
	if !latestAfterCheck.Enabled {
		s.addLoginLog(ctx, batchID, account.ID, "skipped", "账号在登录态检查期间已停用", nil, time.Since(started))
		return
	}
	// credentialSnapshotChanged 表示登录态检查期间已有其他流程写入 Cookie 或 metadata。
	credentialSnapshotChanged := latestAfterCheck.Value != account.Value || latestAfterCheck.MetadataJSON != account.MetadataJSON
	account = latestAfterCheck

	// 对齐浏览器在响应头到达时立即应用 Set-Cookie 的时序。权威 session
	// 因此必须在处理请求或解析错误之前持久化，否则下次请求会
	// 从数据库回滚到旧 Jar。
	// updated 保存updated，供当前处理流程使用
	updated := []string(nil)
	// value、snapshot、changed 保存value、snapshot、changed，供当前处理流程使用
	value, snapshot, changed := cookieSession.State()
	// 完整 Jar 即使本轮没有变化也已经权威接管请求；不能因扁平 Cookie
	// 的顺序或尾分号不同回退写入并清掉快照。
	// sessionHandled 保存会话Handled，供当前处理流程使用
	sessionHandled := snapshot != nil
	if credentialSnapshotChanged {
		// 外部响应基于旧快照，本切片暂不具备可安全重放的 loginuser.get Cookie 集合，因此丢弃旧响应状态。
		value, snapshot, changed = account.Value, nil, false
		sessionHandled = false
		if res != nil {
			res.UpdatedCookies = account.Value
		}
	}
	if changed {
		updated = cookierefresh.ChangedCookieNames(account.Value, value)
		// metadata 保存metadata，供当前处理流程使用
		metadata := cookierefresh.MetadataWithoutSnapshot(account.MetadataJSON)
		if snapshot != nil {
			metadata = cookierefresh.MetadataWithSnapshot(account.MetadataJSON, snapshot)
		}
		if // persistErr 保存persistErr，供当前处理流程使用
		persistErr := s.store.Cookies.UpdateRenewalCookie(ctx, account.ID, value, metadata, time.Now().Unix()); persistErr != nil {
			if callErr != nil {
				persistErr = errors.Join(callErr, fmt.Errorf("保存 loginuser.get 响应 Cookie Jar: %w", persistErr))
			}
			s.addLoginLog(ctx, batchID, account.ID, "failed", persistErr.Error(), updated, time.Since(started))
			s.logger.Warn("login_renew 保存响应 Cookie Jar 失败", "account", account.ID, "err", persistErr)
			return
		}
		credentialUpdated = true
	}
	if callErr != nil {
		s.addLoginLog(ctx, batchID, account.ID, "failed", callErr.Error(), updated, time.Since(started))
		s.logger.Warn("login_renew 失败", "account", account.ID, "err", callErr)
		return
	}
	if res == nil {
		s.addLoginLog(ctx, batchID, account.ID, "failed", "loginuser.get 未返回结果", nil, time.Since(started))
		return
	}
	if !sessionHandled {
		updated = cookierefresh.ChangedCookieNames(account.Value, res.UpdatedCookies)
		if res.UpdatedCookies != "" && res.UpdatedCookies != account.Value {
			// 注入 mock 或没有权威快照的历史账号仍走扁平
			// Cookie 兼容路径。扁平值无法证明旧 snapshot 的
			// Domain/Path/expiry 仍有效，因此必须清除旧快照。
			// metadata 保存metadata，供当前处理流程使用
			metadata := cookierefresh.MetadataWithoutSnapshot(account.MetadataJSON)
			if // err 保存err，供当前处理流程使用
			err := s.store.Cookies.UpdateRenewalCookie(ctx, account.ID, res.UpdatedCookies, metadata, time.Now().Unix()); err != nil {
				s.addLoginLog(ctx, batchID, account.ID, "failed", "保存 Cookie 失败: "+err.Error(), updated, time.Since(started))
				return
			}
			credentialUpdated = true
		}
	}
	s.addLoginLog(ctx, batchID, account.ID, res.Status, res.Message, updated, time.Since(started))
	if res.Status == mtop.LoginStatusSessionExpired || res.Status == mtop.LoginStatusTokenEmpty {
		s.markSessionExpired(account.ID)
	}
	if res.Status == mtop.LoginStatusSessionExpired {
		sessionExpired = true
	}
}

// executeAPICookieRenew 负责executeAPI登录凭证Renew相关处理。
func (s *Scheduler) executeAPICookieRenew(ctx context.Context) {
	s.cleanupExpiredLogs(ctx)
	// batchID 保存批次ID，供当前处理流程使用
	batchID := newBatchID()
	// accounts、err 保存accounts、err，供当前处理流程使用
	accounts, err := s.store.Cookies.ActiveRenewalRuntimeAccounts(ctx)
	if err != nil {
		s.logger.Warn("api_cookie_renew 加载账号失败", "err", err)
		return
	}
	// i、account 表示当前遍历过程中的i、account
	for i, account := range accounts {
		s.apiCookieRenewOne(ctx, batchID, account)
		if i < len(accounts)-1 {
			_ = sleepCtx(ctx, accountRequestInterval)
		}
	}
}

// apiCookieRenewOne 负责api登录凭证RenewOne相关处理。
func (s *Scheduler) apiCookieRenewOne(ctx context.Context, batchID string, account db.RenewalRuntimeAccount) {
	// credentialUnlock 保存credentialUnlock，供当前处理流程使用
	credentialUnlock := s.store.LockAccountCredentials(account.ID)
	// credentialLocked 保存credentialLocked，供当前处理流程使用
	credentialLocked := true
	// credentialChanged 保存credentialChanged，供当前处理流程使用
	credentialChanged := false
	// credentialPersisted 保存credentialPersisted，供当前处理流程使用
	credentialPersisted := false
	// restartHandled 保存restartHandled，供当前处理流程使用
	restartHandled := false
	defer func() {
		if credentialLocked {
			credentialUnlock()
		}
		if credentialPersisted {
			s.wakeCredentialBlockedAutomation(ctx, account.ID)
			if !restartHandled {
				s.restartAfterCredentialUpdate(ctx, account.ID, account.Enabled, "接口续期响应 Cookie")
			}
		}
	}()
	// started 保存started，供当前处理流程使用
	started := time.Now()
	// latest、err 保存latest、err，供当前处理流程使用
	latest, err := s.reloadRenewalAccount(ctx, account)
	if err != nil {
		s.addAPILog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "failed", ErrorMessage: "重读账号凭证失败: " + err.Error(), RenewMethod: "auto_login_plugin"})
		s.logger.Warn("接口续期任务失败", "account", account.ID, "err", "重读账号凭证失败: "+err.Error())
		return
	}
	account = latest
	if !account.Enabled {
		return
	}
	// 续期请求只使用当前快照；慢速外部 API 调用不得持有共享凭证锁。
	credentialUnlock()
	credentialLocked = false
	// res、callErr 保存res、callErr，供当前处理流程使用
	res, callErr := s.renewAPI(ctx, account.Value, cookierefresh.SnapshotFromMetadata(account.MetadataJSON))
	// credentialUnlock 保存外部请求完成后重新进入提交临界区的释放函数。
	credentialUnlock = s.store.LockAccountCredentials(account.ID)
	credentialLocked = true
	// latestAfterCall 和 reloadErr 保存外部调用完成后的最新账号快照及重读错误。
	latestAfterCall, reloadErr := s.reloadRenewalAccount(ctx, account)
	if reloadErr != nil {
		s.addAPILog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "failed", ErrorMessage: "外部续期后重读账号凭证失败: " + reloadErr.Error(), RenewMethod: "auto_login_plugin", DurationMS: time.Since(started).Milliseconds()})
		s.logger.Warn("接口续期完成后重读账号凭证失败", "account", account.ID, "err", reloadErr)
		return
	}
	if !latestAfterCall.Enabled {
		s.addAPILog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "skipped", ErrorMessage: "账号在接口续期期间已停用", RenewMethod: "auto_login_plugin", DurationMS: time.Since(started).Milliseconds()})
		return
	}
	// credentialSnapshotChanged 表示外部请求期间已有其他流程写入 Cookie 或 metadata。
	credentialSnapshotChanged := latestAfterCall.Value != account.Value || latestAfterCall.MetadataJSON != account.MetadataJSON
	// responseMetadataOverride 保存基于最新账号快照重放后的 metadata。
	responseMetadataOverride := ""
	// responseMetadataOverridden 表示续期响应已完成最新快照重放。
	responseMetadataOverridden := false
	account = latestAfterCall
	if res != nil && credentialSnapshotChanged {
		if len(res.SetCookies) > 0 {
			// rebasedCookies、rebasedMetadata 保存基于最新账号快照重放响应 Cookie 的结果。
			rebasedCookies, rebasedMetadata, _ := apirenew.RebaseResponseCookies(account.Value, account.MetadataJSON, res)
			res.NewCookies = rebasedCookies
			res.CookieSnapshot = nil
			res.CookieSnapshotComplete = false
			responseMetadataOverride = rebasedMetadata
			responseMetadataOverridden = true
		} else {
			// 没有可重放的 Set-Cookie 时，拒绝把旧请求计算出的完整状态写回。
			res.NewCookies = account.Value
			res.CookieSnapshot = nil
			res.CookieSnapshotComplete = false
		}
	}
	if res == nil {
		// message 保存消息，供当前处理流程使用
		message := "接口续期未返回结果"
		if callErr != nil {
			message = callErr.Error()
		}
		s.addAPILog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "failed", ErrorMessage: message, RenewMethod: "auto_login_plugin", DurationMS: time.Since(started).Milliseconds()})
		s.logger.Warn("接口续期任务失败", "account", account.ID, "err", message)
		return
	}
	if res.HasPending() {
		s.watchPendingAPIRenew(ctx, batchID, account.ID, res)
	}
	// stepDetails 保存stepDetails，供当前处理流程使用
	stepDetails := make([]string, 0, len(res.StepDetails)+1)
	// step 表示当前遍历过程中的step
	for _, step := range res.StepDetails {
		stepDetails = append(stepDetails, fmt.Sprintf("%s: http=%d business_ok=%v set_cookie=%d", step.Name, step.HTTPStatus, step.BusinessOK, step.SetCookieCount))
	}
	stepDetails = append(stepDetails, fmt.Sprintf("result: success=%v skipped=%v reason=%s", res.Success, res.Skipped, res.SkipReason))
	// updated 保存updated，供当前处理流程使用
	updated := cookierefresh.ChangedCookieNames(account.Value, res.NewCookies)
	if res.CookieSnapshotComplete || len(res.SetCookies) > 0 || res.NewCookies != account.Value {
		// metadata 保存metadata，供当前处理流程使用
		metadata := cookierefresh.MetadataWithoutSnapshot(account.MetadataJSON)
		if res.CookieSnapshotComplete {
			metadata = cookierefresh.MetadataWithSnapshot(account.MetadataJSON, res.CookieSnapshot)
		}
		if responseMetadataOverridden {
			metadata = responseMetadataOverride
		}
		// 扁平 Cookie 的排序和尾分号不是凭证变化；只有字段值变化才需要
		// 写回和重启。完整 Jar 则还要比较 Domain/Path/Expires 等 metadata。
		credentialChanged = len(updated) > 0 || metadata != account.MetadataJSON
		if credentialChanged && !s.saveRenewedCookies(ctx, account.ID, res.NewCookies, metadata) {
			s.addAPILog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "failed", ErrorMessage: "保存 Cookie 失败", UpdatedCookieNames: updated, StepDetails: strings.Join(stepDetails, " | "), RenewMethod: res.RenewMethod, DurationMS: time.Since(started).Milliseconds(), RequestCount: res.RequestCount})
			s.logger.Warn("接口续期任务失败", "account", account.ID, "method", res.RenewMethod, "err", "保存 Cookie 失败")
			return
		}
		credentialPersisted = credentialChanged
	}
	if callErr != nil {
		s.addAPILog(ctx, db.RenewalLog{
			BatchID: batchID, CookieID: account.ID, Status: "failed", ErrorMessage: callErr.Error(),
			UpdatedCookieNames: updated, StepDetails: strings.Join(stepDetails, " | "), RenewMethod: res.RenewMethod,
			DurationMS: time.Since(started).Milliseconds(), RequestCount: res.RequestCount,
		})
		s.logger.Warn("接口续期任务失败，已保存响应头 Cookie", "account", account.ID, "method", res.RenewMethod, "updated", strings.Join(updated, ","), "err", callErr)
		return
	}
	if res.Success && account.Enabled && credentialChanged {
		s.logger.Info("接口续期任务成功", "account", account.ID, "method", res.RenewMethod, "updated", strings.Join(updated, ","), "message", res.Message)
		credentialUnlock()
		credentialLocked = false
		if // restarter、ok 保存restarter、ok，供当前处理流程使用
		restarter, ok := s.starter.(accountRestarter); ok {
			restartHandled = true
			s.logger.Info("接口续期成功，正在重启账号以应用最新登录凭证", "account", account.ID)
			if // err 保存err，供当前处理流程使用
			err := restarter.Restart(ctx, account.ID); err != nil {
				s.addAPILog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "failed", ErrorMessage: "重建消息连接失败: " + err.Error(), UpdatedCookieNames: updated, StepDetails: strings.Join(stepDetails, " | "), RenewMethod: res.RenewMethod, DurationMS: time.Since(started).Milliseconds(), RequestCount: res.RequestCount})
				s.logger.Warn("接口续期成功，但重启账号失败", "account", account.ID, "err", err)
				return
			}
			s.logger.Info("接口续期后的账号重启已完成", "account", account.ID)
		}
	}
	// status 保存状态，供当前处理流程使用
	status := "failed"
	if res.HasPending() {
		status = "pending"
	} else if res.Skipped {
		status = "skipped"
	} else if res.Success && len(updated) > 0 {
		status = "cookie_updated"
	} else if res.Success {
		status = "success"
	}
	s.addAPILog(ctx, db.RenewalLog{
		BatchID:            batchID,
		CookieID:           account.ID,
		Status:             status,
		Message:            res.Message,
		UpdatedCookieNames: updated,
		ResponseContent:    res.ResponseText,
		StepDetails:        strings.Join(stepDetails, " | "),
		RenewMethod:        res.RenewMethod,
		DurationMS:         time.Since(started).Milliseconds(),
		RequestCount:       res.RequestCount,
	})
	if res.HasPending() {
		s.logger.Info("接口续期任务等待底层响应", "account", account.ID, "method", res.RenewMethod, "message", res.Message)
	} else if res.Skipped {
		s.logger.Info("接口续期任务已跳过", "account", account.ID, "reason", res.SkipReason, "message", res.Message)
	} else if !res.Success {
		s.logger.Warn("接口续期任务未成功", "account", account.ID, "method", res.RenewMethod, "message", res.Message)
	}
}

// restartAfterCredentialUpdate 负责restartAfterCredentialUpdate相关处理。
func (s *Scheduler) restartAfterCredentialUpdate(ctx context.Context, accountID string, enabled bool, source string) {
	if !enabled || ctx.Err() != nil {
		return
	}
	// restarter、ok 保存restarter、ok，供当前处理流程使用
	restarter, ok := s.starter.(accountRestarter)
	if !ok {
		return
	}
	s.logger.Info("认证 Cookie 已更新，正在重启账号以刷新 WS Token", "account", accountID, "source", source)
	if // err 保存err，供当前处理流程使用
	err := restarter.Restart(ctx, accountID); err != nil {
		s.logger.Warn("认证 Cookie 已更新，但重启账号刷新 WS Token 失败", "account", accountID, "source", source, "err", err)
	}
}

// renewAPI 负责renewAPI相关处理。
func (s *Scheduler) renewAPI(ctx context.Context, cookieStr string, snapshot []cookierefresh.BrowserCookie) (*apirenew.Result, error) {
	// runCtx、cancel 保存运行Ctx、cancel，供当前处理流程使用
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	return s.api.RenewAPIFirst(runCtx, cookieStr, snapshot)
}

// watchPendingAPIRenew 负责watchPendingAPIRenew相关处理。
func (s *Scheduler) watchPendingAPIRenew(ctx context.Context, batchID, cookieID string, result *apirenew.Result) {
	if result == nil || !result.HasPending() || s.store == nil || s.store.Cookies == nil {
		return
	}
	s.watchers.Add(1)
	go func() {
		defer s.watchers.Done()
		// waitCtx、waitCancel 保存waitCtx、wait取消，供当前处理流程使用
		waitCtx, waitCancel := context.WithTimeout(ctx, 35*time.Second)
		// late、waitErr 保存late、waitErr，供当前处理流程使用
		late, waitErr := result.AwaitPending(waitCtx)
		waitCancel()
		if late == nil {
			if waitErr != nil {
				s.logger.Warn("等待定时静默续期底层响应失败", "account", cookieID, "err", waitErr)
				s.addAPILog(ctx, db.RenewalLog{BatchID: batchID, CookieID: cookieID, Status: "failed", ErrorMessage: waitErr.Error(), RenewMethod: "auto_login_plugin"})
			}
			return
		}
		// opCtx、opCancel 保存opCtx、op取消，供当前处理流程使用
		opCtx, opCancel := context.WithTimeout(ctx, 30*time.Second)
		defer opCancel()
		// finalErr 保存finalErr，供当前处理流程使用
		var finalErr error
		// changed 保存changed，供当前处理流程使用
		changed := func() bool {
			// unlock 保存unlock，供当前处理流程使用
			unlock := s.store.LockAccountCredentials(cookieID)
			defer unlock()
			detail, getErr := s.store.Cookies.GetCookiePlatformRuntimeData(opCtx, cookieID) // detail 只包含迟到 Cookie 合并所需的 Cookie 与 metadata。
			if getErr != nil {
				// 窄查询已统一把账号不存在转换为 ErrNotFound。
				// 这里继续在凭证锁内终止本次迟到响应合并。
				// 记录错误并保留原有异步任务终态语义。
				s.logger.Warn("保存定时静默续期迟到 Cookie 前读取账号失败", "account", cookieID, "err", getErr)
				finalErr = getErr
				return false
			}
			// newCookies、metadata、changed 保存newCookies、metadata、changed，供当前处理流程使用
			newCookies, metadata, changed := apirenew.RebaseResponseCookies(detail.Value, detail.MetadataJSON, late)
			if !changed {
				return false
			}
			if // saveErr 保存saveErr，供当前处理流程使用
			saveErr := s.store.Cookies.UpdateRenewalCookie(opCtx, cookieID, newCookies, metadata, time.Now().Unix()); saveErr != nil {
				s.logger.Warn("保存定时静默续期迟到 Cookie 失败", "account", cookieID, "err", saveErr)
				finalErr = saveErr
				return false
			}
			if s.store.Tokens != nil {
				_ = s.store.Tokens.Clear(opCtx, cookieID)
			}
			return true
		}()
		if changed {
			s.logger.Info("已异步接收定时静默续期迟到 Cookie", "account", cookieID)
			if // restarter、ok 保存restarter、ok，供当前处理流程使用
			restarter, ok := s.starter.(accountRestarter); ok {
				// enabled、statusErr 保存enabled、statusErr，供当前处理流程使用
				enabled, _, statusErr := s.store.Cookies.StatusWithReason(opCtx, cookieID)
				if statusErr != nil {
					s.logger.Warn("迟到续期 Cookie 已保存，但读取账号状态失败", "account", cookieID, "err", statusErr)
					finalErr = statusErr
				} else if !enabled {
					s.logger.Info("迟到续期 Cookie 已保存，账号已停用，不执行重启", "account", cookieID)
				} else {
					s.logger.Info("迟到续期 Cookie 已更新，正在重启账号以应用最新登录凭证", "account", cookieID)
					if // restartErr 保存restartErr，供当前处理流程使用
					restartErr := restarter.Restart(opCtx, cookieID); restartErr != nil {
						s.logger.Warn("迟到续期 Cookie 已保存，但重启账号失败", "account", cookieID, "err", restartErr)
						finalErr = restartErr
					} else {
						s.logger.Info("迟到续期 Cookie 更新后的账号重启已完成", "account", cookieID)
					}
				}
			}
			s.wakeCredentialBlockedAutomation(opCtx, cookieID)
		}
		if waitErr != nil {
			s.logger.Warn("定时静默续期底层响应失败，已保存响应 Cookie", "account", cookieID, "err", waitErr)
			finalErr = errors.Join(finalErr, waitErr)
		}
		// status 保存状态，供当前处理流程使用
		status := "failed"
		// errorMessage 保存错误消息，供当前处理流程使用
		errorMessage := ""
		if finalErr != nil {
			errorMessage = finalErr.Error()
		} else if late.Success {
			if changed {
				status = "cookie_updated"
			} else {
				status = "success"
			}
		} else {
			errorMessage = late.Message
		}
		s.addAPILog(opCtx, db.RenewalLog{
			BatchID: batchID, CookieID: cookieID, Status: status, Message: late.Message,
			ErrorMessage: errorMessage, UpdatedCookieNames: late.UpdatedCookieNames,
			ResponseContent: late.ResponseText, RenewMethod: late.RenewMethod,
			RequestCount: late.RequestCount,
		})
	}()
}

// wakeCredentialBlockedAutomation 负责wakeCredentialBlocked自动化相关处理。
func (s *Scheduler) wakeCredentialBlockedAutomation(ctx context.Context, cookieID string) {
	if s == nil || s.store == nil || s.store.Automation == nil {
		return
	}
	if // err 保存err，供当前处理流程使用
	err := s.store.Automation.WakeCredentialBlocked(ctx, cookieID); err != nil {
		s.logger.Warn("Cookie 更新后唤醒自动化任务失败", "account", cookieID, "err", err)
		return
	}
	s.logger.Info("Cookie 更新后已唤醒待恢复自动化任务", "account", cookieID)
}

// apiRenewEnabled 负责apiRenew启用状态相关处理。
func (s *Scheduler) apiRenewEnabled(ctx context.Context) bool {
	if s.settingConfigured(ctx, apiCookieRenewEnabledSetting) {
		return s.settingEnabled(ctx, apiCookieRenewEnabledSetting, true)
	}
	return s.settingEnabled(ctx, cookiesRefreshEnabledSetting, true)
}

// apiRenewInterval 负责apiRenewInterval相关处理。
func (s *Scheduler) apiRenewInterval(ctx context.Context) time.Duration {
	if s.settingConfigured(ctx, apiCookieRenewIntervalSetting) {
		return s.settingInterval(ctx, apiCookieRenewIntervalSetting, apiCookieRenewInterval)
	}
	return s.settingInterval(ctx, cookiesRefreshIntervalSetting, apiCookieRenewInterval)
}

// settingConfigured 负责设置Configured相关处理。
func (s *Scheduler) settingConfigured(ctx context.Context, key string) bool {
	if s.store == nil || s.store.Settings == nil {
		return false
	}
	// value、err 保存value、err，供当前处理流程使用
	value, err := s.store.Settings.Get(ctx, key)
	return err == nil && strings.TrimSpace(value) != ""
}

// reloadRenewalAccount 负责reloadRenewal账号相关处理。
func (s *Scheduler) reloadRenewalAccount(ctx context.Context, account db.RenewalRuntimeAccount) (db.RenewalRuntimeAccount, error) {
	// latest 是锁内重新读取的续期窄模型，用于避免批次列表中的旧 Cookie 覆盖新状态。
	latest, err := s.store.Cookies.GetRenewalRuntimeAccount(ctx, account.ID)
	if err != nil {
		return db.RenewalRuntimeAccount{}, err
	}
	return latest, nil
}

/*
reloadRenewalAccount 在每个账号续期前重新读取最新的 Cookie。
重新读取同时复核账号启用状态，避免批次快照覆盖并发更新。
repository 负责解密最小运行视图，不再展开登录用户名和密码。
续期任务仍在账号凭证锁内调用此边界。
Cookie 更新后的重启和自动化唤醒逻辑保持原有顺序。
本次切片只收窄读取字段，不改变续期请求参数。
禁用账号会由调用方根据 Enabled 字段跳过后续请求。
查询失败仍由当前任务记录失败并结束该账号处理。
后续的保存函数继续复用 UpdateRenewalCookie。
它不代表新增的业务分支或兼容行为。
窄模型中的 metadata 仍用于完整 Cookie Jar 恢复。
Cookie 明文只在本次调用链中短暂存在。
*/

// saveRenewedCookies 负责saveRenewedCookies相关处理。
func (s *Scheduler) saveRenewedCookies(ctx context.Context, cookieID, cookieStr, metadata string) bool {
	if // err 保存err，供当前处理流程使用
	err := s.store.Cookies.UpdateRenewalCookie(ctx, cookieID, cookieStr, metadata, time.Now().Unix()); err != nil {
		s.logger.Warn("保存续期 Cookie 失败", "account", cookieID, "err", err)
		return false
	}
	return true
}

// addLoginLog 负责add登录Log相关处理。
func (s *Scheduler) addLoginLog(ctx context.Context, batchID, cookieID, status, message string, updated []string, duration time.Duration) {
	_ = s.store.Renewal.AddLoginRenewLog(ctx, db.RenewalLog{
		BatchID:            batchID,
		CookieID:           cookieID,
		Status:             status,
		Message:            message,
		UpdatedCookieNames: updated,
		RenewMethod:        "loginuser.get",
		StepDetails:        fmt.Sprintf("loginuser.get status=%s message=%s updated=%d", status, message, len(updated)),
		DurationMS:         duration.Milliseconds(),
		RequestCount:       1,
	})
}

// addAPILog 负责addAPILog相关处理。
func (s *Scheduler) addAPILog(ctx context.Context, log db.RenewalLog) {
	if // err 保存err，供当前处理流程使用
	err := s.store.Renewal.AddAPICookieRenewLog(ctx, log); err != nil {
		s.logger.Warn("记录 API Cookie 续期日志失败", "account", log.CookieID, "status", log.Status, "err", err)
		return
	}
	if s.notifier == nil || log.Status != "failed" || s.store == nil || s.store.Renewal == nil {
		return
	}
	// statuses、err 保存statuses、err，供当前处理流程使用
	statuses, err := s.store.Renewal.RecentAPICookieRenewStatuses(ctx, log.CookieID, 4)
	if err != nil || len(statuses) < 3 || statuses[0] != "failed" || statuses[1] != "failed" || statuses[2] != "failed" {
		return
	}
	if len(statuses) >= 4 && statuses[3] == "failed" {
		return
	}
	// reason 保存原因，供当前处理流程使用
	reason := strings.TrimSpace(log.ErrorMessage)
	if reason == "" {
		reason = strings.TrimSpace(log.Message)
	}
	if reason == "" {
		reason = "未知错误"
	}
	s.notifier.NotifyAccountEvent(log.CookieID, "token_renewal", "warn", "闲鱼 Cookie 自动续期连续失败", fmt.Sprintf("账号 %s 的 API 自动续期已连续失败 3 次，最近错误：%s", log.CookieID, reason))
}

// cleanupExpiredLogs 负责cleanupExpiredLogs相关处理。
func (s *Scheduler) cleanupExpiredLogs(ctx context.Context) {
	if s.store == nil || s.store.Renewal == nil {
		return
	}
	// days 保存days，供当前处理流程使用
	days := s.settingInt(ctx, "renewal_log_retention_days", 10)
	if // err 保存err，供当前处理流程使用
	err := s.store.Renewal.CleanupLogs(ctx, days); err != nil {
		s.logger.Warn("清理续期日志失败", "err", err)
	}
}

// markSessionExpired 负责mark会话Expired相关处理。
func (s *Scheduler) markSessionExpired(cookieID string) {
	s.cooldown.MarkSessionExpired(cookieID)
}

// isSessionCooled 负责is会话Cooled相关处理。
func (s *Scheduler) isSessionCooled(cookieID string) bool {
	// ok 保存ok，供当前处理流程使用
	ok, _ := s.cooldown.IsSessionCooled(cookieID)
	return ok
}

// settingEnabled 负责设置启用状态相关处理。
func (s *Scheduler) settingEnabled(ctx context.Context, key string, defaultEnabled bool) bool {
	if s.store == nil || s.store.Settings == nil {
		return defaultEnabled
	}
	// value、err 保存value、err，供当前处理流程使用
	value, err := s.store.Settings.Get(ctx, key)
	if err != nil || strings.TrimSpace(value) == "" {
		return defaultEnabled
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	case "0", "false", "no", "off", "disabled":
		return false
	default:
		return defaultEnabled
	}
}

// settingInterval 负责设置Interval相关处理。
func (s *Scheduler) settingInterval(ctx context.Context, key string, defaultInterval time.Duration) time.Duration {
	if s.store == nil || s.store.Settings == nil {
		return defaultInterval
	}
	// value、err 保存value、err，供当前处理流程使用
	value, err := s.store.Settings.Get(ctx, key)
	if err != nil || strings.TrimSpace(value) == "" {
		return defaultInterval
	}
	value = strings.TrimSpace(value)
	if // seconds、err 保存seconds、err，供当前处理流程使用
	seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds > 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	if // d、err 保存d、err，供当前处理流程使用
	d, err := time.ParseDuration(value); err == nil && d > 0 {
		return d
	}
	return defaultInterval
}

// settingInt 负责设置Int相关处理。
func (s *Scheduler) settingInt(ctx context.Context, key string, defaultValue int) int {
	if s.store == nil || s.store.Settings == nil {
		return defaultValue
	}
	// value、err 保存value、err，供当前处理流程使用
	value, err := s.store.Settings.Get(ctx, key)
	if err != nil || strings.TrimSpace(value) == "" {
		return defaultValue
	}
	// n、err 保存n、err，供当前处理流程使用
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return defaultValue
	}
	return n
}

// newBatchID 负责new批次ID相关处理。
func newBatchID() string {
	// b 保存b，供当前处理流程使用
	var b [16]byte
	if // err 保存err，供当前处理流程使用
	_, err := crand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// sleepCtx 负责sleepCtx相关处理。
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	// timer 保存定时器，供当前处理流程使用
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
