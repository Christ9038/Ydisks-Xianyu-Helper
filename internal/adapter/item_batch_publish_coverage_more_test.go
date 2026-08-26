package adapter

import (
	"context"
	"errors"
	"testing"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// TestItemBatchPublishPortRejectsInvalidInputsBeforeRemoteCall 验证批量远端发布在调用平台前拒绝损坏的位置、价格、类目和图片数据。
func TestItemBatchPublishPortRejectsInvalidInputsBeforeRemoteCall(t *testing.T) {
	// ctx 是本测试批次预检共用的非取消上下文。
	ctx := context.Background()
	// store、cleanup 保存本地批次预检测试数据库及资源清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// admin、adminErr 保存批次所属用户及读取错误。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// readImage 是会成功返回图片内容的本地图片读取替身。
	readImage := func(string, string) ([]byte, string, string, error) {
		return []byte("image"), "image/png", "image.png", nil
	}
	// downloadImage 是会成功返回图片内容的远程图片下载替身。
	downloadImage := func(context.Context, string) ([]byte, string, error) {
		return []byte("image"), "image/png", nil
	}
	// invalidLocationBatch 保存损坏发货地配置的批次。
	invalidLocationBatch := &db.ItemPublishBatch{ID: "invalid-location", UserID: admin.ID, UploadDir: t.TempDir(), LocationJSON: "not-json"}
	// invalidLocationRows 保存触发发货地解析的有效金额明细。
	invalidLocationRows := []db.ItemPublishBatchRow{{CookieID: "cid", Title: "商品", Price: "1", ImagesJSON: `[]`}}
	// err 保存损坏发货地批次的创建错误。
	if err := store.PublishBatches.Create(ctx, invalidLocationBatch, invalidLocationRows); err != nil {
		t.Fatal(err)
	}
	// port 是绑定本地数据库和图片替身的批量远端适配器。
	port := NewItemBatchPublishPort(store, nil, nil, nil, nil, readImage, downloadImage)
	// _, locationErr 保存位置配置损坏时的预检错误。
	_, locationErr := port.PublishRemoteRow(ctx, admin.ID, itemapp.BatchRow{BatchID: invalidLocationBatch.ID, ImagesJSON: `[]`}, "worker", nil)
	if locationErr == nil {
		t.Fatal("损坏发货地应在平台调用前失败")
	}

	// invalidPriceBatch 保存有效位置配置的批次，用于继续覆盖价格校验分支。
	invalidPriceBatch := &db.ItemPublishBatch{ID: "invalid-price", UserID: admin.ID, UploadDir: t.TempDir(), LocationJSON: `{}`}
	// invalidPriceRows 保存零价格明细。
	invalidPriceRows := []db.ItemPublishBatchRow{{CookieID: "cid", Title: "商品", Price: "0", ImagesJSON: `[]`}}
	// err 保存零价格批次的创建错误。
	if err := store.PublishBatches.Create(ctx, invalidPriceBatch, invalidPriceRows); err != nil {
		t.Fatal(err)
	}
	// _, priceErr 保存价格校验错误。
	_, priceErr := port.PublishRemoteRow(ctx, admin.ID, itemapp.BatchRow{BatchID: invalidPriceBatch.ID, Price: "0", ImagesJSON: `[]`}, "worker", nil)
	if priceErr == nil {
		t.Fatal("零价格应在平台调用前失败")
	}

	// invalidCategoryBatch 保存损坏默认类目的批次。
	invalidCategoryBatch := &db.ItemPublishBatch{ID: "invalid-category", UserID: admin.ID, UploadDir: t.TempDir(), LocationJSON: `{}`}
	// invalidCategoryRows 保存有效价格明细。
	invalidCategoryRows := []db.ItemPublishBatchRow{{CookieID: "cid", Title: "商品", Price: "1", ImagesJSON: `[]`, CategoryJSON: `{"cat_id":"1"}`}}
	// err 保存不完整类目批次的创建错误。
	if err := store.PublishBatches.Create(ctx, invalidCategoryBatch, invalidCategoryRows); err != nil {
		t.Fatal(err)
	}
	// _, categoryErr 保存类目校验错误。
	_, categoryErr := port.PublishRemoteRow(ctx, admin.ID, itemapp.BatchRow{BatchID: invalidCategoryBatch.ID, Price: "1", ImagesJSON: `[]`, CategoryJSON: `{"cat_id":"1"}`}, "worker", nil)
	if categoryErr == nil {
		t.Fatal("不完整类目应在平台调用前失败")
	}

	// invalidImageBatch 保存有效位置、价格和类目的批次，用于覆盖图片读取错误。
	invalidImageBatch := &db.ItemPublishBatch{ID: "invalid-image", UserID: admin.ID, UploadDir: t.TempDir(), LocationJSON: `{}`}
	// invalidImageRows 保存需要读取本地图片的明细。
	invalidImageRows := []db.ItemPublishBatchRow{{CookieID: "cid", Title: "商品", Price: "1", ImagesJSON: `["image.png"]`}}
	// err 保存图片错误批次的创建错误。
	if err := store.PublishBatches.Create(ctx, invalidImageBatch, invalidImageRows); err != nil {
		t.Fatal(err)
	}
	// imageFailPort 是返回图片读取错误的批量远端适配器。
	imageFailPort := NewItemBatchPublishPort(store, nil, nil, nil, nil, func(string, string) ([]byte, string, string, error) {
		return nil, "", "", errors.New("image read failed")
	}, downloadImage)
	// _, imageErr 保存图片读取错误。
	_, imageErr := imageFailPort.PublishRemoteRow(ctx, admin.ID, itemapp.BatchRow{BatchID: invalidImageBatch.ID, Price: "1", ImagesJSON: `["image.png"]`}, "worker", nil)
	if imageErr == nil {
		t.Fatal("图片读取失败应在平台调用前返回")
	}
}

// TestItemBatchPublishBoundaryHelpersCoverFallbacks 验证批量发布适配器的空依赖、历史数据和结果转换边界。
func TestItemBatchPublishBoundaryHelpersCoverFallbacks(t *testing.T) {
	// nilPort 表示未装配任何批量发布依赖的空接收者。
	var nilPort *ItemBatchPublishPort
	if nilPort.validate() == nil {
		t.Fatal("空批量发布适配器应拒绝执行")
	}
	// port 表示仅绑定数据库但未配置图片读取回调的适配器。
	port := &ItemBatchPublishPort{}
	if port.validateImageDependencies() == nil {
		t.Fatal("缺少图片读取回调时应返回依赖错误")
	}
	// client 表示缺少注入客户端时由适配器提供的默认平台客户端。
	if port.mtopClient() == nil {
		t.Fatal("缺少客户端时应提供默认平台客户端")
	}
	port.recoverExpired(context.Background(), "cid", nil)

	// locationCases 保存空配置、有效配置和损坏配置的发货地边界。
	locationCases := []struct {
		raw     string
		wantNil bool
		wantErr bool
		wantID  string
	}{
		{raw: "", wantNil: true},
		{raw: `{}`, wantNil: true},
		{raw: `{"division_id":"330106","city":"杭州"}`, wantID: "330106"},
		{raw: `not-json`, wantErr: true},
	}
	// locationCase 表示当前发货地配置样例。
	for _, locationCase := range locationCases {
		// location、err 保存发货地解析结果及错误。
		location, err := batchPublishLocation(locationCase.raw)
		if locationCase.wantErr {
			if err == nil {
				t.Errorf("损坏发货地=%q 应返回错误", locationCase.raw)
			}
			continue
		}
		if err != nil || (locationCase.wantNil && location != nil) || (!locationCase.wantNil && location == nil) {
			t.Errorf("发货地=%q location=%+v err=%v", locationCase.raw, location, err)
		}
		if location != nil && location.DivisionID != locationCase.wantID {
			t.Errorf("发货地区划=%q want=%q", location.DivisionID, locationCase.wantID)
		}
	}

	// categoryCases 保存默认类目完整、不完整和空配置边界。
	categoryCases := []struct {
		raw     string
		wantNil bool
		wantErr bool
	}{
		{raw: "", wantNil: true},
		{raw: `{"cat_name":"食品"}`, wantErr: true},
		{raw: `{"cat_id":"1","cat_name":"食品","channel_cat_id":"2","tb_cat_id":"3"}`},
	}
	// categoryCase 表示当前默认类目配置样例。
	for _, categoryCase := range categoryCases {
		// category、err 保存默认类目解析结果及错误。
		category, err := batchPublishCategory(categoryCase.raw)
		if categoryCase.wantErr {
			if err == nil {
				t.Errorf("不完整类目=%q 应返回错误", categoryCase.raw)
			}
			continue
		}
		if err != nil || (categoryCase.wantNil && category != nil) || (!categoryCase.wantNil && category == nil) {
			t.Errorf("类目=%q category=%+v err=%v", categoryCase.raw, category, err)
		}
	}

	// resultFromMTOP 验证空平台结果和完整平台结果的应用层转换。
	if batchPublishResultFromMTOP(nil) != nil {
		t.Fatal("空平台结果应转换为空应用结果")
	}
	// resultFromMTOP 保存完整平台结果的应用层映射。
	resultFromMTOP := batchPublishResultFromMTOP(&mtop.PublishItemResult{ItemID: "item", ItemURL: "url", Quantity: 2, RawData: map[string]any{"ok": true}})
	if resultFromMTOP == nil || resultFromMTOP.ItemID != "item" || resultFromMTOP.Quantity != 2 {
		t.Fatalf("平台结果转换异常：%+v", resultFromMTOP)
	}
	// resultFromRow 保存无法解析原始 JSON 时仍可恢复的检查点结果。
	resultFromRow := batchPublishResultFromRow(itemapp.BatchRow{ItemID: "row-item", Price: "3.00", RawJSON: "not-json"})
	if resultFromRow == nil || resultFromRow.ItemID != "row-item" || len(resultFromRow.RawData) != 0 {
		t.Fatalf("检查点结果转换异常：%+v", resultFromRow)
	}

	// activeContext 是允许创建短期检查点上下文的正常父上下文。
	activeContext := context.Background()
	// derived、cancel 保存正常父上下文派生的检查点上下文及其释放函数。
	derived, cancel := batchPublishStatusContext(activeContext)
	if derived == nil {
		t.Fatal("正常父上下文应创建检查点上下文")
	} else {
		cancel()
	}
	// canceledContext 验证已取消父上下文仍获得独立的收束上下文，避免检查点写入丢失。
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	// canceledDerived、derivedCancel 保存取消父上下文下的新检查点上下文及释放函数。
	canceledDerived, derivedCancel := batchPublishStatusContext(canceledContext)
	if canceledDerived == nil {
		t.Fatal("取消父上下文应创建独立检查点上下文")
	} else {
		derivedCancel()
	}
	if firstBatchNonEmpty("", "  ", "商品") != "商品" || firstBatchNonEmpty("", " ") != "" {
		t.Fatal("批量文本回退选择异常")
	}
}
