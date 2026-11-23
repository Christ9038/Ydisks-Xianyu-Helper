import type React from 'react';
import { useCallback,useEffect,useRef,useState } from 'react';
import type { SystemSettings } from './api';
import { fetchAIModels,getSystemSettings,testAIConnection,updateLoginCredentials,updateSystemSettings,verifySession } from './api';
import { DEFAULT_AI_API_URL,DEFAULT_ANTHROPIC_API_URL } from './constants';
import { aiProtocolOf,buildPersistableSettings,createCredentials,createCredentialsMessage,isCurrentAIConnectionTest,isCurrentSettingsRequest,isSettingsAbortError,settingsErrorMessage,validateCredentials } from './state';
import type { ConnectionTestMessage,CredentialsForm,CredentialsMessage,SettingsFeatureState,SettingsRequestStatus } from './types';

/** Settings feature 的 Hook 返回值。 */
export type UseSettingsResult = SettingsFeatureState & {
  /** 模型选择器 DOM 引用。 */
  modelPickerRef: React.RefObject<HTMLDivElement | null>;
  /** 重新加载系统配置与模型。 */
  loadSettings: () => void;
  /** 加载模型列表。 */
  loadAIModels: (source?: SystemSettings | null, openAfterLoad?: boolean) => void;
  /** 测试 AI 连接。 */
  testConnection: () => void;
  /** 保存系统配置。 */
  handleSave: () => Promise<void>;
  /** 保存登录凭据。 */
  handleCredentialsSave: (event: React.FormEvent) => Promise<void>;
  /** 更新配置草稿。 */
  setSettings: React.Dispatch<React.SetStateAction<SystemSettings | null>>;
  /** 更新模型下拉框状态。 */
  setModelDropdownOpen: React.Dispatch<React.SetStateAction<boolean>>;
  /** 更新敏感字段显示状态。 */
  setShowApiKey: React.Dispatch<React.SetStateAction<boolean>>;
  /** 更新远程秘钥显示状态。 */
  setShowCaptchaSecret: React.Dispatch<React.SetStateAction<boolean>>;
  /** 更新当前密码显示状态。 */
  setShowCurrentPassword: React.Dispatch<React.SetStateAction<boolean>>;
  /** 更新新密码显示状态。 */
  setShowNewPassword: React.Dispatch<React.SetStateAction<boolean>>;
  /** 更新凭据表单。 */
  setCredentials: React.Dispatch<React.SetStateAction<CredentialsForm>>;
  /** 更新凭据提示。 */
  setCredentialsMessage: React.Dispatch<React.SetStateAction<CredentialsMessage>>;
};

/** 管理系统设置、AI 模型和登录凭据的请求与表单状态。 */
export const useSettings = (): UseSettingsResult => {
  // settings 保存当前系统配置草稿。
  const [settings, setSettings] = useState<SystemSettings | null>(null);
  // loading 表示系统配置是否正在加载。
  const [loading, setLoading] = useState(false);
  // loadError 保存系统配置加载失败信息。
  const [loadError, setLoadError] = useState('');
  // saving 表示系统配置是否正在保存。
  const [saving, setSaving] = useState(false);
  // saveError 保存系统配置保存失败信息。
  const [saveError, setSaveError] = useState('');
  // aiModels 保存远端模型发现结果。
  const [aiModels, setAiModels] = useState<string[]>([]);
  // modelsLoading 表示模型发现请求是否进行中。
  const [modelsLoading, setModelsLoading] = useState(false);
  // modelError 保存模型发现失败信息。
  const [modelError, setModelError] = useState('');
  // modelDropdownOpen 表示模型选择下拉框是否展开。
  const [modelDropdownOpen, setModelDropdownOpen] = useState(false);
  // showApiKey 控制 AI API Key 是否明文显示。
  const [showApiKey, setShowApiKey] = useState(false);
  // showCaptchaSecret 控制远程验证秘钥是否明文显示。
  const [showCaptchaSecret, setShowCaptchaSecret] = useState(false);
  // showCurrentPassword 控制当前密码是否明文显示。
  const [showCurrentPassword, setShowCurrentPassword] = useState(false);
  // showNewPassword 控制新密码是否明文显示。
  const [showNewPassword, setShowNewPassword] = useState(false);
  // credentialsSaving 表示登录凭据是否正在保存。
  const [credentialsSaving, setCredentialsSaving] = useState(false);
  // credentialsMessage 保存登录凭据操作提示。
  const [credentialsMessage, setCredentialsMessage] = useState<CredentialsMessage>(null);
  // credentials 保存登录凭据编辑草稿。
  const [credentials, setCredentials] = useState<CredentialsForm>(/* 当前回调处理用户交互或异步状态变化。 */ () => createCredentials());
  // requestStatus 表示最近一次设置请求的阶段。
  const [requestStatus, setRequestStatus] = useState<SettingsRequestStatus>('idle');
  // modelPickerRef 指向模型选择器根节点。
  const modelPickerRef = useRef<HTMLDivElement>(null);
  // settingsRef 保存最新配置供稳定回调读取。
  const settingsRef = useRef<SystemSettings | null>(null);
  // requestSequence 隔离刷新产生的旧响应。
  const requestSequence = useRef(0);
  // requestController 保存当前可取消的设置请求。
  const requestController = useRef<AbortController | null>(null);
  // credentialsRequestSequence 隔离登录凭据保存的旧响应，不与设置加载或保存共用代次。
  const credentialsRequestSequence = useRef(0);
  // credentialsRequestController 保存当前可取消的登录凭据保存请求。
  const credentialsRequestController = useRef<AbortController | null>(null);
  // modelRequestSequence 隔离模型发现的旧响应，防止其覆盖新配置对应的列表。
  const modelRequestSequence = useRef(0);
  // modelRequestController 保存当前模型发现请求的专属取消控制器。
  const modelRequestController = useRef<AbortController | null>(null);
  // connectionTestLoading 表示 AI 连接测试是否进行中。
  const [connectionTestLoading, setConnectionTestLoading] = useState(false);
  // connectionTestMessage 保存 AI 连接测试结果提示。
  const [connectionTestMessage, setConnectionTestMessage] = useState<ConnectionTestMessage>(null);
  // testRequestSequence 隔离连接测试的旧响应。
  const testRequestSequence = useRef(0);
  // testRequestController 保存当前连接测试请求的取消控制器。
  const testRequestController = useRef<AbortController | null>(null);

  // settingsRef 保存最新配置，供稳定的模型加载回调读取。
  settingsRef.current = settings;

  /** 取消当前设置请求并创建新的请求控制器。 */
  const beginRequest = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ () => {
    // controller 是本次请求的取消控制器。
    requestController.current?.abort();
    // controller 请求取消控制器。
    const controller = new AbortController();
    requestController.current = controller;
    requestSequence.current += 1;
    return { controller, sequence: requestSequence.current };
  }, []);

  /** 取消旧的登录凭据保存请求并创建独立的当前请求。 */
  const beginCredentialsRequest = useCallback(/* 当前回调隔离登录凭据保存的取消和晚到响应。 */ () => {
    credentialsRequestController.current?.abort();
    // controller 是本次登录凭据保存的取消控制器。
    const controller = new AbortController();
    credentialsRequestController.current = controller;
    credentialsRequestSequence.current += 1;
    return { controller, sequence: credentialsRequestSequence.current };
  }, []);

  /** 取消旧模型发现并创建当前唯一有效的模型发现请求。 */
  const beginModelRequest = useCallback(/* 当前回调封装模型发现独立代次和取消生命周期。 */ () => {
    modelRequestController.current?.abort();
    // controller 保存本次模型发现请求的独立取消控制器。
    const controller = new AbortController();
    modelRequestController.current = controller;
    modelRequestSequence.current += 1;
    return { controller, sequence: modelRequestSequence.current };
  }, []);

  /** 使当前模型发现请求失效并停止其网络活动。 */
  const cancelModelRequest = useCallback(/* 当前回调用于设置刷新和组件卸载时终止旧模型请求。 */ () => {
    modelRequestSequence.current += 1;
    modelRequestController.current?.abort();
    modelRequestController.current = null;
  }, []);

  // loadAIModels 加载当前数据（AIModels）。
  const loadAIModels = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (source?: SystemSettings | null, openAfterLoad = false) => {
    // current 是本次模型发现使用的配置快照。
    const current = source || settingsRef.current;
    // baseUrl 是兼容模型发现接口的服务地址。
    const baseUrl = current?.ai_api_url || current?.ai_base_url || (aiProtocolOf(current) === 'anthropic' ? DEFAULT_ANTHROPIC_API_URL : DEFAULT_AI_API_URL);
	// request 保存本次模型发现独立的代次和取消控制器。
	const request = beginModelRequest();
    setModelsLoading(true);
    setModelError('');
    try {
      // models 模型列表。
      const models = await fetchAIModels(baseUrl, current?.ai_api_key || '', { signal: request.controller.signal }, aiProtocolOf(current));
      if (!isCurrentSettingsRequest(modelRequestSequence.current, request.sequence, request.controller.signal)) return;
      setAiModels(models);
      setModelDropdownOpen(openAfterLoad && models.length > 0);
      if (!current?.ai_model && models.length > 0) {
        setSettings(/* 当前回调处理用户交互或异步状态变化。 */ previous => previous ? { ...previous, ai_model: models[0] } : previous);
      }
    } catch (/* error 保存模型发现请求的失败原因；取消请求不会进入页面错误状态。 */ error) {
      if (!isCurrentSettingsRequest(modelRequestSequence.current, request.sequence, request.controller.signal) || isSettingsAbortError(error)) return;
      setAiModels([]);
      setModelDropdownOpen(false);
      setModelError(settingsErrorMessage(error, '读取模型失败'));
    } finally {
      if (isCurrentSettingsRequest(modelRequestSequence.current, request.sequence, request.controller.signal)) setModelsLoading(false);
    }
  }, [beginModelRequest]);

  // testConnection 发送一次最小对话请求验证 AI API 可用性。
  const testConnection = useCallback(/* 当前回调由用户点击触发，使用取消器与配置快照拒绝旧测试结果。 */ async () => {
    // current 是用户点击按钮时的配置草稿，后续响应必须与其保持一致才可显示。
    const current = settingsRef.current;
    if (!current || connectionTestLoading) return;
    // snapshot 绑定本次连接测试的端点、密钥和模型，避免编辑后仍展示旧配置结果。
    const baseUrl = current.ai_api_url || current.ai_base_url || (aiProtocolOf(current) === 'anthropic' ? DEFAULT_ANTHROPIC_API_URL : DEFAULT_AI_API_URL);
    // apiKey 是本次连接测试使用的临时或已保存 API Key；它只进入请求作用域。
    const apiKey = current.ai_api_key || '';
    // model 是本次连接测试使用的模型名称，空值交由服务端回退默认模型。
    const model = current.ai_model || '';
    // snapshot 用于在响应到达时确认当前配置没有被编辑。
    const snapshot = { baseURL: baseUrl, apiKey, model, protocol: aiProtocolOf(current) };
    testRequestController.current?.abort();
    // controller 只管理本次连接测试，组件卸载或后续测试会取消它。
    const controller = new AbortController();
    testRequestController.current = controller;
    testRequestSequence.current += 1;
    // sequence 隔离连续点击或卸载后的旧响应。
    const sequence = testRequestSequence.current;
    setConnectionTestLoading(true);
    setConnectionTestMessage(null);
    try {
      // result 是服务端确认实际完成一次 chat completion 后返回的诊断摘要。
      const result = await testAIConnection(baseUrl, apiKey, model, { signal: controller.signal }, snapshot.protocol);
      if (testRequestSequence.current !== sequence || !isCurrentAIConnectionTest(settingsRef.current, snapshot, snapshot.protocol === 'anthropic' ? DEFAULT_ANTHROPIC_API_URL : DEFAULT_AI_API_URL)) return;
      setConnectionTestMessage({ type: 'success', text: `连接正常 · 模型 ${result.model} · 耗时 ${(result.latency_ms / 1000).toFixed(1)}s · 回复：${result.reply}` });
    } catch (/* error 是连接测试的上游、网络或取消错误；过期和主动取消请求不展示提示。 */ error) {
      if (testRequestSequence.current !== sequence || isSettingsAbortError(error)) return;
      setConnectionTestMessage({ type: 'error', text: settingsErrorMessage(error, '连接失败') });
    } finally {
      if (testRequestSequence.current === sequence) setConnectionTestLoading(false);
    }
  }, [connectionTestLoading]);

  // loadSettings 加载当前数据（设置）。
  const loadSettings = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ () => {
    // request 是本次设置读取的代次与控制器。
    const { controller, sequence } = beginRequest();
    // testSequence 使重新读取设置前尚未完成的连接测试结果失效。
    testRequestSequence.current += 1;
    testRequestController.current?.abort();
    testRequestController.current = null;
    setConnectionTestLoading(false);
    setConnectionTestMessage(null);
    cancelModelRequest();
    setLoading(true);
    setRequestStatus('loading');
    setLoadError('');
    Promise.all([
      getSystemSettings({ signal: controller.signal }),
      verifySession({ signal: controller.signal }),
    ]).then(/* 当前回调处理用户交互或异步状态变化。 */ ([data, session]) => {
      if (!isCurrentSettingsRequest(requestSequence.current, sequence, controller.signal)) return;
      setSettings(data);
      if (session.username) setCredentials(/* 当前回调处理用户交互或异步状态变化。 */ previous => ({ ...previous, new_username: session.username || '' }));
      void loadAIModels(data, false);
      setRequestStatus('success');
    }).catch(/* 当前回调处理用户交互或异步状态变化。 */ error => {
      if (!isCurrentSettingsRequest(requestSequence.current, sequence, controller.signal) || isSettingsAbortError(error)) return;
      setSettings(null);
      setLoadError(settingsErrorMessage(error, '加载配置失败'));
      setRequestStatus('error');
    }).finally(/* 当前回调处理用户交互或异步状态变化。 */ () => {
      if (isCurrentSettingsRequest(requestSequence.current, sequence, controller.signal)) setLoading(false);
    });
  }, [beginRequest, cancelModelRequest, loadAIModels]);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    loadSettings();
    return /* 当前回调处理用户交互或异步状态变化。 */ () => {
      requestController.current?.abort();
      credentialsRequestController.current?.abort();
      cancelModelRequest();
      testRequestController.current?.abort();
      testRequestSequence.current += 1;
      testRequestController.current = null;
    };
  }, [cancelModelRequest, loadSettings]);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    // handlePointerDown 负责点击模型选择器外部时关闭下拉框。
    const handlePointerDown = (event: MouseEvent) => {
      if (!modelPickerRef.current?.contains(event.target as Node)) setModelDropdownOpen(false);
    };
    document.addEventListener('mousedown', handlePointerDown);
    return /* 当前回调处理用户交互或异步状态变化。 */ () => document.removeEventListener('mousedown', handlePointerDown);
  }, []);

  // handleSave 处理当前用户操作（Save）。
  const handleSave = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async () => {
    // handleSave 提交当前配置草稿并保护过期响应。
    if (!settings || saving) return;
    // testSequence 使保存前基于旧草稿发出的连接测试不再影响新的已保存配置。
    testRequestSequence.current += 1;
    testRequestController.current?.abort();
    testRequestController.current = null;
    setConnectionTestLoading(false);
    setConnectionTestMessage(null);
    cancelModelRequest();
    setAiModels([]);
    setModelsLoading(false);
    setModelDropdownOpen(false);
    setModelError('');
    // request 是本次保存动作的代次与控制器。
    const { controller, sequence } = beginRequest();
    setSaving(true);
    setSaveError('');
    try {
      await updateSystemSettings(buildPersistableSettings(settings), { signal: controller.signal });
      if (!isCurrentSettingsRequest(requestSequence.current, sequence, controller.signal)) return;
      window.alert('系统配置已保存');
    } catch (/* error 保存系统设置提交请求的失败原因；过期响应不会覆盖当前表单。 */ error) {
      if (!isCurrentSettingsRequest(requestSequence.current, sequence, controller.signal) || isSettingsAbortError(error)) return;
      setSaveError(settingsErrorMessage(error, '保存配置失败'));
    } finally {
      if (isCurrentSettingsRequest(requestSequence.current, sequence, controller.signal)) setSaving(false);
    }
  }, [beginRequest, cancelModelRequest, saving, settings]);

  // handleCredentialsSave 处理当前用户操作（CredentialsSave）。
  const handleCredentialsSave = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (event: React.FormEvent) => {
    // handleCredentialsSave 校验并提交登录凭据表单。
    event.preventDefault();
    setCredentialsMessage(null);
    // validationError 是前端校验得到的第一条可见错误。
    const validationError = validateCredentials(credentials);
    if (validationError) {
      setCredentialsMessage(createCredentialsMessage('error', validationError));
      return;
    }
    // request 是本次凭据保存动作的专属代次与控制器。
    const { controller, sequence } = beginCredentialsRequest();
    setCredentialsSaving(true);
    try {
      // result 是后端返回的凭据更新结果。
      const result = await updateLoginCredentials({
        current_password: credentials.current_password,
        new_username: credentials.new_username.trim(),
        new_password: credentials.new_password || undefined,
      }, { signal: controller.signal });
      if (!isCurrentSettingsRequest(credentialsRequestSequence.current, sequence, controller.signal)) return;
      if (!result.success) {
        setCredentialsMessage(createCredentialsMessage('error', result.message || '登录凭据更新失败'));
        return;
      }
      setCredentialsMessage(createCredentialsMessage('success', result.message || '登录凭据已更新'));
      window.setTimeout(/* 当前回调处理用户交互或异步状态变化。 */ () => window.location.reload(), 1400);
    } catch (/* error 保存登录凭据提交请求的失败原因；不写入日志或持久化状态。 */ error) {
      if (!isCurrentSettingsRequest(credentialsRequestSequence.current, sequence, controller.signal) || isSettingsAbortError(error)) return;
      setCredentialsMessage(createCredentialsMessage('error', settingsErrorMessage(error, '登录凭据更新失败')));
    } finally {
      if (isCurrentSettingsRequest(credentialsRequestSequence.current, sequence, controller.signal)) setCredentialsSaving(false);
    }
  }, [beginCredentialsRequest, credentials]);

  return {
    settings, loading, loadError, saving, saveError, aiModels, modelsLoading, modelError, modelDropdownOpen,
    showApiKey, showCaptchaSecret, showCurrentPassword, showNewPassword, credentialsSaving, credentialsMessage,
    credentials, requestStatus, modelPickerRef, loadSettings, loadAIModels: /* source 是可选设置快照；openAfterLoad 指定成功后是否展开下拉列表。 */ (source, openAfterLoad) => void loadAIModels(source, openAfterLoad),
    testConnection, connectionTestLoading, connectionTestMessage, handleSave, handleCredentialsSave,
    setSettings, setModelDropdownOpen, setShowApiKey, setShowCaptchaSecret,
    setShowCurrentPassword, setShowNewPassword, setCredentials, setCredentialsMessage,
  };
};
