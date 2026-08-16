package server

import (
	"context"

	"xianyu-go/internal/adapter"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// loadBatchPublishImages 保留 Server 测试的历史调用形状，生产路径直接使用 adapter.LoadBatchPublishImages。
func loadBatchPublishImages(ctx context.Context, uploadDir string, row db.ItemPublishBatchRow) ([]mtop.PublishImage, error) {
	return adapter.LoadBatchPublishImages(ctx, uploadDir, row.ImagesJSON, readBatchImageFile, downloadImageURL)
}
