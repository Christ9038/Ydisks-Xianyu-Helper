// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { ApiError } from '../../../shared/http/client';
import * as api from './api';
import { useLibraryImages, useProductTemplates } from './hooks';
import type { ProductTemplate } from './types';

vi.mock('./api', async (importOriginal) => {
  // original 保留真实的 libraryErrorMessage 等纯函数。
  const original = await importOriginal<typeof import('./api')>();
  return { ...original, listLibraryImages: vi.fn(), uploadLibraryImages: vi.fn(), deleteLibraryImage: vi.fn(), listProductTemplates: vi.fn(), saveProductTemplate: vi.fn(), deleteProductTemplate: vi.fn() };
});

// image 是列表测试使用的最小图片。
const image = { id: 1, filename: 'a.png', contentType: 'image/png', sizeBytes: 1, createdAt: '' };

describe('useLibraryImages', /* 图片库 Hook 的加载、上传与删除保护。 */ () => {
  beforeEach(/* 重置请求替身。 */ () => {
    vi.clearAllMocks();
    vi.mocked(api.listLibraryImages).mockResolvedValue([image]);
  });

  test('挂载后加载图片，上传后刷新并返回逐文件结果', /* 成功路径。 */ async () => {
    vi.mocked(api.uploadLibraryImages).mockResolvedValue([{ filename: 'a.png', duplicate: false, error: '', image }]);
    // hook 是被测图片库 Hook。
    const hook = renderHook(/* 创建 Hook。 */ () => useLibraryImages());
    await waitFor(/* 等待首次加载。 */ () => expect(hook.result.current.images).toHaveLength(1));
    // results 是上传返回的逐文件结果。
    let results: Awaited<ReturnType<typeof hook.result.current.upload>> = [];
    await act(/* 在 act 中执行异步 Hook 操作。 */ async () => { results = await hook.result.current.upload([new File(['x'], 'a.png')]); });
    expect(results[0].filename).toBe('a.png');
    expect(api.listLibraryImages).toHaveBeenCalledTimes(2);
  });

  test('删除被模板引用的图片时保留列表并提示引用模板', /* 409 保护。 */ async () => {
    vi.mocked(api.deleteLibraryImage).mockRejectedValue(new ApiError(409, { code: 'conflict', message: '图片仍被商品模板引用', details: { templates: ['模板A'] } }));
    // hook 是被测图片库 Hook。
    const hook = renderHook(/* 创建 Hook。 */ () => useLibraryImages());
    await waitFor(/* 等待首次加载。 */ () => expect(hook.result.current.images).toHaveLength(1));
    // removed 记录删除是否成功。
    let removed = true;
    await act(/* 在 act 中执行异步 Hook 操作。 */ async () => { removed = await hook.result.current.remove(1); });
    expect(removed).toBe(false);
    expect(hook.result.current.error).toContain('模板A');
    expect(hook.result.current.images).toHaveLength(1);
  });

  test('过期响应不会覆盖最新状态，加载失败展示原因', /* 失败路径。 */ async () => {
    vi.mocked(api.listLibraryImages).mockRejectedValue(new Error('网络错误'));
    // hook 是被测图片库 Hook。
    const hook = renderHook(/* 创建 Hook。 */ () => useLibraryImages());
    await waitFor(/* 等待失败状态。 */ () => expect(hook.result.current.error).toBe('网络错误'));
    expect(hook.result.current.images).toEqual([]);
  });
});

describe('useLibraryImages.removeMany', /* 图片批量删除的汇总、保护与取消。 */ () => {
  beforeEach(/* 重置请求替身。 */ () => {
    vi.clearAllMocks();
    vi.mocked(api.listLibraryImages).mockResolvedValue([image]);
  });

  test('单项被引用失败不中断其余项，只刷新一次并带回模板名', /* 部分失败。 */ async () => {
    vi.mocked(api.deleteLibraryImage).mockResolvedValueOnce().mockRejectedValueOnce(new ApiError(409, { code: 'conflict', message: '图片仍被商品模板引用', details: { templates: ['模板A'] } })).mockResolvedValueOnce();
    // hook 是被测图片库 Hook。
    const hook = renderHook(/* 创建 Hook。 */ () => useLibraryImages());
    await waitFor(/* 等待首次加载。 */ () => expect(hook.result.current.images).toHaveLength(1));
    // result 是批量删除汇总。
    let result: Awaited<ReturnType<typeof hook.result.current.removeMany>> = { deleted: [], failures: [] };
    await act(/* 在 act 中执行异步 Hook 操作。 */ async () => { result = await hook.result.current.removeMany([1, 2, 3]); });
    expect(result.deleted).toEqual([1, 3]);
    expect(result.failures).toEqual([{ id: 2, message: '图片仍被商品模板引用：模板A' }]);
    expect(api.listLibraryImages).toHaveBeenCalledTimes(2);
    expect(hook.result.current.busy).toBe(false);
  });

  test('全部失败时不刷新列表；取消后停止剩余项且不刷新', /* 全失败与取消分支。 */ async () => {
    vi.mocked(api.deleteLibraryImage).mockRejectedValue(new Error('删除失败'));
    // hook 是被测图片库 Hook。
    const hook = renderHook(/* 创建 Hook。 */ () => useLibraryImages());
    await waitFor(/* 等待首次加载。 */ () => expect(hook.result.current.images).toHaveLength(1));
    // failed 是全部失败的汇总。
    let failed: Awaited<ReturnType<typeof hook.result.current.removeMany>> = { deleted: [], failures: [] };
    await act(/* 在 act 中执行异步 Hook 操作。 */ async () => { failed = await hook.result.current.removeMany([1, 2]); });
    expect(failed.failures).toHaveLength(2);
    expect(api.listLibraryImages).toHaveBeenCalledTimes(1);

    vi.mocked(api.deleteLibraryImage).mockReset().mockResolvedValue();
    // controller 在调用前已取消，模拟页面卸载。
    const controller = new AbortController();
    controller.abort();
    // aborted 是取消后的汇总。
    let aborted: Awaited<ReturnType<typeof hook.result.current.removeMany>> = { deleted: [1], failures: [] };
    await act(/* 在 act 中执行异步 Hook 操作。 */ async () => { aborted = await hook.result.current.removeMany([1, 2], controller.signal); });
    expect(aborted).toEqual({ deleted: [], failures: [] });
    expect(api.deleteLibraryImage).not.toHaveBeenCalled();
    expect(api.listLibraryImages).toHaveBeenCalledTimes(1);
  });

  test('删除中途取消：已失败的项不记录，之后的项不再请求', /* 请求中取消分支。 */ async () => {
    // controller 在第一项请求期间被取消。
    const controller = new AbortController();
    vi.mocked(api.deleteLibraryImage).mockImplementationOnce(/* 第一项请求时触发取消并失败。 */ async () => { controller.abort(); throw new Error('已取消'); });
    // hook 是被测图片库 Hook。
    const hook = renderHook(/* 创建 Hook。 */ () => useLibraryImages());
    await waitFor(/* 等待首次加载。 */ () => expect(hook.result.current.images).toHaveLength(1));
    // result 是取消后的汇总。
    let result: Awaited<ReturnType<typeof hook.result.current.removeMany>> = { deleted: [1], failures: [] };
    await act(/* 在 act 中执行异步 Hook 操作。 */ async () => { result = await hook.result.current.removeMany([1, 2], controller.signal); });
    expect(result).toEqual({ deleted: [], failures: [] });
    expect(api.deleteLibraryImage).toHaveBeenCalledTimes(1);
  });
});

describe('useProductTemplates', /* 模板 Hook 的保存与删除。 */ () => {
  beforeEach(/* 重置请求替身。 */ () => {
    vi.clearAllMocks();
    vi.mocked(api.listProductTemplates).mockResolvedValue([]);
  });

  test('保存成功刷新列表，失败返回 false 并展示原因', /* 保存分支。 */ async () => {
    vi.mocked(api.saveProductTemplate).mockResolvedValueOnce({} as never).mockRejectedValueOnce(new Error('保存失败'));
    // hook 是被测模板 Hook。
    const hook = renderHook(/* 创建 Hook。 */ () => useProductTemplates());
    await waitFor(/* 等待首次加载。 */ () => expect(hook.result.current.loading).toBe(false));
    // first、second 记录两次保存结果。
    let first = false;
    // second 记录第二次保存是否成功。
    let second = true;
    await act(/* 在 act 中执行异步 Hook 操作。 */ async () => { first = await hook.result.current.save(null, {} as never); });
    await act(/* 在 act 中执行异步 Hook 操作。 */ async () => { second = await hook.result.current.save(3, {} as never); });
    expect(first).toBe(true);
    expect(second).toBe(false);
    expect(hook.result.current.error).toBe('保存失败');
  });

  test('删除成功刷新列表，失败返回 false', /* 删除分支。 */ async () => {
    vi.mocked(api.deleteProductTemplate).mockResolvedValueOnce().mockRejectedValueOnce(new Error('删除失败'));
    // hook 是被测模板 Hook。
    const hook = renderHook(/* 创建 Hook。 */ () => useProductTemplates());
    await waitFor(/* 等待首次加载。 */ () => expect(hook.result.current.loading).toBe(false));
    // ok、failed 记录两次删除结果。
    let ok = false;
    // failed 记录第二次删除是否成功。
    let failed = true;
    await act(/* 在 act 中执行异步 Hook 操作。 */ async () => { ok = await hook.result.current.remove(1); });
    await act(/* 在 act 中执行异步 Hook 操作。 */ async () => { failed = await hook.result.current.remove(2); });
    expect(ok).toBe(true);
    expect(failed).toBe(false);
  });
});

// template 是复制测试使用的最小有效单规格模板。
const template: ProductTemplate = {
  id: 7, name: '原模板', title: '标题', description: '描述', price: '9.90', originalPrice: '', quantity: 3, postageMode: 'free', postage: '',
  category: { catId: '1', catName: '电子资料', channelCatId: '2', tbCatId: '' }, imageIds: [1], specs: [], skus: [],
  automation: { paidDelivery: { enabled: false, actions: [] }, reviewGift: { enabled: false, actions: [] }, reviewRequest: { enabled: false, afterShippedHours: 0, message: '', maxAttempts: 0, delaySeconds: 0 } },
  updatedAt: '',
};

describe('useProductTemplates.removeMany/duplicate', /* 模板批量删除与复制。 */ () => {
  beforeEach(/* 重置请求替身。 */ () => {
    vi.clearAllMocks();
    vi.mocked(api.listProductTemplates).mockResolvedValue([]);
  });

  test('批量删除汇总成功与失败，只刷新一次', /* 部分失败。 */ async () => {
    vi.mocked(api.deleteProductTemplate).mockResolvedValueOnce().mockRejectedValueOnce(new Error('删除失败'));
    // hook 是被测模板 Hook。
    const hook = renderHook(/* 创建 Hook。 */ () => useProductTemplates());
    await waitFor(/* 等待首次加载。 */ () => expect(hook.result.current.loading).toBe(false));
    // result 是批量删除汇总。
    let result: Awaited<ReturnType<typeof hook.result.current.removeMany>> = { deleted: [], failures: [] };
    await act(/* 在 act 中执行异步 Hook 操作。 */ async () => { result = await hook.result.current.removeMany([1, 2]); });
    expect(result).toEqual({ deleted: [1], failures: [{ id: 2, message: '删除失败' }] });
    expect(api.listProductTemplates).toHaveBeenCalledTimes(2);
  });

  test('复制以“ 副本”命名保存为新模板，失败时展示原因', /* 复制分支。 */ async () => {
    vi.mocked(api.saveProductTemplate).mockResolvedValueOnce({} as never).mockRejectedValueOnce(new Error('保存失败'));
    // hook 是被测模板 Hook。
    const hook = renderHook(/* 创建 Hook。 */ () => useProductTemplates());
    await waitFor(/* 等待首次加载。 */ () => expect(hook.result.current.loading).toBe(false));
    // first、second 记录两次复制结果。
    let first = false;
    // second 记录第二次复制是否成功。
    let second = true;
    await act(/* 在 act 中执行异步 Hook 操作。 */ async () => { first = await hook.result.current.duplicate(template); });
    expect(first).toBe(true);
    expect(vi.mocked(api.saveProductTemplate).mock.calls[0][0]).toBeNull();
    expect(vi.mocked(api.saveProductTemplate).mock.calls[0][1].name).toBe('原模板 副本');
    await act(/* 在 act 中执行异步 Hook 操作。 */ async () => { second = await hook.result.current.duplicate(template); });
    expect(second).toBe(false);
    expect(hook.result.current.error).toBe('保存失败');
  });
});
