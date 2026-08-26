package notify

import "testing"

// TestParseEventTypesAndEventAllowedBoundaries 覆盖空值、列表、分隔文本和非法 JSON 配置。
func TestParseEventTypesAndEventAllowedBoundaries(t *testing.T) {
	// emptyEvents、emptyErr 保存空配置解析结果。
	emptyEvents, emptyErr := parseEventTypes("  ")
	if emptyErr != nil || emptyEvents != nil {
		t.Fatalf("empty events=%v err=%v", emptyEvents, emptyErr)
	}
	// listEvents、listErr 保存 JSON 数组配置解析结果。
	listEvents, listErr := parseEventTypes(`["order", "offline", "order"]`)
	if listErr != nil || !listEvents["order"] || !listEvents["offline"] || len(listEvents) != 2 {
		t.Fatalf("list events=%v err=%v", listEvents, listErr)
	}
	// textEvents、textErr 保存多分隔符配置解析结果。
	textEvents, textErr := parseEventTypes("order, offline; token\n")
	if textErr != nil || len(textEvents) != 3 || !textEvents["token"] {
		t.Fatalf("text events=%v err=%v", textEvents, textErr)
	}
	// invalidEvents、invalidErr 保存非法 JSON 配置解析结果。
	invalidEvents, invalidErr := parseEventTypes(`["order"`)
	if invalidErr == nil || invalidEvents != nil {
		t.Fatalf("invalid events=%v err=%v", invalidEvents, invalidErr)
	}
	// separatorEvents、separatorErr 保存只有分隔符时的空事件集合结果。
	separatorEvents, separatorErr := parseEventTypes(", ;\n")
	if separatorErr != nil || separatorEvents != nil {
		t.Fatalf("separator events=%v err=%v", separatorEvents, separatorErr)
	}
	// emptyTypeAllowed、emptyTypeErr 保存未指定事件类型时的默认放行结果。
	emptyTypeAllowed, emptyTypeErr := eventAllowed("offline", "")
	if emptyTypeErr != nil || !emptyTypeAllowed {
		t.Fatalf("empty event type allow=%v err=%v", emptyTypeAllowed, emptyTypeErr)
	}
	// invalidAllowed、invalidAllowedErr 保存事件过滤配置非法时的解析错误。
	invalidAllowed, invalidAllowedErr := eventAllowed("[", "offline")
	if invalidAllowed || invalidAllowedErr == nil {
		t.Fatalf("invalid allow=%v err=%v", invalidAllowed, invalidAllowedErr)
	}
	// allowed、allowedErr 保存指定事件在空配置中的默认放行结果。
	allowed, allowedErr := eventAllowed("", "order")
	if allowedErr != nil || !allowed {
		t.Fatalf("empty allow=%v err=%v", allowed, allowedErr)
	}
	// denied、deniedErr 保存明确配置中的事件过滤结果。
	denied, deniedErr := eventAllowed("order", "offline")
	if deniedErr != nil || denied {
		t.Fatalf("filtered allow=%v err=%v", denied, deniedErr)
	}
}
