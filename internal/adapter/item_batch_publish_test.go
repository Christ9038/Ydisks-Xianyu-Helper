package adapter

import (
	"context"
	"testing"
	"time"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// batchPublishClientStub 是批量远端发布测试使用的平台客户端替身。
type batchPublishClientStub struct {
	// mtop.Client 提供未涉及本测试的方法默认实现。
	mtop.Client
	// publish 保存批量发布请求的可控行为。
	publish func(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error)
}

// PublishItem 调用测试注入的批量发布行为。
func (c batchPublishClientStub) PublishItem(ctx context.Context, cookies string, request mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
	return c.publish(ctx, cookies, request)
}

// TestItemBatchPublishPortPersistsRemoteCheckpoint 验证批量远端端口完成租约检查点和 Cookie 写回。
func TestItemBatchPublishPortPersistsRemoteCheckpoint(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// admin、adminErr 保存批次所属用户及读取错误。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatalf("读取测试用户失败: %v", adminErr)
	}
	// batch 保存待发布的批次配置。
	batch := &db.ItemPublishBatch{ID: "batch-remote-port", UserID: admin.ID, DefaultCookieID: "cid", UploadDir: t.TempDir(), LocationJSON: `{"division_id":"330100"}`, Status: "pending"}
	// rows 保存待发布的单条明细。
	rows := []db.ItemPublishBatchRow{{RowNo: 1, CookieID: "cid", Title: "批量商品", Description: "批量描述", Price: "12.50", Quantity: 2, PostageMode: "free", ImagesJSON: `["a.png"]`, CategoryJSON: `{"cat_id":"1","cat_name":"类目","channel_cat_id":"2"}`, Status: "pending"}}
	// createErr 保存批次写入错误。
	if createErr := store.PublishBatches.Create(ctx, batch, rows); createErr != nil {
		t.Fatalf("创建测试批次失败: %v", createErr)
	}
	// lease 保存本次 worker 的有效租约时间。
	lease := time.Now().UTC().Add(time.Minute).Unix()
	// claimed、claimErr 保存批次租约抢占结果。
	claimed, claimErr := store.PublishBatches.ClaimBatch(ctx, batch.ID, "worker-remote", lease)
	if claimErr != nil || !claimed {
		t.Fatalf("抢占批次失败: claimed=%v err=%v", claimed, claimErr)
	}
	// storedRows、rowsErr 保存抢占后的明细行。
	storedRows, rowsErr := store.PublishBatches.Rows(ctx, batch.ID)
	if rowsErr != nil || len(storedRows) != 1 {
		t.Fatalf("读取测试明细失败: rows=%+v err=%v", storedRows, rowsErr)
	}
	// rowClaimed、rowClaimErr 保存明细租约抢占结果。
	rowClaimed, rowClaimErr := store.PublishBatches.ClaimRow(ctx, storedRows[0].ID, "worker-remote")
	if rowClaimErr != nil || !rowClaimed {
		t.Fatalf("抢占明细失败: claimed=%v err=%v", rowClaimed, rowClaimErr)
	}
	// publishCalls 记录平台调用次数，用于确认远端检查点重试不会再次创建商品。
	publishCalls := 0
	// client 是返回成功商品和新 Cookie 的平台替身。
	client := batchPublishClientStub{publish: func(_ context.Context, cookies string, request mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
		publishCalls++
		if cookies == "" || request.PriceCents != 1250 || request.Quantity != 2 || len(request.Images) != 1 {
			t.Fatalf("平台请求字段异常: cookies=%q price=%d quantity=%d images=%d", cookies, request.PriceCents, request.Quantity, len(request.Images))
		}
		return &mtop.PublishItemResult{ItemID: "remote-item", ItemURL: "https://example.test/item/remote-item", Title: "批量商品", PriceText: "12.50", Quantity: 2, UpdatedCookies: "unb=2; _m_h5_tk=next"}, nil
	}}
	// port 是绑定测试数据库、平台替身和图片回调的批量远端适配器。
	port := NewItemBatchPublishPort(store, func() mtop.Client { return client }, nil, nil, nil,
		func(_ string, ref string) ([]byte, string, string, error) {
			return []byte("image"), "image/png", ref, nil
		},
		func(context.Context, string) ([]byte, string, error) { return nil, "", nil })
	// outcome、publishErr 保存远端端口结果及错误。
	outcome, publishErr := port.PublishRemoteRow(ctx, admin.ID, batchRowApplicationModel(storedRows[0]), "worker-remote")
	if publishErr != nil || outcome.Result == nil || outcome.Result.ItemID != "remote-item" {
		t.Fatalf("远端端口结果异常: outcome=%+v err=%v", outcome, publishErr)
	}
	// savedRows、savedErr 保存远端检查点和明细状态。
	savedRows, savedErr := store.PublishBatches.Rows(ctx, batch.ID)
	if savedErr != nil || len(savedRows) != 1 || savedRows[0].ItemID != "remote-item" || savedRows[0].FailureKind != "post_publish" {
		t.Fatalf("远端检查点未保存: rows=%+v err=%v", savedRows, savedErr)
	}
	// cookieValue、cookieErr 保存平台响应 Cookie 的持久化结果。
	cookieValue, cookieErr := store.Cookies.GetValue(ctx, "cid")
	if cookieErr != nil || cookieValue != "unb=2; _m_h5_tk=next" {
		t.Fatalf("响应 Cookie 未写回: value=%q err=%v", cookieValue, cookieErr)
	}
	// checkpointPort 使用缺少图片回调的适配器，验证已有远端结果可以直接恢复。
	checkpointPort := NewItemBatchPublishPort(store, func() mtop.Client { return client }, nil, nil, nil, nil, nil)
	// checkpointOutcome 保存从远端检查点恢复出的应用结果。
	// checkpointErr 保存检查点恢复阶段的错误。
	checkpointOutcome, checkpointErr := checkpointPort.PublishRemoteRow(ctx, admin.ID, batchRowApplicationModel(savedRows[0]), "worker-remote")
	if checkpointErr != nil || checkpointOutcome.Result == nil || checkpointOutcome.Result.ItemID != "remote-item" {
		t.Fatalf("远端检查点重试异常: outcome=%+v err=%v", checkpointOutcome, checkpointErr)
	}
	if publishCalls != 1 {
		t.Fatalf("远端检查点重试不应再次调用平台: calls=%d", publishCalls)
	}
}

// TestItemBatchPublishPortRejectsMissingDependencies 验证批量远端适配器缺少端口依赖时快速失败。
func TestItemBatchPublishPortRejectsMissingDependencies(t *testing.T) {
	// port 是未配置数据库的批量远端适配器。
	port := NewItemBatchPublishPort(nil, nil, nil, nil, nil, func(string, string) ([]byte, string, string, error) { return nil, "", "", nil }, func(context.Context, string) ([]byte, string, error) { return nil, "", nil })
	// _, publishErr 保存依赖校验错误。
	_, publishErr := port.PublishRemoteRow(context.Background(), 1, itemapp.BatchRow{BatchID: "batch"}, "worker")
	if publishErr == nil {
		t.Fatal("缺少数据库时不应伪装批量远端发布成功")
	}
}
