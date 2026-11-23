package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	itemapp "xianyu-go/internal/application/items"
)

// ItemStockPort 定义当前用户读取和修改自己闲鱼商品库存的最小用例能力。
type ItemStockPort interface {
	// GetStock 读取商品当前库存，不修改平台数据。
	GetStock(ctx context.Context, userID int64, cookieID, itemID string) (itemapp.StockSnapshot, error)
	// UpdateStock 以最新平台数据为底修改库存并报告平台结果与本地会话写回状态。
	UpdateStock(ctx context.Context, userID int64, cookieID, itemID string, update itemapp.StockUpdate) (itemapp.StockUpdateOutcome, error)
}

// itemStockApplication 返回商品库存读取与修改用例。
func (s *Server) itemStockApplication() ItemStockPort {
	return s.applicationServiceSet().itemStock
}

// itemStockSKUResponse 是多规格商品一行规格组合的库存视图。
type itemStockSKUResponse struct {
	// Key 是规格行在同一商品内的稳定标识，修改时原样回传。
	Key string `json:"key"`
	// Label 是展示给用户的规格组合文案。
	Label string `json:"label"`
	// PriceCents 是该规格行售价（分），仅用于展示。
	PriceCents int64 `json:"price_cents"`
	// Quantity 是该规格行当前库存。
	Quantity int `json:"quantity"`
}

// itemStockData 是商品当前库存的非敏感结果。
type itemStockData struct {
	// ItemID 是闲鱼商品标识。
	ItemID string `json:"item_id"`
	// MultiSpec 表示库存由规格行提供。
	MultiSpec bool `json:"multi_spec"`
	// Quantity 是单规格商品库存；多规格商品为各规格库存之和。
	Quantity int `json:"quantity"`
	// SKUs 是多规格商品的规格行；单规格商品为空数组。
	SKUs []itemStockSKUResponse `json:"skus"`
}

// itemStockResponse 是读取商品库存的成功响应。
type itemStockResponse struct {
	// Success 表示读取成功。
	Success bool `json:"success"`
	// Data 是商品当前库存。
	Data itemStockData `json:"data"`
}

// itemStockSKUUpdateRequest 是多规格库存修改中的一行。
type itemStockSKUUpdateRequest struct {
	// Key 对应读取响应中的规格行标识。
	Key string `json:"key"`
	// Quantity 是新库存，范围 0 到 999999。
	Quantity int `json:"quantity"`
	// ExpectedQuantity 是用户看到的原库存，用于并发校验。
	ExpectedQuantity int `json:"expected_quantity"`
}

// itemStockUpdateRequest 是修改商品库存的请求：单规格填 quantity 与 expected_quantity，多规格填全部规格行 skus。
type itemStockUpdateRequest struct {
	// Quantity 是单规格商品的新库存，范围 1 到 999999。
	Quantity int `json:"quantity"`
	// ExpectedQuantity 是单规格商品用户看到的原库存，用于并发校验。
	ExpectedQuantity int `json:"expected_quantity"`
	// SKUs 是多规格商品的全部规格行修改；非空表示按多规格处理。
	SKUs []itemStockSKUUpdateRequest `json:"skus"`
}

// itemStockUpdateData 是库存修改成功后的非敏感结果。
type itemStockUpdateData struct {
	// ItemID 是被修改的商品标识。
	ItemID string `json:"item_id"`
	// Message 是平台返回的安全摘要。
	Message string `json:"message"`
	// CookiePersisted 表示响应中的账号会话变化已可靠写回本地。
	CookiePersisted bool `json:"cookie_persisted"`
}

// itemStockUpdateResponse 是修改商品库存的成功响应。
type itemStockUpdateResponse struct {
	// Success 表示平台已修改库存且账号会话写回完成。
	Success bool `json:"success"`
	// Data 是本次修改的非敏感结果。
	Data itemStockUpdateData `json:"data"`
}

// itemStockIdentity 解析并校验库存接口的路径参数、账号归属和登录身份；失败时已写入错误响应。
func (s *Server) itemStockIdentity(w http.ResponseWriter, r *http.Request) (cookieID, itemID string, userID int64, ok bool) {
	// cookieID、itemID 是路由中指定的账号和商品标识。
	cookieID = strings.TrimSpace(chi.URLParam(r, "cookie_id"))
	itemID = strings.TrimSpace(chi.URLParam(r, "item_id"))
	if cookieID == "" || itemID == "" {
		writeErr(w, http.StatusBadRequest, "账号 ID 和商品 ID 不能为空")
		return "", "", 0, false
	}
	// 只读取账号归属元数据，不提前解密 Cookie；平台端口稍后在凭证锁内读取一次快照。
	if !s.requireCookieOwnership(w, r, cookieID) {
		return "", "", 0, false
	}
	// session 是已认证用户的非敏感身份。
	session := authSess(r)
	if session == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return "", "", 0, false
	}
	return cookieID, itemID, session.UserID, true
}

// getItemStock 读取当前用户拥有的商品库存（编辑详情，一次平台请求）。
func (s *Server) getItemStock(w http.ResponseWriter, r *http.Request) {
	// cookieID、itemID、userID 是通过校验的账号、商品和用户标识。
	cookieID, itemID, userID, ok := s.itemStockIdentity(w, r)
	if !ok {
		return
	}
	// snapshot、err 保存读取到的库存与错误。
	snapshot, err := s.itemStockApplication().GetStock(r.Context(), userID, cookieID, itemID)
	if err != nil {
		writeItemStockError(w, r, err)
		return
	}
	// skus 是转换后的规格行，保证单规格时序列化为空数组而不是 null。
	skus := make([]itemStockSKUResponse, 0, len(snapshot.SKUs))
	// sku 是当前规格行。
	for _, sku := range snapshot.SKUs {
		skus = append(skus, itemStockSKUResponse{Key: sku.Key, Label: sku.Label, PriceCents: sku.PriceCents, Quantity: sku.Quantity})
	}
	setNoStore(w)
	writeJSON(w, http.StatusOK, itemStockResponse{Success: true, Data: itemStockData{ItemID: snapshot.ItemID, MultiSpec: snapshot.MultiSpec, Quantity: snapshot.Quantity, SKUs: skus}})
}

// updateItemStock 修改当前用户拥有的商品库存（先读后写，两次平台请求）。
func (s *Server) updateItemStock(w http.ResponseWriter, r *http.Request) {
	// cookieID、itemID、userID 是通过校验的账号、商品和用户标识。
	cookieID, itemID, userID, ok := s.itemStockIdentity(w, r)
	if !ok {
		return
	}
	// body 保存解码后的库存修改请求。
	var body itemStockUpdateRequest
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体不是有效的库存修改 JSON")
		return
	}
	// update 是转换后的应用层库存修改。
	update := itemapp.StockUpdate{Quantity: body.Quantity, ExpectedQuantity: body.ExpectedQuantity}
	// row 是当前请求中的规格行。
	for _, row := range body.SKUs {
		update.SKUs = append(update.SKUs, itemapp.StockSKUChange{Key: row.Key, Quantity: row.Quantity, ExpectedQuantity: row.ExpectedQuantity})
	}
	// outcome、err 保存修改结果和错误。
	outcome, err := s.itemStockApplication().UpdateStock(r.Context(), userID, cookieID, itemID, update)
	if err != nil {
		if errors.Is(err, itemapp.ErrStockSessionPersistence) {
			logInternalFailure(r.Context(), http.StatusBadGateway, err)
			setNoStore(w)
			writeErrCode(w, http.StatusBadGateway, "platform_stock_updated_cookie_persist_failed",
				"闲鱼平台已修改库存，但本地账号会话写回失败。请先刷新账号和商品库存，不要直接重复提交。", "")
			return
		}
		writeItemStockError(w, r, err)
		return
	}
	setNoStore(w)
	writeJSON(w, http.StatusOK, itemStockUpdateResponse{Success: true, Data: itemStockUpdateData{ItemID: outcome.ItemID, Message: outcome.Message, CookiePersisted: outcome.CookiePersisted}})
}

// writeItemStockError 把库存用例错误映射为统一错误响应；平台失败不回显响应正文或凭证。
func writeItemStockError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, itemapp.ErrStockInvalidInput):
		writeErr(w, http.StatusBadRequest, "库存参数无效：单规格库存需在 1 到 999999 之间，多规格需提交全部规格且至少一个库存大于 0")
	case errors.Is(err, itemapp.ErrStockCredentialChanged):
		writeErrCode(w, http.StatusConflict, "account_credential_changed", "账号凭证已变化，请刷新账号后重试", "")
	case errors.Is(err, itemapp.ErrStockChanged):
		writeErrCode(w, http.StatusConflict, "item_stock_changed", "商品库存已在闲鱼侧变化，请刷新后重新修改", "")
	case errors.Is(err, itemapp.ErrStockEditUnsupported):
		writeErrCode(w, http.StatusUnprocessableEntity, "item_edit_unsupported", "该商品类目暂不支持网页端编辑，请在闲鱼 App 中修改库存", "")
	case errors.Is(err, itemapp.ErrStockUnsupported):
		writeErr(w, http.StatusNotImplemented, "当前平台客户端不支持商品库存修改")
	case errors.Is(err, itemapp.ErrStockRiskVerification):
		logInternalFailure(r.Context(), http.StatusBadGateway, err)
		writeErrCode(w, http.StatusBadGateway, "platform_risk_verification", "闲鱼要求安全验证，请先在闲鱼 App 完成验证后再重试", "")
	case errors.Is(err, itemapp.ErrStockSessionExpired):
		logInternalFailure(r.Context(), http.StatusBadGateway, err)
		writeErrCode(w, http.StatusBadGateway, "platform_session_expired", "账号登录已失效，请重新登录该账号后再试", "")
	default:
		writeInternalErr(w, r, http.StatusBadGateway, "闲鱼平台请求失败，请稍后重试；如刚提交过修改，请先刷新确认库存", err)
	}
}
