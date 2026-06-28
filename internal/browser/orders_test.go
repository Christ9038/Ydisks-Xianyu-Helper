package browser

import (
	"testing"
)

func TestOrderStatusMap(t *testing.T) {
	cases := []struct {
		code   int
		expect string
	}{
		{1, "processing"}, {2, "pending_ship"}, {3, "shipped"},
		{4, "completed"}, {7, "refunding"}, {8, "cancelled"},
		{9, "refunding"}, {10, "cancelled"}, {11, "completed"}, {12, "cancelled"},
	}
	for _, c := range cases {
		if s, ok := orderStatusMap[c.code]; !ok || s != c.expect {
			t.Errorf("orderStatusMap[%d] = %q, want %q", c.code, s, c.expect)
		}
	}
}

func TestParseWantCount(t *testing.T) {
	cases := map[string]int{
		"3":    3,
		"1.5万": 15000,
		"2万":   20000,
		"100":  100,
	}
	for in, want := range cases {
		if got := parseWantCount(in); got != want {
			t.Errorf("parseWantCount(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseAPIResponseStatusCode(t *testing.T) {
	od := &OrderDetail{}
	data := map[string]any{
		"utArgs": map[string]any{"orderStatus": float64(2)},
	}
	parseAPIResponse(od, data)
	if od.OrderStatus != "pending_ship" {
		t.Errorf("got %q, want pending_ship", od.OrderStatus)
	}
}

func TestParseAPIResponseAmountFromComponentData(t *testing.T) {
	od := &OrderDetail{}
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

func TestExtractPaidAmountFromText(t *testing.T) {
	cases := map[string]string{
		"商品标价\n¥99.00\n实付款 ¥0.88\n交易成功": "0.88",
		"订单信息\n实付金额\n￥12.50\n订单编号":      "12.50",
		"合计\n6.00\n其他信息":                "6.00",
		"商品价格\n¥19.90\n没有付款标签":          "",
	}
	for input, want := range cases {
		if got := extractPaidAmountFromText(input); got != want {
			t.Errorf("extractPaidAmountFromText(%q)=%q want %q", input, got, want)
		}
	}
}
