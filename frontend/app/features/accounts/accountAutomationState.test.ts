import { expect, test } from 'vitest';
import type { AccountDetail } from '../../../shared/api-contract';
import { buildAccountTaskDefaults, canStartAccountTask, isCurrentAccountTaskRequest } from './accountAutomationState';

// accountFixture 是覆盖任务默认值和账号禁用边界的最小账号数据。
const accountFixture: AccountDetail = {
  id: 'a1', enabled: true, auto_confirm: false, auto_rate_enabled: true, rate_content: '交易愉快', auto_polish_enabled: false, polish_time: '03:00',
};

test('账号任务默认设置继承账号配置并保持安全文案',
  // 默认值测试验证新账号没有文案时仍使用明确的中文好评内容。
  () => {
    expect(buildAccountTaskDefaults(accountFixture)).toMatchObject({ account_id: 'a1', auto_rate_enabled: true, rate_content: '交易愉快', polish_time: '03:00' });
    expect(buildAccountTaskDefaults({ ...accountFixture, rate_content: '' }).rate_content).toBe('不错的买家，交易愉快');
  });
test('账号任务阻断重复提交并拒绝过期响应',
  // 动作边界测试验证保存或执行中不会重复发起任务，账号切换后的旧响应不能写入。
  () => {
    // controller 请求取消控制器。
    const controller = new AbortController();
    expect(canStartAccountTask(false, '')).toBe(true);
    expect(canStartAccountTask(true, '')).toBe(false);
    expect(canStartAccountTask(false, 'auto_rate')).toBe(false);
    expect(isCurrentAccountTaskRequest(4, 4, controller.signal)).toBe(true);
    expect(isCurrentAccountTaskRequest(3, 4, controller.signal)).toBe(false);
    controller.abort();
    expect(isCurrentAccountTaskRequest(4, 4, controller.signal)).toBe(false);
  });
