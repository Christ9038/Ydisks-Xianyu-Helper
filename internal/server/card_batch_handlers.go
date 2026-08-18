package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	cardsapp "xianyu-go/internal/application/cards"
	"xianyu-go/internal/auth"
)

// maxCardBatchRows 用于本次流程后续判断的max卡密批次Rows
const maxCardBatchRows = 200

// cardBatchResultRow 用于本次流程后续判断的卡密批次结果Row
type cardBatchResultRow struct {
	RowNo   int    `json:"row_no"`
	Success bool   `json:"success"`
	ID      int64  `json:"id,omitempty"`
	Name    string `json:"name"`
	Type    string `json:"type,omitempty"`
	Error   string `json:"error,omitempty"`
}

// batchCreateCards 上传表格批量创建卡密组。每行一个组定义。
func (s *Server) batchCreateCards(w http.ResponseWriter, r *http.Request) {
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// 表格最大 5 MiB（卡密组定义都很小）。
	r.Body = http.MaxBytesReader(w, r.Body, maxCardBatchUploadBytes)
	// err 表示解析 multipart 表单时遇到的格式或大小限制错误。
	if err := r.ParseMultipartForm(maxCardBatchUploadBytes); err != nil {
		writeErr(w, http.StatusBadRequest, "解析上传文件失败")
		return
	}
	// source、sourceHeader、err 用于本次流程后续判断的source、sourceHeader、err
	source, sourceHeader, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "缺少卡密表格文件")
		return
	}
	defer source.Close()
	// sourceBytes、tooLarge、err 用于本次流程后续判断的sourceBytes、tooLarge、err
	sourceBytes, tooLarge, err := readLimitedBytes(source, 5<<20)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "读取卡密表格失败")
		return
	}
	if tooLarge {
		writeErr(w, http.StatusBadRequest, "卡密表格不能超过 5 MiB")
		return
	}
	// sourceName 用于本次流程后续判断的source名称
	sourceName := safeBaseName(sourceHeader.Filename)
	if sourceName == "" {
		sourceName = "cards.csv"
	}
	// maps、err 用于本次流程后续判断的maps、err
	maps, err := parsePublishSheetBytesWithLimit(sourceBytes, sourceName, maxCardBatchRows)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(maps) > maxCardBatchRows {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("单次最多创建 %d 个卡密组", maxCardBatchRows))
		return
	}

	// results 用于本次流程后续判断的results
	results := make([]cardBatchResultRow, 0, len(maps))
	// created、failed 用于本次流程后续判断的created、failed
	created, failed := 0, 0
	// i、m 表示当前遍历过程中的i、m
	for i, m := range maps {
		// rowNo 用于本次流程后续判断的rowNo
		rowNo := i + 2
		// name 用于本次流程后续判断的名称
		name := strings.TrimSpace(firstImportString(m, "name", "名称", "卡密组名称", "卡密名称"))
		// cardType 用于本次流程后续判断的卡密类型
		cardType := strings.ToLower(strings.TrimSpace(firstImportString(m, "type", "类型", "卡密类型")))
		// content 用于本次流程后续判断的内容
		content := firstImportString(m, "content", "内容", "卡密内容")

		// 校验
		if name == "" {
			results = append(results, cardBatchResultRow{RowNo: rowNo, Success: false, Name: name, Type: cardType, Error: "缺少名称"})
			failed++
			continue
		}
		switch cardType {
		case "text", "data", "image":
		case "api":
			results = append(results, cardBatchResultRow{RowNo: rowNo, Success: false, Name: name, Type: cardType, Error: "API 卡密暂不支持自动发货，不能新建"})
			failed++
			continue
		default:
			results = append(results, cardBatchResultRow{RowNo: rowNo, Success: false, Name: name, Type: cardType, Error: "类型必须为 text/data/image"})
			failed++
			continue
		}
		if strings.TrimSpace(content) == "" {
			results = append(results, cardBatchResultRow{RowNo: rowNo, Success: false, Name: name, Type: cardType, Error: "缺少内容"})
			failed++
			continue
		}
		// delaySeconds 用于本次流程后续判断的延迟秒数
		delaySeconds := atoiPublishDefault(firstImportString(m, "delay_seconds", "延迟秒"), 0)
		if delaySeconds < 0 || delaySeconds > 3600 {
			results = append(results, cardBatchResultRow{RowNo: rowNo, Success: false, Name: name, Type: cardType, Error: "延时发货必须在 0 到 3600 秒之间"})
			failed++
			continue
		}

		// draft 保存当前表格行转换出的应用卡券草稿。
		draft := cardsapp.Draft{
			Name:         name,
			Type:         cardType,
			Description:  firstImportString(m, "description", "描述"),
			Enabled:      true,
			DelaySeconds: delaySeconds,
			IsMultiSpec:  parseLooseBool(firstImportString(m, "is_multi_spec", "多规格")),
			SpecName:     firstImportString(m, "spec_name", "规格名"),
			SpecValue:    firstImportString(m, "spec_value", "规格值"),
		}
		// v 保存表格行提供的可选启用状态文本。
		if v := firstImportString(m, "enabled", "启用"); v != "" {
			draft.Enabled = parseLooseBool(v)
		}
		switch cardType {
		case "text":
			draft.TextContent = content
		case "data":
			draft.DataContent = content
		case "image":
			draft.ImageURL = content
		}

		// id、err 保存应用服务创建的卡券标识及逐行错误。
		id, err := s.cardsApplication().Create(r.Context(), sess.UserID, draft)
		if err != nil {
			results = append(results, cardBatchResultRow{RowNo: rowNo, Success: false, Name: name, Type: cardType, Error: "创建失败: " + err.Error()})
			failed++
			continue
		}
		results = append(results, cardBatchResultRow{RowNo: rowNo, Success: true, ID: id, Name: name, Type: cardType})
		created++
	}

	// 批量响应使用具名 DTO，但保留逐行结果供客户端展示失败原因。
	// total、created 和 failed 的统计语义与旧接口保持一致。
	// rows 中的 success 仅表示对应表格行是否创建成功。
	// 卡券批量接口仍返回 HTTP 200，单行失败不改变批次级成功语义。
	// 后续版本化迁移可直接复用该字段结构。
	// 该 DTO 不暴露未使用的数据库用户字段。
	writeJSON(w, http.StatusOK, cardBatchResponse{Success: true, Total: len(maps), Created: created, Failed: failed, Rows: results})
}

// appendCardData 往 data 类型卡密组追加卡密号（按行）。
func (s *Server) appendCardData(w http.ResponseWriter, r *http.Request) {
	// sess 保存认证中间件注入的当前用户会话。
	sess := auth.SessionFromContext(r.Context())
	// id、parseErr 保存路径中的卡券标识及数字解析错误。
	id, parseErr := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if parseErr != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	// req 保存具名 DTO 解码后的追加库存请求。
	var req cardAppendRequest
	// decodeErr 表示追加请求 JSON 的解码错误。
	if decodeErr := decodeJSON(r, &req); decodeErr != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// content 保存去除首尾空白后的待追加卡密内容。
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeErr(w, http.StatusBadRequest, "内容为空")
		return
	}
	// added、err 保存应用服务追加的库存行数及业务或持久化错误。
	added, err := s.cardsApplication().AppendData(r.Context(), sess.UserID, id, content)
	if err != nil {
		// validationErr 用于识别可以直接返回客户端的稳定校验提示。
		var validationErr *cardsapp.ValidationError
		switch {
		case errors.Is(err, cardsapp.ErrNotFound):
			writeErr(w, http.StatusNotFound, "卡券不存在")
		case errors.Is(err, cardsapp.ErrForbidden):
			writeErr(w, http.StatusForbidden, "无权操作该卡密组")
		case errors.Is(err, cardsapp.ErrNotDataType), errors.As(err, &validationErr):
			writeErr(w, http.StatusBadRequest, err.Error())
		default:
			writeErr(w, http.StatusInternalServerError, "追加失败: "+err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, cardAppendResponse{Success: true, Added: added})
}
