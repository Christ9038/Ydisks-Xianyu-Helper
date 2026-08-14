package mtop

import (
	"errors"
	"testing"
)

// TestPublishPayloadBuilders 负责Test发布请求载荷Builders相关处理。
func TestPublishPayloadBuilders(t *testing.T) {
	// req 保存req，供当前处理流程使用
	req := PublishItemRequest{PriceCents: 1234, OriginalPriceCents: 2000, PostageMode: "fixed", PostageCents: 800}
	// price 保存price，供当前处理流程使用
	price := publishPriceDTO(req)
	if price["priceInCent"] != "1234" || price["origPriceInCent"] != "2000" {
		t.Fatalf("publishPriceDTO = %#v", price)
	}
	// postage 保存postage，供当前处理流程使用
	postage := postageDTO(req)
	if postage["postPriceInCent"] != "800" || postage["templateId"] != "0" {
		t.Fatalf("postageDTO = %#v", postage)
	}
	if // got 保存got，供当前处理流程使用
	got := postageDTO(PublishItemRequest{PostageMode: "free"}); got["canFreeShipping"] != true {
		t.Fatalf("free postage = %#v", got)
	}
	if // got 保存got，供当前处理流程使用
	got := postageDTO(PublishItemRequest{PostageMode: "distance"}); got["templateId"] != "-100" {
		t.Fatalf("distance postage = %#v", got)
	}
}

// TestPublishParsingAndErrors 负责Test发布ParsingAnd错误列表相关处理。
func TestPublishParsingAndErrors(t *testing.T) {
	if // w、h 保存w、h，供当前处理流程使用
	w, h := parsePix("800x600"); w != 800 || h != 600 {
		t.Fatalf("parsePix = %d x %d", w, h)
	}
	if // w、h 保存w、h，供当前处理流程使用
	w, h := parsePix("bad"); w != 0 || h != 0 {
		t.Fatalf("invalid parsePix = %d x %d", w, h)
	}
	if // got 保存got，供当前处理流程使用
	got := centsText(1234); got != "12.34" {
		t.Fatalf("centsText = %q", got)
	}
	if // got 保存got，供当前处理流程使用
	got := findStringDeep(map[string]any{"outer": map[string]any{"itemId": "42"}}, "itemId"); got != "42" {
		t.Fatalf("findStringDeep = %q", got)
	}

	// err 保存err，供当前处理流程使用
	err := classifyPublishError([]string{"FAIL_SYS_TOKEN_EXPIRED::令牌过期"}, map[string]any{})
	// publishErr 保存发布Err，供当前处理流程使用
	var publishErr *PublishError
	if !errors.As(err, &publishErr) || publishErr.Code != PublishErrorTokenExpired {
		t.Fatalf("token error = %#v", err)
	}
	err = classifyPublishError([]string{"账号没有库存发布权限"}, map[string]any{})
	if !errors.As(err, &publishErr) || publishErr.Code != PublishErrorStockPermissionMissing {
		t.Fatalf("stock error = %#v", err)
	}
}
