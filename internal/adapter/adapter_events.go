package adapter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/browser"
	"xianyu-go/internal/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/logsafe"
	"xianyu-go/internal/renewal"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/protocol"
)

// HandleChatMessage 将已解码的入站聊天消息转交给自动化与通知协作链，返回任一业务处理错误。
func (a *Adapter) HandleChatMessage(ctx context.Context, message engine.ChatMessage) error {
	if a.chat == nil {
		return nil
	}
	// Xianyu echoes messages sent by this account back over the same WS. Those
	// sends are already captured by HandleOutgoingChatMessage; recording the
	// echo as incoming would put our own bubble on the buyer side and duplicate it.
	// selfID 保存当前账号从 Cookie 解析出的平台用户标识，用于过滤同账号 WebSocket 回显。
	if selfID := protocol.TransCookies(message.CookieStr)["unb"]; selfID != "" &&
		strings.TrimSuffix(strings.TrimSpace(message.SenderUserID), "@goofish") == strings.TrimSuffix(strings.TrimSpace(selfID), "@goofish") {
		a.logger.Info("忽略账号自身发送的聊天回显", "account", message.AccountID, "chat_id", message.ChatID, "sender_id", message.SenderUserID)
		return nil
	}
	// stored、inserted、err 保存落库消息、是否首次插入及持久化错误。
	stored, inserted, err := a.chat.RecordIncoming(ctx, chat.Incoming{
		AccountID: message.AccountID, ChatID: message.ChatID, BuyerID: message.SenderUserID,
		BuyerName: message.SenderName, Text: message.Text, MessageID: message.MessageID, ItemID: message.ItemID, Raw: message.Raw,
	})
	if stored != nil {
		a.logger.Debug("实时聊天消息已入库", "account", message.AccountID, "chat_id", message.ChatID,
			"message_key", stored.MessageKey, "message_type", stored.MessageType, "inserted", inserted)
	}
	return err
}

// HandleOutgoingChatMessage records successful manual/automatic text sends as
// a side channel; it never participates in platform delivery.
// HandleOutgoingChatMessage 处理Outgoing聊天消息。
func (a *Adapter) HandleOutgoingChatMessage(ctx context.Context, message engine.OutgoingChatMessage) error {
	if a.chat == nil {
		return nil
	}
	// err 用于本次流程后续判断的err
	_, err := a.chat.RecordOutgoingSent(ctx, db.ChatSession{CookieID: message.AccountID, ChatID: message.ChatID,
		BuyerID: message.BuyerID}, message.MessageKey, message.Text)
	return err
}

// HandleMessageRead 接收平台出站消息已读回执，并把非敏感已读状态更新委托给聊天服务。
func (a *Adapter) HandleMessageRead(ctx context.Context, event engine.MessageReadEvent) error {
	if a.chat == nil {
		return nil
	}
	// message、err 保存按平台消息键更新的出站消息及持久化错误，以便缺键时回退会话最近消息。
	message, err := a.chat.MarkOutgoingRead(ctx, event.AccountID, event.MessageID, event.ReadAt)
	if errors.Is(err, db.ErrNotFound) && event.ChatID != "" {
		message, err = a.chat.MarkLatestOutgoingRead(ctx, event.AccountID, event.ChatID, event.ReadAt)
	}
	if err == nil && message != nil {
		a.logger.Info("聊天出站消息已标记已读", "account", event.AccountID, "chat_id", event.ChatID,
			"message_id", event.MessageID, "message_key", message.MessageKey, "read_status", message.ReadStatus)
	}
	return err
}

// OnAccountAlert 把账号告警（token 失效/自动恢复失败/风控验证等）转发给通知器，
// 推送到该账号已绑定的通知渠道。
// OnAccountAlert 封装On账号Alert业务协调。
func (a *Adapter) OnAccountAlert(ctx context.Context, cookieID, level, title, body string) {
	a.OnAccountEvent(ctx, cookieID, classifyAccountAlertEvent(title, body), level, title, body)
}

// OnAccountEvent 把带类型的账号事件转发给通知器。
func (a *Adapter) OnAccountEvent(_ context.Context, cookieID, eventType, level, title, body string) {
	if a.notifier == nil {
		a.logger.Warn("账号事件通知未发送：通知器未注入", "account", cookieID, "event_type", eventType, "level", level, "title", title)
		return
	}
	if // n、ok 用于本次流程后续判断的n、ok
	n, ok := a.notifier.(notifyEventNotifier); ok {
		n.NotifyAccountEvent(cookieID, eventType, level, title, body)
		return
	}
	a.notifier.NotifyAccountAlert(cookieID, level, title, body)
}

// classifyAccountAlertEvent 封装classify账号AlertEvent业务协调。
func classifyAccountAlertEvent(title, body string) string {
	// msg 用于本次流程后续判断的msg
	msg := strings.ToLower(title + " " + body)
	switch {
	case strings.Contains(msg, "风控"), strings.Contains(msg, "验证"),
		strings.Contains(msg, "滑块"), strings.Contains(msg, "captcha"),
		strings.Contains(msg, "risk"), strings.Contains(msg, "x5sec"):
		return engine.EventSecurityVerification
	case strings.Contains(msg, "禁用"), strings.Contains(msg, "disabled"):
		return engine.EventAccountDisabled
	case strings.Contains(msg, "掉线"), strings.Contains(msg, "离线"),
		strings.Contains(msg, "offline"), strings.Contains(msg, "session"),
		strings.Contains(msg, "登录凭证已失效"):
		return engine.EventAccountOffline
	case strings.Contains(msg, "token"), strings.Contains(msg, "续期"), strings.Contains(msg, "renew"):
		return engine.EventTokenRenewal
	default:
		return engine.EventSystemError
	}
}

// OnTokenCaptchaVerification 处理 token 刷新触发的闲鱼滑块风控。
func (a *Adapter) OnTokenCaptchaVerification(ctx context.Context, cookieID, cookieStr, verificationURL, deviceID string) (*mtop.RefreshResult, bool) {
	// start 用于本次流程后续判断的开始
	start := time.Now()
	// logID 用于本次流程后续判断的logID
	var logID int64
	if a.store != nil && a.store.RiskLogs != nil {
		if // id、err 用于本次流程后续判断的id、err
		id, err := a.store.RiskLogs.Add(ctx, db.RiskControlLog{
			CookieID:         cookieID,
			EventType:        "slider_captcha",
			EventDescription: "触发场景: Token刷新, URL: " + verificationURL,
			ProcessingStatus: "processing",
		}); err == nil {
			logID = id
		} else {
			a.logger.Warn("记录风控日志失败", "account", cookieID, "err", err)
		}
	}

	// showBrowser 用于本次流程后续判断的show浏览器
	showBrowser := false
	// metadataJSON 用于本次流程后续判断的metadataJSON
	metadataJSON := ""
	if a.store == nil || a.store.Cookies == nil {
		a.OnAccountEvent(ctx, cookieID, engine.EventSecurityVerification, engine.AlertLevelWarn,
			"token 风控验证无法保存", "账号存储未初始化，无法保存验证后的 Cookie。")
		return nil, false
	}

	if // d、err 用于本次流程后续判断的d、err
	d, err := a.store.Cookies.GetCookiePlatformRuntimeData(ctx, cookieID); err == nil {
		showBrowser = d.ShowBrowser
		metadataJSON = d.MetadataJSON
	}

	// provider 用于本次流程后续判断的provider
	provider := func(runCtx context.Context, currentCookies string) (string, bool, string, error) {
		if a.captchaReq == nil {
			return "", false, "", nil
		}
		// res、err 用于本次流程后续判断的res、err
		res, err := a.captchaReq.RequestFreshCaptchaURLContext(runCtx, currentCookies, deviceID)
		if err != nil || res == nil {
			return "", false, "", err
		}
		return res.VerificationURL, res.TokenOK, res.UpdatedCookies, nil
	}

	// newCookies 用于本次流程后续判断的newCookies
	newCookies := ""
	// captchaEngine 用于本次流程后续判断的captchaEngine
	captchaEngine := "playwright"
	// remoteHandled 用于本次流程后续判断的remoteHandled
	remoteHandled := false
	// captchaHeadless 用于本次流程后续判断的captchaHeadless
	captchaHeadless := browser.ResolveHeadless(showBrowser)
	// err 用于本次流程后续判断的err
	var err error
	if // remoteConfig 用于本次流程后续判断的remote配置
	remoteConfig := a.loadRemoteCaptchaConfig(ctx, cookieID); remoteConfig != nil {
		newCookies, remoteHandled, err = solveRemoteCaptcha(
			ctx, newRemoteCaptchaHTTPClient(), *remoteConfig,
			cookieID, verificationURL, cookieStr, deviceID, provider,
		)
		if remoteHandled {
			captchaEngine = "remote"
		} else if err != nil {
			a.logger.Warn("远程过滑块不可用，回退本机逻辑", "account", cookieID, "err", err)
			err = nil
		}
	}
	if !remoteHandled {
		// br、ok 用于本次流程后续判断的br、ok
		br, ok := a.browser.(browserTokenCaptchaRecoverer)
		if a.browser == nil || !ok {
			a.OnAccountEvent(ctx, cookieID, engine.EventSecurityVerification, engine.AlertLevelWarn,
				"token 风控验证无法自动处理", "远程服务不可用且浏览器自动化未启用，无法自动完成 token 滑块验证。")
			return nil, false
		}
		if // withEngine、ok 用于本次流程后续判断的withEngine、ok
		withEngine, ok := a.browser.(browserTokenCaptchaEngineRecoverer); ok {
			newCookies, captchaEngine, err = withEngine.TokenCaptchaRecoverWithEngine(
				ctx, cookieID, cookieStr, verificationURL, captchaHeadless, provider,
			)
		} else {
			newCookies, err = br.TokenCaptchaRecover(
				ctx, cookieID, cookieStr, verificationURL, captchaHeadless, provider,
			)
		}
	}
	if err != nil {
		// manualURL 用于本次流程后续判断的manualURL
		manualURL := browser.TokenCaptchaManualVerificationURL(err)
		if strings.TrimSpace(manualURL) == "" {
			manualURL = verificationURL
		}
		a.logger.Warn("token 风控滑块处理失败", "account", cookieID, "err", logsafe.Error(err), "verification_url", logsafe.URL(manualURL))
		if a.store != nil && a.store.RiskLogs != nil {
			_ = a.store.RiskLogs.Update(ctx, logID, db.RiskControlLog{
				ProcessingStatus: "failed",
				ProcessingResult: fmt.Sprintf("token 风控滑块处理失败，耗时: %.2f秒", time.Since(start).Seconds()),
				CaptchaEngine:    captchaEngine,
				ErrorMessage:     err.Error(),
				DurationMS:       time.Since(start).Milliseconds(),
			})
		}
		a.OnAccountEvent(ctx, cookieID, engine.EventSecurityVerification, engine.AlertLevelWarn,
			"token 风控验证失败", err.Error())
		return nil, false
	}
	if strings.TrimSpace(newCookies) == "" {
		return nil, false
	}
	// cookieSnapshot 用于本次流程后续判断的登录凭证Snapshot
	var cookieSnapshot []cookierefresh.BrowserCookie
	// snapshotComplete 用于本次流程后续判断的snapshotComplete
	snapshotComplete := false
	if !remoteHandled {
		if // reader、ok 用于本次流程后续判断的reader、ok
		reader, ok := a.browser.(browserTokenCaptchaSnapshotReader); ok {
			// profileCookies、profileSnapshot、readErr 用于本次流程后续判断的profileCookies、profileSnapshot、readErr
			profileCookies, profileSnapshot, readErr := reader.TokenCaptchaCookieSnapshot(ctx, cookieID, captchaHeadless)
			if readErr != nil {
				a.logger.Warn("读取滑块验证后完整 Cookie Jar 失败，回退 Go 快照合并", "account", cookieID, "err", readErr)
			} else {
				cookieSnapshot = cookierefresh.NormalizeSnapshot(profileSnapshot)
				if cookieSnapshot == nil {
					cookieSnapshot = []cookierefresh.BrowserCookie{}
				}
				snapshotComplete = true
				newCookies = profileCookies
			}
		}
	}
	if !snapshotComplete {
		if // existing、complete 用于本次流程后续判断的existing、complete
		existing, complete := cookierefresh.SnapshotFromMetadataOK(metadataJSON); complete {
			cookieSnapshot = cookierefresh.ReconcileSnapshotWithCookieString(existing, newCookies)
			snapshotComplete = true
		}
	}
	// updatedMetadata 用于本次流程后续判断的updatedMetadata
	updatedMetadata := cookierefresh.MetadataWithoutSnapshot(metadataJSON)
	if snapshotComplete {
		updatedMetadata = cookierefresh.MetadataWithSnapshot(metadataJSON, cookieSnapshot)
	}
	if // err 用于本次流程后续判断的err
	err := a.store.Cookies.UpdateRenewalCookie(ctx, cookieID, newCookies, updatedMetadata, time.Now().Unix()); err != nil {
		a.logger.Warn("保存 token 风控恢复 Cookie 失败", "account", cookieID, "err", err)
		if a.store != nil && a.store.RiskLogs != nil {
			_ = a.store.RiskLogs.Update(ctx, logID, db.RiskControlLog{
				ProcessingStatus: "error",
				ProcessingResult: "滑块完成但保存 Cookie 失败",
				CaptchaEngine:    captchaEngine,
				ErrorMessage:     err.Error(),
				DurationMS:       time.Since(start).Milliseconds(),
			})
		}
		return nil, false
	}
	if a.store.Tokens != nil {
		_ = a.store.Tokens.Clear(ctx, cookieID)
	}
	if a.store != nil && a.store.RiskLogs != nil {
		_ = a.store.RiskLogs.Update(ctx, logID, db.RiskControlLog{
			ProcessingStatus: "success",
			ProcessingResult: fmt.Sprintf("token 风控滑块验证成功（%s），已更新登录凭证，耗时: %.2f秒", captchaEngine, time.Since(start).Seconds()),
			CaptchaEngine:    captchaEngine,
			DurationMS:       time.Since(start).Milliseconds(),
		})
	}
	a.OnAccountEvent(ctx, cookieID, engine.EventSecurityVerification, engine.AlertLevelInfo,
		"token 风控验证已自动恢复", "系统已完成验证并更新登录凭证。")
	return &mtop.RefreshResult{
		UpdatedCookies:         newCookies,
		CookieSnapshot:         cookieSnapshot,
		CookieSnapshotComplete: snapshotComplete,
		CookieStateChanged:     newCookies != cookieStr || snapshotComplete,
	}, true
}

// HandleSystemEvent 把系统卡片事件转发到自动化中心，由自动化规则决定是否执行。
func (a *Adapter) HandleSystemEvent(ctx context.Context, task automation.Task) error {
	if a.automation == nil {
		return nil
	}
	a.logger.Info("系统自动化事件", "account", task.AccountID, "trigger", task.TriggerType, "order_id", task.OrderID)
	return a.automation.HandleTask(ctx, task)
}

// FetchOrderDetail 实现 automation.OrderDetailFetcher。只在本地订单缺少关键字段时
// 调用纯 Go MTOP 客户端，并将详情请求串行化、至少间隔 3 秒，避免短时间高频访问闲鱼。
// FetchOrderDetail 封装Fetch订单Detail业务协调。
func (a *Adapter) FetchOrderDetail(ctx context.Context, cookieID, orderID, itemID, buyerID, _ string) (*automation.OrderDetail, error) {
	if // detail、ok 用于本次流程后续判断的detail、ok
	detail, ok := a.localOrderDetail(ctx, orderID); ok {
		return detail, nil
	}
	if a.orderMTop == nil {
		return nil, fmt.Errorf("订单详情 MTOP 客户端未配置")
	}
	// detail、err 用于本次流程后续判断的detail、err
	detail, err := a.fetchOrderDetailAttempt(ctx, cookieID, orderID)
	if err == nil || !mtop.IsSessionExpiredErr(err) {
		return detail, err
	}
	a.logger.Warn("订单详情检测到 Session 过期，开始即时续期", "account", cookieID, "order_id", orderID)
	if !a.OnPasswordLoginRefresh(ctx, cookieID) {
		return nil, fmt.Errorf("订单详情 Session 过期且即时续期失败: %w", err)
	}
	a.logger.Info("Cookie 即时续期成功，重新请求订单详情", "account", cookieID, "order_id", orderID)
	return a.fetchOrderDetailAttempt(ctx, cookieID, orderID)
}

// fetchOrderDetailAttempt 封装fetch订单Detail尝试次数业务协调。
func (a *Adapter) fetchOrderDetailAttempt(ctx context.Context, cookieID, orderID string) (*automation.OrderDetail, error) {

	a.orderFetchMu.Lock()
	defer a.orderFetchMu.Unlock()
	// 等锁期间其他流程可能已经补齐订单，再检查一次。
	if detail, ok := a.localOrderDetail(ctx, orderID); ok {
		return detail, nil
	}
	if // remain 用于本次流程后续判断的remain
	remain := 3*time.Second - time.Since(a.lastOrderFetch); !a.lastOrderFetch.IsZero() && remain > 0 {
		// timer 用于本次流程后续判断的定时器
		timer := time.NewTimer(remain)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	a.lastOrderFetch = time.Now()
	// credentialUnlock 用于本次流程后续判断的credentialUnlock
	credentialUnlock := a.store.LockAccountCredentials(cookieID)
	// credentialLocked 标识当前调用是否持有账号凭证锁。
	credentialLocked := true
	defer func() {
		if credentialLocked {
			credentialUnlock()
		}
	}()
	platformData, err := a.store.Cookies.GetCookiePlatformRuntimeData(ctx, cookieID) // platformData 只包含订单 MTOP 请求所需的 Cookie 与 metadata。
	if err != nil {
		return nil, fmt.Errorf("读取订单账号最新 Cookie: %w", err)
	}
	if strings.TrimSpace(platformData.Value) == "" && !hasCompleteCookieSnapshot(platformData.MetadataJSON) {
		return nil, fmt.Errorf("订单账号 %s Cookie 为空", cookieID)
	}
	// cookieStr 用于本次流程后续判断的登录凭证Str
	cookieStr := platformData.Value
	// requestCtx 用于本次流程后续判断的请求Ctx
	var requestCtx context.Context
	// cookieSession 用于本次流程后续判断的登录凭证会话
	var cookieSession *mtop.CookieSession
	if // snapshot、complete 用于本次流程后续判断的snapshot、complete
	snapshot, complete := cookierefresh.SnapshotFromMetadataOK(platformData.MetadataJSON); complete {
		requestCtx, cookieSession = mtop.WithCookieSnapshot(ctx, snapshot)
	} else {
		requestCtx, cookieSession = mtop.WithFlatCookieSession(ctx, cookieStr)
	}
	// 账号凭证快照已读取完成；慢速 MTOP 请求不得继续持有共享凭证锁。
	credentialUnlock()
	credentialLocked = false
	// detail、fetchErr 用于本次流程后续判断的detail、fetchErr
	detail, fetchErr := a.orderMTop.FetchOrderDetail(requestCtx, cookieStr, orderID)
	// authoritativeCookies、authoritativeSnapshot、sessionChanged 用于本次流程后续判断的authoritativeCookies、authoritativeSnapshot、sessionChanged
	authoritativeCookies, authoritativeSnapshot, sessionChanged := cookieSession.State()
	// credentialUnlock 保存重新进入凭证提交临界区的释放函数。
	credentialUnlock = a.store.LockAccountCredentials(cookieID)
	credentialLocked = true
	// latestPlatformData 和 reloadErr 保存外部调用完成后的最新凭证快照及重读错误。
	latestPlatformData, reloadErr := a.store.Cookies.GetCookiePlatformRuntimeData(ctx, cookieID)
	if reloadErr != nil {
		fetchErr = errors.Join(fetchErr, fmt.Errorf("重读订单账号最新 Cookie: %w", reloadErr))
	} else if latestPlatformData.Value != platformData.Value || latestPlatformData.MetadataJSON != platformData.MetadataJSON {
		// 凭证在外部调用期间已被其他流程更新，丢弃旧快照响应，避免覆盖更新后的状态。
		sessionChanged = false
		authoritativeSnapshot = nil
	} else if sessionChanged {
		// metadata 用于本次流程后续判断的metadata
		metadata := cookierefresh.MetadataWithoutSnapshot(latestPlatformData.MetadataJSON)
		if authoritativeSnapshot != nil {
			metadata = cookierefresh.MetadataWithSnapshot(latestPlatformData.MetadataJSON, authoritativeSnapshot)
		}
		if // persistErr 用于本次流程后续判断的persistErr
		persistErr := a.store.Cookies.UpdateRenewalCookie(ctx, cookieID, authoritativeCookies, metadata, time.Now().Unix()); persistErr != nil {
			fetchErr = errors.Join(fetchErr, fmt.Errorf("保存订单详情响应 Cookie: %w", persistErr))
		} else {
			cookieStr = authoritativeCookies
			a.wakeCredentialBlockedAutomation(ctx, cookieID)
		}
	}
	if fetchErr != nil {
		return nil, fetchErr
	}
	if detail == nil {
		return nil, errors.New("订单详情 MTOP 接口返回空结果")
	}
	if reloadErr == nil && latestPlatformData.Value == platformData.Value && latestPlatformData.MetadataJSON == platformData.MetadataJSON && !sessionChanged && authoritativeSnapshot == nil && detail.UpdatedCookies != "" && detail.UpdatedCookies != cookieStr {
		// metadata 用于本次流程后续判断的metadata
		metadata := cookierefresh.MetadataWithoutSnapshot(latestPlatformData.MetadataJSON)
		if // err 用于本次流程后续判断的err
		err := a.store.Cookies.UpdateRenewalCookie(ctx, cookieID, detail.UpdatedCookies, metadata, time.Now().Unix()); err != nil {
			return nil, fmt.Errorf("保存订单详情响应 Cookie: %w", err)
		}
		a.wakeCredentialBlockedAutomation(ctx, cookieID)
	}
	// 外部调用产生的凭证结果已处理完成，释放短暂提交临界区。
	credentialUnlock()
	credentialLocked = false
	return &automation.OrderDetail{
		Quantity: detail.Quantity, SpecName: detail.SpecName, SpecValue: detail.SpecValue,
		Amount: detail.Amount, OrderStatus: detail.OrderStatus,
	}, nil
}

// wakeCredentialBlockedAutomation 封装wakeCredentialBlocked自动化业务协调。
func (a *Adapter) wakeCredentialBlockedAutomation(ctx context.Context, cookieID string) {
	if a.credentialWake == nil {
		return
	}
	if // err 用于本次流程后续判断的err
	err := a.credentialWake.WakeCredentialBlocked(ctx, cookieID); err != nil {
		a.logger.Warn("Cookie 更新后唤醒自动化任务失败", "account", cookieID, "err", err)
	}
}

// hasCompleteCookieSnapshot 封装hasComplete登录凭证Snapshot业务协调。
func hasCompleteCookieSnapshot(metadata string) bool {
	// ok 用于本次流程后续判断的ok
	_, ok := cookierefresh.SnapshotFromMetadataOK(metadata)
	return ok
}

// localOrderDetail 命中本地完整订单时直接返回，避免不必要的 MTOP 请求。
func (a *Adapter) localOrderDetail(ctx context.Context, orderID string) (*automation.OrderDetail, bool) {
	// order、err 用于本次流程后续判断的order、err
	order, err := a.store.Orders.Get(ctx, orderID)
	if err != nil || order == nil {
		return nil, false
	}
	if order.Amount == "" || order.Quantity == "" || order.SpecName == "" || order.SpecValue == "" {
		return nil, false
	}
	return &automation.OrderDetail{
		Quantity: order.Quantity, SpecName: order.SpecName, SpecValue: order.SpecValue,
		Amount: order.Amount, OrderStatus: order.OrderStatus,
	}, true
}

// OnPasswordLoginRefresh 是 engine 的历史回调名。Go 客户端只执行协议级
// auto-login 续期；失败后要求重新扫码，不得启动 Chromium 密码登录或页面校验。
// OnPasswordLoginRefresh 封装On密码登录Refresh业务协调。
func (a *Adapter) OnPasswordLoginRefresh(ctx context.Context, cookieID string) bool {
	// cooldown 用于本次流程后续判断的cooldown
	cooldown := a.cooldown
	if cooldown == nil {
		cooldown = renewal.GlobalCooldown
	}
	if // ok、remain、reason 用于本次流程后续判断的ok、remain、reason
	ok, remain, reason := cooldown.PasswordLoginAllowed(cookieID, engine.PasswordLoginMinGap); !ok {
		a.logger.Warn("协议续期冷却中", "account", cookieID, "remain", remain.Round(time.Second))
		a.recordPasswordLogin(ctx, cookieID, 0, "skipped_cooldown", reason, fmt.Sprintf("协议续期冷却中，还需等待 %s", remain.Round(time.Second)))
		return false
	}
	// primary 标识当前调用是否负责执行协议续期；后到调用方只等待其结果。
	if primary := a.beginPasswordRenewalResult(cookieID); !primary {
		a.logger.Warn("协议续期已在处理中，等待当前结果", "account", cookieID)
		return a.waitPasswordRenewalResult(ctx, cookieID)
	}
	// sharedRenewed 保存首次调用的最终恢复结果，并在所有返回路径唤醒等待者。
	sharedRenewed := false
	defer func() { a.finishPasswordRenewalResult(cookieID, sharedRenewed) }()
	// platformData 保存成功或失败时用于审计的账号平台运行数据；明文凭证只在续期工作闭包内短暂使用。
	var platformData db.CookiePlatformRuntimeData
	// lookupFailed 标识是否在调用协议续期前读取账号数据失败，以保持历史审计原因。
	lookupFailed := false
	// accepted、renewed、renewErr 保存协调器登记结果、续期结果和底层错误。
	accepted, renewed, renewErr := a.passwordCoordinator.Run(ctx, cookieID, func(runCtx context.Context) (bool, error) {
		// loaded、loadErr 保存账号平台运行数据及其读取错误。
		loaded, loadErr := a.store.Cookies.GetCookiePlatformRuntimeData(runCtx, cookieID)
		if loadErr != nil {
			lookupFailed = true
			return false, loadErr
		}
		platformData = loaded
		return a.tryProtocolCredentialRenew(runCtx, &platformData)
	})
	sharedRenewed = renewed
	if !accepted {
		if errors.Is(renewErr, accountapp.ErrCredentialRefreshInFlight) {
			a.logger.Warn("协议续期已在处理中", "account", cookieID)
			return false
		}
		a.logger.Warn("协议续期协调器不可用", "account", cookieID, "err", renewErr)
		return false
	}
	if lookupFailed {
		a.logger.Warn("协议续期失败：读取账号详情失败", "account", cookieID, "err", renewErr)
		// message 保存账号详情读取失败时写入登录审计的脱敏说明。
		message := "读取账号详情失败"
		if renewErr != nil {
			message = renewErr.Error()
		}
		a.recordPasswordLogin(ctx, cookieID, 0, "failed", "account_lookup_failed", message)
		return false
	}
	if renewed {
		a.wakeCredentialBlockedAutomation(ctx, cookieID)
		a.recordPasswordLogin(ctx, cookieID, platformData.UserID, "success", "", "Go 协议续期成功")
		return true
	}
	// message 用于本次流程后续判断的消息
	message := "Go 协议续期未恢复登录凭证，请重新扫码登录"
	if renewErr != nil {
		message += "：" + renewErr.Error()
	}
	a.logger.Warn("协议续期未恢复账号", "account", cookieID, "err", renewErr)
	a.OnAccountEvent(ctx, cookieID, engine.EventAccountOffline, engine.AlertLevelWarn, "账号需要重新扫码", message)
	a.recordPasswordLogin(ctx, cookieID, platformData.UserID, "failed", "qr_login_required", message)
	return false
}

// RecoverExpiredCredential 供自动化外部动作在平台明确拒绝旧 Session 后恢复凭证。
func (a *Adapter) RecoverExpiredCredential(ctx context.Context, cookieID string) bool {
	return a.OnPasswordLoginRefresh(ctx, cookieID)
}

// OnCredentialUpdated 接收账号运行时保存的新凭证并唤醒失败任务。
func (a *Adapter) OnCredentialUpdated(ctx context.Context, cookieID string) {
	a.wakeCredentialBlockedAutomation(ctx, cookieID)
}

// OnTransportReady 在 WS 注册完成后立即唤醒发送前明确未执行的任务。
func (a *Adapter) OnTransportReady(ctx context.Context, cookieID string) {
	a.wakeCredentialBlockedAutomation(ctx, cookieID)
}

// beginPasswordLogin 兼容旧测试对账号恢复登记的访问；生产路径统一使用 account.CredentialRefreshCoordinator.Run。
func (a *Adapter) beginPasswordLogin(cookieID string) bool {
	if a.passwordCoordinator == nil {
		return false
	}
	return a.passwordCoordinator.TryBegin(cookieID)
}

// finishPasswordLogin 兼容旧测试对账号恢复收尾的访问；正式调用必须由协调器 Run 自动释放状态。
func (a *Adapter) finishPasswordLogin(cookieID string) {
	if a.passwordCoordinator != nil {
		a.passwordCoordinator.Finish(cookieID)
	}
}

// beginPasswordRenewalResult 为账号首次协议续期创建共享结果状态；返回 false 表示已有调用负责续期。
func (a *Adapter) beginPasswordRenewalResult(cookieID string) bool {
	// passwordResultMu 只保护账号到共享结果状态的映射，绝不跨越慢速外部 I/O。
	a.passwordResultMu.Lock()
	defer a.passwordResultMu.Unlock()
	if a.passwordResults == nil {
		a.passwordResults = make(map[string]*passwordRenewalResult)
	}
	// exists 表示该账号是否已有首个调用负责协议续期，存在时当前调用改为等待共享结果。
	if _, exists := a.passwordResults[cookieID]; exists {
		return false
	}
	a.passwordResults[cookieID] = &passwordRenewalResult{done: make(chan struct{})}
	return true
}

// finishPasswordRenewalResult 写入首次协议续期结果并唤醒同账号等待者；重复收尾保持幂等。
func (a *Adapter) finishPasswordRenewalResult(cookieID string, renewed bool) {
	// passwordResultMu 只保护结果写入、映射删除和 channel 关闭的唯一性。
	a.passwordResultMu.Lock()
	defer a.passwordResultMu.Unlock()
	// result 保存当前账号等待者共享的续期结果状态，负责承载最终结果并关闭通知通道。
	result := a.passwordResults[cookieID]
	if result == nil {
		return
	}
	result.renewed = renewed
	delete(a.passwordResults, cookieID)
	close(result.done)
}

// waitPasswordRenewalResult 等待首次协议续期完成或调用上下文取消，返回其最终恢复结果。
func (a *Adapter) waitPasswordRenewalResult(ctx context.Context, cookieID string) bool {
	// passwordResultMu 只保护共享状态快照读取，等待发生在释放锁之后。
	a.passwordResultMu.Lock()
	// result 保存锁内取得的共享续期状态快照；释放锁后只等待其完成信号。
	result := a.passwordResults[cookieID]
	a.passwordResultMu.Unlock()
	if result == nil {
		return false
	}
	select {
	case <-result.done:
		return result.renewed
	case <-ctx.Done():
		return false
	}
}

// recordPasswordLogin 封装record密码登录业务协调。
func (a *Adapter) recordPasswordLogin(ctx context.Context, cookieID string, userID int64, status, failureReason, message string) {
	if a.store == nil || a.store.LoginLogs == nil {
		return
	}
	if // err 用于本次流程后续判断的err
	err := a.store.LoginLogs.Add(ctx, db.AccountLoginLog{
		CookieID:          cookieID,
		UserID:            userID,
		Method:            "protocol",
		Status:            status,
		Message:           truncateMessage(message, 500),
		TriggerReason:     "令牌/Session过期",
		FailureReason:     failureReason,
		ErrorMessage:      truncateMessage(message, 500),
		AccountIdentifier: cookieID,
		DurationMS:        0,
		CreatedAt:         time.Now().Unix(),
	}); err != nil {
		a.logger.Warn("记录协议续期日志失败", "account", cookieID, "err", err)
	}
}

// truncateMessage 封装truncate消息业务协调。
func truncateMessage(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n]
}

// tryProtocolCredentialRenew 封装tryProtocolCredentialRenew业务协调。
func (a *Adapter) tryProtocolCredentialRenew(ctx context.Context, d *db.CookiePlatformRuntimeData) (bool, error) {
	if d == nil {
		return false, nil
	}
	// current 用于本次流程后续判断的current
	current := d.Value
	// api 用于本次流程后续判断的api
	api := a.renewSvc
	// save 用于本次流程后续判断的save
	save := func(cookieStr string, setCookies []string, completeSnapshot []cookierefresh.BrowserCookie) error {
		if cookieStr == current && len(setCookies) == 0 && completeSnapshot == nil {
			return nil
		}
		// metadata 用于本次流程后续判断的metadata
		metadata := cookierefresh.MetadataWithoutSnapshot(d.MetadataJSON)
		if completeSnapshot != nil {
			// API 在完整 Jar 基础上得到的快照是权威结果，包含
			// 服务端删除和新的 Domain/Path/expiry 属性。
			metadata = cookierefresh.MetadataWithSnapshot(d.MetadataJSON, completeSnapshot)
		}
		if // err 用于本次流程后续判断的err
		err := a.store.Cookies.UpdateRenewalCookie(ctx, d.ID, cookieStr, metadata, time.Now().Unix()); err != nil {
			a.logger.Warn("轻量续期保存 Cookie 失败", "account", d.ID, "err", err)
			return err
		}
		// valueChanged 用于本次流程后续判断的值Changed
		valueChanged := cookieStr != current
		current = cookieStr
		d.Value = cookieStr
		d.MetadataJSON = metadata
		if valueChanged && a.store.Tokens != nil {
			if // err 用于本次流程后续判断的err
			err := a.store.Tokens.Clear(ctx, d.ID); err != nil {
				// Token 仅是运行期缓存；Cookie 已原子提交后不能再把整次
				// 续期报告成失败，否则调用方可能用旧凭证重试并覆盖新 Jar。
				a.logger.Warn("轻量续期清理旧 Token 缓存失败", "account", d.ID, "err", err)
			}
		}
		return nil
	}
	// 官网始终先由 auto-login plugin 按 havana_lgc_exp/cookie3_bak_exp
	// 决定是否调用 silentHasLogin。Go 客户端复刻该 HTTP 协议，不加载页面。
	// runCtx、cancel 用于本次流程后续判断的运行Ctx、cancel
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	// res、err 用于本次流程后续判断的res、err
	res, err := api.RenewAfterSessionExpired(runCtx, current, cookierefresh.SnapshotFromMetadata(d.MetadataJSON))
	if res != nil {
		// completeSnapshot 用于本次流程后续判断的completeSnapshot
		var completeSnapshot []cookierefresh.BrowserCookie
		if res.CookieSnapshotComplete {
			completeSnapshot = res.CookieSnapshot
			if completeSnapshot == nil {
				completeSnapshot = []cookierefresh.BrowserCookie{}
			}
		}
		if // saveErr 用于本次流程后续判断的saveErr
		saveErr := save(res.NewCookies, res.SetCookies, completeSnapshot); saveErr != nil {
			return false, saveErr
		}
		if res.HasPending() {
			// 恢复路径不能把“底层请求仍在进行”提前记为成功，否则上层会
			// 重置失败计数并继续使用未确认的旧凭证。这里等待最终响应；定时
			// 调度仍使用异步 watcher，不阻塞健康账号。
			// waitCtx、waitCancel 用于本次流程后续判断的waitCtx、wait取消
			waitCtx, waitCancel := context.WithTimeout(ctx, 35*time.Second)
			// late、waitErr 用于本次流程后续判断的late、waitErr
			late, waitErr := res.AwaitPending(waitCtx)
			waitCancel()
			if late == nil {
				if waitErr != nil {
					return false, waitErr
				}
				return false, errors.New("协议续期底层响应未返回结果")
			}
			// lateSnapshot 用于本次流程后续判断的lateSnapshot
			var lateSnapshot []cookierefresh.BrowserCookie
			if late.CookieSnapshotComplete {
				lateSnapshot = late.CookieSnapshot
				if lateSnapshot == nil {
					lateSnapshot = []cookierefresh.BrowserCookie{}
				}
			}
			if // saveErr 用于本次流程后续判断的saveErr
			saveErr := save(late.NewCookies, late.SetCookies, lateSnapshot); saveErr != nil {
				return false, saveErr
			}
			if waitErr != nil {
				return false, waitErr
			}
			if late.Success {
				a.logger.Info("Go 协议续期迟到响应成功", "account", d.ID)
				return true, nil
			}
			// message 用于本次流程后续判断的消息
			message := strings.TrimSpace(late.Message)
			if message == "" {
				message = "协议续期未通过"
			}
			return false, errors.New(message)
		}
		if err == nil && res.Success {
			a.logger.Info("Go 协议续期成功", "account", d.ID)
			return true, nil
		}
		if err == nil {
			// message 保存平台续期失败原因；空原因使用稳定的非敏感默认提示。
			message := strings.TrimSpace(res.Message)
			if message == "" {
				message = "协议续期未通过"
			}
			return false, errors.New(message)
		}
	}
	if err == nil {
		err = errors.New("协议续期未返回结果")
	}
	return false, err
}

// 编译期保证 *Adapter 同时实现 engine.Handler 与 automation.OrderDetailFetcher。
var (
	_ engine.Handler                = (*Adapter)(nil)
	_ automation.OrderDetailFetcher = (*Adapter)(nil)
)
