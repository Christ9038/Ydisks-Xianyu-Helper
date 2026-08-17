import type { AccountDetail, AccountTaskSettings } from '../../../shared/api-contract';
import type { AccountTaskType } from './accountAutomationTypes';

/** 根据账号详情创建任务设置初始草稿。 */
export const buildAccountTaskDefaults = (account: AccountDetail): AccountTaskSettings => ({
  account_id: account.id,
  auto_rate_enabled: account.auto_rate_enabled === true,
  rate_content: account.rate_content || '不错的买家，交易愉快',
  auto_polish_enabled: account.auto_polish_enabled === true,
  polish_time: account.polish_time || '03:00',
  last_rate_scan_at: account.last_rate_scan_at,
  last_polish_date: account.last_polish_date,
  last_polish_at: account.last_polish_at,
});

/** 判断账号任务响应是否仍属于当前编辑账号和请求代次。 */
export const isCurrentAccountTaskRequest = (currentSequence: number, requestSequence: number, signal: AbortSignal): boolean => (
  currentSequence === requestSequence && !signal.aborted
);

/** 统一提取账号任务请求错误文本。 */
export const accountTaskErrorMessage = (error: unknown, fallback: string): string => error instanceof Error ? error.message : fallback;

/** 判断错误是否来自请求主动取消。 */
export const isAccountTaskAbortError = (error: unknown): boolean => error instanceof Error && error.message === '请求已取消';

/** 判断账号任务动作是否允许重复提交。 */
export const canStartAccountTask = (saving: boolean, running: '' | AccountTaskType): boolean => !saving && running === '';
