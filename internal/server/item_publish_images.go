package server

// item_publish_images.go: image zip extraction, image loading/validation, and safe path helpers for publish batches.

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/netguard"
)

// isPublicIP 保留给同包校验调用；实际策略统一由 netguard 维护。
func isPublicIP(ip net.IP) bool { return netguard.IsPublicIP(ip) }

// extractPublishImagesZip 负责extract发布ImagesZip相关处理。
func extractPublishImagesZip(raw []byte, dest string) error {
	// zr、err 保存zr、err，供当前处理流程使用
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return fmt.Errorf("解析图片 zip 失败: %w", err)
	}
	// extractedFiles 保存extracted文件列表，供当前处理流程使用
	extractedFiles := 0
	// extractedBytes 保存extractedBytes，供当前处理流程使用
	extractedBytes := int64(0)
	// f 表示当前遍历过程中的f
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		extractedFiles++
		if extractedFiles > maxItemPublishZipFiles {
			return fmt.Errorf("图片 zip 文件数不能超过 %d", maxItemPublishZipFiles)
		}
		// rel、err 保存rel、err，供当前处理流程使用
		rel, err := safeZipPath(f.Name)
		if err != nil {
			return err
		}
		// root、err 保存root、err，供当前处理流程使用
		root, err := os.OpenRoot(dest)
		if err != nil {
			return err
		}
		if // err 保存err，供当前处理流程使用
		err := root.MkdirAll(filepath.Dir(rel), 0o750); err != nil {
			_ = root.Close()
			return err
		}
		// rc、err 保存rc、err，供当前处理流程使用
		rc, err := f.Open()
		if err != nil {
			_ = root.Close()
			return err
		}
		// data、err 保存data、err，供当前处理流程使用
		data, err := io.ReadAll(io.LimitReader(rc, (10<<20)+1))
		_ = rc.Close()
		if err != nil {
			_ = root.Close()
			return err
		}
		if len(data) > 10<<20 {
			_ = root.Close()
			return fmt.Errorf("图片文件不能超过 10 MiB: %s", f.Name)
		}
		if len(data) == 0 {
			_ = root.Close()
			continue
		}
		if !strings.HasPrefix(http.DetectContentType(data), "image/") {
			_ = root.Close()
			continue
		}
		extractedBytes += int64(len(data))
		if extractedBytes > maxItemPublishZipExtractBytes {
			_ = root.Close()
			return fmt.Errorf("图片 zip 解压后总大小不能超过 %d MiB", maxItemPublishZipExtractBytes>>20)
		}
		// file、err 保存file、err，供当前处理流程使用
		file, err := root.OpenFile(rel, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err == nil {
			_, err = file.Write(data)
			// closeErr 保存closeErr，供当前处理流程使用
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
		}
		_ = root.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// readBatchImageFile 负责read批次图片文件相关处理。
func readBatchImageFile(uploadDir, ref string) ([]byte, string, string, error) {
	// rel、err 保存rel、err，供当前处理流程使用
	rel, err := safeZipPath(ref)
	if err != nil {
		return nil, "", "", err
	}
	// root、err 保存root、err，供当前处理流程使用
	root, err := os.OpenRoot(uploadDir)
	if err != nil {
		return nil, "", "", fmt.Errorf("打开图片目录失败")
	}
	defer root.Close()
	// file、err 保存file、err，供当前处理流程使用
	file, err := root.Open(rel)
	if err != nil {
		return nil, "", "", fmt.Errorf("读取图片失败: %s", ref)
	}
	defer file.Close()
	// data、err 保存data、err，供当前处理流程使用
	data, err := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	if err != nil || len(data) > 10<<20 {
		return nil, "", "", fmt.Errorf("读取图片失败或文件过大: %s", ref)
	}
	if len(data) == 0 {
		return nil, "", "", fmt.Errorf("图片文件为空: %s", ref)
	}
	// contentType 保存内容类型，供当前处理流程使用
	contentType := http.DetectContentType(data)
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", "", fmt.Errorf("不是有效图片: %s", ref)
	}
	return data, contentType, filepath.Base(rel), nil
}

// writeFileWithinRoot 将上传文件限制在指定根目录内，拒绝符号链接和路径逃逸。
func writeFileWithinRoot(rootDir, name string, data []byte) error {
	// root、err 保存root、err，供当前处理流程使用
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return err
	}
	defer root.Close()
	// file、err 保存file、err，供当前处理流程使用
	file, err := root.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if // err 保存err，供当前处理流程使用
	_, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// downloadImageURL 负责download图片URL相关处理。
func downloadImageURL(ctx context.Context, rawURL string) ([]byte, string, error) {
	// req、err 保存req、err，供当前处理流程使用
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") || req.URL.Hostname() == "" {
		return nil, "", fmt.Errorf("图片 URL 无效: %s", rawURL)
	}
	// client 保存client，供当前处理流程使用
	client := publicHTTPClient()
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("下载图片失败: %s", rawURL)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("下载图片失败: %s HTTP %d", rawURL, resp.StatusCode)
	}
	// data、err 保存data、err，供当前处理流程使用
	data, err := io.ReadAll(io.LimitReader(resp.Body, (10<<20)+1))
	if err != nil {
		return nil, "", fmt.Errorf("读取远程图片失败: %s", rawURL)
	}
	if len(data) > 10<<20 {
		return nil, "", fmt.Errorf("远程图片不能超过 10 MiB: %s", rawURL)
	}
	// contentType 保存内容类型，供当前处理流程使用
	contentType := resp.Header.Get("Content-Type")
	if // i 保存i，供当前处理流程使用
	i := strings.Index(contentType, ";"); i >= 0 {
		contentType = contentType[:i]
	}
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", fmt.Errorf("远程文件不是图片: %s", rawURL)
	}
	return data, contentType, nil
}

// publicHTTPClient 只允许连接公网地址，防止批量铺货图片 URL 访问本机或内网服务。
func publicHTTPClient() *http.Client {
	return netguard.PublicHTTPClient(30 * time.Second)
}

// validateBatchImageRef 负责validate批次图片Ref相关处理。
func validateBatchImageRef(uploadDir, ref string) error {
	if isHTTPURL(ref) {
		return nil
	}
	// rel、err 保存rel、err，供当前处理流程使用
	rel, err := safeZipPath(ref)
	if err != nil {
		return err
	}
	// path 保存路径，供当前处理流程使用
	path := filepath.Join(uploadDir, rel)
	// info、err 保存info、err，供当前处理流程使用
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return fmt.Errorf("图片文件不存在: %s", ref)
	}
	return nil
}

// isHTTPURL 负责isHTTPURL相关处理。
func isHTTPURL(ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	return strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://")
}

// pathBaseFromURL 负责路径BaseFromURL相关处理。
func pathBaseFromURL(rawURL string) string {
	// base 保存base，供当前处理流程使用
	base := filepath.Base(strings.Split(rawURL, "?")[0])
	if base == "." || base == "/" || base == "" {
		// exts 保存exts，供当前处理流程使用
		exts, _ := mime.ExtensionsByType(http.DetectContentType(nil))
		if len(exts) > 0 {
			return "image" + exts[0]
		}
		return "image.jpg"
	}
	return base
}

// randomHex 负责randomHex相关处理。
func randomHex(n int) string {
	// buf 保存buf，供当前处理流程使用
	buf := make([]byte, n)
	if // err 保存err，供当前处理流程使用
	_, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

// safeBaseName 负责safeBase名称相关处理。
func safeBaseName(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	name = strings.ReplaceAll(name, string(os.PathSeparator), "_")
	if name == "." || name == "/" {
		return ""
	}
	return name
}

// safeZipPath 负责safeZip路径相关处理。
func safeZipPath(raw string) (string, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" || strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("图片路径不安全: %s", raw)
	}
	// clean 保存clean，供当前处理流程使用
	clean := filepath.Clean(raw)
	if clean == "." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || clean == ".." {
		return "", fmt.Errorf("图片路径不安全: %s", raw)
	}
	return clean, nil
}

// splitImageRefs 负责split图片Refs相关处理。
func splitImageRefs(raw string) []string {
	raw = strings.ReplaceAll(raw, "\n", ";")
	raw = strings.ReplaceAll(raw, "；", ";")
	// parts 保存parts，供当前处理流程使用
	parts := strings.Split(raw, ";")
	// out 保存out，供当前处理流程使用
	out := []string{}
	// p 表示当前遍历过程中的p
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
