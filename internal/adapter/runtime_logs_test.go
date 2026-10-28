package adapter

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	logsapp "xianyu-go/internal/application/logs"
	"xianyu-go/internal/logging"
)

// TestRuntimeLogSourceSnapshotConvertsEntries 验证缓冲记录到应用层记录的完整映射。
func TestRuntimeLogSourceSnapshotConvertsEntries(t *testing.T) {
	// buffer 是写入两条记录的进程内运行时缓冲。
	buffer := logging.NewRuntimeBuffer(4)
	buffer.Append(time.Unix(10, 0), slog.LevelWarn, "HTTP 请求完成",
		[]slog.Attr{
			slog.String("log_source", "http"),
			slog.String("method", "POST"),
			slog.String("path", "/api/v1/orders"),
			slog.Int("status", 404),
			slog.Duration("duration", 1500*time.Millisecond),
			slog.String("request_id", "req-1"),
			slog.String("err", "请求失败"),
			slog.String("account", "acc-1"),
			slog.String("item_id", "item-1"),
			slog.String("order_id", "order-1"),
			slog.String("chat_id", "chat-1"),
			slog.String("caller", "server/server.go:260"),
			slog.String("func", "server.(*Server).requestLogger"),
			slog.String("route", "/api/v1/orders"),
			slog.String("run_id", "run-1"),
			slog.String("legacy_api", "true"),
		})
	buffer.Append(time.Unix(11, 0), slog.LevelError, "MTOP 响应失败",
		[]slog.Attr{
			slog.String("log_source", "mtop"),
			slog.String("api", "mtop.trade.order.render"),
			slog.String("category", "session_expired"),
			slog.String("ret", "FAIL_SYS_SESSION_EXPIRED"),
			slog.String("http_status", "200"),
		})
	// source 是待验证的适配器实例。
	source := NewRuntimeLogSource(buffer)
	// snapshot 是适配器转换后的应用层快照。
	snapshot := source.Snapshot(0)
	if len(snapshot.Records) != 2 || snapshot.NextSeq != 2 || snapshot.Capacity != 4 {
		t.Fatalf("快照边界异常: %+v", snapshot)
	}
	// httpRecord 是第一条 HTTP 记录。
	httpRecord := snapshot.Records[0]
	if httpRecord.Source != logsapp.SourceHTTP || httpRecord.Level != logsapp.LevelWarn {
		t.Fatalf("HTTP 记录来源或等级异常: %+v", httpRecord)
	}
	if httpRecord.Method != "POST" || httpRecord.Path != "/api/v1/orders" || httpRecord.Status != 404 {
		t.Fatalf("HTTP 记录请求字段异常: %+v", httpRecord)
	}
	if httpRecord.DurationMS != 1500 || httpRecord.RequestID != "req-1" || httpRecord.Error != "请求失败" {
		t.Fatalf("HTTP 记录诊断字段异常: %+v", httpRecord)
	}
	if httpRecord.AccountID != "acc-1" || httpRecord.ItemID != "item-1" || httpRecord.OrderID != "order-1" || httpRecord.ChatID != "chat-1" {
		t.Fatalf("HTTP 记录业务标识异常: %+v", httpRecord)
	}
	if httpRecord.Caller != "server/server.go:260" || httpRecord.Func != "server.(*Server).requestLogger" || httpRecord.Route != "/api/v1/orders" || httpRecord.RunID != "run-1" {
		t.Fatalf("HTTP 记录调用位置与关联字段异常: %+v", httpRecord)
	}
	if len(httpRecord.Details) != 1 || httpRecord.Details[0].Key != "legacy_api" {
		t.Fatalf("未识别属性应进入 Details: %+v", httpRecord.Details)
	}
	// mtopRecord 是第二条平台接口记录。
	mtopRecord := snapshot.Records[1]
	if mtopRecord.Source != logsapp.SourceMTop || mtopRecord.Level != logsapp.LevelError {
		t.Fatalf("平台记录来源或等级异常: %+v", mtopRecord)
	}
	if mtopRecord.API != "mtop.trade.order.render" || mtopRecord.Category != "session_expired" || mtopRecord.Ret != "FAIL_SYS_SESSION_EXPIRED" {
		t.Fatalf("平台记录失败字段异常: %+v", mtopRecord)
	}
	if len(mtopRecord.Details) != 1 || mtopRecord.Details[0].Key != "http_status" {
		t.Fatalf("平台记录附加属性异常: %+v", mtopRecord.Details)
	}
}

// TestRuntimeLogSourceHandlesEmptyBufferAndCursor 验证空缓冲、游标过滤和空适配器行为。
func TestRuntimeLogSourceHandlesEmptyBufferAndCursor(t *testing.T) {
	// emptySource 是没有注入缓冲的适配器，所有方法都必须安全返回空快照。
	emptySource := NewRuntimeLogSource(nil)
	if len(emptySource.Snapshot(0).Records) != 0 {
		t.Fatal("缺少缓冲时不应返回记录")
	}
	// nilSource 是未初始化的适配器接收者。
	var nilSource *RuntimeLogSource
	if len(nilSource.Snapshot(0).Records) != 0 {
		t.Fatal("空接收者不应返回记录")
	}
	// buffer 是写入三条记录的缓冲，用于验证游标过滤。
	buffer := logging.NewRuntimeBuffer(4)
	// index 表示当前写入的记录序号，取值 1 到 3。
	for index := 1; index <= 3; index++ {
		buffer.Append(time.Unix(int64(index), 0), slog.LevelInfo, "记录", nil)
	}
	// filtered 是从序号 1 之后读取的增量快照。
	filtered := NewRuntimeLogSource(buffer).Snapshot(1)
	if len(filtered.Records) != 2 || filtered.Records[0].Seq != 2 {
		t.Fatalf("游标过滤异常: %+v", filtered.Records)
	}
}

// TestRuntimeLogSourceDerivesSourceFromSubsystem 验证显式来源优先与子系统推导的完整映射。
func TestRuntimeLogSourceDerivesSourceFromSubsystem(t *testing.T) {
	// cases 覆盖显式来源、子系统推导、未知来源和缺失标记四类情况。
	cases := []struct {
		// name 是当前用例的稳定名称。
		name string
		// explicit 是记录中的显式来源标记。
		explicit string
		// subsystem 是记录中的子系统标记。
		subsystem string
		// want 是期望得到的来源分类。
		want logsapp.Source
	}{
		{name: "显式来源优先", explicit: "MTop", subsystem: "automation", want: logsapp.SourceMTop},
		{name: "自动化子系统", explicit: "", subsystem: "automation", want: logsapp.SourceAutomation},
		{name: "AI 子系统归入引擎", explicit: "", subsystem: "ai", want: logsapp.SourceEngine},
		{name: "回复子系统归入引擎", explicit: "", subsystem: "reply", want: logsapp.SourceEngine},
		{name: "通知子系统", explicit: "", subsystem: "notify", want: logsapp.SourceNotify},
		{name: "WebSocket 子系统", explicit: "", subsystem: "ws", want: logsapp.SourceWebSocket},
		{name: "续期子系统", explicit: "", subsystem: "renewal", want: logsapp.SourceRenewal},
		{name: "未知显式来源回退子系统", explicit: "custom", subsystem: "notify", want: logsapp.SourceNotify},
		{name: "无标记归入其他", explicit: "", subsystem: "", want: logsapp.SourceOther},
	}
	// testCase 是当前待验证的来源推导用例。
	for _, testCase := range cases {
		// testCase 保存当前用例副本，避免闭包引用循环变量。
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			// got 是实际推导出的来源分类。
			got := runtimeLogSource(testCase.explicit, testCase.subsystem)
			if got != testCase.want {
				t.Fatalf("来源推导异常: got=%s want=%s", got, testCase.want)
			}
		})
	}
}

// TestRuntimeLogRecordLimitsAndCoercions 验证附加属性上限、文本截断和数值解析分支。
func TestRuntimeLogRecordLimitsAndCoercions(t *testing.T) {
	// attrs 保存超过上限的附加属性，用于验证数量截断。
	attrs := make([]slog.Attr, 0, maxRuntimeLogDetails+3)
	// index 表示当前构造的附加属性序号。
	for index := 0; index < maxRuntimeLogDetails+3; index++ {
		attrs = append(attrs, slog.String("extra", "value"))
	}
	attrs = append(attrs,
		slog.String("status", " 201 "),
		slog.String("duration", "250"),
		slog.Uint64("item_id", 7),
		slog.Float64("account", 3),
		slog.Int("order_id", 9),
		slog.Duration("chat_id", 2*time.Second),
		slog.Time("request_id", time.Unix(0, 0).UTC()),
	)
	// record 是包含超量属性与多种类型属性的转换结果。
	record := newRuntimeLogRecord(logging.RuntimeEntry{Seq: 1, Level: slog.LevelInfo, Message: "记录", Attrs: attrs})
	if len(record.Details) != maxRuntimeLogDetails {
		t.Fatalf("附加属性数量未截断: %d", len(record.Details))
	}
	if record.Status != 201 || record.DurationMS != 250 {
		t.Fatalf("字符串数值解析异常: status=%d duration=%d", record.Status, record.DurationMS)
	}
	if record.ItemID != "7" || record.AccountID != "3" || record.OrderID != "9" {
		t.Fatalf("非字符串标识转换异常: %+v", record)
	}
	if record.ChatID != "2s" || record.RequestID != "1970-01-01T00:00:00Z" {
		t.Fatalf("时长与时间属性转换异常: chat=%q request=%q", record.ChatID, record.RequestID)
	}
	// invalidRecord 覆盖无法解析的数字文本与高于 error 的自定义等级。
	invalidRecord := newRuntimeLogRecord(logging.RuntimeEntry{
		Seq: 2, Level: slog.LevelError + 4, Message: "记录",
		Attrs: []slog.Attr{slog.String("status", "not-a-number"), slog.String("duration", "not-a-number")},
	})
	if invalidRecord.Status != 0 || invalidRecord.DurationMS != 0 || invalidRecord.Level != logsapp.LevelError {
		t.Fatalf("无法解析的属性应回退为零: %+v", invalidRecord)
	}
	// debugRecord 覆盖低于 info 的等级映射。
	debugRecord := newRuntimeLogRecord(logging.RuntimeEntry{Seq: 3, Level: slog.LevelDebug, Message: "记录"})
	if debugRecord.Level != logsapp.LevelDebug {
		t.Fatalf("调试等级映射异常: %s", debugRecord.Level)
	}
	// longText 是超过单值上限的多字节文本，用于验证按字符截断。
	longText := strings.Repeat("汉", maxRuntimeLogDetailRunes+10)
	// truncated 是截断后的展示文本。
	truncated := truncateRuntimeLogText(longText)
	if len([]rune(truncated)) != maxRuntimeLogDetailRunes+1 || !strings.HasSuffix(truncated, "…") {
		t.Fatalf("文本截断异常: %d", len([]rune(truncated)))
	}
	// shortText 是未超过上限的文本，必须原样返回。
	shortText := truncateRuntimeLogText("短文本")
	if shortText != "短文本" {
		t.Fatalf("短文本不应被修改: %q", shortText)
	}
}

// TestRuntimeLogRecordKeepsLegacySourceAttribute 验证既有 source 诊断属性不再被来源分类键吞掉。
func TestRuntimeLogRecordKeepsLegacySourceAttribute(t *testing.T) {
	// record 是同时带有本系统 log_source 与既有 source 诊断属性的记录。
	record := newRuntimeLogRecord(logging.RuntimeEntry{
		Seq: 1, Level: slog.LevelInfo, Message: "MTOP Token 刷新成功",
		Attrs: []slog.Attr{
			slog.String("log_source", "mtop"),
			slog.String("source", "业务接口响应 Cookie"),
		},
	})
	if record.Source != logsapp.SourceMTop {
		t.Fatalf("来源分类不应被既有 source 属性覆盖: %s", record.Source)
	}
	if len(record.Details) != 1 || record.Details[0].Key != "source" || record.Details[0].Value != "业务接口响应 Cookie" {
		t.Fatalf("既有 source 诊断属性应保留在 Details: %+v", record.Details)
	}
}

// TestRuntimeLogNumericAttributeKinds 验证 status 与 duration 在各种 slog 值类型下的解析分支。
func TestRuntimeLogNumericAttributeKinds(t *testing.T) {
	// numericCases 覆盖无符号、浮点、整数和布尔类型，其中布尔类型走默认回退分支。
	numericCases := []struct {
		// name 是当前用例的稳定名称。
		name string
		// attr 是被测的数值属性。
		attr slog.Attr
		// wantStatus 是期望解析出的状态码。
		wantStatus int
		// wantDurationMS 是期望解析出的耗时毫秒数。
		wantDurationMS int64
	}{
		{name: "无符号状态码", attr: slog.Uint64("status", 204), wantStatus: 204},
		{name: "浮点状态码", attr: slog.Float64("status", 205), wantStatus: 205},
		{name: "布尔状态码回退为零", attr: slog.Bool("status", true)},
		{name: "整数耗时", attr: slog.Int("duration", 30), wantDurationMS: 30},
		{name: "无符号耗时", attr: slog.Uint64("duration", 40), wantDurationMS: 40},
		{name: "浮点耗时", attr: slog.Float64("duration", 5.5), wantDurationMS: 5},
		{name: "布尔耗时回退为零", attr: slog.Bool("duration", true)},
	}
	// numericCase 是当前待验证的数值属性解析用例。
	for _, numericCase := range numericCases {
		// parsed 是只包含单个数值属性的转换结果。
		parsed := newRuntimeLogRecord(logging.RuntimeEntry{Seq: 4, Level: slog.LevelInfo, Message: "记录", Attrs: []slog.Attr{numericCase.attr}})
		if parsed.Status != numericCase.wantStatus || parsed.DurationMS != numericCase.wantDurationMS {
			t.Fatalf("%s 解析异常: status=%d duration=%d", numericCase.name, parsed.Status, parsed.DurationMS)
		}
	}
}
