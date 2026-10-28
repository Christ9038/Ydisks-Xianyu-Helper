package adapter

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	logsapp "xianyu-go/internal/application/logs"
	"xianyu-go/internal/logging"
)

// maxRuntimeLogDetails 是单条日志保留的附加属性数量上限，避免异常记录放大响应体积。
const maxRuntimeLogDetails = 20

// maxRuntimeLogDetailRunes 是单条附加属性值保留的最大字符数，超出部分被截断。
const maxRuntimeLogDetailRunes = 512

// knownRuntimeLogSources 是来源分类的完整取值集合，用于校验显式来源标记。
var knownRuntimeLogSources = map[string]logsapp.Source{
	"http":       logsapp.SourceHTTP,
	"mtop":       logsapp.SourceMTop,
	"automation": logsapp.SourceAutomation,
	"engine":     logsapp.SourceEngine,
	"notify":     logsapp.SourceNotify,
	"ws":         logsapp.SourceWebSocket,
	"renewal":    logsapp.SourceRenewal,
	"other":      logsapp.SourceOther,
}

// RuntimeLogSource 把进程内运行时日志缓冲适配为运行日志查询用例的只读来源。
// 它只读取已经完成脱敏的记录，不接触凭证、数据库或 HTTP 层。
type RuntimeLogSource struct {
	// buffer 是进程级运行时日志缓冲；为空时所有快照都返回空结果。
	buffer *logging.RuntimeBuffer
}

// NewRuntimeLogSource 构造运行日志来源适配器；buffer 为空时适配器仍可用并返回空快照。
func NewRuntimeLogSource(buffer *logging.RuntimeBuffer) *RuntimeLogSource {
	return &RuntimeLogSource{buffer: buffer}
}

// Snapshot 读取缓冲快照并转换为应用层记录，同时透传缓冲区边界值。
func (s *RuntimeLogSource) Snapshot(since uint64) logsapp.Snapshot {
	if s == nil || s.buffer == nil {
		return logsapp.Snapshot{}
	}
	// raw 是缓冲在本次读取时刻的一致快照。
	raw := s.buffer.Snapshot(since)
	// records 保存转换后的应用层记录，容量与原始快照一致。
	records := make([]logsapp.Record, 0, len(raw.Entries))
	// entry 是当前待转换的已脱敏缓冲记录。
	for _, entry := range raw.Entries {
		records = append(records, newRuntimeLogRecord(entry))
	}
	return logsapp.Snapshot{
		Records:   records,
		OldestSeq: raw.OldestSeq,
		NextSeq:   raw.NextSeq,
		Dropped:   raw.Dropped,
		Capacity:  raw.Capacity,
	}
}

// newRuntimeLogRecord 把一条已脱敏缓冲记录转换为应用层记录。
// 已知属性键升为具名字段，其余属性在数量上限内保留到 Details。
// 来源分类只读取本系统写入的 log_source 键，避免与既有的 source 诊断属性同名冲突。
func newRuntimeLogRecord(entry logging.RuntimeEntry) logsapp.Record {
	// record 是转换结果；来源字段在遍历完成后统一推导。
	record := logsapp.Record{
		Seq:     entry.Seq,
		Time:    entry.Time,
		Level:   runtimeLogLevel(entry.Level),
		Message: entry.Message,
	}
	// explicitSource 保存显式来源标记；未标记时由子系统属性推导。
	explicitSource := ""
	// subsystem 保存子系统属性值，用于推导未显式标记的来源。
	subsystem := ""
	// attr 是当前待提取的记录属性。
	for _, attr := range entry.Attrs {
		switch attr.Key {
		case "log_source":
			explicitSource = attr.Value.String()
		case "subsys":
			subsystem = attr.Value.String()
		case "method":
			record.Method = runtimeLogAttrText(attr.Value)
		case "path":
			record.Path = runtimeLogAttrText(attr.Value)
		case "status":
			record.Status = runtimeLogInt(attr.Value)
		case "duration":
			record.DurationMS = runtimeLogDurationMS(attr.Value)
		case "request_id":
			record.RequestID = runtimeLogAttrText(attr.Value)
		case "api":
			record.API = runtimeLogAttrText(attr.Value)
		case "category":
			record.Category = runtimeLogAttrText(attr.Value)
		case "ret":
			record.Ret = runtimeLogAttrText(attr.Value)
		case "err":
			record.Error = runtimeLogAttrText(attr.Value)
		case "account", "account_id", "cookie_id":
			record.AccountID = runtimeLogAttrText(attr.Value)
		case "item_id":
			record.ItemID = runtimeLogAttrText(attr.Value)
		case "order_id":
			record.OrderID = runtimeLogAttrText(attr.Value)
		case "chat_id":
			record.ChatID = runtimeLogAttrText(attr.Value)
		case logging.CallerKey:
			record.Caller = runtimeLogAttrText(attr.Value)
		case logging.FuncKey:
			record.Func = runtimeLogAttrText(attr.Value)
		case "route":
			record.Route = runtimeLogAttrText(attr.Value)
		case "run_id":
			record.RunID = runtimeLogAttrText(attr.Value)
		default:
			if len(record.Details) < maxRuntimeLogDetails {
				record.Details = append(record.Details, logsapp.Detail{Key: attr.Key, Value: runtimeLogAttrText(attr.Value)})
			}
		}
	}
	record.Source = runtimeLogSource(explicitSource, subsystem)
	return record
}

// runtimeLogLevel 把 slog 等级映射为对外稳定等级名称；高于 error 的自定义等级归入 error。
func runtimeLogLevel(level slog.Level) logsapp.Level {
	switch {
	case level >= slog.LevelError:
		return logsapp.LevelError
	case level >= slog.LevelWarn:
		return logsapp.LevelWarn
	case level >= slog.LevelInfo:
		return logsapp.LevelInfo
	default:
		return logsapp.LevelDebug
	}
}

// runtimeLogSource 计算记录来源：显式标记优先，其次按子系统属性推导，最后归入 other。
func runtimeLogSource(explicit string, subsystem string) logsapp.Source {
	// known 是显式来源标记匹配到的已知分类。
	if known, ok := knownRuntimeLogSources[strings.ToLower(strings.TrimSpace(explicit))]; ok {
		return known
	}
	switch strings.ToLower(strings.TrimSpace(subsystem)) {
	case "automation":
		return logsapp.SourceAutomation
	case "ai", "reply":
		return logsapp.SourceEngine
	case "notify":
		return logsapp.SourceNotify
	case "ws":
		return logsapp.SourceWebSocket
	case "renewal":
		return logsapp.SourceRenewal
	default:
		return logsapp.SourceOther
	}
}

// runtimeLogInt 把属性值解析为整数；无法解析时返回零，不影响其余字段。
func runtimeLogInt(value slog.Value) int {
	switch value.Kind() {
	case slog.KindInt64:
		return int(value.Int64())
	case slog.KindUint64:
		return int(value.Uint64())
	case slog.KindFloat64:
		return int(value.Float64())
	case slog.KindString:
		// parsed、err 分别是字符串属性的整数解析结果和失败原因。
		parsed, err := strconv.Atoi(strings.TrimSpace(value.String()))
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

// runtimeLogDurationMS 把属性值解析为毫秒数；duration 类型按实际时长换算，其余按数字或字符串解析。
func runtimeLogDurationMS(value slog.Value) int64 {
	switch value.Kind() {
	case slog.KindDuration:
		return value.Duration().Milliseconds()
	case slog.KindInt64:
		return value.Int64()
	case slog.KindUint64:
		return int64(value.Uint64())
	case slog.KindFloat64:
		return int64(value.Float64())
	case slog.KindString:
		// parsed、err 分别是字符串属性的毫秒解析结果和失败原因。
		parsed, err := strconv.ParseInt(strings.TrimSpace(value.String()), 10, 64)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

// runtimeLogAttrText 把属性值渲染为稳定的展示文本，并按上限截断超长内容。
func runtimeLogAttrText(value slog.Value) string {
	switch value.Kind() {
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindTime:
		return value.Time().Format(time.RFC3339Nano)
	default:
		return truncateRuntimeLogText(value.String())
	}
}

// truncateRuntimeLogText 按字符数截断文本，避免多字节字符被从中间切断。
func truncateRuntimeLogText(text string) string {
	// runes 是文本的字符序列，用于按字符而非字节计算长度。
	runes := []rune(text)
	if len(runes) <= maxRuntimeLogDetailRunes {
		return text
	}
	return string(runes[:maxRuntimeLogDetailRunes]) + "…"
}
