import { useCallback, useEffect, useRef, useState } from 'react';
import { getAgentReplySettings, saveAgentReplySettings, testAgentReplyConnection } from './api';
import type { AgentReplyPayload, AgentReplySettings, AgentReplyTestResult, AgentReplyTokenAction } from './api';
import { isCurrentRequest, nextRequestGeneration } from './interactionState';

// AGENT_REQUEST_TIMEOUT_MIN、AGENT_REQUEST_TIMEOUT_MAX 是单次 Agent 请求超时允许的秒数区间，与后端校验一致。
export const AGENT_REQUEST_TIMEOUT_MIN = 5;
export const AGENT_REQUEST_TIMEOUT_MAX = 30;
// AGENT_SESSION_MIN、AGENT_SESSION_MAX 是单轮对话总时长允许的秒数区间，与后端校验一致。
export const AGENT_SESSION_MIN = 10;
export const AGENT_SESSION_MAX = 300;

// AgentReplyForm 是配置表单的草稿状态；tokenValue 是敏感值，只存在于内存，保存、切换账号或卸载后即丢弃。
export interface AgentReplyForm {
  // enabled 表示是否启用 Agent 回复。
  enabled: boolean;
  // endpointUrl 是外部 Agent 地址。
  endpointUrl: string;
  // requestTimeoutSeconds 是单次请求超时秒数。
  requestTimeoutSeconds: number;
  // maxSessionSeconds 是单轮对话总时长上限秒数。
  maxSessionSeconds: number;
  // tokenAction 是访问令牌写入命令。
  tokenAction: AgentReplyTokenAction;
  // tokenValue 是 tokenAction 为 replace 时用户输入的新令牌明文。
  tokenValue: string;
}

// AgentReplyToast 是一次操作的短提示，文案不包含任何凭证。
export interface AgentReplyToast {
  // type 决定提示的语义颜色。
  type: 'success' | 'error';
  // text 是提示文案。
  text: string;
}

// emptyAgentReplyForm 是尚未加载或未选择账号时的关闭状态表单。
export const emptyAgentReplyForm = (): AgentReplyForm => ({
  enabled: false, endpointUrl: '', requestTimeoutSeconds: 15, maxSessionSeconds: 120, tokenAction: 'retain', tokenValue: '',
});

// agentReplyFormFromSettings 由服务端配置生成表单草稿；令牌命令回到 retain，不携带任何令牌明文。
export const agentReplyFormFromSettings = (settings: AgentReplySettings): AgentReplyForm => ({
  enabled: settings.enabled,
  endpointUrl: settings.endpoint_url,
  requestTimeoutSeconds: settings.request_timeout_seconds,
  maxSessionSeconds: settings.max_session_seconds,
  tokenAction: 'retain',
  tokenValue: '',
});

// agentReplyPayloadFromForm 把表单草稿转换为接口载荷，地址首尾空白会被裁剪。
export const agentReplyPayloadFromForm = (form: AgentReplyForm): AgentReplyPayload => ({
  enabled: form.enabled,
  endpoint_url: form.endpointUrl.trim(),
  request_timeout_seconds: form.requestTimeoutSeconds,
  max_session_seconds: form.maxSessionSeconds,
  token_action: form.tokenAction,
  token_value: form.tokenAction === 'replace' ? form.tokenValue.trim() : '',
});

// validateAgentReplyForm 在提交前校验表单并返回第一条问题；空串表示通过。
// requireEndpoint 为 true 时（测试连接）地址必填，即使未启用。
export const validateAgentReplyForm = (form: AgentReplyForm, requireEndpoint: boolean): string => {
  // endpoint 是裁剪后的地址，用于必填和格式判断。
  const endpoint = form.endpointUrl.trim();
  if ((form.enabled || requireEndpoint) && !endpoint) return requireEndpoint ? '请先填写 Agent 地址' : '启用前请填写 Agent 地址';
  if (endpoint && !/^https?:\/\/\S+$/i.test(endpoint)) return 'Agent 地址需以 http:// 或 https:// 开头';
  if (!Number.isInteger(form.requestTimeoutSeconds) || form.requestTimeoutSeconds < AGENT_REQUEST_TIMEOUT_MIN || form.requestTimeoutSeconds > AGENT_REQUEST_TIMEOUT_MAX) {
    return `单次请求超时需在 ${AGENT_REQUEST_TIMEOUT_MIN} 到 ${AGENT_REQUEST_TIMEOUT_MAX} 秒之间`;
  }
  if (!Number.isInteger(form.maxSessionSeconds) || form.maxSessionSeconds < AGENT_SESSION_MIN || form.maxSessionSeconds > AGENT_SESSION_MAX) {
    return `单轮对话总时长需在 ${AGENT_SESSION_MIN} 到 ${AGENT_SESSION_MAX} 秒之间`;
  }
  if (form.maxSessionSeconds < form.requestTimeoutSeconds) return '单轮对话总时长不能小于单次请求超时';
  if (form.tokenAction === 'replace' && !form.tokenValue.trim()) return '请输入新的访问令牌';
  return '';
};

// AgentReplyErrorLike 描述请求失败时可能携带的统一错误信封字段。
interface AgentReplyErrorLike {
  // message 是服务端返回的用户可读错误消息。
  message?: string;
}

// agentReplyErrorMessage 把请求失败转换为用户可读文案，不回显请求内容。
export const agentReplyErrorMessage = (error: unknown): string => {
  // message 是统一错误信封中的错误消息，缺失时回退到通用文案。
  const { message } = (error ?? {}) as AgentReplyErrorLike;
  return message || '操作失败';
};

// useAgentReply 管理账号 Agent 回复配置的加载、编辑、保存和连接测试。
// accountID 为空表示尚未选择账号；加载与测试请求都带最新代次和取消信号，账号切换或卸载后到达的响应不会覆盖当前状态。
export const useAgentReply = (accountID: string) => {
  // settings 是服务端最近一次确认的配置（不含令牌）；null 表示尚未加载。
  const [settings, setSettings] = useState<AgentReplySettings | null>(null);
  // form 是正在编辑的表单草稿。
  const [form, setForm] = useState<AgentReplyForm>(emptyAgentReplyForm);
  // loading 表示配置读取是否在途；loadError 保存最近一次读取失败文案。
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState('');
  // savingAccounts 是保存请求仍在途的账号集合；按账号区分，切换账号后旧账号的在途保存不会锁住新账号的保存按钮。
  const [savingAccounts, setSavingAccounts] = useState<ReadonlySet<string>>(() => new Set());
  // testing 阻断重复测试。
  const [testing, setTesting] = useState(false);
  // testResult 是最近一次连接测试结果，编辑表单或切换账号后清空。
  const [testResult, setTestResult] = useState<AgentReplyTestResult | null>(null);
  // toast 是最近一次操作反馈。
  const [toast, setToast] = useState<AgentReplyToast | null>(null);
  // loadGeneration、testGeneration 保存两类请求的最新代次，用于丢弃过期响应。
  const loadGeneration = useRef(0);
  const testGeneration = useRef(0);
  // accountRef 保存最新账号，供异步回调判断响应是否仍属于当前账号。
  const accountRef = useRef(accountID);
  accountRef.current = accountID;
  // savingRef 同步记录保存在途的账号集合，用于在同一账号上即时拒绝重复提交，不依赖异步的 React 状态。
  const savingRef = useRef<Set<string>>(new Set());
  // loadAbort、testAbort 取消被新请求取代的在途请求。
  const loadAbort = useRef<AbortController | null>(null);
  const testAbort = useRef<AbortController | null>(null);

  // reload 读取当前账号的配置；没有账号时清空状态。
  const reload = useCallback(/* 当前回调按最新代次加载 Agent 配置。 */ async () => {
    // generation、key 标识本次读取及其账号。
    const generation = nextRequestGeneration(loadGeneration.current);
    loadGeneration.current = generation;
    // key 是发起读取时的账号，响应返回时用于核对是否仍然有效。
    const key = accountRef.current;
    loadAbort.current?.abort();
    testGeneration.current = nextRequestGeneration(testGeneration.current);
    testAbort.current?.abort();
    setTestResult(null);
    setTesting(false);
    if (!key) {
      setSettings(null);
      setForm(emptyAgentReplyForm());
      setLoadError('');
      setLoading(false);
      return;
    }
    // controller 用于在新请求或卸载时取消本次读取。
    const controller = new AbortController();
    loadAbort.current = controller;
    setLoading(true);
    try {
      // next 是服务端返回的配置。
      const next = await getAgentReplySettings(key, { signal: controller.signal });
      if (!isCurrentRequest(generation, loadGeneration.current, key, accountRef.current)) return;
      setSettings(next);
      setForm(agentReplyFormFromSettings(next));
      setLoadError('');
    } catch (/* error 表示配置读取失败的原因。 */ error) {
      if (!isCurrentRequest(generation, loadGeneration.current, key, accountRef.current)) return;
      setLoadError(agentReplyErrorMessage(error));
    } finally {
      if (generation === loadGeneration.current) setLoading(false);
    }
  }, []);

  useEffect(/* 当前副作用在账号变化时重新加载配置，并在卸载时取消全部在途请求。 */ () => {
    void reload();
    return /* 当前回调使卸载后到达的响应失效并取消请求。 */ () => {
      loadGeneration.current = nextRequestGeneration(loadGeneration.current);
      testGeneration.current = nextRequestGeneration(testGeneration.current);
      loadAbort.current?.abort();
      testAbort.current?.abort();
    };
  }, [accountID, reload]);

  // updateForm 合并表单字段；编辑后旧的测试结果不再代表当前配置，因此一并清空。
  const updateForm = useCallback(/* 当前回调以函数式更新合并表单字段。 */ (patch: Partial<AgentReplyForm>) => {
    setForm(/* current 是更新前的表单草稿。 */ current => ({ ...current, ...patch }));
    setTestResult(null);
  }, []);

  // setTokenValue 处理令牌输入：已配置令牌时输入视为替换；未配置时清空输入回到保留（即无令牌）。
  const setTokenValue = useCallback(/* 当前回调根据输入内容决定令牌写入命令。 */ (value: string) => {
    setForm(/* current 是更新前的表单草稿。 */ current => ({
      ...current,
      tokenAction: value === '' && !settings?.token_configured ? 'retain' : 'replace',
      tokenValue: value,
    }));
    setTestResult(null);
  }, [settings?.token_configured]);

  // beginReplaceToken 进入“更换令牌”输入状态。
  const beginReplaceToken = useCallback(/* 当前回调切换到替换命令并清空输入。 */ () => updateForm({ tokenAction: 'replace', tokenValue: '' }), [updateForm]);
  // markClearToken 标记保存时清除已保存令牌。
  const markClearToken = useCallback(/* 当前回调切换到清除命令并丢弃输入。 */ () => updateForm({ tokenAction: 'clear', tokenValue: '' }), [updateForm]);
  // keepToken 撤销更换或清除，保留已保存令牌。
  const keepToken = useCallback(/* 当前回调回到保留命令并丢弃输入。 */ () => updateForm({ tokenAction: 'retain', tokenValue: '' }), [updateForm]);

  // save 校验并保存配置；成功后用服务端确认值重置表单，令牌明文随之丢弃。
  // 保存期间切换账号时，旧账号的结果仍会落库，但不会改写新账号的表单，也不会在新账号页面弹出提示。
  const save = useCallback(/* 当前回调校验表单后保存配置。 */ async (): Promise<boolean> => {
    if (!accountID || savingRef.current.has(accountID)) return false;
    // problem 是表单校验发现的第一个问题。
    const problem = validateAgentReplyForm(form, false);
    if (problem) {
      setToast({ type: 'error', text: problem });
      return false;
    }
    // key 是发起保存时的账号，响应返回时用于核对是否仍然有效。
    const key = accountID;
    savingRef.current.add(key);
    setSavingAccounts(/* current 是更新前的在途保存账号集合。 */ current => new Set(current).add(key));
    try {
      // saved 是服务端确认的配置。
      const saved = await saveAgentReplySettings(key, agentReplyPayloadFromForm(form));
      if (accountRef.current === key) {
        setSettings(saved);
        setForm(agentReplyFormFromSettings(saved));
        setTestResult(null);
        setToast({ type: 'success', text: '保存成功' });
      }
      return true;
    } catch (/* error 表示保存失败的原因。 */ error) {
      if (accountRef.current === key) setToast({ type: 'error', text: agentReplyErrorMessage(error) });
      return false;
    } finally {
      savingRef.current.delete(key);
      setSavingAccounts(/* current 是更新前的在途保存账号集合。 */ current => {
        // next 是移除本次账号后的在途保存账号集合。
        const next = new Set(current);
        next.delete(key);
        return next;
      });
    }
  }, [accountID, form]);

  // submitting 表示当前选中账号是否有保存在途；由在途集合派生，不单独存状态。
  const submitting = accountID !== '' && savingAccounts.has(accountID);

  // test 用当前表单草稿向 Agent 发送一次 ping；新的测试会取消在途测试，过期结果被丢弃。
  const test = useCallback(/* 当前回调校验表单后发起连接测试。 */ async () => {
    if (!accountID) return;
    // problem 是表单校验发现的第一个问题；测试要求地址必填。
    const problem = validateAgentReplyForm(form, true);
    if (problem) {
      setToast({ type: 'error', text: problem });
      return;
    }
    // generation、key 标识本次测试及其账号。
    const generation = nextRequestGeneration(testGeneration.current);
    testGeneration.current = generation;
    testAbort.current?.abort();
    // controller 用于取消本次测试。
    const controller = new AbortController();
    testAbort.current = controller;
    // key 是发起测试时的账号，响应返回时用于核对是否仍然有效。
    const key = accountID;
    setTesting(true);
    setTestResult(null);
    try {
      // result 是 Agent 的诊断结果。
      const result = await testAgentReplyConnection(key, agentReplyPayloadFromForm(form), { signal: controller.signal });
      if (!isCurrentRequest(generation, testGeneration.current, key, accountRef.current)) return;
      setTestResult(result);
    } catch (/* error 表示测试请求失败的原因。 */ error) {
      if (!isCurrentRequest(generation, testGeneration.current, key, accountRef.current)) return;
      setToast({ type: 'error', text: agentReplyErrorMessage(error) });
    } finally {
      if (generation === testGeneration.current) setTesting(false);
    }
  }, [accountID, form]);

  // dismissToast 清除提示。
  const dismissToast = useCallback(/* 当前回调清除提示。 */ () => setToast(null), []);

  return { settings, form, loading, loadError, submitting, testing, testResult, toast, dismissToast, reload, updateForm, setTokenValue, beginReplaceToken, markClearToken, keepToken, save, test };
};
