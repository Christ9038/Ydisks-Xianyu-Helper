package adapter

import "testing"

// TestAccountRunningLookupRejectsMissingManager 验证账号在线查询适配器在缺少管理器时安全返回离线。
func TestAccountRunningLookupRejectsMissingManager(t *testing.T) {
	// lookup 保存未装配管理器时的在线查询函数。
	lookup := AccountRunningLookup(nil)
	if lookup("account") {
		t.Fatal("missing manager must report account offline")
	}
}
