//go:build unix

package adapter

import (
	"os"
	"syscall"
)

// localReplyImageOpenFlags 让 Unix 上被并发替换为 FIFO 的图片路径也不会卡住打开操作。
// 已打开句柄仍必须通过普通文件和身份校验，非阻塞标志不放宽读取权限。
const localReplyImageOpenFlags = os.O_RDONLY | syscall.O_NONBLOCK
