package server

import (
	"errors"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	cardsapp "xianyu-go/internal/application/cards"
	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/auth"
)

// cardSaleRecordResponse 是卡密售出记录的具名响应 DTO；content 为已发出的卡密明文，仅所属用户可读。
type cardSaleRecordResponse struct {
	// ID 是售出记录标识。
	ID int64 `json:"id"`
	// CardID 是售出时的卡密组标识。
	CardID int64 `json:"card_id"`
	// CardName 与 CardType 是售出时的卡密组名称和类型快照。
	CardName string `json:"card_name"`
	CardType string `json:"card_type"`
	// Content 是实际发给买家的卡密正文或 API 返回内容。
	Content string `json:"content"`
	// Status 为 sold（已确认售出）或 reserved（待核对）。
	Status string `json:"status"`
	// CookieID、OrderID、ItemID、BuyerID 是售出时的订单上下文。
	CookieID string `json:"cookie_id"`
	OrderID  string `json:"order_id"`
	ItemID   string `json:"item_id"`
	BuyerID  string `json:"buyer_id"`
	// CreatedAt 是扣减库存时刻，SoldAt 是确认售出时刻，均为 Unix 秒；未确认时 SoldAt 为 0。
	CreatedAt int64 `json:"created_at"`
	SoldAt    int64 `json:"sold_at"`
}

// cardSaleRecordListResponse 是卡密售出记录分页接口的具名响应 DTO。
type cardSaleRecordListResponse struct {
	// Success 表示查询成功。
	Success bool `json:"success"`
	// Data 是当前页记录，无记录时为空数组。
	Data []cardSaleRecordResponse `json:"data"`
	// Total 是符合条件的记录总数。
	Total int64 `json:"total"`
	// Page 与 PageSize 是规范化后的分页参数，TotalPages 是总页数。
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	TotalPages int `json:"total_pages"`
}

// orderDeliveredCardsResponse 是订单已发卡密接口的具名响应 DTO。
type orderDeliveredCardsResponse struct {
	// Success 表示查询成功。
	Success bool `json:"success"`
	// Data 是该订单已发出的卡密，按发出顺序排列；升级前的订单没有记录，返回空数组。
	Data []cardSaleRecordResponse `json:"data"`
}

// newCardSaleRecordResponses 把应用层售出记录转换为 HTTP DTO。
func newCardSaleRecordResponses(records []cardsapp.SaleRecord) []cardSaleRecordResponse {
	// result 保存转换后的 DTO，空输入编码为 [] 而不是 null。
	result := make([]cardSaleRecordResponse, 0, len(records))
	for /* record 是当前待转换的售出记录 */ _, record := range records {
		result = append(result, cardSaleRecordResponse{
			ID: record.ID, CardID: record.CardID, CardName: record.CardName, CardType: record.CardType, Content: record.Content,
			Status: record.Status, CookieID: record.CookieID, OrderID: record.OrderID, ItemID: record.ItemID,
			BuyerID: record.BuyerID, CreatedAt: record.CreatedAt, SoldAt: record.SoldAt,
		})
	}
	return result
}

// writeCardSaleError 把卡密售出相关应用错误映射为统一错误响应；卡密内容绝不出现在错误文本中。
func writeCardSaleError(w http.ResponseWriter, err error, fallback string) {
	// validationErr 用于识别可以直接返回客户端的稳定校验提示。
	var validationErr *cardsapp.ValidationError
	switch {
	case errors.Is(err, cardsapp.ErrNotFound):
		writeErr(w, http.StatusNotFound, "卡券不存在")
	case errors.Is(err, cardsapp.ErrForbidden):
		writeErr(w, http.StatusForbidden, "无权操作该卡密组")
	case errors.Is(err, cardsapp.ErrNotDataType), errors.Is(err, cardsapp.ErrInvalidCardID), errors.As(err, &validationErr):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, fallback)
	}
}

// listCardSaleRecords 分页返回卡密组的售出记录，支持按状态筛选和按订单号/卡密内容搜索。
func (s *Server) listCardSaleRecords(w http.ResponseWriter, r *http.Request) {
	// sess 保存认证中间件注入的当前用户会话。
	sess := auth.SessionFromContext(r.Context())
	// cardID、parseErr 保存路径中的卡券标识及数字解析错误。
	cardID, parseErr := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if parseErr != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	// query 保存 URL 查询参数；非法页码由应用层规范化。
	query := r.URL.Query()
	// page、pageSize 保存解析出的分页参数，解析失败按 0 交给应用层取默认值。
	page, _ := strconv.Atoi(query.Get("page"))
	// pageSize 保存每页数量，解析失败按 0 交给应用层取默认值。
	pageSize, _ := strconv.Atoi(query.Get("page_size"))
	// result、err 保存应用服务返回的一页售出记录及错误。
	result, err := s.cardsApplication().ListSaleRecords(r.Context(), sess.UserID, cardID, cardsapp.SaleListInput{
		Status: query.Get("status"), Search: query.Get("search"), Page: page, PageSize: pageSize,
	})
	if err != nil {
		writeCardSaleError(w, err, "读取售出记录失败")
		return
	}
	// totalPages 是按总数和页大小计算的总页数。
	totalPages := int((result.Total + int64(result.PageSize) - 1) / int64(result.PageSize))
	writeJSON(w, http.StatusOK, cardSaleRecordListResponse{
		Success: true, Data: newCardSaleRecordResponses(result.Records), Total: result.Total,
		Page: result.Page, PageSize: result.PageSize, TotalPages: totalPages,
	})
}

// exportCard 按 scope 导出卡密：unsold 为 data 卡密组当前库存 TXT，sold 为售出记录 CSV。
// 响应包含卡密明文，因此禁止缓存，仅所属用户可下载。
func (s *Server) exportCard(w http.ResponseWriter, r *http.Request) {
	// sess 保存认证中间件注入的当前用户会话。
	sess := auth.SessionFromContext(r.Context())
	// cardID、parseErr 保存路径中的卡券标识及数字解析错误。
	cardID, parseErr := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if parseErr != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	// export、err 保存应用服务生成的导出文件及错误。
	var export cardsapp.Export
	// err 保存导出生成错误，与 export 在下方 switch 的各分支中共同赋值。
	var err error
	switch r.URL.Query().Get("scope") {
	case cardsapp.ExportScopeUnsold:
		export, err = s.cardsApplication().ExportUnsold(r.Context(), sess.UserID, cardID)
	case cardsapp.ExportScopeSold:
		export, err = s.cardsApplication().ExportSold(r.Context(), sess.UserID, cardID)
	default:
		writeErr(w, http.StatusBadRequest, "scope 必须为 unsold 或 sold")
		return
	}
	if err != nil {
		writeCardSaleError(w, err, "导出失败")
		return
	}
	w.Header().Set("Content-Type", export.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": export.Filename}))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(export.Body)
}

// getOrderDeliveredCards 返回订单自动发货时发出的卡密；先复用订单详情的账号归属校验，再按订单读取台账。
func (s *Server) getOrderDeliveredCards(w http.ResponseWriter, r *http.Request) {
	// sess 保存认证中间件注入的当前用户会话。
	sess := auth.SessionFromContext(r.Context())
	// order、err 保存通过归属校验的订单及错误。
	order, err := s.orders().Get(r.Context(), sess.UserID, chi.URLParam(r, "order_id"))
	if err != nil {
		if errors.Is(err, orderapp.ErrForbidden) {
			writeErr(w, http.StatusForbidden, "无权操作此订单")
		} else {
			writeErr(w, http.StatusNotFound, "订单不存在")
		}
		return
	}
	// records、recordsErr 保存订单的已发卡密台账及读取错误。
	records, recordsErr := s.cardsApplication().SaleRecordsByOrder(r.Context(), sess.UserID, order.CookieID, order.OrderID)
	if recordsErr != nil {
		writeCardSaleError(w, recordsErr, "读取已发卡密失败")
		return
	}
	// 响应含卡密明文，禁止被浏览器或代理缓存。
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, orderDeliveredCardsResponse{Success: true, Data: newCardSaleRecordResponses(records)})
}
