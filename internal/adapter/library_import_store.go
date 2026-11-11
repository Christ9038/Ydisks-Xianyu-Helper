package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	libraryapp "xianyu-go/internal/application/library"
)

// libraryImportsDirName 是上传根目录下存放 Excel 导入暂存的子目录，与 library、publish_batches 同级。
const libraryImportsDirName = "library_imports"

const (
	// libraryImportSheetFile 是暂存表格文件名；原始文件名只记录在清单中，不参与路径。
	libraryImportSheetFile = "sheet.bin"
	// libraryImportZipFile 是暂存图片压缩包文件名。
	libraryImportZipFile = "images.zip"
	// libraryImportManifestFile 是进度清单文件名。
	libraryImportManifestFile = "manifest.json"
)

// libraryImportIDPattern 约束导入任务标识，防止拼出越界目录。
var libraryImportIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// LibraryImportStore 把 Excel 导入暂存保存在 <上传根目录>/library_imports/<导入ID>/ 下。
// 全部读写通过 os.Root 完成，符号链接和 .. 都无法逃出导入目录；清单先写临时文件再重命名，读取不会看到半截内容。
// 它没有可变状态，可被多个 goroutine 并发使用；同一任务的并发写由导入服务的 busy 表串行化。
type LibraryImportStore struct {
	// uploadRoot 是上传根目录，来自 uploadroot.Dir，可为相对路径。
	uploadRoot string
}

// libraryImportManifestJSON 是进度清单的磁盘格式，与应用模型分离以便演进。
type libraryImportManifestJSON struct {
	// ImportID 是导入任务标识。
	ImportID string `json:"import_id"`
	// UserID 是任务所属用户。
	UserID int64 `json:"user_id"`
	// SheetName 是原始表格文件名。
	SheetName string `json:"sheet_name"`
	// HasZip 表示是否附带图片压缩包。
	HasZip bool `json:"has_zip"`
	// CreatedAt 是任务创建时间。
	CreatedAt time.Time `json:"created_at"`
	// Rows 是逐行进度。
	Rows []libraryImportManifestRowJSON `json:"rows"`
}

// libraryImportManifestRowJSON 是清单中一行的磁盘格式，不包含变量取值或图片内容。
type libraryImportManifestRowJSON struct {
	// RowNo 是表格行号。
	RowNo int `json:"row_no"`
	// Status 是行状态。
	Status string `json:"status"`
	// TemplateID 是已创建模板主键。
	TemplateID int64 `json:"template_id,omitempty"`
	// Errors 是失败原因。
	Errors []string `json:"errors,omitempty"`
}

// NewLibraryImportStore 创建以 uploadRoot 为上传根目录的导入暂存存储；构造本身不做 I/O。
func NewLibraryImportStore(uploadRoot string) (*LibraryImportStore, error) {
	if strings.TrimSpace(uploadRoot) == "" {
		return nil, errors.New("上传根目录不能为空")
	}
	return &LibraryImportStore{uploadRoot: uploadRoot}, nil
}

// importsRoot 打开（必要时创建）导入暂存根目录。
func (s *LibraryImportStore) importsRoot() (*os.Root, error) {
	// dir 是导入暂存根目录路径。
	dir := filepath.Join(s.uploadRoot, libraryImportsDirName)
	// err 保存 MkdirAll 调用的失败原因。
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("创建导入暂存目录失败: %w", err)
	}
	return os.OpenRoot(dir)
}

// taskRoot 打开指定导入任务目录；目录不存在时返回 ErrImportNotFound。
func (s *LibraryImportStore) taskRoot(importID string) (*os.Root, error) {
	if !libraryImportIDPattern.MatchString(importID) {
		return nil, libraryapp.ErrImportNotFound
	}
	// root、err 保存导入暂存根目录句柄。
	root, err := s.importsRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	// task、openErr 保存任务目录句柄。
	task, openErr := root.OpenRoot(importID)
	if errors.Is(openErr, fs.ErrNotExist) {
		return nil, libraryapp.ErrImportNotFound
	}
	return task, openErr
}

// Create 建立任务目录并写入表格与可选压缩包。
func (s *LibraryImportStore) Create(_ context.Context, importID string, sheet, zip []byte) error {
	if !libraryImportIDPattern.MatchString(importID) {
		return errors.New("导入任务标识无效")
	}
	// root、err 保存导入暂存根目录句柄。
	root, err := s.importsRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	// mkdirErr 保存任务目录创建错误；目录已存在说明标识冲突。
	if mkdirErr := root.Mkdir(importID, 0o750); mkdirErr != nil {
		return fmt.Errorf("创建导入任务目录失败: %w", mkdirErr)
	}
	// files 是待写入的暂存文件；没有压缩包时不写 images.zip。
	files := []struct {
		name string
		data []byte
	}{{libraryImportSheetFile, sheet}}
	if len(zip) > 0 {
		files = append(files, struct {
			name string
			data []byte
		}{libraryImportZipFile, zip})
	}
	// file 是当前待写入的暂存文件。
	for _, file := range files {
		// writeErr 保存文件写入错误，只会在磁盘写满等环境故障时发生。
		if writeErr := root.WriteFile(filepath.Join(importID, file.name), file.data, 0o600); writeErr != nil {
			return fmt.Errorf("保存导入暂存失败: %w", writeErr)
		}
	}
	return nil
}

// ReadSheet 读取暂存表格。
func (s *LibraryImportStore) ReadSheet(_ context.Context, importID string) ([]byte, error) {
	// task、err 保存任务目录句柄。
	task, err := s.taskRoot(importID)
	if err != nil {
		return nil, err
	}
	defer task.Close()
	// data、readErr 保存表格字节。
	data, readErr := task.ReadFile(libraryImportSheetFile)
	if errors.Is(readErr, fs.ErrNotExist) {
		return nil, libraryapp.ErrImportNotFound
	}
	return data, readErr
}

// OpenZip 打开暂存压缩包；任务没有压缩包时返回 nil 句柄与 0。
func (s *LibraryImportStore) OpenZip(_ context.Context, importID string) (libraryapp.ImportArchiveFile, int64, error) {
	// task、err 保存任务目录句柄。
	task, err := s.taskRoot(importID)
	if err != nil {
		return nil, 0, err
	}
	defer task.Close()
	// file、openErr 保存压缩包文件句柄；关闭任务目录句柄不影响已打开的文件。
	file, openErr := task.Open(libraryImportZipFile)
	if errors.Is(openErr, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if openErr != nil {
		return nil, 0, openErr
	}
	// info、statErr 保存压缩包大小。
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, 0, statErr
	}
	return file, info.Size(), nil
}

// SaveManifest 原子写入进度清单：先写同目录临时文件再重命名。
func (s *LibraryImportStore) SaveManifest(_ context.Context, manifest libraryapp.ImportManifest) error {
	// task、err 保存任务目录句柄。
	task, err := s.taskRoot(manifest.ImportID)
	if err != nil {
		return err
	}
	defer task.Close()
	// encoded 是清单的磁盘格式。
	encoded := libraryImportManifestJSON{ImportID: manifest.ImportID, UserID: manifest.UserID, SheetName: manifest.SheetName, HasZip: manifest.HasZip, CreatedAt: manifest.CreatedAt}
	// row 是应用层清单行。
	for _, row := range manifest.Rows {
		encoded.Rows = append(encoded.Rows, libraryImportManifestRowJSON{RowNo: row.RowNo, Status: row.Status, TemplateID: row.TemplateID, Errors: row.Errors})
	}
	// data 是序列化结果；清单只含字符串、整数、布尔与时间，json.Marshal 不会失败。
	data, _ := json.Marshal(encoded)
	// temp 是同目录临时文件名。
	temp := libraryImportManifestFile + ".tmp"
	// writeErr 保存临时文件写入错误。
	if writeErr := task.WriteFile(temp, data, 0o600); writeErr != nil {
		return fmt.Errorf("保存导入进度失败: %w", writeErr)
	}
	// renameErr 保存重命名错误。
	if renameErr := task.Rename(temp, libraryImportManifestFile); renameErr != nil {
		_ = task.Remove(temp)
		return fmt.Errorf("保存导入进度失败: %w", renameErr)
	}
	return nil
}

// LoadManifest 读取进度清单。
func (s *LibraryImportStore) LoadManifest(_ context.Context, importID string) (libraryapp.ImportManifest, error) {
	// task、err 保存任务目录句柄。
	task, err := s.taskRoot(importID)
	if err != nil {
		return libraryapp.ImportManifest{}, err
	}
	defer task.Close()
	return readLibraryImportManifest(task)
}

// readLibraryImportManifest 从任务目录读取并解码清单；文件缺失视为任务不存在。
func readLibraryImportManifest(task *os.Root) (libraryapp.ImportManifest, error) {
	// data、readErr 保存清单字节。
	data, readErr := task.ReadFile(libraryImportManifestFile)
	if errors.Is(readErr, fs.ErrNotExist) {
		return libraryapp.ImportManifest{}, libraryapp.ErrImportNotFound
	}
	if readErr != nil {
		return libraryapp.ImportManifest{}, readErr
	}
	// decoded 保存磁盘格式的清单。
	var decoded libraryImportManifestJSON
	// decodeErr 保存解码错误。
	if decodeErr := json.Unmarshal(data, &decoded); decodeErr != nil {
		return libraryapp.ImportManifest{}, fmt.Errorf("导入进度已损坏: %w", decodeErr)
	}
	// manifest 是应用模型。
	manifest := libraryapp.ImportManifest{ImportID: decoded.ImportID, UserID: decoded.UserID, SheetName: decoded.SheetName, HasZip: decoded.HasZip, CreatedAt: decoded.CreatedAt}
	// row 是磁盘格式的清单行。
	for _, row := range decoded.Rows {
		manifest.Rows = append(manifest.Rows, libraryapp.ImportManifestRow{RowNo: row.RowNo, Status: row.Status, TemplateID: row.TemplateID, Errors: row.Errors})
	}
	return manifest, nil
}

// Remove 删除导入任务目录；目录不存在视为成功。
func (s *LibraryImportStore) Remove(_ context.Context, importID string) error {
	if !libraryImportIDPattern.MatchString(importID) {
		return nil
	}
	// root、err 保存导入暂存根目录句柄。
	root, err := s.importsRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveAll(importID)
}

// CleanupBefore 删除创建时间早于 cutoff 的导入任务；清单缺失或损坏时按目录修改时间判断。
func (s *LibraryImportStore) CleanupBefore(_ context.Context, cutoff time.Time) error {
	// root、err 保存导入暂存根目录句柄。
	root, err := s.importsRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	// entries、readErr 保存全部任务目录。
	entries, readErr := fs.ReadDir(root.FS(), ".")
	if readErr != nil {
		return readErr
	}
	// cleanupErrs 汇总单个目录的删除错误，不因一个失败中断其他清理。
	var cleanupErrs []error
	// entry 是当前任务目录。
	for _, entry := range entries {
		if !entry.IsDir() || !libraryImportIDPattern.MatchString(entry.Name()) {
			continue
		}
		// createdAt 是任务创建时间。
		createdAt, known := libraryImportCreatedAt(root, entry)
		if !known || !createdAt.Before(cutoff) {
			continue
		}
		cleanupErrs = append(cleanupErrs, root.RemoveAll(entry.Name()))
	}
	return errors.Join(cleanupErrs...)
}

// libraryImportCreatedAt 返回任务创建时间：优先读清单，其次取目录修改时间；都无法读取时返回 false。
func libraryImportCreatedAt(root *os.Root, entry fs.DirEntry) (time.Time, bool) {
	// task、err 保存任务目录句柄。
	task, err := root.OpenRoot(entry.Name())
	if err == nil {
		defer task.Close()
		// manifest、manifestErr 保存清单读取结果。
		if manifest, manifestErr := readLibraryImportManifest(task); manifestErr == nil && !manifest.CreatedAt.IsZero() {
			return manifest.CreatedAt, true
		}
	}
	// info、infoErr 保存目录属性。
	info, infoErr := entry.Info()
	if infoErr != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

// 编译期确认存储满足资源库导入暂存端口。
var _ libraryapp.ImportStagingStore = (*LibraryImportStore)(nil)
