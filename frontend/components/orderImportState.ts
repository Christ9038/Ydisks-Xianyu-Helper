export interface OrderImportRowResult {
  order_id: string;
  success: boolean;
  message: string;
}

export interface OrderImportResult {
  total: number;
  success_count: number;
  failed_count: number;
  results: OrderImportRowResult[];
}

export const normalizeOrderImportResult = (value: any): OrderImportResult => ({
  total: Number(value?.total || 0),
  success_count: Number(value?.success_count || 0),
  failed_count: Number(value?.failed_count || 0),
  results: Array.isArray(value?.results) ? value.results.map((row: any) => ({
    order_id: String(row?.order_id || 'unknown'),
    success: row?.success === true,
    message: String(row?.message || ''),
  })) : [],
});

export const failedOrderImportRows = (result: OrderImportResult | null): OrderImportRowResult[] =>
  result?.results.filter(row => !row.success) || [];
