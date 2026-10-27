package items

import (
	"context"
	"errors"
	"strings"
)

// PreviewRows 校验已经结构化的预检行（例如由商品模板展开），复用表格导入使用的同一套确定性规则，
// 并额外校验多规格快照、规格图片引用和发货动作的规格绑定。逐行错误写入各行，不中断其他行。
func (service *BatchPreviewService) PreviewRows(ctx context.Context, input BatchPreviewInput, rows []BatchPreviewRow) ([]BatchPreviewRow, error) {
	if service == nil || service.ownership == nil || service.images == nil {
		return nil, errors.New("批量预检服务未初始化")
	}
	if len(rows) == 0 {
		return nil, ErrBatchPreviewNoRows
	}
	// result 保存校验后的行副本，避免调用方持有的切片被就地修改。
	result := make([]BatchPreviewRow, len(rows))
	copy(result, rows)
	// index 是当前校验行的下标。
	for index := range result {
		// row 指向当前行，错误直接追加到该行。
		row := &result[index]
		row.RowNo = index + 2
		if strings.TrimSpace(row.CookieID) == "" {
			row.CookieID = input.DefaultCookieID
		}
		if strings.TrimSpace(row.Description) == "" {
			row.Description = row.Title
		}
		row.PostageMode = normalizePostageMode(row.PostageMode)
		service.validateRow(ctx, input, row)
		service.validateRowSpecs(input, row)
	}
	return result, nil
}

// validateRowSpecs 校验行的多规格快照、规格图片引用，以及发货动作与 SKU 的完整对应关系。
func (service *BatchPreviewService) validateRowSpecs(input BatchPreviewInput, row *BatchPreviewRow) {
	// err 保存 ValidateBatchSpecs 调用的失败原因。
	if err := ValidateBatchSpecs(row.Specs); err != nil {
		row.Errors = append(row.Errors, err.Error())
		return
	}
	// reference 是当前待校验的规格图片引用；单规格快照没有规格图片。
	for _, reference := range specImageReferences(row.Specs) {
		// err 表示规格图片引用安全校验错误。
		if err := service.images.ValidateImageReference(input.UploadDir, reference); err != nil {
			row.Errors = append(row.Errors, err.Error())
		}
	}
	row.Errors = append(row.Errors, automationSpecErrors(row.Specs, row.Automation)...)
}

// ValidateAutomationConfig 校验自动化配置的归属、动作范围，以及与规格快照的绑定关系；
// 商品模板在保存时调用，与发布前预检使用同一套规则。
func (service *BatchPreviewService) ValidateAutomationConfig(ctx context.Context, userID int64, config BatchPreviewAutomation, specs *BatchSpecs) []string {
	if service == nil || service.ownership == nil {
		return []string{"批量预检服务未初始化"}
	}
	return append(service.validateAutomation(ctx, userID, config), automationSpecErrors(specs, config)...)
}

// automationSpecErrors 校验发货动作与规格快照的对应关系：
// 单规格商品不能携带规格绑定；多规格商品的付款发货必须覆盖每个 SKU，评价赠品则只要求绑定到有效 SKU。
func automationSpecErrors(specs *BatchSpecs, automation BatchPreviewAutomation) []string {
	// combinations、err 保存每个 SKU 的规范化规格元组；单规格时为空。
	combinations, err := specs.Combinations()
	if err != nil {
		return []string{err.Error()}
	}
	// known 保存商品真实存在的 SKU 规格键。
	known := make(map[SKUCombination]struct{}, len(combinations))
	// combination 是当前 SKU 的规格元组。
	for _, combination := range combinations {
		known[combination] = struct{}{}
	}
	return append(specBindingErrors("付款发货", automation.PaidDelivery, known, true),
		specBindingErrors("评价赠品", automation.ReviewGift, known, false)...)
}

// specBindingErrors 校验一组发货动作的规格绑定：单规格商品（known 为空）不能绑定规格；
// 多规格商品的每个动作必须绑定到真实 SKU，requireAll 为真时还必须覆盖全部 SKU。
func specBindingErrors(label string, config BatchPreviewCardAutomation, known map[SKUCombination]struct{}, requireAll bool) []string {
	if !config.Enabled || config.ParseError != "" {
		return nil
	}
	// covered 保存已经被动作覆盖的 SKU 规格键。
	covered := make(map[SKUCombination]struct{}, len(known))
	// found 保存校验提示。
	var found []string
	// action 是当前发货动作。
	for _, action := range config.Actions {
		// bound 表示动作是否携带规格绑定。
		bound := strings.TrimSpace(action.SpecName) != "" || strings.TrimSpace(action.SpecValue) != ""
		if len(known) == 0 {
			if bound {
				found = append(found, label+"单规格商品的发货动作不能绑定规格")
			}
			continue
		}
		// key 是动作绑定的规格元组。
		key := SKUCombination{SpecName: action.SpecName, SpecValue: action.SpecValue}
		// ok 表示该规格绑定是否命中已知 SKU 组合。
		if _, ok := known[key]; !ok {
			found = append(found, label+"存在未绑定到有效 SKU 的发货动作")
			continue
		}
		covered[key] = struct{}{}
	}
	if len(known) > 0 && requireAll && len(covered) != len(known) {
		found = append(found, label+"必须为每个 SKU 规格组合配置发货内容")
	}
	return found
}

// specImageReferences 返回规格快照的图片引用，单规格返回空。
func specImageReferences(specs *BatchSpecs) []string {
	if !specs.IsMultiSpec() {
		return nil
	}
	return specs.SpecImages
}
