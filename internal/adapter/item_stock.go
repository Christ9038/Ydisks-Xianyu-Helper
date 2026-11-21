package adapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/xianyu/mtop"
)

// stockCall 是在凭证快照上执行的一次平台库存调用，返回响应更新的平面 Cookie（没有变化时为空）。
type stockCall func(ctx context.Context, client mtop.ItemStockClient, cookies string) (string, error)

// GetStock 读取商品当前库存。调用最多一次平台请求，不修改任何平台数据。
func (p *ItemPublishPort) GetStock(ctx context.Context, userID int64, cookieID, itemID string) (itemapp.StockSnapshot, error) {
	// snapshot 保存平台详情转换后的库存视图，由调用闭包写入。
	var snapshot itemapp.StockSnapshot
	// persistErr、callErr 分别是会话写回错误和平台调用错误。
	persistErr, callErr := p.runStockCall(ctx, userID, cookieID, 30*time.Second, func(callCtx context.Context, client mtop.ItemStockClient, cookies string) (string, error) {
		// detail、err 是平台返回的编辑详情。
		detail, err := client.FetchItemEditDetail(callCtx, cookies, itemID)
		if err != nil {
			return "", err
		}
		if detail == nil {
			return "", errors.New("闲鱼商品编辑详情接口未返回结果")
		}
		snapshot = toStockSnapshot(detail)
		return detail.UpdatedCookies, nil
	})
	if callErr != nil {
		return itemapp.StockSnapshot{}, callErr
	}
	if persistErr != nil {
		// 读取没有改变任何平台数据，会话写回失败只记日志，仍把已读取的库存返回给用户。
		p.logger.WarnContext(ctx, "读取商品库存后保存账号会话失败", "account", cookieID, "error", persistErr)
	}
	return snapshot, nil
}

// UpdateStock 以最新平台详情为底修改库存。调用固定最多两次平台请求（读取 + 提交），失败不重试。
func (p *ItemPublishPort) UpdateStock(ctx context.Context, userID int64, cookieID, itemID string, update itemapp.StockUpdate) (itemapp.StockUpdateOutcome, error) {
	// request 是转换后的 MTOP 库存修改请求。
	request := toStockEditRequest(itemID, update)
	// result 保存平台提交结果，由调用闭包写入。
	var result *mtop.ItemStockEditResult
	// persistErr、callErr 分别是会话写回错误和平台调用错误。
	persistErr, callErr := p.runStockCall(ctx, userID, cookieID, 90*time.Second, func(callCtx context.Context, client mtop.ItemStockClient, cookies string) (string, error) {
		// edited、err 是平台提交结果。
		edited, err := client.EditItemStock(callCtx, cookies, request)
		if err != nil {
			return "", err
		}
		if edited == nil {
			return "", errors.New("闲鱼库存修改接口未返回结果")
		}
		result = edited
		return edited.UpdatedCookies, nil
	})
	if callErr != nil {
		return itemapp.StockUpdateOutcome{}, callErr
	}
	// outcome 明确区分平台修改结果与本地会话写回结果。
	outcome := itemapp.StockUpdateOutcome{ItemID: itemID, Message: result.Message, CookiePersisted: persistErr == nil}
	if persistErr != nil {
		return outcome, errors.Join(itemapp.ErrStockSessionPersistence, persistErr)
	}
	return outcome, nil
}

// runStockCall 在库存可选客户端能力上执行一次平台库存调用；其余流程见 runItemEditCall。
func (p *ItemPublishPort) runStockCall(ctx context.Context, userID int64, cookieID string, timeout time.Duration, call stockCall) (persistErr, callErr error) {
	if p == nil || p.store == nil || p.store.Cookies == nil {
		return nil, errors.New("商品库存存储未初始化")
	}
	// stockClient、ok 保存当前 MTOP 客户端的可选库存能力。
	stockClient, ok := p.mtopClient().(mtop.ItemStockClient)
	if !ok {
		return nil, itemapp.ErrStockUnsupported
	}
	return p.runItemEditCall(ctx, userID, cookieID, timeout, itemapp.ErrStockCredentialChanged, func(callCtx context.Context, _ mtop.Client, cookies string) (string, error) {
		return call(callCtx, stockClient, cookies)
	}, mapStockError)
}

// itemEditCall 是在凭证快照上执行的一次平台商品编辑类调用，返回响应更新的平面 Cookie（没有变化时为空）。
type itemEditCall func(ctx context.Context, client mtop.Client, cookies string) (string, error)

// runItemEditCall 按账号凭证锁和会话快照规则执行一次平台商品编辑类调用：加锁读取并校验凭证 → 释放锁 → 平台 I/O →
// 复核凭证后写回会话。credentialChanged 是凭证或归属变化时返回的用例哨兵错误；平台调用错误经 mapErr 映射为应用层哨兵错误；会话过期会触发账号恢复。
func (p *ItemPublishPort) runItemEditCall(ctx context.Context, userID int64, cookieID string, timeout time.Duration, credentialChanged error, call itemEditCall, mapErr func(error) error) (persistErr, callErr error) {
	if p == nil || p.store == nil || p.store.Cookies == nil {
		return nil, errors.New("商品编辑存储未初始化")
	}
	// client 保存当前 MTOP 客户端。
	client := p.mtopClient()
	// unlock 保护账号凭证快照读取。
	unlock := p.store.LockAccountCredentials(cookieID)
	// latest、loadErr 保存加锁后读取的平台凭证视图。
	latest, loadErr := p.store.Cookies.GetCookiePlatformRuntimeData(ctx, cookieID)
	if loadErr != nil || latest.UserID != userID || !hasStoredCredential(latest) {
		unlock()
		return nil, credentialChanged
	}
	// requestCtx、cancel 控制平台调用及其 Token 续期流程的最长执行时间。
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// mtopCtx、cookieSession 保存带当前凭证快照的平台调用上下文。
	mtopCtx, cookieSession := withCookieSnapshot(requestCtx, latest)
	// initialValue、initialMetadata 保存调用前的凭证版本，用于提交阶段防止覆盖并发更新。
	initialValue, initialMetadata := latest.Value, latest.MetadataJSON
	unlock()
	// updatedCookies、platformErr 保存平台响应更新的平面 Cookie 和平台调用错误。
	updatedCookies, platformErr := call(mtopCtx, client, latest.Value)
	if platformErr != nil {
		updatedCookies = ""
	}
	// runtimeCookie、persistErr 保存会话写回后的运行时同步值和持久化错误。
	runtimeCookie, persistErr := p.persistPlatformSession(ctx, userID, cookieID, initialValue, initialMetadata, cookieSession, updatedCookies, credentialChanged)
	if runtimeCookie != "" && p.updateRunningCookie != nil {
		p.updateRunningCookie(ctx, cookieID, runtimeCookie)
	}
	if platformErr != nil {
		if persistErr != nil {
			platformErr = errors.Join(platformErr, fmt.Errorf("保存商品编辑响应 Cookie: %w", persistErr))
		}
		if mtop.IsSessionExpiredErr(platformErr) {
			p.recoverExpired(ctx, cookieID, platformErr)
		}
		return nil, mapErr(platformErr)
	}
	return persistErr, nil
}

// mapStockError 把 MTOP 库存错误映射为应用层哨兵错误，其余错误（网络、未知平台错误）原样返回。
func mapStockError(err error) error {
	switch {
	case errors.Is(err, mtop.ErrItemStockChanged):
		return itemapp.ErrStockChanged
	case errors.Is(err, mtop.ErrItemEditUnsupported):
		return itemapp.ErrStockEditUnsupported
	case errors.Is(err, mtop.ErrItemStockInvalid):
		return errors.Join(itemapp.ErrStockInvalidInput, err)
	case mtop.IsRiskVerificationErr(err):
		return errors.Join(itemapp.ErrStockRiskVerification, err)
	case mtop.IsSessionExpiredErr(err):
		return errors.Join(itemapp.ErrStockSessionExpired, err)
	default:
		return err
	}
}

// toStockSnapshot 把 MTOP 编辑详情转换为应用层库存视图。
func toStockSnapshot(detail *mtop.ItemEditDetail) itemapp.StockSnapshot {
	// snapshot 是转换结果。
	snapshot := itemapp.StockSnapshot{ItemID: detail.ItemID, MultiSpec: detail.MultiSpec, Quantity: detail.Quantity}
	// sku 是当前规格行。
	for _, sku := range detail.SKUs {
		snapshot.SKUs = append(snapshot.SKUs, itemapp.StockSKU{Key: sku.Key, Label: sku.Label, PriceCents: sku.PriceCents, Quantity: sku.Quantity})
	}
	return snapshot
}

// toStockEditRequest 把应用层库存修改转换为 MTOP 请求。
func toStockEditRequest(itemID string, update itemapp.StockUpdate) mtop.ItemStockEditRequest {
	// request 是转换结果，单规格字段与规格行互斥。
	request := mtop.ItemStockEditRequest{ItemID: itemID, Quantity: update.Quantity, ExpectedQuantity: update.ExpectedQuantity}
	// row 是当前规格行修改。
	for _, row := range update.SKUs {
		request.SKUs = append(request.SKUs, mtop.ItemStockSKUEdit{Key: row.Key, Quantity: row.Quantity, ExpectedQuantity: row.ExpectedQuantity})
	}
	return request
}

// 确保发布端口覆盖应用层定义的库存最小接口。
var _ itemapp.ItemStockPort = (*ItemPublishPort)(nil)
