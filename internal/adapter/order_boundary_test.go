package adapter

import (
	"errors"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
)

// TestNormalizeOrderError 验证数据库错误不会越过订单应用适配边界。
func TestNormalizeOrderError(t *testing.T) {
	// cases 保存数据库错误到应用错误的映射样例及空错误行为。
	cases := []struct {
		name  string
		input error
		want  error
	}{
		{name: "not found", input: db.ErrNotFound, want: orderapp.ErrNotFound},
		{name: "forbidden", input: db.ErrForbidden, want: orderapp.ErrForbidden},
		{name: "wrapped not found", input: errors.Join(errors.New("查询失败"), db.ErrNotFound), want: orderapp.ErrNotFound},
		{name: "other", input: errors.New("数据库暂时不可用"), want: nil},
		{name: "nil", input: nil, want: nil},
	}
	// testCase 表示当前错误映射样例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// got 保存适配器归一化后的错误。
			got := NormalizeOrderError(testCase.input)
			if testCase.want == nil {
				if got != testCase.input {
					t.Fatalf("error identity changed: got=%v want=%v", got, testCase.input)
				}
				return
			}
			if !errors.Is(got, testCase.want) {
				t.Fatalf("error=%v want=%v", got, testCase.want)
			}
		})
	}
}
