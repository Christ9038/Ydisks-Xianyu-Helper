package account

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// 确认发货凭证模式与数据库取值一致；应用层单独声明以免依赖 internal/db。
const (
	// ConsignProofModeDelivery 表示确认发货时携带买家实际收到的卡密或发货模板全文及图片（历史默认行为）。
	ConsignProofModeDelivery = "delivery"
	// ConsignProofModeFixed 表示确认发货时只携带用户填写的固定文字。
	ConsignProofModeFixed = "fixed"
	// ConsignProofModeNone 表示确认发货时不携带任何凭证。
	ConsignProofModeNone = "none"
	// ConsignProofTextMaxRunes 是固定文字的最大字符数（按 Unicode 字符计）；与 MySQL VARCHAR(255) 列保持余量。
	ConsignProofTextMaxRunes = 200
)

// ErrInvalidConsignProof 是确认发货凭证设置非法时的哨兵错误，HTTP 层统一映射为 400。
var ErrInvalidConsignProof = errors.New("确认发货凭证设置无效")

// validateConsignProofInput 校验本次请求中显式提供的凭证模式与固定文字。
// 输入只在本次请求中存在；跨字段的最终状态（固定文字模式必须有文字）由数据库事务按加锁读取的当前值复核。
func validateConsignProofInput(mode, text *string) error {
	if mode != nil {
		switch *mode {
		case ConsignProofModeDelivery, ConsignProofModeFixed, ConsignProofModeNone:
		default:
			return fmt.Errorf("%w: 未知的凭证模式 %q", ErrInvalidConsignProof, *mode)
		}
	}
	if text != nil && utf8.RuneCountInString(*text) > ConsignProofTextMaxRunes {
		return fmt.Errorf("%w: 固定文字不能超过 %d 个字符", ErrInvalidConsignProof, ConsignProofTextMaxRunes)
	}
	if mode != nil && *mode == ConsignProofModeFixed && text != nil && strings.TrimSpace(*text) == "" {
		return fmt.Errorf("%w: 选择固定文字时，固定文字不能为空", ErrInvalidConsignProof)
	}
	return nil
}
