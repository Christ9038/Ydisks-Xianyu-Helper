package money

import "testing"

// TestParseYuanToCentsAcceptsStrictValues 验证合法金额在整数分单位下保持精确。
func TestParseYuanToCentsAcceptsStrictValues(t *testing.T) {
	// testCase 保存一组合法金额及其精确分值。
	for _, testCase := range []struct {
		input string
		want  int64
	}{
		{input: "0", want: 0},
		{input: "001.2", want: 120},
		{input: "19.90", want: 1990},
	} {
		// got 和 err 保存金额转换结果及格式错误。
		got, err := ParseYuanToCents(testCase.input)
		if err != nil || got != testCase.want {
			t.Fatalf("ParseYuanToCents(%q)=%d,%v，期望 %d,nil", testCase.input, got, err, testCase.want)
		}
	}
}

// TestParseYuanToCentsRejectsAmbiguousOrOverflowValues 验证危险格式和整数溢出不会进入改价请求。
func TestParseYuanToCentsRejectsAmbiguousOrOverflowValues(t *testing.T) {
	// input 是当前应被拒绝的危险金额文本。
	for _, input := range []string{"", "1.", ".1", "1.234", "+1", "1e2", "¥1", "1,000", "1 0", "92233720368547758.08"} {
		// got 和 err 保存拒绝结果及错误分类。
		if got, err := ParseYuanToCents(input); err == nil || got != 0 {
			t.Fatalf("ParseYuanToCents(%q)=%d,%v，期望拒绝", input, got, err)
		}
	}
}

// TestParseOrderAmountCentsExactValues 验证订单金额按整数分精确换算，含货币符号、千位逗号与第三位小数进位。
func TestParseOrderAmountCentsExactValues(t *testing.T) {
	// testCase 保存一组输入文本及期望分值。
	for _, testCase := range []struct {
		input string
		want  int64
	}{
		{"0", 0}, {"92.5", 9250}, {"92.50", 9250}, {"¥19.90", 1990}, {"￥ 1,234.56", 123456},
		{"0.10", 10}, {"0.30", 30}, {"1.004", 100}, {"1.005", 101}, {"1.995", 200}, {".5", 0},
		{"000012.3", 1230}, {"10000000", 1_000_000_000}, {"10000000.00", 1_000_000_000}, {"9999999.995", 1_000_000_000},
	} {
		// got 是换算得到的分值，err 是换算错误。
		got, err := ParseOrderAmountCents(testCase.input)
		if testCase.input == ".5" {
			if err == nil {
				t.Fatalf("%q 缺少整数部分应被拒绝", testCase.input)
			}
			continue
		}
		if err != nil || got != testCase.want {
			t.Fatalf("ParseOrderAmountCents(%q)=%d,%v，期望 %d", testCase.input, got, err, testCase.want)
		}
	}
}

// TestParseOrderAmountCentsRejectsInvalidAndOutOfRange 验证非法格式与超范围金额的错误分类，含 1 亿元和超长整数。
func TestParseOrderAmountCentsRejectsInvalidAndOutOfRange(t *testing.T) {
	// invalid 是格式非法的输入。
	for _, input := range []string{"", "  ", "¥", "1.", "-1", "+1", "1e8", "abc", "1.2.3", "1 0", "0x10", "Inf", "NaN"} {
		// got 是应为 0 的换算结果，err 是应为 ErrInvalidYuan 的错误。
		if got, err := ParseOrderAmountCents(input); err != ErrInvalidYuan || got != 0 {
			t.Fatalf("ParseOrderAmountCents(%q)=%d,%v，期望 ErrInvalidYuan", input, got, err)
		}
	}
	// outOfRange 是格式合法但超过上限的输入。
	for _, input := range []string{"10000000.01", "100000000", "100,000,000.00", "100000092.50", "9999999999999", "99999999999999999999999999", "10000000.005"} {
		// got 是应为 0 的换算结果，err 是应为 ErrAmountOutOfRange 的错误。
		if got, err := ParseOrderAmountCents(input); err != ErrAmountOutOfRange || got != 0 {
			t.Fatalf("ParseOrderAmountCents(%q)=%d,%v，期望 ErrAmountOutOfRange", input, got, err)
		}
	}
}
