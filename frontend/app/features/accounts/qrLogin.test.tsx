// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { AccountDetail } from './api';

// qrLoginMocks 保存二维码登录协调器测试使用的接口替身。
const qrLoginMocks = vi.hoisted(/* qrLoginMockFactory 创建二维码登录 API 替身。 */ () => ({
  generateQRLogin: vi.fn(),
  checkQRLoginStatus: vi.fn(),
  completeQRVerification: vi.fn(),
  startCredentialVerification: vi.fn(),
  isVerificationExpiredError: vi.fn(/* 与真实实现一致：仅 status 为 409 的错误视为登记过期。 */ (error: { status?: number }) => error?.status === 409),
}));

vi.mock('./api', /* qrApiMockFactory 提供二维码登录接口替身。 */ () => ({
  generateQRLogin: qrLoginMocks.generateQRLogin,
  checkQRLoginStatus: qrLoginMocks.checkQRLoginStatus,
  completeQRVerification: qrLoginMocks.completeQRVerification,
  startCredentialVerification: qrLoginMocks.startCredentialVerification,
  isVerificationExpiredError: qrLoginMocks.isVerificationExpiredError,
}));

import { useAccountQRCodeLogin } from './qrLogin';

// accountFixture 表示二维码重新授权测试使用的账号。
const accountFixture: AccountDetail = {
  id: 'account-1',
  nickname: 'Alpha',
  remark: '主账号',
  enabled: true,
  auto_confirm: false,
  auto_consign: false,
  auto_bargain: false,
  runtime_message: '在线',
};

// DelayedVerificationResponse 是测试手动完成的验证会话创建响应。
interface DelayedVerificationResponse {
  /** success 表示验证会话是否已创建。 */ success: boolean;
  /** session_id 是交给轮询使用的会话标识。 */ session_id: string;
}

// flushMicrotasks 等待轮询请求和回调完成。
const flushMicrotasks = async () => {
  await Promise.resolve();
  await Promise.resolve();
};

describe('useAccountQRCodeLogin 二维码登录协调器', /* 当前回调验证二维码生成、轮询、验证和清理边界。 */ () => {
  beforeEach(/* 当前回调重置二维码接口替身并启用可控定时器。 */ () => {
    vi.useFakeTimers();
    vi.clearAllMocks();
    qrLoginMocks.generateQRLogin.mockResolvedValue({ success: true, qr_code_url: 'qr-url', session_id: 'session-1' });
    qrLoginMocks.checkQRLoginStatus.mockResolvedValue({ status: 'waiting' });
    qrLoginMocks.completeQRVerification.mockResolvedValue({ success: true, account_id: 'account-1' });
  });

  afterEach(/* 当前回调恢复真实定时器，避免污染其他测试。 */ () => {
    vi.useRealTimers();
  });

  test('成功登录后完成持久化并延迟刷新账号列表', /* 当前回调验证二维码成功的完整生命周期。 */ async () => {
    // onLoginSuccess 刷新登录成功后的账号列表。
    const onLoginSuccess = vi.fn();
    // hook 保存二维码登录 Hook 的渲染结果。
    const hook = renderHook(/* hookFactory 创建二维码登录 Hook。 */ () => useAccountQRCodeLogin({ onLoginSuccess }));

    await act(/* startAction 启动二维码登录流程。 */ async () => {
      await hook.result.current.startQRLogin(accountFixture);
    });
    expect(hook.result.current.qrStatus).toBe('waiting');
    expect(hook.result.current.qrCodeUrl).toBe('qr-url');

    qrLoginMocks.checkQRLoginStatus.mockResolvedValueOnce({ status: 'success' });
    await act(/* pollAction 触发二维码状态轮询。 */ async () => {
      await vi.advanceTimersByTimeAsync(2000);
      await flushMicrotasks();
    });

    expect(qrLoginMocks.completeQRVerification).toHaveBeenCalledWith('session-1', 'account-1', expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.qrStatus).toBe('success');
    await act(/* closeAction 推进成功后的延迟关闭定时器。 */ async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(onLoginSuccess).toHaveBeenCalledTimes(1);
    expect(hook.result.current.showQRModal).toBe(false);
    hook.unmount();
  });

  test('风控验证状态会展示截图和人脸二维码', /* 当前回调验证风控中间态数据透传。 */ async () => {
    // hook 保存风控验证场景的二维码登录 Hook。
    const hook = renderHook(/* hookFactory 创建风控验证场景 Hook。 */ () => useAccountQRCodeLogin({ onLoginSuccess: vi.fn() }));
    await act(/* startAction 启动二维码登录流程。 */ async () => {
      await hook.result.current.startQRLogin(accountFixture);
    });

    qrLoginMocks.checkQRLoginStatus.mockResolvedValueOnce({
      status: 'verification_required',
      verification_screenshot: 'screenshot-url',
      face_qr_url: 'face-qr-url',
    });
    await act(/* pollAction 触发风控验证状态轮询。 */ async () => {
      await vi.advanceTimersByTimeAsync(2000);
      await flushMicrotasks();
    });

    expect(hook.result.current.qrStatus).toBe('verification');
    expect(hook.result.current.verificationScreenshot).toBe('screenshot-url');
    expect(hook.result.current.faceQrUrl).toBe('face-qr-url');
    expect(hook.result.current.qrReauthTarget).toEqual(accountFixture);
  });

  test('生成二维码失败时展示接口错误', /* 当前回调验证二维码生成异常反馈。 */ async () => {
    qrLoginMocks.generateQRLogin.mockResolvedValueOnce({ success: false, message: '登录服务不可用' });
    // hook 保存二维码生成异常场景的 Hook。
    const hook = renderHook(/* hookFactory 创建二维码生成异常场景 Hook。 */ () => useAccountQRCodeLogin({ onLoginSuccess: vi.fn() }));

    await act(/* startAction 启动会失败的二维码生成流程。 */ async () => {
      await hook.result.current.startQRLogin();
    });

    expect(hook.result.current.qrStatus).toBe('error');
    expect(hook.result.current.qrErrorMessage).toBe('登录服务不可用');
  });

  test('持久化失败时将成功轮询转换为错误状态', /* 当前回调验证扫码成功后的保存失败边界。 */ async () => {
    qrLoginMocks.completeQRVerification.mockResolvedValueOnce({ success: false, message: '账号保存失败' });
    // hook 保存持久化失败场景的 Hook。
    const hook = renderHook(/* hookFactory 创建保存失败场景 Hook。 */ () => useAccountQRCodeLogin({ onLoginSuccess: vi.fn() }));
    await act(/* startAction 启动二维码登录流程。 */ async () => {
      await hook.result.current.startQRLogin();
    });

    qrLoginMocks.checkQRLoginStatus.mockResolvedValueOnce({ status: 'success' });
    await act(/* pollAction 触发成功状态并执行持久化。 */ async () => {
      await vi.advanceTimersByTimeAsync(2000);
      await flushMicrotasks();
    });

    expect(hook.result.current.qrStatus).toBe('error');
    expect(hook.result.current.showQRModal).toBe(true);
  });

  test('扫描、终止和轮询异常状态均能更新页面状态', /* 当前回调验证二维码轮询的非成功状态回调。 */ async () => {
    // hook 保存二维码状态切换场景的 Hook。
    const hook = renderHook(/* hookFactory 创建二维码状态切换场景 Hook。 */ () => useAccountQRCodeLogin({ onLoginSuccess: vi.fn() }));
    await act(/* startAction 启动二维码登录流程。 */ async () => {
      await hook.result.current.startQRLogin();
    });

    qrLoginMocks.checkQRLoginStatus.mockResolvedValueOnce({ status: 'scanned' });
    await act(/* scannedAction 触发已扫描状态轮询。 */ async () => {
      await vi.advanceTimersByTimeAsync(2000);
      await flushMicrotasks();
    });
    expect(hook.result.current.qrStatus).toBe('waiting');

    qrLoginMocks.checkQRLoginStatus.mockResolvedValueOnce({ status: 'expired' });
    await act(/* terminalAction 触发二维码过期状态轮询。 */ async () => {
      await vi.advanceTimersByTimeAsync(2000);
      await flushMicrotasks();
    });
    expect(hook.result.current.qrStatus).toBe('error');

    await act(/* restartAction 重新启动二维码以验证轮询异常。 */ async () => {
      await hook.result.current.startQRLogin();
    });
    qrLoginMocks.checkQRLoginStatus.mockRejectedValueOnce(new Error('网络不可用'));
    await act(/* pollErrorAction 触发二维码轮询异常。 */ async () => {
      await vi.advanceTimersByTimeAsync(2000);
      await flushMicrotasks();
    });
    expect(hook.result.current.qrStatus).toBe('error');
  });

  test('二维码响应缺少必要字段时使用默认错误提示', /* 当前回调验证二维码响应结构不完整的错误提示。 */ async () => {
    qrLoginMocks.generateQRLogin.mockResolvedValueOnce({ success: true });
    // hook 保存二维码响应缺字段场景的 Hook。
    const hook = renderHook(/* hookFactory 创建响应缺字段场景 Hook。 */ () => useAccountQRCodeLogin({ onLoginSuccess: vi.fn() }));

    await act(/* startAction 启动响应缺字段的二维码流程。 */ async () => {
      await hook.result.current.startQRLogin();
    });

    expect(hook.result.current.qrStatus).toBe('error');
    expect(hook.result.current.qrErrorMessage).toBe('闲鱼未返回可用的登录二维码');
  });

  test('成功后的手动关闭会清理延迟关闭定时器', /* 当前回调验证成功状态下主动关闭的资源清理。 */ async () => {
    // hook 保存成功后主动关闭场景的 Hook。
    const hook = renderHook(/* hookFactory 创建成功后主动关闭场景 Hook。 */ () => useAccountQRCodeLogin({ onLoginSuccess: vi.fn() }));
    await act(/* startAction 启动二维码登录流程。 */ async () => {
      await hook.result.current.startQRLogin();
    });
    qrLoginMocks.checkQRLoginStatus.mockResolvedValueOnce({ status: 'success' });
    await act(/* pollAction 触发二维码成功状态。 */ async () => {
      await vi.advanceTimersByTimeAsync(2000);
      await flushMicrotasks();
    });

    act(/* closeAction 在延迟关闭前主动关闭二维码弹窗。 */ () => {
      hook.result.current.closeQRModal();
    });
    expect(hook.result.current.showQRModal).toBe(false);
  });

  test('关闭弹窗会取消未完成的二维码生成请求', /* 当前回调验证关闭操作的异步资源收束。 */ async () => {
    // resolveGeneration 允许测试在关闭后完成原始请求。
    let resolveGeneration: ((value: {
      /** success 表示二维码生成请求是否成功。 */
      success: boolean;
      /** qr_code_url 表示返回的二维码地址。 */
      qr_code_url?: string;
      /** session_id 表示二维码登录会话标识。 */
      session_id?: string;
    }) => void) | undefined;
    qrLoginMocks.generateQRLogin.mockImplementationOnce(/* generateAction 创建可取消的二维码请求。 */ ({ signal }: { signal: AbortSignal }) => new Promise(resolve => {
      signal.addEventListener('abort', /* abortAction 记录二维码请求取消事件。 */ () => resolveGeneration = resolve, { once: true });
      resolveGeneration = resolve;
    }));
    // hook 保存请求取消场景的二维码登录 Hook。
    const hook = renderHook(/* hookFactory 创建请求取消场景 Hook。 */ () => useAccountQRCodeLogin({ onLoginSuccess: vi.fn() }));
    // startPromise 保存尚未完成的二维码生成请求。
    const startPromise = hook.result.current.startQRLogin();

    act(/* closeAction 关闭二维码弹窗并取消请求。 */ () => {
      hook.result.current.closeQRModal();
    });
    expect(hook.result.current.showQRModal).toBe(false);
    expect(qrLoginMocks.generateQRLogin).toHaveBeenCalledWith(expect.objectContaining({ signal: expect.any(AbortSignal) }));
    resolveGeneration?.({ success: true, qr_code_url: 'stale-url', session_id: 'stale-session' });
    await act(/* settleAction 等待被取消请求的异步收束。 */ async () => {
      await startPromise;
    });
    expect(hook.result.current.qrCodeUrl).toBe('');
  });
  test('续期安全验证创建会话后进入验证态并沿用轮询完成授权', /* 当前回调验证 credential 流程成功路径。 */ async () => {
    // 验证会话创建响应：success 表示已创建，session_id 是交给轮询使用的会话标识。
    qrLoginMocks.startCredentialVerification.mockResolvedValue({ success: true, session_id: 'verify-1' });
    qrLoginMocks.checkQRLoginStatus.mockResolvedValue({ status: 'verification_required', face_qr_url: 'face-url' });
    // onLoginSuccess 是授权成功后刷新账号列表的测试替身。
    const onLoginSuccess = vi.fn();
    // hook 保存二维码登录 Hook 的渲染结果。
    const hook = renderHook(/* hookFactory 创建二维码登录 Hook。 */ () => useAccountQRCodeLogin({ onLoginSuccess }));
    await act(/* 启动续期安全验证。 */ async () => {
      await hook.result.current.startCredentialVerification(accountFixture);
    });
    expect(qrLoginMocks.startCredentialVerification).toHaveBeenCalledWith('account-1', expect.anything());
    expect(qrLoginMocks.generateQRLogin).not.toHaveBeenCalled();
    expect(hook.result.current.qrFlow).toBe('credential');
    expect(hook.result.current.qrStatus).toBe('verification');
    expect(hook.result.current.showQRModal).toBe(true);
  });

  test('验证登记已过期（409）时回退到扫码重新授权', /* 当前回调验证 409 回退。 */ async () => {
    qrLoginMocks.startCredentialVerification.mockRejectedValue({ status: 409, message: '已过期' });
    // hook 保存二维码登录 Hook 的渲染结果。
    const hook = renderHook(/* hookFactory 创建二维码登录 Hook。 */ () => useAccountQRCodeLogin({ onLoginSuccess: vi.fn() }));
    await act(/* 启动续期安全验证。 */ async () => {
      await hook.result.current.startCredentialVerification(accountFixture);
    });
    expect(qrLoginMocks.generateQRLogin).toHaveBeenCalledTimes(1);
    expect(hook.result.current.qrFlow).toBe('login');
    expect(hook.result.current.qrStatus).toBe('waiting');
  });

  test('验证会话创建失败时展示错误且不回退', /* 当前回调验证非 409 失败。 */ async () => {
    qrLoginMocks.startCredentialVerification.mockRejectedValue({ status: 502, message: '平台失败' });
    // hook 保存二维码登录 Hook 的渲染结果。
    const hook = renderHook(/* hookFactory 创建二维码登录 Hook。 */ () => useAccountQRCodeLogin({ onLoginSuccess: vi.fn() }));
    await act(/* 启动续期安全验证。 */ async () => {
      await hook.result.current.startCredentialVerification(accountFixture);
    });
    expect(qrLoginMocks.generateQRLogin).not.toHaveBeenCalled();
    expect(hook.result.current.qrStatus).toBe('error');
  });

  test('关闭弹窗取消创建请求，过期响应不再改变状态', /* 当前回调验证取消与过期响应。 */ async () => {
    // resolveStart 在测试中手动完成创建请求，模拟关闭后才到达的响应。
    let resolveStart: (value: DelayedVerificationResponse) => void = () => undefined;
    qrLoginMocks.startCredentialVerification.mockReturnValue(new Promise(/* 挂起直到测试手动完成。 */ resolve => { resolveStart = resolve; }));
    // hook 保存二维码登录 Hook 的渲染结果。
    const hook = renderHook(/* hookFactory 创建二维码登录 Hook。 */ () => useAccountQRCodeLogin({ onLoginSuccess: vi.fn() }));
    act(/* 发起请求但不等待。 */ () => { void hook.result.current.startCredentialVerification(accountFixture); });
    act(/* 用户关闭弹窗。 */ () => { hook.result.current.closeQRModal(); });
    await act(/* 过期响应到达。 */ async () => {
      // success 与 session_id 模拟关闭弹窗后才到达的创建响应。
      resolveStart({ success: true, session_id: 'late' });
      await flushMicrotasks();
    });
    expect(hook.result.current.showQRModal).toBe(false);
    expect(qrLoginMocks.checkQRLoginStatus).not.toHaveBeenCalled();
  });
});
