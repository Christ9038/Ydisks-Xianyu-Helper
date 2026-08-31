import type React from 'react';
import { useCallback,useEffect,useRef,useState } from 'react';
import type { SystemSettings } from './api';
import { fetchAIModels,getSystemSettings,updateLoginCredentials,updateSystemSettings,verifySession } from './api';
import { DEFAULT_AI_API_URL } from './constants';
import { buildPersistableSettings,createCredentials,createCredentialsMessage,isCurrentSettingsRequest,isSettingsAbortError,settingsErrorMessage,validateCredentials } from './state';
import type { CredentialsForm,CredentialsMessage,SettingsFeatureState,SettingsRequestStatus } from './types';

/** Settings feature 的 Hook 返回值。 */
export type UseSettingsResult = SettingsFeatureState & {
  /** 模型选择器 DOM 引用。 */
  modelPickerRef: React.RefObject<HTMLDivElement | null>;
  /** 重新加载系统配置与模型。 */
  loadSettings: () => void;
  /** 加载模型列表。 */
  loadAIModels: (source?: SystemSettings | null, openAfterLoad?: boolean) => void;
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
  // modelRequestSequence 隔离模型发现的旧响应，防止其覆盖新配置对应的列表。
  const modelRequestSequence = useRef(0);
  // modelRequestController 保存当前模型发现请求的专属取消控制器。
  const modelRequestController = useRef<AbortController | null>(null);

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
    const baseUrl = current?.ai_api_url || current?.ai_base_url || DEFAULT_AI_API_URL;
	// request 保存本次模型发现独立的代次和取消控制器。
	const request = beginModelRequest();
    setModelsLoading(true);
    setModelError('');
    try {
      // models 模型列表。
      const models = await fetchAIModels(baseUrl, current?.ai_api_key || '', { signal: request.controller.signal });
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

  // loadSettings 加载当前数据（设置）。
  const loadSettings = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ () => {
    // request 是本次设置读取的代次与控制器。
    const { controller, sequence } = beginRequest();
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
    return /* 当前回调处理用户交互或异步状态变化。 */ () => { requestController.current?.abort(); cancelModelRequest(); };
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
    // request 是本次凭据保存动作的代次与控制器。
    const { controller, sequence } = beginRequest();
    setCredentialsSaving(true);
    try {
      // result 是后端返回的凭据更新结果。
      const result = await updateLoginCredentials({
        current_password: credentials.current_password,
        new_username: credentials.new_username.trim(),
        new_password: credentials.new_password || undefined,
      }, { signal: controller.signal });
      if (!isCurrentSettingsRequest(requestSequence.current, sequence, controller.signal)) return;
      if (!result.success) {
        setCredentialsMessage(createCredentialsMessage('error', result.message || '登录凭据更新失败'));
        return;
      }
      setCredentialsMessage(createCredentialsMessage('success', result.message || '登录凭据已更新'));
      window.setTimeout(/* 当前回调处理用户交互或异步状态变化。 */ () => window.location.reload(), 1400);
    } catch (/* error 保存登录凭据提交请求的失败原因；不写入日志或持久化状态。 */ error) {
      if (!isCurrentSettingsRequest(requestSequence.current, sequence, controller.signal) || isSettingsAbortError(error)) return;
      setCredentialsMessage(createCredentialsMessage('error', settingsErrorMessage(error, '登录凭据更新失败')));
    } finally {
      if (isCurrentSettingsRequest(requestSequence.current, sequence, controller.signal)) setCredentialsSaving(false);
    }
  }, [beginRequest, credentials]);

  return {
    settings, loading, loadError, saving, saveError, aiModels, modelsLoading, modelError, modelDropdownOpen,
    showApiKey, showCaptchaSecret, showCurrentPassword, showNewPassword, credentialsSaving, credentialsMessage,
    credentials, requestStatus, modelPickerRef, loadSettings, loadAIModels: /* 当前回调处理用户交互或异步状态变化。 */ (source, openAfterLoad) => void loadAIModels(source, openAfterLoad),
    handleSave, handleCredentialsSave, setSettings, setModelDropdownOpen, setShowApiKey, setShowCaptchaSecret,
    setShowCurrentPassword, setShowNewPassword, setCredentials, setCredentialsMessage,
  };
};
