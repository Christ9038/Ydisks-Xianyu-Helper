package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// maxCardBatchRows 保存max卡密批次Rows，供当前处理流程使用
const maxCardBatchRows = 200

// cardBatchResultRow 保存卡密批次结果Row，供当前处理流程使用
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
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// 表格最大 5 MiB（卡密组定义都很小）。
	r.Body = http.MaxBytesReader(w, r.Body, maxCardBatchUploadBytes)
	if // err 保存err，供当前处理流程使用
	err := r.ParseMultipartForm(maxCardBatchUploadBytes); err != nil {
		writeErr(w, http.StatusBadRequest, "解析上传文件失败")
		return
	}
	// source、sourceHeader、err 保存source、sourceHeader、err，供当前处理流程使用
	source, sourceHeader, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "缺少卡密表格文件")
		return
	}
	defer source.Close()
	// sourceBytes、tooLarge、err 保存sourceBytes、tooLarge、err，供当前处理流程使用
	sourceBytes, tooLarge, err := readLimitedBytes(source, 5<<20)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "读取卡密表格失败")
		return
	}
	if tooLarge {
		writeErr(w, http.StatusBadRequest, "卡密表格不能超过 5 MiB")
		return
	}
	// sourceName 保存source名称，供当前处理流程使用
	sourceName := safeBaseName(sourceHeader.Filename)
	if sourceName == "" {
		sourceName = "cards.csv"
	}
	// maps、err 保存maps、err，供当前处理流程使用
	maps, err := parsePublishSheetBytesWithLimit(sourceBytes, sourceName, maxCardBatchRows)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(maps) > maxCardBatchRows {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("单次最多创建 %d 个卡密组", maxCardBatchRows))
		return
	}

	// results 保存results，供当前处理流程使用
	results := make([]cardBatchResultRow, 0, len(maps))
	// created、failed 保存created、failed，供当前处理流程使用
	created, failed := 0, 0
	// i、m 表示当前遍历过程中的i、m
	for i, m := range maps {
		// rowNo 保存rowNo，供当前处理流程使用
		rowNo := i + 2
		// name 保存名称，供当前处理流程使用
		name := strings.TrimSpace(firstImportString(m, "name", "名称", "卡密组名称", "卡密名称"))
		// cardType 保存卡密类型，供当前处理流程使用
		cardType := strings.ToLower(strings.TrimSpace(firstImportString(m, "type", "类型", "卡密类型")))
		// content 保存内容，供当前处理流程使用
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
		// delaySeconds 保存延迟秒数，供当前处理流程使用
		delaySeconds := atoiPublishDefault(firstImportString(m, "delay_seconds", "延迟秒"), 0)
		if delaySeconds < 0 || delaySeconds > 3600 {
			results = append(results, cardBatchResultRow{RowNo: rowNo, Success: false, Name: name, Type: cardType, Error: "延时发货必须在 0 到 3600 秒之间"})
			failed++
			continue
		}

		// cf 保存cf，供当前处理流程使用
		cf := &db.CardFull{
			Name:         name,
			Type:         cardType,
			Description:  firstImportString(m, "description", "描述"),
			Enabled:      true,
			DelaySeconds: delaySeconds,
			IsMultiSpec:  parseLooseBool(firstImportString(m, "is_multi_spec", "多规格")),
			SpecName:     firstImportString(m, "spec_name", "规格名"),
			SpecValue:    firstImportString(m, "spec_value", "规格值"),
			UserID:       sess.UserID,
		}
		if // v 保存v，供当前处理流程使用
		v := firstImportString(m, "enabled", "启用"); v != "" {
			cf.Enabled = parseLooseBool(v)
		}
		switch cardType {
		case "text":
			cf.TextContent = content
		case "data":
			cf.DataContent = content
		case "image":
			cf.ImageURL = content
		}

		// id、err 保存id、err，供当前处理流程使用
		id, err := s.Store.Cards.Create(r.Context(), cf)
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
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// id、err 保存id、err，供当前处理流程使用
	id, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	// req 保存req，供当前处理流程使用
	var req struct {
		Content string `json:"content"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// content 保存内容，供当前处理流程使用
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeErr(w, http.StatusBadRequest, "内容为空")
		return
	}
	// cf、err 保存cf、err，供当前处理流程使用
	cf, err := s.Store.Cards.Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "卡券不存在")
		return
	}
	if cf.UserID != sess.UserID {
		writeErr(w, http.StatusForbidden, "无权操作该卡密组")
		return
	}
	if cf.Type != "data" {
		writeErr(w, http.StatusBadRequest, "只有 data（批量卡密）类型支持追加卡密")
		return
	}
	// added、err 保存added、err，供当前处理流程使用
	added, err := s.Store.Cards.AppendBatchData(r.Context(), id, content)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "追加失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cardAppendResponse{Success: true, Added: added})
}
