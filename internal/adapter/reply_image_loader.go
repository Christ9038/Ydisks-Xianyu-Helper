package adapter

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/gif"  // 注册本地 GIF 图片头校验。
	_ "image/jpeg" // 注册本地 JPEG 图片头校验。
	_ "image/png"  // 注册本地 PNG 图片头校验。
	"io"
	"os"
	"path/filepath"
	"strings"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/replyimage"
)

// localReplyImageLimit 限定单张本地回复图片的最大内存读取量，单位为字节。
const localReplyImageLimit = 10 << 20

// defaultReplyImageRoot 使用现有上传目录下的独立子目录，避免发布批次清理删除长期回复素材。
func defaultReplyImageRoot() string {
	// uploadRoot 固定本次服务构造使用的上传目录；未配置时与发布上传默认值保持一致。
	uploadRoot := strings.TrimSpace(os.Getenv("XIANYU_UPLOAD_DIR"))
	if uploadRoot == "" {
		uploadRoot = filepath.Join("data", "uploads")
	}
	return filepath.Join(uploadRoot, "reply-images")
}

// newLocalReplyImageLoader 固定由运维控制的 rootPath，不创建目录或缓存图片，每次发送独立打开并关闭文件。
// 返回端口按 accountID 建立第二层沙箱，使 reference 不能访问其他账号素材；不读取或记录凭证。
func newLocalReplyImageLoader(rootPath string) chatapp.LocalImageLoader {
	// ctx 控制当前读取的取消，accountID 定位账号目录，reference 只接受正斜杠相对文件路径。
	return func(ctx context.Context, accountID, reference string) ([]byte, string, string, error) {
		// cancelErr 在接触文件系统之前检查调用方是否已经取消。
		if cancelErr := ctx.Err(); cancelErr != nil {
			return nil, "", "", cancelErr
		}
		if accountID == "" || strings.ContainsAny(accountID, "/\\:") || reference == "" {
			return nil, "", "", errors.New("本地回复图片路径无效")
		}
		// accountErr 拒绝保留名称、点目录、控制字符及非相对的账号路径。
		if accountErr := replyimage.ValidatePath(accountID); accountErr != nil {
			return nil, "", "", errors.New("本地回复图片账号目录无效")
		}
		// referenceErr 使用配置保存时相同的纯路径边界，阻止历史脏数据绕过校验。
		if referenceErr := replyimage.ValidatePath(reference); referenceErr != nil {
			return nil, "", "", referenceErr
		}
		// root 与 rootErr 保存素材根目录句柄与稳定错误；原始路径不进入对外错误。
		root, rootErr := os.OpenRoot(rootPath)
		if rootErr != nil {
			return nil, "", "", errors.New("本地回复图片根目录不可用")
		}
		defer root.Close()
		if !hasExactReplyAccountDirectory(ctx, root, accountID) {
			// cancelErr 区分账号目录不存在和扫描过程中被取消，保持取消类别可判定。
			if cancelErr := ctx.Err(); cancelErr != nil {
				return nil, "", "", cancelErr
			}
			return nil, "", "", errors.New("本地回复图片账号目录不存在或名称不完全匹配")
		}
		// entry 与 entryErr 保留账号目录的文件身份，并拒绝账号目录本身的符号链接。
		entry, entryErr := root.Lstat(accountID)
		if entryErr != nil || !entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 {
			return nil, "", "", errors.New("本地回复图片账号目录不可用")
		}
		// accountRoot 与 accountErr 保存账号级受限目录句柄，不允许链接越过该边界。
		accountRoot, accountErr := root.OpenRoot(accountID)
		if accountErr != nil {
			return nil, "", "", errors.New("本地回复图片账号目录不可用")
		}
		defer accountRoot.Close()
		// opened 与 statErr 检查打开期间账号目录未被替换成其他账号目录。
		opened, statErr := accountRoot.Stat(".")
		if statErr != nil || !os.SameFile(entry, opened) {
			return nil, "", "", errors.New("本地回复图片账号目录已变化")
		}
		return readLocalReplyImage(ctx, accountRoot, reference)
	}
}

// hasExactReplyAccountDirectory 从 root 的真实目录项匹配 accountID，拒绝 Windows/macOS 的大小写或文件名别名。
// 分页读取只保留当前小批目录项，不将其他账号名称返回到应用层；目录句柄在调用结束时关闭。
// ctx 控制目录扫描取消，避免账号目录较多时继续处理已取消的回复。
func hasExactReplyAccountDirectory(ctx context.Context, root *os.Root, accountID string) bool {
	// directory 与 openErr 保存根目录的只读枚举句柄。
	directory, openErr := root.Open(".")
	if openErr != nil {
		return false
	}
	defer directory.Close()
	for {
		if ctx.Err() != nil {
			return false
		}
		// entries 与 readErr 保存当前最多 128 个目录项及枚举终止原因。
		entries, readErr := directory.ReadDir(128)
		// entry 是当前实际文件名，不使用可能已被文件系统归一化的查询参数。
		for _, entry := range entries {
			if entry.Name() == accountID {
				return entry.IsDir() && entry.Type()&os.ModeSymlink == 0
			}
		}
		if readErr != nil {
			return false
		}
	}
}

// readLocalReplyImage 在账号 root 沙箱内读取 reference，返回已校验的图片字节及上传元数据。
// 普通文件与大小检查在读取前后执行；错误只包含稳定原因，不包含宿主绝对路径或文件内容。
func readLocalReplyImage(ctx context.Context, root *os.Root, reference string) ([]byte, string, string, error) {
	// metadata 与 statErr 提前拒绝目录、命名管道和其他特殊文件，避免普通读取阻塞。
	metadata, statErr := root.Stat(reference)
	if statErr != nil || !metadata.Mode().IsRegular() {
		return nil, "", "", errors.New("本地回复图片不存在或不是普通文件")
	}
	if metadata.Size() <= 0 || metadata.Size() > localReplyImageLimit {
		return nil, "", "", errors.New("本地回复图片为空或超过 10 MiB")
	}
	// file 与 openErr 保存只读文件句柄与打开结果；Root 阻止相对路径和链接越过账号目录。
	file, openErr := root.OpenFile(reference, localReplyImageOpenFlags, 0)
	if openErr != nil {
		return nil, "", "", errors.New("本地回复图片读取失败")
	}
	defer file.Close()
	// opened 与 openedErr 在文件打开后再次检查身份，避免读取替换后的特殊文件。
	opened, openedErr := file.Stat()
	if openedErr != nil || !opened.Mode().IsRegular() || !os.SameFile(metadata, opened) {
		return nil, "", "", errors.New("本地回复图片文件已变化")
	}
	// data 与 readErr 保存限量且可取消的内存读取结果；额外一字节用于检测读取期间的文件增长。
	data, readErr := io.ReadAll(io.LimitReader(replyImageContextReader{ctx: ctx, reader: file}, localReplyImageLimit+1))
	if readErr != nil {
		// cancelErr 优先保留取消类别，其余文件错误不暴露路径。
		if cancelErr := ctx.Err(); cancelErr != nil {
			return nil, "", "", cancelErr
		}
		return nil, "", "", errors.New("本地回复图片读取失败")
	}
	if len(data) == 0 || len(data) > localReplyImageLimit {
		return nil, "", "", errors.New("本地回复图片为空或超过 10 MiB")
	}
	// config、format 与 decodeErr 按实际图片头验证格式和尺寸，不信任文件扩展名，也不解压全部像素。
	config, format, decodeErr := image.DecodeConfig(bytes.NewReader(data))
	if decodeErr != nil || config.Width <= 0 || config.Height <= 0 || (format != "png" && format != "jpeg" && format != "gif") {
		return nil, "", "", errors.New("本地回复图片必须为有效的 PNG、JPEG 或 GIF")
	}
	// cancelErr 让读取和图片头校验期间的取消也能阻止后续上传。
	if cancelErr := ctx.Err(); cancelErr != nil {
		return nil, "", "", cancelErr
	}
	return data, "image/" + format, filepath.Base(reference), nil
}

// replyImageContextReader 在每次文件读取前检查请求取消，不启动额外 goroutine 或持有长期句柄。
type replyImageContextReader struct {
	// ctx 是本次完整回复的调用方上下文。
	ctx context.Context
	// reader 是由外层函数负责关闭的已打开文件。
	reader io.Reader
}

// Read 在 r 的请求未取消时把字节读入 buffer，返回底层读取量与错误。
func (r replyImageContextReader) Read(buffer []byte) (int, error) {
	// cancelErr 保留调用方的取消或超时类别。
	if cancelErr := r.ctx.Err(); cancelErr != nil {
		return 0, cancelErr
	}
	return r.reader.Read(buffer)
}
