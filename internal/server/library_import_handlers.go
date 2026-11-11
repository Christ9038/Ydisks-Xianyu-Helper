package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	libraryapp "xianyu-go/internal/application/library"
)

const (
	// maxLibraryImportRequestBytes 是导入预检请求体上限：表格 20 MiB、图片压缩包 200 MiB 加表单元数据。
	maxLibraryImportRequestBytes = libraryapp.MaxImportSheetBytes + libraryapp.MaxImportZipBytes + maxMultipartMetadataBytes
	// libraryImportMemoryBytes 是 multipart 解析时保留在内存中的字节数，超出部分落到临时文件。
	libraryImportMemoryBytes = 32 << 20
)

// libraryImportPreviewRow 是导入预检结果中的一行。
type libraryImportPreviewRow struct {
	// RowNo 是表格行号（表头后第一行为 2）。
	RowNo int `json:"row_no"`
	// Valid 表示该行能否导入。
	Valid bool `json:"valid"`
	// Name 是将要创建的模板名称。
	Name string `json:"name"`
	// Title 是商品标题。
	Title string `json:"title"`
	// Price 是售价文本。
	Price string `json:"price"`
	// ImageCount 是图片数量。
	ImageCount int `json:"image_count"`
	// RemoteImageCount 是其中需要下载的网址图片数量。
	RemoteImageCount int `json:"remote_image_count"`
	// Errors 是阻止导入的问题。
	Errors []string `json:"errors"`
	// Warnings 是不阻止导入的提示。
	Warnings []string `json:"warnings"`
}

// libraryImportPreviewResponse 是导入预检响应；import_id 为空表示没有可导入的行。
type libraryImportPreviewResponse struct {
	// ImportID 是导入任务标识。
	ImportID string `json:"import_id"`
	// Total 是数据行数。
	Total int `json:"total"`
	// Valid 是可导入行数。
	Valid int `json:"valid"`
	// Invalid 是不可导入行数。
	Invalid int `json:"invalid"`
	// RemoteImages 是可导入行中需要下载的网址图片总数。
	RemoteImages int `json:"remote_images"`
	// Rows 是逐行结果。
	Rows []libraryImportPreviewRow `json:"rows"`
}

// libraryImportProgressRow 是导入进度中的一行。
type libraryImportProgressRow struct {
	// RowNo 是表格行号。
	RowNo int `json:"row_no"`
	// Status 是 invalid、pending、created 或 failed。
	Status string `json:"status"`
	// TemplateID 是已创建模板主键，未创建时为 0。
	TemplateID int64 `json:"template_id"`
	// Errors 是预检或导入失败原因。
	Errors []string `json:"errors"`
}

// libraryImportProgressResponse 是一次分段提交后的导入进度响应。
type libraryImportProgressResponse struct {
	// ImportID 是导入任务标识。
	ImportID string `json:"import_id"`
	// Done 表示已没有待导入的行。
	Done bool `json:"done"`
	// Created 是已创建模板的行数。
	Created int `json:"created"`
	// Failed 是导入时失败的行数。
	Failed int `json:"failed"`
	// Pending 是尚未导入的行数。
	Pending int `json:"pending"`
	// Invalid 是预检未通过的行数。
	Invalid int `json:"invalid"`
	// CreatedTemplateIDs 是至今创建的全部模板主键。
	CreatedTemplateIDs []int64 `json:"created_template_ids"`
	// Rows 是逐行进度。
	Rows []libraryImportProgressRow `json:"rows"`
}

// previewLibraryTemplateImport 接收商品表格（字段 file）与可选图片压缩包（字段 images_zip），返回逐行预检结果。
// 只做 multipart 解析与 DTO 转换；解析、校验与暂存由资源库应用服务完成，不访问闲鱼平台接口。
func (s *Server) previewLibraryTemplateImport(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	if !parseMultipartRequest(w, r, maxLibraryImportRequestBytes, libraryImportMemoryBytes, "上传内容不能超过 220 MiB") {
		return
	}
	// input、inputOK 保存读取到的表格与可选压缩包。
	input, inputOK := readLibraryImportInput(w, r, libraryapp.MaxImportSheetBytes, libraryapp.MaxImportZipBytes)
	if !inputOK {
		return
	}
	input.UserID = userID
	// preview、err 保存预检结果。
	preview, err := s.libraryApplication().PreviewImport(r.Context(), input)
	if err != nil {
		writeLibraryError(w, r, err, "导入预检失败")
		return
	}
	writeJSON(w, http.StatusOK, newLibraryImportPreviewResponse(preview))
}

// readLibraryImportInput 读取必填的表格字段 file 与可选的压缩包字段 images_zip；sheetLimit、zipLimit 是各自的字节上限。
// 任一字段读取失败或超限时已写入 400 并返回 false；返回值的 UserID 由调用方填写。
func readLibraryImportInput(w http.ResponseWriter, r *http.Request, sheetLimit, zipLimit int64) (libraryapp.PreviewImportInput, bool) {
	// sheet、sheetName、sheetOK 保存表格字节与文件名。
	sheet, sheetName, sheetOK := readLibraryImportFile(w, r, "file", sheetLimit, true)
	if !sheetOK {
		return libraryapp.PreviewImportInput{}, false
	}
	// archive、archiveOK 保存可选压缩包字节。
	archive, _, archiveOK := readLibraryImportFile(w, r, "images_zip", zipLimit, false)
	if !archiveOK {
		return libraryapp.PreviewImportInput{}, false
	}
	return libraryapp.PreviewImportInput{SheetName: sheetName, Sheet: sheet, Zip: archive}, true
}

// readLibraryImportFile 读取 multipart 中的一个文件字段；required 为 false 且未上传时返回空字节与成功。
// 超过 limit 或读取失败时写入 400 并返回 false。
func readLibraryImportFile(w http.ResponseWriter, r *http.Request, field string, limit int64, required bool) ([]byte, string, bool) {
	// file、header、err 保存文件句柄、文件头与读取错误。
	file, header, err := r.FormFile(field)
	if err != nil {
		if required {
			writeErrRequest(w, r, http.StatusBadRequest, "缺少商品表格文件")
			return nil, "", false
		}
		return nil, "", true
	}
	defer file.Close()
	// data、tooLarge、readErr 保存文件内容、是否超限与读取错误。
	data, tooLarge, readErr := readLimitedBytes(file, limit)
	if readErr != nil {
		writeErrRequest(w, r, http.StatusBadRequest, "读取上传文件失败")
		return nil, "", false
	}
	if tooLarge {
		writeErrRequest(w, r, http.StatusBadRequest, "上传文件超过大小限制")
		return nil, "", false
	}
	// name 是清理后的文件名，只用于判断表格格式。
	name := safeBaseName(header.Filename)
	if name == "" {
		name = "products.csv"
	}
	return data, name, true
}

// commitLibraryTemplateImport 继续导入任务中待导入的行，前端循环调用直到 done。
func (s *Server) commitLibraryTemplateImport(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// progress、err 保存本次提交后的进度。
	progress, err := s.libraryApplication().CommitImport(r.Context(), userID, strings.TrimSpace(chi.URLParam(r, "import_id")))
	if err != nil {
		writeLibraryError(w, r, err, "导入商品模板失败")
		return
	}
	writeJSON(w, http.StatusOK, newLibraryImportProgressResponse(progress))
}

// discardLibraryTemplateImport 丢弃导入任务的暂存；已创建的模板保留。
func (s *Server) discardLibraryTemplateImport(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// err 保存丢弃结果。
	if err := s.libraryApplication().DiscardImport(r.Context(), userID, strings.TrimSpace(chi.URLParam(r, "import_id"))); err != nil {
		writeLibraryError(w, r, err, "取消导入失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// newLibraryImportPreviewResponse 把应用预检结果转换为响应 DTO；空切片输出为 []。
func newLibraryImportPreviewResponse(preview libraryapp.ImportPreview) libraryImportPreviewResponse {
	// out 是响应 DTO。
	out := libraryImportPreviewResponse{ImportID: preview.ImportID, Total: preview.Total, Valid: preview.Valid, Invalid: preview.Invalid, RemoteImages: preview.RemoteImages,
		Rows: make([]libraryImportPreviewRow, 0, len(preview.Rows))}
	// row 是应用预检行。
	for _, row := range preview.Rows {
		out.Rows = append(out.Rows, libraryImportPreviewRow{RowNo: row.RowNo, Valid: len(row.Errors) == 0, Name: row.Name, Title: row.Title, Price: row.Price,
			ImageCount: row.ImageCount, RemoteImageCount: row.RemoteImageCount, Errors: nonNilStrings(row.Errors), Warnings: nonNilStrings(row.Warnings)})
	}
	return out
}

// newLibraryImportProgressResponse 把应用导入进度转换为响应 DTO；空切片输出为 []。
func newLibraryImportProgressResponse(progress libraryapp.ImportProgress) libraryImportProgressResponse {
	// out 是响应 DTO。
	out := libraryImportProgressResponse{ImportID: progress.ImportID, Done: progress.Done, Created: progress.Created, Failed: progress.Failed, Pending: progress.Pending,
		Invalid: progress.Invalid, CreatedTemplateIDs: append([]int64{}, progress.CreatedTemplateIDs...), Rows: make([]libraryImportProgressRow, 0, len(progress.Rows))}
	// row 是应用进度行。
	for _, row := range progress.Rows {
		out.Rows = append(out.Rows, libraryImportProgressRow{RowNo: row.RowNo, Status: row.Status, TemplateID: row.TemplateID, Errors: nonNilStrings(row.Errors)})
	}
	return out
}

// nonNilStrings 把 nil 切片转换为空切片，保证 JSON 输出 [] 而不是 null。
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
