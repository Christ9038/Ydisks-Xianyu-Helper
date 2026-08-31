// @vitest-environment jsdom
import { act,renderHook,waitFor } from '@testing-library/react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import type { OperationResponse,SystemSettings } from './api';
import { fetchAIModels,getSystemSettings,updateLoginCredentials,updateSystemSettings,verifySession } from './api';
import { useSettings } from './hooks';

vi.mock('./api', /* settingsApiMockFactory 提供设置 Hook 的确定性 API 替身。 */ () => ({
  fetchAIModels: vi.fn(),
  getSystemSettings: vi.fn(),
  updateLoginCredentials: vi.fn(),
  updateSystemSettings: vi.fn(),
  verifySession: vi.fn(),
}));

// fetchModelsMock 是模型发现请求的可控替身。
const fetchModelsMock = vi.mocked(fetchAIModels);
// getSettingsMock 是系统设置读取请求的可控替身。
const getSettingsMock = vi.mocked(getSystemSettings);
// updateCredentialsMock 是登录凭据更新请求的可控替身。
const updateCredentialsMock = vi.mocked(updateLoginCredentials);
// updateSettingsMock 是系统设置保存请求的可控替身。
const updateSettingsMock = vi.mocked(updateSystemSettings);
// verifySessionMock 是会话校验请求的可控替身。
const verifySessionMock = vi.mocked(verifySession);

// settingsFixture 是覆盖 AI、验证码和日志字段的系统配置。
const settingsFixture: SystemSettings = { ai_api_url: 'https://ai.example.com', ai_api_key: 'secret', ai_model: '', 'captcha.remote_service_url': 'https://captcha.example.com', log_level: 'info' };
// noopReload 是设置凭据成功后定时重载页面的浏览器行为替身。
const noopReload = vi.fn();
// renderSettingsHook 是渲染系统设置 Hook 的具名回调。
const renderSettingsHook = () => useSettings();

describe('useSettings', /* 当前回调处理系统设置、模型和凭据请求状态。 */ () => {
  beforeEach(/* 当前回调重置设置 API 替身和浏览器副作用。 */ () => {
    vi.clearAllMocks();
    getSettingsMock.mockResolvedValue(settingsFixture);
    verifySessionMock.mockResolvedValue({ authenticated: true, username: 'admin' });
    fetchModelsMock.mockResolvedValue(['model-a', 'model-b']);
    updateSettingsMock.mockResolvedValue({ success: true });
    updateCredentialsMock.mockResolvedValue({ success: true, message: '凭据已更新' });
    vi.spyOn(window, 'alert').mockImplementation(
      // alertImplementation 屏蔽设置保存成功时的浏览器弹窗。
      () => undefined,
    );
    Object.defineProperty(window, 'location', { configurable: true, value: { ...window.location, reload: noopReload } });
  });

  test('加载设置和模型后可以保存配置', /* 当前回调验证系统设置成功加载和保存路径。 */ async () => {
    // hook 是系统设置 Hook 的渲染结果。
    const hook = renderHook(renderSettingsHook);
    await waitFor(
      // statusAssertion 等待系统设置加载成功。
      () => expect(hook.result.current.requestStatus).toBe('success'),
    );
    expect(hook.result.current.settings).toMatchObject({ ...settingsFixture, ai_model: 'model-a' });
    expect(hook.result.current.aiModels).toEqual(['model-a', 'model-b']);
    expect(hook.result.current.credentials.new_username).toBe('admin');

    await act(
      // saveAction 执行系统设置保存动作。
      async () => hook.result.current.handleSave(),
    );
    expect(updateSettingsMock).toHaveBeenCalledWith(expect.objectContaining({ ai_api_url: settingsFixture.ai_api_url }), expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(window.alert).toHaveBeenCalledWith('系统配置已保存');
  });

  test('模型发现失败时清空列表并展示错误', /* 当前回调验证模型接口错误路径。 */ async () => {
    fetchModelsMock.mockRejectedValue(new Error('模型服务不可用'));
    // hook 是模型发现失败场景下的系统设置 Hook 渲染结果。
    const hook = renderHook(renderSettingsHook);
    await waitFor(
      // statusAssertion 等待初始系统设置请求完成。
      () => expect(hook.result.current.requestStatus).toBe('success'),
    );
    await act(
      // loadModelsAction 触发失败的模型发现请求。
      async () => hook.result.current.loadAIModels(settingsFixture, true),
    );
    expect(hook.result.current.aiModels).toEqual([]);
    expect(hook.result.current.modelDropdownOpen).toBe(false);
    expect(hook.result.current.modelError).toBe('模型服务不可用');
  });

  test('凭据校验失败和服务拒绝都写入错误提示', /* 当前回调验证登录凭据校验和后端拒绝路径。 */ async () => {
    // hook 是凭据表单测试使用的系统设置 Hook 渲染结果。
    const hook = renderHook(renderSettingsHook);
    await waitFor(
      // statusAssertion 等待凭据测试使用的系统设置请求完成。
      () => expect(hook.result.current.requestStatus).toBe('success'),
    );
    // invalidEvent 是触发前端凭据校验的最小表单事件。
    const invalidEvent = { preventDefault: vi.fn() } as unknown as React.FormEvent;
    await act(
      // invalidAction 提交非法凭据表单并触发前端校验。
      async () => hook.result.current.handleCredentialsSave(invalidEvent),
    );
    expect(hook.result.current.credentialsMessage?.type).toBe('error');
    expect(updateCredentialsMock).not.toHaveBeenCalled();

    await act(
      // invalidCredentials 设置密码确认不一致的草稿。
      () => hook.result.current.setCredentials({ new_username: 'new-admin', current_password: 'old-password', new_password: 'new-password', confirm_password: 'wrong-password' }),
    );
    // validEvent 是触发后端凭据拒绝的表单事件。
    const validEvent = { preventDefault: vi.fn() } as unknown as React.FormEvent;
    await act(
      // mismatchAction 提交确认密码不一致的表单。
      async () => hook.result.current.handleCredentialsSave(validEvent),
    );
    expect(updateCredentialsMock).not.toHaveBeenCalled();

    await act(
      // validCredentials 设置可以提交的凭据草稿。
      () => hook.result.current.setCredentials({ new_username: 'new-admin', current_password: 'old-password', new_password: 'new-password', confirm_password: 'new-password' }),
    );
    // rejectedResponse 是后端拒绝凭据更新的统一响应。
    const rejectedResponse: OperationResponse = { success: false, message: '当前密码错误' };
    updateCredentialsMock.mockResolvedValueOnce(rejectedResponse);
    // rejectedEvent 是触发后端拒绝分支的表单事件。
    const rejectedEvent = { preventDefault: vi.fn() } as unknown as React.FormEvent;
    await act(
      // rejectedAction 提交合法表单并验证后端拒绝提示。
      async () => hook.result.current.handleCredentialsSave(rejectedEvent),
    );
    expect(hook.result.current.credentialsMessage).toEqual({ type: 'error', text: '当前密码错误' });
  });

  test('设置读取失败时展示加载错误', /* 当前回调验证系统设置初始请求失败路径。 */ async () => {
    getSettingsMock.mockRejectedValueOnce(new Error('设置服务失败'));
    // hook 是设置读取失败场景下的系统设置 Hook 渲染结果。
    const hook = renderHook(renderSettingsHook);
    await waitFor(
      // errorAssertion 等待设置读取错误状态写入。
      () => expect(hook.result.current.requestStatus).toBe('error'),
    );
    expect(hook.result.current.loadError).toBe('设置服务失败');
    expect(hook.result.current.settings).toBeNull();
    await act(
      // saveGuardAction 在没有配置数据时阻止保存请求。
      async () => hook.result.current.handleSave(),
    );
    expect(updateSettingsMock).not.toHaveBeenCalled();
    hook.unmount();
  });

  test('配置保存和凭据网络异常时保留错误状态', /* 当前回调验证设置保存与凭据网络错误分支。 */ async () => {
    // hook 是设置保存异常场景的系统设置 Hook 渲染结果。
    const hook = renderHook(renderSettingsHook);
    await waitFor(
      // statusAssertion 等待设置读取完成。
      () => expect(hook.result.current.requestStatus).toBe('success'),
    );
    updateSettingsMock.mockRejectedValueOnce(new Error('配置保存失败'));
    await act(
      // saveErrorAction 触发系统配置保存错误。
      async () => hook.result.current.handleSave(),
    );
    expect(hook.result.current.saveError).toBe('配置保存失败');

    await act(
      // credentialsAction 写入合法登录凭据。
      () => hook.result.current.setCredentials({ new_username: 'new-admin', current_password: 'old-password', new_password: 'new-password', confirm_password: 'new-password' }),
    );
    updateCredentialsMock.mockRejectedValueOnce(new Error('凭据网络失败'));
    // credentialsEvent 是触发凭据保存的表单事件。
    const credentialsEvent = { preventDefault: vi.fn() } as unknown as React.FormEvent;
    await act(
      // credentialsErrorAction 触发凭据网络异常。
      async () => hook.result.current.handleCredentialsSave(credentialsEvent),
    );
    expect(hook.result.current.credentialsMessage).toEqual({ type: 'error', text: '凭据网络失败' });

    updateCredentialsMock.mockResolvedValueOnce({ success: true, message: '更新成功' });
    await act(
      // credentialsSuccessAction 验证凭据成功提示和重载调度。
      async () => hook.result.current.handleCredentialsSave(credentialsEvent),
    );
    expect(hook.result.current.credentialsMessage).toEqual({ type: 'success', text: '更新成功' });
    await waitFor(
      // reloadAssertion 等待凭据保存成功后的页面重载调度。
      () => expect(noopReload).toHaveBeenCalled(),
      { timeout: 2_000 },
    );
    hook.unmount();
  });

  test('点击模型选择器外部会关闭下拉框', /* 当前回调验证模型选择器的文档事件边界。 */ async () => {
    // hook 是模型选择器外部点击场景的系统设置 Hook 渲染结果。
    const hook = renderHook(renderSettingsHook);
    await waitFor(
      // statusAssertion 等待设置加载完成后再操作下拉框。
      () => expect(hook.result.current.requestStatus).toBe('success'),
    );
    // picker 是模型选择器根节点替身。
    const picker = document.createElement('div');
    hook.result.current.modelPickerRef.current = picker;
    await act(
      // openAction 打开模型下拉框。
      () => hook.result.current.setModelDropdownOpen(true),
    );
    document.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    await waitFor(
      // closeAssertion 等待外部点击关闭下拉框。
      () => expect(hook.result.current.modelDropdownOpen).toBe(false),
    );
    hook.unmount();
  });

  test('重复加载设置时丢弃先发出的旧响应', /* 当前回调验证系统设置请求代次隔离。 */ async () => {
    // resolveFirst 是旧设置请求的完成控制器。
    let resolveFirst: (value: SystemSettings) => void = () => undefined;
    // firstRequest 是保持未完成的旧设置请求 Promise。
    const firstRequest = new Promise<SystemSettings>(/* firstExecutor 保存旧请求完成函数。 */ resolve => { resolveFirst = resolve; });
    getSettingsMock.mockReset();
    getSettingsMock.mockReturnValueOnce(firstRequest);
    getSettingsMock.mockResolvedValue(settingsFixture);
    // hook 是系统设置刷新竞态场景的 Hook 渲染结果。
    const hook = renderHook(renderSettingsHook);
    await act(
      // refreshAction 发起第二次设置加载并使首次请求过期。
      () => hook.result.current.loadSettings(),
    );
    await waitFor(
      // successAssertion 等待第二次设置加载成功。
      () => expect(hook.result.current.requestStatus).toBe('success'),
    );
    resolveFirst(settingsFixture);
    await act(
      // staleResolveAction 完成已过期的首次设置响应。
      async () => { await firstRequest; },
    );
    expect(hook.result.current.settings).toMatchObject({ ...settingsFixture, ai_model: 'model-a' });
    hook.unmount();
  });

  test('保存新配置时取消并丢弃旧配置发起的模型响应', /* 当前回调验证保存动作隔离旧模型发现请求。 */ async () => {
    // resolveModels 是旧模型请求的完成控制器。
    let resolveModels: (value: string[]) => void = () => undefined;
    // staleModelsRequest 模拟使用旧地址和旧密钥发出的延迟模型请求。
    const staleModelsRequest = new Promise<string[]>(/* staleModelsExecutor 保存旧模型请求完成函数。 */ resolve => { resolveModels = resolve; });
    fetchModelsMock.mockReset();
    fetchModelsMock.mockReturnValueOnce(staleModelsRequest);
    // hook 是保存配置与模型发现竞态场景的 Hook 渲染结果。
    const hook = renderHook(renderSettingsHook);
    await waitFor(
      // statusAssertion 等待设置读取成功并发出旧模型请求。
      () => expect(hook.result.current.requestStatus).toBe('success'),
    );
    expect(fetchModelsMock).toHaveBeenCalledTimes(1);
    // staleSignal 是旧模型发现请求使用的取消信号。
    const staleSignal = fetchModelsMock.mock.calls[0]?.[2]?.signal;
    await act(
      // editSettingsAction 将配置草稿切换到新的模型服务凭据。
      () => hook.result.current.setSettings(
        // settingsUpdater 基于现有草稿替换模型服务地址和密钥。
        previous => previous ? { ...previous, ai_api_url: 'https://new-ai.example.com', ai_api_key: 'new-secret' } : previous,
      ),
    );
    await act(
      // saveAction 保存新配置并使旧模型请求失效。
      async () => hook.result.current.handleSave(),
    );
    expect(staleSignal?.aborted).toBe(true);
    expect(updateSettingsMock).toHaveBeenCalledWith(expect.objectContaining({ ai_api_url: 'https://new-ai.example.com', ai_api_key: 'new-secret' }), expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.aiModels).toEqual([]);
    expect(hook.result.current.modelsLoading).toBe(false);

    resolveModels(['stale-model']);
    await act(
      // staleResolveAction 完成被保存动作取消的旧模型响应。
      async () => { await staleModelsRequest; },
    );
    expect(hook.result.current.aiModels).toEqual([]);
    expect(hook.result.current.settings?.ai_model).toBe('');
    hook.unmount();
  });
});
