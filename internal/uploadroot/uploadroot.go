// Package uploadroot 解析服务端本地上传文件的根目录；批量铺货批次目录和资源库图片共用同一个位置。
package uploadroot

import (
	"os"
	"path/filepath"
	"strings"
)

// Dir 返回环境变量 XIANYU_UPLOAD_DIR 指定的上传根目录，未设置时回退到相对工作目录的 data/uploads。
// Docker、桌面服务和 systemd 都把工作目录或该环境变量指向已持久化的数据目录。
func Dir() string {
	// configured 是去除首尾空白后的环境变量值。
	if configured := strings.TrimSpace(os.Getenv("XIANYU_UPLOAD_DIR")); configured != "" {
		return configured
	}
	return filepath.Join("data", "uploads")
}
