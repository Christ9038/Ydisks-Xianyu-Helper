package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/adapter"
	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/auth"
)

// mountChat 负责mount聊天相关处理。
func (s *Server) mountChat(r chi.Router) {
	r.Get("/api/chat/sessions", s.listChatSessions)
	r.Get("/api/chat/messages", s.listChatMessages)
	r.Post("/api/chat/messages", s.sendChatMessage)
	r.Post("/api/chat/images", s.sendChatImage)
	r.Post("/api/chat/read", s.markChatRead)
	r.Get("/api/chat/ws", s.chatWebSocket)
}

// chatApplication 返回当前 Server 绑定的聊天历史应用服务。
func (s *Server) chatApplication() *chatapp.Service {
	return s.applicationServiceSet().chat
}

// listChatSessions 负责list聊天Sessions相关处理。
func (s *Server) listChatSessions(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// accountID 保存账号ID，供当前处理流程使用
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	if !s.ownsAccount(r, accountID) {
		writeErr(w, http.StatusForbidden, "无权访问该账号")
		return
	}
	// cursor 保存游标，供当前处理流程使用
	cursor, _ := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64)
	// refresh 保存refresh，供当前处理流程使用
	refresh := r.URL.Query().Get("refresh") == "1"
	// hasMore 保存hasMore，供当前处理流程使用
	var hasMore bool
	// nextCursor 保存next游标，供当前处理流程使用
	var nextCursor int64
	if // err 保存清理空会话的错误。
	err := s.chatApplication().CleanupEmptySessions(r.Context(), accountID); err != nil {
		writeErr(w, http.StatusInternalServerError, "清理无效聊天会话失败")
		return
	}
	if refresh && s.chat != nil && s.Manager != nil {
		if // sender、ok 保存sender、ok，供当前处理流程使用
		sender, ok := s.Manager.GetInstance(accountID); ok {
			if // fetcher、ok 保存fetcher、ok，供当前处理流程使用
			fetcher, ok := sender.(interface {
				FetchChatConversations(context.Context, int64, int) (map[string]any, string, error)
			}); ok {
				// fetchCtx、cancel 保存fetchCtx、cancel，供当前处理流程使用
				fetchCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
				// body、myID、fetchErr 保存body、myID、fetchErr，供当前处理流程使用
				body, myID, fetchErr := fetcher.FetchChatConversations(fetchCtx, cursor, 100)
				cancel()
				if fetchErr == nil {
					// page、saveErr 保存page、saveErr，供当前处理流程使用
					page, saveErr := s.chat.RecordConversationPage(r.Context(), accountID, myID, body)
					if saveErr != nil {
						writeErr(w, http.StatusInternalServerError, "保存历史联系人失败")
						return
					}
					hasMore, nextCursor = page.HasMore, page.NextCursor
				} else {
					s.recoverExpiredMTOPSession(r.Context(), accountID, fetchErr)
				}
			}
		}
	}
	// rows、err 保存应用层会话摘要及查询错误。
	rows, err := s.chatApplication().ListSessions(r.Context(), sess.UserID, accountID, parsePositiveInt(r.URL.Query().Get("limit"), 200))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取聊天会话失败")
		return
	}
	if refresh {
		// resolveCtx 和 resolveCancel 限制联系人身份补全的总时长。
		resolveCtx, resolveCancel := context.WithTimeout(r.Context(), 25*time.Second)
		// refreshedRows 和 sessionErr 保存应用层身份补全结果及首个平台错误。
		refreshedRows, sessionErr := s.chatApplication().RefreshSessionIdentities(resolveCtx, accountID, rows)
		resolveCancel()
		rows = refreshedRows
		if sessionErr != nil {
			s.recoverExpiredMTOPSession(r.Context(), accountID, sessionErr)
		}
	}
	writeJSON(w, http.StatusOK, chatSessionPageResponse{Sessions: newChatSessionDTOsFromApplication(rows), HasMore: hasMore, NextCursor: nextCursor})
}

// sendChatImage 负责send聊天图片相关处理。
func (s *Server) sendChatImage(w http.ResponseWriter, r *http.Request) {
	if s.chat == nil || s.Manager == nil {
		writeErr(w, http.StatusServiceUnavailable, "聊天服务未启用")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if // err 保存err，供当前处理流程使用
	err := r.ParseMultipartForm(10 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "图片不能为空且不能超过 10MB")
		return
	}
	// accountID 保存账号ID，供当前处理流程使用
	accountID := strings.TrimSpace(r.FormValue("account_id"))
	// chatID 保存聊天ID，供当前处理流程使用
	chatID := strings.TrimSpace(r.FormValue("chat_id"))
	// buyerID 保存买家ID，供当前处理流程使用
	buyerID := strings.TrimSpace(r.FormValue("buyer_id"))
	if !s.ownsAccount(r, accountID) {
		writeErr(w, http.StatusForbidden, "无权操作该账号")
		return
	}
	if chatID == "" || buyerID == "" {
		writeErr(w, http.StatusBadRequest, "会话和买家不能为空")
		return
	}
	// file、header、err 保存file、header、err，供当前处理流程使用
	file, header, err := r.FormFile("image")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "请选择图片")
		return
	}
	defer file.Close()
	// contentType 保存内容类型，供当前处理流程使用
	contentType := header.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		writeErr(w, http.StatusBadRequest, "只支持图片文件")
		return
	}
	// data、err 保存data、err，供当前处理流程使用
	data, err := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	if err != nil || len(data) == 0 || len(data) > 10<<20 {
		writeErr(w, http.StatusBadRequest, "图片不能为空且不能超过 10MB")
		return
	}
	// session 保存已完成账号归属校验的应用层会话摘要。
	session := chatapp.Session{AccountID: accountID, ChatID: chatID, BuyerID: buyerID,
		BuyerName: r.FormValue("buyer_name"), BuyerAvatar: r.FormValue("buyer_avatar_url"),
		ItemID: r.FormValue("item_id"), ItemTitle: r.FormValue("item_title")}
	// sent、err 保存sent、err，供当前处理流程使用
	sent, err := s.chatApplication().SendImage(r.Context(), chatapp.ImageInput{Session: session, Filename: header.Filename, ContentType: contentType, Data: data})
	if err != nil {
		if errors.Is(err, chatapp.ErrUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "图片上传服务未启用")
		} else if errors.Is(err, chatapp.ErrOffline) {
			writeErr(w, http.StatusConflict, "账号当前离线，无法发送图片")
		} else if errors.Is(err, chatapp.ErrSend) {
			writeErrDetails(w, http.StatusBadGateway, "chat_image_send_failed", "图片发送失败，请重试", "", map[string]any{"outgoing_message": sent})
		} else if errors.Is(err, chatapp.ErrStatusSave) {
			writeErr(w, http.StatusInternalServerError, "图片已发送，但状态保存失败")
		} else {
			writeErr(w, http.StatusInternalServerError, "保存待发送图片失败")
		}
		return
	}
	writeJSON(w, http.StatusCreated, chatMessageEnvelope{Message: newChatMessageDTOFromApplication(sent)})
}

// listChatMessages 负责list聊天消息列表相关处理。
func (s *Server) listChatMessages(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// accountID 保存账号ID，供当前处理流程使用
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	// chatID 保存聊天ID，供当前处理流程使用
	chatID := strings.TrimSpace(r.URL.Query().Get("chat_id"))
	if !s.ownsAccount(r, accountID) {
		writeErr(w, http.StatusForbidden, "无权访问该账号")
		return
	}
	if chatID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 chat_id")
		return
	}
	// beforeID 保存beforeID，供当前处理流程使用
	beforeID, _ := strconv.ParseInt(r.URL.Query().Get("before_id"), 10, 64)
	// cursor 保存游标，供当前处理流程使用
	cursor, _ := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64)
	// limit 保存上限，供当前处理流程使用
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	if s.chat != nil && s.Manager != nil {
		if // sender、ok 保存sender、ok，供当前处理流程使用
		sender, ok := s.Manager.GetInstance(accountID); ok {
			if // fetcher、ok 保存fetcher、ok，供当前处理流程使用
			fetcher, ok := sender.(interface {
				FetchChatHistory(context.Context, string, int64, int) (map[string]any, string, error)
			}); ok {
				// fetchCtx、cancel 保存fetchCtx、cancel，供当前处理流程使用
				fetchCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
				// body、myID、fetchErr 保存body、myID、fetchErr，供当前处理流程使用
				body, myID, fetchErr := fetcher.FetchChatHistory(fetchCtx, chatID, cursor, limit)
				cancel()
				if fetchErr == nil {
					// sessions 保存sessions，供当前处理流程使用
					current, _ := s.chatApplication().FindSession(r.Context(), sess.UserID, accountID, chatID)
					// page、saveErr 保存page、saveErr，供当前处理流程使用
					page, saveErr := s.chat.RecordHistoryPage(r.Context(), accountID, chatID, myID, adapter.ChatSessionFromApplication(current), body)
					if saveErr != nil {
						writeErr(w, http.StatusInternalServerError, "保存聊天历史失败")
						return
					}
					// current 和 identityErr 保存身份补全后的会话及平台查询错误。
					current, identityErr := s.chatApplication().ResolveSessionIdentity(r.Context(), current)
					if identityErr != nil {
						s.recoverExpiredMTOPSession(r.Context(), accountID, identityErr)
					}
					writeJSON(w, http.StatusOK, chatMessagePageResponse{Messages: newChatMessageDTOsFromApplication(adapter.ChatMessagesFromDB(page.Messages)), HasMore: page.HasMore, NextCursor: page.NextCursor, Session: newChatSessionDTOFromApplication(current)})
					return
				}
			}
		}
	}
	// page、err 保存page、err，供当前处理流程使用
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
			s.recoverExpiredMTOPSession(r.Context(), accountID, identityErr)
		}
		session = resolved
	}
	writeJSON(w, http.StatusOK, chatMessagePageResponse{Messages: newChatMessageDTOsFromApplication(page.Messages), HasMore: page.HasMore, Session: newChatSessionDTOFromApplication(session)})
}

// sendChatMessageRequest 保存send聊天消息请求，供当前处理流程使用
type sendChatMessageRequest struct {
	AccountID string `json:"account_id"`
	ChatID    string `json:"chat_id"`
	BuyerID   string `json:"buyer_id"`
	BuyerName string `json:"buyer_name"`
	ItemID    string `json:"item_id"`
	ItemTitle string `json:"item_title"`
	Text      string `json:"text"`
}

// sendChatMessage 负责send聊天消息相关处理。
func (s *Server) sendChatMessage(w http.ResponseWriter, r *http.Request) {
	if s.chat == nil || s.Manager == nil {
		writeErr(w, http.StatusServiceUnavailable, "聊天服务未启用")
		return
	}
	// input 保存input，供当前处理流程使用
	var input sendChatMessageRequest
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &input); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.AccountID, input.ChatID, input.BuyerID = strings.TrimSpace(input.AccountID), strings.TrimSpace(input.ChatID), strings.TrimSpace(input.BuyerID)
	input.Text = strings.TrimSpace(input.Text)
	if !s.ownsAccount(r, input.AccountID) {
		writeErr(w, http.StatusForbidden, "无权操作该账号")
		return
	}
	if input.ChatID == "" || input.BuyerID == "" || input.Text == "" {
		writeErr(w, http.StatusBadRequest, "会话、买家和消息内容不能为空")
		return
	}
	if len([]rune(input.Text)) > 2000 {
		writeErr(w, http.StatusBadRequest, "消息不能超过 2000 个字符")
		return
	}
	// sent、err 保存应用层发送结果及错误；应用层返回的消息不含凭证。
	sent, err := s.chatApplication().SendText(r.Context(), chatapp.OutgoingInput{Session: chatapp.Session{AccountID: input.AccountID, ChatID: input.ChatID, BuyerID: input.BuyerID, BuyerName: input.BuyerName, ItemID: input.ItemID, ItemTitle: input.ItemTitle}, Text: input.Text})
	if err != nil {
		if errors.Is(err, chatapp.ErrUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "聊天服务未启用")
		} else if errors.Is(err, chatapp.ErrOffline) {
			writeErr(w, http.StatusConflict, "账号当前离线，无法发送消息")
		} else if errors.Is(err, chatapp.ErrSend) {
			writeErrDetails(w, http.StatusBadGateway, "chat_message_send_failed", "发送失败，请重试", "", map[string]any{"outgoing_message": sent})
		} else if errors.Is(err, chatapp.ErrStatusSave) {
			writeErr(w, http.StatusInternalServerError, "消息已发送，但状态保存失败")
		} else {
			writeErr(w, http.StatusInternalServerError, "保存待发送消息失败")
		}
		return
	}
	writeJSON(w, http.StatusCreated, chatMessageEnvelope{Message: newChatMessageDTOFromApplication(sent)})
}

// markChatRead 负责mark聊天Read相关处理。
func (s *Server) markChatRead(w http.ResponseWriter, r *http.Request) {
	// input 保存input，供当前处理流程使用
	var input struct {
		AccountID string `json:"account_id"`
		ChatID    string `json:"chat_id"`
	}
	if decodeJSON(r, &input) != nil || input.ChatID == "" {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if !s.ownsAccount(r, input.AccountID) {
		writeErr(w, http.StatusForbidden, "无权操作该账号")
		return
	}
	// sess 保存当前认证用户，用于应用层归属隔离。
	sess := auth.SessionFromContext(r.Context())
	if // err 保存应用层已读状态更新错误。
	err := s.chatApplication().MarkRead(r.Context(), sess.UserID, input.AccountID, input.ChatID); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新已读状态失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// chatWebSocket 负责聊天WebSocket相关处理。
func (s *Server) chatWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.chat == nil {
		writeErr(w, http.StatusServiceUnavailable, "聊天服务未启用")
		return
	}
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// events、unsubscribe、err 保存events、unsubscribe、err，供当前处理流程使用
	events, unsubscribe, err := s.chat.Subscribe(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "订阅聊天消息失败")
		return
	}
	// conn、err 保存conn、err，供当前处理流程使用
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionContextTakeover})
	if err != nil {
		unsubscribe()
		return
	}
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithCancel(r.Context())
	conn.SetReadLimit(8 << 10)
	// readerWG 等待读取 goroutine 在连接关闭后退出，避免请求返回时遗留后台任务。
	var readerWG sync.WaitGroup
	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		for {
			if // readErr 保存readErr，供当前处理流程使用
			_, _, readErr := conn.Read(ctx); readErr != nil {
				cancel()
				return
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
	if // err 保存err，供当前处理流程使用
	err := wsjson.Write(ctx, conn, map[string]any{"type": "ready", "at": time.Now().UTC().UnixMilli()}); err != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case // event、ok 保存event、ok，供当前处理流程使用
		event, ok := <-events:
			if !ok || wsjson.Write(ctx, conn, event) != nil {
				return
			}
		}
	}
}

// ownsAccount 负责owns账号相关处理。
func (s *Server) ownsAccount(r *http.Request, accountID string) bool {
	if accountID == "" {
		return false
	}
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// owned 和 err 表示聊天应用端口返回的账号归属及查询错误。
	owned, err := s.chatApplication().OwnsAccount(r.Context(), sess.UserID, accountID)
	return err == nil && owned
}

/*
账号查询已采用所有权窄接口。
*/
// parsePositiveInt 将正整数文本转换为整数，无法解析时返回备用值。
// parsePositiveInt 负责parsePositiveInt相关处理。
func parsePositiveInt(raw string, fallback int) int {
	// value、err 保存value、err，供当前处理流程使用
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
