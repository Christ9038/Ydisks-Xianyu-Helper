package library

import (
	"archive/zip"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
)

const (
	// maxImportArchiveFiles 是图片压缩包内允许的文件数上限，与批量铺货图片包一致。
	maxImportArchiveFiles = 500
	// maxImportArchiveExtractBytes 是压缩包声明的解压总量上限，防止压缩炸弹，与批量铺货图片包一致。
	maxImportArchiveExtractBytes = 500 << 20
)

// importArchive 是导入用图片压缩包的只读索引：按规范化路径定位条目，读取时再按单张上限截断。
// 它只持有调用方提供的 ReaderAt，不负责关闭；同一实例只在单个请求内顺序使用，不跨 goroutine 共享。
type importArchive struct {
	// entries 是规范化相对路径到压缩包条目的映射，只登记普通文件。
	entries map[string]*zip.File
}

// newImportArchive 解析压缩包目录并建立索引；source 与 size 描述压缩包字节。
// 拒绝路径穿越、文件数超限、单个文件声明超过 10 MiB 或解压总量超限的压缩包；空目录项会被忽略。
func newImportArchive(source io.ReaderAt, size int64) (*importArchive, error) {
	// reader、err 保存压缩包目录读取结果。
	reader, err := zip.NewReader(source, size)
	if err != nil {
		return nil, invalid("解析图片 zip 失败，请确认文件是有效的 zip 压缩包")
	}
	// archive 是待返回的索引。
	archive := &importArchive{entries: make(map[string]*zip.File, len(reader.File))}
	// declared 是全部条目声明的解压字节总数。
	declared := uint64(0)
	// file 是当前压缩包条目。
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if len(archive.entries) >= maxImportArchiveFiles {
			return nil, invalid(fmt.Sprintf("图片 zip 文件数不能超过 %d", maxImportArchiveFiles))
		}
		// name、nameErr 保存规范化后的条目路径及安全校验错误。
		name, nameErr := normalizeArchivePath(file.Name)
		if nameErr != nil {
			return nil, nameErr
		}
		if file.UncompressedSize64 > MaxImageBytes {
			return nil, invalid(fmt.Sprintf("图片文件不能超过 10 MiB: %s", name))
		}
		declared += file.UncompressedSize64
		if declared > maxImportArchiveExtractBytes {
			return nil, invalid(fmt.Sprintf("图片 zip 解压后总大小不能超过 %d MiB", maxImportArchiveExtractBytes>>20))
		}
		archive.entries[name] = file
	}
	return archive, nil
}

// has 判断压缩包是否包含表格引用的图片路径；reference 是表格中的原始路径。
func (archive *importArchive) has(reference string) bool {
	if archive == nil {
		return false
	}
	// name、err 保存规范化路径及安全校验错误。
	name, err := normalizeArchivePath(reference)
	return err == nil && archive.entries[name] != nil
}

// read 读取表格引用的图片字节；最多读取 10 MiB 加一字节，超出部分交给图片库上传校验拒绝，
// 防止条目实际解压量超过目录声明。
func (archive *importArchive) read(reference string) ([]byte, error) {
	// name、err 保存规范化路径及安全校验错误。
	name, err := normalizeArchivePath(reference)
	if err != nil {
		return nil, err
	}
	if archive == nil || archive.entries[name] == nil {
		return nil, invalid("图片不在压缩包中: " + name)
	}
	// file 是引用对应的压缩包条目。
	file := archive.entries[name]
	// opened、openErr 保存条目读取流及打开错误。
	opened, openErr := file.Open()
	if openErr != nil {
		return nil, invalid("读取压缩包图片失败: " + name)
	}
	defer opened.Close()
	// data、readErr 保存条目字节及读取错误。
	data, readErr := io.ReadAll(io.LimitReader(opened, MaxImageBytes+1))
	if readErr != nil {
		return nil, invalid("读取压缩包图片失败: " + name)
	}
	return data, nil
}

// normalizeArchivePath 把压缩包条目名或表格图片路径规范为不含 .. 的相对路径，规则与批量铺货图片包一致：
// 统一斜杠、去除首尾空白、拒绝绝对路径和越出根目录的路径。
func normalizeArchivePath(raw string) (string, error) {
	// value 是统一分隔符后的路径文本。
	value := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if value == "" || strings.HasPrefix(value, "/") {
		return "", invalid("图片路径不安全: " + value)
	}
	// clean 是规范化后的路径。
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", invalid("图片路径不安全: " + value)
	}
	return clean, nil
}

// isRemoteImageReference 判断表格图片引用是否以 http/https 开头，即应按网址下载而不是在压缩包中查找。
func isRemoteImageReference(reference string) bool {
	// lower 是小写化的引用文本。
	lower := strings.ToLower(strings.TrimSpace(reference))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// validRemoteImageURL 判断网址引用能否被解析为带主机名的 http/https 地址；是否为公网地址由下载端口在连接时校验。
func validRemoteImageURL(reference string) bool {
	// parsed、err 保存网址解析结果。
	parsed, err := url.Parse(strings.TrimSpace(reference))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != ""
}
