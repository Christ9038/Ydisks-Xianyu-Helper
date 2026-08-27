// Package orderspec 提供订单与自动化规则共用的规格规范化能力。
package orderspec

import (
	"errors"
	"strings"
)

// ErrDimensionMismatch 表示规格名称和值的维度数量不一致。
var ErrDimensionMismatch = errors.New("规格名称和值的维度数量不一致")

// ErrEmptyDimension 表示规格中存在空维度。
var ErrEmptyDimension = errors.New("规格维度不能为空")

// Normalized 保存已经清理空白并统一分隔符的规格元组。
type Normalized struct {
	// Name 是按原始顺序拼接的规格名称。
	Name string
	// Value 是按名称顺序拼接的规格值。
	Value string
	// Dimensions 是名称和值共同包含的维度数量。
	Dimensions int
}

// NormalizeColumns 校验并规范化规格名称和值，输出统一的中文分号格式。
func NormalizeColumns(name, value string) (Normalized, error) {
	// names、values 保存拆分并去除边界空白后的规格列。
	// names 保存规格名称拆分结果。
	names := split(name)
	// values 保存规格值拆分结果。
	values := split(value)
	if len(names) != len(values) {
		return Normalized{}, ErrDimensionMismatch
	}
	if len(names) == 0 || len(names) == 1 && names[0] == "" && values[0] == "" {
		return Normalized{}, nil
	}
	// index 表示当前规格维度位置。
	for index := range names {
		if names[index] == "" || values[index] == "" {
			return Normalized{}, ErrEmptyDimension
		}
	}
	return Normalized{Name: strings.Join(names, "；"), Value: strings.Join(values, "；"), Dimensions: len(names)}, nil
}

// Equal 比较两个规格元组的规范化结果。
func Equal(leftName, leftValue, rightName, rightValue string) bool {
	// left、right 保存两侧规格的规范化结果；对应错误值表示规格无效。
	// left、leftErr 保存左侧规格及其校验错误。
	left, leftErr := NormalizeColumns(leftName, leftValue)
	// right、rightErr 保存右侧规格及其校验错误。
	right, rightErr := NormalizeColumns(rightName, rightValue)
	return leftErr == nil && rightErr == nil && left.Name == right.Name && left.Value == right.Value
}

// split 按两种受支持的分号拆分单个规格列，并清理每个维度的边界空白。
func split(raw string) []string {
	raw = strings.NewReplacer(";", "；").Replace(strings.TrimSpace(raw))
	if raw == "" {
		return nil
	}
	// parts 保存拆分后的规格维度。
	parts := strings.Split(raw, "；")
	// index 表示当前规格维度的位置，用于清理维度边界空白。
	// index 表示当前规格维度位置。
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return parts
}
