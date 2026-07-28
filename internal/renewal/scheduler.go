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
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	apirenew "xianyu-go/internal/xianyu/renew"
)

const (
	loginRenewInterval     = 600 * time.Second
	cookiesRefreshInterval = 600 * time.Second
	apiCookieRenewInterval = 4 * time.Hour
	accountRequestInterval = time.Minute
	sessionExpiredCooldown = 300 * time.Second
	passwordLoginCooldown  = 300 * time.Second
	passwordErrorCooldown  = 5 * time.Hour
)

const (
	loginRenewEnabledSetting      = "renewal.login_renew.enabled"
	loginRenewIntervalSetting     = "renewal.login_renew.interval_seconds"
	apiCookieRenewEnabledSetting  = "renewal.api_cookie_renew.enabled"
	apiCookieRenewIntervalSetting = "renewal.api_cookie_renew.interval_seconds"
	cookiesRefreshEnabledSetting  = "renewal.cookies_refresh.enabled"
	cookiesRefreshIntervalSetting = "renewal.cookies_refresh.interval_seconds"
)

type AccountStarter interface {
	Start(ctx context.Context, cookieID, cookieValue string) error
}

type accountRestarter interface {
	Restart(ctx context.Context, cookieID string) error
}

type PasswordRefresher interface {
	OnPasswordLoginRefresh(ctx context.Context, cookieID string) bool
}

type Scheduler struct {
	store     *db.Store
	starter   AccountStarter
	refresher PasswordRefresher
	logger    *slog.Logger
	mtop      *mtop.ClientImpl
	api       apirenew.Service
	cooldown  *CooldownManager
	notifier  RenewalNotifier
}

type RenewalNotifier interface {
	NotifyAccountEvent(cookieID, eventType, level, title, body string)
}

// NewScheduler 的最后一个参数可选，用于发送连续续期失败告警，保持旧调用方兼容。
func NewScheduler(store *db.Store, starter AccountStarter, refresher PasswordRefresher, logger *slog.Logger, notifiers ...RenewalNotifier) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
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
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	go s.runFixed(ctx, "login_renew", loginRenewEnabledSetting, loginRenewIntervalSetting, false, loginRenewInterval, s.executeLoginRenew)
	// 官网静默插件在账号启动时执行，并由此任务按保守频率持续检查；闲鱼
	// 下发的 sdkSilent 疲劳窗口仍会在请求前阻止重复续期。
	// cookies_refresh 仅作为旧配置名的兼容别名。两套配置必须汇聚到同一个
	// goroutine，否则同时开启时会重复续期并连续重启同一账号。
	go s.runAPICookieRenewFixed(ctx)
}

func (s *Scheduler) runAPICookieRenewFixed(ctx context.Context) {
	if s.apiRenewEnabled(ctx) {
		s.executeAPICookieRenew(ctx)
	}
	for {
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

func (s *Scheduler) runFixed(ctx context.Context, name, settingKey, intervalKey string, defaultEnabled bool, defaultInterval time.Duration, fn func(context.Context)) {
	if s.settingEnabled(ctx, settingKey, defaultEnabled) {
		fn(ctx)
	}
	for {
		interval := s.settingInterval(ctx, intervalKey, defaultInterval)
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

func (s *Scheduler) executeLoginRenew(ctx context.Context) {
	s.cleanupExpiredLogs(ctx)
	batchID := newBatchID()
	accounts, err := s.store.Cookies.ActiveRenewalAccounts(ctx)
	if err != nil {
		s.logger.Warn("login_renew 加载账号失败", "err", err)
		return
	}
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

func (s *Scheduler) loginRenewOne(ctx context.Context, batchID string, account db.RenewalAccount) {
	credentialUnlock := s.store.LockAccountCredentials(account.ID)
	defer credentialUnlock()
	started := time.Now()
	latest, err := s.reloadRenewalAccount(ctx, account)
	if err != nil {
		s.addLoginLog(ctx, batchID, account.ID, "failed", "重读账号凭证失败: "+err.Error(), nil, time.Since(started))
		return
	}
	account = latest
	if !account.Enabled {
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var mtopCtx context.Context
	var cookieSession *mtop.CookieSession
	if snapshot, ok := cookierefresh.SnapshotFromMetadataOK(account.MetadataJSON); ok {
		mtopCtx, cookieSession = mtop.WithCookieSnapshot(runCtx, snapshot)
	} else {
		mtopCtx, cookieSession = mtop.WithFlatCookieSession(runCtx, account.Value)
	}
	res, callErr := s.mtop.CheckLoginStatusContext(mtopCtx, account.Value)

	// 对齐浏览器在响应头到达时立即应用 Set-Cookie 的时序。权威 session
	// 因此必须在处理请求或解析错误之前持久化，否则下次请求会
	// 从数据库回滚到旧 Jar。
	updated := []string(nil)
	value, snapshot, changed := cookieSession.State()
	// 完整 Jar 即使本轮没有变化也已经权威接管请求；不能因扁平 Cookie
	// 的顺序或尾分号不同回退写入并清掉快照。
	sessionHandled := snapshot != nil
	if changed {
		updated = cookierefresh.ChangedCookieNames(account.Value, value)
		metadata := cookierefresh.MetadataWithoutSnapshot(account.MetadataJSON)
		if snapshot != nil {
			metadata = cookierefresh.MetadataWithSnapshot(account.MetadataJSON, snapshot)
		}
		if persistErr := s.store.Cookies.UpdateRenewalCookie(ctx, account.ID, value, metadata, time.Now().Unix()); persistErr != nil {
			if callErr != nil {
				persistErr = errors.Join(callErr, fmt.Errorf("保存 loginuser.get 响应 Cookie Jar: %w", persistErr))
			}
			s.addLoginLog(ctx, batchID, account.ID, "failed", persistErr.Error(), updated, time.Since(started))
			s.logger.Warn("login_renew 保存响应 Cookie Jar 失败", "account", account.ID, "err", persistErr)
			return
		}
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
			metadata := cookierefresh.MetadataWithoutSnapshot(account.MetadataJSON)
			if err := s.store.Cookies.UpdateRenewalCookie(ctx, account.ID, res.UpdatedCookies, metadata, time.Now().Unix()); err != nil {
				s.addLoginLog(ctx, batchID, account.ID, "failed", "保存 Cookie 失败: "+err.Error(), updated, time.Since(started))
				return
			}
		}
	}
	s.addLoginLog(ctx, batchID, account.ID, res.Status, res.Message, updated, time.Since(started))
	if res.Status == mtop.LoginStatusSessionExpired || res.Status == mtop.LoginStatusTokenEmpty {
		s.markSessionExpired(account.ID)
	}
}

func (s *Scheduler) executeAPICookieRenew(ctx context.Context) {
	s.cleanupExpiredLogs(ctx)
	batchID := newBatchID()
	accounts, err := s.store.Cookies.ActiveRenewalAccounts(ctx)
	if err != nil {
		s.logger.Warn("api_cookie_renew 加载账号失败", "err", err)
		return
	}
	for i, account := range accounts {
		s.apiCookieRenewOne(ctx, batchID, account)
		if i < len(accounts)-1 {
			_ = sleepCtx(ctx, accountRequestInterval)
		}
	}
}

func (s *Scheduler) apiCookieRenewOne(ctx context.Context, batchID string, account db.RenewalAccount) {
	credentialUnlock := s.store.LockAccountCredentials(account.ID)
	credentialLocked := true
	defer func() {
		if credentialLocked {
			credentialUnlock()
		}
	}()
	started := time.Now()
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
	res, callErr := s.renewAPI(ctx, account.Value, cookierefresh.SnapshotFromMetadata(account.MetadataJSON))
	if res == nil {
		message := "接口续期未返回结果"
		if callErr != nil {
			message = callErr.Error()
		}
		s.addAPILog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "failed", ErrorMessage: message, RenewMethod: "auto_login_plugin", DurationMS: time.Since(started).Milliseconds()})
		s.logger.Warn("接口续期任务失败", "account", account.ID, "err", message)
		return
	}
	if res.HasPending() {
		s.watchPendingAPIRenew(batchID, account.ID, res)
	}
	stepDetails := make([]string, 0, len(res.StepDetails)+1)
	for _, step := range res.StepDetails {
		stepDetails = append(stepDetails, fmt.Sprintf("%s: http=%d business_ok=%v set_cookie=%d", step.Name, step.HTTPStatus, step.BusinessOK, step.SetCookieCount))
	}
	stepDetails = append(stepDetails, fmt.Sprintf("result: success=%v skipped=%v reason=%s", res.Success, res.Skipped, res.SkipReason))
	updated := cookierefresh.ChangedCookieNames(account.Value, res.NewCookies)
	credentialChanged := false
	if res.CookieSnapshotComplete || len(res.SetCookies) > 0 || res.NewCookies != account.Value {
		metadata := cookierefresh.MetadataWithoutSnapshot(account.MetadataJSON)
		if res.CookieSnapshotComplete {
			metadata = cookierefresh.MetadataWithSnapshot(account.MetadataJSON, res.CookieSnapshot)
		}
		// 扁平 Cookie 的排序和尾分号不是凭证变化；只有字段值变化才需要
		// 写回和重启。完整 Jar 则还要比较 Domain/Path/Expires 等 metadata。
		credentialChanged = len(updated) > 0 || metadata != account.MetadataJSON
		if credentialChanged && !s.saveRenewedCookies(ctx, account.ID, res.NewCookies, metadata) {
			s.addAPILog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "failed", ErrorMessage: "保存 Cookie 失败", UpdatedCookieNames: updated, StepDetails: strings.Join(stepDetails, " | "), RenewMethod: res.RenewMethod, DurationMS: time.Since(started).Milliseconds(), RequestCount: res.RequestCount})
			s.logger.Warn("接口续期任务失败", "account", account.ID, "method", res.RenewMethod, "err", "保存 Cookie 失败")
			return
		}
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
		if restarter, ok := s.starter.(accountRestarter); ok {
			s.logger.Info("接口续期成功，正在重启账号以应用最新登录凭证", "account", account.ID)
			if err := restarter.Restart(ctx, account.ID); err != nil {
				s.addAPILog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "failed", ErrorMessage: "重建消息连接失败: " + err.Error(), UpdatedCookieNames: updated, StepDetails: strings.Join(stepDetails, " | "), RenewMethod: res.RenewMethod, DurationMS: time.Since(started).Milliseconds(), RequestCount: res.RequestCount})
				s.logger.Warn("接口续期成功，但重启账号失败", "account", account.ID, "err", err)
				return
			}
			s.logger.Info("接口续期后的账号重启已完成", "account", account.ID)
		}
	}
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

func (s *Scheduler) renewAPI(ctx context.Context, cookieStr string, snapshot []cookierefresh.BrowserCookie) (*apirenew.Result, error) {
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	return s.api.RenewAPIFirst(runCtx, cookieStr, snapshot)
}

func (s *Scheduler) watchPendingAPIRenew(batchID, cookieID string, result *apirenew.Result) {
	if result == nil || !result.HasPending() || s.store == nil || s.store.Cookies == nil {
		return
	}
	go func() {
		waitCtx, waitCancel := context.WithTimeout(context.Background(), 35*time.Second)
		late, waitErr := result.AwaitPending(waitCtx)
		waitCancel()
		if late == nil {
			if waitErr != nil {
				s.logger.Warn("等待定时静默续期底层响应失败", "account", cookieID, "err", waitErr)
				s.addAPILog(context.Background(), db.RenewalLog{BatchID: batchID, CookieID: cookieID, Status: "failed", ErrorMessage: waitErr.Error(), RenewMethod: "auto_login_plugin"})
			}
			return
		}
		opCtx, opCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer opCancel()
		var finalErr error
		changed := func() bool {
			unlock := s.store.LockAccountCredentials(cookieID)
			defer unlock()
			detail, getErr := s.store.Cookies.GetDetails(opCtx, cookieID)
			if getErr != nil || detail == nil {
				if getErr == nil {
					getErr = db.ErrNotFound
				}
				s.logger.Warn("保存定时静默续期迟到 Cookie 前读取账号失败", "account", cookieID, "err", getErr)
				finalErr = getErr
				return false
			}
			newCookies, metadata, changed := apirenew.RebaseResponseCookies(detail.Value, detail.MetadataJSON, late)
			if !changed {
				return false
			}
			if saveErr := s.store.Cookies.UpdateRenewalCookie(opCtx, cookieID, newCookies, metadata, time.Now().Unix()); saveErr != nil {
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
			if restarter, ok := s.starter.(accountRestarter); ok {
				enabled, _, statusErr := s.store.Cookies.StatusWithReason(opCtx, cookieID)
				if statusErr != nil {
					s.logger.Warn("迟到续期 Cookie 已保存，但读取账号状态失败", "account", cookieID, "err", statusErr)
					finalErr = statusErr
				} else if !enabled {
					s.logger.Info("迟到续期 Cookie 已保存，账号已停用，不执行重启", "account", cookieID)
				} else {
					s.logger.Info("迟到续期 Cookie 已更新，正在重启账号以应用最新登录凭证", "account", cookieID)
					if restartErr := restarter.Restart(opCtx, cookieID); restartErr != nil {
						s.logger.Warn("迟到续期 Cookie 已保存，但重启账号失败", "account", cookieID, "err", restartErr)
						finalErr = restartErr
					} else {
						s.logger.Info("迟到续期 Cookie 更新后的账号重启已完成", "account", cookieID)
					}
				}
			}
		}
		if waitErr != nil {
			s.logger.Warn("定时静默续期底层响应失败，已保存响应 Cookie", "account", cookieID, "err", waitErr)
			finalErr = errors.Join(finalErr, waitErr)
		}
		status := "failed"
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

func (s *Scheduler) apiRenewEnabled(ctx context.Context) bool {
	if s.settingConfigured(ctx, apiCookieRenewEnabledSetting) {
		return s.settingEnabled(ctx, apiCookieRenewEnabledSetting, true)
	}
	return s.settingEnabled(ctx, cookiesRefreshEnabledSetting, true)
}

func (s *Scheduler) apiRenewInterval(ctx context.Context) time.Duration {
	if s.settingConfigured(ctx, apiCookieRenewIntervalSetting) {
		return s.settingInterval(ctx, apiCookieRenewIntervalSetting, apiCookieRenewInterval)
	}
	return s.settingInterval(ctx, cookiesRefreshIntervalSetting, apiCookieRenewInterval)
}

func (s *Scheduler) settingConfigured(ctx context.Context, key string) bool {
	if s.store == nil || s.store.Settings == nil {
		return false
	}
	value, err := s.store.Settings.Get(ctx, key)
	return err == nil && strings.TrimSpace(value) != ""
}

func (s *Scheduler) reloadRenewalAccount(ctx context.Context, account db.RenewalAccount) (db.RenewalAccount, error) {
	detail, err := s.store.Cookies.GetDetails(ctx, account.ID)
	if err != nil || detail == nil {
		if err == nil {
			err = db.ErrNotFound
		}
		return db.RenewalAccount{}, err
	}
	enabled, reason, err := s.store.Cookies.StatusWithReason(ctx, account.ID)
	if err != nil {
		return db.RenewalAccount{}, err
	}
	account.Value = detail.Value
	account.UserID = detail.UserID
	account.Enabled = enabled
	account.DisableReason = reason
	account.Username = detail.Username
	account.Password = detail.Password
	account.ShowBrowser = detail.ShowBrowser
	account.MetadataJSON = detail.MetadataJSON
	account.LastRefreshAt = detail.LastRefreshAt
	return account, nil
}

func (s *Scheduler) saveRenewedCookies(ctx context.Context, cookieID, cookieStr, metadata string) bool {
	if err := s.store.Cookies.UpdateRenewalCookie(ctx, cookieID, cookieStr, metadata, time.Now().Unix()); err != nil {
		s.logger.Warn("保存续期 Cookie 失败", "account", cookieID, "err", err)
		return false
	}
	return true
}

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

func (s *Scheduler) addAPILog(ctx context.Context, log db.RenewalLog) {
	if err := s.store.Renewal.AddAPICookieRenewLog(ctx, log); err != nil {
		s.logger.Warn("记录 API Cookie 续期日志失败", "account", log.CookieID, "status", log.Status, "err", err)
		return
	}
	if s.notifier == nil || log.Status != "failed" || s.store == nil || s.store.Renewal == nil {
		return
	}
	statuses, err := s.store.Renewal.RecentAPICookieRenewStatuses(ctx, log.CookieID, 4)
	if err != nil || len(statuses) < 3 || statuses[0] != "failed" || statuses[1] != "failed" || statuses[2] != "failed" {
		return
	}
	if len(statuses) >= 4 && statuses[3] == "failed" {
		return
	}
	reason := strings.TrimSpace(log.ErrorMessage)
	if reason == "" {
		reason = strings.TrimSpace(log.Message)
	}
	if reason == "" {
		reason = "未知错误"
	}
	s.notifier.NotifyAccountEvent(log.CookieID, "token_renewal", "warn", "闲鱼 Cookie 自动续期连续失败", fmt.Sprintf("账号 %s 的 API 自动续期已连续失败 3 次，最近错误：%s", log.CookieID, reason))
}

func (s *Scheduler) cleanupExpiredLogs(ctx context.Context) {
	if s.store == nil || s.store.Renewal == nil {
		return
	}
	days := s.settingInt(ctx, "renewal_log_retention_days", 10)
	if err := s.store.Renewal.CleanupLogs(ctx, days); err != nil {
		s.logger.Warn("清理续期日志失败", "err", err)
	}
}

func (s *Scheduler) markSessionExpired(cookieID string) {
	s.cooldown.MarkSessionExpired(cookieID)
}

func (s *Scheduler) isSessionCooled(cookieID string) bool {
	ok, _ := s.cooldown.IsSessionCooled(cookieID)
	return ok
}

func (s *Scheduler) settingEnabled(ctx context.Context, key string, defaultEnabled bool) bool {
	if s.store == nil || s.store.Settings == nil {
		return defaultEnabled
	}
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

func (s *Scheduler) settingInterval(ctx context.Context, key string, defaultInterval time.Duration) time.Duration {
	if s.store == nil || s.store.Settings == nil {
		return defaultInterval
	}
	value, err := s.store.Settings.Get(ctx, key)
	if err != nil || strings.TrimSpace(value) == "" {
		return defaultInterval
	}
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds > 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	if d, err := time.ParseDuration(value); err == nil && d > 0 {
		return d
	}
	return defaultInterval
}

func (s *Scheduler) settingInt(ctx context.Context, key string, defaultValue int) int {
	if s.store == nil || s.store.Settings == nil {
		return defaultValue
	}
	value, err := s.store.Settings.Get(ctx, key)
	if err != nil || strings.TrimSpace(value) == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return defaultValue
	}
	return n
}

func newBatchID() string {
	var b [16]byte
	if _, err := crand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
