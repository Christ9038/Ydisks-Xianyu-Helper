import { useCallback,useEffect,useRef,useState } from 'react';
import { downloadBlob } from '../../../shared/browser/download';
import { exportCard,getCardSaleRecords } from './api';
import type { CardExportScope,CardSalePage,CardSaleQuery,CardSaleStatus } from './models';

// defaultSaleQuery 是打开库存记录弹窗时的初始查询：不筛选状态和搜索词，第一页，每页 20 条。
const defaultSaleQuery: CardSaleQuery = { status: '', search: '', page: 1, pageSize: 20 };

// unsoldLines 把 data 卡密组的库存正文拆成未售卡密行：统一换行、去除首尾空白并丢弃空行，与服务端导出口径一致。
export const unsoldLines = (dataContent: string | undefined): string[] =>
  (dataContent ?? '').split(/\r?\n/).map(/* line 是库存正文中的一行。 */ line => line.trim()).filter(Boolean);

// errorText 把请求失败原因转换为可展示的中文提示，取消错误由调用方在此之前过滤。
const errorText = (error: unknown, fallback: string): string => (error instanceof Error && error.message ? error.message : fallback);

// CardSaleRecordsResult 暴露当前页售出记录、加载状态和查询操作。
export interface CardSaleRecordsResult {
  // page 是最近一次成功加载的一页记录；首次加载完成前为 null。
  page: CardSalePage | null;
  // query 是当前生效的查询条件。
  query: CardSaleQuery;
  // loading 表示当前查询是否仍在进行。
  loading: boolean;
  // error 是最近一次查询失败的提示；成功或开始新查询后清空。
  error: string | null;
  // setStatus 切换状态筛选并回到第一页。
  setStatus: (status: CardSaleStatus | '') => void;
  // applySearch 应用搜索词并回到第一页。
  applySearch: (search: string) => void;
  // setPage 跳转到指定页码。
  setPage: (page: number) => void;
  // reload 用当前条件重新查询。
  reload: () => void;
}

// useCardSaleRecords 加载某个卡密组的售出记录。cardId 变化时调用方应通过 key 重新挂载，避免沿用旧筛选；
// 每次查询用独立 AbortController，条件变化或卸载会取消旧请求，过期响应不会覆盖当前结果。
export const useCardSaleRecords = (cardId: number): CardSaleRecordsResult => {
  // query 保存用户选择的筛选、搜索和分页条件，属于瞬时 UI 状态。
  const [query, setQuery] = useState<CardSaleQuery>(defaultSaleQuery);
  // page 保存最近一次成功的服务端结果。
  const [page, setPageData] = useState<CardSalePage | null>(null);
  // loading 表示查询进行中。
  const [loading, setLoading] = useState(true);
  // error 保存最近一次查询失败的提示。
  const [error, setError] = useState<string | null>(null);
  // reloadToken 递增时强制按当前条件重新查询。
  const [reloadToken, setReloadToken] = useState(0);

  useEffect(
    // 查询副作用：条件、卡密组或重载令牌变化时发起请求，清理函数取消尚未完成的旧请求。
    () => {
      // controller 绑定本次查询的生命周期。
      const controller = new AbortController();
      setLoading(true);
      setError(null);
      getCardSaleRecords(cardId, query, { signal: controller.signal })
        .then(/* result 是本次查询返回的一页记录，仅当请求未被取消时才写入。 */ result => {
          if (!controller.signal.aborted) setPageData(result);
        })
        .catch(/* failure 是本次查询的失败原因，被取消的请求不展示错误。 */ failure => {
          if (!controller.signal.aborted) setError(errorText(failure, '加载售出记录失败'));
        })
        .finally(/* 仅当前请求结束时才关闭加载状态，避免旧请求提前清除新请求的加载提示。 */ () => {
          if (!controller.signal.aborted) setLoading(false);
        });
      // 清理函数：条件变化或卸载时中止尚未完成的查询，避免旧响应覆盖新状态。
      return /* 清理函数：中止未完成的查询。 */ () => controller.abort();
    },
    [cardId, query, reloadToken],
  );

  // setStatus 切换状态筛选，并按函数式更新回到第一页。
  const setStatus = useCallback((status: CardSaleStatus | '') => setQuery(/* previous 是更新前的查询条件。 */ previous => ({ ...previous, status, page: 1 })), []);
  // applySearch 应用去除首尾空白的搜索词，并回到第一页。
  const applySearch = useCallback((search: string) => setQuery(/* previous 是更新前的查询条件。 */ previous => ({ ...previous, search: search.trim(), page: 1 })), []);
  // setPage 跳转页码，下限为 1。
  const setPage = useCallback((next: number) => setQuery(/* previous 是更新前的查询条件。 */ previous => ({ ...previous, page: Math.max(1, next) })), []);
  // reload 递增令牌触发重新查询。
  const reload = useCallback(() => setReloadToken(/* previous 是更新前的令牌。 */ previous => previous + 1), []);

  return { page, query, loading, error, setStatus, applySearch, setPage, reload };
};

// CardExportResult 暴露导出动作和进行中的范围。
export interface CardExportResult {
  // exporting 是正在导出的范围；空闲时为 null，同一时间只允许一个导出。
  exporting: CardExportScope | null;
  // exportError 是最近一次导出失败的提示。
  exportError: string | null;
  // runExport 请求并下载指定范围的导出文件。
  runExport: (scope: CardExportScope) => Promise<void>;
}

// useCardExport 管理单个卡密组的导出请求；卸载时取消进行中的请求，且已卸载后不再触发下载。
export const useCardExport = (cardId: number): CardExportResult => {
  // exporting 保存正在导出的范围，用于禁用按钮。
  const [exporting, setExporting] = useState<CardExportScope | null>(null);
  // exportError 保存最近一次导出失败的提示。
  const [exportError, setExportError] = useState<string | null>(null);
  // activeController 保存进行中的导出请求控制器，卸载时取消。
  const activeController = useRef<AbortController | null>(null);

  useEffect(
    // 卸载清理：取消进行中的导出，避免弹窗关闭后仍弹出下载。
    () => /* 卸载清理：中止仍在进行的导出请求。 */ () => activeController.current?.abort(),
    [],
  );

  // runExport 发起导出；已有导出进行中时忽略重复点击。
  // runExport 触发指定范围的导出；scope 是 unsold 或 sold。
  const runExport = useCallback(/* 触发指定范围导出的回调。 */ async (scope: CardExportScope) => {
    if (activeController.current) return;
    // controller 绑定本次导出请求。
    const controller = new AbortController();
    activeController.current = controller;
    setExporting(scope);
    setExportError(null);
    try {
      // file 是服务端生成的导出文件。
      const file = await exportCard(cardId, scope, { signal: controller.signal });
      if (!controller.signal.aborted) downloadBlob(file.blob, file.filename);
    } catch (failure /* failure 是导出请求或下载失败的原因。 */) {
      if (!controller.signal.aborted) setExportError(errorText(failure, '导出失败'));
    } finally {
      activeController.current = null;
      if (!controller.signal.aborted) setExporting(null);
    }
  }, [cardId]);

  return { exporting, exportError, runExport };
};
