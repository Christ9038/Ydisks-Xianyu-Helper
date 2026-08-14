package browser

import (
	"testing"
)

// TestOrderStatusMap 负责Test订单状态Map相关处理。
func TestOrderStatusMap(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
	cases := []struct {
		code   int
		expect string
	}{
		{1, "processing"}, {2, "pending_ship"}, {3, "shipped"},
		{4, "completed"}, {7, "refunding"}, {8, "cancelled"},
		{9, "refunding"}, {10, "cancelled"}, {11, "completed"}, {12, "cancelled"},
	}
	// c 表示当前遍历过程中的c
	for _, c := range cases {
		if // s、ok 保存s、ok，供当前处理流程使用
		s, ok := orderStatusMap[c.code]; !ok || s != c.expect {
			t.Errorf("orderStatusMap[%d] = %q, want %q", c.code, s, c.expect)
		}
	}
}

// TestParseAPIResponseStatusCode 负责TestParseAPI响应状态Code相关处理。
func TestParseAPIResponseStatusCode(t *testing.T) {
	// od 保存od，供当前处理流程使用
	od := &OrderDetail{}
	// data 保存数据，供当前处理流程使用
	data := map[string]any{
		"utArgs": map[string]any{"orderStatus": float64(2)},
	}
	parseAPIResponse(od, data)
	if od.OrderStatus != "pending_ship" {
		t.Errorf("got %q, want pending_ship", od.OrderStatus)
	}
}

// TestParseAPIResponseAmountFromComponentData 负责TestParseAPI响应AmountFromComponent数据相关处理。
func TestParseAPIResponseAmountFromComponentData(t *testing.T) {
	// od 保存od，供当前处理流程使用
	od := &OrderDetail{}
	// data 保存数据，供当前处理流程使用
	data := map[string]any{
		"components": []any{
			map[string]any{
				"render": "orderInfoVO",
				"data": map[string]any{
					"priceInfo": map[string]any{
						"amount": map[string]any{"value": "0.88"},
					},
				},
			},
		},
	}
	parseAPIResponse(od, data)
	if od.Amount != "0.88" {
		t.Fatalf("Amount=%q want 0.88", od.Amount)
	}
}

// TestExtractPaidAmountFromText 负责TestExtractPaidAmountFrom文本相关处理。
func TestExtractPaidAmountFromText(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
	cases := map[string]string{
		"商品标价\n¥99.00\n实付款 ¥0.88\n交易成功": "0.88",
		"订单信息\n实付金额\n￥12.50\n订单编号":      "12.50",
		"合计\n6.00\n其他信息":                "6.00",
		"商品价格\n¥19.90\n没有付款标签":          "",
	}
	// input、want 表示当前遍历过程中的input、want
	for input, want := range cases {
		if // got 保存got，供当前处理流程使用
		got := extractPaidAmountFromText(input); got != want {
			t.Errorf("extractPaidAmountFromText(%q)=%q want %q", input, got, want)
		}
	}
}
