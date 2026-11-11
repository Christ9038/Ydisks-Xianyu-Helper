// csvDownload 提供把表格行保存为 Excel 可直接打开的 CSV 文件的共享能力，供批量铺货与资源库导入模板下载复用。
import { downloadBlob } from './download';

/**
 * buildCsv 把二维单元格转换为 CSV 文本：每个单元格都加双引号，内部双引号按 CSV 规则写成两个，行之间用换行分隔。
 * rows 第一行通常是表头；返回值不含 BOM。
 */
export const buildCsv = (rows: ReadonlyArray<ReadonlyArray<string>>): string => rows
  .map(/* row 是当前行。 */ row => row.map(/* cell 是当前单元格。 */ cell => `"${String(cell).replace(/"/g, '""')}"`).join(','))
  .join('\n');

/**
 * downloadCsv 生成带 UTF-8 BOM 的 CSV 文件并触发浏览器下载；BOM 让 Excel 按 UTF-8 正确识别中文表头。
 * filename 是建议文件名，rows 是包含表头的全部行。
 */
export const downloadCsv = (filename: string, rows: ReadonlyArray<ReadonlyArray<string>>): void => {
  downloadBlob(new Blob(['﻿' + buildCsv(rows)], { type: 'text/csv;charset=utf-8' }), filename);
};
