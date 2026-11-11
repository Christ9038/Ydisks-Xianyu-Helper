// @vitest-environment jsdom
// 本测试需要 File、Blob 与 React Hook 渲染环境，因此使用 jsdom。
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { ApiError } from '../../../shared/http/client';
import * as api from './api';
import { downloadImportTemplate, importSheetHeaders, useTemplateImport } from './templateImport';
import type { LibraryImportPreview, LibraryImportProgress } from './types';

vi.mock('./api', async (importOriginal) => {
  // original 保留真实的 libraryErrorMessage 等纯函数。
  const original = await importOriginal<typeof import('./api')>();
  return { ...original, previewTemplateImport: vi.fn(), commitTemplateImport: vi.fn(), discardTemplateImport: vi.fn() };
});

// sheet、zip 是测试使用的文件。
const sheet = new File(['x'], 't.csv');
// zip 是可选图片压缩包。
const zip = new File(['z'], 'i.zip');

/** previewOf 构造预检结果；importId 为空表示没有可导入的行。 */
const previewOf = (importId: string, valid = 2): LibraryImportPreview => ({ importId, total: valid + 1, valid, invalid: 1, remoteImages: 0, rows: [] });

/** progressOf 构造导入进度。 */
const progressOf = (done: boolean, ids: number[], pending: number): LibraryImportProgress => ({ importId: 'import_1', done, created: ids.length, failed: 0, pending, invalid: 1, createdTemplateIds: ids, rows: [] });

/** deferred 创建可由测试控制完成时机的 Promise。 */
const deferred = <T,>() => {
  // resolve 是完成函数。
  let resolve!: (value: T) => void;
  // promise 是待完成的 Promise。
  const promise = new Promise<T>(/* executor 保存完成函数。 */ settle => { resolve = settle; });
  return { promise, resolve };
};

// Excel 导入 Hook：预检、分段循环导入、失败续传、取消、换文件、过期响应与卸载清理。
describe('useTemplateImport', /* 测试组回调覆盖导入流程状态机。 */ () => {
  beforeEach(/* 重置替身及其排队的一次性返回值，避免用例之间串值。 */ () => {
    vi.resetAllMocks();
    vi.mocked(api.discardTemplateImport).mockResolvedValue(undefined);
  });

  test('预检后循环提交直到完成，并把新模板交给回调', /* 当前回调验证完整成功路径。 */ async () => {
    // onImported 记录导入完成时的模板主键。
    const onImported = vi.fn();
    vi.mocked(api.previewTemplateImport).mockResolvedValue(previewOf('import_1'));
    vi.mocked(api.commitTemplateImport).mockResolvedValueOnce(progressOf(false, [7], 1)).mockResolvedValueOnce(progressOf(true, [7, 8], 0));
    // hook 是被测 Hook。
    const hook = renderHook(/* 渲染导入 Hook。 */ () => useTemplateImport(onImported));
    act(/* 选择文件。 */ () => { hook.result.current.selectSheet(sheet); hook.result.current.selectImagesZip(zip); });
    await act(/* 执行预检。 */ () => hook.result.current.runPreview());
    expect(api.previewTemplateImport).toHaveBeenCalledWith({ sheet, imagesZip: zip }, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.phase).toBe('previewed');
    await act(/* 执行导入。 */ () => hook.result.current.startImport());
    expect(api.commitTemplateImport).toHaveBeenCalledTimes(2);
    expect(hook.result.current.phase).toBe('done');
    expect(hook.result.current.progress?.created).toBe(2);
    expect(onImported).toHaveBeenCalledWith([7, 8]);
    hook.unmount();
    expect(api.discardTemplateImport).not.toHaveBeenCalled();
  });

  test('未选表格时提示，预检失败显示原因，全部无效时不能导入', /* 当前回调验证预检失败与空任务。 */ async () => {
    // hook 是被测 Hook。
    const hook = renderHook(/* 渲染导入 Hook。 */ () => useTemplateImport(vi.fn()));
    await act(/* 未选表格时预检。 */ () => hook.result.current.runPreview());
    expect(hook.result.current.error).toBe('请选择商品表格');
    act(/* 选择表格。 */ () => hook.result.current.selectSheet(sheet));
    vi.mocked(api.previewTemplateImport).mockRejectedValueOnce(new ApiError(400, { code: 'bad_request', message: '表格格式错误' }));
    await act(/* 预检失败。 */ () => hook.result.current.runPreview());
    expect(hook.result.current.error).toBe('表格格式错误');
    expect(hook.result.current.phase).toBe('idle');
    vi.mocked(api.previewTemplateImport).mockResolvedValueOnce(previewOf('', 0));
    await act(/* 全部无效的预检。 */ () => hook.result.current.runPreview());
    await act(/* 无任务时导入应无操作。 */ () => hook.result.current.startImport());
    expect(api.commitTemplateImport).not.toHaveBeenCalled();
    hook.unmount();
  });

  test('提交失败后保留进度并可继续导入', /* 当前回调验证失败续传。 */ async () => {
    // onImported 记录导入完成时的模板主键。
    const onImported = vi.fn();
    vi.mocked(api.previewTemplateImport).mockResolvedValue(previewOf('import_1'));
    vi.mocked(api.commitTemplateImport).mockResolvedValueOnce(progressOf(false, [7], 1)).mockRejectedValueOnce(new ApiError(409, { code: 'conflict', message: '导入任务正在处理中' })).mockResolvedValueOnce(progressOf(true, [7, 8], 0));
    // hook 是被测 Hook。
    const hook = renderHook(/* 渲染导入 Hook。 */ () => useTemplateImport(onImported));
    act(/* 选择表格。 */ () => hook.result.current.selectSheet(sheet));
    await act(/* 预检。 */ () => hook.result.current.runPreview());
    await act(/* 第一次导入在第二段失败。 */ () => hook.result.current.startImport());
    expect(hook.result.current.error).toBe('导入任务正在处理中');
    expect(hook.result.current.phase).toBe('previewed');
    expect(hook.result.current.progress?.created).toBe(1);
    await act(/* 继续导入。 */ () => hook.result.current.startImport());
    expect(hook.result.current.phase).toBe('done');
    expect(onImported).toHaveBeenCalledWith([7, 8]);
  });

  test('导入中取消会停止循环、丢弃任务，并把已创建模板交给回调', /* 当前回调验证取消。 */ async () => {
    // onImported 记录取消时已创建的模板。
    const onImported = vi.fn();
    vi.mocked(api.previewTemplateImport).mockResolvedValue(previewOf('import_1'));
    // second 是第二段提交，由测试控制完成时机。
    const second = deferred<LibraryImportProgress>();
    vi.mocked(api.commitTemplateImport).mockResolvedValueOnce(progressOf(false, [7], 1)).mockReturnValueOnce(second.promise);
    // hook 是被测 Hook。
    const hook = renderHook(/* 渲染导入 Hook。 */ () => useTemplateImport(onImported));
    act(/* 选择表格。 */ () => hook.result.current.selectSheet(sheet));
    await act(/* 预检。 */ () => hook.result.current.runPreview());
    // running 是进行中的导入循环。
    let running: Promise<void> = Promise.resolve();
    act(/* 开始导入。 */ () => { running = hook.result.current.startImport(); });
    await waitFor(/* 等待第一段进度。 */ () => expect(hook.result.current.progress?.created).toBe(1));
    act(/* 取消导入。 */ () => hook.result.current.cancel());
    expect(api.discardTemplateImport).toHaveBeenCalledWith('import_1');
    expect(onImported).toHaveBeenCalledWith([7]);
    expect(hook.result.current.phase).toBe('idle');
    await act(/* 过期的第二段响应到达后被丢弃。 */ async () => { second.resolve(progressOf(true, [7, 8], 0)); await running; });
    expect(hook.result.current.progress).toBeNull();
    expect(onImported).toHaveBeenCalledTimes(1);
  });

  test('换文件或过期预检响应会丢弃旧任务，卸载时丢弃未完成任务', /* 当前回调验证换文件、过期响应与卸载。 */ async () => {
    // first 是第一次预检，由测试控制完成时机。
    const first = deferred<LibraryImportPreview>();
    vi.mocked(api.previewTemplateImport).mockReturnValueOnce(first.promise).mockResolvedValueOnce(previewOf('import_2'));
    vi.mocked(api.discardTemplateImport).mockRejectedValueOnce(new Error('网络错误'));
    // hook 是被测 Hook。
    const hook = renderHook(/* 渲染导入 Hook。 */ () => useTemplateImport(vi.fn()));
    act(/* 选择表格。 */ () => hook.result.current.selectSheet(sheet));
    // pending 是进行中的第一次预检。
    let pending: Promise<void> = Promise.resolve();
    act(/* 开始第一次预检。 */ () => { pending = hook.result.current.runPreview(); });
    act(/* 预检进行中更换压缩包。 */ () => hook.result.current.selectImagesZip(zip));
    await act(/* 第一次预检在作废后返回。 */ async () => { first.resolve(previewOf('import_1')); await pending; });
    expect(hook.result.current.preview).toBeNull();
    expect(api.discardTemplateImport).toHaveBeenCalledWith('import_1');
    await act(/* 重新预检。 */ () => hook.result.current.runPreview());
    expect(hook.result.current.preview?.importId).toBe('import_2');
    act(/* 换表格作废任务。 */ () => hook.result.current.selectSheet(new File(['y'], 'u.csv')));
    expect(api.discardTemplateImport).toHaveBeenCalledWith('import_2');
    expect(hook.result.current.phase).toBe('idle');
    vi.mocked(api.previewTemplateImport).mockResolvedValueOnce(previewOf('import_3'));
    await act(/* 再次预检。 */ () => hook.result.current.runPreview());
    hook.unmount();
    expect(api.discardTemplateImport).toHaveBeenCalledWith('import_3');
  });

  test('请求被取消时不显示错误', /* 当前回调验证取消错误被忽略。 */ async () => {
    vi.mocked(api.previewTemplateImport).mockRejectedValueOnce(Object.assign(new Error('请求已取消'), { name: 'AbortError' }));
    // hook 是被测 Hook。
    const hook = renderHook(/* 渲染导入 Hook。 */ () => useTemplateImport(vi.fn()));
    act(/* 选择表格。 */ () => hook.result.current.selectSheet(sheet));
    await act(/* 预检被取消。 */ () => hook.result.current.runPreview());
    expect(hook.result.current.error).toBe('');
    expect(hook.result.current.phase).toBe('idle');
    vi.mocked(api.previewTemplateImport).mockResolvedValueOnce(previewOf('import_1'));
    vi.mocked(api.commitTemplateImport).mockRejectedValueOnce(new Error('请求已取消'));
    await act(/* 重新预检。 */ () => hook.result.current.runPreview());
    await act(/* 提交被取消。 */ () => hook.result.current.startImport());
    expect(hook.result.current.error).toBe('');
    expect(hook.result.current.phase).toBe('previewed');
  });

  test('作废后到达的空任务、失败响应被丢弃，丢弃失败不影响界面，未导入时取消不触发回调', /* 当前回调验证过期响应的其余分支。 */ async () => {
    // onImported 记录取消时的回调。
    const onImported = vi.fn();
    // stalePreview、staleFailure 是作废后才返回的预检请求。
    const stalePreview = deferred<LibraryImportPreview>();
    // staleFailure 是作废后才失败的预检请求。
    const staleFailure = deferred<LibraryImportPreview>();
    vi.mocked(api.previewTemplateImport).mockReturnValueOnce(stalePreview.promise).mockReturnValueOnce(staleFailure.promise.then(/* 转为失败。 */ () => { throw new Error('网络错误'); })).mockResolvedValueOnce(previewOf('import_9'));
    vi.mocked(api.discardTemplateImport).mockRejectedValue(new Error('网络错误'));
    // hook 是被测 Hook。
    const hook = renderHook(/* 渲染导入 Hook。 */ () => useTemplateImport(onImported));
    act(/* 选择表格。 */ () => hook.result.current.selectSheet(sheet));
    // first、second 是两次被作废的预检。
    let first: Promise<void> = Promise.resolve();
    act(/* 开始第一次预检。 */ () => { first = hook.result.current.runPreview(); });
    act(/* 作废第一次预检。 */ () => hook.result.current.selectImagesZip(zip));
    await act(/* 作废后返回没有任务的预检结果。 */ async () => { stalePreview.resolve(previewOf('', 0)); await first; });
    // second 是第二次被作废的预检。
    let second: Promise<void> = Promise.resolve();
    act(/* 开始第二次预检。 */ () => { second = hook.result.current.runPreview(); });
    act(/* 作废第二次预检。 */ () => hook.result.current.selectImagesZip(null));
    await act(/* 作废后预检失败。 */ async () => { staleFailure.resolve(previewOf('')); await second.catch(/* 失败已在 Hook 内处理。 */ () => undefined); });
    expect(hook.result.current.error).toBe('');
    expect(api.discardTemplateImport).not.toHaveBeenCalled();
    await act(/* 第三次预检成功。 */ () => hook.result.current.runPreview());
    // commit 是作废后才失败的提交。
    const commit = deferred<LibraryImportProgress>();
    vi.mocked(api.commitTemplateImport).mockReturnValueOnce(commit.promise.then(/* 转为失败。 */ () => { throw new Error('网络错误'); }));
    // running 是进行中的导入。
    let running: Promise<void> = Promise.resolve();
    act(/* 开始导入。 */ () => { running = hook.result.current.startImport(); });
    act(/* 未产生进度时取消，不触发回调。 */ () => hook.result.current.cancel());
    await act(/* 作废后提交失败。 */ async () => { commit.resolve(progressOf(true, [], 0)); await running; });
    expect(hook.result.current.error).toBe('');
    expect(onImported).not.toHaveBeenCalled();
    expect(api.discardTemplateImport).toHaveBeenCalledWith('import_9');
  });

  test('下载导入模板：没有账号列、首列为模板名称', /* 当前回调验证导入模板内容。 */ async () => {
    // createObjectURL 记录生成的文件。
    const createObjectURL = vi.fn(/* fakeUrl 返回固定临时地址。 */ (_blob: Blob) => 'blob:x');
    URL.createObjectURL = createObjectURL;
    URL.revokeObjectURL = vi.fn();
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(/* 屏蔽真实下载。 */ () => undefined);
    downloadImportTemplate();
    // lines 是下载模板的逐行文本。
    const lines = (await createObjectURL.mock.calls[0][0].text()).split('\n');
    expect(importSheetHeaders[0]).toBe('模板名称');
    expect(importSheetHeaders).not.toContain('账号ID');
    expect(lines[0].startsWith('"模板名称","标题"')).toBe(true);
    expect(lines[1]).toContain('"T1"');
  });
});
