package renew

import (
	"testing"

	"xianyu-go/internal/xianyu/cookierefresh"
)

// TestLongLoginCookieStateCoversFlatAuthoritativeAndNilBoundaries 验证长登录 Cookie 状态的平面、完整 Jar 与空接收者路径。
func TestLongLoginCookieStateCoversFlatAuthoritativeAndNilBoundaries(t *testing.T) {
	// nilState 表示尚未创建的 Cookie 状态接收者。
	var nilState *longLoginCookieState
	if nilState.requestCookies("https://www.goofish.com/im") != "" {
		t.Fatal("空状态不应生成请求 Cookie")
	}
	nilState.apply("https://www.goofish.com/im", []string{"sid=1; Path=/"})
	nilState.populate(nil)

	// flatState 保存只有平面 Cookie 字符串的兼容状态。
	flatState := newLongLoginCookieState("unb=1", nil)
	// flatRequestCookies 保存平面状态生成的请求 Cookie。
	flatRequestCookies := flatState.requestCookies("https://www.goofish.com/im")
	if flatRequestCookies != "unb=1" {
		t.Fatalf("平面状态请求 Cookie=%q", flatRequestCookies)
	}
	flatState.apply("https://www.goofish.com/im", []string{"sid=2; Path=/"})
	// flatResult 保存平面状态填充后的对外结果。
	flatResult := &LongLoginSettings{}
	flatState.populate(flatResult)
	if flatResult.CookieSnapshotComplete || flatResult.NewCookies == "" {
		t.Fatalf("平面状态结果异常：%+v", flatResult)
	}

	// snapshot 保存完整浏览器 Cookie Jar，验证作用域请求与响应回放。
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "1", Domain: ".goofish.com", Path: "/"},
		{Name: "document_only", Value: "doc", Domain: "www.goofish.com", Path: "/im"},
	}
	// authoritativeState 保存完整 Jar 状态。
	authoritativeState := newLongLoginCookieState("flat-leak=ignored", [][]cookierefresh.BrowserCookie{snapshot})
	if !authoritativeState.authoritative {
		t.Fatal("提供快照后应进入权威 Jar 模式")
	}
	// authoritativeRequestCookies 保存完整 Jar 按作用域生成的请求 Cookie。
	authoritativeRequestCookies := authoritativeState.requestCookies("https://www.goofish.com/im")
	if authoritativeRequestCookies == "" || authoritativeRequestCookies == "flat-leak=ignored" {
		t.Fatalf("权威状态请求 Cookie=%q", authoritativeRequestCookies)
	}
	authoritativeState.apply(goofishIMDocumentURL, []string{"sid=2; Path=/"})
	// authoritativeResult 保存完整 Jar 状态填充后的对外结果。
	authoritativeResult := &LongLoginSettings{}
	authoritativeState.populate(authoritativeResult)
	if !authoritativeResult.CookieSnapshotComplete || len(authoritativeResult.CookieSnapshot) == 0 {
		t.Fatalf("权威状态结果异常：%+v", authoritativeResult)
	}

	// emptySnapshotState 验证空快照仍保持权威模式且输出非空快照切片。
	emptySnapshotState := newLongLoginCookieState("flat", [][]cookierefresh.BrowserCookie{nil})
	// emptySnapshotResult 保存空权威快照填充后的结果。
	emptySnapshotResult := &LongLoginSettings{}
	emptySnapshotState.populate(emptySnapshotResult)
	if !emptySnapshotResult.CookieSnapshotComplete || emptySnapshotResult.CookieSnapshot == nil {
		t.Fatalf("空权威快照结果异常：%+v", emptySnapshotResult)
	}
}

// TestFindMapChildCoversDepthAndNestedShapeBoundaries 验证长登录响应递归查找的深度和数据形状边界。
func TestFindMapChildCoversDepthAndNestedShapeBoundaries(t *testing.T) {
	// nested 保存多层响应结构中的目标 returnValue。
	nested := map[string]any{"outer": map[string]any{"inner": map[string]any{"returnValue": map[string]any{"ok": true}}}}
	// found、ok 表示递归查找的目标值与是否命中。
	found, ok := findMapChild(nested, "returnValue", 0)
	if !ok || found["ok"] != true {
		t.Fatalf("嵌套 returnValue 查找失败：value=%v ok=%v", found, ok)
	}
	// tooDeep、tooDeepOK 验证超过最大递归深度时拒绝继续遍历。
	tooDeep, tooDeepOK := findMapChild(map[string]any{"returnValue": map[string]any{}}, "returnValue", 7)
	if tooDeep != nil || tooDeepOK {
		t.Fatal("超过最大深度不应命中")
	}
	// malformed、malformedOK 验证目标字段不是对象时返回未命中。
	malformed, malformedOK := findMapChild(map[string]any{"returnValue": "text"}, "returnValue", 0)
	if malformed != nil || malformedOK {
		t.Fatal("非对象 returnValue 不应命中")
	}
}
