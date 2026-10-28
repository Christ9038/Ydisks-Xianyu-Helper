package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	logsapp "xianyu-go/internal/application/logs"
	"xianyu-go/internal/auth"
)

// runtimeLogsPath 是运行日志查询接口的版本化路径。
// 该路径自身的轮询请求不会写入请求日志，避免前端刷新日志时产生自我刷屏。
const runtimeLogsPath = "/api/v1/logs/runtime"

// runtimeLogDetailDTO 是运行日志附加属性的具名响应 DTO。
type runtimeLogDetailDTO struct {
	// Key 是属性键；分组属性使用点号连接的完整前缀。
	Key string `json:"key"`
	// Value 是已经过脱敏处理的属性文本。
	Value string `json:"value"`
}

// runtimeLogEntryDTO 是单条运行日志的具名响应 DTO，不直接复用应用层模型。
type runtimeLogEntryDTO struct {
	// Seq 是记录序号，作为增量读取游标。
	Seq uint64 `json:"seq"`
	// Time 是记录产生时间的 RFC3339Nano 文本。
	Time string `json:"time"`
	// Level 是记录等级，取 debug/info/warn/error。
	Level string `json:"level"`
	// Source 是记录来源分类。
	Source string `json:"source"`
	// Message 是记录消息。
	Message string `json:"message"`
	// Method 是 HTTP 请求方法；非 HTTP 来源省略。
	Method string `json:"method,omitempty"`
	// Path 是 HTTP 请求路径或平台接口路径；缺失时省略。
	Path string `json:"path,omitempty"`
	// Status 是 HTTP 响应状态码；缺失时省略。
	Status int `json:"status,omitempty"`
	// DurationMS 是请求或调用耗时毫秒数；缺失时省略。
	DurationMS int64 `json:"duration_ms,omitempty"`
	// RequestID 是服务端请求关联标识；缺失时省略。
	RequestID string `json:"request_id,omitempty"`
	// API 是平台接口名称；非平台调用省略。
	API string `json:"api,omitempty"`
	// Category 是平台失败分类；非平台失败省略。
	Category string `json:"category,omitempty"`
	// Ret 是平台返回标记摘要；缺失时省略。
	Ret string `json:"ret,omitempty"`
	// Error 是已经过脱敏的错误文本；无错误时省略。
	Error string `json:"error,omitempty"`
	// AccountID 是记录关联的账号标识；缺失时省略。
	AccountID string `json:"account_id,omitempty"`
	// ItemID 是记录关联的商品标识；缺失时省略。
	ItemID string `json:"item_id,omitempty"`
	// OrderID 是记录关联的订单标识；缺失时省略。
	OrderID string `json:"order_id,omitempty"`
	// ChatID 是记录关联的会话标识；缺失时省略。
	ChatID string `json:"chat_id,omitempty"`
	// Caller 是写出该记录的源码位置（文件路径:行号）；缺失时省略。
	Caller string `json:"caller,omitempty"`
	// Func 是写出该记录的包名.方法名；缺失时省略。
	Func string `json:"func,omitempty"`
	// Route 是 HTTP 请求匹配的路由模板；缺失时省略。
	Route string `json:"route,omitempty"`
	// RunID 是记录关联的自动化运行标识；缺失时省略。
	RunID string `json:"run_id,omitempty"`
	// Details 是其余附加属性；没有时省略。
	Details []runtimeLogDetailDTO `json:"details,omitempty"`
}

// runtimeLogPageResponse 是运行日志查询接口的具名响应 DTO。
type runtimeLogPageResponse struct {
	// Entries 是本次返回的记录，按序号升序排列。
	Entries []runtimeLogEntryDTO `json:"entries"`
	// NextCursor 是下一次增量读取应使用的排他游标。
	NextCursor uint64 `json:"next_cursor"`
	// OldestSeq 是服务端当前最早记录的序号；没有记录时为零。
	OldestSeq uint64 `json:"oldest_seq"`
	// Dropped 是进程启动以来被覆盖的累计记录数。
	Dropped uint64 `json:"dropped"`
	// Capacity 是服务端可保留的最大记录数。
	Capacity int `json:"capacity"`
}

// mountVersionedRuntimeLogRoutes 挂载管理员专用的运行日志查询入口。
func (s *Server) mountVersionedRuntimeLogRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)
		r.Use(auth.RequireAdmin)
		r.Get(runtimeLogsPath, s.listRuntimeLogs)
	})
}

// listRuntimeLogs 返回管理员可查询的最近运行日志。
func (s *Server) listRuntimeLogs(w http.ResponseWriter, r *http.Request) {
	// runtimeLogs 是 transport 注入的运行日志查询用例。
	runtimeLogs := s.runtimeLogApplication()
	if runtimeLogs == nil {
		writeErr(w, http.StatusServiceUnavailable, "运行日志服务未启用")
		return
	}
	// query、parseErr 分别是解析后的查询条件与参数错误。
	query, parseErr := runtimeLogQueryFromRequest(r)
	if parseErr != nil {
		writeErr(w, http.StatusBadRequest, parseErr.Error())
		return
	}
	// page、queryErr 分别是查询结果与执行错误。
	page, queryErr := runtimeLogs.Query(query)
	if queryErr != nil {
		// 等级过滤值是唯一由用户输入决定的失败条件，其余失败按服务端错误处理。
		if errors.Is(queryErr, logsapp.ErrInvalidLevel) {
			writeErr(w, http.StatusBadRequest, "日志等级无效")
			return
		}
		writeErr(w, http.StatusInternalServerError, "查询运行日志失败")
		return
	}
	writeJSON(w, http.StatusOK, newRuntimeLogPageResponse(page))
}

// runtimeLogApplication 返回已装配的运行日志查询用例；缺失时返回 nil。
func (s *Server) runtimeLogApplication() RuntimeLogsPort {
	if s == nil || s.applications == nil {
		return nil
	}
	return s.applications.runtimeLogs
}

// runtimeLogQueryFromRequest 把查询参数解析为应用层查询条件。
func runtimeLogQueryFromRequest(r *http.Request) (logsapp.Query, error) {
	// values 是当前请求的查询参数集合。
	values := r.URL.Query()
	// query 保存解析后的查询条件，等级与关键字直接透传由应用层校验。
	query := logsapp.Query{MinLevel: values.Get("level"), Keyword: values.Get("keyword")}
	// rawSince 是排他游标文本；空值表示从头读取当前保留的全部记录。
	rawSince := strings.TrimSpace(values.Get("since"))
	if rawSince != "" {
		// since、sinceErr 分别是游标解析结果与失败原因。
		since, sinceErr := strconv.ParseUint(rawSince, 10, 64)
		if sinceErr != nil {
			return logsapp.Query{}, errors.New("日志游标无效")
		}
		query.Since = since
	}
	// rawLimit 是条数上限文本；空值由应用层使用默认上限。
	rawLimit := strings.TrimSpace(values.Get("limit"))
	if rawLimit != "" {
		// limit、limitErr 分别是条数解析结果与失败原因。
		limit, limitErr := strconv.Atoi(rawLimit)
		if limitErr != nil || limit <= 0 {
			return logsapp.Query{}, errors.New("日志条数上限无效")
		}
		query.Limit = limit
	}
	query.Sources = parseRuntimeLogSources(values["source"])
	return query, nil
}

// parseRuntimeLogSources 解析来源过滤参数，同时支持重复参数与逗号分隔写法。
func parseRuntimeLogSources(rawValues []string) []string {
	if len(rawValues) == 0 {
		return nil
	}
	// sources 保存展开后的来源过滤项。
	sources := make([]string, 0, len(rawValues))
	// raw 是当前待展开的来源参数值。
	for _, raw := range rawValues {
		// part 是逗号分隔后的单个来源名。
		for _, part := range strings.Split(raw, ",") {
			// trimmed 是去除首尾空白后的来源名。
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			sources = append(sources, trimmed)
		}
	}
	if len(sources) == 0 {
		return nil
	}
	return sources
}

// newRuntimeLogPageResponse 把应用层查询结果转换为 HTTP 响应 DTO。
func newRuntimeLogPageResponse(page logsapp.Page) runtimeLogPageResponse {
	// entries 保存转换后的日志条目；空结果保持为空切片以便序列化为 JSON 数组。
	entries := make([]runtimeLogEntryDTO, 0, len(page.Entries))
	// record 是当前待转换的应用层记录。
	for _, record := range page.Entries {
		entries = append(entries, newRuntimeLogEntryDTO(record))
	}
	return runtimeLogPageResponse{
		Entries:    entries,
		NextCursor: page.NextCursor,
		OldestSeq:  page.OldestSeq,
		Dropped:    page.Dropped,
		Capacity:   page.Capacity,
	}
}

// newRuntimeLogEntryDTO 把一条应用层运行日志转换为 HTTP 响应 DTO。
func newRuntimeLogEntryDTO(record logsapp.Record) runtimeLogEntryDTO {
	// entry 是转换结果；时间统一输出为 RFC3339Nano 文本。
	entry := runtimeLogEntryDTO{
		Seq:        record.Seq,
		Time:       record.Time.Format(time.RFC3339Nano),
		Level:      string(record.Level),
		Source:     string(record.Source),
		Message:    record.Message,
		Method:     record.Method,
		Path:       record.Path,
		Status:     record.Status,
		DurationMS: record.DurationMS,
		RequestID:  record.RequestID,
		API:        record.API,
		Category:   record.Category,
		Ret:        record.Ret,
		Error:      record.Error,
		AccountID:  record.AccountID,
		ItemID:     record.ItemID,
		OrderID:    record.OrderID,
		ChatID:     record.ChatID,
		Caller:     record.Caller,
		Func:       record.Func,
		Route:      record.Route,
		RunID:      record.RunID,
	}
	if len(record.Details) == 0 {
		return entry
	}
	// details 保存转换后的附加属性。
	details := make([]runtimeLogDetailDTO, 0, len(record.Details))
	// detail 是当前待转换的附加属性。
	for _, detail := range record.Details {
		details = append(details, runtimeLogDetailDTO{Key: detail.Key, Value: detail.Value})
	}
	entry.Details = details
	return entry
}
