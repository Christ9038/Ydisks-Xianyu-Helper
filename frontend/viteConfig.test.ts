import { expect, test } from 'vitest';
import type { UserConfig } from 'vite';
import config from './vite.config';

test('development server proxies versioned API and health checks', () => {
  const proxy = (config as UserConfig).server?.proxy; /* proxy 表示proxy。 */
  expect(proxy).toBeDefined();
  expect(proxy).toHaveProperty('/api');
  expect(proxy).toHaveProperty('/health');
  expect(proxy).not.toHaveProperty('/automation-rules');
  expect(proxy).not.toHaveProperty('/automation-issues');
  expect(proxy).not.toHaveProperty('/automation-runs');
  expect(proxy).not.toHaveProperty('/automation-pending-tasks');
  expect(proxy).not.toHaveProperty('/password-login');
  expect(proxy).not.toHaveProperty('/qr-login');
  expect(proxy).not.toHaveProperty('/items');
  expect(proxy).not.toHaveProperty('/cookies');
} /* 回调函数负责当前业务流程。 */);
