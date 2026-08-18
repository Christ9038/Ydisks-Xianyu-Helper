import type { OrderImportResult,OrderImportRowResult } from './types';

// orderStatusOptions 是订单状态筛选标签的稳定配置。
export const orderStatusOptions = [
  { key: 'all', label: '全部' },
  { key: 'processing', label: '处理中' },
  { key: 'shipped', label: '已发货' },
  { key: 'pending_ship', label: '待发货' },
  { key: 'completed', label: '已完成' },
  { key: 'cancelled', label: '已取消' },
  { key: 'refunding', label: '退款中' },
] as const;

// normalizeOrderImportResult 将后端兼容响应归一化为稳定的导入结果。
export const normalizeOrderImportResult = (value: unknown): OrderImportResult => {
  // payload 是待读取字段的未知响应对象。
  const payload = typeof value === 'object' && value !== null ? value as Record<string, unknown> : {};
  // rawResults 是后端返回的逐行结果数组。
  const rawResults = Array.isArray(payload.results) ? payload.results : [];
  return {
    total: Number(payload.total || 0),
    success_count: Number(payload.success_count || 0),
    failed_count: Number(payload.failed_count || 0),
    results: rawResults.map(normalizeOrderImportRow),
  };
};

// normalizeOrderImportRow 将单行未知响应转换为导入结果行。
const normalizeOrderImportRow = (value: unknown): OrderImportRowResult => {
  // row 是待归一化的单行未知对象。
  const row = typeof value === 'object' && value !== null ? value as Record<string, unknown> : {};
  return {
    order_id: String(row.order_id || 'unknown'),
    success: row.success === true,
    message: String(row.message || ''),
  };
};

// failedOrderImportRows 筛出需要在导入弹窗中展示的失败行。
export const failedOrderImportRows = (result: OrderImportResult | null): OrderImportRowResult[] =>
  result?.results.filter(isFailedOrderImportRow) || [];

// isFailedOrderImportRow 判断导入结果行是否失败。
const isFailedOrderImportRow = (row: OrderImportRowResult): boolean => !row.success;

// canSubmitOrderImport 检查订单导入是否具备文件且没有重复提交。
export const canSubmitOrderImport = (file: File | null, importing: boolean): boolean =>
  file !== null && !importing;

// validateOrderImportFile 检查订单导入文件是否使用支持的扩展名。
export const validateOrderImportFile = (file: File | null): string => {
  if (!file) return '请选择订单文件';
  // extension 是文件名中最后一个点号后的标准化扩展名。
  const extension = file.name.toLowerCase().split('.').pop() || '';
  return ['xlsx', 'csv', 'tsv', 'json'].includes(extension) ? '' : '仅支持 .xlsx、.csv、.tsv、.json 格式';
};

// isCurrentOrderRequest 判断异步订单请求响应是否仍属于当前代次。
export const isCurrentOrderRequest = (generation: number, currentGeneration: number): boolean =>
  generation === currentGeneration;

// orderErrorMessage 将未知异常提取为稳定的用户可见说明。
export const orderErrorMessage = (error: unknown, fallback: string): string =>
  error instanceof Error && error.message ? error.message : fallback;
