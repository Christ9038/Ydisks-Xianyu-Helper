import { useCallback, useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react';
import { getAccountDetails, getAccountRuntimeStatuses, getAllAISettings } from './api';
import type { AccountDetail } from '../../../shared/api-contract';
import { mergeAccountRuntimeStatuses } from './runtime';
import type { AccountAISettingsState } from './types';

// AccountsDataResult 暴露账号列表、加载状态和可复用的刷新动作。
export interface AccountsDataResult {
  // accounts 是当前账号列表及其 AI、运行时展示字段。
  accounts: AccountDetail[];
  // setAccounts 允许页面在删除或任务设置保存后局部更新列表。
  setAccounts: Dispatch<SetStateAction<AccountDetail[]>>;
  // loading 表示账号基础数据是否正在加载。
  loading: boolean;
  // loadAccounts 刷新账号基础资料和 AI 设置。
  loadAccounts: () => Promise<void>;
}

// mergeAccountAISettings 将 AI 配置补充到账号详情，缺省值保持页面原有行为。
export const mergeAccountAISettings = (
  accounts: AccountDetail[],
  allAISettings: AccountAISettingsState,
): AccountDetail[] => accounts.map(
  // account 是待补充 AI 配置的账号对象。
  account => ({
    ...account,
    ai_enabled: allAISettings[account.id]?.ai_enabled ?? false,
    max_discount_percent: allAISettings[account.id]?.max_discount_percent ?? 10,
    max_discount_amount: allAISettings[account.id]?.max_discount_amount ?? 100,
    max_bargain_rounds: allAISettings[account.id]?.max_bargain_rounds ?? 3,
    custom_prompts: allAISettings[account.id]?.custom_prompts ?? '',
  }),
);

// useAccountsData 统一管理账号加载、AI 合并和运行状态短轮询。
export const useAccountsData = (): AccountsDataResult => {
  // accounts 保存页面当前可见的账号详情。
  const [accounts, setAccounts] = useState<AccountDetail[]>([]);
  // loading 表示首次加载或手动刷新的账号基础数据是否未完成。
  const [loading, setLoading] = useState(true);
  // accountLoadGeneration 丢弃切换或连续刷新后到达的旧账号响应。
  const accountLoadGeneration = useRef(0);
  // accountLoadAbort 保存当前账号加载请求的取消控制器。
  const accountLoadAbort = useRef<AbortController | null>(null);

  // loadAccounts 并行读取账号详情和 AI 设置，并只提交最新请求结果。
  const loadAccounts = useCallback(
    // 账号加载回调并行请求详情和 AI 设置。
    async () => {
    // generation 标记本次账号刷新请求的代次。
    const generation = ++accountLoadGeneration.current;
    accountLoadAbort.current?.abort();
    // controller 让新刷新开始时主动取消旧的网络请求。
    const controller = new AbortController();
    accountLoadAbort.current = controller;
    setLoading(true);
    try {
      // options 将同一个取消信号传给两个彼此独立的请求。
      const options = { signal: controller.signal };
      // results 让账号详情和 AI 配置并行加载，AI 失败不阻断账号列表。
      const [detailsResult, aiResult] = await Promise.allSettled([
        getAccountDetails(options),
        getAllAISettings(options),
      ]);
      if (generation !== accountLoadGeneration.current) return;
      if (detailsResult.status === 'rejected') throw detailsResult.reason;
      // allAISettings 是按账号 ID 索引的 AI 配置快照。
      const allAISettings = aiResult.status === 'fulfilled' ? aiResult.value : {};
      if (aiResult.status === 'rejected') console.error('加载 AI 设置失败:', aiResult.reason);
      setAccounts(mergeAccountAISettings(detailsResult.value, allAISettings));
    } catch (error /* 账号加载错误 */) {
      if (generation === accountLoadGeneration.current && !controller.signal.aborted) {
        console.error('加载账号失败:', error);
      }
    } finally {
      if (generation === accountLoadGeneration.current) setLoading(false);
    }
    },
    [],
  );

  useEffect(
    // 账号数据副作用负责首次加载和运行状态轮询。
    () => {
    // cancelled 表示页面已卸载，阻止轮询继续写入 React 状态。
    let cancelled = false;
    // timer 保存下一次运行状态轮询的句柄。
    let timer: number | null = null;
    // runtimeController 取消页面卸载时仍在执行的运行状态请求。
    const runtimeController = new AbortController();

    // pollRuntimeStatuses 读取进程内运行状态并安排下一次轮询。
    const pollRuntimeStatuses = async () => {
      try {
        // statuses 是后端按账号 ID 返回的最新运行状态快照。
        const statuses = await getAccountRuntimeStatuses({ signal: runtimeController.signal, timeoutMs: 10_000 });
        if (!cancelled) {
          setAccounts(
            // current 是运行状态轮询开始前的最新账号列表。
            current => mergeAccountRuntimeStatuses(current, statuses),
          );
        }
      } catch (error /* 运行状态轮询错误 */) {
        if (!cancelled) console.error('加载账号运行状态失败:', error);
      } finally {
        if (!cancelled) {
          timer = window.setTimeout(
            // timerCallback 在两秒后启动下一次运行状态轮询。
            () => void pollRuntimeStatuses(),
            2_000,
          );
        }
      }
    };

    void loadAccounts().finally(
      // loadFinished 在初始账号加载完成后启动第一次状态轮询。
      () => {
      if (!cancelled) void pollRuntimeStatuses();
      },
    );
    // cleanup 在页面卸载时停止账号请求和运行状态轮询。
    const cleanup = () => {
      cancelled = true;
      runtimeController.abort();
      accountLoadGeneration.current += 1;
      accountLoadAbort.current?.abort();
      if (timer !== null) window.clearTimeout(timer);
    };
    return cleanup;
    },
    [loadAccounts],
  );

  return { accounts, setAccounts, loading, loadAccounts };
};
