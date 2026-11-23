// Package money 提供不使用浮点数的金额解析能力。
package money

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// ErrInvalidYuan 表示金额文本不符合元到分的严格格式。
var ErrInvalidYuan = errors.New("金额格式无效")

// ParseYuanToCents 将最多两位小数的元金额转换为整数分，并在溢出前拒绝输入。
func ParseYuanToCents(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, ErrInvalidYuan
	}
	// wholeText 和 fracText 保存整数部分与小数部分文本。
	wholeText, fracText := raw, ""
	// dot 保存小数点在原始金额中的位置。
	if dot := strings.IndexByte(raw, '.'); dot >= 0 {
		wholeText, fracText = raw[:dot], raw[dot+1:]
	}
	if wholeText == "" || fracText == "" && strings.Contains(raw, ".") || len(fracText) > 2 {
		return 0, ErrInvalidYuan
	}
	// digit 是当前待校验的金额数字字符。
	for _, digit := range wholeText + fracText {
		if digit < '0' || digit > '9' {
			return 0, ErrInvalidYuan
		}
	}
	// whole 和 err 保存整数元解析结果及解析错误。
	whole, err := strconv.ParseInt(wholeText, 10, 64)
	if err != nil || whole < 0 || whole > math.MaxInt64/100 {
		return 0, ErrInvalidYuan
	}
	// frac 保存换算为分的小数部分。
	frac := int64(0)
	if fracText == "1" || len(fracText) == 1 {
		frac = int64(fracText[0]-'0') * 10
	} else if len(fracText) == 2 {
		frac = int64(fracText[0]-'0')*10 + int64(fracText[1]-'0')
	}
	// cents 保存最终整数分结果，并用于溢出检查。
	cents := whole*100 + frac
	if cents < 0 || (whole == math.MaxInt64/100 && cents < whole*100) {
		return 0, ErrInvalidYuan
	}
	return cents, nil
}

// MaxOrderAmountCents 是单笔订单金额的合理上限（1000 万元，单位：分）。
// 闲鱼商品价格不会达到该量级，超过它的金额只可能来自平台异常数据、错误解析或手工误填，必须拒绝而不是计入统计。
const MaxOrderAmountCents int64 = 1_000_000_000

// ErrAmountOutOfRange 表示订单金额格式合法但超出 MaxOrderAmountCents。
var ErrAmountOutOfRange = errors.New("订单金额超出合理范围")

// ParseOrderAmountCents 把订单金额文本换算为整数分，全程不使用浮点数。
// 允许可选的 ¥/￥ 前缀与千位逗号；小数位不限，第三位起按四舍五入进位到分；
// 格式非法返回 ErrInvalidYuan，超过 MaxOrderAmountCents 返回 ErrAmountOutOfRange（超长整数部分在转换前直接判定，不会溢出）。
func ParseOrderAmountCents(raw string) (int64, error) {
	// text 是去掉货币符号和千位逗号后的纯数字文本。
	text := strings.TrimSpace(raw)
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, "¥"), "￥"))
	text = strings.ReplaceAll(text, ",", "")
	// wholeText 和 fracText 保存整数元与小数部分文本。
	wholeText, fracText := text, ""
	// dot 是小数点位置，缺失为 -1。
	if dot := strings.IndexByte(text, '.'); dot >= 0 {
		wholeText, fracText = text[:dot], text[dot+1:]
		if fracText == "" {
			return 0, ErrInvalidYuan
		}
	}
	if wholeText == "" {
		return 0, ErrInvalidYuan
	}
	// digit 是当前待校验的数字字符。
	for _, digit := range wholeText + fracText {
		if digit < '0' || digit > '9' {
			return 0, ErrInvalidYuan
		}
	}
	// trimmedWhole 去掉前导零后用于长度判断，避免超长整数进入 ParseInt。
	trimmedWhole := strings.TrimLeft(wholeText, "0")
	if len(trimmedWhole) > 12 {
		return 0, ErrAmountOutOfRange
	}
	// whole 保存整数元；trimmedWhole 为空表示 0。
	whole := int64(0)
	if trimmedWhole != "" {
		// parsed 是整数元解析结果，err 是解析错误（长度已限制，正常不会溢出）。
		parsed, err := strconv.ParseInt(trimmedWhole, 10, 64)
		if err != nil {
			return 0, ErrInvalidYuan
		}
		whole = parsed
	}
	// padded 把小数补齐到至少三位，便于读取分位与进位位。
	padded := fracText + "000"
	// cents 是整数元换算的分加上小数前两位。
	cents := whole*100 + int64(padded[0]-'0')*10 + int64(padded[1]-'0')
	if padded[2] >= '5' {
		cents++
	}
	if cents > MaxOrderAmountCents {
		return 0, ErrAmountOutOfRange
	}
	return cents, nil
}
