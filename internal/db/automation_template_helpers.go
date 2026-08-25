package db

import (
	"context"
	"encoding/json"
	"strconv"

	"xianyu-go/internal/deliverytemplate"
)

// loadTemplateAction 加载模板动作的变量绑定和有序消息，供自动化执行器使用。
func (a *AutomationRules) loadTemplateAction(ctx context.Context, action *AutomationAction) error {
	// rows 保存当前动作的变量绑定查询结果。
	// rows、err 保存模板绑定查询结果及错误。
	rows, err := a.DB.QueryContext(ctx, `SELECT b.variable_key,b.card_id,COALESCE(c.name,''),b.delivery_count FROM automation_action_template_bindings b LEFT JOIN cards c ON c.id=b.card_id WHERE b.action_id=? ORDER BY b.id`, action.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	action.TemplateBindings = make([]DeliveryTemplateBinding, 0)
	for rows.Next() {
		// binding 保存当前变量绑定。
		var binding DeliveryTemplateBinding
		// err 保存模板绑定行扫描错误。
		if err := rows.Scan(&binding.VariableKey, &binding.CardID, &binding.CardName, &binding.DeliveryCount); err != nil {
			return err
		}
		action.TemplateBindings = append(action.TemplateBindings, binding)
	}
	// err 保存模板绑定遍历错误。
	if err := rows.Err(); err != nil {
		return err
	}
	// messageRows 保存模板消息查询结果。
	// messageRows、err 保存模板消息查询结果及错误。
	messageRows, err := a.DB.QueryContext(ctx, `SELECT content FROM delivery_template_messages WHERE template_id=? ORDER BY sort_order ASC,id ASC`, action.DeliveryTemplateID)
	if err != nil {
		return err
	}
	defer messageRows.Close()
	// messages 保存模板消息正文。
	messages := make([]string, 0)
	for messageRows.Next() {
		// content 保存当前消息正文。
		var content string
		// err 保存模板消息行扫描错误。
		if err := messageRows.Scan(&content); err != nil {
			return err
		}
		messages = append(messages, content)
	}
	// err 保存模板消息遍历错误。
	if err := messageRows.Err(); err != nil {
		return err
	}
	// parsed 保存消息解析结果，避免自动化执行时重复解析。
	// parsed、err 保存模板消息解析结果及错误。
	parsed, err := deliverytemplate.Parse(messages)
	if err != nil {
		return err
	}
	action.TemplateMessages = parsed.Messages
	action.TemplateKeys = parsed.Keys
	// config 保存动作配置中的扩展字段原文。
	var config map[string]json.RawMessage
	// err 保存动作配置 JSON 解码错误。
	if err := json.Unmarshal([]byte(action.ConfigJSON), &config); err != nil {
		return err
	}
	// rawValues 保存自定义变量字段的 JSON 原文。
	rawValues := config["custom_variables"]
	// values 保存新格式的自定义变量键值表。
	var values map[string]string
	if json.Unmarshal(rawValues, &values) == nil && values != nil {
		action.CustomVariables = values
		return nil
	}
	// legacyValues 保存历史数组格式的自定义变量值。
	var legacyValues []string
	if json.Unmarshal(rawValues, &legacyValues) == nil {
		// converted 保存按历史数组下标转换得到的兼容键值表。
		converted := make(map[string]string, len(legacyValues))
		for /* index 表示历史数组下标；value 表示历史自定义字符串。 */ index, value := range legacyValues {
			converted[strconv.Itoa(index)] = value
		}
		action.CustomVariables = converted
	}
	return nil
}
