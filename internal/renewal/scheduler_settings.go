package renewal

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

// apiRenewEnabled 读取 API Cookie 续期开关，并在设置缺失时保留安全默认值。
func (s *Scheduler) apiRenewEnabled(ctx context.Context) bool {
	if s.settingConfigured(ctx, apiCookieRenewEnabledSetting) {
		return s.settingEnabled(ctx, apiCookieRenewEnabledSetting, true)
	}
	return s.settingEnabled(ctx, cookiesRefreshEnabledSetting, true)
}

// apiRenewInterval 封装apiRenewInterval业务协调。
func (s *Scheduler) apiRenewInterval(ctx context.Context) time.Duration {
	if s.settingConfigured(ctx, apiCookieRenewIntervalSetting) {
		return s.settingInterval(ctx, apiCookieRenewIntervalSetting, apiCookieRenewInterval)
	}
	return s.settingInterval(ctx, cookiesRefreshIntervalSetting, apiCookieRenewInterval)
}

// settingConfigured 封装设置Configured业务协调。
func (s *Scheduler) settingConfigured(ctx context.Context, key string) bool {
	if s.store == nil || s.store.Settings == nil {
		return false
	}
	// value、err 用于本次流程后续判断的value、err
	value, err := s.store.Settings.Get(ctx, key)
	return err == nil && strings.TrimSpace(value) != ""
}

// reloadRenewalAccount 封装reloadRenewal账号业务协调。
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

// saveRenewedCookies 封装saveRenewedCookies业务协调。
func (s *Scheduler) saveRenewedCookies(ctx context.Context, cookieID, cookieStr, metadata string) bool {
	if // err 用于本次流程后续判断的err
	err := s.store.Cookies.UpdateRenewalCookie(ctx, cookieID, cookieStr, metadata, time.Now().Unix()); err != nil {
		s.logger.Warn("保存续期 Cookie 失败", "account", cookieID, "err", err)
		return false
	}
	return true
}

// addLoginLog 封装add登录Log业务协调。
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

// addAPILog 封装addAPILog业务协调。
func (s *Scheduler) addAPILog(ctx context.Context, log db.RenewalLog) {
	if // err 用于本次流程后续判断的err
	err := s.store.Renewal.AddAPICookieRenewLog(ctx, log); err != nil {
		s.logger.Warn("记录 API Cookie 续期日志失败", "account", log.CookieID, "status", log.Status, "err", err)
		return
	}
	if s.notifier == nil || log.Status != "failed" || s.store == nil || s.store.Renewal == nil {
		return
	}
	// statuses、err 用于本次流程后续判断的statuses、err
	statuses, err := s.store.Renewal.RecentAPICookieRenewStatuses(ctx, log.CookieID, 4)
	if err != nil || len(statuses) < 3 || statuses[0] != "failed" || statuses[1] != "failed" || statuses[2] != "failed" {
		return
	}
	if len(statuses) >= 4 && statuses[3] == "failed" {
		return
	}
	// reason 用于本次流程后续判断的原因
	reason := strings.TrimSpace(log.ErrorMessage)
	if reason == "" {
		reason = strings.TrimSpace(log.Message)
	}
	if reason == "" {
		reason = "未知错误"
	}
	s.notifier.NotifyAccountEvent(log.CookieID, "token_renewal", "warn", "闲鱼 Cookie 自动续期连续失败", fmt.Sprintf("账号 %s 的 API 自动续期已连续失败 3 次，最近错误：%s", log.CookieID, reason))
}

// cleanupExpiredLogs 封装cleanupExpiredLogs业务协调。
func (s *Scheduler) cleanupExpiredLogs(ctx context.Context) {
	if s.store == nil || s.store.Renewal == nil {
		return
	}
	// days 用于本次流程后续判断的days
	days := s.settingInt(ctx, "renewal_log_retention_days", 10)
	if // err 用于本次流程后续判断的err
	err := s.store.Renewal.CleanupLogs(ctx, days); err != nil {
		s.logger.Warn("清理续期日志失败", "err", err)
	}
}

// markSessionExpired 封装mark会话Expired业务协调。
func (s *Scheduler) markSessionExpired(cookieID string) {
	s.cooldown.MarkSessionExpired(cookieID)
}

// isSessionCooled 封装is会话Cooled业务协调。
func (s *Scheduler) isSessionCooled(cookieID string) bool {
	// ok 用于本次流程后续判断的ok
	ok, _ := s.cooldown.IsSessionCooled(cookieID)
	return ok
}

// settingEnabled 封装设置启用状态业务协调。
func (s *Scheduler) settingEnabled(ctx context.Context, key string, defaultEnabled bool) bool {
	if s.store == nil || s.store.Settings == nil {
		return defaultEnabled
	}
	// value、err 用于本次流程后续判断的value、err
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

// settingInterval 封装设置Interval业务协调。
func (s *Scheduler) settingInterval(ctx context.Context, key string, defaultInterval time.Duration) time.Duration {
	if s.store == nil || s.store.Settings == nil {
		return defaultInterval
	}
	// value、err 用于本次流程后续判断的value、err
	value, err := s.store.Settings.Get(ctx, key)
	if err != nil || strings.TrimSpace(value) == "" {
		return defaultInterval
	}
	value = strings.TrimSpace(value)
	if // seconds、err 用于本次流程后续判断的seconds、err
	seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds > 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	if // d、err 用于本次流程后续判断的d、err
	d, err := time.ParseDuration(value); err == nil && d > 0 {
		return d
	}
	return defaultInterval
}

// settingInt 封装设置Int业务协调。
func (s *Scheduler) settingInt(ctx context.Context, key string, defaultValue int) int {
	if s.store == nil || s.store.Settings == nil {
		return defaultValue
	}
	// value、err 用于本次流程后续判断的value、err
	value, err := s.store.Settings.Get(ctx, key)
	if err != nil || strings.TrimSpace(value) == "" {
		return defaultValue
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return defaultValue
	}
	return n
}

// newBatchID 封装new批次ID业务协调。
func newBatchID() string {
	// b 用于本次流程后续判断的b
	var b [16]byte
	if // err 用于本次流程后续判断的err
	_, err := crand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// sleepCtx 封装sleepCtx业务协调。
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	// timer 用于本次流程后续判断的定时器
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
