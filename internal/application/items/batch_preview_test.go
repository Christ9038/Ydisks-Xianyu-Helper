package items

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// batchPreviewOwnershipFake 是批量预检使用的账号与卡券归属替身。
type batchPreviewOwnershipFake struct {
	// cookieOwned 表示允许通过预检的账号标识。
	cookieOwned string
	// cardOwned 表示允许通过预检的卡券组标识。
	cardOwned int64
	// err 模拟归属查询故障。
	err error
}

// CookieOwned 返回预置的账号归属结果。
func (fake batchPreviewOwnershipFake) CookieOwned(_ context.Context, _ int64, cookieID string) (bool, error) {
	return cookieID == fake.cookieOwned, fake.err
}

// CardOwned 返回预置的卡券组归属结果。
func (fake batchPreviewOwnershipFake) CardOwned(_ context.Context, _ int64, cardID int64) (bool, error) {
	return cardID == fake.cardOwned, fake.err
}

// batchPreviewImageFake 是批量预检使用的图片引用校验替身。
type batchPreviewImageFake struct {
	// invalid 保存应被拒绝的图片引用。
	invalid string
}

// ValidateImageReference 拒绝预置的图片引用并接受其他引用。
func (fake batchPreviewImageFake) ValidateImageReference(_ string, reference string) error {
	if reference == fake.invalid {
		return errors.New("图片文件不存在: " + reference)
	}
	return nil
}

// TestParseSheetNormalizesCSV 验证 CSV 表头、字段和行上限归一化。
func TestParseSheetNormalizesCSV(t *testing.T) {
	// rows 和 err 表示解析后的字段行及解析错误。
	rows, err := ParseSheet([]byte("账号ID,标题,价格,库存,图片\nacc1,商品A,12.50,5,a.png\n"), "products.csv", 2)
	if err != nil {
		t.Fatalf("ParseSheet() error = %v", err)
	}
	if len(rows) != 1 || rows[0]["cookie_id"] != "acc1" || rows[0]["title"] != "商品A" {
		t.Fatalf("ParseSheet() rows = %#v", rows)
	}
}

// TestParseSheetRejectsEmptyAndUnsupportedInput 验证空输入、旧版 XLS 和表头缺行错误。
func TestParseSheetRejectsEmptyAndUnsupportedInput(t *testing.T) {
	// cases 保存不同无效表格输入。
	cases := []struct {
		// name 是当前输入场景名称。
		name string
		// raw 是待解析的表格内容。
		raw []byte
		// filename 是用于选择解析器的文件名。
		filename string
	}{
		{name: "empty", raw: []byte("  \n"), filename: "x.csv"},
		{name: "xls", raw: []byte("x"), filename: "x.xls"},
		{name: "header-only", raw: []byte("标题,价格\n"), filename: "x.csv"},
	}
	// testCase 表示当前无效输入样例。
	for _, testCase := range cases {
		// err 表示当前样例的解析错误。
		if _, err := ParseSheet(testCase.raw, testCase.filename, 0); err == nil {
			t.Errorf("ParseSheet(%s) expected error", testCase.name)
		}
	}
}

// TestBatchPreviewValidatesBusinessRules 验证预检服务的归属、金额、类目、图片和自动化规则。
func TestBatchPreviewValidatesBusinessRules(t *testing.T) {
	// service 和 err 表示批量预检服务及构造错误。
	service, err := NewBatchPreviewService(batchPreviewOwnershipFake{cookieOwned: "acc1", cardOwned: 9}, batchPreviewImageFake{invalid: "missing.png"})
	if err != nil {
		t.Fatalf("NewBatchPreviewService() error = %v", err)
	}
	// rows 和 err 表示逐行预检结果及执行错误。
	rows, err := service.Preview(context.Background(), BatchPreviewInput{
		UserID: 7, DefaultCookieID: "acc1", UploadDir: "/tmp/uploads",
		FallbackCategory: BatchPreviewCategory{CatID: "5001", CatName: "虚拟商品", ChannelCatID: "6001"},
		Rows: []map[string]any{
			{"标题": "商品A", "价格": "12.50", "图片": "a.png", "付款发货启用": "true", "付款发货内容": "9:2:10"},
			{"账号ID": "other", "标题": "", "价格": "0", "库存": "0", "图片": "missing.png", "类目ID": "1"},
		},
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("Preview() rows = %#v", rows)
	}
	if len(rows[0].Errors) != 0 || rows[0].CookieID != "acc1" || rows[0].Category.CatID != "5001" || len(rows[0].Automation.PaidDelivery.Actions) != 1 {
		t.Fatalf("valid row = %#v", rows[0])
	}
	// joined 保存无效行的拼接错误文本。
	joined := strings.Join(rows[1].Errors, "|")
	// expected 表示当前必须出现的错误片段。
	for _, expected := range []string{"账号不存在或不属于当前用户", "缺少标题", "价格必须大于 0", "库存必须大于 0", "图片文件不存在", "指定行类目时必须同时填写"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("invalid row errors %q missing %q", joined, expected)
		}
	}
}

// TestBatchPreviewParsesMultipleAutomationActions 验证应用层保留多条卡密动作和开关语义。
func TestBatchPreviewParsesMultipleAutomationActions(t *testing.T) {
	// service 和 err 表示批量预检服务及构造错误。
	service, err := NewBatchPreviewService(batchPreviewOwnershipFake{cookieOwned: "acc1", cardOwned: 101}, batchPreviewImageFake{})
	if err != nil {
		t.Fatalf("NewBatchPreviewService() error = %v", err)
	}
	// rows 和 previewErr 表示应用层解析后的逐行结果。
	rows, previewErr := service.Preview(context.Background(), BatchPreviewInput{
		UserID: 7, DefaultCookieID: "acc1", UploadDir: "/tmp/uploads",
		Rows: []map[string]any{{"标题": "商品", "价格": "1", "图片": "a.png", "付款发货启用": "是", "付款发货内容": "101:1:0;102:2:3"}},
	})
	if previewErr != nil || len(rows) != 1 {
		t.Fatalf("Preview() rows=%#v err=%v", rows, previewErr)
	}
	// actions 保存应用层解析出的付款发货动作顺序和参数。
	actions := rows[0].Automation.PaidDelivery.Actions
	if len(actions) != 2 || actions[0].CardID != 101 || actions[1].DeliveryCount != 2 || actions[1].DelaySeconds != 3 {
		t.Fatalf("parsed actions=%#v", actions)
	}
}

// TestBatchPreviewRejectsMissingPorts 验证服务构造时拒绝缺失基础设施 Port。
func TestBatchPreviewRejectsMissingPorts(t *testing.T) {
	// images 和 ownership 是缺失 Port 检查使用的替身。
	images := batchPreviewImageFake{}
	// ownership 是账号与卡券归属查询替身。
	ownership := batchPreviewOwnershipFake{}
	// err 表示缺失归属 Port 的构造结果。
	if _, err := NewBatchPreviewService(nil, images); err == nil {
		t.Error("missing ownership port should fail")
	}
	// err 表示缺失图片 Port 的构造结果。
	if _, err := NewBatchPreviewService(ownership, nil); err == nil {
		t.Error("missing image port should fail")
	}
	// service 和 err 表示完整 Port 构造结果。
	service, err := NewBatchPreviewService(ownership, images)
	if err != nil {
		t.Fatalf("NewBatchPreviewService() error = %v", err)
	}
	// err 表示空输入的预检错误。
	if _, err := service.Preview(context.Background(), BatchPreviewInput{}); !errors.Is(err, ErrBatchPreviewNoRows) {
		t.Fatalf("empty preview error = %v", err)
	}
}
