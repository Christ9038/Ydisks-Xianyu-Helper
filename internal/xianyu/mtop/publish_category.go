package mtop

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// RecommendPublishCategory 根据关键词调用闲鱼推荐接口，返回可直接用于发布的完整类目。
func (c *ClientImpl) RecommendPublishCategory(ctx context.Context, cookiesStr, keyword string) (PublishCategory, string, error) {
	// candidates、updated、err 保存候选类目列表、刷新后的 Cookie 和调用错误；首项始终是平台主推荐。
	candidates, updated, err := c.RecommendPublishCategoryCandidates(ctx, cookiesStr, keyword)
	if err != nil {
		return PublishCategory{}, updated, err
	}
	return candidates[0], updated, nil
}

// RecommendPublishCategoryCandidates 根据关键词调用一次闲鱼推荐接口，返回按推荐优先级排序的完整候选类目。
//
// 首项是平台主推荐（categoryPredictResult 或已选中的分类卡片项），其后依次是“分类”卡片
// （propertyId=-10000）valuesList 中其余字段完整的类目，按 catId 去重。成功时至少包含一项；
// 只发起一次平台请求，不额外扇出。
func (c *ClientImpl) RecommendPublishCategoryCandidates(ctx context.Context, cookiesStr, keyword string) ([]PublishCategory, string, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, cookiesStr, errors.New("类目关键词不能为空")
	}
	// data、updated、err 保存推荐接口响应数据、刷新后的 Cookie 和调用错误。
	data, updated, err := c.recommendPublishCategory(ctx, cookiesStr, keyword, keyword, nil)
	if err != nil {
		return nil, updated, err
	}
	// primary 是平台主推荐类目。
	primary := publishCategoryFromMap(mapFromAny(data["categoryPredictResult"]))
	if !validPublishCategory(primary) {
		return nil, updated, fmt.Errorf("%w: 推荐结果缺少完整类目路径", ErrPublishCategoryUnrecognized)
	}
	return mergePublishCategoryCandidates(primary, categoryCandidatesFromCards(data)), updated, nil
}

// publishCategoryFromMap 把平台类目对象转换为去除空白后的 PublishCategory。
func publishCategoryFromMap(raw map[string]any) PublishCategory {
	return PublishCategory{
		CatID:        strings.TrimSpace(mtopString(raw["catId"])),
		CatName:      strings.TrimSpace(mtopString(raw["catName"])),
		ChannelCatID: strings.TrimSpace(mtopString(raw["channelCatId"])),
		TBCatID:      strings.TrimSpace(mtopString(raw["tbCatId"])),
	}
}

// categoryCandidatesFromCards 读取“分类”卡片 valuesList 中全部字段完整的候选类目，保持平台返回顺序。
func categoryCandidatesFromCards(data map[string]any) []PublishCategory {
	// cards 是推荐响应里的卡片列表；缺失时没有额外候选。
	cards, _ := data["cardList"].([]any)
	// candidates 保存按平台顺序收集的有效候选。
	var candidates []PublishCategory
	// rawCard 表示当前遍历的原始卡片。
	for _, rawCard := range cards {
		// cardData 是卡片数据；非分类卡片会被跳过。
		cardData := mapFromAny(mapFromAny(rawCard)["cardData"])
		if cardData == nil || mtopString(cardData["propertyId"]) != "-10000" {
			continue
		}
		// values 是该分类卡片的候选类目值列表。
		values, _ := cardData["valuesList"].([]any)
		// rawValue 表示当前遍历的原始候选值。
		for _, rawValue := range values {
			// candidate 是转换后的候选类目，缺少必要字段时丢弃。
			candidate := publishCategoryFromMap(mapFromAny(rawValue))
			if validPublishCategory(candidate) {
				candidates = append(candidates, candidate)
			}
		}
	}
	return candidates
}

// mergePublishCategoryCandidates 把主推荐放在首位并按 catId 去重追加其余候选。
func mergePublishCategoryCandidates(primary PublishCategory, others []PublishCategory) []PublishCategory {
	// merged 保存去重后的有序候选，主推荐固定为首项。
	merged := []PublishCategory{primary}
	// seen 记录已加入候选的类目 ID。
	seen := map[string]bool{primary.CatID: true}
	// candidate 表示当前待合并的候选。
	for _, candidate := range others {
		if seen[candidate.CatID] {
			continue
		}
		seen[candidate.CatID] = true
		merged = append(merged, candidate)
	}
	return merged
}
