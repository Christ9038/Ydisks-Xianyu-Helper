package items

import (
	"context"
	"testing"
)

// TestBatchPreviewCoversFormatAndInitializationBranches 验证 TSV、未知扩展名和预检服务未装配分支。
func TestBatchPreviewCoversFormatAndInitializationBranches(t *testing.T) {
	// tsvRows 保存制表符解析结果。
	tsvRows, tsvErr := ParseSheet([]byte("账号ID\t标题\naccount\t商品\n"), "products.tsv", 1)
	if tsvErr != nil || len(tsvRows) != 1 || tsvRows[0]["title"] != "商品" {
		t.Fatalf("TSV 解析结果=%v err=%v", tsvRows, tsvErr)
	}
	// defaultRows 保存未知扩展名按 CSV 兼容解析的结果。
	defaultRows, defaultErr := ParseSheet([]byte("账号ID,标题\naccount,商品\n"), "products.data", 1)
	if defaultErr != nil || len(defaultRows) != 1 {
		t.Fatalf("未知扩展名解析结果=%v err=%v", defaultRows, defaultErr)
	}
	// fallbackValue 保存数字文本转换失败时的默认值。
	fallbackValue := parseIntDefault("not-number", 7)
	if fallbackValue != 7 || parseIntDefault(" 3.9 ", 7) != 3 || parseIntDefault("", 7) != 7 {
		t.Fatalf("整数默认值转换异常=%d", fallbackValue)
	}
	// nilService 保存空接收者预检服务。
	var nilService *BatchPreviewService
	// nilCookieOwnedErr 保存空服务账号归属查询错误。
	if _, nilCookieOwnedErr := nilService.CookieOwned(context.Background(), 1, "account"); nilCookieOwnedErr == nil {
		t.Fatal("空预检服务账号归属不应成功")
	}
	// nilPreviewRows、nilPreviewErr 保存空服务预检结果。
	nilPreviewRows, nilPreviewErr := nilService.Preview(context.Background(), BatchPreviewInput{})
	if nilPreviewRows != nil || nilPreviewErr == nil {
		t.Fatalf("空预检服务结果=%v err=%v", nilPreviewRows, nilPreviewErr)
	}
	// service 保存完整依赖的预检服务。
	service, serviceErr := NewBatchPreviewService(batchPreviewOwnershipFake{cookieOwned: "account"}, batchPreviewImageFake{})
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	// emptyRows、emptyErr 保存无数据预检结果。
	emptyRows, emptyErr := service.Preview(context.Background(), BatchPreviewInput{})
	if emptyRows != nil || emptyErr == nil {
		t.Fatalf("无数据预检结果=%v err=%v", emptyRows, emptyErr)
	}
}
