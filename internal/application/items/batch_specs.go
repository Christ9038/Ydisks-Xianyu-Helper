package items

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"xianyu-go/internal/orderspec"
)

// maxBatchSpecDimensions 是平台允许的规格类型数量上限。
const maxBatchSpecDimensions = 2

// maxBatchSpecCombinations 是平台允许的 SKU 组合数量上限。
const maxBatchSpecCombinations = 1500

// BatchSpecs 是批量行持久化的多规格快照；Specs 为空表示单规格商品。
type BatchSpecs struct {
	// Specs 是规格维度列表，规格值的图片通过 ImageIndex 引用 SpecImages。
	Specs []PublishSpec `json:"specs"`
	// SKUs 是规格笛卡尔积的价格库存列表。
	SKUs []PublishSKU `json:"skus"`
	// SpecImages 是规格图片引用，相对批次上传目录，与普通商品图片使用同一套安全校验。
	SpecImages []string `json:"spec_images"`
}

// IsMultiSpec 判断快照是否描述了多规格商品。
func (specs *BatchSpecs) IsMultiSpec() bool {
	return specs != nil && len(specs.Specs) > 0
}

// TotalQuantity 汇总所有 SKU 的库存，供多规格商品作为顶层库存。
func (specs *BatchSpecs) TotalQuantity() int {
	if specs == nil {
		return 0
	}
	// total 保存累计库存。
	total := 0
	// sku 是当前累计的 SKU。
	for _, sku := range specs.SKUs {
		total += sku.Quantity
	}
	return total
}

// DecodeBatchSpecs 解析持久化的规格快照；空串和空对象返回 nil 表示单规格。
func DecodeBatchSpecs(raw string) (*BatchSpecs, error) {
	// trimmed 是去除空白后的快照文本。
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" || trimmed == "null" {
		return nil, nil
	}
	// specs 保存解码结果。
	var specs BatchSpecs
	// err 表示快照 JSON 损坏。
	if err := json.Unmarshal([]byte(trimmed), &specs); err != nil {
		return nil, errors.New("规格快照格式错误")
	}
	if !specs.IsMultiSpec() {
		return nil, nil
	}
	return &specs, nil
}

// EncodeBatchSpecs 把规格快照序列化为持久化文本，单规格返回空串。
func EncodeBatchSpecs(specs *BatchSpecs) (string, error) {
	if !specs.IsMultiSpec() {
		return "", nil
	}
	// encoded、err 保存序列化结果。
	encoded, err := json.Marshal(specs)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// ValidateBatchSpecs 校验规格维度、SKU 组合、价格库存和图片索引，规则与平台发布前校验保持一致。
func ValidateBatchSpecs(specs *BatchSpecs) error {
	if !specs.IsMultiSpec() {
		if specs != nil && (len(specs.SKUs) > 0 || len(specs.SpecImages) > 0) {
			return errors.New("单规格商品不能包含 SKU 规格数据")
		}
		return nil
	}
	if len(specs.Specs) > maxBatchSpecDimensions {
		return errors.New("商品规格最多支持 2 个规格类型")
	}
	// combinations 保存规格值笛卡尔积数量。
	combinations := 1
	// names 保存已出现的规格名称。
	names := make(map[string]struct{}, len(specs.Specs))
	// valueSets 保存每个维度允许的规格值，用于核对 SKU 引用。
	valueSets := make([]map[string]struct{}, len(specs.Specs))
	// specIndex、spec 是当前校验的规格维度。
	for specIndex, spec := range specs.Specs {
		// name 是去除空白后的规格名称。
		name := strings.TrimSpace(spec.PropertyName)
		if name == "" {
			return errors.New("规格类型不能为空")
		}
		if strings.ContainsAny(name, ";；") {
			return fmt.Errorf("规格 %q 不能包含分号", name)
		}
		// exists 表示规格名称是否已在前面的维度中出现。
		if _, exists := names[name]; exists {
			return errors.New("规格类型不能重复")
		}
		names[name] = struct{}{}
		// values 保存当前维度已出现的规格值。
		values := make(map[string]struct{}, len(spec.Values))
		// value 是当前校验的规格值。
		for _, value := range spec.Values {
			// text 是去除空白后的规格值文本。
			text := strings.TrimSpace(value.Value)
			if text == "" {
				return fmt.Errorf("规格 %q 存在空规格值", name)
			}
			if strings.ContainsAny(text, ";；") {
				return fmt.Errorf("规格值 %q 不能包含分号", text)
			}
			// exists 表示规格值是否已在同一维度中出现。
			if _, exists := values[text]; exists {
				return fmt.Errorf("规格 %q 的规格值不能重复", name)
			}
			values[text] = struct{}{}
			if value.ImageIndex < -1 || value.ImageIndex >= len(specs.SpecImages) {
				return errors.New("规格图片索引无效")
			}
		}
		if len(values) < 2 {
			return fmt.Errorf("规格 %q 至少需要 2 个规格值", name)
		}
		valueSets[specIndex] = values
		combinations *= len(values)
	}
	if combinations > maxBatchSpecCombinations {
		return errors.New("规格组合数量不能超过 1500")
	}
	if len(specs.SKUs) != combinations {
		return errors.New("SKU 组合数量与规格值不匹配")
	}
	// seen 保存已出现的组合键，防止重复 SKU 掩盖缺失组合。
	seen := make(map[string]struct{}, len(specs.SKUs))
	// sku 是当前校验的 SKU。
	for _, sku := range specs.SKUs {
		if sku.PriceCents <= 0 {
			return errors.New("SKU 价格必须大于 0")
		}
		if sku.Quantity <= 0 {
			return errors.New("SKU 库存必须大于 0")
		}
		if len(sku.PropertyList) != len(specs.Specs) {
			return errors.New("SKU 规格组合不完整")
		}
		// keyParts 保存当前 SKU 各维度规格值。
		keyParts := make([]string, 0, len(sku.PropertyList))
		// index、property 是当前维度位置及其名称值对。
		for index, property := range sku.PropertyList {
			// valueText 是规范化后的规格值。
			valueText := strings.TrimSpace(property.ValueText)
			if strings.TrimSpace(property.PropertyText) != strings.TrimSpace(specs.Specs[index].PropertyName) || valueText == "" {
				return errors.New("SKU 规格组合与规格定义不匹配")
			}
			// exists 表示 SKU 的规格值是否属于该维度声明的规格值集合。
			if _, exists := valueSets[index][valueText]; !exists {
				return errors.New("SKU 规格组合与规格定义不匹配")
			}
			keyParts = append(keyParts, valueText)
		}
		// key 标识当前 SKU 的规格值组合。
		key := strings.Join(keyParts, "\x00")
		// exists 表示该规格组合是否已被前面的 SKU 使用。
		if _, exists := seen[key]; exists {
			return errors.New("SKU 规格组合不能重复")
		}
		seen[key] = struct{}{}
	}
	return nil
}

// SKUCombination 是一个 SKU 对应的规范化规格元组，用于自动发货动作按 SKU 绑定。
type SKUCombination struct {
	// SpecName 是按维度顺序以中文分号拼接的规格名称。
	SpecName string
	// SpecValue 是与 SpecName 同序的规格值。
	SpecValue string
}

// Combinations 返回每个 SKU 的规范化规格元组；调用前需通过 ValidateBatchSpecs。
func (specs *BatchSpecs) Combinations() ([]SKUCombination, error) {
	if !specs.IsMultiSpec() {
		return nil, nil
	}
	// combinations 保存每个 SKU 的规格元组。
	combinations := make([]SKUCombination, 0, len(specs.SKUs))
	// sku 是当前转换的 SKU。
	for _, sku := range specs.SKUs {
		// names、values 保存当前 SKU 的规格名称列和值列。
		names := make([]string, 0, len(sku.PropertyList))
		// values 保存当前 SKU 的规格值列。
		values := make([]string, 0, len(sku.PropertyList))
		// property 是当前维度的名称值对。
		for _, property := range sku.PropertyList {
			names = append(names, property.PropertyText)
			values = append(values, property.ValueText)
		}
		// normalized、err 保存规范化结果；与运行时订单规格比较使用同一套规则。
		normalized, err := orderspec.NormalizeColumns(strings.Join(names, "；"), strings.Join(values, "；"))
		if err != nil || normalized.Dimensions == 0 {
			return nil, errors.New("SKU 规格组合无法规范化")
		}
		combinations = append(combinations, SKUCombination{SpecName: normalized.Name, SpecValue: normalized.Value})
	}
	return combinations, nil
}
