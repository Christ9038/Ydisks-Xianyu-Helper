package orders

import (
	"context"
	"errors"
	"fmt"
)

// RefreshSoldPage 是平台已售订单分页请求得到的、可独立提交的一页增量。
type RefreshSoldPage struct {
	// SellerID 是截至本页所有分页请求身份一致且完整时的卖家标识；任一页缺失身份后保持为空，禁止跨账号归属修复。
	SellerID string
	// Orders 是本页平台订单，不包含其他页内容。
	Orders []RefreshSoldOrder
	// CookieUpdate 是截至本页请求结束时观察到的累计 Cookie 会话状态。
	CookieUpdate RefreshCookieUpdate
}

// RefreshSoldPageHandler 逐页接收已通过结构校验的订单页；返回错误会立即终止后续分页请求。
// 实现方在平台请求之外自行加锁提交，调用方（运行时）绝不能在持有凭证锁时调用它。
type RefreshSoldPageHandler func(context.Context, RefreshSoldPage) error

// pagedSoldFetcher 是运行时可选的分页提交能力：每取得一页即交给 handler 落库，使取消或超时不丢失已完成页。
// 未实现该能力的运行时退化为一次性 FetchSoldOrders，再以单页形式提交，行为与旧版一致。
type pagedSoldFetcher interface {
	// FetchSoldOrdersPaged 逐页读取 detail 对应账号的已售订单；返回值只携带最终身份与 Cookie 状态，不累积订单。
	FetchSoldOrdersPaged(context.Context, *PlatformRuntimeData, RefreshSoldPageHandler) (RefreshSoldFetchResult, error)
}

// soldDiscoveryState 保存单次账号发现在多页提交之间共享的状态，仅由发现调用的 goroutine 访问，无需加锁。
type soldDiscoveryState struct {
	// baseline 是最近一次通过复核的账号凭证视图；本调用自己写入新 Cookie 后会刷新它，避免把自己的写入误判为外部变化。
	baseline *PlatformRuntimeData
	// cookiePersisted 与 lastCookie 记录已提交的最新 Cookie 更新，避免每页重复写入相同会话。
	cookiePersisted bool
	// lastCookie 是最近一次已处理的 Cookie 更新。
	lastCookie RefreshCookieUpdate
	// lastRunningCookie 是最近一次同步给运行中账号的 Cookie 值，避免每页重复通知。
	lastRunningCookie string
	// chatsRefreshed 表示本次发现已尝试过联系人缓存刷新，之后的分页不再重复请求平台。
	chatsRefreshed bool
	// discovered、updated 是已提交页累计的新增与变化订单数。
	discovered, updated int
	// result 累计各页恢复明细与新增订单标识。
	result refreshDiscoveryResult
	// activeIDs 是已提交页的远端订单标识并集，仅在完整分页成功后用于缺失订单清理。
	activeIDs map[string]struct{}
	// blockedErr 记录已隔离的归属冲突；存在时其他页继续同步，但禁止缺失清理。
	blockedErr error
}

// credentialUnchanged 判断最新账号凭证视图是否与 state 基线一致，读取失败视为已变化。
func (state *soldDiscoveryState) credentialUnchanged(latest *PlatformRuntimeData, userID int64, reloadErr error) bool {
	return reloadErr == nil && latest != nil && latest.UserID == userID &&
		latest.Value == state.baseline.Value && latest.MetadataJSON == state.baseline.MetadataJSON
}

// fetchSoldPaged 优先使用运行时的分页能力；否则一次读取全部订单后以单页提交。
// 读取失败时不提交任何页（与旧版“不完整快照不落库”一致），但仍返回已观察到的 Cookie 状态供续期保存。
func (s *RefreshService) fetchSoldPaged(ctx context.Context, detail *PlatformRuntimeData, handler RefreshSoldPageHandler) (RefreshSoldFetchResult, error) {
	// paged、ok 是运行时的可选分页能力及其是否实现。
	if paged, ok := s.runtime.(pagedSoldFetcher); ok {
		return paged.FetchSoldOrdersPaged(ctx, detail, handler)
	}
	// fetched、err 保存一次性读取的全部订单及错误。
	fetched, err := s.runtime.FetchSoldOrders(ctx, detail)
	// final 是不再携带订单的最终身份与 Cookie 状态，订单已通过 handler 提交。
	final := RefreshSoldFetchResult{SellerID: fetched.SellerID, CookieUpdate: fetched.CookieUpdate}
	if err != nil {
		return final, err
	}
	// handlerErr 是单页提交错误，非空时放弃本次发现但保留最终 Cookie 状态。
	if handlerErr := handler(ctx, RefreshSoldPage(fetched)); handlerErr != nil {
		return final, handlerErr
	}
	return final, nil
}

// commitSoldPage 在凭证锁内复核代次与凭证后提交 page，并累计到 state。
// 联系人缓存刷新在锁外完成；任何复核失败都返回错误并终止分页，已提交的前序页保留。
func (s *RefreshService) commitSoldPage(ctx context.Context, userID int64, cookieID string, generation *context.Context, state *soldDiscoveryState, page RefreshSoldPage) error {
	// chatErr 保存锁外联系人补关联结果，仅取消类错误会终止本页。
	if chatErr := s.prepareSoldChatsOnce(ctx, cookieID, page.Orders, &state.chatsRefreshed); chatErr != nil {
		return chatErr
	}
	// unlock 保存凭证锁释放函数，所有返回路径都必须释放。
	unlock := s.repository.LockCredentials(cookieID)
	defer unlock()
	// err 是调用方取消或超时错误；此时本页不得写入。
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.discoveryCurrent(cookieID, generation) {
		return errors.New("该账号已有更新的订单同步，本次旧结果未写入")
	}
	// current、reloadErr 保存提交前重新读取的凭证视图及错误。
	current, reloadErr := s.repository.LoadCookiePlatformDetail(ctx, cookieID)
	if !state.credentialUnchanged(current, userID, reloadErr) {
		return ErrRefreshCredentialChanged
	}
	// cookieErr 是响应 Cookie 会话写入错误；失败时本页订单不得提交。
	if cookieErr := s.persistDiscoveryCookie(ctx, cookieID, state, page.CookieUpdate); cookieErr != nil {
		return cookieErr
	}
	// discovered、updated、pageResult、pageIDs、writeErr 保存本页提交统计、恢复明细、远端订单集合和写入错误。
	discovered, updated, pageResult, pageIDs, writeErr := s.persistSoldSnapshot(ctx, userID, cookieID, page.SellerID, page.Orders)
	state.discovered += discovered
	state.updated += updated
	state.result.Restored += pageResult.Restored
	state.result.Reassigned += pageResult.Reassigned
	state.result.Results = append(state.result.Results, pageResult.Results...)
	// id 是本页新增订单标识，并入累计集合。
	for id := range pageResult.NewOrderIDs {
		state.result.NewOrderIDs[id] = struct{}{}
	}
	// id 是本页平台订单标识，并入完整快照集合。
	for id := range pageIDs {
		state.activeIDs[id] = struct{}{}
	}
	if errors.Is(writeErr, ErrSoldRecoveryUnsafe) {
		// 已隔离的归属冲突不阻止其余页同步，但整次发现不再具备清理缺失订单的资格。
		state.blockedErr = writeErr
		return nil
	}
	return writeErr
}

// persistDiscoveryCookie 在凭证锁内提交 update 并刷新 state 基线；与上次已处理的更新相同时跳过。
// 调用方必须持有 cookieID 的凭证锁。
func (s *RefreshService) persistDiscoveryCookie(ctx context.Context, cookieID string, state *soldDiscoveryState, update RefreshCookieUpdate) error {
	if state.cookiePersisted && state.lastCookie == update {
		return nil
	}
	// persistErr 保存响应 Cookie 会话写入错误；失败时本页及后续订单都不得提交。
	_, _, _, persistErr := s.runtime.PersistCookieSession(ctx, state.baseline, update)
	if persistErr != nil {
		return fmt.Errorf("保存订单列表响应 Cookie Jar: %w", persistErr)
	}
	state.cookiePersisted, state.lastCookie = true, update
	if update.Handled && update.Changed {
		// refreshed、refreshErr 保存写入后的最新凭证视图，作为后续页的复核基线。
		refreshed, refreshErr := s.repository.LoadCookiePlatformDetail(ctx, cookieID)
		if refreshErr != nil || refreshed == nil {
			return ErrRefreshCredentialChanged
		}
		state.baseline = refreshed
	}
	return nil
}

// notifyRunningCookie 在凭证锁之外把已提交的新 Cookie 同步给运行中账号，同一值只通知一次。
func (s *RefreshService) notifyRunningCookie(ctx context.Context, cookieID string, state *soldDiscoveryState, update RefreshCookieUpdate) {
	if update.Changed && update.Value != "" && update.Value != state.lastRunningCookie {
		s.runtime.UpdateRunningCookie(ctx, cookieID, update.Value)
		state.lastRunningCookie = update.Value
	}
}
