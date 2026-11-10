// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import type { AgentReplySettings, AgentReplyTestResult } from './api';
import { getAgentReplySettings, saveAgentReplySettings, testAgentReplyConnection } from './api';
import { agentReplyErrorMessage, agentReplyFormFromSettings, agentReplyPayloadFromForm, emptyAgentReplyForm, useAgentReply, validateAgentReplyForm } from './agentReply';

vi.mock('./api', /* agentReplyApiMockFactory 提供 Agent 回复 Hook 的确定性 API 替身。 */ () => ({
  getAgentReplySettings: vi.fn(),
  saveAgentReplySettings: vi.fn(),
  testAgentReplyConnection: vi.fn(),
}));

// loadMock、saveMock、testMock 是三个 API 的可控替身。
const loadMock = vi.mocked(getAgentReplySettings);
// saveMock 是配置保存接口的可控替身。
const saveMock = vi.mocked(saveAgentReplySettings);
// testMock 是连接测试接口的可控替身。
const testMock = vi.mocked(testAgentReplyConnection);

// settingsFixture 是账号 a1 已启用且已保存令牌的配置。
const settingsFixture: AgentReplySettings = { cookie_id: 'a1', enabled: true, endpoint_url: 'https://agent.example.com/v1', token_configured: true, request_timeout_seconds: 15, max_session_seconds: 120 };
// okResult 是成功的连接测试结果。
const okResult: AgentReplyTestResult = { status: 'ok', status_code: 200, duration_ms: 5, handled: true, working: false, reply_count: 1 };

// deferred 创建可手动决定完成时机的 Promise，用于构造过期响应。
const deferred = <T,>() => {
  // resolve、reject 在测试需要时完成挂起的 Promise。
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  // promise 是由 resolve 与 reject 手动控制完成时机的挂起请求。
  const promise = new Promise<T>(/* executor 暴露 resolve 与 reject。 */ (ok, fail) => { resolve = ok; reject = fail; });
  return { promise, resolve, reject };
};

describe('useAgentReply', /* 当前回调覆盖 Agent 回复 Hook 的加载、编辑、保存、测试、切换和过期响应。 */ () => {
  beforeEach(/* 当前回调重置替身为成功默认值。 */ () => {
    vi.clearAllMocks();
    loadMock.mockResolvedValue(settingsFixture);
    saveMock.mockResolvedValue(settingsFixture);
    testMock.mockResolvedValue(okResult);
  });

  test('加载配置填充表单，失败时暴露错误并可重试', /* 当前回调执行该行为用例。 */ async () => {
    // result 提供被测 Hook 的最新返回值。
    const { result } = renderHook(/* 当前回调渲染被测 Hook。 */ () => useAgentReply('a1'));
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.settings).toEqual(settingsFixture));
    expect(result.current.form).toEqual(agentReplyFormFromSettings(settingsFixture));
    expect(loadMock).toHaveBeenCalledWith('a1', expect.objectContaining({ signal: expect.any(AbortSignal) }));
    loadMock.mockRejectedValueOnce(new Error('网络错误'));
    await act(/* 当前回调在 act 中触发重试并等待完成。 */ async () => { await result.current.reload(); });
    expect(result.current.loadError).toBe('网络错误');
    await act(/* 当前回调在 act 中再次重试。 */ async () => { await result.current.reload(); });
    expect(result.current.loadError).toBe('');
  });

  test('未选择账号时不发请求并保持关闭表单', /* 当前回调执行该行为用例。 */ async () => {
    // result 提供被测 Hook 的最新返回值。
    const { result } = renderHook(/* 当前回调渲染被测 Hook。 */ () => useAgentReply(''));
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.loading).toBe(false));
    expect(loadMock).not.toHaveBeenCalled();
    expect(result.current.form).toEqual(emptyAgentReplyForm());
    await act(/* 当前回调在 act 中触发保存与测试。 */ async () => { await result.current.save(); await result.current.test(); });
    expect(saveMock).not.toHaveBeenCalled();
    expect(testMock).not.toHaveBeenCalled();
  });

  test('切换账号后丢弃旧账号的过期响应并重置令牌草稿', /* 当前回调执行该行为用例。 */ async () => {
    // slow 是故意延迟完成的旧账号请求。
    const slow = deferred<AgentReplySettings>();
    loadMock.mockImplementationOnce(/* 当前回调返回由测试控制完成时机的挂起请求。 */ () => slow.promise);
    // result、rerender 提供 Hook 的最新返回值和重新渲染入口。
    const { result, rerender } = renderHook(/* 当前回调渲染被测 Hook。 */ ({ id }) => useAgentReply(id), { initialProps: { id: 'a1' } });
    loadMock.mockResolvedValueOnce({ ...settingsFixture, cookie_id: 'a2', endpoint_url: 'https://two.example.com' });
    rerender({ id: 'a2' });
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.settings?.cookie_id).toBe('a2'));
    await act(/* 当前回调让过期响应此时到达。 */ async () => { slow.resolve(settingsFixture); await slow.promise; });
    expect(result.current.settings?.cookie_id).toBe('a2');
    expect(result.current.form.endpointUrl).toBe('https://two.example.com');
    // 输入令牌后再切换账号，令牌草稿被丢弃。
    act(/* 当前回调输入新令牌。 */ () => result.current.setTokenValue('secret'));
    expect(result.current.form.tokenValue).toBe('secret');
    loadMock.mockResolvedValueOnce(settingsFixture);
    rerender({ id: 'a1' });
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.form.endpointUrl).toBe('https://agent.example.com/v1'));
    expect(result.current.form.tokenValue).toBe('');
    expect(result.current.form.tokenAction).toBe('retain');
  });

  test('令牌命令：已配置时更换、清除、撤销；未配置时输入即替换、清空即保留', /* 当前回调执行该行为用例。 */ async () => {
    // result 提供被测 Hook 的最新返回值。
    const { result } = renderHook(/* 当前回调渲染被测 Hook。 */ () => useAgentReply('a1'));
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.settings).not.toBeNull());
    act(/* 当前回调进入更换令牌。 */ () => result.current.beginReplaceToken());
    expect(result.current.form).toMatchObject({ tokenAction: 'replace', tokenValue: '' });
    act(/* 当前回调输入令牌。 */ () => result.current.setTokenValue('abc'));
    expect(result.current.form).toMatchObject({ tokenAction: 'replace', tokenValue: 'abc' });
    act(/* 当前回调清空输入（已配置令牌时仍是替换）。 */ () => result.current.setTokenValue(''));
    expect(result.current.form.tokenAction).toBe('replace');
    act(/* 当前回调标记清除。 */ () => result.current.markClearToken());
    expect(result.current.form).toMatchObject({ tokenAction: 'clear', tokenValue: '' });
    act(/* 当前回调撤销。 */ () => result.current.keepToken());
    expect(result.current.form.tokenAction).toBe('retain');
    // 未配置令牌的账号：输入即替换，清空回到保留。
    loadMock.mockResolvedValue({ ...settingsFixture, token_configured: false });
    // fresh 提供未配置令牌场景的 Hook 返回值。
    const fresh = renderHook(/* 当前回调渲染未配置令牌场景。 */ () => useAgentReply('a9'));
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(fresh.result.current.settings?.token_configured).toBe(false));
    act(/* 当前回调输入令牌。 */ () => fresh.result.current.setTokenValue('t'));
    expect(fresh.result.current.form.tokenAction).toBe('replace');
    act(/* 当前回调清空输入。 */ () => fresh.result.current.setTokenValue(''));
    expect(fresh.result.current.form.tokenAction).toBe('retain');
  });

  test('保存先校验，成功后用服务端确认值重置表单并丢弃令牌明文', /* 当前回调执行该行为用例。 */ async () => {
    // result 提供被测 Hook 的最新返回值。
    const { result } = renderHook(/* 当前回调渲染被测 Hook。 */ () => useAgentReply('a1'));
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.settings).not.toBeNull());
    act(/* 当前回调清空地址后启用。 */ () => result.current.updateForm({ endpointUrl: '', enabled: true }));
    await act(/* 当前回调触发保存。 */ async () => { await result.current.save(); });
    expect(result.current.toast).toEqual({ type: 'error', text: '启用前请填写 Agent 地址' });
    expect(saveMock).not.toHaveBeenCalled();
    act(/* 当前回调填写地址并更换令牌。 */ () => { result.current.updateForm({ endpointUrl: ' https://new.example.com ' }); result.current.beginReplaceToken(); });
    await act(/* 当前回调在令牌为空时保存。 */ async () => { await result.current.save(); });
    expect(result.current.toast?.text).toBe('请输入新的访问令牌');
    act(/* 当前回调输入令牌。 */ () => result.current.setTokenValue(' new-token '));
    saveMock.mockResolvedValueOnce({ ...settingsFixture, endpoint_url: 'https://new.example.com' });
    // saved 保存本次保存是否成功。
    let saved = false;
    await act(/* 当前回调保存。 */ async () => { saved = await result.current.save(); });
    expect(saved).toBe(true);
    expect(saveMock).toHaveBeenCalledWith('a1', { enabled: true, endpoint_url: 'https://new.example.com', request_timeout_seconds: 15, max_session_seconds: 120, token_action: 'replace', token_value: 'new-token' });
    expect(result.current.toast).toEqual({ type: 'success', text: '保存成功' });
    expect(result.current.form).toMatchObject({ endpointUrl: 'https://new.example.com', tokenAction: 'retain', tokenValue: '' });
  });

  test('保存失败保留草稿并提示，进行中拒绝重复提交', /* 当前回调执行该行为用例。 */ async () => {
    // result 提供被测 Hook 的最新返回值。
    const { result } = renderHook(/* 当前回调渲染被测 Hook。 */ () => useAgentReply('a1'));
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.settings).not.toBeNull());
    act(/* 当前回调修改地址。 */ () => result.current.updateForm({ endpointUrl: 'https://edit.example.com' }));
    saveMock.mockRejectedValueOnce(new Error('保存失败了'));
    await act(/* 当前回调保存并失败。 */ async () => { await result.current.save(); });
    expect(result.current.toast).toEqual({ type: 'error', text: '保存失败了' });
    expect(result.current.form.endpointUrl).toBe('https://edit.example.com');
    // slow 是故意延迟的保存请求。
    const slow = deferred<AgentReplySettings>();
    saveMock.mockImplementationOnce(/* 当前回调返回由测试控制完成时机的挂起请求。 */ () => slow.promise);
    // first 保存第一次保存的完成结果。
    let first!: Promise<boolean>;
    // second 保存进行中再次提交得到的返回值，应为 false。
    let second!: boolean;
    await act(/* 当前回调连续提交两次。 */ async () => {
      first = result.current.save();
      second = await result.current.save();
    });
    expect(second).toBe(false);
    await act(/* 当前回调完成第一次保存。 */ async () => { slow.resolve(settingsFixture); await first; });
    expect(saveMock).toHaveBeenCalledTimes(2);
  });

  test('保存进行中切换账号：旧结果不串到新账号，新账号可立即保存', /* 当前回调执行该行为用例。 */ async () => {
    // result、rerender 提供 Hook 的最新返回值和重新渲染入口。
    const { result, rerender } = renderHook(/* 当前回调渲染被测 Hook。 */ ({ id }) => useAgentReply(id), { initialProps: { id: 'a1' } });
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.settings?.cookie_id).toBe('a1'));
    // slowSuccess 是账号 a1 故意延迟成功的保存请求。
    const slowSuccess = deferred<AgentReplySettings>();
    saveMock.mockImplementationOnce(/* 当前回调返回由测试控制完成时机的挂起请求。 */ () => slowSuccess.promise);
    // pendingA1 保存账号 a1 在途保存的完成结果。
    let pendingA1!: Promise<boolean>;
    act(/* 当前回调发起 a1 的保存。 */ () => { pendingA1 = result.current.save(); });
    expect(result.current.submitting).toBe(true);
    // a2Settings 是切换后账号 a2 的服务端配置。
    const a2Settings: AgentReplySettings = { ...settingsFixture, cookie_id: 'a2', endpoint_url: 'https://two.example.com' };
    loadMock.mockResolvedValueOnce(a2Settings);
    rerender({ id: 'a2' });
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.settings?.cookie_id).toBe('a2'));
    // a1 的在途保存不能把 a2 标为保存中，也不能挡住 a2 的保存。
    expect(result.current.submitting).toBe(false);
    saveMock.mockResolvedValueOnce(a2Settings);
    // savedA2 保存账号 a2 的保存结果。
    let savedA2 = false;
    await act(/* 当前回调保存 a2。 */ async () => { savedA2 = await result.current.save(); });
    expect(savedA2).toBe(true);
    expect(saveMock).toHaveBeenLastCalledWith('a2', expect.objectContaining({ endpoint_url: 'https://two.example.com' }));
    act(/* 当前回调清除 a2 的成功提示，便于观察 a1 结果是否串入。 */ () => result.current.dismissToast());
    await act(/* 当前回调让 a1 的保存此时成功返回。 */ async () => { slowSuccess.resolve(settingsFixture); await pendingA1; });
    expect(result.current.toast).toBeNull();
    expect(result.current.settings?.cookie_id).toBe('a2');
    expect(result.current.form.endpointUrl).toBe('https://two.example.com');
    // a1 的失败结果同样不能在 a2 页面弹出提示。
    // slowFailure 是账号 a1 故意延迟失败的保存请求。
    const slowFailure = deferred<AgentReplySettings>();
    loadMock.mockResolvedValueOnce(settingsFixture);
    rerender({ id: 'a1' });
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.settings?.cookie_id).toBe('a1'));
    saveMock.mockImplementationOnce(/* 当前回调返回由测试控制完成时机的挂起请求。 */ () => slowFailure.promise);
    act(/* 当前回调再次发起 a1 的保存。 */ () => { pendingA1 = result.current.save(); });
    loadMock.mockResolvedValueOnce(a2Settings);
    rerender({ id: 'a2' });
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.settings?.cookie_id).toBe('a2'));
    await act(/* 当前回调让 a1 的保存此时失败。 */ async () => { slowFailure.reject(new Error('旧账号保存失败')); await pendingA1; });
    expect(result.current.toast).toBeNull();
  });

  test('连接测试：地址必填、结果展示、编辑后清空，过期结果被丢弃', /* 当前回调执行该行为用例。 */ async () => {
    // result、rerender 提供 Hook 的最新返回值和重新渲染入口。
    const { result, rerender } = renderHook(/* 当前回调渲染被测 Hook。 */ ({ id }) => useAgentReply(id), { initialProps: { id: 'a1' } });
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.settings).not.toBeNull());
    act(/* 当前回调清空地址。 */ () => result.current.updateForm({ endpointUrl: '' }));
    await act(/* 当前回调在无地址时测试。 */ async () => { await result.current.test(); });
    expect(result.current.toast).toEqual({ type: 'error', text: '请先填写 Agent 地址' });
    expect(testMock).not.toHaveBeenCalled();
    act(/* 当前回调恢复地址。 */ () => result.current.updateForm({ endpointUrl: 'https://agent.example.com/v1' }));
    await act(/* 当前回调发起测试。 */ async () => { await result.current.test(); });
    expect(result.current.testResult).toEqual(okResult);
    expect(testMock).toHaveBeenCalledWith('a1', expect.objectContaining({ endpoint_url: 'https://agent.example.com/v1', token_action: 'retain' }), expect.objectContaining({ signal: expect.any(AbortSignal) }));
    act(/* 当前回调编辑表单。 */ () => result.current.updateForm({ requestTimeoutSeconds: 20 }));
    expect(result.current.testResult).toBeNull();
    // 测试进行中切换账号：旧结果到达后被丢弃。
    // slow 是故意延迟的测试请求。
    const slow = deferred<AgentReplyTestResult>();
    testMock.mockImplementationOnce(/* 当前回调返回由测试控制完成时机的挂起请求。 */ () => slow.promise);
    // pending 保存进行中的测试 Promise。
    let pending!: Promise<void>;
    act(/* 当前回调发起测试但不等待。 */ () => { pending = result.current.test(); });
    loadMock.mockResolvedValueOnce({ ...settingsFixture, cookie_id: 'a2' });
    rerender({ id: 'a2' });
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.settings?.cookie_id).toBe('a2'));
    await act(/* 当前回调让旧账号测试结果此时到达。 */ async () => { slow.resolve(okResult); await pending; });
    expect(result.current.testResult).toBeNull();
    expect(result.current.testing).toBe(false);
  });

  test('连接测试失败给出提示', /* 当前回调执行该行为用例。 */ async () => {
    testMock.mockRejectedValueOnce(new Error('测试失败了'));
    // result 提供被测 Hook 的最新返回值。
    const { result } = renderHook(/* 当前回调渲染被测 Hook。 */ () => useAgentReply('a1'));
    await waitFor(/* 当前回调轮询等待断言条件成立。 */ () => expect(result.current.settings).not.toBeNull());
    await act(/* 当前回调发起测试。 */ async () => { await result.current.test(); });
    expect(result.current.toast).toEqual({ type: 'error', text: '测试失败了' });
    expect(result.current.testing).toBe(false);
    act(/* 当前回调清除提示。 */ () => result.current.dismissToast());
    expect(result.current.toast).toBeNull();
  });
});

describe('Agent 回复表单纯函数', /* 当前回调覆盖校验、载荷与错误文案的边界。 */ () => {
  // base 是通过校验的表单基线。
  const base = { ...emptyAgentReplyForm(), enabled: true, endpointUrl: 'https://agent.example.com' };

  test('校验覆盖地址、范围、总时长与令牌', /* 当前回调执行该行为用例。 */ () => {
    expect(validateAgentReplyForm(base, false)).toBe('');
    expect(validateAgentReplyForm({ ...base, enabled: false, endpointUrl: '' }, false)).toBe('');
    expect(validateAgentReplyForm({ ...base, endpointUrl: '' }, false)).toBe('启用前请填写 Agent 地址');
    expect(validateAgentReplyForm({ ...base, enabled: false, endpointUrl: '' }, true)).toBe('请先填写 Agent 地址');
    expect(validateAgentReplyForm({ ...base, endpointUrl: 'ftp://x' }, false)).toContain('http://');
    expect(validateAgentReplyForm({ ...base, requestTimeoutSeconds: 4 }, false)).toContain('单次请求超时');
    expect(validateAgentReplyForm({ ...base, requestTimeoutSeconds: 1.5 }, false)).toContain('单次请求超时');
    expect(validateAgentReplyForm({ ...base, maxSessionSeconds: 301 }, false)).toContain('单轮对话总时长');
    expect(validateAgentReplyForm({ ...base, requestTimeoutSeconds: 30, maxSessionSeconds: 20 }, false)).toBe('单轮对话总时长不能小于单次请求超时');
    expect(validateAgentReplyForm({ ...base, tokenAction: 'replace', tokenValue: '  ' }, false)).toBe('请输入新的访问令牌');
  });

  test('载荷裁剪地址，仅替换命令携带令牌', /* 当前回调执行该行为用例。 */ () => {
    expect(agentReplyPayloadFromForm({ ...base, endpointUrl: ' https://a.example.com ', tokenAction: 'replace', tokenValue: ' t ' })).toMatchObject({ endpoint_url: 'https://a.example.com', token_action: 'replace', token_value: 't' });
    expect(agentReplyPayloadFromForm({ ...base, tokenAction: 'clear', tokenValue: 'stale' }).token_value).toBe('');
  });

  test('错误文案回退到通用提示', /* 当前回调执行该行为用例。 */ () => {
    expect(agentReplyErrorMessage(new Error('x'))).toBe('x');
    expect(agentReplyErrorMessage({})).toBe('操作失败');
    expect(agentReplyErrorMessage(undefined)).toBe('操作失败');
  });
});
