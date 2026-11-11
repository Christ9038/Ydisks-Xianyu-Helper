package automation

import (
	"strings"

	"xianyu-go/internal/db"
)

// resolveConsignProof 按账号确认发货凭证设置，从已投递凭证中得出本次确认发货请求实际提交的 tradeText/picList。
// delivered 是买家实际收到的内容，只读不改：运行协调器持久化的重发快照和人工补发仍使用完整内容，
// 这里只决定提交给平台确认发货接口的那一份。返回值只在本次 MTOP 调用期间存在，不得写入日志或任务快照。
func resolveConsignProof(setting db.ConsignProofSetting, delivered shipmentDeliveryProof) shipmentDeliveryProof {
	switch db.NormalizeConsignProofMode(setting.Mode) {
	case db.ConsignProofModeNone:
		// 不携带凭证：tradeText 为空，picList 由 MTOP 层序列化为空数组，与 591363ca 之前的请求一致。
		return shipmentDeliveryProof{}
	case db.ConsignProofModeFixed:
		// 固定文字只提交用户填写的文本，不附带卡密、模板全文或图片，避免触发平台敏感词审核。
		return shipmentDeliveryProof{tradeText: strings.TrimSpace(setting.FixedText)}
	default:
		// delivery 模式保持历史行为：原样提交已投递的文本与图片；复制切片避免与调用方共享底层数组。
		return shipmentDeliveryProof{tradeText: delivered.tradeText, picList: append([]string(nil), delivered.picList...)}
	}
}
