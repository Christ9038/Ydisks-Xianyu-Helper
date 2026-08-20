package adapter

import (
	"context"
	"sync"

	chatapp "xianyu-go/internal/application/chat"
	domainchat "xianyu-go/internal/chat"
	"xianyu-go/internal/db"
)

// chatSubscriptionProvider 将聊天领域事件转换为不暴露数据库模型的应用事件。
type chatSubscriptionProvider struct {
	// service 保存聊天领域事件中心；HTTP 层只接收应用层事件。
	service *domainchat.Service
}

// NewChatSubscriptionProvider 创建实时聊天订阅适配器。
func NewChatSubscriptionProvider(service *domainchat.Service) chatapp.SubscriptionProvider {
	if service == nil {
		return nil
	}
	return chatSubscriptionProvider{service: service}
}

// Subscribe 转发指定用户有权接收的领域事件，并由取消函数负责释放领域订阅和转发协程。
func (p chatSubscriptionProvider) Subscribe(ctx context.Context, userID int64) (<-chan chatapp.Event, func(), error) {
	if p.service == nil || userID <= 0 {
		return nil, nil, chatapp.ErrSubscriptionUnavailable
	}
	// source 和 sourceCancel 保存领域订阅通道及其幂等清理函数。
	source, sourceCancel, err := p.service.Subscribe(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	// forwardCtx 控制事件转换协程的退出；父请求结束时会自动取消。
	forwardCtx, cancelForward := context.WithCancel(ctx)
	// output 保存转换后的应用层事件，缓冲避免短暂写入抖动阻塞领域发布者。
	output := make(chan chatapp.Event, 128)
	// forwardDone 标记转发 goroutine 已退出，取消方等待它后再返回，避免留下游离后台任务。
	forwardDone := make(chan struct{})
	// stopOnce 保证调用方重复清理时不会重复关闭资源。
	var stopOnce sync.Once
	// stop 取消转发并释放领域订阅；调用方可在 WebSocket 握手失败和正常关闭路径重复调用。
	stop := func() {
		stopOnce.Do(func() {
			cancelForward()
			sourceCancel()
		})
		<-forwardDone
	}
	go func() {
		defer close(forwardDone)
		defer close(output)
		defer stopOnce.Do(func() {
			cancelForward()
			sourceCancel()
		})
		for {
			select {
			case <-forwardCtx.Done():
				return
			case // event、ok 保存领域事件及源订阅通道是否仍然开放。
			event, ok := <-source:
				if !ok {
					return
				}
				// converted 保存转换后的应用层事件，避免领域数据库模型越过适配器。
				converted := chatApplicationEvent(event)
				select {
				case output <- converted:
				case <-forwardCtx.Done():
					return
				}
			}
		}
	}()
	return output, stop, nil
}

// chatApplicationEvent 将领域事件中的数据库模型转换为敏感数据隔离后的应用模型。
func chatApplicationEvent(event domainchat.Event) chatapp.Event {
	// converted 保存实时事件的非敏感应用层表示。
	converted := chatapp.Event{Type: event.Type}
	if event.Message != nil {
		// message 保存转换后的非敏感消息指针。
		message := chatApplicationMessage(event.Message)
		converted.Message = &message
	}
	if event.Session != nil {
		// session 保存转换后的非敏感会话摘要指针。
		session := chatApplicationSession(event.Session)
		converted.Session = &session
	}
	return converted
}

// chatApplicationSession 将领域会话摘要转换为应用层会话摘要。
func chatApplicationSession(session *db.ChatSession) chatapp.Session {
	if session == nil {
		return chatapp.Session{}
	}
	return chatapp.Session{
		AccountID: session.CookieID, ChatID: session.ChatID, BuyerID: session.BuyerID,
		BuyerName: session.BuyerName, BuyerAvatar: session.BuyerAvatar, ItemID: session.ItemID,
		ItemTitle: session.ItemTitle, ItemImageURL: session.ItemImageURL, LastMessage: session.LastMessage, LastMessageAt: session.LastMessageAt,
		UnreadCount: session.UnreadCount,
	}
}

// 编译期确认实时聊天订阅适配器覆盖应用层端口。
var _ chatapp.SubscriptionProvider = chatSubscriptionProvider{}
