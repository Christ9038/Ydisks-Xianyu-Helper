package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/ws"
)

// connectionShutdownJoinTimeout 是连接协调器取消账号运行 Context 后等待自有 worker 退出的总预算。
// recorder 的单次数据库 I/O 已使用同一数量级的超时；该预算避免不响应取消的底层实现永久阻塞 Run。
const connectionShutdownJoinTimeout = WSRecordWriteTimeout

// connectionCoordinator 拥有单账号 WebSocket 拨号、注册、重连和会话轮换编排。
// account 是构造完成后固定的 facade；协调器不单独启动 goroutine，所有任务都继承
// Account.Run 创建的 Context，并在返回前使用有限预算等待 recorder 与迟到续期任务收束。
type connectionCoordinator struct {
	// account 是本协调器唯一服务的账号 facade，New 在对象对外可见前写入。
	account *Account
}

// run 阻塞执行单账号连接主循环，直到调用方取消、账号禁用或不可恢复的认证错误。
// parent 是 account.Manager 提供的运行上下文；返回值保留原 Account.Run 的错误语义。
func (c *connectionCoordinator) run(parent context.Context) error {
	// a 是当前连接协调器绑定的账号 facade；其状态与固定依赖均已在 New 中完成装配。
	a := c.account
	if a == nil {
		return errors.New("账号连接协调器未初始化")
	}
	// ctx、cancel 保存当前 Run 派生的可取消运行上下文及其取消函数。
	ctx, cancel := context.WithCancel(parent)
	defer func() {
		cancel()
		// shutdownCtx、shutdownCancel 提供独立于 parent 取消状态的有限 Join 预算；
		// 这样父 Context 已取消时仍优先收束正常 worker，而不响应取消的外部实现最多占用该预算。
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), connectionShutdownJoinTimeout)
		defer shutdownCancel()
		c.waitForOwnedWorkers(shutdownCtx)
	}()
	// lifecycleStarted 表示 Stop 是否已经在 Run 建立生命周期前完成；false 时必须放弃迟到启动。
	lifecycleStarted := a.lifecycle.start(ctx, cancel)
	if !lifecycleStarted {
		return context.Canceled
	}
	if a.store != nil && a.store.Cookies != nil && !a.store.Cookies.GetStatus(ctx, a.CookieID) {
		a.logger.Info("账号在启动续期前已禁用")
		return nil
	}
	a.startWSRecorder(ctx)
	// 官网 /im 启动时执行 auto-login plugin；成功后 location.reload() 会重建
	// FishEngine 和页面级 device ID。Go 客户端用 HTTP 复刻续期，并在成功时
	// 只重建这一本地运行时身份。续期失败不能用网页 DOM 阻断 token + WS；
	// Chromium 仅用于读取本机指纹和处理 token 滑块。
	if a.renewer != nil {
		if a.tryAPIRenew(ctx) {
			a.rotatePageDeviceID()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// 账号是否被禁用。
		if !a.store.Cookies.GetStatus(ctx, a.CookieID) {
			a.logger.Info("账号已禁用，停止主循环")
			return nil
		}

		// 每次新建 IM 连接前吸收数据库中的最新 Cookie。健康连接不会被续期任务
		// 主动打断；Cookie 变化只会使本次重连放弃旧 token 并重新派生。
		a.reloadCookieFromDB(ctx)

		// 官网先完成原生 WebSocket 握手，再从 authTokenCallback 获取本次
		// 连接专用 token，最后发送 /reg。
		// conn、err 保存当前 WebSocket 连接及拨号或后续认证阶段的错误。
		conn, err := a.wsDialer.Dial(ctx, ws.Config{Recorder: a.wsRecorder()}, a.logger)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			a.logger.Error("WS 握手失败", "err", err)
			// retryErr 保存握手失败处理后的终止错误；非 nil 时连接循环必须退出。
			if retryErr := a.handleWSConnectFailure(ctx, err); retryErr != nil {
				return retryErr
			}
			continue
		}
		// The official web client calls
		// mtop.taobao.idlemessage.pc.login.token from authTokenCallback for every
		// loginV2/reConnect attempt. Do the same here: an access token belongs to
		// one connection attempt and must never be reused for a later /reg.
		// token、cookieStr、err 保存本次连接专用 Token、其绑定 Cookie 快照与获取错误。
		token, cookieStr, err := a.acquireFreshConnectionToken(ctx)
		if err != nil {
			_ = conn.Close()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			a.logger.Error("获取 token 失败", "err", err)
			a.mu.Lock()
			// status 是刷新失败分类快照，用于决定是否累加失败计数。
			status := a.lastTokenStatus
			// nonCounted 表示验证码与冷却失败不计入连接 Token 获取失败次数。
			nonCounted := tokenFailureIsNonCounted(status)
			if !nonCounted {
				a.tokenFetchFailures++
			}
			// tokenFailures 是当前失败次数快照，只用于保留原有诊断路径。
			tokenFailures := a.tokenFetchFailures
			a.mu.Unlock()
			a.setRuntimeError(ctx, err)
			_ = tokenFailures // 仅用于诊断；官网不会按次数永久禁用账号。
			if mtop.IsRiskVerificationErr(err) {
				a.logger.Warn("闲鱼要求安全验证，停止本次消息登录", "err", err)
				a.alertEvent(ctx, EventSecurityVerification, AlertLevelWarn, "闲鱼要求安全验证",
					"账号触发闲鱼风控验证（滑块/人脸等），需要重新登录或完成人工验证。")
				return err
			}
			if mtop.IsSessionExpiredErr(err) {
				// reason 是当前凭证失效或恢复失败时展示给运行状态与通知的原因。
				reason := "登录凭证已失效，正在立即续期"
				a.logger.Warn("token API 检测到 Session 过期，停止重试并开始即时续期", "err", err)
				a.clearTokenCache(ctx)
				a.setRuntimeState(RuntimeReconnecting, reason)
				a.notifyOffline(ctx, reason+"："+errString(err))
				if a.handler != nil && a.handler.OnPasswordLoginRefresh(ctx, a.CookieID) {
					a.reloadCookieFromDB(ctx)
					a.clearCurrentToken()
					a.resetFailures()
					a.setRuntimeState(RuntimeConnecting, "Session 续期成功，正在重新连接")
					continue
				}
				reason = "登录凭证已失效，自动续期失败，请重新扫码登录"
				a.setRuntimeState(RuntimeAuthExpired, reason)
				a.notifyOffline(ctx, reason+"："+errString(err))
				return err
			}
			// 网络或服务端瞬时错误不能让账号运行时永久退出。只要登录 Session
			// 没有明确失效，就持续重试获取连接级 Token。
			a.setRuntimeState(RuntimeReconnecting, "获取消息凭证失败，正在重试")
			a.notifyOffline(ctx, "获取消息凭证失败，正在自动重试："+errString(err))
			// sleepErr 是本次重试退避等待被取消或到期时返回的错误。
			if sleepErr := sleepCtx(ctx, a.tokenRetryDelay()); sleepErr != nil {
				return sleepErr
			}
			continue
		}
		a.mu.Lock()
		a.currentToken = token
		a.CookieStr = cookieStr
		a.tokenFetchFailures = 0
		a.mu.Unlock()
		a.setRuntimeState(RuntimeConnecting, "登录凭证有效，正在连接消息服务")
		a.logger.Info("token 刷新成功")

		// 2) 使用刚获得的 token 注册已经打开的 WS。
		a.mu.Lock()
		// deviceID 是本次注册使用的页面生命周期设备标识快照。
		deviceID := a.deviceID
		// tokenCredentialFP 是本次 Token 绑定的完整凭证指纹快照。
		tokenCredentialFP := a.tokenCredentialFP
		a.mu.Unlock()
		// registerResult 是凭证快照复核与 WebSocket 注册的窄边界结果。
		registerResult := a.registerConnection(ctx, conn, deviceID, token, tokenCredentialFP)
		if !registerResult.Registered {
			_ = conn.Close()
			a.reloadCookieFromDB(ctx)
			continue
		}
		err = registerResult.Err
		if err != nil {
			_ = conn.Close()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			a.logger.Error("WS 注册失败", "err", err)
			// retryErr 保存注册失败处理后的终止错误；非 nil 时连接循环必须退出。
			if retryErr := a.handleWSConnectFailure(ctx, err); retryErr != nil {
				return retryErr
			}
			continue
		}
		a.runtimeMu.Lock()
		a.conn = conn
		a.connStartedAt = time.Now()
		a.connFailures = 0
		a.networkFailures = 0
		a.authExpiredAlerted = false // 连接成功，复位 auth_expired 告警标记
		// shouldRecovered 表示本次连接是否结束了一个已告警的离线周期。
		shouldRecovered := a.offlineNotified
		// offlineSince 是本次恢复前离线周期的起始时间快照。
		offlineSince := a.offlineSince
		a.offlineNotified = false
		a.offlineSince = time.Time{}
		a.lastOfflineReason = ""
		a.runtimeMu.Unlock()
		a.setRuntimeState(RuntimeOnline, "消息服务连接正常")
		a.notifyTransportReady(ctx)
		if shouldRecovered {
			a.alertEvent(ctx, EventAccountRecovered, AlertLevelInfo, "账号已恢复在线",
				fmt.Sprintf("账号 %s 已重新连接闲鱼消息服务。掉线开始时间：%s。", a.CookieID, formatTimeOrUnknown(offlineSince)))
		}

		// 3) 健康连接维持心跳和收包，并在服务端 Token 过期前主动关闭，
		// 进入下一轮连接以重新调用 Token API 和 /reg。
		// refreshAt 和 expiresAt 是本次连接 Token 的轮换时间与服务端过期时间快照。
		a.mu.Lock()
		// refreshAt 是本次已注册连接主动轮换 Token 的时间点。
		refreshAt := a.tokenRefreshAt
		// expiresAt 是服务端声明的 Token 过期时间，仅用于诊断日志。
		expiresAt := a.tokenExpiresAt
		a.mu.Unlock()
		// session 是本次已注册连接的心跳、接收和 Token 轮换收束结果。
		session := a.runConnectionSession(ctx, conn, refreshAt)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// 连接结束：只有认证类失败才清 token。已经建立后的网络断线继续
		// 使用内存 token 与数据库缓存，避免无意义调用 Token API。
		if session.Rotated {
			a.logger.Info("WS Token 到达提前轮换时间，正在重新获取 Token", "expires_at", expiresAt, "remaining", time.Until(expiresAt).Round(time.Second))
			a.clearConnectionToken(ctx)
			a.setRuntimeState(RuntimeReconnecting, "WS Token 即将到期，正在主动轮换")
			continue
		}
		if ws.IsConnectLimitError(session.ReceiveErr) {
			a.clearConnectionToken(ctx)
			// reason 是服务端移除会话时记录给用户与通知的认证失效原因。
			reason := "消息会话已被服务端移除"
			a.setRuntimeState(RuntimeAuthExpired, reason)
			a.notifyOffline(ctx, reason)
			return nil
		}
		if ws.IsAuthenticationError(session.ReceiveErr) {
			// 官网把 /push/kickout 转成 UNCONNECTED，页面监听器随后立即
			// reConnect，并由 authTokenCallback 获取新的连接凭证。
			a.clearConnectionToken(ctx)
			a.setRuntimeState(RuntimeReconnecting, "消息会话被服务端踢下线，正在重新连接")
			continue
		}
		// 心跳失败会先关闭连接，ReceiveLoop 往往只观察到 context canceled。
		// 官网以心跳 Promise 的 reject 为真实断线原因并立即 reConnect。
		// receiveErr 是后续错误分类使用的本地副本；心跳错误优先于 context canceled。
		// receiveErr 是用于下方重连分类的最终接收错误。
		receiveErr := session.ReceiveErr
		if session.HeartbeatErr != nil && !errors.Is(session.HeartbeatErr, context.Canceled) &&
			(receiveErr == nil || errors.Is(receiveErr, context.Canceled)) {
			receiveErr = session.HeartbeatErr
		}

		// 正常 close 的 async-for 会直接进入下一轮，不计任何失败。
		if receiveErr == nil {
			a.clearConnectionToken(ctx)
			a.setRuntimeState(RuntimeReconnecting, "消息连接已结束，正在重新连接")
			continue
		}

		if isEstablishedNetworkError(receiveErr) || errors.Is(receiveErr, context.Canceled) {
			a.clearConnectionToken(ctx)
			a.setRuntimeState(RuntimeReconnecting, "网络连接已断开，正在重新连接")
			a.logger.Warn("WS 网络连接结束", "err", receiveErr, "connected_duration", session.ConnectedDuration.Round(time.Second), "heartbeat_err", session.HeartbeatErr)
			// 官网当前页面在 CONN_UNCONNECTED 事件后立即调用 reConnect。
			continue
		}

		// 其他已经建立后的非认证错误同样进入 UNCONNECTED；不升级为
		// 密码登录、指数退避或永久禁用。
		a.clearConnectionToken(ctx)
		a.setRuntimeState(RuntimeReconnecting, "消息连接已断开，正在重新连接")
		a.logger.Warn("WS 连接结束", "err", receiveErr, "heartbeat_err", session.HeartbeatErr)
	}
}

// waitForOwnedWorkers 在 shutdownCtx 的总预算内等待连接协调器启动的 recorder 与迟到续期任务。
// 两类任务都继承 Account.Run 的 Context；本方法只观察各自完成信号，不创建新的等待 goroutine。
func (c *connectionCoordinator) waitForOwnedWorkers(shutdownCtx context.Context) {
	// a 是当前连接协调器绑定的账号 facade；run 已在调用本方法前校验其非空。
	a := c.account
	if a == nil {
		return
	}
	if a.recorder != nil && !a.recorder.waitContext(shutdownCtx) {
		a.logger.Warn("等待账号 WS 记录 worker 退出超时")
	}
	if !a.pendingRenewal.waitContext(shutdownCtx) {
		a.logger.Warn("等待账号迟到续期任务退出超时")
	}
}
