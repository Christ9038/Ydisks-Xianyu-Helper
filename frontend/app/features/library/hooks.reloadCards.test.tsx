// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import * as api from './api';
import { useAutomationOptions } from './hooks';
import type { LibraryCardOption } from './types';

vi.mock('./api', /* 保留真实纯函数并替换三个读取接口。 */ async (importOriginal) => {
  // original 保留真实的 libraryErrorMessage 等纯函数。
  const original = await importOriginal<typeof import('./api')>();
  return { ...original, listCardOptions: vi.fn(), listDeliveryTemplateOptions: vi.fn(), listAccountOptions: vi.fn() };
});

describe('useAutomationOptions.reloadCards', /* 只刷新卡密，且丢弃过期响应。 */ () => {
  beforeEach(/* 重置替身。 */ () => {
    vi.clearAllMocks();
    vi.mocked(api.listCardOptions).mockResolvedValue([{ id: 1, name: '旧卡', type: 'data' }]);
    vi.mocked(api.listDeliveryTemplateOptions).mockResolvedValue([]);
    vi.mocked(api.listAccountOptions).mockResolvedValue([]);
  });

  test('刷新后出现新卡密，其余选项不变', /* 成功路径。 */ async () => {
    // hook 是被测 Hook。
    const hook = renderHook(/* 创建 Hook。 */ () => useAutomationOptions(true));
    await waitFor(/* 等待首次加载。 */ () => expect(hook.result.current.cards).toHaveLength(1));
    vi.mocked(api.listCardOptions).mockResolvedValue([{ id: 1, name: '旧卡', type: 'data' }, { id: 2, name: '新卡', type: 'text' }]);
    await act(/* 触发刷新。 */ async () => hook.result.current.reloadCards());
    expect(hook.result.current.cards.map(/* card 提取名称。 */ card => card.name)).toEqual(['旧卡', '新卡']);
    expect(api.listDeliveryTemplateOptions).toHaveBeenCalledTimes(1);
  });

  test('较早的刷新晚于较新的刷新返回时被丢弃', /* 过期响应。 */ async () => {
    // hook 是被测 Hook。
    const hook = renderHook(/* 创建 Hook。 */ () => useAutomationOptions(true));
    await waitFor(/* 等待首次加载。 */ () => expect(hook.result.current.cards).toHaveLength(1));
    // releaseOld 用于最后才结束较早的刷新。
    let releaseOld: (cards: LibraryCardOption[]) => void = () => undefined;
    vi.mocked(api.listCardOptions).mockReturnValueOnce(new Promise(/* resolver 捕获 resolve。 */ resolve => { releaseOld = resolve; }));
    vi.mocked(api.listCardOptions).mockResolvedValueOnce([{ id: 9, name: '最新', type: 'text' }]);
    // first 是较早的刷新。
    let first: Promise<void> = Promise.resolve();
    await act(/* 先后发起两次刷新。 */ async () => { first = hook.result.current.reloadCards(); await hook.result.current.reloadCards(); });
    await act(/* 较早的刷新此时才返回。 */ async () => { releaseOld([{ id: 5, name: '过期', type: 'text' }]); await first; });
    expect(hook.result.current.cards.map(/* card 提取名称。 */ card => card.name)).toEqual(['最新']);
  });

  test('刷新失败只写入错误，取消不提示', /* 失败路径。 */ async () => {
    // hook 是被测 Hook。
    const hook = renderHook(/* 创建 Hook。 */ () => useAutomationOptions(true));
    await waitFor(/* 等待首次加载。 */ () => expect(hook.result.current.cards).toHaveLength(1));
    vi.mocked(api.listCardOptions).mockRejectedValueOnce(new Error('网络错误'));
    await act(/* 触发失败刷新。 */ async () => hook.result.current.reloadCards());
    expect(hook.result.current.error).toContain('网络错误');
    expect(hook.result.current.cards).toHaveLength(1);
  });
});
