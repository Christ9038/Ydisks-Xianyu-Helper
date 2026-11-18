// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { listAPIAccessTokens, type APIAccessTokenListResponse, type APIAccessTokenSummary } from './api';
import { useAPIAccessTokenList } from './useAPIAccessTokenList';

vi.mock('./api', /* apiTokenListMockFactory 以确定性异步响应替代实际网络请求。 */ () => ({
  listAPIAccessTokens: vi.fn(),
}));

// listMock 是可控制每次请求返回顺序的列表 API 替身。
const listMock = vi.mocked(listAPIAccessTokens);

// tokenSummary 构造不包含令牌明文的测试管理摘要。
const tokenSummary = (id: string): APIAccessTokenSummary => ({
  id, name: id, prefix: 'ydk_live_fixture', scopes: ['accounts:read'],
  created_at: 1, expires_at: 4_100_000_000, last_used_at: 0, revoked_at: 0,
});

// deferredList 让测试主动选择旧列表请求与新列表请求完成的顺序。
const deferredList = () => {
  // resolve、reject 对应本次模拟网络读取的成功与失败结果。
  let resolve!: (value: APIAccessTokenListResponse) => void;
  let reject!: (error: Error) => void;
  // promise 是由测试控制完成时刻的列表响应。
  const promise = new Promise<APIAccessTokenListResponse>(/* deferredExecutor 保存此异步响应的成功与失败回调。 */ (onResolve, onReject) => {
    resolve = onResolve;
    reject = onReject;
  });
  return { promise, resolve, reject };
};

// renderTokenListHook 渲染列表 Hook，验证代次保护而不依赖 UI 文案。
const renderTokenListHook = () => useAPIAccessTokenList();

describe('useAPIAccessTokenList', /* tokenListSuite 验证列表读取的成功、失败、竞争及卸载隔离。 */ () => {
  beforeEach(/* resetTokenMocks 防止其他测试残留调用状态。 */ () => {
    vi.clearAllMocks();
  });

  test('签发或撤销后的新列表先返回时，旧成功结果不能覆盖它', /* staleSuccessScenario 校验旧请求在新请求之后返回时的完整状态。 */ async () => {
    // old、latest 是模拟签发前后两个网络响应的独立可控 Promise。
    const old = deferredList();
    // latest 是新发起的列表请求，提交结果应优先于旧请求。
    const latest = deferredList();
    listMock.mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise);
    // hook 是正在处理首次自动读取的令牌列表状态。
    const hook = renderHook(renderTokenListHook);
    await waitFor(/* initialCallReady 确认首次请求已触发。 */ () => expect(listMock).toHaveBeenCalledTimes(1));
    // 新一次刷新模拟令牌签发或撤销后的列表回读。
    act(/* refreshAction 生成一个覆盖旧请求的新读取代次。 */ () => { void hook.result.current.loadTokens(); });
    await waitFor(/* latestCallReady 确认第二次请求已触发。 */ () => expect(listMock).toHaveBeenCalledTimes(2));
    await act(/* resolveLatest 先返回最新列表。 */ async () => {
      latest.resolve({ success: true, data: [tokenSummary('new-token')] });
      await latest.promise;
    });
    expect(hook.result.current.tokens.map(/* tokenId 映射当前列表的记录标识。 */ token => token.id)).toEqual(['new-token']);
    expect(hook.result.current.loading).toBe(false);
    await act(/* resolveOld 再返回已过时的旧列表。 */ async () => {
      old.resolve({ success: true, data: [tokenSummary('old-token')] });
      await old.promise;
    });
    expect(hook.result.current.tokens.map(/* tokenId 映射旧请求完成后的记录标识。 */ token => token.id)).toEqual(['new-token']);
    expect(hook.result.current.loadError).toBe('');
  });

  test('旧请求失败不能覆盖后续成功结果与加载状态', /* staleFailureScenario 确认过期错误不覆盖成功列表。 */ async () => {
    // old、latest 分别模拟先请求和后请求。
    const old = deferredList();
    // latest 是新发起的列表请求，提交结果应优先于旧请求。
    const latest = deferredList();
    listMock.mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise);
    // hook 保存当前最新列表和错误提示。
    const hook = renderHook(renderTokenListHook);
    await waitFor(/* initialReady 读取首次请求。 */ () => expect(listMock).toHaveBeenCalledTimes(1));
    act(/* refreshAction 在首次请求未完成时启动第二代请求。 */ () => { void hook.result.current.loadTokens(); });
    await act(/* resolveLatest 使新请求先成功。 */ async () => {
      latest.resolve({ success: true, data: [tokenSummary('active')] });
      await latest.promise;
    });
    await act(/* rejectOld 使旧请求随后失败。 */ async () => {
      old.reject(new Error('obsolete network error'));
      try { await old.promise; } catch { /* 测试已显式处理预期的旧请求失败。 */ }
    });
    expect(hook.result.current.tokens[0]?.id).toBe('active');
    expect(hook.result.current.loadError).toBe('');
    expect(hook.result.current.loading).toBe(false);
  });

  test('最新请求失败可重试，卸载后迟到响应不能再次提交', /* retryAndUnmountScenario 校验失败提示与生命周期隔离。 */ async () => {
    // expired、recovered 分别模拟失败列表请求和下一次成功请求。
    const expired = deferredList();
    // recovered 是失败重试时仍未返回的下一代请求。
    const recovered = deferredList();
    listMock.mockReturnValueOnce(expired.promise).mockReturnValueOnce(recovered.promise);
    // hook 接收挂载后自动发起的首次请求。
    const hook = renderHook(renderTokenListHook);
    await act(/* rejectCurrent 使当前请求失败。 */ async () => {
      expired.reject(new Error('temporary outage'));
      try { await expired.promise; } catch { /* 预期读取错误由 Hook 显示。 */ }
    });
    expect(hook.result.current.loadError).toContain('读取 API 访问令牌失败');
    act(/* retryAction 发起一代新请求并清除旧错误提示。 */ () => { void hook.result.current.loadTokens(); });
    expect(hook.result.current.loadError).toBe('');
    hook.unmount();
    await act(/* resolveAfterUnmount 页面卸载后才返回的网络请求不得写入 React 状态。 */ async () => {
      recovered.resolve({ success: true, data: [tokenSummary('after-unmount')] });
      await recovered.promise;
    });
  });
});
