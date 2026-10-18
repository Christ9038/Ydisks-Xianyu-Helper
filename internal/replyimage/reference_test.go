package replyimage

import (
	"strings"
	"testing"
)

// TestValidatePath 验证图片引用仅接受账号专用目录内的相对路径，空值表示不配置图片。
func TestValidatePath(t *testing.T) {
	// valid 是可跨平台解析且不会越过根目录的图片引用。
	for _, valid := range []string{"", "hello.png", "商品/介绍 图.jpg"} {
		if err := ValidatePath(valid); err != nil { // err 是合法引用被拒绝的原因。
			t.Errorf("合法引用被拒绝: %q: %v", valid, err)
		}
	}
	// invalid 覆盖绝对路径、目录穿越、Windows 盘符、分隔符和控制字符。
	for _, invalid := range []string{"/a.png", "../a.png", "a/../b.png", "a//b.png", "./a.png", ".", "a/", `C:\a.png`, `a\b.png`, "a:b.png", "a\x00.png", "a\n.png", " a.png", strings.Repeat("a", 1025)} {
		if err := ValidatePath(invalid); err == nil { // err 应说明引用不满足沙箱格式要求。
			t.Errorf("非法引用被接受: %q", invalid)
		}
	}
	if err := ValidateSource("https://example.test/a.png", "a.png"); err == nil { // err 应拒绝两种图片来源同时存在。
		t.Fatal("URL 与本地图不能同时配置")
	}
	if err := ValidateSource("https://example.test/a.png", ""); err != nil { // err 不应改变旧 URL 配置行为。
		t.Fatal(err)
	}
}
