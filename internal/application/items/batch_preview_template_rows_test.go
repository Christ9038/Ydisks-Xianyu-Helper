package items

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// TestPreviewTemplateRowsSkipsAccountAndImageFiles 验证模板导入模式跳过账号与图片文件校验、清空账号，
// 同时保留其余字段校验并完成 #107 模板变量分发；「模板名称」表头归一为 template_name。
func TestPreviewTemplateRowsSkipsAccountAndImageFiles(t *testing.T) {
	// service 使用模板需求替身，图片替身会拒绝 bad.png 以证明导入模式不校验图片文件。
	service := newSheetTemplateService(t)
	service.images = batchPreviewImageFake{invalid: "bad.png"}
	// fields 包含：合法模板行（他人账号、不存在的图片文件）与价格非法行。
	fields, err := ParseSheet([]byte("模板名称,账号ID,标题,价格,图片,邮费模式,付款发货启用,付款发货内容,变量.link,变量.code,模板卡密.main\n"+
		"网盘A,someone-else,资源A,9.9,bad.png,none,是,T1,https://pan.example.com/s/a,c1,9\n"+
		",,资源B,abc,bad.png,none,否,,,,\n"), "t.csv", 300)
	if err != nil {
		t.Fatal(err)
	}
	// rows、previewErr 保存模板导入模式的解析结果。
	rows, previewErr := service.PreviewTemplateRows(context.Background(), 1, fields)
	if previewErr != nil || len(rows) != 2 {
		t.Fatalf("rows=%d err=%v", len(rows), previewErr)
	}
	if len(rows[0].Errors) != 0 || rows[0].CookieID != "" || rows[0].Raw["template_name"] != "网盘A" {
		t.Fatalf("合法行异常: errors=%v cookie=%q raw=%v", rows[0].Errors, rows[0].CookieID, rows[0].Raw)
	}
	// paid 是合法行的模板动作，应已按模板需求填入变量与绑定。
	paid := rows[0].Automation.PaidDelivery.Actions[0]
	if !reflect.DeepEqual(paid.CustomVariables, map[string]string{"link": "https://pan.example.com/s/a", "code": "c1"}) || len(paid.TemplateBindings) != 1 {
		t.Fatalf("模板变量分发异常: %+v", paid)
	}
	// joined 是价格非法行的错误合集，不应出现账号或图片文件错误。
	joined := strings.Join(rows[1].Errors, "|")
	if !strings.Contains(joined, "价格必须大于 0") || strings.Contains(joined, "账号") || strings.Contains(joined, "图片文件不存在") {
		t.Fatalf("价格非法行错误异常: %s", joined)
	}
	// emptyErr 保存空输入的错误。
	if _, emptyErr := service.PreviewTemplateRows(context.Background(), 1, nil); emptyErr != ErrBatchPreviewNoRows {
		t.Fatalf("空输入应返回 ErrBatchPreviewNoRows: %v", emptyErr)
	}
	// nilErr 保存未初始化服务的错误。
	if _, nilErr := (*BatchPreviewService)(nil).PreviewTemplateRows(context.Background(), 1, fields); nilErr == nil {
		t.Fatal("未初始化服务应返回错误")
	}
	// legacy 保存普通批量预检对同一份表格的结果，证明原路径仍校验账号与图片文件。
	legacy, legacyErr := service.Preview(context.Background(), BatchPreviewInput{UserID: 1, Rows: fields[:1]})
	if legacyErr != nil || !strings.Contains(strings.Join(legacy[0].Errors, "|"), "账号不存在") || !strings.Contains(strings.Join(legacy[0].Errors, "|"), "图片文件不存在") {
		t.Fatalf("普通预检不应跳过账号与图片校验: %+v %v", legacy, legacyErr)
	}
}

// TestParseMoneyCentsExport 验证导出的金额解析与内部规则一致。
func TestParseMoneyCentsExport(t *testing.T) {
	// cents、err 保存合法金额的解析结果。
	if cents, err := ParseMoneyCents("￥9.90"); err != nil || cents != 990 {
		t.Fatalf("cents=%d err=%v", cents, err)
	}
	// err 保存非法金额的解析错误。
	if _, err := ParseMoneyCents("1.234"); err == nil {
		t.Fatal("超过两位小数应报错")
	}
}
