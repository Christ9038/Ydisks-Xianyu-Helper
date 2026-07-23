import type { AccountRuntimeStatus } from '../services/api';
import type { AccountDetail } from '../types';

const isOlderStatus = (currentUpdatedAt?: string, incomingUpdatedAt?: string): boolean => {
  if (!currentUpdatedAt || !incomingUpdatedAt) return false;
  const currentTime = Date.parse(currentUpdatedAt);
  const incomingTime = Date.parse(incomingUpdatedAt);
  return Number.isFinite(currentTime) && Number.isFinite(incomingTime) && incomingTime < currentTime;
};

/**
 * 将运行时快照合并到账号列表。较晚到达的旧请求不能覆盖更新的状态。
 */
export const mergeAccountRuntimeStatuses = (
  accounts: AccountDetail[],
  statuses: Record<string, AccountRuntimeStatus>,
): AccountDetail[] => accounts.map(account => {
  const status = statuses[account.id];
  if (!status || isOlderStatus(account.runtime_updated_at, status.updated_at)) return account;
  return {
    ...account,
    runtime_state: status.state,
    runtime_message: status.message || '',
    runtime_connected: status.connected,
    runtime_updated_at: status.updated_at,
  };
});
