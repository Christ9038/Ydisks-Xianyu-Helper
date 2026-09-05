package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
)

// ownershipEventHandler 在同步消息分发测试中把系统事件交给真实自动化中心，记录是否错误投递买家副本。
// 本夹具由测试单协程持有；不启动账号连接或后台调度。
type ownershipEventHandler struct {
	// recordingHandler 提供聊天等既有接口，系统事件另行接入真实事实记录路径。
	*recordingHandler
	// center 使用当前测试隔离 SQLite 数据库，不配置任何自动化规则。
	center *automation.Center
	// accounts 记录实际进入自动化中心的接收账号，不保存凭证或原始报文。
	accounts []string
	// failures 保存中心处理错误，防止分发层仅记录日志后掩盖持久化失败。
	failures []error
}

// HandleSystemEvent 将 task 交给 h 的中心并使用 ctx 限定数据库操作；返回处理错误供分发层记录。
func (h *ownershipEventHandler) HandleSystemEvent(ctx context.Context, task automation.Task) error {
	h.accounts = append(h.accounts, task.AccountID)
	// handleErr 是当前事件事实记录或规则匹配的错误，保留给测试断言。
	handleErr := h.center.HandleTask(ctx, task)
	if handleErr != nil {
		h.failures = append(h.failures, handleErr)
	}
	return handleErr
}

// TestMessageDispatch_ReviewOwnershipWithoutRules 使用 t 验证评价副本乱序、重放时，仅卖家事实落库且不生成自动化运行。
// 同步调用真实消息分发入口保证每一步断言确定，不依赖等待时间或外部平台。
func TestMessageDispatch_ReviewOwnershipWithoutRules(t *testing.T) {
	// roles 保存接收副本的顺序；空角色表示已有协议允许的合法卖家评价。
	for _, roles := range [][]string{
		{"buyer", "buyer"},
		{"buyer", "seller", "buyer", "seller"},
		{"seller", "buyer", "seller", "buyer"},
		{"buyer", "", "buyer", ""},
		{"", "buyer", "", "buyer"},
	} {
		// t 隔离每个顺序的账号、订单和运行状态。
		t.Run(fmt.Sprint(roles), func(t *testing.T) {
			// account、store、cleanup 复用本地数据库夹具；account 不启动连接，由测试退出时停止并关闭数据库。
			account, _, store, cleanup := newAccountForTest(t)
			defer cleanup()
			defer account.Stop()
			// ctx 是本测试同步数据库操作的上下文。
			ctx := context.Background()
			// admin、adminErr 提供第二个接收账号的管理用户归属。
			admin, adminErr := store.Users.GetByUsername(ctx, "admin")
			if adminErr != nil || admin == nil {
				t.Fatalf("读取测试管理员失败: %v", adminErr)
			}
			// saveErr 是创建买家接收账号的错误；本地账号键故意不同于平台发送者 UID。
			if saveErr := store.Cookies.Save(ctx, "buyer-account", "", admin.ID); saveErr != nil {
				t.Fatal(saveErr)
			}
			// statusErr 保证买家账号启用，避免停用状态掩盖错误的事件投递。
			if statusErr := store.Cookies.SetStatus(ctx, "buyer-account", true); statusErr != nil {
				t.Fatal(statusErr)
			}
			// handler 将消息分发和真实 Center 串联；无规则时不得发生外部动作。
			handler := &ownershipEventHandler{recordingHandler: &recordingHandler{}, center: automation.New(store, nil, nil)}
			// sellerSeen 记录是否已有合法卖家副本；sellerCalls 是应进入中心的次数，重放也必须保持归属。
			sellerSeen, sellerCalls := false, 0
			// step、role 分别是当前重放步骤和该副本携带的接收方角色。
			for step, role := range roles {
				// accountID 按副本接收方选择本地账号，绝不按 senderUserId 推断卖家。
				accountID := "cid"
				if role == "buyer" {
					accountID = "buyer-account"
				} else {
					sellerSeen = true
					sellerCalls++
				}
				// dispatcher 使用真实同步分发入口；回调仅返回当前夹具 handler，不创建连接或调度任务。
				dispatcher := newMessageDispatcher(messageDispatcherConfig{
					CookieID: accountID,
					// 返回本测试共享的系统事件处理器，用于跨账号副本对照。
					CurrentHandler: func() Handler { return handler },
				})
				// raw 保存同一交易卡片，订单链接、业务键一致；文案及发送者在两侧完全相同。
				var raw map[string]any
				// decodeErr 是确定性夹具的 JSON 解码错误；role 为空时不携带有效角色。
				if decodeErr := json.Unmarshal([]byte(fmt.Sprintf(`{"1":{"2":"62904549781@goofish","10":{
					"reminderContent":"[我完成了评价]","redReminder":"有新交易评价","senderUserId":"platform-buyer",
					"reminderUrl":"fleamarket://order_detail?id=3310145690545023994&role=%s&itemId=1063217820795",
					"extJson":"{\"updateKey\":\"62904549781:3310145690545023994:10:BUYER_RATE_SELLER:26\",\"contentType\":\"26\"}"}}}`, role)), &raw); decodeErr != nil {
					t.Fatal(decodeErr)
				}
				dispatcher.handleMessage(raw)
				if len(handler.failures) != 0 || len(handler.accounts) != sellerCalls {
					t.Errorf("步骤 %d: 中心处理错误数=%d，调用数=%d，期望=%d", step, len(handler.failures), len(handler.accounts), sellerCalls)
				}
				// receiver 是已经进入中心的接收账号，买家副本应在事实写入之前被拒绝。
				for _, receiver := range handler.accounts {
					if receiver != "cid" {
						t.Error("买家副本错误进入卖家自动化中心")
					}
				}
				// order、readErr 是当前步骤后的订单事实，买家先到时必须仍不存在。
				order, readErr := store.Orders.Get(ctx, "3310145690545023994")
				if !sellerSeen {
					if !errors.Is(readErr, db.ErrNotFound) || order != nil {
						t.Fatalf("步骤 %d: 买家副本不应新建订单，读取错误=%v", step, readErr)
					}
				} else if readErr != nil || order == nil {
					t.Fatalf("步骤 %d: 无匹配规则仍须记录合法卖家事实，错误=%v", step, readErr)
				} else if order.CookieID != "cid" || order.BuyerID != "platform-buyer" || order.ItemID != "1063217820795" || order.ChatID != "62904549781" || order.BuyerReviewedAt == "" {
					t.Fatalf("步骤 %d: 卖家归属或评价事实被副本污染", step)
				}
				// table 是需验证无副作用的运行和恢复任务表，仅查询本地夹具数据库。
				for _, table := range []string{"automation_runs", "automation_pending_tasks"} {
					// count 是当前表中的任务数；无匹配规则时两侧都不得创建运行或延期任务。
					var count int
					// countErr 保存固定测试表的计数查询错误。
					if countErr := store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); countErr != nil {
						t.Fatal(countErr)
					}
					if count != 0 {
						t.Fatalf("步骤 %d: %s 不应创建任务，数量=%d", step, table, count)
					}
				}
			}
		})
	}
}
