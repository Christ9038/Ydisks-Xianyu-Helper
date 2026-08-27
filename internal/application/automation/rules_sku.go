package automation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"xianyu-go/internal/orderspec"
)

// normalizeRuleSKUConfigs 统一规则规格文本，并阻止多 SKU 规则保存不完整元组。
func normalizeRuleSKUConfigs(ctx context.Context, ownership RuleOwnership, userID int64, cookieID, itemID string, actions []ActionInput) error {
	// isMultiSpec 表示当前商品是否按多规格规则处理。
	isMultiSpec := false
	if itemID != "" {
		// provider、ok 保存商品规格事实读取能力及接口是否装配。
		if provider, ok := ownership.(interface {
			GetItemDeliveryProfile(context.Context, int64, string, string) (ItemDeliveryProfile, error)
		}); ok {
			// profile、err 保存商品规格事实及读取错误。
			profile, err := provider.GetItemDeliveryProfile(ctx, userID, cookieID, itemID)
			if err != nil {
				return err
			}
			isMultiSpec = profile.IsMultiSpec
		}
	}
	// expectedName 保存本规则已确认的规格名称顺序。
	var expectedName string
	// index 表示当前发货动作位置。
	for index := range actions {
		if actions[index].ActionType != ActionSendCard && actions[index].ActionType != ActionSendTemplate {
			continue
		}
		// config 保存当前动作配置对象。
		var config map[string]any
		// err 保存动作配置解析错误。
		if err := json.Unmarshal([]byte(actions[index].ConfigJSON), &config); err != nil {
			return errors.New("动作配置必须是 JSON 对象")
		}
		// name、value 保存当前动作规格列文本。
		name, _ := config["spec_name"].(string)
		// value 保存当前动作规格值列文本。
		value, _ := config["spec_value"].(string)
		if !isMultiSpec && strings.TrimSpace(name) != "" && strings.ContainsAny(name, ";；") {
			isMultiSpec = true
		}
		if !isMultiSpec {
			config["spec_name"], config["spec_value"] = "", ""
		} else {
			// normalized、err 保存规范化规格及校验错误。
			normalized, err := orderspec.NormalizeColumns(name, value)
			if err != nil || normalized.Dimensions == 0 {
				return errors.New("多规格商品的发货动作必须填写完整规格")
			}
			if expectedName != "" && expectedName != normalized.Name {
				return errors.New("同一规则的规格名称和顺序必须一致")
			}
			expectedName = normalized.Name
			config["spec_name"], config["spec_value"] = normalized.Name, normalized.Value
		}
		// encoded、err 保存规范化配置 JSON 及编码错误。
		encoded, err := json.Marshal(config)
		if err != nil {
			return err
		}
		actions[index].ConfigJSON = string(encoded)
	}
	return nil
}
