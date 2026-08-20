package server

import (
	"errors"
	"mime"
	"net/http"
)

// 上传请求体大小上限。MaxBytesReader 必须在 ParseMultipartForm 之前应用到 r.Body。
// 各值按对应业务场景的实际上限设定，#nosec G120 注释保留在各调用处。

const (
	// maxMultipartMetadataBytes 为 boundary、字段名和文件头预留 1 MiB，文件上限不应被 multipart 包装开销侵蚀。
	maxMultipartMetadataBytes = 1 << 20
	// maxCardBatchUploadBytes 卡密组批量上传表格上限：5 MiB（卡密组定义都很小）。
	maxCardBatchUploadBytes = 6 << 20
	// maxOrderImportBytes 是订单原始文件与 JSON 请求体上限：32 MiB。
	maxOrderImportBytes = 32 << 20
	// maxOrderImportMultipartBytes 是订单文件加 multipart 元数据后的总请求上限。
	maxOrderImportMultipartBytes = maxOrderImportBytes + maxMultipartMetadataBytes
	// maxChatImageBytes 是聊天单张图片的业务上限：10 MiB。
	maxChatImageBytes = 10 << 20
	// maxChatImageMultipartBytes 是聊天图片加 multipart 元数据后的总请求上限。
	maxChatImageMultipartBytes = maxChatImageBytes + maxMultipartMetadataBytes
	// maxOrderRefreshRequestBytes 限制订单刷新筛选 DTO，避免无文件端点接受无界请求体。
	maxOrderRefreshRequestBytes = 1 << 20
	// maxOrderImportRows 单次订单导入最多订单数。
	maxOrderImportRows = 5000
	// maxItemPublishBytes 单品发布上限：9 张 10 MiB 图片 + multipart 元数据。
	maxItemPublishBytes = 96 << 20
	// maxItemPublishBatchBytes 批量发布上传上限：20 MiB 表格 + 200 MiB 图片压缩包 + multipart 元数据。
	maxItemPublishBatchBytes = 224 << 20
	// maxItemPublishBatchParseBytes 批量发布 multipart 解析上限（含图片压缩包解压缓冲）。
	maxItemPublishBatchParseBytes = 256 << 20
	// maxItemPublishZipFiles 批量发布图片 zip 内最多文件数。
	maxItemPublishZipFiles = 500
	// maxItemPublishZipExtractBytes 批量发布图片 zip 总解压上限。
	maxItemPublishZipExtractBytes = 500 << 20
	// maxXLSXXMLPartBytes xlsx 内 worksheet/sharedStrings 单个 XML 解压上限。
	maxXLSXXMLPartBytes = 32 << 20
)

// parseMultipartRequest 验证 multipart 媒体类型、限制总请求大小并解析表单；调用者据返回值决定是否继续读取字段。
func parseMultipartRequest(w http.ResponseWriter, r *http.Request, requestLimit, memoryLimit int64, tooLargeMessage string) bool {
	// mediaType、contentTypeErr 保存标准化的请求媒体类型及头部解析错误。
	mediaType, _, contentTypeErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if contentTypeErr != nil || mediaType != "multipart/form-data" {
		writeErr(w, http.StatusBadRequest, "请求格式错误，请使用 multipart/form-data")
		return false
	}
	// Body 由当前 handler 独占并在解析前限制总大小，避免 boundary 元数据绕过上传配额。
	r.Body = http.MaxBytesReader(w, r.Body, requestLimit)
	// parseErr 保存 multipart 流、boundary 或大小校验失败的原因，不向 API 响应泄露底层实现细节。
	if parseErr := r.ParseMultipartForm(memoryLimit); parseErr != nil {
		// maxBytesErr 仅表示总请求超过显式上限，和表单 boundary 损坏使用不同提示。
		var maxBytesErr *http.MaxBytesError
		if errors.As(parseErr, &maxBytesErr) {
			writeErr(w, http.StatusBadRequest, tooLargeMessage)
		} else {
			writeErr(w, http.StatusBadRequest, "上传表单损坏，请检查后重试")
		}
		return false
	}
	return true
}
