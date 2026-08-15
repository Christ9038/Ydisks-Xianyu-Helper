// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import type { AccountDetail, AccountTaskSettingsResponse } from '../../../types';
import { getAccountTaskSettings, runAccountTask, updateAccountTaskSettings } from './api';
import { useAccountAutomation } from './hooks';

vi.mock('./api', /* apiMockFactory 提供账号任务 Hook 的确定性 API 替身。 */ () => ({
  getAccountTaskSettings: vi.fn(),
  runAccountTask: vi.fn(),
  updateAccountTaskSettings: vi.fn(),
}));

// getSettingsMock 是读取账号任务设置的可控请求替身。
const getSettingsMock = vi.mocked(getAccountTaskSettings);
// runTaskMock 是立即执行账号任务的可控请求替身。
const runTaskMock = vi.mocked(runAccountTask);
// updateSettingsMock 是保存账号任务设置的可控请求替身。
const updateSettingsMock = vi.mocked(updateAccountTaskSettings);

// accountFixture 是 Hook 测试使用的启用账号对象。
const accountFixture: AccountDetail = { id: 'account-1', enabled: true, auto_confirm: false, auto_rate_enabled: true, rate_content: '交易愉快', auto_polish_enabled: false, polish_time: '03:00' };

// settingsFixture 是账号任务接口返回的完整设置对象。
const settingsFixture: AccountTaskSettingsResponse = { account_id: 'account-1', auto_rate_enabled: true, rate_content: '已保存文案', auto_polish_enabled: true, polish_time: '04:00', last_rate_scan_at: 0, last_polish_date: '', last_polish_at: 0 };

// onSavedFixture 是保存或执行成功后的页面同步回调替身。
const onSavedFixture = vi.fn();

// renderAutomationHook 是渲染账号任务 Hook 的具名回调。
const renderAutomationHook = () => useAccountAutomation({ account: accountFixture, onSaved: onSavedFixture });

describe('useAccountAutomation', /* 当前回调处理账号任务 Hook 的请求与动作状态。 */ () => {
  beforeEach(/* 当前回调重置所有 API 替身和页面回调。 */ () => {
    vi.clearAllMocks();
    getSettingsMock.mockResolvedValue(settingsFixture);
    updateSettingsMock.mockResolvedValue(settingsFixture);
    runTaskMock.mockResolvedValue({ success: true, summary: { task_type: 'auto_rate', found: 1, success: 1, failed: 0, skipped: 0 } });
  });

  test('加载设置后可以保存和立即执行任务', /* 当前回调验证成功加载、保存和执行路径。 */ async () => {
    // hook 是账号任务 Hook 的渲染结果。
    const hook = renderHook(renderAutomationHook);
    await waitFor(
      // loadingAssertion 等待首次设置请求完成。
      () => expect(hook.result.current.loading).toBe(false),
    );
    expect(hook.result.current.form).toEqual(settingsFixture);

    await act(
      // saveAction 执行设置保存动作。
      async () => hook.result.current.save(),
    );
    expect(updateSettingsMock).toHaveBeenCalledWith('account-1', settingsFixture, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(onSavedFixture).toHaveBeenCalledWith(settingsFixture);

    await act(
      // runAction 执行自动擦亮任务。
      async () => hook.result.current.run('auto_polish'),
    );
    expect(runTaskMock).toHaveBeenCalledWith('account-1', 'auto_polish', expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.summary).toMatchObject({ success: 1 });
    expect(hook.result.current.error).toBe('');
  });

  test('保存失败后可通过 retry 重试并清除错误', /* 当前回调验证失败状态和安全重试路径。 */ async () => {
    // hook 是保存失败场景下的账号任务 Hook 渲染结果。
    const hook = renderHook(renderAutomationHook);
    await waitFor(
      // loadingAssertion 等待失败场景的首次设置请求完成。
      () => expect(hook.result.current.loading).toBe(false),
    );
    updateSettingsMock.mockRejectedValueOnce(new Error('保存失败'));
    await act(
      // saveAction 执行预期失败的设置保存动作。
      async () => hook.result.current.save(),
    );
    expect(hook.result.current.error).toBe('保存失败');
    expect(hook.result.current.retryAvailable).toBe(true);
    await act(
      // retryAction 触发最近一次保存动作的重试。
      async () => hook.result.current.retry(),
    );
    expect(updateSettingsMock).toHaveBeenCalledTimes(2);
    expect(hook.result.current.error).toBe('');
  });

  test('禁用账号不会发起立即执行请求', /* 当前回调验证账号运行门禁。 */ async () => {
    // disabledAccount 是不允许执行账号级计划任务的账号对象。
    const disabledAccount = { ...accountFixture, enabled: false };
    // renderDisabledHook 是禁用账号 Hook 的渲染回调。
    const renderDisabledHook = () => useAccountAutomation({ account: disabledAccount, onSaved: onSavedFixture });
    // hook 是禁用账号 Hook 的渲染结果。
    const hook = renderHook(renderDisabledHook);
    await waitFor(
      // loadingAssertion 等待禁用账号的设置读取完成。
      () => expect(hook.result.current.loading).toBe(false),
    );
    await act(
      // runAction 验证禁用账号不会提交自动评价任务。
      async () => hook.result.current.run('auto_rate'),
    );
    expect(runTaskMock).not.toHaveBeenCalled();
  });
});
