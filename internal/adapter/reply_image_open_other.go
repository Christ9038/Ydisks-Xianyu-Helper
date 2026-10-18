//go:build !unix

package adapter

import "os"

// localReplyImageOpenFlags 在非 Unix 平台只读打开受沙箱保护的普通文件；路径校验拒绝设备及管道名称。
const localReplyImageOpenFlags = os.O_RDONLY
