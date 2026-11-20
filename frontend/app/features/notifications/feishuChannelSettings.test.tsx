// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { NotificationChannel } from './api';
import { getFeishuChannelCategories,updateFeishuChannelCategories } from './api';
import { useFeishuChannelSettings } from './feishuChannelSettings';

vi.mock('./api', /* feishuChannelSettingsApiMockFactory 提供分类设置 Hook 的确定性 API 替身。 */ () => ({
  getFeishuChannelCategories: vi.fn(),
  updateFeishuChannelCategories: vi.fn(),
}));

// getCategoriesMock 是分类设置读取请求的可控替身。
const getCategoriesMock = vi.mocked(getFeishuChannelCategories);
// updateCategoriesMock 是分类设置保存请求的可控替身。
const updateCategoriesMock = vi.mocked(updateFeishuChannelCategories);

// feishuChannel 是分类设置弹窗操作的飞书应用渠道摘要。
const feishuChannel: NotificationChannel = { id: '7', name: '飞书扫码渠道', type: 'feishu_app', config: {}, enabled: true, event_types: [] };

/** CategoryFixture 是分类设置读取响应的最小测试模型。 */
interface CategoryFixture {
  /** warning 是警告类通知开关。 */
  warning: boolean;
  /** info 是普通提示类通知开关。 */
  info: boolean;
}

/** OperationFixture 是分类设置保存成功响应的最小测试模型。 */
interface OperationFixture {
  /** success 表示保存操作是否完成。 */
  success: boolean;
}

// Deferred 描述测试可以手动完成的异步响应。
interface Deferred<T> {
  // promise 是等待测试手动完成的 Promise。
  promise: Promise<T>;
  // resolve 以成功值完成 Promise。
  resolve: (value: T) => void;
  // reject 以错误完成 Promise。
  reject: (reason: Error) => void;
}

// createDeferred 创建可由测试控制完成时机的异步响应。
const createDeferred = <T,>(): Deferred<T> => {
  // resolveFn 保存成功完成函数，由 Promise 构造器写入。
  let resolveFn!: (value: T) => void;
  // rejectFn 保存失败完成函数，由 Promise 构造器写入。
  let rejectFn!: (reason: Error) => void;
  // promise 是测试待完成的异步响应。
  const promise = new Promise<T>(
    // executor 保存 Promise 的成功与失败完成入口。
    (resolve, reject) => {
      resolveFn = resolve;
      rejectFn = reject;
    },
  );
  return { promise, resolve: resolveFn, reject: rejectFn };
};

describe('useFeishuChannelSettings', /* 当前测试组验证分类设置的加载、保存、取消和旧响应隔离。 */ () => {
  beforeEach(/* 当前回调重置分类设置 API 替身。 */ () => {
    vi.resetAllMocks();
  });

  afterEach(/* 当前回调清理尚未完成的异步任务标记。 */ () => {
    vi.clearAllMocks();
  });

  test('打开渠道后加载分类开关并更新草稿', /* 当前回调验证读取成功后的弹窗状态与开关值。 */ async () => {
    getCategoriesMock.mockResolvedValue({ warning: false, info: true });
    // hook 是分类设置加载成功场景的 Hook 渲染结果。
    const hook = renderHook(/* settingsHookFactory 创建飞书渠道设置状态。 */ () => useFeishuChannelSettings({ onSaved: vi.fn() }));
    await act(/* openAction 打开并加载飞书渠道设置。 */ async () => { await hook.result.current.openSettings(feishuChannel); });
    expect(getCategoriesMock).toHaveBeenCalledWith('7', expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.open).toBe(true);
    expect(hook.result.current.channelID).toBe('7');
    expect(hook.result.current.channelName).toBe('飞书扫码渠道');
    expect(hook.result.current.loading).toBe(false);
    expect(hook.result.current.loadFailed).toBe(false);
    expect(hook.result.current.warning).toBe(false);
    expect(hook.result.current.info).toBe(true);
    act(/* warningAction 修改警告类通知草稿。 */ () => hook.result.current.setWarning(true));
    expect(hook.result.current.warning).toBe(true);
    hook.unmount();
  });

  test('读取失败展示可重试错误并允许重新加载', /* 当前回调验证 loadFailed 与 reload 恢复路径。 */ async () => {
    getCategoriesMock.mockRejectedValueOnce(new Error('读取设置失败')).mockResolvedValueOnce({ warning: true, info: false });
    // hook 是读取失败场景的 Hook 渲染结果。
    const hook = renderHook(/* failureHookFactory 创建分类设置失败状态。 */ () => useFeishuChannelSettings({ onSaved: vi.fn() }));
    await act(/* openAction 打开并读取分类设置。 */ async () => { await hook.result.current.openSettings(feishuChannel); });
    expect(hook.result.current.open).toBe(true);
    expect(hook.result.current.loading).toBe(false);
    expect(hook.result.current.loadFailed).toBe(true);
    expect(hook.result.current.error).toBe('读取设置失败');
    await act(/* retryAction 重新读取分类设置。 */ async () => { await hook.result.current.reload(); });
    expect(hook.result.current.loadFailed).toBe(false);
    expect(hook.result.current.error).toBe('');
    expect(hook.result.current.warning).toBe(true);
    expect(hook.result.current.info).toBe(false);
    hook.unmount();
  });

  test('保存成功关闭弹窗并回调渠道标识', /* 当前回调验证 PUT 请求体和成功后的页面刷新回调。 */ async () => {
    getCategoriesMock.mockResolvedValue({ warning: false, info: true });
    updateCategoriesMock.mockResolvedValue({ success: true });
    // onSaved 保存保存成功后的页面回调替身。
    const onSaved = vi.fn();
    // hook 是保存成功场景的 Hook 渲染结果。
    const hook = renderHook(/* saveHookFactory 创建飞书渠道设置状态。 */ () => useFeishuChannelSettings({ onSaved }));
    await act(/* openAction 打开并读取分类设置。 */ async () => { await hook.result.current.openSettings(feishuChannel); });
    act(/* warningAction 把警告类通知切换到开启。 */ () => hook.result.current.setWarning(true));
    await act(/* saveAction 保存分类开关。 */ async () => { await hook.result.current.save(); });
    expect(updateCategoriesMock).toHaveBeenCalledWith('7', { warning: true, info: true }, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.open).toBe(false);
    expect(hook.result.current.saving).toBe(false);
    expect(onSaved).toHaveBeenCalledWith('7');
    hook.unmount();
  });

  test('保存失败保留弹窗草稿并展示错误', /* 当前回调验证失败后用户可以再次修改或重试。 */ async () => {
    getCategoriesMock.mockResolvedValue({ warning: true, info: true });
    updateCategoriesMock.mockRejectedValueOnce(new Error('保存设置失败'));
    // hook 是保存失败场景的 Hook 渲染结果。
    const hook = renderHook(/* saveFailureHookFactory 创建分类设置保存失败状态。 */ () => useFeishuChannelSettings({ onSaved: vi.fn() }));
    await act(/* openAction 打开并读取分类设置。 */ async () => { await hook.result.current.openSettings(feishuChannel); });
    await act(/* saveAction 提交会失败的分类设置。 */ async () => { await hook.result.current.save(); });
    expect(hook.result.current.open).toBe(true);
    expect(hook.result.current.saving).toBe(false);
    expect(hook.result.current.error).toBe('保存设置失败');
    expect(hook.result.current.warning).toBe(true);
    expect(hook.result.current.info).toBe(true);
    hook.unmount();
  });

  test('关闭弹窗取消读取请求并拒绝晚到响应', /* 当前回调验证加载期间关闭后不会重新打开或写入开关。 */ async () => {
    // readDeferred 是保持未完成的分类设置读取响应。
    const readDeferred = createDeferred<CategoryFixture>();
    // readSignal 保存读取请求使用的取消信号。
    let readSignal: AbortSignal | undefined;
    getCategoriesMock.mockImplementation(/* pendingReadFactory 保存取消信号并返回未完成响应。 */ (_channelID, options) => {
      readSignal = options?.signal;
      return readDeferred.promise;
    });
    // hook 是读取取消场景的 Hook 渲染结果。
    const hook = renderHook(/* closeReadHookFactory 创建可关闭的分类设置状态。 */ () => useFeishuChannelSettings({ onSaved: vi.fn() }));
    // pendingOpen 是尚未完成的打开设置动作。
    let pendingOpen!: Promise<void>;
    act(/* beginOpenAction 发起分类设置读取。 */ () => { pendingOpen = hook.result.current.openSettings(feishuChannel); });
    expect(hook.result.current.open).toBe(true);
    expect(hook.result.current.loading).toBe(true);
    act(/* closeAction 关闭弹窗并取消读取。 */ () => hook.result.current.close());
    expect(readSignal?.aborted).toBe(true);
    expect(hook.result.current.open).toBe(false);
    expect(hook.result.current.loading).toBe(false);
    readDeferred.resolve({ warning: false, info: false });
    await act(/* lateReadAction 等待已取消响应到达。 */ async () => { await pendingOpen; });
    expect(hook.result.current.open).toBe(false);
    expect(hook.result.current.warning).toBe(true);
    hook.unmount();
  });

  test('关闭弹窗取消保存请求且不触发保存回调', /* 当前回调验证保存期间关闭后旧成功响应不会通知页面。 */ async () => {
    getCategoriesMock.mockResolvedValue({ warning: false, info: false });
    // saveDeferred 是保持未完成的分类设置保存响应。
    const saveDeferred = createDeferred<OperationFixture>();
    // saveSignal 保存保存请求使用的取消信号。
    let saveSignal: AbortSignal | undefined;
    updateCategoriesMock.mockImplementation(/* pendingSaveFactory 保存取消信号并返回未完成响应。 */ (_channelID, _categories, options) => {
      saveSignal = options?.signal;
      return saveDeferred.promise;
    });
    // onSaved 保存成功回调替身，用于确认取消保存后不会触发页面刷新。
    const onSaved = vi.fn();
    // hook 是保存取消场景的 Hook 渲染结果。
    const hook = renderHook(/* closeSaveHookFactory 创建可关闭的分类设置状态。 */ () => useFeishuChannelSettings({ onSaved }));
    await act(/* openAction 打开并读取分类设置。 */ async () => { await hook.result.current.openSettings(feishuChannel); });
    // pendingSave 是尚未完成的保存动作。
    let pendingSave!: Promise<void>;
    act(/* beginSaveAction 发起分类设置保存。 */ () => { pendingSave = hook.result.current.save(); });
    expect(hook.result.current.saving).toBe(true);
    act(/* closeAction 关闭弹窗并取消保存。 */ () => hook.result.current.close());
    expect(saveSignal?.aborted).toBe(true);
    expect(hook.result.current.open).toBe(false);
    saveDeferred.resolve({ success: true });
    await act(/* lateSaveAction 等待已取消响应到达。 */ async () => { await pendingSave; });
    expect(onSaved).not.toHaveBeenCalled();
    expect(hook.result.current.open).toBe(false);
    hook.unmount();
  });

  test('连续打开不同渠道时丢弃先发出的旧响应', /* 当前回调验证渠道切换代次隔离。 */ async () => {
    // firstRead 是首个渠道的未完成读取响应。
    const firstRead = createDeferred<CategoryFixture>();
    // secondRead 是当前渠道的成功读取响应。
    const secondRead = createDeferred<CategoryFixture>();
    // firstSignal 保存首个渠道读取的取消信号。
    let firstSignal: AbortSignal | undefined;
    getCategoriesMock
      .mockImplementationOnce(/* firstReadFactory 保存首个渠道取消信号。 */ (_channelID, options) => {
        firstSignal = options?.signal;
        return firstRead.promise;
      })
      .mockImplementationOnce(/* secondReadFactory 返回当前渠道待完成响应。 */ () => secondRead.promise);
    // hook 是连续打开场景的 Hook 渲染结果。
    const hook = renderHook(/* staleHookFactory 创建渠道切换状态。 */ () => useFeishuChannelSettings({ onSaved: vi.fn() }));
    // firstOpen 是首个渠道的设置读取。
    let firstOpen!: Promise<void>;
    // secondOpen 是替代首个渠道的第二个设置读取。
    let secondOpen!: Promise<void>;
    act(/* openActions 连续打开两个飞书渠道。 */ () => {
      firstOpen = hook.result.current.openSettings(feishuChannel);
      secondOpen = hook.result.current.openSettings({ ...feishuChannel, id: '8', name: '第二个飞书渠道' });
    });
    expect(firstSignal?.aborted).toBe(true);
    secondRead.resolve({ warning: true, info: false });
    await act(/* secondResolveAction 完成最新渠道读取。 */ async () => { await secondOpen; });
    expect(hook.result.current.channelID).toBe('8');
    expect(hook.result.current.channelName).toBe('第二个飞书渠道');
    expect(hook.result.current.warning).toBe(true);
    expect(hook.result.current.info).toBe(false);

    firstRead.resolve({ warning: false, info: true });
    await act(/* firstResolveAction 完成已过期的首个读取。 */ async () => { await firstOpen; });
    expect(hook.result.current.channelID).toBe('8');
    expect(hook.result.current.warning).toBe(true);
    expect(hook.result.current.info).toBe(false);
    hook.unmount();
  });
});
