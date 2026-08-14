import { useCallback, useEffect, useMemo, useRef, useState, type Dispatch, type SetStateAction } from 'react';
import type { AccountDetail, Item, Order } from '../../../types';
import { getAccountDetails, getItems, getOrders, importOrders } from './api';
import { canSubmitOrderImport, isCurrentOrderRequest, normalizeOrderImportResult, orderErrorMessage, validateOrderImportFile } from './state';
import type { OrderImportState, OrderQueryState } from './types';

// OrderQueryOptions 描述订单查询 Hook 的固定分页参数。
interface OrderQueryOptions {
  // pageSize 是每页请求的订单数量。
  pageSize: number;
}

// useOrderQuery 统一管理订单列表加载、筛选、防抖和过期响应保护。
export const useOrderQuery = (options: OrderQueryOptions = { pageSize: 20 }): OrderQueryState => {
  // orders 保存当前页订单。
  const [orders, setOrders] = useState<Order[]>([]);
  // items 保存用于补充订单商品名称的商品列表。
  const [items, setItems] = useState<Item[]>([]);
  // accounts 保存订单账号筛选下拉框的数据。
  const [accounts, setAccounts] = useState<AccountDetail[]>([]);
  // filter 保存订单状态筛选值。
  const [filter, setFilter] = useState('all');
  // accountFilter 保存账号筛选值。
  const [accountFilter, setAccountFilter] = useState('');
  // searchText 保存搜索框实时输入值。
  const [searchText, setSearchText] = useState('');
  // debouncedSearch 保存防抖后的服务端搜索值。
  const [debouncedSearch, setDebouncedSearch] = useState('');
  // page 保存当前分页页码。
  const [page, setPage] = useState(1);
  // totalPages 保存服务端返回的总页数。
  const [totalPages, setTotalPages] = useState(1);
  // loading 表示当前订单查询是否正在执行。
  const [loading, setLoading] = useState(false);
  // orderGeneration 隔离筛选变化和手动刷新的旧订单响应。
  const orderGeneration = useRef(0);
  // orderAbort 保存当前订单查询的取消控制器。
  const orderAbort = useRef<AbortController | null>(null);
  // auxiliaryAbort 保存账号和商品辅助数据请求的取消控制器。
  const auxiliaryAbort = useRef<AbortController | null>(null);

  // loadOrders 按当前筛选条件加载订单并取消上一轮请求。
  const loadOrders = useCallback(
    // 订单加载回调读取当前筛选条件并更新列表。
    async () => {
    // generation 标记本次订单查询请求的代次。
    const generation = ++orderGeneration.current;
    orderAbort.current?.abort();
    // controller 允许筛选条件变化时取消旧查询。
    const controller = new AbortController();
    orderAbort.current = controller;
    setLoading(true);
    try {
      // result 是当前筛选条件下的分页订单结果。
      const result = await getOrders(accountFilter || undefined, filter, page, options.pageSize, debouncedSearch, { signal: controller.signal });
      if (!isCurrentOrderRequest(generation, orderGeneration.current)) return;
      setOrders(result.data);
      setTotalPages(result.total_pages);
    } catch (error /* 订单查询异常 */) {
      if (isCurrentOrderRequest(generation, orderGeneration.current) && !controller.signal.aborted) {
        console.error('加载订单失败:', error);
      }
    } finally {
      if (isCurrentOrderRequest(generation, orderGeneration.current)) setLoading(false);
    }
    },
    [accountFilter, debouncedSearch, filter, options.pageSize, page],
  );

  // useEffect 防抖搜索输入，避免每次键入都立即请求服务端。
  useEffect(
    // 搜索防抖副作用延迟提交用户输入。
    () => {
    // timer 是当前搜索输入对应的防抖定时器。
    const timer = window.setTimeout(
      // 防抖定时器回调提交标准化搜索文本。
      () => {
      setPage(1);
      setDebouncedSearch(searchText.trim());
      },
      300,
    );
    return (
      // 防抖清理回调取消上一次搜索定时器。
      () => window.clearTimeout(timer)
    );
    },
    [searchText],
  );

  // useEffect 在筛选条件变化时加载订单，并在离开页面时取消请求。
  useEffect(
    // 订单查询副作用响应筛选变化并在卸载时取消请求。
    () => {
      void loadOrders();
      return (
        // 订单查询清理回调使旧请求代次失效并取消网络请求。
        () => {
          orderGeneration.current += 1;
          orderAbort.current?.abort();
        }
      );
    },
    [loadOrders],
  );

  // useEffect 加载订单展示所需的账号和商品辅助数据。
  useEffect(
    // 辅助数据副作用并行读取账号和商品列表。
    () => {
    // controller 允许页面卸载时取消辅助数据请求。
    const controller = new AbortController();
    auxiliaryAbort.current?.abort();
    auxiliaryAbort.current = controller;
    void Promise.all([
      getAccountDetails({ signal: controller.signal }),
      getItems(undefined, { signal: controller.signal }),
    ]).then(
      // 辅助数据成功回调只接受尚未取消的响应。
      ([accountList, itemsList]) => {
        if (controller.signal.aborted) return;
        setAccounts(accountList);
        setItems(itemsList);
      },
    ).catch(
      // 辅助数据失败回调忽略主动取消产生的异常。
      (error: unknown) => {
        if (!controller.signal.aborted) console.error('加载订单辅助数据失败:', error);
      },
    );
      return (
        // 辅助数据清理回调取消账号和商品请求。
        () => controller.abort()
      );
    },
    [],
  );

  // accountMap 将账号 ID 映射为账号详情，避免订单列表重复线性查找。
  const accountMap = useMemo(
    // 账号索引回调构建稳定的 ID 到详情映射。
    () => new Map(accounts.map(
      // account 是当前账号详情。
      account => [account.id, account],
    )),
    [accounts],
  );
  // itemNames 将商品 ID 映射为商品标题，供订单行快速读取。
  const itemNames = useMemo(
    // 商品索引回调构建稳定的商品 ID 到标题映射。
    () => {
      // namesMap 是商品名称索引的可变构建结果。
      const namesMap: Record<string, string> = {};
      items.forEach(
        // item 是当前商品列表项。
        item => {
          if (item.item_id) namesMap[`${item.cookie_id}:${item.item_id}`] = item.item_title || item.item_id;
        },
      );
      return namesMap;
    },
    [items],
  );

  // accountName 将账号 ID 格式化为筛选下拉框名称。
  const accountName = useCallback(
    // 账号名称回调格式化筛选下拉框文本。
    (cookieId: string): string => {
    // account 是当前订单所属账号详情。
    const account = accountMap.get(cookieId);
    // name 是优先使用备注或昵称的展示名称。
    const name = account?.remark || account?.nickname;
    return name ? `${name} · ${cookieId.slice(0, 6)}` : `账号 ${cookieId.slice(0, 8)}`;
    },
    [accountMap],
  );

  // accountNickname 将账号 ID 格式化为订单行中的短名称。
  const accountNickname = useCallback(
    // 账号昵称回调格式化订单行文本。
    (cookieId: string): string => {
    // account 是当前订单所属账号详情。
    const account = accountMap.get(cookieId);
    return account?.remark || account?.nickname || '未命名账号';
    },
    [accountMap],
  );

  // getItemNameById 按订单标题、商品索引和相似标题依次解析商品名称。
  const getItemNameById = useCallback(
    // 商品名称回调按订单信息解析展示标题。
    (cookieId: string, itemId: string, orderItemTitle?: string): string => {
    if (orderItemTitle?.trim()) return orderItemTitle;
    // mappedName 是商品索引中直接命中的标题。
    const mappedName = itemNames[`${cookieId}:${itemId}`];
    if (mappedName) return mappedName;
    // matchingItem 是标题相似度匹配到的商品。
    const matchingItem = items.find(
      // item 是参与标题相似度匹配的商品。
      item => {
      if (!orderItemTitle || !item.item_title) return false;
      // orderTitleLower 是订单标题的标准化小写文本。
      const orderTitleLower = orderItemTitle.toLowerCase();
      // itemTitleLower 是商品标题的标准化小写文本。
      const itemTitleLower = item.item_title.toLowerCase();
      return itemTitleLower.includes(orderTitleLower) || orderTitleLower.includes(itemTitleLower);
      },
    );
    return matchingItem?.item_title || '未知商品';
    },
    [itemNames, items],
  );

  return {
    orders,
    items,
    accounts,
    filter,
    setFilter,
    accountFilter,
    setAccountFilter,
    searchText,
    setSearchText,
    page,
    setPage,
    totalPages,
    loading,
    loadOrders,
    accountName,
    accountNickname,
    getItemNameById,
  };
};

// useOrderImport 统一管理订单导入文件、取消、重试和导入后刷新。
export const useOrderImport = (loadOrders: () => Promise<void>): OrderImportState => {
  // showImportModal 表示订单导入弹窗是否打开。
  const [showImportModal, setShowImportModal] = useState(false);
  // importFile 保存当前选择的导入文件。
  const [importFile, setImportFile] = useState<File | null>(null);
  // importResult 保存最近一次导入的逐行结果。
  const [importResult, setImportResult] = useState<ReturnType<typeof normalizeOrderImportResult> | null>(null);
  // importing 表示导入请求是否正在执行。
  const [importing, setImporting] = useState(false);
  // importError 保存导入失败后的可重试错误。
  const [importError, setImportError] = useState('');
  // importGeneration 隔离关闭弹窗后的旧导入响应。
  const importGeneration = useRef(0);
  // importAbort 保存当前导入请求的取消控制器。
  const importAbort = useRef<AbortController | null>(null);

  // openImportModal 打开订单导入弹窗并重置上一次结果。
  const openImportModal = useCallback(
    // 打开弹窗回调清理上一次导入状态。
    () => {
    setImportFile(null);
    setImportResult(null);
    setImportError('');
    setShowImportModal(true);
    },
    [],
  );

  // closeImportModal 关闭弹窗并取消未完成的导入请求。
  const closeImportModal = useCallback(
    // 关闭弹窗回调取消上传并使旧响应失效。
    () => {
    importGeneration.current += 1;
    importAbort.current?.abort();
    setImporting(false);
    setShowImportModal(false);
    setImportFile(null);
    setImportResult(null);
    setImportError('');
    },
    [],
  );

  // handleImportOrders 提交文件，刷新列表并保留失败文件供重试。
  const handleImportOrders = useCallback(
    // 订单导入回调执行预检、上传和列表刷新。
    async () => {
    if (!canSubmitOrderImport(importFile, importing)) return;
    // file 是经过提交门禁确认存在的本次上传文件。
    const file = importFile;
    if (!file) return;
    // validationError 是文件预检失败时展示给用户的说明。
    const validationError = validateOrderImportFile(file);
    if (validationError) {
      setImportError(validationError);
      return;
    }
    // generation 标记当前订单导入请求的代次。
    const generation = ++importGeneration.current;
    importAbort.current?.abort();
    // controller 允许关闭弹窗时取消文件上传。
    const controller = new AbortController();
    importAbort.current = controller;
    setImporting(true);
    setImportResult(null);
    setImportError('');
    try {
      // formData 是上传订单文件的 multipart 请求体。
      const formData = new FormData();
      formData.append('file', file);
      // result 是后端返回的订单导入统计。
      const result = normalizeOrderImportResult(await importOrders(formData, { signal: controller.signal }));
      if (!isCurrentOrderRequest(generation, importGeneration.current)) return;
      setImportResult(result);
      setImportFile(null);
      await loadOrders();
      if (result.failed_count === 0 && isCurrentOrderRequest(generation, importGeneration.current)) {
        alert(`订单导入成功，共 ${result.success_count} 条`);
        setShowImportModal(false);
        setImportResult(null);
      }
    } catch (error: unknown /* 订单导入异常 */) {
      if (isCurrentOrderRequest(generation, importGeneration.current) && !controller.signal.aborted) {
        setImportError(orderErrorMessage(error, '导入失败，请检查文件格式'));
      }
    } finally {
      if (isCurrentOrderRequest(generation, importGeneration.current)) setImporting(false);
    }
    },
    [importFile, importing, loadOrders],
  );

  // handleRetryImport 复用当前文件重试最近一次失败的导入。
  const handleRetryImport = useCallback(
    // 导入重试回调复用当前文件和请求边界。
    async () => {
      await handleImportOrders();
    },
    [handleImportOrders],
  );

  return {
    showImportModal,
    importFile,
    setImportFile,
    importResult,
    importing,
    importError,
    openImportModal,
    closeImportModal,
    handleImportOrders,
    handleRetryImport,
  };
};
