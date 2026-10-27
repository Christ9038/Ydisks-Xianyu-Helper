package uploadroot

import (
	"path/filepath"
	"testing"
)

// TestDir 验证环境变量优先，空值或纯空白回退到默认相对目录。
func TestDir(t *testing.T) {
	t.Setenv("XIANYU_UPLOAD_DIR", " /srv/uploads ")
	if Dir() != "/srv/uploads" {
		t.Fatalf("Dir()=%q", Dir())
	}
	t.Setenv("XIANYU_UPLOAD_DIR", "  ")
	if Dir() != filepath.Join("data", "uploads") {
		t.Fatalf("默认目录异常: %q", Dir())
	}
}
