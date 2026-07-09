package renewal

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	apirenew "xianyu-go/internal/xianyu/renew"
)

const (
	loginRenewInterval       = 600 * time.Second
	cookiesRefreshInterval   = 600 * time.Second
	apiCookieRenewInterval   = 3600 * time.Second
	accountRequestInterval   = time.Second
	browserInitialDelayMax   = 30 * time.Second
	browserSuccessDelayMin   = 60 * time.Second
	browserSuccessDelayRange = 240 * time.Second
	disabledFailureLimit     = 10
	sessionExpiredCooldown   = 300 * time.Second
	passwordLoginCooldown    = 300 * time.Second
	passwordErrorCooldown    = 5 * time.Hour
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

type BrowserRenewer interface {
	BrowserQuickRenew(ctx context.Context, cookieID, cookieStr string, headless bool) (string, error)
	CookiesRefreshSnapshot(ctx context.Context, cookieID, cookieStr string, snapshot []cookierefresh.BrowserCookie, headless bool) (string, []cookierefresh.BrowserCookie, error)
}

type PasswordRefresher interface {
	OnPasswordLoginRefresh(ctx context.Context, cookieID string) bool
}

type Scheduler struct {
	store     *db.Store
	starter   AccountStarter
	browser   BrowserRenewer
	refresher PasswordRefresher
	logger    *slog.Logger
	mtop      *mtop.ClientImpl
	api       apirenew.Service
	cooldown  *CooldownManager
}

func NewScheduler(store *db.Store, starter AccountStarter, browser BrowserRenewer, refresher PasswordRefresher, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		store:     store,
		starter:   starter,
		browser:   browserRenewerOrNil(browser),
		refresher: refresher,
		logger:    logger,
		mtop:      mtop.NewClient(),
		api:       apirenew.Service{},
		cooldown:  GlobalCooldown,
	}
}

func browserRenewerOrNil(browser BrowserRenewer) BrowserRenewer {
	if browser == nil {
		return nil
	}
	v := reflect.ValueOf(browser)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if v.IsNil() {
			return nil
		}
	}
	return browser
}

func (s *Scheduler) Run(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	go s.runFixed(ctx, "login_renew", loginRenewEnabledSetting, loginRenewIntervalSetting, false, loginRenewInterval, s.executeLoginRenew)
	go s.runFixed(ctx, "api_cookie_renew", apiCookieRenewEnabledSetting, apiCookieRenewIntervalSetting, true, apiCookieRenewInterval, s.executeAPICookieRenew)
	go s.runFixed(ctx, "cookies_refresh", cookiesRefreshEnabledSetting, cookiesRefreshIntervalSetting, false, cookiesRefreshInterval, s.executeBrowserCookieRefresh)
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
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := s.mtop.CheckLoginStatusContext(runCtx, account.Value)
	if err != nil {
		s.addLoginLog(ctx, batchID, account.ID, "failed", err.Error(), nil)
		s.logger.Warn("login_renew 失败", "account", account.ID, "err", err)
		return
	}
	if res == nil {
		s.addLoginLog(ctx, batchID, account.ID, "failed", "loginuser.get 未返回结果", nil)
		return
	}
	updated := cookierefresh.ChangedCookieNames(account.Value, res.UpdatedCookies)
	if res.UpdatedCookies != "" && res.UpdatedCookies != account.Value {
		metadata := cookierefresh.MetadataWithoutSnapshot(account.MetadataJSON)
		if err := s.store.Cookies.UpdateRenewalCookie(ctx, account.ID, res.UpdatedCookies, metadata, time.Now().Unix()); err != nil {
			s.addLoginLog(ctx, batchID, account.ID, "failed", "保存 Cookie 失败: "+err.Error(), updated)
			return
		}
		s.clearToken(ctx, account.ID)
	}
	s.addLoginLog(ctx, batchID, account.ID, res.Status, res.Message, updated)
	if res.Status == mtop.LoginStatusSessionExpired || res.Status == mtop.LoginStatusTokenEmpty {
		s.markSessionExpired(account.ID)
		s.triggerPasswordLoginAsync(account.ID)
	}
}

func (s *Scheduler) executeAPICookieRenew(ctx context.Context) {
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
	res, err := s.renewAPI(ctx, account.Value)
	if err != nil {
		s.addAPILog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "failed", ErrorMessage: err.Error()})
		s.logger.Warn("api_cookie_renew 失败", "account", account.ID, "err", err)
		return
	}
	if res == nil {
		s.addAPILog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "failed", ErrorMessage: "接口续期未返回结果"})
		return
	}
	if res.Success {
		updated := cookierefresh.ChangedCookieNames(account.Value, res.NewCookies)
		if res.NewCookies != "" && res.NewCookies != account.Value {
			if !s.saveRenewedCookies(ctx, account.ID, res.NewCookies, cookierefresh.MetadataWithoutSnapshot(account.MetadataJSON)) {
				s.addAPILog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "failed", ErrorMessage: "保存 Cookie 失败", UpdatedCookieNames: updated})
				return
			}
		}
		status := "success"
		if len(updated) > 0 {
			status = "cookie_updated"
		}
		s.addAPILog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: status, Message: res.Message, UpdatedCookieNames: updated, ResponseContent: res.ResponseText})
		return
	}

	if res.NewCookies != "" && res.NewCookies != account.Value {
		updated := cookierefresh.ChangedCookieNames(account.Value, res.NewCookies)
		if !s.saveRenewedCookies(ctx, account.ID, res.NewCookies, cookierefresh.MetadataWithoutSnapshot(account.MetadataJSON)) {
			s.addAPILog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "failed", ErrorMessage: "保存部分 Cookie 失败", UpdatedCookieNames: updated})
			return
		}
		s.logger.Info("api_cookie_renew 保留接口返回的部分 Cookie 更新", "account", account.ID, "updated", strings.Join(updated, ","))
	}

	browserInput := firstNonEmpty(res.NewCookies, account.Value)
	var browserCookies string
	var browserErr error
	if s.browser != nil {
		bctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		browserCookies, browserErr = s.browser.BrowserQuickRenew(bctx, account.ID, browserInput, !account.ShowBrowser)
		cancel()
	} else {
		browserErr = fmt.Errorf("浏览器自动化未启用")
	}
	if browserErr == nil && browserCookies != "" {
		verify, verifyErr := s.renewAPI(ctx, browserCookies)
		if verifyErr == nil && verify != nil && verify.Success {
			finalCookies := firstNonEmpty(verify.NewCookies, browserCookies)
			updated := cookierefresh.ChangedCookieNames(account.Value, finalCookies)
			if finalCookies != account.Value {
				if !s.saveRenewedCookies(ctx, account.ID, finalCookies, cookierefresh.MetadataWithoutSnapshot(account.MetadataJSON)) {
					s.addAPILog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "failed", ErrorMessage: "保存 Cookie 失败", UpdatedCookieNames: updated})
					return
				}
			}
			s.addAPILog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "browser_renewed", Message: "接口续期失败，浏览器续期成功，setLoginSettings验证通过", UpdatedCookieNames: updated, ResponseContent: verify.ResponseText})
			return
		}
		updated := cookierefresh.ChangedCookieNames(account.Value, browserCookies)
		if len(updated) > 0 {
			_ = s.saveRenewedCookies(ctx, account.ID, browserCookies, cookierefresh.MetadataWithoutSnapshot(account.MetadataJSON))
		}
		res.Message = "接口续期和浏览器续期均失败，需要账号密码登录"
	}

	s.markSessionExpired(account.ID)
	s.triggerPasswordLoginAsync(account.ID)
	msg := res.Message
	if browserErr != nil {
		msg = msg + "；浏览器续期失败: " + browserErr.Error()
	}
	updated := cookierefresh.ChangedCookieNames(account.Value, firstNonEmpty(browserCookies, res.NewCookies))
	s.addAPILog(ctx, db.RenewalLog{
		BatchID:            batchID,
		CookieID:           account.ID,
		Status:             "need_password_login",
		ErrorMessage:       msg,
		UpdatedCookieNames: updated,
		ResponseContent:    res.ResponseText,
	})
}

func (s *Scheduler) executeBrowserCookieRefresh(ctx context.Context) {
	batchID := newBatchID()
	accounts, err := s.store.Cookies.AllRenewalAccounts(ctx)
	if err != nil {
		s.logger.Warn("cookies_refresh 加载账号失败", "err", err)
		return
	}
	now := time.Now().Unix()
	for _, account := range accounts {
		if !account.Enabled && s.disabledAccountShouldSkip(ctx, account.ID) {
			continue
		}
		schedule, err := s.store.Renewal.GetCookieRefreshSchedule(ctx, account.ID)
		if err != nil {
			expireAt := now + int64(randomDuration(browserInitialDelayMax).Seconds())
			schedule = &db.CookieRefreshSchedule{CookieID: account.ID, ExpireAt: expireAt, LastStatus: "initialized"}
			_ = s.store.Renewal.UpsertCookieRefreshSchedule(ctx, *schedule)
			_ = s.store.Renewal.AddBrowserCookieRenewLog(ctx, db.RenewalLog{
				BatchID:      batchID,
				CookieID:     account.ID,
				Status:       "initialized",
				Message:      "首次初始化续期时间",
				NextExpireAt: expireAt,
			})
			continue
		}
		if schedule.Disabled || schedule.ExpireAt > now {
			_ = s.store.Renewal.AddBrowserCookieRenewLog(ctx, db.RenewalLog{
				BatchID:      batchID,
				CookieID:     account.ID,
				Status:       "success",
				Message:      fmt.Sprintf("未到期，跳过，剩余 %d 秒", schedule.ExpireAt-now),
				NextExpireAt: schedule.ExpireAt,
			})
			continue
		}
		s.browserCookieRefreshOne(ctx, batchID, account, *schedule)
	}
}

func (s *Scheduler) browserCookieRefreshOne(ctx context.Context, batchID string, account db.RenewalAccount, schedule db.CookieRefreshSchedule) {
	if s.browser == nil {
		schedule.LastStatus = "failed"
		schedule.LastErrorMessage = "浏览器自动化未启用"
		_ = s.store.Renewal.UpsertCookieRefreshSchedule(ctx, schedule)
		_ = s.store.Renewal.AddBrowserCookieRenewLog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "failed", ErrorMessage: schedule.LastErrorMessage})
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	oldSnapshot := cookierefresh.SnapshotFromMetadata(account.MetadataJSON)
	newCookies, newSnapshot, err := s.browser.CookiesRefreshSnapshot(runCtx, account.ID, account.Value, oldSnapshot, !account.ShowBrowser)
	if err != nil {
		schedule.ConsecutiveFailures++
		schedule.LastStatus = "failed"
		schedule.LastError = err.Error()
		schedule.LastErrorMessage = err.Error()
		_ = s.store.Renewal.UpsertCookieRefreshSchedule(ctx, schedule)
		_ = s.store.Renewal.AddBrowserCookieRenewLog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "failed", ErrorMessage: err.Error(), NextExpireAt: schedule.ExpireAt})
		s.logger.Warn("cookies_refresh 失败", "account", account.ID, "err", err)
		return
	}
	metadata := cookierefresh.MetadataWithSnapshot(account.MetadataJSON, newSnapshot)
	updated := cookierefresh.ChangedSnapshotLabels(oldSnapshot, newSnapshot)
	if err := s.store.Cookies.UpdateRenewalCookie(ctx, account.ID, newCookies, metadata, time.Now().Unix()); err != nil {
		_ = s.store.Renewal.AddBrowserCookieRenewLog(ctx, db.RenewalLog{BatchID: batchID, CookieID: account.ID, Status: "failed", ErrorMessage: "保存 Cookie 失败: " + err.Error(), UpdatedCookieNames: updated})
		return
	}
	s.clearToken(ctx, account.ID)
	if !account.Enabled {
		if err := s.store.Cookies.SetStatusWithReason(ctx, account.ID, true, ""); err != nil {
			s.logger.Warn("cookies_refresh 自动启用账号失败", "account", account.ID, "err", err)
		} else if s.starter != nil {
			if err := s.starter.Start(ctx, account.ID, newCookies); err != nil {
				s.logger.Warn("cookies_refresh 启动账号失败", "account", account.ID, "err", err)
			}
		}
	}
	nextExpire := time.Now().Add(browserSuccessDelay()).Unix()
	schedule.Disabled = false
	schedule.ConsecutiveFailures = 0
	schedule.LastError = ""
	schedule.LastStatus = "success"
	schedule.LastErrorMessage = ""
	schedule.LastRefreshAt = time.Now().Unix()
	schedule.ExpireAt = nextExpire
	_ = s.store.Renewal.UpsertCookieRefreshSchedule(ctx, schedule)
	_ = s.store.Renewal.AddBrowserCookieRenewLog(ctx, db.RenewalLog{
		BatchID:            batchID,
		CookieID:           account.ID,
		Status:             "success",
		Message:            fmt.Sprintf("页面校验通过，全量获取到 %d 个浏览器Cookie", len(newSnapshot)),
		UpdatedCookieNames: updated,
		NextExpireAt:       nextExpire,
	})
}

func (s *Scheduler) renewAPI(ctx context.Context, cookieStr string) (*apirenew.Result, error) {
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	return s.api.RenewAPIFirst(runCtx, cookieStr)
}

func (s *Scheduler) saveRenewedCookies(ctx context.Context, cookieID, cookieStr, metadata string) bool {
	if err := s.store.Cookies.UpdateRenewalCookie(ctx, cookieID, cookieStr, metadata, time.Now().Unix()); err != nil {
		s.logger.Warn("保存续期 Cookie 失败", "account", cookieID, "err", err)
		return false
	}
	s.clearToken(ctx, cookieID)
	return true
}

func (s *Scheduler) addLoginLog(ctx context.Context, batchID, cookieID, status, message string, updated []string) {
	_ = s.store.Renewal.AddLoginRenewLog(ctx, db.RenewalLog{BatchID: batchID, CookieID: cookieID, Status: status, Message: message, UpdatedCookieNames: updated})
}

func (s *Scheduler) addAPILog(ctx context.Context, log db.RenewalLog) {
	_ = s.store.Renewal.AddAPICookieRenewLog(ctx, log)
}

func (s *Scheduler) markSessionExpired(cookieID string) {
	s.cooldown.MarkSessionExpired(cookieID)
}

func (s *Scheduler) isSessionCooled(cookieID string) bool {
	ok, _ := s.cooldown.IsSessionCooled(cookieID)
	return ok
}

func (s *Scheduler) triggerPasswordLoginAsync(cookieID string) {
	if s.refresher == nil {
		return
	}
	go s.refresher.OnPasswordLoginRefresh(context.Background(), cookieID)
}

func (s *Scheduler) disabledAccountShouldSkip(ctx context.Context, cookieID string) bool {
	statuses, err := s.store.Renewal.RecentBrowserCookieRenewStatuses(ctx, cookieID, disabledFailureLimit)
	if err != nil || len(statuses) < disabledFailureLimit {
		return false
	}
	for _, status := range statuses {
		if status != "failed" {
			return false
		}
	}
	return true
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

func (s *Scheduler) clearToken(ctx context.Context, cookieID string) {
	if s.store.Tokens == nil {
		return
	}
	if err := s.store.Tokens.Clear(ctx, cookieID); err != nil {
		s.logger.Warn("续期后清除 token 缓存失败", "account", cookieID, "err", err)
	}
}

func browserSuccessDelay() time.Duration {
	return browserSuccessDelayMin + randomDuration(browserSuccessDelayRange)
}

func randomDuration(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	n, err := crand.Int(crand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0
	}
	return time.Duration(n.Int64())
}

func newBatchID() string {
	var b [16]byte
	if _, err := crand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
