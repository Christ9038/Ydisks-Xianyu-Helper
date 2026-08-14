package server

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/xianyu/mtop"
)

func (s *Server) mountChat(r chi.Router) {
	r.Get("/api/chat/sessions", s.listChatSessions)
	r.Get("/api/chat/messages", s.listChatMessages)
	r.Post("/api/chat/messages", s.sendChatMessage)
	r.Post("/api/chat/images", s.sendChatImage)
	r.Post("/api/chat/read", s.markChatRead)
	r.Get("/api/chat/ws", s.chatWebSocket)
}

func (s *Server) listChatSessions(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	if !s.ownsAccount(r, accountID) {
		writeErr(w, http.StatusForbidden, "无权访问该账号")
		return
	}
	cursor, _ := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64)
	refresh := r.URL.Query().Get("refresh") == "1"
	var hasMore bool
	var nextCursor int64
	if err := s.Store.Chats.DeleteEmptySessions(r.Context(), accountID); err != nil {
		writeErr(w, http.StatusInternalServerError, "清理无效聊天会话失败")
		return
	}
	if refresh && s.chat != nil && s.Manager != nil {
		if sender, ok := s.Manager.GetInstance(accountID); ok {
			if fetcher, ok := sender.(interface {
				FetchChatConversations(context.Context, int64, int) (map[string]any, string, error)
			}); ok {
				fetchCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
				body, myID, fetchErr := fetcher.FetchChatConversations(fetchCtx, cursor, 100)
				cancel()
				if fetchErr == nil {
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
	rows, err := s.Store.Chats.ListSessions(r.Context(), sess.UserID, accountID, parsePositiveInt(r.URL.Query().Get("limit"), 200))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取聊天会话失败")
		return
	}
	if refresh {
		if cookieValue, cookieErr := s.Store.Cookies.GetValue(r.Context(), accountID); cookieErr == nil {
			client, canResolve := s.mtopClient().(interface {
				FetchChatUserInfo(context.Context, string, string) (*mtop.ChatUserInfo, error)
			})
			if !canResolve {
				writeJSON(w, http.StatusOK, map[string]any{"sessions": rows, "has_more": hasMore, "next_cursor": nextCursor})
				return
			}
			resolveCtx, resolveCancel := context.WithTimeout(r.Context(), 25*time.Second)
			defer resolveCancel()
			jobs := make(chan int)
			var workers sync.WaitGroup
			var sessionOnce sync.Once
			var sessionErr error
			for worker := 0; worker < 8; worker++ {
				workers.Add(1)
				go func() {
					defer workers.Done()
					for index := range jobs {
						infoCtx, cancel := context.WithTimeout(resolveCtx, 3*time.Second)
						info, infoErr := client.FetchChatUserInfo(infoCtx, cookieValue, rows[index].ChatID)
						cancel()
						if mtop.IsSessionExpiredErr(infoErr) {
							sessionOnce.Do(func() {
								sessionErr = infoErr
								resolveCancel()
							})
							continue
						}
						if infoErr != nil || info == nil {
							continue
						}
						if nickname := strings.TrimSpace(info.Nickname); nickname != "" {
							rows[index].BuyerName = nickname
						}
						if info.AvatarURL != "" {
							rows[index].BuyerAvatar = info.AvatarURL
						}
						_ = s.Store.Chats.UpdateSessionIdentity(resolveCtx, accountID, rows[index].ChatID,
							rows[index].BuyerID, rows[index].BuyerName, rows[index].BuyerAvatar)
					}
				}()
			}
		queue:
			for index := range rows {
				if rows[index].BuyerID == "1400" {
					continue
				}
				select {
				case jobs <- index:
				case <-resolveCtx.Done():
					break queue
				}
			}
			close(jobs)
			workers.Wait()
			if sessionErr != nil {
				s.recoverExpiredMTOPSession(r.Context(), accountID, sessionErr)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": rows, "has_more": hasMore, "next_cursor": nextCursor})
}

func (s *Server) sendChatImage(w http.ResponseWriter, r *http.Request) {
	if s.chat == nil || s.Manager == nil {
		writeErr(w, http.StatusServiceUnavailable, "聊天服务未启用")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "图片不能为空且不能超过 10MB")
		return
	}
	accountID := strings.TrimSpace(r.FormValue("account_id"))
	chatID := strings.TrimSpace(r.FormValue("chat_id"))
	buyerID := strings.TrimSpace(r.FormValue("buyer_id"))
	if !s.ownsAccount(r, accountID) {
		writeErr(w, http.StatusForbidden, "无权操作该账号")
		return
	}
	if chatID == "" || buyerID == "" {
		writeErr(w, http.StatusBadRequest, "会话和买家不能为空")
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "请选择图片")
		return
	}
	defer file.Close()
	contentType := header.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		writeErr(w, http.StatusBadRequest, "只支持图片文件")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	if err != nil || len(data) == 0 || len(data) > 10<<20 {
		writeErr(w, http.StatusBadRequest, "图片不能为空且不能超过 10MB")
		return
	}
	sender, ok := s.Manager.GetInstance(accountID)
	if !ok || sender == nil {
		writeErr(w, http.StatusConflict, "账号当前离线，无法发送图片")
		return
	}
	cookies, err := s.Store.Cookies.GetValue(r.Context(), accountID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取账号凭证失败")
		return
	}
	uploader, ok := s.mtopClient().(interface {
		UploadChatImage(context.Context, string, string, string, []byte) (*mtop.ChatImageUpload, error)
	})
	if !ok {
		writeErr(w, http.StatusServiceUnavailable, "图片上传服务未启用")
		return
	}
	upload, err := uploader.UploadChatImage(r.Context(), cookies, header.Filename, contentType, data)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "图片上传到闲鱼失败: "+err.Error())
		return
	}
	if upload.UpdatedCookies != "" && upload.UpdatedCookies != cookies {
		_ = s.Store.Cookies.UpdateValueExisting(r.Context(), accountID, upload.UpdatedCookies)
		sender.UpdateCookie(upload.UpdatedCookies)
	}
	session := db.ChatSession{CookieID: accountID, ChatID: chatID, BuyerID: buyerID,
		BuyerName: r.FormValue("buyer_name"), BuyerAvatar: r.FormValue("buyer_avatar_url"),
		ItemID: r.FormValue("item_id"), ItemTitle: r.FormValue("item_title")}
	message, err := s.chat.CreateOutgoingMedia(r.Context(), session, "image", upload.URL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存待发送图片失败")
		return
	}
	if err := sender.SendImage(engine.WithOutgoingMessageKey(r.Context(), message.MessageKey), chatID, buyerID, upload.URL, 0); err != nil {
		failed, _ := s.chat.SetOutgoingStatus(context.Background(), accountID, message.MessageKey, "failed")
		writeJSON(w, http.StatusBadGateway, map[string]any{"message": failed, "error": "图片发送失败，请重试"})
		return
	}
	sent, err := s.chat.SetOutgoingStatus(r.Context(), accountID, message.MessageKey, "sent")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "图片已发送，但状态保存失败")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"message": sent})
}

func (s *Server) listChatMessages(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	chatID := strings.TrimSpace(r.URL.Query().Get("chat_id"))
	if !s.ownsAccount(r, accountID) {
		writeErr(w, http.StatusForbidden, "无权访问该账号")
		return
	}
	if chatID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 chat_id")
		return
	}
	beforeID, _ := strconv.ParseInt(r.URL.Query().Get("before_id"), 10, 64)
	cursor, _ := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64)
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	if s.chat != nil && s.Manager != nil {
		if sender, ok := s.Manager.GetInstance(accountID); ok {
			if fetcher, ok := sender.(interface {
				FetchChatHistory(context.Context, string, int64, int) (map[string]any, string, error)
			}); ok {
				fetchCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
				body, myID, fetchErr := fetcher.FetchChatHistory(fetchCtx, chatID, cursor, limit)
				cancel()
				if fetchErr == nil {
					sessions, _ := s.Store.Chats.ListSessions(r.Context(), sess.UserID, accountID, 500)
					var current db.ChatSession
					for _, candidate := range sessions {
						if candidate.ChatID == chatID {
							current = candidate
							break
						}
					}
					page, saveErr := s.chat.RecordHistoryPage(r.Context(), accountID, chatID, myID, current, body)
					if saveErr != nil {
						writeErr(w, http.StatusInternalServerError, "保存聊天历史失败")
						return
					}
					current = s.resolveSelectedChatIdentity(r.Context(), accountID, current)
					writeJSON(w, http.StatusOK, map[string]any{"messages": page.Messages, "has_more": page.HasMore, "next_cursor": page.NextCursor, "session": current})
					return
				}
			}
		}
	}
	rows, err := s.Store.Chats.ListMessages(r.Context(), sess.UserID, accountID, chatID, beforeID,
		limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取聊天消息失败")
		return
	}
	var current db.ChatSession
	if sessions, sessionErr := s.Store.Chats.ListSessions(r.Context(), sess.UserID, accountID, 500); sessionErr == nil {
		for _, candidate := range sessions {
			if candidate.ChatID == chatID {
				current = s.resolveSelectedChatIdentity(r.Context(), accountID, candidate)
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": rows, "has_more": len(rows) == limit, "session": current})
}

func (s *Server) resolveSelectedChatIdentity(ctx context.Context, accountID string, session db.ChatSession) db.ChatSession {
	if session.BuyerID != "1400" {
		cookies, cookieErr := s.Store.Cookies.GetValue(ctx, accountID)
		client, supported := s.mtopClient().(interface {
			FetchChatUserInfo(context.Context, string, string) (*mtop.ChatUserInfo, error)
		})
		if cookieErr == nil && supported {
			resolveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			info, err := client.FetchChatUserInfo(resolveCtx, cookies, session.ChatID)
			cancel()
			if err == nil && info != nil {
				if nickname := strings.TrimSpace(info.Nickname); nickname != "" {
					session.BuyerName = nickname
				}
				if info.AvatarURL != "" {
					session.BuyerAvatar = info.AvatarURL
				}
			} else if err != nil {
				s.recoverExpiredMTOPSession(ctx, accountID, err)
			}
		}
	}
	_ = s.Store.Chats.UpdateSessionIdentity(ctx, accountID, session.ChatID, session.BuyerID, session.BuyerName, session.BuyerAvatar)
	return session
}

type sendChatMessageRequest struct {
	AccountID string `json:"account_id"`
	ChatID    string `json:"chat_id"`
	BuyerID   string `json:"buyer_id"`
	BuyerName string `json:"buyer_name"`
	ItemID    string `json:"item_id"`
	ItemTitle string `json:"item_title"`
	Text      string `json:"text"`
}

func (s *Server) sendChatMessage(w http.ResponseWriter, r *http.Request) {
	if s.chat == nil || s.Manager == nil {
		writeErr(w, http.StatusServiceUnavailable, "聊天服务未启用")
		return
	}
	var input sendChatMessageRequest
	if err := decodeJSON(r, &input); err != nil {
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
	sender, ok := s.Manager.GetInstance(input.AccountID)
	if !ok || sender == nil {
		writeErr(w, http.StatusConflict, "账号当前离线，无法发送消息")
		return
	}
	message, err := s.chat.CreateOutgoing(r.Context(), db.ChatSession{CookieID: input.AccountID, ChatID: input.ChatID,
		BuyerID: input.BuyerID, BuyerName: input.BuyerName, ItemID: input.ItemID, ItemTitle: input.ItemTitle}, input.Text)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存待发送消息失败")
		return
	}
	sendContext := engine.WithOutgoingMessageKey(r.Context(), message.MessageKey)
	if err := sender.SendText(sendContext, input.ChatID, input.BuyerID, input.Text); err != nil {
		failed, _ := s.chat.SetOutgoingStatus(context.Background(), input.AccountID, message.MessageKey, "failed")
		writeJSON(w, http.StatusBadGateway, map[string]any{"message": failed, "error": "发送失败，请重试"})
		return
	}
	sent, err := s.chat.SetOutgoingStatus(r.Context(), input.AccountID, message.MessageKey, "sent")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "消息已发送，但状态保存失败")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"message": sent})
}

func (s *Server) markChatRead(w http.ResponseWriter, r *http.Request) {
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
	sess := auth.SessionFromContext(r.Context())
	if err := s.Store.Chats.MarkRead(r.Context(), sess.UserID, input.AccountID, input.ChatID); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新已读状态失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) chatWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.chat == nil {
		writeErr(w, http.StatusServiceUnavailable, "聊天服务未启用")
		return
	}
	sess := auth.SessionFromContext(r.Context())
	events, unsubscribe, err := s.chat.Subscribe(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "订阅聊天消息失败")
		return
	}
	defer unsubscribe()
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionContextTakeover})
	if err != nil {
		return
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	conn.SetReadLimit(8 << 10)
	go func() {
		for {
			if _, _, readErr := conn.Read(ctx); readErr != nil {
				cancel()
				return
			}
		}
	}()
	if err := wsjson.Write(ctx, conn, map[string]any{"type": "ready", "at": time.Now().UTC().UnixMilli()}); err != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok || wsjson.Write(ctx, conn, event) != nil {
				return
			}
		}
	}
}

func (s *Server) ownsAccount(r *http.Request, accountID string) bool {
	if accountID == "" {
		return false
	}
	sess := auth.SessionFromContext(r.Context())
	owned, err := s.Store.Cookies.ExistsOwned(r.Context(), sess.UserID, accountID) // owned 和 err 表示账号归属及查询错误。
	return err == nil && owned
}

/*
账号查询已采用所有权窄接口。
*/
// parsePositiveInt 将正整数文本转换为整数，无法解析时返回备用值。
func parsePositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
