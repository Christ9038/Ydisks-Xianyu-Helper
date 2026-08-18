package mtop

import "strings"

// publishLabels 将推荐类目中的用户选择转换为最终发布请求标签。
func publishLabels(category map[string]any) []any {
	// cards 保存推荐响应的属性卡片集合。
	cards, _ := category["cardList"].([]any)
	// out 保存所有已选属性对应的发布标签。
	out := []any{}
	// rawCard 是当前待处理的属性卡片原始结构。
	for _, rawCard := range cards {
		// cardData 是当前属性卡片的嵌套配置。
		cardData := mapFromAny(mapFromAny(rawCard)["cardData"])
		if cardData == nil {
			continue
		}
		// rawValue 是当前属性候选值。
		for _, rawValue := range cardData["valuesList"].([]any) {
			// value 是标准化后的候选属性值。
			value := mapFromAny(rawValue)
			if !publishLabelSelected(value["isClicked"]) {
				continue
			}
			// propertyID、propertyName 分别是当前属性的稳定平台标识和展示名称。
			propertyID, propertyName := mtopString(cardData["propertyId"]), mtopString(cardData["propertyName"])
			// channelCatID、catName 分别是已选类目值的频道标识和展示名称。
			channelCatID, catName := mtopString(value["channelCatId"]), mtopString(value["catName"])
			out = append(out, map[string]any{"channelCateName": catName, "valueId": nil, "channelCateId": channelCatID, "valueName": nil, "tbCatId": mtopString(value["tbCatId"]), "subPropertyId": nil, "labelType": "common", "subValueId": nil, "labelId": nil, "propertyName": propertyName, "isUserClick": "1", "isUserCancel": nil, "from": "newPublishChoice", "propertyId": propertyID, "labelFrom": "newPublish", "text": catName, "properties": propertyID + "##" + propertyName + ":" + channelCatID + "##" + catName})
			break
		}
	}
	return out
}

// publishLabelSelected 判断平台值是否以布尔或文本真值形式被选中。
func publishLabelSelected(value any) bool {
	// selected、ok 分别是 value 是否为布尔类型及其实际真值。
	if selected, ok := value.(bool); ok {
		return selected
	}
	return strings.EqualFold(strings.TrimSpace(mtopString(value)), "1") || strings.EqualFold(strings.TrimSpace(mtopString(value)), "true")
}
