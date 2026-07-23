import { expect, test, vi } from 'vitest';
import { commitIfLatest } from './latestRequest';

test('stale account responses cannot replace the latest rules', () => {
  const commit = vi.fn();
  expect(commitIfLatest(1, 2, 'a', 'b', ['account-a'], commit)).toBe(false);
  expect(commit).not.toHaveBeenCalled();
  expect(commitIfLatest(2, 2, 'a', 'b', ['account-a'], commit)).toBe(false);
  expect(commitIfLatest(2, 2, 'b', 'b', ['account-b'], commit)).toBe(true);
  expect(commit).toHaveBeenCalledWith(['account-b']);
});
