import { useCallback,useEffect,useRef,useState } from 'react';
import { getAuditOrderDeliveredCards,getAutomationAuditEvents } from './api';
import type { AuditDeliveredCard,AutomationAuditPage,AutomationAuditQuery,AutomationAuditStatus } from './auditModels';

// auditAutoRefreshMs 是自动刷新的间隔；规则日志只读取本地数据库，不触发任何平台请求。
export const auditAutoRefreshMs = 15_000;

// timelinePageSize 是单次运行时间线的读取上限，一次运行的事件数远小于该值。
const timelinePageSize = 100;

// errorText 把请求失败原因转换为可展示的中文提示，取消错误由调用方在此之前过滤。
const errorText = (error: unknown, fallback: string): string => (error instanceof Error && error.message ? error.message : fallback);

// isAbort 判断错误是否由取消请求触发，取消不应展示为失败。
const isAbort = (error: unknown): boolean => error instanceof DOMException && error.name === 'AbortError';

// AutomationAuditLogResult 暴露规则日志当前页、加载状态与筛选操作。
export interface AutomationAuditLogResult {
  // page 是最近一次成功加载的一页；首次加载完成前为 null。
  page: AutomationAuditPage | null;
  // query 是当前生效的查询条件（含外部传入的账号）。
  query: AutomationAuditQuery;
  // loading 表示当前查询是否仍在进行。
  loading: boolean;
  // error 是最近一次查询失败的提示；成功或开始新查询后清空。
  error: string | null;
  // autoRefresh 表示是否开启定时自动刷新。
  autoRefresh: boolean;
  // setStatus、setTriggerType 切换筛选并回到第一页。
  setStatus: (status: AutomationAuditStatus | '') => void;
  setTriggerType: (triggerType: string) => void;
  // applySearch 应用订单号搜索词并回到第一页。
  applySearch: (search: string) => void;
  // setPage 跳转到指定页码。
  setPage: (page: number) => void;
  // setAutoRefresh 开关自动刷新。
  setAutoRefresh: (enabled: boolean) => void;
  // reload 用当前条件重新查询。
  reload: () => void;
}

// useAutomationAuditLog 加载规则日志。cookieId 来自页面顶部的账号选择；账号变化时调用方应通过 key 重新挂载以重置分页。
// 每次查询使用独立 AbortController，条件变化或卸载会取消旧请求，过期响应不会覆盖当前结果。
export const useAutomationAuditLog = (cookieId: string): AutomationAuditLogResult => {
  // filters 保存用户选择的筛选、搜索和分页条件，属于瞬时 UI 状态；账号由参数提供，不重复存放。
  const [filters, setFilters] = useState({ status: '' as AutomationAuditStatus | '', triggerType: '', search: '', page: 1 });
  // page 保存最近一次成功的服务端结果。
  const [page, setPageData] = useState<AutomationAuditPage | null>(null);
  // loading 表示查询进行中。
  const [loading, setLoading] = useState(true);
  // error 保存最近一次查询失败的提示。
  const [error, setError] = useState<string | null>(null);
  // autoRefresh 表示是否定时刷新。
  const [autoRefresh, setAutoRefresh] = useState(false);
  // reloadToken 递增时强制按当前条件重新查询。
  const [reloadToken, setReloadToken] = useState(0);

  // query 是传给 API 的完整条件，由筛选状态与外部账号派生。
  const query: AutomationAuditQuery = { cookieId, runId: 0, pageSize: 20, ...filters };
  // queryKey 把查询条件序列化为稳定字符串，作为查询副作用的依赖，避免每次渲染的新对象重复触发请求。
  const queryKey = JSON.stringify(query);

  useEffect(
    // 查询副作用：条件或重载令牌变化时发起请求，清理函数取消尚未完成的旧请求。
    () => {
      // controller 绑定本次查询的生命周期。
      const controller = new AbortController();
      setLoading(true);
      setError(null);
      getAutomationAuditEvents(JSON.parse(queryKey) as AutomationAuditQuery, { signal: controller.signal })
        .then(/* result 是本次查询返回的一页事件，仅当请求未被取消时才写入。 */ result => {
          if (!controller.signal.aborted) setPageData(result);
        })
        .catch(/* failure 是本次查询的失败原因，取消不展示。 */ failure => {
          if (!controller.signal.aborted && !isAbort(failure)) setError(errorText(failure, '读取规则日志失败'));
        })
        .finally(/* 当前回调只在请求仍有效时结束加载态。 */ () => {
          if (!controller.signal.aborted) setLoading(false);
        });
      return /* 清理函数取消尚未完成的旧查询。 */ () => controller.abort();
    },
    [queryKey, reloadToken],
  );

  useEffect(
    // 自动刷新副作用：开启后按固定间隔递增重载令牌，关闭或卸载时清除定时器。
    () => {
      if (!autoRefresh) return undefined;
      // timer 是自动刷新定时器。
      const timer = window.setInterval(/* 当前回调触发一次重新查询。 */ () => setReloadToken(/* token 是当前重载令牌。 */ token => token + 1), auditAutoRefreshMs);
      return /* 清理函数停止自动刷新定时器。 */ () => window.clearInterval(timer);
    },
    [autoRefresh],
  );

  return {
    page,
    query,
    loading,
    error,
    autoRefresh,
    setStatus: useCallback(/* status 是新的状态筛选。 */ (status: AutomationAuditStatus | '') => setFilters(/* current 是当前筛选。 */ current => ({ ...current, status, page: 1 })), []),
    setTriggerType: useCallback(/* triggerType 是新的触发类型筛选。 */ (triggerType: string) => setFilters(/* current 是当前筛选。 */ current => ({ ...current, triggerType, page: 1 })), []),
    applySearch: useCallback(/* search 是新的订单号搜索词。 */ (search: string) => setFilters(/* current 是当前筛选。 */ current => ({ ...current, search, page: 1 })), []),
    setPage: useCallback(/* next 是目标页码。 */ (next: number) => setFilters(/* current 是当前筛选。 */ current => ({ ...current, page: Math.max(1, next) })), []),
    setAutoRefresh,
    reload: useCallback(/* 当前回调递增重载令牌。 */ () => setReloadToken(/* token 是当前重载令牌。 */ token => token + 1), []),
  };
};

// RunTimelineResult 暴露某次运行的完整时间线。
export interface RunTimelineResult {
  // page 是时间线事件；加载完成前为 null。
  page: AutomationAuditPage | null;
  // loading 表示是否仍在读取。
  loading: boolean;
  // error 是读取失败的提示。
  error: string | null;
}

// useRunTimeline 读取某次运行的事件时间线（正序）；runId 变化或卸载会取消旧请求。
export const useRunTimeline = (runId: number): RunTimelineResult => {
  // state 合并保存读取结果、加载态与错误，三者始终同时变化。
  const [state, setState] = useState<RunTimelineResult>({ page: null, loading: true, error: null });
  useEffect(
    // 时间线副作用：runId 变化时重新读取，清理函数取消旧请求。
    () => {
      // controller 绑定本次读取的生命周期。
      const controller = new AbortController();
      setState({ page: null, loading: true, error: null });
      getAutomationAuditEvents({ cookieId: '', status: '', triggerType: '', search: '', runId, page: 1, pageSize: timelinePageSize }, { signal: controller.signal })
        .then(/* result 是该运行的全部事件。 */ result => {
          if (!controller.signal.aborted) setState({ page: result, loading: false, error: null });
        })
        .catch(/* failure 是读取失败原因，取消不展示。 */ failure => {
          if (!controller.signal.aborted && !isAbort(failure)) setState({ page: null, loading: false, error: errorText(failure, '读取运行时间线失败') });
        });
      return /* 清理函数取消尚未完成的时间线读取。 */ () => controller.abort();
    },
    [runId],
  );
  return state;
};

// DeliveredCardsRevealResult 暴露按需查看已发卡密的状态与操作。
export interface DeliveredCardsRevealResult {
  // cards 是已读取的卡密；未查看时为 null，隐藏后立即清空。
  cards: AuditDeliveredCard[] | null;
  // loading 表示是否正在读取。
  loading: boolean;
  // error 是读取失败的提示。
  error: string | null;
  // reveal 读取并展示卡密明文，hide 清空明文。
  reveal: () => void;
  // hide 取消读取并立即清空明文。
  hide: () => void;
}

// useDeliveredCardsReveal 按需读取订单已发卡密。明文只在用户主动 reveal 后保存在组件状态中，hide 或卸载（切换订单、关闭抽屉）即清空。
export const useDeliveredCardsReveal = (orderId: string): DeliveredCardsRevealResult => {
  // cards 保存已读取的卡密明文。
  const [cards, setCards] = useState<AuditDeliveredCard[] | null>(null);
  // loading 表示读取中。
  const [loading, setLoading] = useState(false);
  // error 保存读取失败的提示。
  const [error, setError] = useState<string | null>(null);
  // controllerRef 保存当前读取的取消控制器，hide 与卸载时用于取消。
  const controllerRef = useRef<AbortController | null>(null);

  useEffect(
    // 订单切换或卸载时取消读取并清空明文，防止卡密残留在下一个订单的界面上。
    () => /* 清理函数在订单切换或卸载时取消读取并清空明文。 */ () => {
      controllerRef.current?.abort();
      controllerRef.current = null;
      setCards(null);
      setError(null);
      setLoading(false);
    },
    [orderId],
  );

  // reveal 发起读取；重复点击会取消上一次未完成的读取。
  const reveal = useCallback(/* 当前回调发起一次按需读取。 */ () => {
    controllerRef.current?.abort();
    // controller 绑定本次读取的生命周期。
    const controller = new AbortController();
    controllerRef.current = controller;
    setLoading(true);
    setError(null);
    getAuditOrderDeliveredCards(orderId, { signal: controller.signal })
      .then(/* result 是订单已发卡密。 */ result => {
        if (!controller.signal.aborted) setCards(result);
      })
      .catch(/* failure 是读取失败原因，取消不展示。 */ failure => {
        if (!controller.signal.aborted && !isAbort(failure)) setError(errorText(failure, '读取已发卡密失败'));
      })
      .finally(/* 当前回调只在请求仍有效时结束加载态。 */ () => {
        if (!controller.signal.aborted) setLoading(false);
      });
  }, [orderId]);

  // hide 取消读取并立即清空明文。
  const hide = useCallback(/* 当前回调取消读取并清空明文。 */ () => {
    controllerRef.current?.abort();
    controllerRef.current = null;
    setCards(null);
    setLoading(false);
  }, []);

  return { cards, loading, error, reveal, hide };
};
