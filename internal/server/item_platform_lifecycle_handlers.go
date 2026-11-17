package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	itemapp "xianyu-go/internal/application/items"
)

// itemPlatformLifecycleData 是商品平台下架操作的非敏感结果字段。
type itemPlatformLifecycleData struct {
	// ItemID 是本次操作的闲鱼商品标识。
	ItemID string `json:"item_id"`
	// Status 是平台确认后的商品状态。
	Status string `json:"status"`
	// Message 是平台返回的安全摘要。
	Message string `json:"message"`
	// CookiePersisted 表示响应中的账号会话变化已可靠写回本地。
	CookiePersisted bool `json:"cookie_persisted"`
}

// itemPlatformLifecycleResponse 是商品平台下架成功响应。
type itemPlatformLifecycleResponse struct {
	// Success 表示平台已确认下架且本地会话写回完成。
	Success bool `json:"success"`
	// Data 是本次下架的非敏感结果。
	Data itemPlatformLifecycleData `json:"data"`
}

// unpublishItem 在闲鱼平台下架当前用户拥有的商品，不删除平台商品或本地商品记录。
func (s *Server) unpublishItem(w http.ResponseWriter, r *http.Request) {
	// cookieID、itemID 是路由中指定的账号和商品标识。
	cookieID := strings.TrimSpace(chi.URLParam(r, "cookie_id"))
	// itemID 是本次请求要在闲鱼平台下架的商品标识。
	itemID := strings.TrimSpace(chi.URLParam(r, "item_id"))
	if cookieID == "" || itemID == "" {
		writeErr(w, http.StatusBadRequest, "账号 ID 和商品 ID 不能为空")
		return
	}
	// ok 只读取账号归属元数据，不提前解密 Cookie；平台端口稍后在凭证锁内读取一次快照。
	if !s.requireCookieOwnership(w, r, cookieID) {
		return
	}
	// session 是已认证用户的非敏感身份；应用端口会再次按该用户校验账号凭证归属。
	session := authSess(r)
	if session == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// userID 是当前登录用户或外部令牌所属用户的主键。
	userID := session.UserID
	// outcome、err 保存平台下架结果和本地会话写回状态。
	outcome, err := s.itemPlatformLifecycleApplication().DownShelf(r.Context(), userID, cookieID, itemID)
	if err != nil {
		if outcome.PlatformSuccess {
			setNoStore(w)
			writeErrDetails(w, http.StatusBadGateway, "platform_unpublished_cookie_persist_failed",
				"闲鱼平台已确认下架，但本地账号会话写回失败。请先刷新账号和商品状态，不要直接重复下架。", "",
				map[string]any{"item_id": itemID, "status": "off_sale", "platform_success": true, "cookie_persisted": false})
			return
		}
		switch {
		case errors.Is(err, itemapp.ErrDownShelfInvalidInput):
			writeErr(w, http.StatusBadRequest, "商品下架参数无效")
		case errors.Is(err, itemapp.ErrDownShelfCredentialChanged):
			writeErr(w, http.StatusConflict, "账号凭证已变化，请刷新账号后重试")
		case errors.Is(err, itemapp.ErrDownShelfUnsupported):
			writeErr(w, http.StatusNotImplemented, "当前平台客户端不支持商品下架")
		default:
			writeErr(w, http.StatusBadGateway, "闲鱼平台未明确确认下架；请先刷新商品状态，确认仍在售后再重试")
		}
		return
	}
	if !outcome.PlatformSuccess || outcome.Status != "off_sale" || !outcome.CookiePersisted {
		writeErr(w, http.StatusBadGateway, "闲鱼平台未确认下架或账号会话未完成写回")
		return
	}
	setNoStore(w)
	writeJSON(w, http.StatusOK, itemPlatformLifecycleResponse{
		Success: true,
		Data:    itemPlatformLifecycleData{ItemID: outcome.ItemID, Status: outcome.Status, Message: outcome.Message, CookiePersisted: outcome.CookiePersisted},
	})
}
