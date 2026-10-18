package automation

import (
	"errors"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// TestManualFullDeliveryReplaysUploadedImage 验证真实加密快照的人工补发不读本地素材、不取卡，保留平台尺寸和确认凭证。
func TestManualFullDeliveryReplaysUploadedImage(t *testing.T) {
	// ctx、store、center、platform、order、cleanup 复用真实 SQLite 及平台替身的人工发货夹具。
	ctx, store, center, _, platform, order, cleanup := newManualDeliveryFixture(t, "local-image-replay")
	defer cleanup()
	// sender 把原素材设为不可用，任何再次加载都必须使测试失败。
	sender := &localDeliverySender{prepareErr: errors.New("本地原图已删除")}
	center.actions.senders = blockingSenderProvider{sender: sender}
	// ruleID 关联订单原有规则，补发不能从该规则再次取卡。
	var ruleID int64
	if err := store.DB.QueryRowContext(ctx, `SELECT id FROM automation_rules WHERE cookie_id=? AND item_id=? AND trigger_type=?`, order.CookieID, order.ItemID, TriggerOrderPaid).Scan(&ruleID); err != nil { // err 是本地夹具读取错误。
		t.Fatal(err)
	}
	// runID、started、startErr 创建等待人工核对的历史发货运行。
	runID, started, startErr := store.Automation.TryStartRun(ctx, db.AutomationRun{
		RuleID: ruleID, CookieID: order.CookieID, ItemID: order.ItemID, OrderID: order.OrderID,
		TriggerType: TriggerOrderPaid, TriggerKey: buildManualDeliveryTriggerKey(Task{OrderID: order.OrderID}), LeaseExpiresAt: time.Now().Add(time.Minute).Unix(),
	})
	if startErr != nil || !started {
		t.Fatalf("创建图片快照运行失败: %v", startErr)
	}
	// run、runErr 保存开始动作所需的租约代次。
	run, runErr := store.Automation.GetRun(ctx, runID)
	if runErr != nil {
		t.Fatal(runErr)
	}
	if claimed, claimErr := store.Automation.StartRunAction(ctx, runID, run.AttemptCount, 0, time.Now().Add(time.Minute).Unix()); claimErr != nil || !claimed { // claimed、claimErr 必须保持生产检查点占用语义。
		t.Fatalf("领取图片动作失败: %v", claimErr)
	}
	// proof 模拟本地上传成功、图片消息结果未知后持久化的平台引用，不含本地路径。
	proof := db.AutomationDeliveryProof{PicList: []string{"https://cdn.example/frozen.png"}, ExpectedUnits: 1, UnknownUnits: 1,
		Messages: []db.AutomationDeliveryMessage{{Kind: "uploaded_image", Content: "https://cdn.example/frozen.png", Width: 40, Height: 30}},
	}
	saveManualReplayTestPlan(t, ctx, store, runID, proof.ExpectedUnits)
	if err := store.Automation.QuarantineRunResultWithProof(ctx, runID, run.AttemptCount, 0, "图片回显超时", &proof); err != nil { // err 表示加密凭证无法落库，禁止假定后续可重放。
		t.Fatal(err)
	}
	// sent、replayErr 验证人工补发只直发一次平台快照并按原先规则确认发货。
	sent, replayErr := center.ManualFullDelivery(ctx, order)
	if replayErr != nil || sent != 1 || len(sender.paths) != 0 || len(sender.images) != 1 || len(sender.texts) != 0 || platform.consignCalls != 1 {
		t.Fatalf("图片快照未原样补发: sent=%d prepares=%d err=%v", sent, len(sender.paths), replayErr)
	}
	// saved、savedErr 从数据库重新解密，验证尺寸不会因快照持久化丢失。
	saved, savedErr := store.Automation.GetRun(ctx, runID)
	if savedErr != nil {
		t.Fatal(savedErr)
	}
	if saved.Status != "success" || len(saved.DeliveryProof.Messages) != 1 || saved.DeliveryProof.Messages[0].Width != 40 || saved.DeliveryProof.Messages[0].Height != 30 || saved.DeliveryProof.PicList[0] != sender.images[0].URL {
		t.Fatal("图片快照收口后缺少原始平台地址或尺寸")
	}
}
