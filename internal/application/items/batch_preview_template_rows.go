package items

import (
	"context"
	"errors"
)

// PreviewTemplateRows 把表格字段行解析为预检行，供导入资源库商品模板使用。
// 与 Preview 共用字段解析、#107 的模板动作与逐行变量分发，以及价格、库存、邮费、图片数量、类目和自动化校验；
// 区别是不校验账号（模板发布时才选账号）、不校验上传目录中的图片文件（由资源库按压缩包与网址核对），
// 并清空行内账号。userID 是导入用户，fields 是 ParseSheet 的输出；逐行错误写入各行，不访问平台接口。
func (service *BatchPreviewService) PreviewTemplateRows(ctx context.Context, userID int64, fields []map[string]any) ([]BatchPreviewRow, error) {
	if service == nil || service.ownership == nil || service.images == nil {
		return nil, errors.New("批量预检服务未初始化")
	}
	if len(fields) == 0 {
		return nil, ErrBatchPreviewNoRows
	}
	// input 是模板导入模式的预检上下文。
	input := BatchPreviewInput{UserID: userID, templateImport: true}
	// rows 保存逐行解析结果。
	rows := make([]BatchPreviewRow, 0, len(fields))
	// index 和 row 是表格行下标及其字段。
	for index, row := range fields {
		// parsed 是当前行的解析与校验结果。
		parsed := service.parseRow(ctx, input, index, row)
		parsed.CookieID = ""
		rows = append(rows, parsed)
	}
	return rows, nil
}

// ParseMoneyCents 把表格中的金额文本（允许 ¥/￥ 前缀与正负号，最多两位小数）转换为分；空文本为 0。
func ParseMoneyCents(raw string) (int64, error) {
	return parseMoneyCents(raw)
}
