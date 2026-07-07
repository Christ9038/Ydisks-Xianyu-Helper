package server

// item_publish_images.go: image zip extraction, image loading/validation, and safe path helpers for publish batches.

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

func extractPublishImagesZip(raw []byte, dest string) error {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return fmt.Errorf("解析图片 zip 失败: %w", err)
	}
	extractedFiles := 0
	extractedBytes := int64(0)
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		extractedFiles++
		if extractedFiles > maxItemPublishZipFiles {
			return fmt.Errorf("图片 zip 文件数不能超过 %d", maxItemPublishZipFiles)
		}
		rel, err := safeZipPath(f.Name)
		if err != nil {
			return err
		}
		root, err := os.OpenRoot(dest)
		if err != nil {
			return err
		}
		if err := root.MkdirAll(filepath.Dir(rel), 0o750); err != nil {
			_ = root.Close()
			return err
		}
		rc, err := f.Open()
		if err != nil {
			_ = root.Close()
			return err
		}
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
		file, err := root.OpenFile(rel, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err == nil {
			_, err = file.Write(data)
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

func loadBatchPublishImages(ctx context.Context, uploadDir string, row db.ItemPublishBatchRow) ([]mtop.PublishImage, error) {
	var refs []string
	if err := json.Unmarshal([]byte(row.ImagesJSON), &refs); err != nil {
		return nil, fmt.Errorf("图片字段格式错误")
	}
	if len(refs) == 0 {
		return nil, errors.New("至少上传 1 张商品图片")
	}
	images := make([]mtop.PublishImage, 0, len(refs))
	for _, ref := range refs {
		var data []byte
		var contentType string
		var filename string
		var err error
		if isHTTPURL(ref) {
			data, contentType, err = downloadImageURL(ctx, ref)
			filename = pathBaseFromURL(ref)
		} else {
			data, contentType, filename, err = readBatchImageFile(uploadDir, ref)
		}
		if err != nil {
			return nil, err
		}
		images = append(images, mtop.PublishImage{Filename: filename, ContentType: contentType, Data: data})
	}
	return images, nil
}

func readBatchImageFile(uploadDir, ref string) ([]byte, string, string, error) {
	rel, err := safeZipPath(ref)
	if err != nil {
		return nil, "", "", err
	}
	root, err := os.OpenRoot(uploadDir)
	if err != nil {
		return nil, "", "", fmt.Errorf("打开图片目录失败")
	}
	defer root.Close()
	file, err := root.Open(rel)
	if err != nil {
		return nil, "", "", fmt.Errorf("读取图片失败: %s", ref)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	if err != nil || len(data) > 10<<20 {
		return nil, "", "", fmt.Errorf("读取图片失败或文件过大: %s", ref)
	}
	if len(data) == 0 {
		return nil, "", "", fmt.Errorf("图片文件为空: %s", ref)
	}
	contentType := http.DetectContentType(data)
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", "", fmt.Errorf("不是有效图片: %s", ref)
	}
	return data, contentType, filepath.Base(rel), nil
}

// writeFileWithinRoot 将上传文件限制在指定根目录内，拒绝符号链接和路径逃逸。
func writeFileWithinRoot(rootDir, name string, data []byte) error {
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := root.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func downloadImageURL(ctx context.Context, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") || req.URL.Hostname() == "" {
		return nil, "", fmt.Errorf("图片 URL 无效: %s", rawURL)
	}
	client := publicHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("下载图片失败: %s", rawURL)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("下载图片失败: %s HTTP %d", rawURL, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (10<<20)+1))
	if err != nil {
		return nil, "", fmt.Errorf("读取远程图片失败: %s", rawURL)
	}
	if len(data) > 10<<20 {
		return nil, "", fmt.Errorf("远程图片不能超过 10 MiB: %s", rawURL)
	}
	contentType := resp.Header.Get("Content-Type")
	if i := strings.Index(contentType, ";"); i >= 0 {
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
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, resolved := range ips {
				if isPublicIP(resolved.IP) {
					return dialer.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
				}
			}
			return nil, fmt.Errorf("拒绝访问非公网地址")
		},
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("图片 URL 重定向次数过多")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("图片 URL 重定向协议不安全")
			}
			return nil
		},
	}
}

func isPublicIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !ip.IsMulticast()
}

func validateBatchImageRef(uploadDir, ref string) error {
	if isHTTPURL(ref) {
		return nil
	}
	rel, err := safeZipPath(ref)
	if err != nil {
		return err
	}
	path := filepath.Join(uploadDir, rel)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return fmt.Errorf("图片文件不存在: %s", ref)
	}
	return nil
}

func isHTTPURL(ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	return strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://")
}

func pathBaseFromURL(rawURL string) string {
	base := filepath.Base(strings.Split(rawURL, "?")[0])
	if base == "." || base == "/" || base == "" {
		exts, _ := mime.ExtensionsByType(http.DetectContentType(nil))
		if len(exts) > 0 {
			return "image" + exts[0]
		}
		return "image.jpg"
	}
	return base
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

func safeBaseName(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	name = strings.ReplaceAll(name, string(os.PathSeparator), "_")
	if name == "." || name == "/" {
		return ""
	}
	return name
}

func safeZipPath(raw string) (string, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" || strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("图片路径不安全: %s", raw)
	}
	clean := filepath.Clean(raw)
	if clean == "." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || clean == ".." {
		return "", fmt.Errorf("图片路径不安全: %s", raw)
	}
	return clean, nil
}

func splitImageRefs(raw string) []string {
	raw = strings.ReplaceAll(raw, "\n", ";")
	raw = strings.ReplaceAll(raw, "；", ";")
	parts := strings.Split(raw, ";")
	out := []string{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
