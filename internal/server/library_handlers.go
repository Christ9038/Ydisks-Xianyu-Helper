package server

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	itemapp "xianyu-go/internal/application/items"
	libraryapp "xianyu-go/internal/application/library"
)

const (
	// maxLibraryUploadBytes 是单次图片上传请求的总大小上限：20 张 10 MiB 图片。
	maxLibraryUploadBytes = libraryapp.MaxUploadFiles * libraryapp.MaxImageBytes
	// libraryUploadMemoryBytes 是 multipart 解析时保留在内存中的字节数，超出部分落到临时文件。
	libraryUploadMemoryBytes = 32 << 20
)

// libraryImageID 解析路径中的图片主键，非法值返回 400。
func libraryImageID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	return libraryPathID(w, r, "image_id", "图片标识无效")
}

// libraryTemplateID 解析路径中的模板主键，非法值返回 400。
func libraryTemplateID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	return libraryPathID(w, r, "template_id", "模板标识无效")
}

// libraryPathID 解析路径参数中的正整数主键。
func libraryPathID(w http.ResponseWriter, r *http.Request, name, message string) (int64, bool) {
	// id、err 保存解析结果；主键必须是正整数。
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		writeErrRequest(w, r, http.StatusBadRequest, message)
		return 0, false
	}
	return id, true
}

// writeLibraryError 将资源库应用错误映射为统一错误信封；图片仍被模板引用时通过 details 返回模板名称。
func writeLibraryError(w http.ResponseWriter, r *http.Request, err error, fallback string) {
	// inUse 保存图片被引用的详细信息。
	var inUse *libraryapp.ImageInUseError
	// validation 保存可直接展示的校验错误。
	var validation *libraryapp.ValidationError
	switch {
	case errors.As(err, &inUse):
		writeErrDetails(w, http.StatusConflict, "conflict", "图片仍被模板、回复或卡密引用，请先移除引用后再删除", middleware.GetReqID(r.Context()),
			map[string]any{"templates": inUse.Templates})
	case errors.As(err, &validation):
		writeErrRequest(w, r, http.StatusBadRequest, validation.Message)
	case errors.Is(err, libraryapp.ErrImageNotFound):
		writeErrRequest(w, r, http.StatusNotFound, "图片不存在")
	case errors.Is(err, libraryapp.ErrTemplateNotFound):
		writeErrRequest(w, r, http.StatusNotFound, "商品模板不存在")
	case errors.Is(err, itemapp.ErrBatchPreviewNoRows):
		writeErrRequest(w, r, http.StatusBadRequest, "没有可发布的商品")
	default:
		writeErrRequest(w, r, http.StatusInternalServerError, fallback)
	}
}

// listLibraryImages 返回当前用户的图片库。
func (s *Server) listLibraryImages(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// images、err 保存图片列表及查询错误。
	images, err := s.libraryApplication().ListImages(r.Context(), userID)
	if err != nil {
		writeLibraryError(w, r, err, "查询图片失败")
		return
	}
	// out 保存响应 DTO，空结果返回 []。
	out := make([]libraryImageResponse, 0, len(images))
	// image 是当前图片。
	for _, image := range images {
		out = append(out, newLibraryImageResponse(image))
	}
	writeJSON(w, http.StatusOK, out)
}

// uploadLibraryImages 接收一组图片（表单字段 files）并保存到图片库。
func (s *Server) uploadLibraryImages(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	if !parseMultipartRequest(w, r, maxLibraryUploadBytes+maxMultipartMetadataBytes, libraryUploadMemoryBytes, "图片总大小超出上传限制") {
		return
	}
	// headers 是表单字段 files 中的全部文件头。
	headers := r.MultipartForm.File["files"]
	// uploads 保存读取到内存的文件，单个文件最多读取上限加一字节以识别超限。
	uploads := make([]libraryapp.ImageUpload, 0, len(headers))
	// header 是当前文件头。
	for _, header := range headers {
		// file、openErr 保存文件句柄及打开错误。
		file, openErr := header.Open()
		if openErr != nil {
			writeErrRequest(w, r, http.StatusBadRequest, "读取上传图片失败")
			return
		}
		// data、readErr 保存文件内容及读取错误。
		data, readErr := io.ReadAll(io.LimitReader(file, libraryapp.MaxImageBytes+1))
		_ = file.Close()
		if readErr != nil {
			writeErrRequest(w, r, http.StatusBadRequest, "读取上传图片失败")
			return
		}
		uploads = append(uploads, libraryapp.ImageUpload{Filename: safeBaseName(header.Filename), Data: data})
	}
	// results、err 保存逐文件上传结果。
	results, err := s.libraryApplication().UploadImages(r.Context(), userID, uploads)
	if err != nil {
		writeLibraryError(w, r, err, "上传图片失败")
		return
	}
	// first 保存 firstUploadFailure 的返回结果。
	if first := firstUploadFailure(results); first != "" {
		// 全部文件失败时按请求错误返回，避免调用方把全失败当成成功。
		if allUploadsFailed(results) {
			writeErrRequest(w, r, http.StatusBadRequest, first)
			return
		}
	}
	writeJSON(w, http.StatusOK, newLibraryUploadResponse(results))
}

// firstUploadFailure 返回第一个失败文件的原因，没有失败时返回空串。
func firstUploadFailure(results []libraryapp.UploadResult) string {
	// result 是当前文件结果。
	for _, result := range results {
		if result.Error != "" {
			return result.Filename + "：" + result.Error
		}
	}
	return ""
}

// allUploadsFailed 判断是否所有文件都失败。
func allUploadsFailed(results []libraryapp.UploadResult) bool {
	// result 是当前文件结果。
	for _, result := range results {
		if result.Error == "" {
			return false
		}
	}
	return len(results) > 0
}

// getLibraryImageContent 返回图片字节；响应只对当前登录用户私有缓存，并禁止浏览器猜测内容类型。
func (s *Server) getLibraryImageContent(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// id、ok 保存路径中的图片主键。
	id, ok := libraryImageID(w, r)
	if !ok {
		return
	}
	// image、data、err 保存图片元数据、字节和读取错误。
	image, data, err := s.libraryApplication().ImageContent(r.Context(), userID, id)
	if err != nil {
		writeLibraryError(w, r, err, "读取图片失败")
		return
	}
	w.Header().Set("Content-Type", image.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// deleteLibraryImage 删除图片；仍被模板引用时返回 409。
func (s *Server) deleteLibraryImage(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// id、ok 保存路径中的图片主键。
	id, ok := libraryImageID(w, r)
	if !ok {
		return
	}
	// err 保存 libraryApplication 调用的失败原因。
	if err := s.libraryApplication().DeleteImage(r.Context(), userID, id); err != nil {
		writeLibraryError(w, r, err, "删除图片失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// listLibraryTemplates 返回当前用户的商品模板。
func (s *Server) listLibraryTemplates(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// templates、err 保存模板列表及查询错误。
	templates, err := s.libraryApplication().ListTemplates(r.Context(), userID)
	if err != nil {
		writeLibraryError(w, r, err, "查询商品模板失败")
		return
	}
	// out 保存响应 DTO，空结果返回 []。
	out := make([]libraryTemplateResponse, 0, len(templates))
	// template 是当前模板。
	for _, template := range templates {
		out = append(out, newLibraryTemplateResponse(template))
	}
	writeJSON(w, http.StatusOK, out)
}

// getLibraryTemplate 返回单个商品模板。
func (s *Server) getLibraryTemplate(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// id、ok 保存路径中的模板主键。
	id, ok := libraryTemplateID(w, r)
	if !ok {
		return
	}
	// template、err 保存模板及查询错误。
	template, err := s.libraryApplication().GetTemplate(r.Context(), userID, id)
	if err != nil {
		writeLibraryError(w, r, err, "查询商品模板失败")
		return
	}
	writeJSON(w, http.StatusOK, newLibraryTemplateResponse(template))
}

// createLibraryTemplate 创建商品模板。
func (s *Server) createLibraryTemplate(w http.ResponseWriter, r *http.Request) {
	s.saveLibraryTemplate(w, r, false)
}

// updateLibraryTemplate 覆盖商品模板。
func (s *Server) updateLibraryTemplate(w http.ResponseWriter, r *http.Request) {
	s.saveLibraryTemplate(w, r, true)
}

// saveLibraryTemplate 解析请求并创建或更新模板，两个入口共用同一套解析与错误映射。
func (s *Server) saveLibraryTemplate(w http.ResponseWriter, r *http.Request, update bool) {
	// userID、ok 保存认证会话中的当前用户。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// id 是更新目标主键，创建时为 0。
	var id int64
	if update {
		if id, ok = libraryTemplateID(w, r); !ok {
			return
		}
	}
	// req 是请求体 DTO。
	var req libraryTemplateRequest
	// decodeErr 保存 decodeJSON 调用的失败原因。
	if decodeErr := decodeJSON(r, &req); decodeErr != nil {
		writeErrRequest(w, r, http.StatusBadRequest, "请求格式错误")
		return
	}
	// template、convertErr 保存转换后的应用模板。
	template, convertErr := req.toTemplate(userID, id)
	if convertErr != nil {
		writeErrRequest(w, r, http.StatusBadRequest, convertErr.Error())
		return
	}
	// saved、err 保存写入后的模板。
	var saved libraryapp.Template
	// err 保存创建或更新模板的失败原因。
	var err error
	// status 是保存成功时返回的 HTTP 状态码，创建为 201、更新为 200。
	status := http.StatusCreated
	if update {
		saved, err = s.libraryApplication().UpdateTemplate(r.Context(), template)
		status = http.StatusOK
	} else {
		saved, err = s.libraryApplication().CreateTemplate(r.Context(), template)
	}
	if err != nil {
		writeLibraryError(w, r, err, "保存商品模板失败")
		return
	}
	writeJSON(w, status, newLibraryTemplateResponse(saved))
}

// deleteLibraryTemplate 删除商品模板。
func (s *Server) deleteLibraryTemplate(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// id、ok 保存路径中的模板主键。
	id, ok := libraryTemplateID(w, r)
	if !ok {
		return
	}
	// err 保存 libraryApplication 调用的失败原因。
	if err := s.libraryApplication().DeleteTemplate(r.Context(), userID, id); err != nil {
		writeLibraryError(w, r, err, "删除商品模板失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// previewLibraryTemplatePublish 把选中的模板展开到选中的账号并生成批量铺货预检；
// 确认后由前端调用既有的批次启动接口，worker、进度和重试完全沿用批量铺货。
func (s *Server) previewLibraryTemplatePublish(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// req 是请求体 DTO。
	var req libraryTemplatePublishRequest
	// decodeErr 保存 decodeJSON 调用的失败原因。
	if decodeErr := decodeJSON(r, &req); decodeErr != nil {
		writeErrRequest(w, r, http.StatusBadRequest, "请求格式错误")
		return
	}
	// interval 校验发布间隔范围，0 表示使用默认值。
	if req.PublishIntervalSeconds < 0 || req.PublishIntervalSeconds > 3600 {
		writeErrRequest(w, r, http.StatusBadRequest, "发布间隔必须在 0 到 3600 秒之间")
		return
	}
	// result、err 保存预检批次结果。
	result, err := s.libraryApplication().PreviewPublish(r.Context(), libraryapp.PublishPreviewInput{
		UserID: userID, TemplateIDs: req.TemplateIDs, CookieIDs: trimStrings(req.CookieIDs),
		Location: itemapp.Location{Area: req.Location.Area, City: req.Location.City, DivisionID: req.Location.DivisionID,
			Longitude: req.Location.Longitude, Latitude: req.Location.Latitude, POIID: req.Location.POIID,
			POIName: req.Location.POIName, Province: req.Location.Province},
		PublishIntervalSeconds: req.PublishIntervalSeconds,
	})
	if err != nil {
		writeLibraryError(w, r, err, "生成发布预检失败")
		return
	}
	writeJSON(w, http.StatusOK, publishBatchPreviewApplicationResponse(result))
}

// trimStrings 去除每个字符串的首尾空白。
func trimStrings(values []string) []string {
	// out 保存清理后的字符串。
	out := make([]string, 0, len(values))
	// value 是当前字符串。
	for _, value := range values {
		out = append(out, strings.TrimSpace(value))
	}
	return out
}
