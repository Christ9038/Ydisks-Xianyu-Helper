import { expect, test } from 'vitest';
import type { UserConfig } from 'vite';
import config from './vite.config';

test('development server proxies every automation API prefix', () => {
  const proxy = (config as UserConfig).server?.proxy;
  expect(proxy).toBeDefined();
  expect(proxy).toHaveProperty('/automation-rules');
  expect(proxy).toHaveProperty('/automation-issues');
  expect(proxy).toHaveProperty('/automation-runs');
  expect(proxy).toHaveProperty('/automation-pending-tasks');
});
