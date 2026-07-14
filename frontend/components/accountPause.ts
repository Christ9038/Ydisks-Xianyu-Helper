import type { AccountDetail } from '../types';

export const shouldUpdateAccountPause = (
  requestedMinutes: number,
  account: Pick<AccountDetail, 'pause_duration' | 'paused'>,
): boolean => {
  const savedMinutes = account.pause_duration || 0;
  return requestedMinutes !== savedMinutes;
};
