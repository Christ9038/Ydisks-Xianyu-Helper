import { describe, expect, test } from 'vitest';
import type { AccountDetail } from '../types';
import { mergeAccountRuntimeStatuses } from './accountRuntimeState';

const account = (overrides: Partial<AccountDetail> = {}): AccountDetail => ({
  id: 'account-1',
  cookie: 'cookie',
  enabled: true,
  auto_confirm: false,
  ...overrides,
});

describe('mergeAccountRuntimeStatuses', () => {
  test('用最新在线状态替换风控恢复中的旧提示', () => {
    const result = mergeAccountRuntimeStatuses([
      account({
        runtime_state: 'connecting',
        runtime_message: 'token 风控验证已处理，正在重新获取登录凭证',
        runtime_updated_at: '2026-07-13T13:16:00+08:00',
      }),
    ], {
      'account-1': {
        state: 'online',
        message: '消息服务连接正常',
        connected: true,
        failures: 0,
        updated_at: '2026-07-13T13:16:02+08:00',
      },
    });

    expect(result[0]).toMatchObject({
      runtime_state: 'online',
      runtime_message: '消息服务连接正常',
      runtime_connected: true,
      runtime_updated_at: '2026-07-13T13:16:02+08:00',
    });
  });

  test('忽略晚到达的旧状态响应', () => {
    const current = account({
      runtime_state: 'online',
      runtime_message: '消息服务连接正常',
      runtime_updated_at: '2026-07-13T13:16:02+08:00',
    });
    const result = mergeAccountRuntimeStatuses([current], {
      'account-1': {
        state: 'connecting',
        message: 'token 风控验证已处理，正在重新获取登录凭证',
        connected: false,
        failures: 0,
        updated_at: '2026-07-13T13:16:00+08:00',
      },
    });

    expect(result[0]).toBe(current);
  });

  test('缺少对应运行时状态时保留账号对象', () => {
    const current = account();
    expect(mergeAccountRuntimeStatuses([current], {})[0]).toBe(current);
  });
});
