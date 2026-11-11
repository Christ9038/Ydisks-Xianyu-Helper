package library

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	itemapp "xianyu-go/internal/application/items"
)

const (
	// MaxImportRows 是单次 Excel 导入商品模板的最大数据行数。
	MaxImportRows = 300
	// MaxImportSheetBytes 是导入表格文件的大小上限，与批量铺货表格一致。
	MaxImportSheetBytes = 20 << 20
	// MaxImportZipBytes 是导入图片压缩包的大小上限，与批量铺货图片包一致。
	MaxImportZipBytes = 200 << 20
	// importStartBudget 是单次分段提交允许开始新行的时长：超过后不再开始新行，已开始的行会做完。
	// 单行最多 9 张网址图片、每张由下载端口限时 10 秒，最坏 20+90 秒，仍低于服务端 2 分钟写超时，且每次提交至少完成一行。
	importStartBudget = 20 * time.Second
	// importStagingTTL 是导入暂存的保留时长，超过后在下一次预检时被清理。
	importStagingTTL = 24 * time.Hour
)

// ErrImportNotFound 表示导入任务不存在、已过期或不属于当前用户。
var ErrImportNotFound = errors.New("导入任务不存在或已过期")

// ErrImportBusy 表示同一导入任务正在被另一个请求处理。
var ErrImportBusy = errors.New("导入任务正在处理中，请稍候")

// 导入行状态：invalid 预检未通过、pending 待导入、created 已创建模板、failed 导入时失败。
const (
	// ImportRowInvalid 表示预检未通过，不会被导入。
	ImportRowInvalid = "invalid"
	// ImportRowPending 表示预检通过、尚未导入。
	ImportRowPending = "pending"
	// ImportRowCreated 表示已成功创建模板。
	ImportRowCreated = "created"
	// ImportRowFailed 表示导入时图片或模板校验失败。
	ImportRowFailed = "failed"
)

// TemplateRowParser 把表格字段行解析为预检行（含 #107 模板动作与逐行变量分发），由批量预检服务实现。
type TemplateRowParser interface {
	// PreviewTemplateRows 解析并校验模板导入行，跳过账号与上传目录图片文件校验。
	PreviewTemplateRows(ctx context.Context, userID int64, fields []map[string]any) ([]itemapp.BatchPreviewRow, error)
}

// RemoteImageFetcher 下载表格中的网址图片；实现方负责只连接公网地址、限时并限制大小。
type RemoteImageFetcher interface {
	// Fetch 返回图片字节；ctx 取消时应尽快返回 ctx 错误。
	Fetch(ctx context.Context, rawURL string) ([]byte, error)
}

// ImportArchiveFile 是暂存图片压缩包的随机读取句柄，调用方负责关闭。
type ImportArchiveFile interface {
	io.ReaderAt
	io.Closer
}

// ImportStagingStore 保存预检通过的导入任务（表格、可选压缩包与进度清单），供后续分段提交读取。
type ImportStagingStore interface {
	// Create 为导入任务写入表格与可选压缩包；zip 为空表示没有压缩包。
	Create(ctx context.Context, importID string, sheet, zip []byte) error
	// ReadSheet 读取暂存表格；任务不存在时返回 ErrImportNotFound。
	ReadSheet(ctx context.Context, importID string) ([]byte, error)
	// OpenZip 打开暂存压缩包并返回字节数；没有压缩包时返回 nil 句柄。
	OpenZip(ctx context.Context, importID string) (ImportArchiveFile, int64, error)
	// SaveManifest 原子写入进度清单。
	SaveManifest(ctx context.Context, manifest ImportManifest) error
	// LoadManifest 读取进度清单；任务不存在时返回 ErrImportNotFound。
	LoadManifest(ctx context.Context, importID string) (ImportManifest, error)
	// Remove 删除导入任务的全部暂存文件，任务不存在视为成功。
	Remove(ctx context.Context, importID string) error
	// CleanupBefore 删除创建时间早于 cutoff 的导入任务。
	CleanupBefore(ctx context.Context, cutoff time.Time) error
}

// ImportManifest 是导入任务的进度清单；只记录行号、状态、模板主键和错误，不保存变量取值或图片。
type ImportManifest struct {
	// ImportID 是导入任务标识。
	ImportID string
	// UserID 是任务所属用户，用于归属核对。
	UserID int64
	// SheetName 是原始表格文件名，决定重新解析时的格式。
	SheetName string
	// HasZip 表示任务是否附带图片压缩包。
	HasZip bool
	// CreatedAt 是任务创建时间，用于过期清理。
	CreatedAt time.Time
	// Rows 是按行号排列的逐行进度。
	Rows []ImportManifestRow
}

// ImportManifestRow 是导入清单中的一行进度。
type ImportManifestRow struct {
	// RowNo 是表格行号（表头后第一行为 2）。
	RowNo int
	// Status 是 ImportRow* 常量之一。
	Status string
	// TemplateID 是已创建模板的主键，仅 created 状态有效。
	TemplateID int64
	// Errors 是预检或导入失败原因，不含变量取值。
	Errors []string
}

// PreviewImportInput 是导入预检的输入。
type PreviewImportInput struct {
	// UserID 是导入用户。
	UserID int64
	// SheetName 是表格文件名，决定按 CSV、TSV 或 XLSX 解析。
	SheetName string
	// Sheet 是表格字节。
	Sheet []byte
	// Zip 是可选图片压缩包字节。
	Zip []byte
}

// ImportPreviewRow 是预检结果中的一行。
type ImportPreviewRow struct {
	// RowNo 是表格行号。
	RowNo int
	// Name 是将要创建的模板名称。
	Name string
	// Title 是商品标题。
	Title string
	// Price 是售价文本。
	Price string
	// ImageCount 是该行图片数量。
	ImageCount int
	// RemoteImageCount 是其中需要下载的网址图片数量。
	RemoteImageCount int
	// Errors 是阻止导入的问题。
	Errors []string
	// Warnings 是不阻止导入的提示，例如标题重复。
	Warnings []string
}

// ImportPreview 是导入预检结果；ImportID 为空表示没有可导入的行、未建立暂存。
type ImportPreview struct {
	// ImportID 是暂存任务标识。
	ImportID string
	// Total、Valid、Invalid 是总行数、可导入行数和不可导入行数。
	Total, Valid, Invalid int
	// RemoteImages 是可导入行中需要下载的网址图片总数。
	RemoteImages int
	// Rows 是逐行结果。
	Rows []ImportPreviewRow
}

// ImportProgress 是一次分段提交后的导入进度。
type ImportProgress struct {
	// ImportID 是导入任务标识。
	ImportID string
	// Done 表示已没有待导入的行，暂存已删除。
	Done bool
	// Created、Failed、Pending、Invalid 是各状态的行数。
	Created, Failed, Pending, Invalid int
	// CreatedTemplateIDs 是本任务至今创建的全部模板主键，按行号排列。
	CreatedTemplateIDs []int64
	// Rows 是逐行进度。
	Rows []ImportManifestRow
}

// TemplateImportService 负责 Excel 导入商品模板：预检、分段提交与丢弃。导入全程不访问闲鱼平台接口。
//
// 并发：busy 记录正在提交或丢弃的任务，由 mu 保护；mu 只保护这张内存表，持锁期间不做任何 I/O。
// 同一任务的并发请求返回 ErrImportBusy；不同任务互不影响。服务不启动后台 goroutine。
type TemplateImportService struct {
	// parser 解析并校验表格行。
	parser TemplateRowParser
	// fetcher 下载网址图片。
	fetcher RemoteImageFetcher
	// staging 保存导入暂存。
	staging ImportStagingStore
	// images 把图片写入图片库并按内容去重。
	images *ImageService
	// templates 创建并完整校验模板。
	templates *TemplateService
	// newImportID 生成导入任务标识。
	newImportID func() string
	// now 提供当前时间，测试可注入。
	now func() time.Time
	// mu 保护 busy。
	mu sync.Mutex
	// busy 是正在处理的导入任务标识集合。
	busy map[string]bool
}

// NewTemplateImportService 创建导入服务并校验必需依赖；now 为 nil 时使用 time.Now。
func NewTemplateImportService(parser TemplateRowParser, fetcher RemoteImageFetcher, staging ImportStagingStore, images *ImageService, templates *TemplateService, newImportID func() string, now func() time.Time) (*TemplateImportService, error) {
	if parser == nil || fetcher == nil || staging == nil || images == nil || templates == nil || newImportID == nil {
		return nil, errors.New("商品模板导入依赖不能为空")
	}
	if now == nil {
		now = time.Now
	}
	return &TemplateImportService{parser: parser, fetcher: fetcher, staging: staging, images: images, templates: templates, newImportID: newImportID, now: now, busy: map[string]bool{}}, nil
}

// PreviewImport 解析表格并逐行检查能否导入；有可导入行时建立暂存并返回任务标识。
// 预检不写入模板、不下载图片，只核对压缩包内路径存在与网址格式。
func (s *TemplateImportService) PreviewImport(ctx context.Context, input PreviewImportInput) (ImportPreview, error) {
	// 过期暂存清理失败不影响本次预检，下次预检会再次尝试。
	_ = s.staging.CleanupBefore(ctx, s.now().Add(-importStagingTTL))
	if len(input.Sheet) > MaxImportSheetBytes {
		return ImportPreview{}, invalid("商品表格不能超过 20 MiB")
	}
	if len(input.Zip) > MaxImportZipBytes {
		return ImportPreview{}, invalid("图片 zip 不能超过 200 MiB")
	}
	// rows、archive、err 保存解析后的行与压缩包索引。
	rows, archive, err := s.parse(ctx, input.UserID, input.SheetName, input.Sheet, readerOf(input.Zip), int64(len(input.Zip)))
	if err != nil {
		return ImportPreview{}, err
	}
	// existing、listErr 保存用户已有模板，用于标题重复提示。
	existing, listErr := s.templates.ListTemplates(ctx, input.UserID)
	if listErr != nil {
		return ImportPreview{}, listErr
	}
	// preview、manifest 是返回结果与待暂存的进度清单。
	preview := ImportPreview{Total: len(rows), Rows: make([]ImportPreviewRow, 0, len(rows))}
	// manifest 是待暂存的进度清单，逐行记录待导入或预检未通过及原因，不含变量取值。
	manifest := ImportManifest{UserID: input.UserID, SheetName: input.SheetName, HasZip: len(input.Zip) > 0, CreatedAt: s.now().UTC()}
	// titles 记录已有模板与前面行的标题，值为首次出现的行号（已有模板为 0）。
	titles := make(map[string]int, len(existing)+len(rows))
	// template 是已有模板。
	for _, template := range existing {
		titles[template.Title] = 0
	}
	// row 是当前解析行。
	for _, row := range rows {
		// item 是该行的预检结果。
		item := previewImportRow(row, archive)
		// firstRow、seen 是同标题首次出现的位置。
		if firstRow, seen := titles[item.Title]; seen && item.Title != "" {
			if firstRow == 0 {
				item.Warnings = append(item.Warnings, "已有同标题的商品模板")
			} else {
				item.Warnings = append(item.Warnings, fmt.Sprintf("与第 %d 行标题相同", firstRow))
			}
		} else {
			titles[item.Title] = item.RowNo
		}
		// status 是该行在清单中的初始状态。
		status := ImportRowPending
		if len(item.Errors) > 0 {
			status = ImportRowInvalid
			preview.Invalid++
		} else {
			preview.Valid++
			preview.RemoteImages += item.RemoteImageCount
		}
		manifest.Rows = append(manifest.Rows, ImportManifestRow{RowNo: item.RowNo, Status: status, Errors: item.Errors})
		preview.Rows = append(preview.Rows, item)
	}
	if preview.Valid == 0 {
		return preview, nil
	}
	manifest.ImportID = s.newImportID()
	// createErr 保存暂存写入错误。
	if createErr := s.staging.Create(ctx, manifest.ImportID, input.Sheet, input.Zip); createErr != nil {
		return ImportPreview{}, createErr
	}
	// saveErr 保存清单写入错误；失败时回收刚建立的暂存。
	if saveErr := s.staging.SaveManifest(ctx, manifest); saveErr != nil {
		_ = s.staging.Remove(ctx, manifest.ImportID)
		return ImportPreview{}, saveErr
	}
	preview.ImportID = manifest.ImportID
	return preview, nil
}

// CommitImport 继续导入任务中待导入的行，直到全部完成或超过单次开始新行的时长预算；调用方循环调用直到 Done。
// 每行依次：取齐图片（压缩包或网址下载）→ 写入图片库 → 创建模板（再次完整校验）→ 写入清单。
// 请求取消导致某行未完成时，该行保持待导入，下次提交重试；数据库等系统错误直接返回，已完成的进度保留。
func (s *TemplateImportService) CommitImport(ctx context.Context, userID int64, importID string) (ImportProgress, error) {
	// release 保存任务占用的释放函数。
	release, err := s.acquire(importID)
	if err != nil {
		return ImportProgress{}, err
	}
	defer release()
	// manifest、loadErr 保存进度清单。
	manifest, loadErr := s.loadOwned(ctx, userID, importID)
	if loadErr != nil {
		return ImportProgress{}, loadErr
	}
	if pendingRows(manifest) > 0 {
		// processErr 保存逐行导入中的系统错误。
		if processErr := s.processPending(ctx, &manifest); processErr != nil {
			return ImportProgress{}, processErr
		}
	}
	// progress 是本次提交后的进度。
	progress := progressOf(manifest)
	if progress.Done {
		// 暂存删除失败不影响结果，过期清理会兜底。
		_ = s.staging.Remove(ctx, importID)
	}
	return progress, nil
}

// DiscardImport 删除导入任务的暂存；任务不存在时视为成功，已创建的模板保留。
func (s *TemplateImportService) DiscardImport(ctx context.Context, userID int64, importID string) error {
	// release 保存任务占用的释放函数。
	release, err := s.acquire(importID)
	if err != nil {
		return err
	}
	defer release()
	// loadErr 保存清单读取与归属核对结果。
	if _, loadErr := s.loadOwned(ctx, userID, importID); loadErr != nil {
		if errors.Is(loadErr, ErrImportNotFound) {
			return nil
		}
		return loadErr
	}
	return s.staging.Remove(ctx, importID)
}

// acquire 占用导入任务，返回释放函数；任务已被占用时返回 ErrImportBusy。持锁期间只读写内存表。
func (s *TemplateImportService) acquire(importID string) (func(), error) {
	if strings.TrimSpace(importID) == "" {
		return nil, ErrImportNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy[importID] {
		return nil, ErrImportBusy
	}
	s.busy[importID] = true
	return func() {
		s.mu.Lock()
		delete(s.busy, importID)
		s.mu.Unlock()
	}, nil
}

// loadOwned 读取清单并核对归属；不属于当前用户时与不存在同样返回 ErrImportNotFound。
func (s *TemplateImportService) loadOwned(ctx context.Context, userID int64, importID string) (ImportManifest, error) {
	// manifest、err 保存清单读取结果。
	manifest, err := s.staging.LoadManifest(ctx, importID)
	if err != nil {
		return ImportManifest{}, err
	}
	if manifest.UserID != userID {
		return ImportManifest{}, ErrImportNotFound
	}
	return manifest, nil
}

// processPending 重新解析暂存表格，并在时长预算内逐行导入待导入的行；manifest 原地更新并在每行后持久化。
func (s *TemplateImportService) processPending(ctx context.Context, manifest *ImportManifest) error {
	// sheet、err 保存暂存表格。
	sheet, err := s.staging.ReadSheet(ctx, manifest.ImportID)
	if err != nil {
		return err
	}
	// zipFile、zipSize、zipErr 保存暂存压缩包句柄。
	zipFile, zipSize, zipErr := s.staging.OpenZip(ctx, manifest.ImportID)
	if zipErr != nil {
		return zipErr
	}
	if zipFile != nil {
		defer zipFile.Close()
	}
	// source 是压缩包读取源；没有压缩包时为 nil。
	var source io.ReaderAt
	if zipFile != nil {
		source = zipFile
	}
	// rows、archive、parseErr 保存重新解析的行与压缩包索引；暂存内容在预检时已通过解析，这里失败属于系统错误。
	rows, archive, parseErr := s.parse(ctx, manifest.UserID, manifest.SheetName, sheet, source, zipSize)
	if parseErr != nil {
		return parseErr
	}
	// byRow 是行号到解析行的映射。
	byRow := make(map[int]itemapp.BatchPreviewRow, len(rows))
	// row 是当前解析行。
	for _, row := range rows {
		byRow[row.RowNo] = row
	}
	// deadline 是本次提交允许开始新行的截止时间。
	deadline := s.now().Add(importStartBudget)
	// started 表示本次提交是否已经开始过至少一行，保证每次提交都有进展。
	started := false
	// index 是清单行下标。
	for index := range manifest.Rows {
		// entry 指向当前清单行。
		entry := &manifest.Rows[index]
		if entry.Status != ImportRowPending {
			continue
		}
		if ctx.Err() != nil || (started && !s.now().Before(deadline)) {
			return nil
		}
		started = true
		// row、found 是该行重新解析的结果；暂存表格不可变，找不到只可能是解析规则变化，按失败处理。
		row, found := byRow[entry.RowNo]
		if !found {
			*entry = ImportManifestRow{RowNo: entry.RowNo, Status: ImportRowFailed, Errors: []string{"表格内容已变化，请重新导入"}}
			// saveErr 保存清单写入错误。
			if saveErr := s.staging.SaveManifest(ctx, *manifest); saveErr != nil {
				return saveErr
			}
			continue
		}
		// outcome、rowErr 保存该行导入结果；interrupted 表示因请求取消未完成。
		outcome, interrupted, rowErr := s.importRow(ctx, manifest.UserID, row, archive)
		if rowErr != nil {
			return rowErr
		}
		if interrupted {
			return nil
		}
		*entry = outcome
		// saveErr 保存清单写入错误。
		if saveErr := s.staging.SaveManifest(ctx, *manifest); saveErr != nil {
			return saveErr
		}
	}
	return nil
}

// importRow 导入一行：返回更新后的清单行、是否因请求取消而中断，以及系统错误。
func (s *TemplateImportService) importRow(ctx context.Context, userID int64, row itemapp.BatchPreviewRow, archive *importArchive) (ImportManifestRow, bool, error) {
	// result 是该行的新状态，默认失败，成功时改写。
	result := ImportManifestRow{RowNo: row.RowNo, Status: ImportRowFailed}
	// recheck 是重新解析后的校验结果；预检后模板、卡密被修改可能导致该行不再合法。
	if recheck := previewImportRow(row, archive); len(recheck.Errors) > 0 {
		result.Errors = recheck.Errors
		return result, false, nil
	}
	// uploads 保存该行全部图片字节，图片按表格顺序排列。
	uploads := make([]ImageUpload, 0, len(row.Images))
	// reference 是当前图片引用。
	for _, reference := range row.Images {
		// data、fetchErr 保存图片字节及获取错误。
		data, fetchErr := s.imageBytes(ctx, reference, archive)
		if fetchErr != nil {
			if ctx.Err() != nil {
				return ImportManifestRow{}, true, nil
			}
			result.Errors = []string{fetchErr.Error()}
			return result, false, nil
		}
		uploads = append(uploads, ImageUpload{Filename: imageFilename(reference), Data: data})
	}
	// uploaded、uploadErr 保存图片库写入结果；仓储或存储故障属于系统错误。
	uploaded, uploadErr := s.images.UploadImages(ctx, userID, uploads)
	if uploadErr != nil {
		return ImportManifestRow{}, false, uploadErr
	}
	// imageIDs 是按顺序去重后的图片主键；内容相同的两张图只保留一张。
	imageIDs := make([]int64, 0, len(uploaded))
	// seen 记录已加入的图片主键。
	seen := make(map[int64]bool, len(uploaded))
	// item 是单张图片的上传结果。
	for _, item := range uploaded {
		if item.Error != "" {
			result.Errors = []string{fmt.Sprintf("图片「%s」无效：%s", item.Filename, item.Error)}
			return result, false, nil
		}
		if !seen[item.Image.ID] {
			seen[item.Image.ID] = true
			imageIDs = append(imageIDs, item.Image.ID)
		}
	}
	// template、buildErr 保存待创建的模板。
	template, buildErr := templateFromRow(userID, row, imageIDs)
	if buildErr != nil {
		result.Errors = []string{buildErr.Error()}
		return result, false, nil
	}
	// created、createErr 保存模板创建结果；校验错误记为该行失败，其他错误属于系统错误。
	created, createErr := s.templates.CreateTemplate(ctx, template)
	if createErr != nil {
		// validation 保存可展示的校验错误。
		var validation *ValidationError
		if errors.As(createErr, &validation) {
			result.Errors = []string{validation.Message}
			return result, false, nil
		}
		return ImportManifestRow{}, false, createErr
	}
	return ImportManifestRow{RowNo: row.RowNo, Status: ImportRowCreated, TemplateID: created.ID}, false, nil
}

// imageBytes 读取一张图片：网址交给下载端口，其他引用在压缩包中查找。
func (s *TemplateImportService) imageBytes(ctx context.Context, reference string, archive *importArchive) ([]byte, error) {
	if isRemoteImageReference(reference) {
		// data、err 保存下载结果。
		data, err := s.fetcher.Fetch(ctx, strings.TrimSpace(reference))
		if err != nil {
			return nil, fmt.Errorf("图片下载失败（%s）：%w", strings.TrimSpace(reference), err)
		}
		return data, nil
	}
	return archive.read(reference)
}

// parse 解析表格与可选压缩包；source 为 nil 表示没有压缩包。表格或压缩包格式错误返回校验错误。
func (s *TemplateImportService) parse(ctx context.Context, userID int64, sheetName string, sheet []byte, source io.ReaderAt, size int64) ([]itemapp.BatchPreviewRow, *importArchive, error) {
	// fields、err 保存表格字段行。
	fields, err := itemapp.ParseSheet(sheet, sheetName, MaxImportRows)
	if err != nil {
		return nil, nil, invalid(err.Error())
	}
	// archive 是压缩包索引；没有压缩包时为 nil。
	var archive *importArchive
	if source != nil && size > 0 {
		archive, err = newImportArchive(source, size)
		if err != nil {
			return nil, nil, err
		}
	}
	// rows、parseErr 保存逐行解析结果。
	rows, parseErr := s.parser.PreviewTemplateRows(ctx, userID, fields)
	if errors.Is(parseErr, itemapp.ErrBatchPreviewNoRows) {
		return nil, nil, invalid("表格中没有有效数据行")
	}
	if parseErr != nil {
		return nil, nil, parseErr
	}
	return rows, archive, nil
}

// previewImportRow 把解析行转换为预检结果，补充模板专属检查：名称、标题长度、压缩包路径与网址格式。
func previewImportRow(row itemapp.BatchPreviewRow, archive *importArchive) ImportPreviewRow {
	// item 是预检结果，错误继承行解析结果。
	item := ImportPreviewRow{RowNo: row.RowNo, Name: templateNameOf(row), Title: strings.TrimSpace(row.Title), Price: row.Price, ImageCount: len(row.Images),
		Errors: append([]string(nil), row.Errors...)}
	if utf8.RuneCountInString(item.Name) > maxTemplateNameRunes {
		item.Errors = append(item.Errors, fmt.Sprintf("模板名称不能超过 %d 个字符", maxTemplateNameRunes))
	}
	if utf8.RuneCountInString(item.Title) > maxTemplateTitleRunes {
		item.Errors = append(item.Errors, fmt.Sprintf("商品标题不能超过 %d 个字符", maxTemplateTitleRunes))
	}
	// reference 是当前图片引用。
	for _, reference := range row.Images {
		switch {
		case isRemoteImageReference(reference):
			item.RemoteImageCount++
			if !validRemoteImageURL(reference) {
				item.Errors = append(item.Errors, "图片网址无效: "+strings.TrimSpace(reference))
			}
		case archive == nil:
			item.Errors = append(item.Errors, "图片不在压缩包中（未上传图片 zip）: "+strings.TrimSpace(reference))
		case !archive.has(reference):
			item.Errors = append(item.Errors, "图片不在压缩包中: "+strings.TrimSpace(reference))
		}
	}
	return item
}

// templateNameOf 返回模板名称：优先使用「模板名称」列，留空时取标题前 100 个字符。
func templateNameOf(row itemapp.BatchPreviewRow) string {
	// name 是「模板名称」列文本。
	name := ""
	// value、ok 是原始字段值及其是否为字符串。
	if value, ok := row.Raw["template_name"].(string); ok {
		name = strings.TrimSpace(value)
	}
	if name != "" {
		return name
	}
	// runes 是标题字符序列，用于按字符截断。
	runes := []rune(strings.TrimSpace(row.Title))
	if len(runes) > maxTemplateNameRunes {
		runes = runes[:maxTemplateNameRunes]
	}
	return string(runes)
}

// templateFromRow 把解析行转换为模板应用模型；金额已在行解析时校验，这里只做元到分的换算。
func templateFromRow(userID int64, row itemapp.BatchPreviewRow, imageIDs []int64) (Template, error) {
	// price、priceErr 保存售价分值。
	price, priceErr := itemapp.ParseMoneyCents(row.Price)
	// original、originalErr 保存原价分值。
	original, originalErr := itemapp.ParseMoneyCents(row.OriginalPrice)
	// postage、postageErr 保存固定邮费分值；只有一口价邮费才读取「邮费」列，其他模式忽略该列，与批量铺货一致。
	postage, postageErr := int64(0), error(nil)
	if row.PostageMode == "fixed" {
		postage, postageErr = itemapp.ParseMoneyCents(row.Postage)
	}
	if priceErr != nil || originalErr != nil || postageErr != nil {
		return Template{}, invalid("金额格式错误")
	}
	return Template{UserID: userID, Name: templateNameOf(row), Title: row.Title, Description: row.Description, PriceCents: price, OriginalPriceCents: original,
		Quantity: row.Quantity, PostageMode: row.PostageMode, PostageCents: postage, Category: row.Category, ImageIDs: imageIDs, Automation: row.Automation}, nil
}

// imageFilename 返回图片在图片库中展示的文件名：取路径或网址最后一段，去掉查询参数。
func imageFilename(reference string) string {
	// value 是去掉查询参数与片段后的引用。
	value := strings.TrimSpace(reference)
	// cut 是查询参数或片段的起始位置。
	if cut := strings.IndexAny(value, "?#"); cut >= 0 {
		value = value[:cut]
	}
	value = strings.TrimRight(strings.ReplaceAll(value, "\\", "/"), "/")
	// slash 是最后一个分隔符的位置。
	if slash := strings.LastIndex(value, "/"); slash >= 0 {
		value = value[slash+1:]
	}
	if value == "" {
		return "image"
	}
	return value
}

// pendingRows 统计清单中待导入的行数。
func pendingRows(manifest ImportManifest) int {
	// count 是待导入行数。
	count := 0
	// row 是清单行。
	for _, row := range manifest.Rows {
		if row.Status == ImportRowPending {
			count++
		}
	}
	return count
}

// progressOf 汇总清单为导入进度。
func progressOf(manifest ImportManifest) ImportProgress {
	// progress 是汇总结果。
	progress := ImportProgress{ImportID: manifest.ImportID, Rows: manifest.Rows}
	// row 是清单行。
	for _, row := range manifest.Rows {
		switch row.Status {
		case ImportRowCreated:
			progress.Created++
			progress.CreatedTemplateIDs = append(progress.CreatedTemplateIDs, row.TemplateID)
		case ImportRowFailed:
			progress.Failed++
		case ImportRowPending:
			progress.Pending++
		default:
			progress.Invalid++
		}
	}
	progress.Done = progress.Pending == 0
	return progress
}

// readerOf 把可选的压缩包字节包装为 ReaderAt；空字节返回 nil 表示没有压缩包。
func readerOf(data []byte) io.ReaderAt {
	if len(data) == 0 {
		return nil
	}
	return bytes.NewReader(data)
}
