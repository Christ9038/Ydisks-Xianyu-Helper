package browser

import (
	"testing"
)

// TestSanitize 特殊字符替换为下划线（用于 userDataDir 命名）。
func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"acc_1":      "acc_1",
		"acc/1:2 3":  "acc_1_2_3",
		`a\b:c d`:    "a_b_c_d",
		"":           "",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q)=%q want %q", in, got, want)
		}
	}
}

// TestGetCSSSelector 始终返回首选器（元素句柄仅占位，nil 与非 nil 同结果）。
func TestGetCSSSelector(t *testing.T) {
	selectors := []string{"#user", "input[name=user]"}
	if got := getCSSSelector(nil, selectors); got != "#user" {
		t.Errorf("应返回首选器，got %q", got)
	}
	// 单元素切片。
	if got := getCSSSelector(nil, []string{"#pwd"}); got != "#pwd" {
		t.Errorf("单元素应返回该项，got %q", got)
	}
}

// TestCalculateSlideDistance_Fallback nil 轨道/按钮时走兜底距离。
func TestCalculateSlideDistance_Fallback(t *testing.T) {
	// 无 scratch：220-259。
	dist, err := calculateSlideDistance(nil, nil, false)
	if err != nil || dist < 220 || dist > 259 {
		t.Fatalf("无 scratch 兜底应 220-259，got %v err=%v", dist, err)
	}
	// scratch：兜底 * 0.25-0.35 → 55-90。
	dist, err = calculateSlideDistance(nil, nil, true)
	if err != nil || dist < 55 || dist > 91 {
		t.Fatalf("scratch 兜底应 55-91，got %v err=%v", dist, err)
	}
}

