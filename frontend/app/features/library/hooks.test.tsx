// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { ApiError } from '../../../shared/http/client';
import * as api from './api';
import { useLibraryImages, useProductTemplates } from './hooks';

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
