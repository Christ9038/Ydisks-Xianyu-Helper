import type { AccountDetail } from './api';
import type { AccountRuntimeStatus } from './api';
import type { AccountRuntimePresentation } from './types';

// isOlderStatus 判断传入的运行时快照是否早于账号当前已展示的快照。
export const isOlderStatus = (currentUpdatedAt?: string, incomingUpdatedAt?: string): boolean => {
  if (!currentUpdatedAt || !incomingUpdatedAt) return false;
  // currentTime 是账号当前已展示快照的时间戳。
  const currentTime = Date.parse(currentUpdatedAt);
  // incomingTime 是本次轮询返回快照的时间戳。
  const incomingTime = Date.parse(incomingUpdatedAt);
  return Number.isFinite(currentTime) && Number.isFinite(incomingTime) && incomingTime < currentTime;
};

// mergeAccountRuntimeStatuses 将最新运行时快照合并到账号列表，并拒绝过期响应。
export const mergeAccountRuntimeStatuses = (
  accounts: AccountDetail[],
  statuses: Record<string, AccountRuntimeStatus>,
): AccountDetail[] => accounts.map(
  // account 是待合并运行时快照的账号对象。
  account => {
  // status 是服务端按账号 ID 返回的运行时状态。
  const status = statuses[account.id];
  if (!status || isOlderStatus(account.runtime_updated_at, status.updated_at)) return account;
  return {
    ...account,
    runtime_state: status.state,
    runtime_message: status.message || '',
    runtime_connected: status.connected,
    runtime_updated_at: status.updated_at,
  };
  },
);

// accountRuntimePresentation 将后端运行状态转换为列表徽标和状态点样式。
export const accountRuntimePresentation = (account: AccountDetail): AccountRuntimePresentation => {
  if (!account.enabled || account.runtime_state === 'disabled') {
    return { label: '已停用', badge: 'bg-gray-100 text-gray-500', dot: 'bg-gray-300' };
  }
  switch (account.runtime_state) {
    case 'online':
      return { label: '在线', badge: 'bg-green-100 text-green-700', dot: 'bg-green-500' };
    case 'starting':
    case 'connecting':
      return { label: '连接中', badge: 'bg-blue-100 text-blue-700', dot: 'bg-blue-500' };
    case 'reconnecting':
      return { label: '重连中', badge: 'bg-amber-100 text-amber-700', dot: 'bg-amber-500' };
    case 'auth_expired':
      return { label: '登录已失效', badge: 'bg-red-100 text-red-700', dot: 'bg-red-500' };
    case 'verification_required':
      return { label: '需要验证', badge: 'bg-orange-100 text-orange-700', dot: 'bg-orange-500' };
    case 'runtime_conflict':
      return { label: '运行状态冲突', badge: 'bg-red-100 text-red-700', dot: 'bg-red-500' };
    case 'error':
    case 'stopped':
      return { label: '运行异常', badge: 'bg-red-100 text-red-700', dot: 'bg-red-500' };
    default:
      return { label: '状态检测中', badge: 'bg-gray-100 text-gray-600', dot: 'bg-gray-400' };
  }
};
