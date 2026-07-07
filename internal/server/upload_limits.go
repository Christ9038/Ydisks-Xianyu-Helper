package server

// 上传请求体大小上限。MaxBytesReader 必须在 ParseMultipartForm 之前应用到 r.Body。
// 各值按对应业务场景的实际上限设定，#nosec G120 注释保留在各调用处。

const (
	// maxCardBatchUploadBytes 卡密组批量上传表格上限：5 MiB（卡密组定义都很小）。
	maxCardBatchUploadBytes = 6 << 20
	// maxOrderImportBytes 订单导入文件/请求体上限：32 MiB。
	maxOrderImportBytes = 32 << 20
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
