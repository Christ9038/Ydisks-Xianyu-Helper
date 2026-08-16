package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"xianyu-go/internal/xianyu/mtop"
)

// ReadPublishImageFile 是批量发布图片本地读取回调；实现方负责路径安全和图片格式校验。
type ReadPublishImageFile func(uploadDir, ref string) ([]byte, string, string, error)

// DownloadPublishImageURL 是批量发布远程图片下载回调；实现方负责 URL 安全、超时和响应大小限制。
type DownloadPublishImageURL func(context.Context, string) ([]byte, string, error)

// LoadBatchPublishImages 解析批次图片引用并转换为平台发布图片模型。
// 文件读取和远程下载通过回调注入，避免应用/适配器层重复拥有网络或文件安全策略。
func LoadBatchPublishImages(ctx context.Context, uploadDir, imagesJSON string, readLocal ReadPublishImageFile, downloadRemote DownloadPublishImageURL) ([]mtop.PublishImage, error) {
	if readLocal == nil || downloadRemote == nil {
		return nil, errors.New("批量发布图片读取端口未初始化")
	}
	// refs 保存持久化图片 JSON 中的本地路径或远程 URL 引用。
	var refs []string
	// err 表示持久化图片引用 JSON 无法解析，调用方应将其作为批次数据损坏处理。
	if err := json.Unmarshal([]byte(imagesJSON), &refs); err != nil {
		return nil, fmt.Errorf("图片字段格式错误")
	}
	if len(refs) == 0 {
		return nil, errors.New("至少上传 1 张商品图片")
	}
	// images 保存经过读取回调校验、可交给平台发布端口的图片数据。
	images := make([]mtop.PublishImage, 0, len(refs))
	// ref 表示当前遍历的图片引用。
	for _, ref := range refs {
		// data、contentType 和 filename 保存当前图片的内容、媒体类型及展示文件名。
		var data []byte
		// contentType 保存当前图片的媒体类型。
		var contentType string
		// filename 保存当前图片交给平台时使用的文件名。
		var filename string
		// err 保存当前图片读取或下载失败的原因。
		var err error
		if isPublishImageHTTPURL(ref) {
			data, contentType, err = downloadRemote(ctx, ref)
			filename = publishImagePathBase(ref)
		} else {
			data, contentType, filename, err = readLocal(uploadDir, ref)
		}
		if err != nil {
			return nil, err
		}
		images = append(images, mtop.PublishImage{Filename: filename, ContentType: contentType, Data: data})
	}
	return images, nil
}

// isPublishImageHTTPURL 判断图片引用是否为远程 HTTP(S) 地址；具体网络访问由调用方回调执行。
func isPublishImageHTTPURL(ref string) bool {
	ref = strings.TrimSpace(strings.ToLower(ref))
	return strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://")
}

// publishImagePathBase 从远程 URL 提取稳定文件名，缺少路径名时使用通用图片名。
func publishImagePathBase(rawURL string) string {
	// base 保存从 URL 路径提取出的文件名；空路径回退到通用图片名。
	base := filepath.Base(strings.Split(rawURL, "?")[0])
	if base == "." || base == "/" || base == "" {
		return "image.jpg"
	}
	return base
}
