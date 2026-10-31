package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"

	accountapp "xianyu-go/internal/application/account"
	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/auth"
)

// markChatReadRequest 是提交聊天已读状态的 HTTP 请求 DTO。
type markChatReadRequest struct {
	// AccountID 是当前用户有权操作的账号标识。
	AccountID string `json:"account_id"`
	// ChatID 是会话标识。
	ChatID string `json:"chat_id"`
	// MessageIDs 是平台已读接口需要的消息标识集合。
	MessageIDs []map[string]any `json:"message_ids"`
}

// platformReadReportTimeout 限制平台已读回执的独立远端等待时间，浏览器断开不应取消该尽力上报。
const platformReadReportTimeout = 5 * time.Second

// mountChat 封装mount聊天业务协调。
func (s *Server) mountChat(r chi.Router) {
	r.Get("/api/chat/sessions", s.listChatSessions)
	r.Get("/api/chat/messages", s.listChatMessages)
	r.Post("/api/chat/messages", s.sendChatMessage)
	r.Post("/api/chat/images", s.sendChatImage)
	r.Post("/api/chat/read", s.markChatRead)
	r.Get("/api/chat/ws", s.chatWebSocket)
}

// chatApplication 返回当前 Server 绑定的聊天历史应用服务。
func (s *Server) chatApplication() ChatPort {
	return s.applicationServiceSet().chat
}

// listChatSessions 封装list聊天Sessions业务协调。
func (s *Server) listChatSessions(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证用户，用于本地会话归属过滤。
	sess := auth.SessionFromContext(r.Context())
	// accountID 保存请求目标的闲鱼账号标识。
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	if !s.ownsAccount(r, accountID) {
		writeErr(w, http.StatusForbidden, "无权访问该账号")
		return
	}
	// cursor 保存兼容旧客户端的平台联系人游标。
	cursor, _ := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64)
	// storedCursor 保存本地缓存键集分页位置，空值表示第一页。
	storedCursor, cursorErr := decodeChatSessionCursor(r.URL.Query().Get("stored_cursor"))
	if cursorErr != nil {
		writeErr(w, http.StatusBadRequest, cursorErr.Error())
		return
	}
	// refresh 表示本次是否同时拉取一页平台联系人并持久化。
	refresh := r.URL.Query().Get("refresh") == "1"
	// platformHasMore 表示平台游标是否仍有更多可拉取联系人页。
	var platformHasMore bool
	// nextCursor 保存兼容旧客户端的下一平台游标。
	var nextCursor int64
	if // err 保存清理空会话的错误。
	err := s.chatApplication().CleanupEmptySessions(r.Context(), accountID); err != nil {
		writeErr(w, http.StatusInternalServerError, "清理无效聊天会话失败")
		return
	}
	if refresh {
		// fetchCtx 和 cancel 限制平台联系人刷新请求的最长时间。
		fetchCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		// page 和 fetchErr 保存应用层联系人分页结果及平台/持久化错误。
		page, fetchErr := s.chatApplication().RefreshConversations(fetchCtx, accountID, cursor, 100)
		cancel()
		if fetchErr == nil {
			platformHasMore, nextCursor = page.HasMore, page.NextCursor
		} else if errors.Is(fetchErr, chatapp.ErrRefreshPersist) {
			writeErr(w, http.StatusInternalServerError, "保存历史联系人失败")
			return
		} else if errors.Is(fetchErr, chatapp.ErrRefreshUnavailable) || errors.Is(fetchErr, chatapp.ErrOffline) {
			platformHasMore = false
		} else {
			s.recoverExpiredSession(r.Context(), accountID, fetchErr)
			writeErr(w, http.StatusBadGateway, "刷新聊天联系人失败")
			return
		}
	}
	// page 和 pageErr 保存应用层本地会话分页结果及读取错误。
	page, pageErr := s.chatApplication().ListSessionPage(r.Context(), sess.UserID, accountID, storedCursor, parsePositiveInt(r.URL.Query().Get("limit"), 200))
	if pageErr != nil {
		writeErr(w, http.StatusInternalServerError, "读取聊天会话失败")
		return
	}
	// rows 保存当前本地页，身份补全后会替换为刷新后的展示信息。
	rows := page.Sessions
	if refresh {
		// resolveCtx 和 resolveCancel 限制联系人身份补全的总时长。
		resolveCtx, resolveCancel := context.WithTimeout(r.Context(), 25*time.Second)
		// refreshedRows 和 sessionErr 保存应用层身份补全结果及首个平台错误。
		refreshedRows, sessionErr := s.chatApplication().RefreshSessionIdentities(resolveCtx, accountID, rows)
		resolveCancel()
		rows = refreshedRows
		if sessionErr != nil {
			s.recoverExpiredSession(r.Context(), accountID, sessionErr)
		}
	}
	// nextStoredCursor 保存编码后的下一本地页游标，编码失败表示服务端内部实现异常。
	nextStoredCursor, encodeErr := encodeChatSessionCursor(page.NextCursor)
	if encodeErr != nil {
		writeErr(w, http.StatusInternalServerError, "生成本地会话游标失败")
		return
	}
	writeJSON(w, http.StatusOK, chatSessionPageResponse{
		Sessions: newChatSessionDTOsFromApplication(rows), HasMore: platformHasMore || page.HasMore,
		NextCursor: nextCursor, NextStoredCursor: nextStoredCursor,
		PlatformHasMore: platformHasMore, StoredHasMore: page.HasMore,
	})
}

// deleteChatSession 隐藏当前用户的本地聊天会话并物理清空展示消息，不调用闲鱼平台删除能力。
func (s *Server) deleteChatSession(w http.ResponseWriter, r *http.Request) {
	// session 保存认证中间件注入的当前管理用户身份。
	session := auth.SessionFromContext(r.Context())
	// accountID 和 chatID 是查询参数提供的账号及会话标识，应用层会再次规范化和校验。
	accountID, chatID := r.URL.Query().Get("account_id"), r.URL.Query().Get("chat_id")
	// deleteErr 保存应用层归属检查和原子清空操作的结果。
	deleteErr := s.chatApplication().DeleteConversation(r.Context(), session.UserID, accountID, chatID)
	switch {
	case deleteErr == nil:
		writeJSON(w, http.StatusOK, operationResponse{Success: true})
	case errors.Is(deleteErr, chatapp.ErrInvalidInput):
		writeErrCode(w, http.StatusBadRequest, "chat_session_invalid", "账号或会话标识无效", "")
	case errors.Is(deleteErr, chatapp.ErrSessionForbidden):
		writeErrCode(w, http.StatusForbidden, "chat_session_forbidden", "无权访问该账号", "")
	case errors.Is(deleteErr, chatapp.ErrChatSessionNotFound):
		writeErrCode(w, http.StatusNotFound, "chat_session_not_found", "聊天会话不存在", "")
	case errors.Is(deleteErr, chatapp.ErrSessionUnavailable):
		writeErrCode(w, http.StatusServiceUnavailable, "chat_session_service_unavailable", "聊天会话服务未启用", "")
	default:
		writeErrCode(w, http.StatusInternalServerError, "chat_session_delete_failed", "删除聊天会话失败", "")
	}
}

// sendChatImage 封装send聊天图片业务协调。
func (s *Server) sendChatImage(w http.ResponseWriter, r *http.Request) {
	if !s.chatApplication().ImageUploadAvailable() {
		writeErr(w, http.StatusServiceUnavailable, "聊天服务未启用")
		return
	}
	// 单张图片上限仍为 10 MiB，总请求额外保留 multipart 元数据空间，避免恰好 10 MiB 的合法图片被包装开销拒绝。
	if !parseMultipartRequest(w, r, maxChatImageMultipartBytes, maxChatImageMultipartBytes, "图片上传内容不能超过 11 MiB") {
		return
	}
	// accountID 用于本次流程后续判断的账号ID
	accountID := strings.TrimSpace(r.FormValue("account_id"))
	// chatID 用于本次流程后续判断的聊天ID
	chatID := strings.TrimSpace(r.FormValue("chat_id"))
	// peerUserID 是图片接收方的平台标识；买家侧会话中该值可以是卖家。
	peerUserID := strings.TrimSpace(r.FormValue("peer_user_id"))
	if !s.ownsAccount(r, accountID) {
		writeErr(w, http.StatusForbidden, "无权操作该账号")
		return
	}
	if chatID == "" || peerUserID == "" {
		writeErr(w, http.StatusBadRequest, "会话和对方用户不能为空")
		return
	}
	// file、header、err 用于本次流程后续判断的file、header、err
	file, header, err := r.FormFile("image")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "请选择图片")
		return
	}
	defer file.Close()
	// contentType 用于本次流程后续判断的内容类型
	contentType := header.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		writeErr(w, http.StatusBadRequest, "只支持图片文件")
		return
	}
	// data、err 用于本次流程后续判断的data、err
	data, err := io.ReadAll(io.LimitReader(file, maxChatImageBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxChatImageBytes {
		writeErr(w, http.StatusBadRequest, "图片不能为空且不能超过 10MB")
		return
	}
	// session 保存已完成账号归属校验的应用层会话摘要。
	session := chatapp.Session{AccountID: accountID, ChatID: chatID, PeerUserID: peerUserID,
		PeerName: r.FormValue("peer_name"), PeerAvatar: r.FormValue("peer_avatar_url"),
		ItemID: r.FormValue("item_id"), ItemTitle: r.FormValue("item_title")}
	// sent、err 用于本次流程后续判断的sent、err
	sent, err := s.chatApplication().SendImage(r.Context(), chatapp.ImageInput{Session: session, Filename: header.Filename, ContentType: contentType, Data: data})
	if err != nil {
		writeChatImageSendError(w, err, sent)
		return
	}
	writeJSON(w, http.StatusCreated, chatMessageEnvelope{Message: newChatMessageDTOFromApplication(sent)})
}

// chatLibraryImageRequest 是发送图片库图片的具名请求 DTO；字节由服务端按账号所有者从图片库读取，不经过浏览器。
type chatLibraryImageRequest struct {
	// AccountID 是当前用户拥有的发送账号标识。
	AccountID string `json:"account_id"`
	// ChatID 是目标聊天会话标识。
	ChatID string `json:"chat_id"`
	// PeerUserID 是图片接收方的平台用户标识。
	PeerUserID string `json:"peer_user_id"`
	// LibraryImageID 是待发送的图片库图片主键。
	LibraryImageID int64 `json:"library_image_id"`
	// PeerName 是会话对方展示名，用于本地会话摘要补全。
	PeerName string `json:"peer_name,omitempty"`
	// PeerAvatarURL 是会话对方头像地址，用于本地会话摘要补全。
	PeerAvatarURL string `json:"peer_avatar_url,omitempty"`
	// ItemID 是会话关联商品标识。
	ItemID string `json:"item_id,omitempty"`
	// ItemTitle 是会话关联商品标题。
	ItemTitle string `json:"item_title,omitempty"`
}

// sendChatLibraryImage 发送图片库图片：服务端读取字节后复用与手动上传相同的上传、真实宽高和状态收口链路。
func (s *Server) sendChatLibraryImage(w http.ResponseWriter, r *http.Request) {
	// request、decodeErr 保存请求体及其解析错误。
	var request chatLibraryImageRequest
	// decodeErr 保存请求体解码失败原因，非空时返回 400。
	if decodeErr := decodeJSON(r, &request); decodeErr != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// accountID、chatID、peerUserID 保存去除空白后的会话定位。
	accountID, chatID, peerUserID := strings.TrimSpace(request.AccountID), strings.TrimSpace(request.ChatID), strings.TrimSpace(request.PeerUserID)
	if !s.ownsAccount(r, accountID) {
		writeErr(w, http.StatusForbidden, "无权操作该账号")
		return
	}
	if chatID == "" || peerUserID == "" || request.LibraryImageID <= 0 {
		writeErr(w, http.StatusBadRequest, "会话、对方用户和图片不能为空")
		return
	}
	// session 保存已完成账号归属校验的应用层会话摘要。
	session := chatapp.Session{AccountID: accountID, ChatID: chatID, PeerUserID: peerUserID,
		PeerName: request.PeerName, PeerAvatar: request.PeerAvatarURL, ItemID: request.ItemID, ItemTitle: request.ItemTitle}
	// sent、err 保存发送结果及错误。
	sent, err := s.chatApplication().SendLibraryImage(r.Context(), chatapp.LibraryImageInput{Session: session, ImageID: request.LibraryImageID})
	if err != nil {
		writeChatImageSendError(w, err, sent)
		return
	}
	writeJSON(w, http.StatusCreated, chatMessageEnvelope{Message: newChatMessageDTOFromApplication(sent)})
}

// writeChatImageSendError 把图片发送用例错误映射为统一错误信封；手动上传与图片库发送共用，保证状态码与错误码一致。
func writeChatImageSendError(w http.ResponseWriter, err error, sent *chatapp.Message) {
	if errors.Is(err, chatapp.ErrUnavailable) {
		writeErr(w, http.StatusServiceUnavailable, "图片上传服务未启用")
	} else if errors.Is(err, chatapp.ErrOffline) {
		writeErr(w, http.StatusConflict, "账号当前离线，无法发送图片")
	} else if errors.Is(err, chatapp.ErrSendInvalidInput) {
		writeErr(w, http.StatusBadRequest, "图片或会话参数无效")
	} else if errors.Is(err, chatapp.ErrSendUncertain) {
		writeErrDetails(w, http.StatusBadGateway, "chat_image_send_uncertain", "图片发送结果待确认，请先到闲鱼核对，避免重复发送", "", map[string]any{"outgoing_message": newChatMessageDTOFromApplication(sent)})
	} else if errors.Is(err, chatapp.ErrSend) {
		writeErrDetails(w, http.StatusBadGateway, "chat_image_send_failed", "图片发送失败，请重试", "", map[string]any{"outgoing_message": newChatMessageDTOFromApplication(sent)})
	} else if errors.Is(err, chatapp.ErrStatusSave) {
		writeChatStatusSaveError(w, sent)
	} else {
		writeErr(w, http.StatusInternalServerError, "保存待发送图片失败")
	}
}

// listChatMessages 封装list聊天消息列表业务协调。
func (s *Server) listChatMessages(w http.ResponseWriter, r *http.Request) {
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// accountID 用于本次流程后续判断的账号ID
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	// chatID 用于本次流程后续判断的聊天ID
	chatID := strings.TrimSpace(r.URL.Query().Get("chat_id"))
	if !s.ownsAccount(r, accountID) {
		writeErr(w, http.StatusForbidden, "无权访问该账号")
		return
	}
	if chatID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 chat_id")
		return
	}
	// beforeID 用于本次流程后续判断的beforeID
	beforeID, _ := strconv.ParseInt(r.URL.Query().Get("before_id"), 10, 64)
	// cursor 用于本次流程后续判断的游标
	cursor, _ := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64)
	// limit 用于本次流程后续判断的上限
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	// current 保存刷新前的本地会话摘要，供平台历史写入和响应展示使用。
	current, _ := s.chatApplication().FindSession(r.Context(), sess.UserID, accountID, chatID)
	// fetchCtx 和 cancel 限制平台历史刷新请求的最长时间。
	fetchCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	// refreshed 和 fetchErr 保存应用层消息分页结果及平台/持久化错误。
	refreshed, fetchErr := s.chatApplication().RefreshHistory(fetchCtx, accountID, chatID, cursor, limit, current)
	cancel()
	if fetchErr == nil {
		// resolved 和 identityErr 保存身份补全后的会话及平台查询错误。
		resolved, identityErr := s.chatApplication().ResolveSessionIdentity(r.Context(), refreshed.Session)
		if identityErr != nil {
			s.recoverExpiredSession(r.Context(), accountID, identityErr)
		}
		writeJSON(w, http.StatusOK, chatMessagePageResponse{Messages: newChatMessageDTOsFromApplication(refreshed.Messages), HasMore: refreshed.HasMore, NextCursor: refreshed.NextCursor, Session: newChatSessionDTOFromApplication(resolved)})
		return
	}
	if errors.Is(fetchErr, chatapp.ErrRefreshPersist) {
		writeErr(w, http.StatusInternalServerError, "保存聊天历史失败")
		return
	}
	if !errors.Is(fetchErr, chatapp.ErrRefreshUnavailable) && !errors.Is(fetchErr, chatapp.ErrOffline) {
		s.recoverExpiredSession(r.Context(), accountID, fetchErr)
	}
	// page、err 用于本次流程后续判断的page、err
	page, err := s.chatApplication().ListStoredMessages(r.Context(), sess.UserID, accountID, chatID, beforeID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取聊天消息失败")
		return
	}
	// session 是应用层返回的非敏感会话摘要，供平台身份适配器补齐展示名称。
	session := page.Session
	if session.ChatID != "" {
		// resolved 和 identityErr 保存身份补全后的会话及平台查询错误。
		resolved, identityErr := s.chatApplication().ResolveSessionIdentity(r.Context(), session)
		if identityErr != nil {
			s.recoverExpiredSession(r.Context(), accountID, identityErr)
		}
		session = resolved
	}
	writeJSON(w, http.StatusOK, chatMessagePageResponse{Messages: newChatMessageDTOsFromApplication(page.Messages), HasMore: page.HasMore, Session: newChatSessionDTOFromApplication(session)})
}

// sendChatMessageRequest 用于本次流程后续判断的send聊天消息请求
type sendChatMessageRequest struct {
	// AccountID 是消息所属账号标识。
	AccountID string `json:"account_id"`
	// ChatID 是目标会话标识。
	ChatID string `json:"chat_id"`
	// PeerUserID 是消息接收方平台标识。
	PeerUserID string `json:"peer_user_id"`
	// PeerName 是消息接收方展示名称。
	PeerName string `json:"peer_name"`
	// ItemID 是会话关联商品标识。
	ItemID string `json:"item_id"`
	// ItemTitle 是会话关联商品标题。
	ItemTitle string `json:"item_title"`
	// Text 是待发送文本。
	Text string `json:"text"`
}

// sendChatMessage 封装send聊天消息业务协调。
func (s *Server) sendChatMessage(w http.ResponseWriter, r *http.Request) {
	if !s.chatApplication().SendingAvailable() {
		writeErr(w, http.StatusServiceUnavailable, "聊天服务未启用")
		return
	}
	// input 用于本次流程后续判断的input
	var input sendChatMessageRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &input); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.AccountID, input.ChatID, input.PeerUserID = strings.TrimSpace(input.AccountID), strings.TrimSpace(input.ChatID), strings.TrimSpace(input.PeerUserID)
	input.Text = strings.TrimSpace(input.Text)
	if !s.ownsAccount(r, input.AccountID) {
		writeErr(w, http.StatusForbidden, "无权操作该账号")
		return
	}
	if input.ChatID == "" || input.PeerUserID == "" || input.Text == "" {
		writeErr(w, http.StatusBadRequest, "会话、对方用户和消息内容不能为空")
		return
	}
	if len([]rune(input.Text)) > 2000 {
		writeErr(w, http.StatusBadRequest, "消息不能超过 2000 个字符")
		return
	}
	// sent、err 保存应用层发送结果及错误；应用层返回的消息不含凭证。
	sent, err := s.chatApplication().SendText(r.Context(), chatapp.OutgoingInput{Session: chatapp.Session{AccountID: input.AccountID, ChatID: input.ChatID, PeerUserID: input.PeerUserID, PeerName: input.PeerName, ItemID: input.ItemID, ItemTitle: input.ItemTitle}, Text: input.Text})
	if err != nil {
		if errors.Is(err, chatapp.ErrUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "聊天服务未启用")
		} else if errors.Is(err, chatapp.ErrOffline) {
			writeErr(w, http.StatusConflict, "账号当前离线，无法发送消息")
		} else if errors.Is(err, chatapp.ErrSendUncertain) {
			writeErrDetails(w, http.StatusBadGateway, "chat_send_uncertain", "发送结果待确认，请先到闲鱼核对，避免重复发送", "", map[string]any{"outgoing_message": newChatMessageDTOFromApplication(sent)})
		} else if errors.Is(err, chatapp.ErrSend) {
			writeErrDetails(w, http.StatusBadGateway, "chat_message_send_failed", "发送失败，请重试", "", map[string]any{"outgoing_message": newChatMessageDTOFromApplication(sent)})
		} else if errors.Is(err, chatapp.ErrStatusSave) {
			writeChatStatusSaveError(w, sent)
		} else {
			writeErr(w, http.StatusInternalServerError, "保存待发送消息失败")
		}
		return
	}
	writeJSON(w, http.StatusCreated, chatMessageEnvelope{Message: newChatMessageDTOFromApplication(sent)})
}

// writeChatStatusSaveError 返回平台已确认发送、但本地状态收口失败的稳定错误契约。
// message 的展示状态强制为 sent，前端不得把已远端投递的消息再次发送。
func writeChatStatusSaveError(w http.ResponseWriter, message *chatapp.Message) {
	// dto 保存向前端返回的 snake_case 外发消息，并固定远端已确认的状态。
	dto := newChatMessageDTOFromApplication(message)
	dto.Status = "sent"
	writeErrDetails(w, http.StatusInternalServerError, "chat_send_status_save_failed", "消息已发送，但本地状态同步失败，请刷新会话确认状态。", "", map[string]any{"outgoing_message": dto})
}

// markChatRead 封装mark聊天Read业务协调。
func (s *Server) markChatRead(w http.ResponseWriter, r *http.Request) {
	// input 是聊天已读请求的具名传输 DTO。
	var input markChatReadRequest
	if decodeJSON(r, &input) != nil || input.ChatID == "" {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if !s.ownsAccount(r, input.AccountID) {
		writeErr(w, http.StatusForbidden, "无权操作该账号")
		return
	}
	// sess 保存当前认证用户，用于本地未读状态归属隔离。
	sess := auth.SessionFromContext(r.Context())
	slog.Debug("收到聊天已读请求", "account", input.AccountID, "chat_id", input.ChatID, "message_count", len(input.MessageIDs))
	if len(input.MessageIDs) == 0 {
		// page 保存应用层返回的本地消息页，只含非敏感字段。
		page, listErr := s.chatApplication().ListStoredMessages(r.Context(), sess.UserID, input.AccountID, input.ChatID, 0, 200)
		if listErr == nil {
			// message 是当前用于补全平台已读消息标识的入站消息。
			for _, message := range page.Messages {
				if message.Direction == "incoming" && message.MessageType != "system" {
					input.MessageIDs = append(input.MessageIDs, map[string]any{"messageId": message.MessageKey})
				}
			}
		}
	}
	// 旧版本把实时 WS 通知里的 bizTag/extJson messageId 当成了平台消息
	// ID，数据库里会留下 32 位关联 ID。闲鱼的 read 接口实际要求 1.3 的
	// PNM ID；这里从已保存的解密 WS 诊断帧把旧 ID 转回 PNM，避免升级后
	// 仍有历史实时消息无法被标记已读。
	input.MessageIDs = s.resolveChatReadMessageIDs(r.Context(), input.AccountID, input.ChatID, input.MessageIDs)
	if // err 保存应用层已读状态更新错误。
	err := s.chatApplication().MarkRead(r.Context(), sess.UserID, input.AccountID, input.ChatID); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新已读状态失败")
		return
	}
	// reportCtx 和 reportCancel 为尽力上报提供独立的有界生命周期：本地已读提交成功后，浏览器中止请求不应取消平台回执。
	reportCtx, reportCancel := context.WithTimeout(context.Background(), platformReadReportTimeout)
	defer reportCancel()
	// reportErr 表示平台已读上报失败；本地已读状态已成功保存，不能回滚。
	if reportErr := s.chatApplication().ReportPlatformRead(reportCtx, input.AccountID, input.ChatID, input.MessageIDs); reportErr != nil {
		if errors.Is(reportErr, context.Canceled) {
			slog.Debug("上报闲鱼已读状态已取消", "account", input.AccountID, "chat_id", input.ChatID)
		} else {
			slog.Warn("上报闲鱼已读状态失败", "account", input.AccountID, "chat_id", input.ChatID, "err", reportErr)
		}
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// resolveChatReadMessageIDs 将旧版关联标识交给应用服务解析，并移除无效或重复的已读项。
func (s *Server) resolveChatReadMessageIDs(ctx context.Context, accountID, chatID string, messageIDs []map[string]any) []map[string]any {
	// resolved 保存可安全提交给平台的去重消息标识列表。
	resolved := make([]map[string]any, 0, len(messageIDs))
	// seen 保存已加入结果的平台 PNM 标识，避免重复上报。
	seen := make(map[string]struct{}, len(messageIDs))
	// item 是当前待转换的已读消息参数。
	for _, item := range messageIDs {
		// rawID 保存请求携带的原始消息标识。
		rawID, ok := item["messageId"].(string)
		if !ok || strings.TrimSpace(rawID) == "" {
			continue
		}
		// id 保存应用层解析后的平台消息标识。
		id := s.chatApplication().ResolveReadMessageID(ctx, accountID, chatID, rawID)
		if !strings.HasSuffix(id, ".PNM") {
			slog.Debug("未找到旧聊天消息对应的 PNM，跳过已读上报", "account", accountID, "chat_id", chatID, "message_id", rawID)
			continue
		}
		// exists 表示该平台消息标识是否已经加入本次上报请求。
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		// copyItem 保存保留其他平台参数、只替换消息标识的请求副本。
		copyItem := make(map[string]any, len(item)+1)
		// key、value 保存当前平台参数名及其原始值。
		for key, value := range item {
			copyItem[key] = value
		}
		copyItem["messageId"] = id
		resolved = append(resolved, copyItem)
	}
	return resolved
}

// findChatPlatformMessageID 保留既有包内测试入口，实际解析由聊天应用服务拥有。
func findChatPlatformMessageID(value any, chatID, legacyID string) string {
	return chatapp.FindPlatformMessageID(value, chatID, legacyID)
}

// chatWebSocket 将应用层聊天事件转发到当前认证用户的 WebSocket 连接。
func (s *Server) chatWebSocket(w http.ResponseWriter, r *http.Request) {
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// sessionID 是后续每次写帧前校验的本地管理会话标识，不包含闲鱼凭证。
	sessionCookie, cookieErr := r.Cookie(auth.CookieName)
	if cookieErr != nil || strings.TrimSpace(sessionCookie.Value) == "" {
		writeErr(w, http.StatusUnauthorized, "管理会话已失效")
		return
	}
	// sessionID 是后续每次写帧前校验的本地管理会话标识，不包含闲鱼凭证。
	sessionID := sessionCookie.Value
	// validateSession 在数据库边界内重新确认管理会话，最长等待三秒且不会触发平台调用。
	validateSession := func(parent context.Context) error {
		// checkCtx 和 checkCancel 将单次本地授权查询限制为三秒。
		checkCtx, checkCancel := context.WithTimeout(parent, 3*time.Second)
		defer checkCancel()
		return s.authenticationApplication().ValidateSession(checkCtx, sessionID, sess.UserID)
	}
	// validationErr 保存连接建立前的本地授权检查结果。
	if validationErr := validateSession(r.Context()); validationErr != nil {
		writeErr(w, http.StatusUnauthorized, "管理会话已失效")
		return
	}
	// events、unsubscribe、err 保存应用层实时事件、清理函数和订阅错误。
	events, unsubscribe, err := s.chatApplication().Subscribe(r.Context(), sess.UserID)
	if err != nil {
		if errors.Is(err, chatapp.ErrSubscriptionUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "聊天服务未启用")
		} else {
			writeErr(w, http.StatusInternalServerError, "订阅聊天消息失败")
		}
		return
	}
	// conn、err 用于本次流程后续判断的conn、err
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionContextTakeover})
	if err != nil {
		unsubscribe()
		return
	}
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(r.Context())
	conn.SetReadLimit(8 << 10)
	// readerWG 等待读取 goroutine 在连接关闭后退出，避免请求返回时遗留后台任务。
	var readerWG sync.WaitGroup
	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		for {
			// messageType、payload、readErr 分别是客户端帧类型、帧内容和读取错误。
			messageType, payload, readErr := conn.Read(ctx)
			if readErr != nil {
				cancel()
				return
			}
			// 客户端心跳帧由读取 goroutine 立即回复 pong，使空闲连接在两个方向上都有流量，避免被中间代理按空闲超时断开；
			// coder/websocket 的 Write 并发安全，因此可与下方事件循环同时写帧。
			if messageType == websocket.MessageText && isChatHeartbeatPing(payload) {
				if wsjson.Write(ctx, conn, map[string]any{"type": "pong", "at": time.Now().UTC().UnixMilli()}) != nil {
					cancel()
					return
				}
			}
		}
	}()
	// cleanup 统一负责取消请求、关闭 WebSocket、等待读取任务和释放聊天订阅。
	cleanup := func() {
		cancel()
		_ = conn.Close(websocket.StatusNormalClosure, "")
		readerWG.Wait()
		unsubscribe()
	}
	defer cleanup()
	// validationErr 保存 ready 写出前的授权检查结果。
	if validationErr := validateSession(ctx); validationErr != nil {
		if errors.Is(validationErr, accountapp.ErrSessionInvalid) {
			_ = conn.Close(websocket.StatusPolicyViolation, "session_invalid")
		} else {
			_ = conn.Close(websocket.StatusInternalError, "session_validation_failed")
		}
		return
	}
	if // err 用于本次流程后续判断的err
	err := wsjson.Write(ctx, conn, map[string]any{"type": "ready", "at": time.Now().UTC().UnixMilli()}); err != nil {
		return
	}
	// validationTicker 每五秒检查一次空闲连接的管理会话截止时间。
	validationTicker := time.NewTicker(5 * time.Second)
	defer validationTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-validationTicker.C:
			// validationErr 保存空闲连接定时授权检查结果。
			if validationErr := validateSession(ctx); validationErr != nil {
				if errors.Is(validationErr, accountapp.ErrSessionInvalid) {
					_ = conn.Close(websocket.StatusPolicyViolation, "session_invalid")
				} else {
					_ = conn.Close(websocket.StatusInternalError, "session_validation_failed")
				}
				return
			}
		case // event、ok 用于本次流程后续判断的event、ok
		event, ok := <-events:
			if !ok {
				return
			}
			// validationErr 保存业务帧写出前的授权检查结果。
			if validationErr := validateSession(ctx); validationErr != nil {
				if errors.Is(validationErr, accountapp.ErrSessionInvalid) {
					_ = conn.Close(websocket.StatusPolicyViolation, "session_invalid")
				} else {
					_ = conn.Close(websocket.StatusInternalError, "session_validation_failed")
				}
				return
			}
			// accountID 保存事件中待校验的非敏感账号标识。
			accountID := ""
			if event.Message != nil {
				accountID = event.Message.AccountID
			}
			if event.Session != nil {
				if accountID != "" && event.Session.AccountID != "" && accountID != event.Session.AccountID {
					continue
				}
				if accountID == "" {
					accountID = event.Session.AccountID
				}
			}
			if accountID != "" {
				// owned 和 ownershipErr 保存事件账号归属校验结果。
				owned, ownershipErr := s.chatApplication().OwnsAccount(ctx, sess.UserID, accountID)
				if ownershipErr != nil || !owned {
					continue
				}
			}
			if wsjson.Write(ctx, conn, newChatEventDTOFromApplication(event)) != nil {
				return
			}
		}
	}
}

// isChatHeartbeatPing 判断客户端文本帧是否为 {"type":"ping"} 心跳；非 JSON 或其他类型一律忽略。
// payload 是客户端发送的原始文本帧内容，长度已受 SetReadLimit 限制。
func isChatHeartbeatPing(payload []byte) bool {
	// frame 保存解析出的帧类型字段。
	var frame struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(payload, &frame) == nil && frame.Type == "ping"
}

// ownsAccount 封装owns账号业务协调。
func (s *Server) ownsAccount(r *http.Request, accountID string) bool {
	if accountID == "" {
		return false
	}
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// owned 和 err 表示聊天应用端口返回的账号归属及查询错误。
	owned, err := s.chatApplication().OwnsAccount(r.Context(), sess.UserID, accountID)
	return err == nil && owned
}

/*
账号查询已采用所有权窄接口。
*/
// parsePositiveInt 将正整数文本转换为整数，无法解析时返回备用值。
// parsePositiveInt 封装parsePositiveInt业务协调。
func parsePositiveInt(raw string, fallback int) int {
	// value、err 用于本次流程后续判断的value、err
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
