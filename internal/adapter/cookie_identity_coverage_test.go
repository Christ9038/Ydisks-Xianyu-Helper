package adapter

import (
	"testing"

	"xianyu-go/internal/xianyu/cookierefresh"
)

// TestCookieSnapshotsFromResultMapsAndRejectsInputs 验证扫码结果快照转换的类型、空值和归一化路径。
func TestCookieSnapshotsFromResultMapsAndRejectsInputs(t *testing.T) {
	// missing、missingOK 保存缺少快照键时的结果。
	missing, missingOK := CookieSnapshotsFromResult(map[string]any{})
	if missing != nil || missingOK {
		t.Fatalf("缺少快照键结果异常 snapshots=%v ok=%v", missing, missingOK)
	}
	// wrongType、wrongTypeOK 保存快照字段类型错误时的结果。
	wrongType, wrongTypeOK := CookieSnapshotsFromResult(map[string]any{"cookie_snapshot": []any{}})
	if wrongType != nil || wrongTypeOK {
		t.Fatalf("错误快照类型结果异常 snapshots=%v ok=%v", wrongType, wrongTypeOK)
	}
	// nilSnapshot、nilSnapshotOK 保存显式 nil 快照的结果。
	nilSnapshot, nilSnapshotOK := CookieSnapshotsFromResult(map[string]any{"cookie_snapshot": []cookierefresh.BrowserCookie(nil)})
	if nilSnapshot != nil || nilSnapshotOK {
		t.Fatalf("nil 快照结果异常 snapshots=%v ok=%v", nilSnapshot, nilSnapshotOK)
	}
	// source 保存包含重复 Cookie 的平台快照，转换时应按平台规则归一化。
	source := []cookierefresh.BrowserCookie{
		{Name: "z", Value: "old", Domain: ".goofish.com", Path: "/"},
		{Name: "a", Value: "1", Domain: ".goofish.com", Path: "/"},
		{Name: "z", Value: "new", Domain: ".goofish.com", Path: "/"},
	}
	// converted、convertedOK 保存归一化后的应用层 Cookie 快照。
	converted, convertedOK := CookieSnapshotsFromResult(map[string]any{"cookie_snapshot": source})
	if !convertedOK || len(converted) != 3 || converted[0].Name != "z" || converted[2].Value != "new" {
		t.Fatalf("快照转换异常 snapshots=%+v ok=%v", converted, convertedOK)
	}
}
