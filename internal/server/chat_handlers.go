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

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
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

// storeChatApplicationRepository 将数据库聊天查询适配为应用层聊天端口。
type storeChatApplicationRepository struct {
	// store 保存数据库聚合入口，仅在适配器内执行窄聊天查询。
	store *db.Store
}

// ListMessages 查询带用户归属条件的聊天消息，并转换为应用层模型。
func (r storeChatApplicationRepository) ListMessages(ctx context.Context, userID int64, accountID, chatID string, beforeID int64, limit int) ([]chatapp.Message, error) {
	// rows 保存数据库返回的消息记录。
	rows, err := r.store.Chats.ListMessages(ctx, userID, accountID, chatID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	// messages 保存脱离数据库模型的应用层消息。
	messages := make([]chatapp.Message, 0, len(rows))
	// row 表示当前待转换的数据库聊天消息。
	for _, row := range rows {
		messages = append(messages, chatapp.Message{
			ID: row.ID, AccountID: row.CookieID, ChatID: row.ChatID, MessageKey: row.MessageKey,
			Direction: row.Direction, SenderID: row.SenderID, SenderName: row.SenderName,
			MessageType: row.MessageType, Content: row.Content, Status: row.Status, SentAt: row.SentAt,
		})
	}
	return messages, nil
}

// ListSessions 查询带用户归属条件的聊天会话，并转换为应用层模型。
func (r storeChatApplicationRepository) ListSessions(ctx context.Context, userID int64, accountID string, limit int) ([]chatapp.Session, error) {
	// rows 保存数据库返回的会话记录。
	rows, err := r.store.Chats.ListSessions(ctx, userID, accountID, limit)
	if err != nil {
		return nil, err
	}
	// sessions 保存脱离数据库模型的应用层会话摘要。
	sessions := make([]chatapp.Session, 0, len(rows))
	// row 表示当前待转换的数据库聊天会话。
	for _, row := range rows {
		sessions = append(sessions, chatapp.Session{
			AccountID: row.CookieID, ChatID: row.ChatID, BuyerID: row.BuyerID,
			BuyerName: row.BuyerName, BuyerAvatar: row.BuyerAvatar, ItemID: row.ItemID,
			ItemTitle: row.ItemTitle, LastMessage: row.LastMessage, LastMessageAt: row.LastMessageAt,
			UnreadCount: row.UnreadCount,
		})
	}
	return sessions, nil
}

// newStoreChatApplicationRepository 创建聊天历史应用服务使用的数据库适配器。
func newStoreChatApplicationRepository(store *db.Store) chatapp.Repository {
	if store == nil || store.Chats == nil {
		return nil
	}
	return storeChatApplicationRepository{store: store}
}

// dbChatSessionFromApplication 将非敏感应用会话转换为平台身份适配器可消费的数据库形状。
func dbChatSessionFromApplication(session chatapp.Session) db.ChatSession {
	return db.ChatSession{
		CookieID: session.AccountID, ChatID: session.ChatID, BuyerID: session.BuyerID,
		BuyerName: session.BuyerName, BuyerAvatar: session.BuyerAvatar, ItemID: session.ItemID,
		ItemTitle: session.ItemTitle, LastMessage: session.LastMessage, LastMessageAt: session.LastMessageAt,
		UnreadCount: session.UnreadCount,
	}
}

// 确保数据库适配器覆盖聊天历史应用端口的全部能力。
var _ chatapp.Repository = storeChatApplicationRepository{}

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
	if // err 保存err，供当前处理流程使用
	err := s.Store.Chats.DeleteEmptySessions(r.Context(), accountID); err != nil {
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
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := s.Store.Chats.ListSessions(r.Context(), sess.UserID, accountID, parsePositiveInt(r.URL.Query().Get("limit"), 200))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取聊天会话失败")
		return
	}
	if refresh {
		if // cookieValue、cookieErr 保存登录凭证Value、cookieErr，供当前处理流程使用
		cookieValue, cookieErr := s.Store.Cookies.GetValue(r.Context(), accountID); cookieErr == nil {
			// client、canResolve 保存client、canResolve，供当前处理流程使用
			client, canResolve := s.mtopClient().(interface {
				FetchChatUserInfo(context.Context, string, string) (*mtop.ChatUserInfo, error)
			})
			if !canResolve {
				writeJSON(w, http.StatusOK, chatSessionPageResponse{Sessions: newChatSessionDTOs(rows), HasMore: hasMore, NextCursor: nextCursor})
				return
			}
			// resolveCtx、resolveCancel 保存resolveCtx、resolve取消，供当前处理流程使用
			resolveCtx, resolveCancel := context.WithTimeout(r.Context(), 25*time.Second)
			defer resolveCancel()
			// jobs 保存jobs，供当前处理流程使用
			jobs := make(chan int)
			// workers 保存workers，供当前处理流程使用
			var workers sync.WaitGroup
			// sessionOnce 保存会话Once，供当前处理流程使用
			var sessionOnce sync.Once
			// sessionErr 保存会话Err，供当前处理流程使用
			var sessionErr error
			for // worker 保存工作器，供当前处理流程使用
			worker := 0; worker < 8; worker++ {
				workers.Add(1)
				go func() {
					defer workers.Done()
					// index 表示当前遍历过程中的index
					for index := range jobs {
						// infoCtx、cancel 保存infoCtx、cancel，供当前处理流程使用
						infoCtx, cancel := context.WithTimeout(resolveCtx, 3*time.Second)
						// info、infoErr 保存info、infoErr，供当前处理流程使用
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
						if // nickname 保存nickname，供当前处理流程使用
						nickname := strings.TrimSpace(info.Nickname); nickname != "" {
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
			// index 表示当前遍历过程中的index
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
	writeJSON(w, http.StatusOK, chatSessionPageResponse{Sessions: newChatSessionDTOs(rows), HasMore: hasMore, NextCursor: nextCursor})
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
	// session 保存会话，供当前处理流程使用
	session := db.ChatSession{CookieID: accountID, ChatID: chatID, BuyerID: buyerID,
		BuyerName: r.FormValue("buyer_name"), BuyerAvatar: r.FormValue("buyer_avatar_url"),
		ItemID: r.FormValue("item_id"), ItemTitle: r.FormValue("item_title")}
	// sent、err 保存sent、err，供当前处理流程使用
	sent, err := s.communicationApplication().SendChatImage(r.Context(), chatImageInput{Session: session, Filename: header.Filename, ContentType: contentType, Data: data})
	if err != nil {
		if errors.Is(err, errCommunicationUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "图片上传服务未启用")
		} else if errors.Is(err, errChatOffline) {
			writeErr(w, http.StatusConflict, "账号当前离线，无法发送图片")
		} else if errors.Is(err, errChatSend) {
			writeErrDetails(w, http.StatusBadGateway, "chat_image_send_failed", "图片发送失败，请重试", "", map[string]any{"outgoing_message": sent})
		} else if errors.Is(err, errChatStatusSave) {
			writeErr(w, http.StatusInternalServerError, "图片已发送，但状态保存失败")
		} else {
			writeErr(w, http.StatusInternalServerError, "保存待发送图片失败")
		}
		return
	}
	writeJSON(w, http.StatusCreated, chatMessageEnvelope{Message: newChatMessageDTOFromPointer(sent)})
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
					sessions, _ := s.Store.Chats.ListSessions(r.Context(), sess.UserID, accountID, 500)
					// current 保存current，供当前处理流程使用
					var current db.ChatSession
					// candidate 表示当前遍历过程中的candidate
					for _, candidate := range sessions {
						if candidate.ChatID == chatID {
							current = candidate
							break
						}
					}
					// page、saveErr 保存page、saveErr，供当前处理流程使用
					page, saveErr := s.chat.RecordHistoryPage(r.Context(), accountID, chatID, myID, current, body)
					if saveErr != nil {
						writeErr(w, http.StatusInternalServerError, "保存聊天历史失败")
						return
					}
					current = s.resolveSelectedChatIdentity(r.Context(), accountID, current)
					writeJSON(w, http.StatusOK, chatMessagePageResponse{Messages: newChatMessageDTOs(page.Messages), HasMore: page.HasMore, NextCursor: page.NextCursor, Session: newChatSessionDTO(current)})
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
	session := dbChatSessionFromApplication(page.Session)
	if session.ChatID != "" {
		session = s.resolveSelectedChatIdentity(r.Context(), accountID, session)
	}
	writeJSON(w, http.StatusOK, chatMessagePageResponse{Messages: newChatMessageDTOsFromApplication(page.Messages), HasMore: page.HasMore, Session: newChatSessionDTO(session)})
}

// resolveSelectedChatIdentity 负责resolveSelected聊天Identity相关处理。
func (s *Server) resolveSelectedChatIdentity(ctx context.Context, accountID string, session db.ChatSession) db.ChatSession {
	if session.BuyerID != "1400" {
		// cookies、cookieErr 保存cookies、cookieErr，供当前处理流程使用
		cookies, cookieErr := s.Store.Cookies.GetValue(ctx, accountID)
		// client、supported 保存client、supported，供当前处理流程使用
		client, supported := s.mtopClient().(interface {
			FetchChatUserInfo(context.Context, string, string) (*mtop.ChatUserInfo, error)
		})
		if cookieErr == nil && supported {
			// resolveCtx、cancel 保存resolveCtx、cancel，供当前处理流程使用
			resolveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			// info、err 保存info、err，供当前处理流程使用
			info, err := client.FetchChatUserInfo(resolveCtx, cookies, session.ChatID)
			cancel()
			if err == nil && info != nil {
				if // nickname 保存nickname，供当前处理流程使用
				nickname := strings.TrimSpace(info.Nickname); nickname != "" {
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
	// sent、err 保存sent、err，供当前处理流程使用
	sent, err := s.communicationApplication().SendChatText(r.Context(), chatTextInput{Session: db.ChatSession{CookieID: input.AccountID, ChatID: input.ChatID, BuyerID: input.BuyerID, BuyerName: input.BuyerName, ItemID: input.ItemID, ItemTitle: input.ItemTitle}, Text: input.Text})
	if err != nil {
		if errors.Is(err, errCommunicationUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "聊天服务未启用")
		} else if errors.Is(err, errChatOffline) {
			writeErr(w, http.StatusConflict, "账号当前离线，无法发送消息")
		} else if errors.Is(err, errChatSend) {
			writeErrDetails(w, http.StatusBadGateway, "chat_message_send_failed", "发送失败，请重试", "", map[string]any{"outgoing_message": sent})
		} else if errors.Is(err, errChatStatusSave) {
			writeErr(w, http.StatusInternalServerError, "消息已发送，但状态保存失败")
		} else {
			writeErr(w, http.StatusInternalServerError, "保存待发送消息失败")
		}
		return
	}
	writeJSON(w, http.StatusCreated, chatMessageEnvelope{Message: newChatMessageDTOFromPointer(sent)})
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
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	if // err 保存err，供当前处理流程使用
	err := s.communicationApplication().MarkChatRead(r.Context(), sess.UserID, input.AccountID, input.ChatID); err != nil {
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
	defer unsubscribe()
	// conn、err 保存conn、err，供当前处理流程使用
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionContextTakeover})
	if err != nil {
		return
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	conn.SetReadLimit(8 << 10)
	go func() {
		for {
			if // readErr 保存readErr，供当前处理流程使用
			_, _, readErr := conn.Read(ctx); readErr != nil {
				cancel()
				return
			}
		}
	}()
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
	owned, err := s.Store.Cookies.ExistsOwned(r.Context(), sess.UserID, accountID) // owned 和 err 表示账号归属及查询错误。
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
