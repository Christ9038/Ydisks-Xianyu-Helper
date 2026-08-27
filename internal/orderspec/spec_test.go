package orderspec

import (
	"errors"
	"testing"
)

// TestNormalizeColumns 验证规格分隔符、空白和维度校验。
func TestNormalizeColumns(t *testing.T) {
	// cases 保存规格规范化测试用例集合。
	cases := []struct {
		name      string
		inputName string
		inputVal  string
		wantName  string
		wantVal   string
		wantCount int
		wantErr   error
	}{
		{name: "empty", wantCount: 0},
		{name: "single", inputName: " 颜色 ", inputVal: " 红色 ", wantName: "颜色", wantVal: "红色", wantCount: 1},
		{name: "mixed separators", inputName: "颜色; 尺码", inputVal: "红色； M", wantName: "颜色；尺码", wantVal: "红色；M", wantCount: 2},
		{name: "value punctuation", inputName: "套餐；版本", inputVal: "红,蓝；S/M:v1", wantName: "套餐；版本", wantVal: "红,蓝；S/M:v1", wantCount: 2},
		{name: "mismatch", inputName: "颜色；尺码", inputVal: "红色", wantErr: ErrDimensionMismatch},
		{name: "empty name dimension", inputName: "颜色；", inputVal: "红色；M", wantErr: ErrEmptyDimension},
		{name: "empty value dimension", inputName: "颜色；尺码", inputVal: "红色；", wantErr: ErrEmptyDimension},
	}
	for /* testCase 表示当前规格规范化测试场景。 */ _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// got、err 保存规范化结果及校验错误。
			got, err := NormalizeColumns(testCase.inputName, testCase.inputVal)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("err=%v want=%v", err, testCase.wantErr)
			}
			if testCase.wantErr == nil && (got.Name != testCase.wantName || got.Value != testCase.wantVal || got.Dimensions != testCase.wantCount) {
				t.Fatalf("got=%+v", got)
			}
		})
	}
}

// TestEqual 验证规范化后的完整元组比较。
func TestEqual(t *testing.T) {
	if !Equal("颜色; 尺码", "红色；M", "颜色；尺码", "红色；M") {
		t.Fatal("等价规格应匹配")
	}
	if Equal("颜色；尺码", "红色；M", "尺码；颜色", "M；红色") {
		t.Fatal("维度顺序不同不应匹配")
	}
	if Equal("颜色", "红色", "颜色；尺码", "红色；M") {
		t.Fatal("维度数量不同不应匹配")
	}
}
