// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { FeishuAppBindingStartResult } from './models';
import { getFeishuAppBindingStatus,startFeishuAppBinding } from './api';
import { useFeishuAppBinding } from './feishuBinding';

vi.mock('./api', /* feishuBindingApiMockFactory 提供扫码绑定 Hook 的确定性 API 替身。 */ () => ({
  startFeishuAppBinding: vi.fn(),
  getFeishuAppBindingStatus: vi.fn(),
}));

// startBindingMock 是开始扫码绑定请求的可控替身。
const startBindingMock = vi.mocked(startFeishuAppBinding);
// statusBindingMock 是扫码状态轮询请求的可控替身。
const statusBindingMock = vi.mocked(getFeishuAppBindingStatus);

// bindingStartResult 是后端返回的扫码会话样例。
const bindingStartResult: FeishuAppBindingStartResult = { session_id: 'session-1', qr_code_url: 'data:image/png;base64,qr', expires_in: 300 };

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

describe('useFeishuAppBinding', /* 当前测试组验证扫码绑定的轮询、终态和取消边界。 */ () => {
  beforeEach(/* 当前回调启用假定时器并重置 API 替身。 */ () => {
    vi.useFakeTimers();
    vi.resetAllMocks();
  });

  afterEach(/* 当前回调恢复真实定时器，避免影响其他测试文件。 */ () => {
    vi.useRealTimers();
  });

  test('开始绑定后进入等待并按两秒轮询到成功', /* 当前回调验证二维码展示、轮询间隔和成功回调参数。 */ async () => {
    startBindingMock.mockResolvedValue(bindingStartResult);
    statusBindingMock
      .mockResolvedValueOnce({ status: 'waiting', message: '等待用户扫码' })
      .mockResolvedValueOnce({ status: 'success', channel_id: 42, message: '渠道已创建' });
    // onSuccess 保存绑定成功后的页面回调替身。
    const onSuccess = vi.fn();
    // hook 是飞书扫码绑定 Hook 的渲染结果。
    const hook = renderHook(/* bindingHookFactory 创建扫码绑定状态。 */ () => useFeishuAppBinding({ onSuccess }));
    act(/* openAction 打开扫码绑定弹窗。 */ () => hook.result.current.open());
    expect(hook.result.current.status).toBe('starting');
    expect(hook.result.current.bindAllAccounts).toBe(true);
    expect(hook.result.current.notifyWarning).toBe(true);
    expect(hook.result.current.notifyInfo).toBe(true);
    await act(/* startAction 创建扫码会话。 */ async () => { await hook.result.current.start(); });
    expect(startBindingMock).toHaveBeenCalledWith({ bindAllAccounts: true, notifyWarning: true, notifyInfo: true }, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.status).toBe('waiting');
    expect(hook.result.current.qrCodeUrl).toBe(bindingStartResult.qr_code_url);
    expect(hook.result.current.sessionID).toBe('session-1');
    expect(hook.result.current.bindAllAccounts).toBe(true);
    expect(statusBindingMock).not.toHaveBeenCalled();

    await act(/* firstPollAction 推进首个两秒轮询。 */ async () => { await vi.advanceTimersByTimeAsync(2_000); });
    expect(statusBindingMock).toHaveBeenCalledTimes(1);
    expect(hook.result.current.status).toBe('waiting');
    expect(hook.result.current.message).toBe('等待用户扫码');

    await act(/* secondPollAction 推进下一次轮询并获得成功结果。 */ async () => { await vi.advanceTimersByTimeAsync(2_000); });
    expect(statusBindingMock).toHaveBeenCalledTimes(2);
    expect(hook.result.current.status).toBe('success');
    expect(hook.result.current.message).toBe('渠道已创建');
    expect(onSuccess).toHaveBeenCalledWith(42);
    hook.unmount();
  });

  test('自定义账号与分类开关随开始请求提交', /* 当前回调验证 setter 写入的三个开关都会进入扫码会话请求。 */ async () => {
    startBindingMock.mockResolvedValue(bindingStartResult);
    // hook 是分类开关传值场景的 Hook 渲染结果。
    const hook = renderHook(/* customOptionsHookFactory 创建扫码绑定状态。 */ () => useFeishuAppBinding({ onSuccess: vi.fn() }));
    act(/* openAction 打开扫码绑定弹窗。 */ () => hook.result.current.open());
    await act(/* optionActions 关闭三个账号与分类开关。 */ async () => {
      hook.result.current.setBindAllAccounts(false);
      hook.result.current.setNotifyWarning(false);
      hook.result.current.setNotifyInfo(false);
    });
    expect(hook.result.current.notifyWarning).toBe(false);
    expect(hook.result.current.notifyInfo).toBe(false);
    await act(/* customStartAction 使用自定义开关创建扫码会话。 */ async () => { await hook.result.current.start(); });
    expect(startBindingMock).toHaveBeenCalledWith({ bindAllAccounts: false, notifyWarning: false, notifyInfo: false }, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    hook.unmount();
  });

  test('过期和错误阶段终止轮询并展示后端说明', /* 当前回调验证两个非成功终态不会继续发起请求。 */ async () => {
    startBindingMock.mockResolvedValue(bindingStartResult);
    statusBindingMock
      .mockResolvedValueOnce({ status: 'expired', message: '二维码已过期' })
      .mockResolvedValueOnce({ status: 'error', message: '授权被拒绝' });
    // hook 是扫码终态场景的 Hook 渲染结果。
    const hook = renderHook(/* terminalHookFactory 创建扫码终态状态。 */ () => useFeishuAppBinding({ onSuccess: vi.fn() }));
    act(/* openAction 打开扫码绑定弹窗。 */ () => hook.result.current.open());
    await act(/* startAction 创建扫码会话。 */ async () => { await hook.result.current.start(); });
    await act(/* expireAction 推进轮询获得过期结果。 */ async () => { await vi.advanceTimersByTimeAsync(2_000); });
    expect(hook.result.current.status).toBe('expired');
    expect(hook.result.current.message).toBe('二维码已过期');
    await act(/* expiredAdvanceAction 再次推进时间，验证终态不再轮询。 */ async () => { await vi.advanceTimersByTimeAsync(2_000); });
    expect(statusBindingMock).toHaveBeenCalledTimes(1);

    // retryAction 在过期后重试并以错误状态结束，验证第二条终态路径。
    await act(/* retryAction 重新创建扫码会话。 */ async () => { await hook.result.current.start(); });
    await act(/* errorAction 推进轮询获得错误结果。 */ async () => { await vi.advanceTimersByTimeAsync(2_000); });
    expect(hook.result.current.status).toBe('error');
    expect(hook.result.current.message).toBe('授权被拒绝');
    hook.unmount();
  });

  test('会话创建或状态查询失败时进入可重试错误状态', /* 当前回调验证网络异常不会留下等待中的弹窗。 */ async () => {
    startBindingMock.mockRejectedValueOnce(new Error('网络失败'));
    statusBindingMock.mockRejectedValueOnce(new Error('状态读取失败'));
    // hook 是扫码请求失败场景的 Hook 渲染结果。
    const hook = renderHook(/* failureHookFactory 创建扫码请求失败状态。 */ () => useFeishuAppBinding({ onSuccess: vi.fn() }));
    await act(/* failedStartAction 提交会失败的扫码会话请求。 */ async () => {
      hook.result.current.open();
      await hook.result.current.start();
    });
    expect(hook.result.current.status).toBe('error');
    expect(hook.result.current.message).toBe('网络失败');
    expect(hook.result.current.requesting).toBe(false);

    startBindingMock.mockResolvedValueOnce(bindingStartResult);
    await act(/* retryAction 重试创建扫码会话。 */ async () => { await hook.result.current.start(); });
    expect(hook.result.current.status).toBe('waiting');
    await act(/* failedPollAction 推进轮询并触发状态查询错误。 */ async () => { await vi.advanceTimersByTimeAsync(2_000); });
    expect(hook.result.current.status).toBe('error');
    expect(hook.result.current.message).toBe('状态读取失败');
    hook.unmount();
  });

  test('关闭弹窗取消会话请求并拒绝晚到响应', /* 当前回调验证关闭后旧二维码不会重新写入状态。 */ async () => {
    // startDeferred 是保持未完成的扫码会话响应。
    const startDeferred = createDeferred<FeishuAppBindingStartResult>();
    // requestSignal 保存会话请求使用的取消信号。
    let requestSignal: AbortSignal | undefined;
    startBindingMock.mockImplementation(/* pendingStartFactory 保存取消信号并返回未完成响应。 */ (_bindAllAccounts, options) => {
      requestSignal = options?.signal;
      return startDeferred.promise;
    });
    // hook 是扫码请求取消场景的 Hook 渲染结果。
    const hook = renderHook(/* closeHookFactory 创建可关闭的扫码绑定状态。 */ () => useFeishuAppBinding({ onSuccess: vi.fn() }));
    act(/* openAction 打开扫码绑定弹窗。 */ () => hook.result.current.open());
    // pendingStart 是尚未完成的开始绑定动作。
    let pendingStart!: Promise<void>;
    act(/* beginStartAction 发起扫码会话请求。 */ () => { pendingStart = hook.result.current.start(); });
    expect(hook.result.current.requesting).toBe(true);
    act(/* closeAction 关闭弹窗并取消请求。 */ () => hook.result.current.close());
    expect(requestSignal?.aborted).toBe(true);
    expect(hook.result.current.status).toBe('closed');
    expect(hook.result.current.qrCodeUrl).toBe('');
    expect(hook.result.current.bindAllAccounts).toBe(true);

    startDeferred.resolve(bindingStartResult);
    await act(/* lateResponseAction 等待已取消响应到达。 */ async () => { await pendingStart; });
    expect(hook.result.current.status).toBe('closed');
    expect(hook.result.current.sessionID).toBe('');
    expect(hook.result.current.qrCodeUrl).toBe('');
    hook.unmount();
  });

  test('关闭或卸载后不再执行已安排的轮询', /* 当前回调验证等待阶段的定时器会随关闭和卸载清理。 */ async () => {
    startBindingMock.mockResolvedValue(bindingStartResult);
    // pollSignal 保存轮询流程共享的取消信号。
    let pollSignal: AbortSignal | undefined;
    startBindingMock.mockImplementation(/* signalFactory 记录取消信号并返回成功会话。 */ (_bindAllAccounts, options) => {
      pollSignal = options?.signal;
      return Promise.resolve(bindingStartResult);
    });
    // closeHook 是关闭场景的 Hook 渲染结果。
    const closeHook = renderHook(/* closeTimerHookFactory 创建关闭场景状态。 */ () => useFeishuAppBinding({ onSuccess: vi.fn() }));
    act(/* openAction 打开扫码绑定弹窗。 */ () => closeHook.result.current.open());
    await act(/* startAction 创建扫码会话。 */ async () => { await closeHook.result.current.start(); });
    act(/* closeAction 关闭弹窗并取消定时器。 */ () => closeHook.result.current.close());
    await act(/* advanceAfterCloseAction 推进两秒确认没有状态请求。 */ async () => { await vi.advanceTimersByTimeAsync(2_000); });
    expect(statusBindingMock).not.toHaveBeenCalled();
    expect(pollSignal?.aborted).toBe(true);
    closeHook.unmount();

    // unmountHook 是卸载场景的 Hook 渲染结果。
    const unmountHook = renderHook(/* unmountTimerHookFactory 创建卸载场景状态。 */ () => useFeishuAppBinding({ onSuccess: vi.fn() }));
    act(/* openAction 打开扫码绑定弹窗。 */ () => unmountHook.result.current.open());
    await act(/* startAction 创建扫码会话。 */ async () => { await unmountHook.result.current.start(); });
    unmountHook.unmount();
    await act(/* advanceAfterUnmountAction 推进两秒确认卸载后不再轮询。 */ async () => { await vi.advanceTimersByTimeAsync(2_000); });
    expect(statusBindingMock).not.toHaveBeenCalled();
  });

  test('重新开始时丢弃上一会话的晚到响应和轮询', /* 当前回调验证最新开始请求拥有二维码与轮询状态。 */ async () => {
    // firstStart 是首个扫码会话的未完成响应。
    const firstStart = createDeferred<FeishuAppBindingStartResult>();
    // secondStart 是替代首个会话的第二个未完成响应。
    const secondStart = createDeferred<FeishuAppBindingStartResult>();
    // firstSignal 保存首个会话的取消信号。
    let firstSignal: AbortSignal | undefined;
    startBindingMock
      .mockImplementationOnce(/* firstStartFactory 保存首个会话取消信号。 */ (_bindAllAccounts, options) => {
        firstSignal = options?.signal;
        return firstStart.promise;
      })
      .mockImplementationOnce(/* secondStartFactory 返回第二个待完成会话。 */ () => secondStart.promise);
    // hook 是连续开始场景的 Hook 渲染结果。
    const hook = renderHook(/* restartHookFactory 创建连续开始状态。 */ () => useFeishuAppBinding({ onSuccess: vi.fn() }));
    act(/* openAction 打开扫码绑定弹窗。 */ () => hook.result.current.open());
    // firstTask 是首个开始绑定动作。
    let firstTask!: Promise<void>;
    // secondTask 是第二个开始绑定动作。
    let secondTask!: Promise<void>;
    act(/* startActions 连续发起两个扫码会话。 */ () => {
      firstTask = hook.result.current.start();
      secondTask = hook.result.current.start();
    });
    expect(firstSignal?.aborted).toBe(true);

    secondStart.resolve({ ...bindingStartResult, session_id: 'session-2', qr_code_url: 'data:image/png;base64,second' });
    await act(/* secondResolveAction 完成最新会话。 */ async () => { await secondTask; });
    expect(hook.result.current.sessionID).toBe('session-2');
    expect(hook.result.current.qrCodeUrl).toBe('data:image/png;base64,second');

    firstStart.resolve(bindingStartResult);
    await act(/* firstResolveAction 完成已过期的首个会话。 */ async () => { await firstTask; });
    expect(hook.result.current.sessionID).toBe('session-2');
    expect(hook.result.current.qrCodeUrl).toBe('data:image/png;base64,second');
    expect(hook.result.current.status).toBe('waiting');
    hook.unmount();
  });
});
