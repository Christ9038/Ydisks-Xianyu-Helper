package adapter

import (
	"context"
	"encoding/json"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/xianyu/mtop"
)

// mtopSpecsFromApplication 把应用层规格维度与 SKU 转换为平台发布请求结构。
func mtopSpecsFromApplication(specs []itemapp.PublishSpec, skus []itemapp.PublishSKU) ([]mtop.PublishSpec, []mtop.PublishSKU) {
	// specRows 保存转换后的平台规格维度。
	specRows := make([]mtop.PublishSpec, 0, len(specs))
	// spec 是当前待转换的规格维度。
	for _, spec := range specs {
		// values 保存当前维度的规格值及其图片索引。
		values := make([]mtop.PublishSpecValue, 0, len(spec.Values))
		// value 是当前维度中的一个规格值。
		for _, value := range spec.Values {
			values = append(values, mtop.PublishSpecValue{Value: value.Value, ImageIndex: value.ImageIndex})
		}
		specRows = append(specRows, mtop.PublishSpec{PropertyName: spec.PropertyName, SupportImage: spec.SupportImage, Values: values})
	}
	// skuRows 保存转换后的平台 SKU 组合。
	skuRows := make([]mtop.PublishSKU, 0, len(skus))
	// sku 是当前待转换的 SKU 组合。
	for _, sku := range skus {
		// properties 保存当前 SKU 的规格名称和值对。
		properties := make([]mtop.PublishSKUProperty, 0, len(sku.PropertyList))
		// property 是当前 SKU 中的一组规格名称和值。
		for _, property := range sku.PropertyList {
			properties = append(properties, mtop.PublishSKUProperty{PropertyText: property.PropertyText, ValueText: property.ValueText})
		}
		skuRows = append(skuRows, mtop.PublishSKU{PriceCents: sku.PriceCents, Quantity: sku.Quantity, PropertyList: properties})
	}
	return specRows, skuRows
}

// loadBatchPublishSpecs 解析批量行的多规格快照并读取规格图片；单规格快照返回全部空值。
// 规格图片与商品图片共用同一套本地路径和远程 URL 安全回调，totalQuantity 为各 SKU 库存之和。
func loadBatchPublishSpecs(ctx context.Context, uploadDir, specsJSON string, readLocal ReadPublishImageFile, downloadRemote DownloadPublishImageURL) ([]mtop.PublishSpec, []mtop.PublishSKU, []mtop.PublishImage, int, error) {
	// snapshot、err 保存解码后的多规格快照及解码错误。
	snapshot, err := itemapp.DecodeBatchSpecs(specsJSON)
	if err != nil || snapshot == nil {
		return nil, nil, nil, 0, err
	}
	// specs、skus 保存转换后的平台规格结构。
	specs, skus := mtopSpecsFromApplication(snapshot.Specs, snapshot.SKUs)
	// specImages 保存规格图片内容，没有规格图时保持为空。
	var specImages []mtop.PublishImage
	if len(snapshot.SpecImages) > 0 {
		// refsJSON 是规格图片引用的 JSON 数组，复用商品图片加载入口。
		refsJSON, marshalErr := json.Marshal(snapshot.SpecImages)
		if marshalErr != nil {
			return nil, nil, nil, 0, marshalErr
		}
		// loaded、loadErr 保存规格图片读取结果。
		loaded, loadErr := LoadBatchPublishImages(ctx, uploadDir, string(refsJSON), readLocal, downloadRemote)
		if loadErr != nil {
			return nil, nil, nil, 0, loadErr
		}
		specImages = loaded
	}
	return specs, skus, specImages, snapshot.TotalQuantity(), nil
}
