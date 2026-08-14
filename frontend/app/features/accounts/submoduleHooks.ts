import { useCallback, useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react';
import type { AccountDetail, AIReplySettings, NotificationChannel } from '../../../types';
import { cancelPasswordLogin, checkPasswordLoginStatus, getAccountAISettings, getAccountBindings, getLongLoginSettings, getNotificationChannels, passwordLogin, setLongLoginSettings, updateAccountAISettings, updateAccountPauseDuration, updateAccountSettings } from './api';
import { buildAccountLoginInfoUpdate, isCurrentAccountRequest, passwordLoginViewFromStatus, shouldUpdateAccountPause } from './state';
import type { AccountEditForm, LongLoginState, PasswordLoginView } from './types';
import { shouldSaveNotificationBindings } from '../../../components/accountBindings';

/** AccountList 当前编辑弹窗类型。 */
export type AccountModalType = 'edit' | 'ai-settings' | null;

/** AccountList 子模块 Hook 的输入。 */
export type AccountSubmoduleOptions = {
  /** 当前编辑账号。 */
  editingAccount: AccountDetail | null;
  /** 写入当前编辑账号。 */
  setEditingAccount: Dispatch<SetStateAction<AccountDetail | null>>;
  /** 当前编辑弹窗类型。 */
  setActiveModal: Dispatch<SetStateAction<AccountModalType>>;
  /** 当前编辑表单。 */
  editForm: AccountEditForm;
  /** 更新编辑表单。 */
  setEditForm: Dispatch<SetStateAction<AccountEditForm>>;
  /** 刷新账号列表。 */
  loadAccounts: () => Promise<void>;
};

/** 账号编辑、AI、通知绑定和密码登录子模块共享状态。 */
export const useAccountSubmodules = ({ editingAccount, setEditingAccount, setActiveModal, editForm, setEditForm, loadAccounts }: AccountSubmoduleOptions) => {
  // longLogin 保存官方长登录设置状态。
  const [longLogin, setLongLogin] = useState<LongLoginState>({ loading: false, saving: false, canOpen: false, enabled: false, error: '' });
  // notifChannels 保存可供账号绑定的通知渠道。
  const [notifChannels, setNotifChannels] = useState<NotificationChannel[]>([]);
  // selectedChannelIds 保存当前选中的通知渠道。
  const [selectedChannelIds, setSelectedChannelIds] = useState<number[]>([]);
  // bindingsLoaded 表示绑定关系是否成功加载。
  const [bindingsLoaded, setBindingsLoaded] = useState(false);
  // bindingsLoading 表示通知绑定请求是否进行中。
  const [bindingsLoading, setBindingsLoading] = useState(false);
  // bindingsDirty 表示用户是否修改过通知绑定。
  const [bindingsDirty, setBindingsDirty] = useState(false);
  // bindingsLoadError 保存通知绑定加载错误。
  const [bindingsLoadError, setBindingsLoadError] = useState('');
  // aiSettings 保存账号 AI 编辑草稿。
  const [aiSettings, setAiSettings] = useState<AIReplySettings>({ ai_enabled: false, max_discount_percent: 10, max_discount_amount: 100, max_bargain_rounds: 3, custom_prompts: '' });
  // saving 表示编辑、AI 或暂停动作是否正在保存。
  const [saving, setSaving] = useState(false);
  // passwordLoginView 保存密码登录刷新授权状态。
  const [passwordLoginView, setPasswordLoginView] = useState<PasswordLoginView>({ sessionId: '', status: 'idle', message: '', qrCodeUrl: '' });
  // bindingsSequence 隔离账号切换后的绑定响应。
  const bindingsSequence = useRef(0);
  // bindingsAbort 保存通知绑定请求控制器。
  const bindingsAbort = useRef<AbortController | null>(null);
  // longLoginSequence 隔离账号切换后的长登录响应。
  const longLoginSequence = useRef(0);
  // longLoginAbort 保存长登录状态读取控制器。
  const longLoginAbort = useRef<AbortController | null>(null);
  // aiSequence 隔离账号切换后的 AI 响应。
  const aiSequence = useRef(0);
  // aiAbort 保存 AI 查询控制器。
  const aiAbort = useRef<AbortController | null>(null);
  // passwordGeneration 隔离密码登录轮询响应。
  const passwordGeneration = useRef(0);
  // passwordAccount 保存密码登录所属账号。
  const passwordAccount = useRef('');
  // passwordAbort 保存密码登录状态查询控制器。
  const passwordAbort = useRef<AbortController | null>(null);
  // passwordTimer 保存下一次密码登录轮询定时器。
  const passwordTimer = useRef<number | null>(null);

  /** 清理密码登录轮询定时器。 */
  // clearPasswordTimerCallback 是 React 稳定的定时器清理函数。
  const clearPasswordTimer = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ () => {
    if (passwordTimer.current !== null) {
      window.clearTimeout(passwordTimer.current);
      passwordTimer.current = null;
    }
  }, []);

  /** 加载当前账号通知渠道和绑定关系，并隔离过期响应。 */
  // loadNotificationBindingsCallback 是通知绑定加载回调。
  const loadNotificationBindings = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (accountId: string): Promise<void> => {
    // sequence 是通知绑定请求的递增代次。
    const sequence = ++bindingsSequence.current;
    bindingsAbort.current?.abort();
    // controller 控制当前通知绑定请求的取消。
    const controller = new AbortController();
    bindingsAbort.current = controller;
    setBindingsLoading(true);
    setBindingsLoaded(false);
    setBindingsDirty(false);
    setBindingsLoadError('');
    // bindingResults 保存通知渠道和账号绑定的并行结果。
    const [channelsResult, bindingsResult] = await Promise.allSettled([
      getNotificationChannels({ signal: controller.signal }),
      getAccountBindings(accountId, { signal: controller.signal }),
    ]);
    if (!isCurrentAccountRequest(sequence, bindingsSequence.current, accountId, accountId)) return;
    if (channelsResult.status === 'fulfilled') setNotifChannels(channelsResult.value.data || []);
    else { setNotifChannels([]); setBindingsLoadError('通知渠道列表加载失败，请重试'); }
    if (bindingsResult.status === 'fulfilled') { setSelectedChannelIds(bindingsResult.value || []); setBindingsLoaded(true); }
    else { setSelectedChannelIds([]); setBindingsLoadError('通知绑定加载失败；本次保存不会修改现有绑定'); }
    setBindingsLoading(false);
  }, []);

  /** 切换一个通知渠道绑定。 */
  // toggleNotificationChannelCallback 是通知渠道切换回调。
  const toggleNotificationChannel = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ (channelId: number) => {
    setSelectedChannelIds(/* 当前回调处理集合中的单个元素。 */ current => current.includes(channelId) ? current.filter(/* 当前回调处理集合中的单个元素。 */ id => id !== channelId) : [...current, channelId]);
    setBindingsDirty(true);
  }, []);

  /** 打开账号编辑弹窗并并行读取绑定和长登录状态。 */
  // openEditModalCallback 是账号编辑弹窗打开回调。
  const openEditModal = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (account: AccountDetail): Promise<void> => {
    // longLoginRequest 是当前长登录读取请求代次。
    const longLoginRequest = ++longLoginSequence.current;
    longLoginAbort.current?.abort();
    // longLoginController 控制当前长登录读取请求的取消。
    const longLoginController = new AbortController();
    longLoginAbort.current = longLoginController;
    passwordGeneration.current += 1;
    passwordAccount.current = account.id;
    passwordAbort.current?.abort();
    clearPasswordTimer();
    setPasswordLoginView({ sessionId: '', status: 'idle', message: '', qrCodeUrl: '' });
    setEditingAccount(account);
    setEditForm({ remark: account.remark || '', cookie: account.value || '', auto_confirm: account.auto_confirm || false, pause_duration: account.pause_duration || 0, username: account.username || '', login_password: account.login_password || '', show_browser: account.show_browser || false, showLoginPassword: false, clear_password: false });
    setActiveModal('edit');
    setLongLogin({ loading: true, saving: false, canOpen: false, enabled: false, error: '' });
    // longLoginResult 保存长登录设置读取结果。
    const [, longLoginResult] = await Promise.allSettled([loadNotificationBindings(account.id), getLongLoginSettings(account.id, { signal: longLoginController.signal })]);
    if (longLoginRequest !== longLoginSequence.current) return;
    if (longLoginResult.status === 'fulfilled') setLongLogin({ loading: false, saving: false, canOpen: longLoginResult.value.can_open_long_login, enabled: longLoginResult.value.enabled, error: '' });
    else setLongLogin({ loading: false, saving: false, canOpen: false, enabled: false, error: '无法读取闲鱼保存登录信息状态' });
  }, [clearPasswordTimer, loadNotificationBindings, setActiveModal, setEditForm, setEditingAccount]);

  /** 切换官方长登录设置。 */
  // handleLongLoginToggleCallback 是长登录设置切换回调。
  const handleLongLoginToggle = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (): Promise<void> => {
    if (!editingAccount || longLogin.loading || longLogin.saving || !longLogin.canOpen) return;
    // enabled 是本次准备提交的长登录开关值。
    const enabled = !longLogin.enabled;
    setLongLogin(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, saving: true, error: '' }));
    try {
      // result 保存长登录设置接口返回值。
      const result = await setLongLoginSettings(editingAccount.id, enabled);
      setLongLogin({ loading: false, saving: false, canOpen: result.can_open_long_login, enabled: result.enabled, error: '' });
    } catch (/* error 表示错误。 */ error) {
      // error 保存长登录设置保存失败原因。
      setLongLogin(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, saving: false, error: error instanceof Error ? error.message : '保存登录信息设置失败' }));
    }
  }, [editingAccount, longLogin]);

  /** 打开 AI 设置并隔离过期账号响应。 */
  // openAIModalCallback 是 AI 设置弹窗打开回调。
  const openAIModal = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (account: AccountDetail): Promise<void> => {
    // sequence 是 AI 设置请求的递增代次。
    const sequence = ++aiSequence.current;
    aiAbort.current?.abort();
    // controller 控制当前 AI 设置请求的取消。
    const controller = new AbortController();
    aiAbort.current = controller;
    setEditingAccount(account);
    setActiveModal('ai-settings');
    setSaving(true);
    try {
      // settings 保存当前账号的 AI 设置。
      const settings = await getAccountAISettings(account.id, { signal: controller.signal });
      if (!isCurrentAccountRequest(sequence, aiSequence.current, account.id, account.id)) return;
      setAiSettings({ ai_enabled: settings.ai_enabled ?? false, max_discount_percent: settings.max_discount_percent ?? 10, max_discount_amount: settings.max_discount_amount ?? 100, max_bargain_rounds: settings.max_bargain_rounds ?? 3, custom_prompts: settings.custom_prompts ?? '' });
    } catch (/* error 表示错误。 */ error) {
      // error 保存 AI 设置读取失败原因。
      if (isCurrentAccountRequest(sequence, aiSequence.current, account.id, account.id)) console.error('加载 AI 设置失败:', error);
    } finally {
      if (isCurrentAccountRequest(sequence, aiSequence.current, account.id, account.id)) setSaving(false);
    }
  }, [setActiveModal, setEditingAccount]);

  /** 保存 AI 设置并刷新账号列表。 */
  // handleSaveAISettingsCallback 是 AI 设置保存回调。
  const handleSaveAISettings = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (): Promise<void> => {
    if (!editingAccount || saving) return;
    setSaving(true);
    try {
      await updateAccountAISettings(editingAccount.id, aiSettings);
      setActiveModal(null);
      await loadAccounts();
    } catch (/* error 表示错误。 */ error) {
      // error 保存 AI 设置保存失败原因。
      console.error('更新 AI 设置失败:', error);
    } finally { setSaving(false); }
  }, [aiSettings, editingAccount, loadAccounts, saving, setActiveModal]);

  /** 保存账号编辑表单和通知绑定。 */
  // handleSaveEditCallback 是账号编辑表单保存回调。
  const handleSaveEdit = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (): Promise<void> => {
    if (!editingAccount || saving) return;
    setSaving(true);
    try {
      // payload 保存需要提交的账号编辑补丁。
      const payload: Parameters<typeof updateAccountSettings>[1] = {};
      if (editForm.remark !== (editingAccount.remark || '')) payload.remark = editForm.remark;
      if (editForm.cookie && editForm.cookie !== (editingAccount.value || '')) payload.cookie = editForm.cookie;
      if (editForm.auto_confirm !== editingAccount.auto_confirm) payload.auto_confirm = editForm.auto_confirm;
      if (shouldUpdateAccountPause(editForm.pause_duration, editingAccount)) payload.pause_duration = editForm.pause_duration;
      // loginInfo 保存登录字段变更补丁。
      const loginInfo = buildAccountLoginInfoUpdate(editingAccount, editForm);
      if (loginInfo) Object.assign(payload, loginInfo);
      if (shouldSaveNotificationBindings(bindingsLoaded, bindingsDirty)) payload.channel_ids = selectedChannelIds;
      if (Object.keys(payload).length > 0) await updateAccountSettings(editingAccount.id, payload);
      setActiveModal(null);
      await loadAccounts();
    } catch (/* error 表示错误。 */ error) {
      // error 保存账号编辑保存失败原因。
      console.error('更新账号失败:', error);
    } finally { setSaving(false); }
  }, [bindingsDirty, bindingsLoaded, editForm, editingAccount, loadAccounts, selectedChannelIds, saving, setActiveModal]);

  /** 按当前暂停时长重新暂停账号。 */
  // handleRestartPauseCallback 是重新暂停账号回调。
  const handleRestartPause = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (): Promise<void> => {
    if (!editingAccount || editForm.pause_duration <= 0 || saving) return;
    setSaving(true);
    try {
      // result 保存重新暂停接口返回的账号状态。
      const result = await updateAccountPauseDuration(editingAccount.id, editForm.pause_duration);
      setEditingAccount(/* 当前回调处理用户交互或异步状态变化。 */ current => current ? { ...current, pause_duration: editForm.pause_duration, paused: result?.paused === true, paused_until: Number(result?.paused_until || 0) } : current);
      await loadAccounts();
    } catch (/* error 表示错误。 */ error) { console.error('重新暂停失败:', error); }
    finally { setSaving(false); }
  }, [editForm.pause_duration, editingAccount, loadAccounts, saving, setEditingAccount]);

  /** 按账号和请求代次轮询密码登录状态。 */
  // pollPasswordLoginCallback 是密码登录状态轮询回调。
  const pollPasswordLogin = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (sessionId: string, generation: number, accountId: string): Promise<void> => {
    passwordAbort.current?.abort();
    // controller 控制当前密码登录状态查询的取消。
    const controller = new AbortController();
    passwordAbort.current = controller;
    try {
      // result 保存后端返回的密码登录状态。
      const result = await checkPasswordLoginStatus(sessionId, controller.signal);
      if (!isCurrentAccountRequest(generation, passwordGeneration.current, accountId, passwordAccount.current)) return;
      // nextView 是面向编辑弹窗展示的状态。
      const nextView = { ...passwordLoginViewFromStatus(result), sessionId };
      if (result.status === 'success') {
        clearPasswordTimer();
        setPasswordLoginView(nextView);
        setEditForm(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, login_password: '', showLoginPassword: false }));
        await loadAccounts();
        return;
      }
      if (result.status === 'processing' || result.status === 'verification_required') {
        setPasswordLoginView(nextView);
        clearPasswordTimer();
        passwordTimer.current = window.setTimeout(/* 当前回调处理用户交互或异步状态变化。 */ () => void pollPasswordLogin(sessionId, generation, accountId), 1500);
        return;
      }
      clearPasswordTimer();
      setPasswordLoginView(nextView);
    } catch (/* error 表示错误。 */ error) {
      // error 保存密码登录状态查询失败原因。
      if (!isCurrentAccountRequest(generation, passwordGeneration.current, accountId, passwordAccount.current)) return;
      clearPasswordTimer();
      setPasswordLoginView({ sessionId, status: 'failed', message: error instanceof Error ? error.message : '查询密码登录状态失败', qrCodeUrl: '' });
    }
  }, [clearPasswordTimer, loadAccounts, setEditForm]);

  /** 启动账号密码登录刷新授权。 */
  // handlePasswordLoginCallback 是密码登录启动回调。
  const handlePasswordLogin = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (): Promise<void> => {
    if (!editingAccount) return;
    // accountName 是本次登录使用的闲鱼账号名。
    const accountName = editForm.username.trim();
    if (!accountName || !editForm.login_password) return;
    clearPasswordTimer();
    passwordAbort.current?.abort();
    // generation 是本次密码登录请求的递增代次。
    const generation = ++passwordGeneration.current;
    passwordAccount.current = editingAccount.id;
    // startController 控制密码登录启动请求的取消。
    const startController = new AbortController();
    passwordAbort.current = startController;
    setPasswordLoginView({ sessionId: '', status: 'processing', message: '正在启动密码登录…', qrCodeUrl: '' });
    try {
      // result 保存密码登录启动接口返回值。
      const result = await passwordLogin({ account_id: editingAccount.id, account: accountName, password: editForm.login_password, show_browser: editForm.show_browser }, { signal: startController.signal });
      if (!isCurrentAccountRequest(generation, passwordGeneration.current, editingAccount.id, passwordAccount.current)) return;
      if (!result.success || !result.session_id) throw new Error(result.message || '无法启动密码登录');
      setPasswordLoginView({ sessionId: result.session_id, status: 'processing', message: result.message || '登录处理中', qrCodeUrl: '' });
      await pollPasswordLogin(result.session_id, generation, editingAccount.id);
    } catch (/* error 表示错误。 */ error) {
      // error 保存密码登录启动失败原因。
      if (!isCurrentAccountRequest(generation, passwordGeneration.current, editingAccount.id, passwordAccount.current)) return;
      setPasswordLoginView({ sessionId: '', status: 'failed', message: error instanceof Error ? error.message : '启动密码登录失败', qrCodeUrl: '' });
    }
  }, [clearPasswordTimer, editForm.login_password, editForm.show_browser, editForm.username, editingAccount, pollPasswordLogin]);

  /** 取消当前密码登录会话。 */
  // handleCancelPasswordLoginCallback 是密码登录取消回调。
  const handleCancelPasswordLogin = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (): Promise<void> => {
    // sessionId 是当前待取消的密码登录会话。
    const sessionId = passwordLoginView.sessionId;
    passwordGeneration.current += 1;
    passwordAccount.current = '';
    passwordAbort.current?.abort();
    clearPasswordTimer();
    if (sessionId) await cancelPasswordLogin(sessionId).catch(/* 当前回调处理异步操作结果。 */ () => undefined);
    setPasswordLoginView({ sessionId: '', status: 'idle', message: '', qrCodeUrl: '' });
  }, [clearPasswordTimer, passwordLoginView.sessionId]);

  /** 关闭编辑弹窗并取消未完成的子模块请求。 */
  // closeEditModalCallback 是编辑弹窗关闭回调。
  const closeEditModal = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (): Promise<void> => {
    bindingsAbort.current?.abort();
    longLoginSequence.current += 1;
    longLoginAbort.current?.abort();
    aiAbort.current?.abort();
    if (passwordLoginView.status === 'processing' || passwordLoginView.status === 'verification_required') await handleCancelPasswordLogin();
    setActiveModal(null);
  }, [handleCancelPasswordLogin, passwordLoginView.status, setActiveModal]);

  /** 关闭 AI 设置弹窗并取消读取请求。 */
  // closeAIModalCallback 是 AI 设置弹窗关闭回调。
  const closeAIModal = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ () => {
    aiSequence.current += 1;
    aiAbort.current?.abort();
    setActiveModal(null);
  }, [setActiveModal]);

  // cleanupSubmodulesEffect 在 Hook 卸载时取消所有子模块请求。
  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => /* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    bindingsAbort.current?.abort();
    longLoginAbort.current?.abort();
    aiAbort.current?.abort();
    passwordAbort.current?.abort();
    clearPasswordTimer();
  }, [clearPasswordTimer]);

  return {
    longLogin, notifChannels, selectedChannelIds, bindingsLoaded, bindingsLoading, bindingsDirty, bindingsLoadError,
    aiSettings, saving, passwordLoginView, setAiSettings, setBindingsDirty, setEditForm, openEditModal,
    closeEditModal, openAIModal, closeAIModal, loadNotificationBindings, toggleNotificationChannel,
    handleLongLoginToggle, handleSaveAISettings, handleSaveEdit, handleRestartPause, handlePasswordLogin,
    handleCancelPasswordLogin,
  };
};
