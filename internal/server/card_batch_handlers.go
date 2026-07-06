package server

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

const maxCardBatchRows = 200

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
	sess := auth.SessionFromContext(r.Context())
	// 表格最大 5 MiB（卡密组定义都很小）。
	r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
	if err := r.ParseMultipartForm(6 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "解析上传文件失败")
		return
	}
	source, sourceHeader, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "缺少卡密表格文件")
		return
	}
	defer source.Close()
	sourceBytes, err := io.ReadAll(io.LimitReader(source, 5<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "读取卡密表格失败")
		return
	}
	sourceName := safeBaseName(sourceHeader.Filename)
	if sourceName == "" {
		sourceName = "cards.csv"
	}
	maps, err := parsePublishSheetBytes(sourceBytes, sourceName)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(maps) > maxCardBatchRows {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("单次最多创建 %d 个卡密组", maxCardBatchRows))
		return
	}

	results := make([]cardBatchResultRow, 0, len(maps))
	created, failed := 0, 0
	for i, m := range maps {
		rowNo := i + 2
		name := strings.TrimSpace(firstImportString(m, "name", "名称", "卡密组名称", "卡密名称"))
		cardType := strings.ToLower(strings.TrimSpace(firstImportString(m, "type", "类型", "卡密类型")))
		content := firstImportString(m, "content", "内容", "卡密内容")

		// 校验
		if name == "" {
			results = append(results, cardBatchResultRow{RowNo: rowNo, Success: false, Name: name, Type: cardType, Error: "缺少名称"})
			failed++
			continue
		}
		switch cardType {
		case "text", "data", "image", "api":
		default:
			results = append(results, cardBatchResultRow{RowNo: rowNo, Success: false, Name: name, Type: cardType, Error: "类型必须为 text/data/image/api"})
			failed++
			continue
		}
		// content 按类型映射；api 类型允许 content 为空（用 description 等另行说明时）
		if cardType != "api" && strings.TrimSpace(content) == "" {
			results = append(results, cardBatchResultRow{RowNo: rowNo, Success: false, Name: name, Type: cardType, Error: "缺少内容"})
			failed++
			continue
		}

		cf := &db.CardFull{
			Name:         name,
			Type:         cardType,
			Description:  firstImportString(m, "description", "描述"),
			Enabled:      true,
			DelaySeconds: atoiPublishDefault(firstImportString(m, "delay_seconds", "延迟秒"), 0),
			IsMultiSpec:  parseLooseBool(firstImportString(m, "is_multi_spec", "多规格")),
			SpecName:     firstImportString(m, "spec_name", "规格名"),
			SpecValue:    firstImportString(m, "spec_value", "规格值"),
			UserID:       sess.UserID,
		}
		if v := firstImportString(m, "enabled", "启用"); v != "" {
			cf.Enabled = parseLooseBool(v)
		}
		switch cardType {
		case "text":
			cf.TextContent = content
		case "data":
			cf.DataContent = content
		case "image":
			cf.ImageURL = content
		case "api":
			cf.APIConfig = content
		}

		id, err := s.Store.Cards.Create(r.Context(), cf)
		if err != nil {
			results = append(results, cardBatchResultRow{RowNo: rowNo, Success: false, Name: name, Type: cardType, Error: "创建失败: " + err.Error()})
			failed++
			continue
		}
		results = append(results, cardBatchResultRow{RowNo: rowNo, Success: true, ID: id, Name: name, Type: cardType})
		created++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"total":   len(maps),
		"created": created,
		"failed":  failed,
		"rows":    results,
	})
}

// appendCardData 往 data 类型卡密组追加卡密号（按行）。
func (s *Server) appendCardData(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeErr(w, http.StatusBadRequest, "内容为空")
		return
	}
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
	added, err := s.Store.Cards.AppendBatchData(r.Context(), id, content)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "追加失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "added": added})
}
