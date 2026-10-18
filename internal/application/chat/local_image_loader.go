package chat

import (
	"context"
)

// LocalImageLoader 把 accountID 账号素材目录内的 reference 相对路径读入内存。
// 返回图片字节、实际媒体类型、上传文件名与错误；实现负责沙箱、大小限制及取消，不长期缓存文件内容。
// 该端口由实际发货账号的图片发送器注入，与「商品默认回复」的本地图片读取共用同一实现。
type LocalImageLoader func(ctx context.Context, accountID, reference string) (data []byte, contentType, filename string, err error)
