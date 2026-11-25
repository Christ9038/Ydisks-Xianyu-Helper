// @vitest-environment jsdom
import { act,renderHook,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import { isNewerVersion,parseVersion,useLatestRelease } from './releaseCheck';

describe('releaseCheck', /* 当前回调验证新版本检查的比较与请求行为。 */ () => {
  // fetchMock 替代浏览器 fetch，避免真实访问 GitHub。
  const fetchMock = vi.fn();
  beforeEach(/* 重置网络替身与本地缓存。 */ () => {
    fetchMock.mockReset();
    vi.stubGlobal('fetch', fetchMock);
    window.localStorage.clear();
  });
  afterEach(/* 还原全局替身。 */ () => { vi.unstubAllGlobals(); });

  test('版本解析与比较只接受标准三段版本', /* 当前回调覆盖比较分支。 */ () => {
    expect(parseVersion('v1.2.3')).toEqual([1, 2, 3]);
    expect(parseVersion('dev')).toBeUndefined();
    expect(parseVersion('1.2.3-rc1')).toBeUndefined();
    expect(isNewerVersion('v1.10.0', '1.9.9')).toBe(true);
    expect(isNewerVersion('v1.2.3', '1.2.3')).toBe(false);
    expect(isNewerVersion('v1.2.2', '1.2.3')).toBe(false);
    expect(isNewerVersion('v2.0.0', 'dev')).toBe(false);
    expect(isNewerVersion('latest', '1.0.0')).toBe(false);
  });

  test('GitHub 有更高版本时返回标签并写缓存，命中缓存后不再请求', /* 当前回调验证成功与缓存路径。 */ async () => {
    fetchMock.mockResolvedValue({ ok: true, json: /* 返回最新 Release。 */ async () => ({ tag_name: 'v1.5.0' }) });
    // first 是首次渲染的 Hook。
    const first = renderHook(/* 当前版本 1.4.0。 */ () => useLatestRelease('1.4.0'));
    await waitFor(/* 等待提示出现。 */ () => expect(first.result.current).toBe('v1.5.0'));
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ credentials: 'omit' });
    first.unmount();
    // second 复用缓存。
    const second = renderHook(/* 当前版本 1.4.0。 */ () => useLatestRelease('1.4.0'));
    await waitFor(/* 缓存命中后直接提示。 */ () => expect(second.result.current).toBe('v1.5.0'));
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  test('已是最新、非标准版本或请求失败时不提示', /* 当前回调验证静默分支。 */ async () => {
    fetchMock.mockResolvedValue({ ok: true, json: /* 返回同版本。 */ async () => ({ tag_name: 'v1.4.0' }) });
    // same 是已最新的 Hook。
    const same = renderHook(/* 当前版本等于最新。 */ () => useLatestRelease('1.4.0'));
    await waitFor(/* 等待请求完成。 */ () => expect(fetchMock).toHaveBeenCalled());
    expect(same.result.current).toBeUndefined();
    window.localStorage.clear();
    fetchMock.mockReset();
    // dev 是非标准构建版本的 Hook。
    const dev = renderHook(/* dev 构建不检查。 */ () => useLatestRelease('dev'));
    expect(fetchMock).not.toHaveBeenCalled();
    expect(dev.result.current).toBeUndefined();
    fetchMock.mockRejectedValue(new Error('network'));
    const failed = renderHook(/* 网络失败。 */ () => useLatestRelease('1.0.0'));
    await act(/* 等待拒绝被吞掉。 */ async () => { await Promise.resolve(); });
    expect(failed.result.current).toBeUndefined();
    fetchMock.mockReset();
    window.localStorage.clear();
    fetchMock.mockResolvedValue({ ok: true, json: /* 缺少 tag_name。 */ async () => ({}) });
    const noTag = renderHook(/* 响应无标签。 */ () => useLatestRelease('1.0.0'));
    await waitFor(/* 等待请求。 */ () => expect(fetchMock).toHaveBeenCalled());
    expect(noTag.result.current).toBeUndefined();
  });

  test('getPublicJSON 对非 2xx 响应抛错并被静默', /* 当前回调验证 HTTP 错误分支。 */ async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 403, json: /* 不应被读取。 */ async () => ({}) });
    const limited = renderHook(/* 被限流。 */ () => useLatestRelease('1.0.0'));
    await waitFor(/* 等待请求。 */ () => expect(fetchMock).toHaveBeenCalled());
    expect(limited.result.current).toBeUndefined();
  });
});
