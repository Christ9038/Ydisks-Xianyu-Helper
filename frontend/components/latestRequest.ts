// commitIfLatest 仅在请求仍为最新时提交结果。
export const commitIfLatest = <T>(
  requestID: number,
  currentRequestID: number,
  requestKey: string,
  currentKey: string,
  value: T,
  commit: (value: T) => void,
): boolean => {
  if (requestID !== currentRequestID || requestKey !== currentKey) return false;
  commit(value);
  return true;
};
