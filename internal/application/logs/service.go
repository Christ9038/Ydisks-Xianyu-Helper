// Package logs 提供运行日志查询用例。
// 本包只依赖消费者定义的 Port，不感知 HTTP、数据库模型或具体日志实现。
package logs

import (
	"errors"
	"strings"
	"time"
)

// DefaultLimit 是未指定条数时单次返回的最大日志条数。
const DefaultLimit = 200

// MaxLimit 是单次返回日志条数的硬上限，避免一次响应过大。
const MaxLimit = 500

// ErrInvalidLevel 表示调用方提供了不受支持的日志等级过滤值。
var ErrInvalidLevel = errors.New("运行日志等级无效")

// Level 是运行日志对外使用的稳定等级名称。
type Level string

const (
	// LevelDebug 表示调试等级记录，只有把日志等级调至 debug 才会产生。
	LevelDebug Level = "debug"
	// LevelInfo 表示信息等级记录。
	LevelInfo Level = "info"
	// LevelWarn 表示警告等级记录。
	LevelWarn Level = "warn"
	// LevelError 是错误等级记录，包含全部接口失败。
	LevelError Level = "error"
)

// Source 是运行日志的来源分类，用于在界面上区分内部接口、平台接口和后台子系统。
type Source string

const (
	// SourceHTTP 表示服务端 HTTP 接口请求。
	SourceHTTP Source = "http"
	// SourceMTop 表示闲鱼 MTOP 平台接口调用。
	SourceMTop Source = "mtop"
	// SourceAutomation 表示自动化中心与发货规则运行。
	SourceAutomation Source = "automation"
	// SourceEngine 表示账号消息运行时、回复与 AI 引擎。
	SourceEngine Source = "engine"
	// SourceNotify 表示通知投递。
	SourceNotify Source = "notify"
	// SourceWebSocket 表示平台 WebSocket 连接诊断。
	SourceWebSocket Source = "ws"
	// SourceRenewal 表示凭证续期任务。
	SourceRenewal Source = "renewal"
	// SourceOther 表示尚未归类的其他运行日志。
	SourceOther Source = "other"
)

// Detail 是未升为具名字段的附加日志属性。
type Detail struct {
	// Key 是属性键；分组属性使用点号连接的完整前缀。
	Key string
	// Value 是已经过脱敏处理的属性文本。
	Value string
}

// Record 是应用层读取的一条已脱敏运行日志。
// 具名字段覆盖界面上需要直接展示的常见属性，其余属性保留在 Details 中。
type Record struct {
	// Seq 是来源内单调递增的记录序号，用作增量读取游标。
	Seq uint64
	// Time 是记录产生时间。
	Time time.Time
	// Level 是记录等级。
	Level Level
	// Source 是记录来源分类。
	Source Source
	// Message 是记录消息。
	Message string
	// Method 是 HTTP 请求方法；非 HTTP 来源为空。
	Method string
	// Path 是 HTTP 请求路径或 MTOP 接口路径；缺失时为空。
	Path string
	// Status 是 HTTP 响应状态码；缺失时为零。
	Status int
	// DurationMS 是请求或调用耗时毫秒数；缺失时为零。
	DurationMS int64
	// RequestID 是服务端请求关联标识；缺失时为空。
	RequestID string
	// API 是平台接口名称；非平台调用为空。
	API string
	// Category 是平台失败分类；非平台失败为空。
	Category string
	// Ret 是平台返回标记摘要；缺失时为空。
	Ret string
	// Error 是已经过脱敏的错误文本；无错误时为空。
	Error string
	// AccountID 是记录关联的账号标识；缺失时为空。
	AccountID string
	// ItemID 是记录关联的商品标识；缺失时为空。
	ItemID string
	// OrderID 是记录关联的订单标识；缺失时为空。
	OrderID string
	// ChatID 是记录关联的会话标识；缺失时为空。
	ChatID string
	// Caller 是写出该记录的源码位置（相对 internal 的文件路径:行号）；缺失时为空。
	Caller string
	// Func 是写出该记录的包名.方法名；缺失时为空。
	Func string
	// Route 是 HTTP 请求匹配的路由模板；非 HTTP 来源为空。
	Route string
	// RunID 是记录关联的自动化运行标识；缺失时为空。
	RunID string
	// Details 是其余附加属性，数量与单值长度都由来源限制。
	Details []Detail
}

// Snapshot 是日志来源在某一时刻的一致快照，边界值与记录来自同一次读取。
type Snapshot struct {
	// Records 是按序号升序排列的记录，每条记录序号都严格大于调用方传入的 since。
	Records []Record
	// OldestSeq 是快照时刻来源中最早记录的序号；无记录时为零。
	OldestSeq uint64
	// NextSeq 是快照时刻最后一条记录的序号；无记录时为零。
	NextSeq uint64
	// Dropped 是来源启动以来因容量上限被覆盖的累计记录数。
	Dropped uint64
	// Capacity 是来源可保留的最大记录数。
	Capacity int
}

// Reader 定义运行日志查询用例所需的只读日志来源能力。
type Reader interface {
	// Snapshot 返回序号严格大于 since 的全部记录及来源边界；实现必须保证边界与记录一致。
	Snapshot(since uint64) Snapshot
}

// Query 是运行日志查询条件。
type Query struct {
	// Since 是排他游标，只返回序号大于该值的记录；零值表示读取来源当前保留的最近 limit 条记录。
	// 当取值大于来源末尾（例如服务端缓冲已重启）时按零值处理，避免调用方长期读到空页。
	Since uint64
	// Limit 是本次最多返回的记录数；非正数使用 DefaultLimit，超过 MaxLimit 时收敛到上限。
	Limit int
	// MinLevel 是最低等级过滤，取 debug/info/warn/error；空值表示不过滤。
	MinLevel string
	// Sources 是来源过滤集合；空集合表示不过滤。
	Sources []string
	// Keyword 是消息与关键属性文本的包含过滤；空值表示不过滤。
	Keyword string
}

// Page 是运行日志查询结果。
type Page struct {
	// Entries 是本次返回的记录，按序号升序排列。
	Entries []Record
	// NextCursor 是下一次增量读取应使用的排他游标；来源重启后会回到较小序号，调用方必须无条件采用。
	NextCursor uint64
	// OldestSeq 是来源当前最早记录的序号；无记录时为零。
	OldestSeq uint64
	// Dropped 是来源启动以来被覆盖的累计记录数，用于提示历史记录已不完整。
	Dropped uint64
	// Capacity 是来源可保留的最大记录数。
	Capacity int
}

// Service 编排运行日志的读取、过滤与游标推进。
type Service struct {
	// source 是消费者注入的只读日志来源；为空时所有查询返回空页。
	source Reader
}

// NewService 构造运行日志查询服务；source 为空时服务仍然可用并返回空页。
func NewService(source Reader) *Service {
	return &Service{source: source}
}

// Query 按条件读取运行日志；游标只按已返回记录推进，保证前端不会丢失中间记录。
func (s *Service) Query(query Query) (Page, error) {
	// minLevel 是解析后的最低等级阈值；无效过滤值直接返回错误而不静默放行。
	minLevel, err := parseMinLevel(query.MinLevel)
	if err != nil {
		return Page{}, err
	}
	if s == nil || s.source == nil {
		return Page{}, nil
	}
	// snapshot 是来源在本次查询时刻的一致快照。
	snapshot := s.source.Snapshot(query.Since)
	// since 是实际生效的排他游标；大于来源末尾说明来源已被重启或替换，必须按零游标重新读取。
	since := query.Since
	if since > snapshot.NextSeq {
		since = 0
		snapshot = s.source.Snapshot(0)
	}
	// limit 是归一化后的返回条数上限。
	limit := normalizeLimit(query.Limit)
	// sources 是归一化后的来源过滤集合；空集合表示不过滤。
	sources := normalizeSources(query.Sources)
	// keyword 是去除首尾空白后的关键字过滤值。
	keyword := strings.TrimSpace(query.Keyword)
	// matched 保存满足全部过滤条件的记录，供首次读取时再取最近的一批。
	matched := make([]Record, 0, len(snapshot.Records))
	// record 是当前待过滤的来源记录。
	for _, record := range snapshot.Records {
		// 来源已经按游标过滤；这里再次校验，避免实现缺陷把旧记录混入增量结果。
		if record.Seq <= since {
			continue
		}
		if !matchesLevel(record.Level, minLevel) {
			continue
		}
		if !matchesSource(record.Source, sources) {
			continue
		}
		if !matchesKeyword(record, keyword) {
			continue
		}
		matched = append(matched, record)
	}
	// entries 是本次返回的记录；首次读取取最近 limit 条，增量读取按序号从旧到新取前 limit 条。
	entries := matched
	if len(matched) > limit {
		if since == 0 {
			entries = matched[len(matched)-limit:]
		} else {
			entries = matched[:limit]
		}
	}
	// nextCursor 是下一次增量读取使用的排他游标；没有匹配记录时推进到来源末尾，且不早于生效游标。
	nextCursor := since
	if len(entries) > 0 {
		nextCursor = entries[len(entries)-1].Seq
	} else if snapshot.NextSeq > nextCursor {
		nextCursor = snapshot.NextSeq
	}
	return Page{
		Entries:    entries,
		NextCursor: nextCursor,
		OldestSeq:  snapshot.OldestSeq,
		Dropped:    snapshot.Dropped,
		Capacity:   snapshot.Capacity,
	}, nil
}

// parseMinLevel 把过滤文本解析为等级阈值；空值表示不过滤，即从 debug 起放行。
func parseMinLevel(raw string) (Level, error) {
	switch Level(strings.ToLower(strings.TrimSpace(raw))) {
	case "":
		return LevelDebug, nil
	case LevelDebug:
		return LevelDebug, nil
	case LevelInfo:
		return LevelInfo, nil
	case LevelWarn:
		return LevelWarn, nil
	case LevelError:
		return LevelError, nil
	default:
		return "", ErrInvalidLevel
	}
}

// normalizeLimit 归一化返回条数上限，非正数使用默认值，超大值收敛到硬上限。
func normalizeLimit(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}

// normalizeSources 归一化来源过滤集合，忽略空白项并对重复项去重。
func normalizeSources(sources []string) map[Source]struct{} {
	if len(sources) == 0 {
		return nil
	}
	// normalized 保存去重后的来源集合；空集合按不过滤处理。
	normalized := make(map[Source]struct{}, len(sources))
	// raw 是当前待归一化的来源过滤项。
	for _, raw := range sources {
		// value 是去除首尾空白并统一小写后的来源名。
		value := strings.ToLower(strings.TrimSpace(raw))
		if value == "" {
			continue
		}
		normalized[Source(value)] = struct{}{}
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// matchesLevel 判断记录等级是否达到最低等级阈值。
func matchesLevel(record Level, minLevel Level) bool {
	return levelRank(record) >= levelRank(minLevel)
}

// levelRank 返回等级的排序权重，未知等级按最低权重处理以免被静默丢弃。
func levelRank(level Level) int {
	switch level {
	case LevelError:
		return 3
	case LevelWarn:
		return 2
	case LevelInfo:
		return 1
	default:
		return 0
	}
}

// matchesSource 判断记录来源是否命中过滤集合；空集合表示全部命中。
func matchesSource(source Source, sources map[Source]struct{}) bool {
	if sources == nil {
		return true
	}
	// ok 表示记录来源是否命中过滤集合。
	_, ok := sources[source]
	return ok
}

// matchesKeyword 判断记录的消息与关键属性文本是否包含关键字；空关键字表示全部命中。
func matchesKeyword(record Record, keyword string) bool {
	if keyword == "" {
		return true
	}
	// needle 是统一小写后的关键字，保证匹配与大小写无关。
	needle := strings.ToLower(keyword)
	// haystack 拼接消息与界面关注的关键属性文本，覆盖接口名、错误和路径。
	haystack := strings.ToLower(strings.Join([]string{
		record.Message, record.Method, record.Path, record.API,
		record.Category, record.Ret, record.Error,
	}, "\n"))
	if strings.Contains(haystack, needle) {
		return true
	}
	// detail 是当前待匹配的附加属性。
	for _, detail := range record.Details {
		if strings.Contains(strings.ToLower(detail.Value), needle) {
			return true
		}
	}
	return false
}
