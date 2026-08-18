import { describe,expect,test } from 'vitest';
import type { AccountDetail } from '../../../shared/api-contract/accounts';
import { accountRuntimePresentation,isOlderStatus,mergeAccountRuntimeStatuses } from './runtime';

// account 创建运行状态测试使用的最小账号对象。
const account = (overrides: Partial<AccountDetail> = {}): AccountDetail => ({
  id: 'account-1',
  enabled: true,
  auto_confirm: false,
  ...overrides,
});

describe('Accounts runtime state', /* 当前回调处理用户交互或异步状态变化。 */ () => {
  test('最新在线状态替换风控恢复中的旧提示', /* 当前回调处理用户交互或异步状态变化。 */ () => {
    // result 是合并当前运行状态后的账号列表。
    const result = mergeAccountRuntimeStatuses([account({
      runtime_state: 'connecting',
      runtime_message: 'token 风控验证已处理，正在重新获取登录凭证',
      runtime_updated_at: '2026-07-13T13:16:00+08:00',
    })], {
      'account-1': {
        state: 'online',
        message: '消息服务连接正常',
        connected: true,
        failures: 0,
        updated_at: '2026-07-13T13:16:02+08:00',
      },
    });

    expect(result[0]).toMatchObject({ runtime_state: 'online', runtime_message: '消息服务连接正常', runtime_connected: true });
  });

  test('晚到达的旧状态响应不会覆盖当前账号状态', /* 当前回调处理用户交互或异步状态变化。 */ () => {
    // current 是当前页面已经展示的较新账号状态。
    const current = account({
      runtime_state: 'online',
      runtime_message: '消息服务连接正常',
      runtime_updated_at: '2026-07-13T13:16:02+08:00',
    });
    // result 处理结果。
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

  test('缺少对应运行状态时保留原账号对象', /* 当前回调处理用户交互或异步状态变化。 */ () => {
    // current 是没有新运行状态可合并的账号对象。
    const current = account();
    expect(mergeAccountRuntimeStatuses([current], {})[0]).toBe(current);
  });

  test('时间戳缺失或无效时不误判为旧状态', /* 当前回调处理用户交互或异步状态变化。 */ () => {
    expect(isOlderStatus(undefined, '2026-01-01T00:00:00Z')).toBe(false);
    expect(isOlderStatus('invalid', '2026-01-01T00:00:00Z')).toBe(false);
    expect(isOlderStatus('2026-01-02T00:00:00Z', '2026-01-01T00:00:00Z')).toBe(true);
    expect(isOlderStatus('2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z')).toBe(false);
  });

  test('所有运行状态都映射到稳定的中文展示', /* 当前回调处理用户交互或异步状态变化。 */ () => {
    // states 是运行状态到中文标签的完整断言表。
    const states = [
      ['online', '在线'], ['starting', '连接中'], ['connecting', '连接中'], ['reconnecting', '重连中'],
      ['auth_expired', '登录已失效'], ['verification_required', '需要验证'], ['error', '运行异常'], ['stopped', '运行异常'],
    ] as const;
    states.forEach(
      // state 是需要验证展示文案的运行状态和预期标签。
      ([state, label]) => expect(accountRuntimePresentation(account({ runtime_state: state })).label).toBe(label),
    );
    expect(accountRuntimePresentation(account({ enabled: false, runtime_state: 'online' })).label).toBe('已停用');
    expect(accountRuntimePresentation(account({ runtime_state: 'disabled' })).label).toBe('已停用');
    expect(accountRuntimePresentation(account()).label).toBe('状态检测中');
  });
});
