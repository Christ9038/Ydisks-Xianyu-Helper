package cards

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestServiceListAndGetFillSoldCount 验证列表和详情都从售出台账填充已售数量，台账读取失败不被吞掉。
func TestServiceListAndGetFillSoldCount(t *testing.T) {
	// repository 是被测服务依赖的内存仓储替身。
	repository := &cardRepositoryStub{cards: []Card{{ID: 3, UserID: 7}, {ID: 4, UserID: 7}}, card: Card{ID: 3, UserID: 7},
		saleCounts: map[int64]int64{3: 5}}
	// service 是被测卡密应用服务。
	service := NewService(repository)
	// ctx 是测试使用的根上下文。
	ctx := context.Background()

	// cards 保存被测服务返回的卡密组列表；err 保存被测调用返回的错误，用于断言成功或期望的失败。
	cards, err := service.List(ctx, 7)
	if err != nil || cards[0].SoldCount != 5 || cards[1].SoldCount != 0 || repository.saleUserID != 7 {
		t.Fatalf("cards=%+v err=%v", cards, err)
	}
	// card 保存被测卡密组数据；err 保存被测调用返回的错误，用于断言成功或期望的失败。
	card, err := service.Get(ctx, 7, 3)
	if err != nil || card.SoldCount != 5 {
		t.Fatalf("card=%+v err=%v", card, err)
	}
	// countErr 模拟台账统计读取失败。
	countErr := errors.New("db down")
	repository.saleCountsErr = countErr
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.List(ctx, 7); !errors.Is(err, countErr) {
		t.Fatalf("List 应返回台账错误: %v", err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.Get(ctx, 7, 3); !errors.Is(err, countErr) {
		t.Fatalf("Get 应返回台账错误: %v", err)
	}
}

// TestServiceListSaleRecords 验证归属校验、状态校验、分页规范化和仓储错误透传。
func TestServiceListSaleRecords(t *testing.T) {
	// repository 是被测服务依赖的内存仓储替身。
	repository := &cardRepositoryStub{card: Card{ID: 8, UserID: 7}, salePage: SalePage{Total: 3, Records: []SaleRecord{{ID: 1}}}}
	// service 是被测卡密应用服务。
	service := NewService(repository)
	// ctx 是测试使用的根上下文。
	ctx := context.Background()

	// err 保存被测调用返回的错误，用于断言成功或期望的失败；page 保存被测分页查询的返回结果。
	page, err := service.ListSaleRecords(ctx, 7, 8, SaleListInput{Status: SaleStatusSold, Search: "  o1 ", Page: 3, PageSize: 10})
	if err != nil || page.Total != 3 || page.Page != 3 || page.PageSize != 10 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	// q 保存传给仓储的已规范化分页查询。
	if q := repository.saleQuery; q.CardID != 8 || q.Status != SaleStatusSold || q.Search != "o1" || q.Limit != 10 || q.Offset != 20 {
		t.Fatalf("查询条件应被规范化: %+v", q)
	}
	// 非法页码和超限页大小回落到默认值。
	if page, _ := service.ListSaleRecords(ctx, 7, 8, SaleListInput{Page: -1, PageSize: 0}); page.Page != 1 || page.PageSize != DefaultSalePageSize {
		t.Fatalf("默认分页错误: %+v", page)
	}
	// page 保存被测分页查询的返回结果。
	if page, _ := service.ListSaleRecords(ctx, 7, 8, SaleListInput{PageSize: 5000}); page.PageSize != MaxSalePageSize {
		t.Fatalf("最大分页错误: %+v", page)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.ListSaleRecords(ctx, 7, 8, SaleListInput{Status: "bogus"}); !errors.As(err, new(*ValidationError)) {
		t.Fatalf("非法状态应为校验错误: %v", err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.ListSaleRecords(ctx, 9, 8, SaleListInput{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("越权应被拒绝: %v", err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.ListSaleRecords(ctx, 0, 8, SaleListInput{}); !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("无效用户: %v", err)
	}
	repository.saleErr = errors.New("db down")
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.ListSaleRecords(ctx, 7, 8, SaleListInput{}); !errors.Is(err, repository.saleErr) {
		t.Fatalf("仓储错误应透传: %v", err)
	}
}

// TestServiceExportUnsold 验证未售导出去空行、统一换行，且仅 data 类型可导出。
func TestServiceExportUnsold(t *testing.T) {
	// repository 是被测服务依赖的内存仓储替身。
	repository := &cardRepositoryStub{card: Card{ID: 8, UserID: 7, Type: "data", Name: "组/A", DataContent: "k1\r\n\r\n k2 \nk3\n"}}
	// service 是被测卡密应用服务。
	service := NewService(repository)
	// ctx 是测试使用的根上下文。
	ctx := context.Background()

	// err 保存被测调用返回的错误，用于断言成功或期望的失败；export 保存被测导出生成的文件内容。
	export, err := service.ExportUnsold(ctx, 7, 8)
	if err != nil || string(export.Body) != "k1\nk2\nk3\n" || !strings.HasPrefix(export.ContentType, "text/plain") {
		t.Fatalf("export=%+v err=%v", export, err)
	}
	if !strings.HasPrefix(export.Filename, "组_A-未售-") || !strings.HasSuffix(export.Filename, ".txt") {
		t.Fatalf("文件名应去掉路径字符: %q", export.Filename)
	}
	repository.card.Type = "text"
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.ExportUnsold(ctx, 7, 8); !errors.Is(err, ErrNotDataType) {
		t.Fatalf("非 data 类型应拒绝: %v", err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.ExportUnsold(ctx, 9, 8); !errors.Is(err, ErrForbidden) {
		t.Fatalf("越权应拒绝: %v", err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.ExportUnsold(ctx, 0, 8); !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("无效用户: %v", err)
	}
}

// TestServiceExportSold 验证已售 CSV 的 BOM、列头、状态标签、北京时间和公式注入防护。
func TestServiceExportSold(t *testing.T) {
	// repository 是被测服务依赖的内存仓储替身。
	repository := &cardRepositoryStub{card: Card{ID: 8, UserID: 7, Type: "data", Name: ""}, saleRecords: []SaleRecord{
		{Content: "=cmd|x", Status: SaleStatusSold, CardName: "组A", CookieID: "c1", OrderID: "o1", ItemID: "i1", BuyerID: "b,1", SoldAt: 1700000000},
		{Content: "plain", Status: SaleStatusReserved, CreatedAt: 1700000000},
		{Content: "empty-time", Status: SaleStatusReserved},
	}}
	// service 是被测卡密应用服务。
	service := NewService(repository)
	// ctx 是测试使用的根上下文。
	ctx := context.Background()

	// err 保存被测调用返回的错误，用于断言成功或期望的失败；export 保存被测导出生成的文件内容。
	export, err := service.ExportSold(ctx, 7, 8)
	if err != nil || !strings.HasPrefix(export.ContentType, "text/csv") || !strings.HasSuffix(export.Filename, ".csv") {
		t.Fatalf("export=%+v err=%v", export, err)
	}
	// body 保存导出文件或响应的正文。
	body := string(export.Body)
	if !strings.HasPrefix(body, "\xEF\xBB\xBF卡密,状态,卡密组") {
		t.Fatalf("应带 BOM 与列头: %q", body)
	}
	for /* want 是 CSV 中必须出现的片段 */ _, want := range []string{
		"'=cmd|x,已售,组A,c1,o1,i1,\"b,1\",2023-11-15 06:13:20", "plain,待核对,,,,,,2023-11-15 06:13:20", "empty-time,待核对,,,,,,\n",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("CSV 缺少 %q:\n%s", want, body)
		}
	}
	repository.saleErr = errors.New("db down")
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.ExportSold(ctx, 7, 8); !errors.Is(err, repository.saleErr) {
		t.Fatalf("仓储错误应透传: %v", err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.ExportSold(ctx, 9, 8); !errors.Is(err, ErrForbidden) {
		t.Fatalf("越权应拒绝: %v", err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.ExportSold(ctx, 0, 8); !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("无效用户: %v", err)
	}
}

// TestCSVSafeAndFilename 验证公式前缀覆盖全部危险字符，且空名称回退为带时间戳的默认名。
func TestCSVSafeAndFilename(t *testing.T) {
	for /* value 是各类以危险字符开头的单元格 */ _, value := range []string{"=1", "+1", "-1", "@a", "\tx", "\rx"} {
		if got := csvSafe(value); got != "'"+value {
			t.Fatalf("csvSafe(%q)=%q", value, got)
		}
	}
	if csvSafe("") != "" || csvSafe("abc") != "abc" {
		t.Fatal("普通单元格不得改变")
	}
	// name 是当前表驱动场景的卡密组名称。
	if name := exportFilename("  ", "已售", "csv"); !strings.HasPrefix(name, "卡密") || !strings.HasSuffix(name, ".csv") {
		t.Fatalf("空名称应回退: %q", name)
	}
	// name 是当前表驱动场景的卡密组名称。
	if name := exportFilename("a\x01b:c", "未售", "txt"); !strings.HasPrefix(name, "a_b_c-未售-") {
		t.Fatalf("控制与路径字符应被替换: %q", name)
	}
}

// TestServiceSaleRecordsByOrder 验证订单维度查询的参数校验和仓储透传。
func TestServiceSaleRecordsByOrder(t *testing.T) {
	// repository 是被测服务依赖的内存仓储替身。
	repository := &cardRepositoryStub{saleRecords: []SaleRecord{{ID: 1, Content: "K"}}}
	// service 是被测卡密应用服务。
	service := NewService(repository)
	// ctx 是测试使用的根上下文。
	ctx := context.Background()

	// err 保存被测调用返回的错误，用于断言成功或期望的失败；records 保存读取到的台账记录，用于断言数量、状态与内容。
	records, err := service.SaleRecordsByOrder(ctx, 7, "c1", "o1")
	if err != nil || len(records) != 1 || repository.saleUserID != 7 || repository.saleOrderCookie != "c1" || repository.saleOrderID != "o1" {
		t.Fatalf("records=%+v err=%v repo=%+v", records, err, repository)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.SaleRecordsByOrder(ctx, 7, " ", "o1"); !errors.As(err, new(*ValidationError)) {
		t.Fatalf("空账号应为校验错误: %v", err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.SaleRecordsByOrder(ctx, 7, "c1", ""); !errors.As(err, new(*ValidationError)) {
		t.Fatalf("空订单号应为校验错误: %v", err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := service.SaleRecordsByOrder(ctx, 0, "c1", "o1"); !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("无效用户: %v", err)
	}
}
