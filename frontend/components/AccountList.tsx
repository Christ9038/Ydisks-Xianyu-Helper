import React, { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { AccountDetail, AIReplySettings, NotificationChannel } from '../types';
import {
  updateAccountStatus,
  deleteAccount,
  generateQRLogin,
  checkQRLoginStatus,
  completeQRVerification,
  updateAccountPauseDuration,
  refreshAccountProfile,
  updateAccountSettings,
  passwordLogin,
  checkPasswordLoginStatus,
  cancelPasswordLogin,
  updateAccountAISettings,
  getAccountAISettings,
  getNotificationChannels,
  getAccountBindings,
  getLongLoginSettings,
  setLongLoginSettings,
} from '../app/features/accounts/api';
import {
  Power, Edit2, Trash2, QrCode, X, Check, Loader2,
  RefreshCw, Save, User, Clock, MessageCircle,
  Bot, Settings, AlertCircle, CalendarClock, Sparkles
} from 'lucide-react';
import { buildAccountLoginInfoUpdate, isCurrentAccountRequest, passwordLoginViewFromStatus, shouldUpdateAccountPause } from '../app/features/accounts/state';
import { shouldSaveNotificationBindings } from './accountBindings';
import { createLatestRequestGate, createQRLoginPoller } from './qrPolling';
import { RiskVerificationPanel } from './RiskVerificationPanel';
import { SquareQRCode } from './SquareQRCode';
import AccountAutomationModal from './AccountAutomationModal';
import { AccountEditModal } from '../app/features/accounts/components/AccountEditModal';
import { useAccountsData } from '../app/features/accounts/hooks';
import { accountRuntimePresentation } from '../app/features/accounts/runtime';
import type { AccountEditForm, LongLoginState, PasswordLoginView } from '../app/features/accounts/types';

type ModalType = 'edit' | 'ai-settings' | null;

const AccountList: React.FC = () => {
  const { accounts, setAccounts, loading, loadAccounts } = useAccountsData();
  const [accountSearch, setAccountSearch] = useState('');
  const [refreshingProfileId, setRefreshingProfileId] = useState<string>('');
  const [deletingAccountId, setDeletingAccountId] = useState<string>('');
  const [deleteDialogAccount, setDeleteDialogAccount] = useState<AccountDetail | null>(null);
  const [deleteError, setDeleteError] = useState('');
  const [showQRModal, setShowQRModal] = useState(false);
  const [qrCodeUrl, setQrCodeUrl] = useState<string>('');
  const [qrStatus, setQrStatus] = useState<string>('pending');
  const [qrErrorMessage, setQrErrorMessage] = useState<string>('');
  const [verificationScreenshot, setVerificationScreenshot] = useState<string>('');
  const [faceQrUrl, setFaceQrUrl] = useState<string>('');
  const [qrReauthTarget, setQrReauthTarget] = useState<AccountDetail | null>(null);
  const [activeModal, setActiveModal] = useState<ModalType>(null);
  const [editingAccount, setEditingAccount] = useState<AccountDetail | null>(null);
  const [taskAccount, setTaskAccount] = useState<AccountDetail | null>(null);
  const [longLogin, setLongLogin] = useState<LongLoginState>({ loading: false, saving: false, canOpen: false, enabled: false, error: '' });

  // 通知渠道绑定（编辑弹窗用）
  const [notifChannels, setNotifChannels] = useState<NotificationChannel[]>([]);
  const [selectedChannelIds, setSelectedChannelIds] = useState<number[]>([]);
  const [bindingsLoaded, setBindingsLoaded] = useState(false);
  const [bindingsLoading, setBindingsLoading] = useState(false);
  const [bindingsDirty, setBindingsDirty] = useState(false);
  const [bindingsLoadError, setBindingsLoadError] = useState('');

  // 编辑表单状态
  const [editForm, setEditForm] = useState<AccountEditForm>({
    remark: '',
    cookie: '',
    auto_confirm: false,
    pause_duration: 0,
    username: '',
    login_password: '',
    show_browser: false,
    showLoginPassword: false,
    clear_password: false,
  });

  // AI设置表单状态
  const [aiSettings, setAiSettings] = useState<AIReplySettings>({
    ai_enabled: false,
    max_discount_percent: 10,
    max_discount_amount: 100,
    max_bargain_rounds: 3,
    custom_prompts: '',
  });
  const [saving, setSaving] = useState(false);
  const [passwordLoginView, setPasswordLoginView] = useState<PasswordLoginView>({ sessionId: '', status: 'idle', message: '', qrCodeUrl: '' });
  const qrPollerRef = useRef<ReturnType<typeof createQRLoginPoller> | null>(null);
  const qrRequestGateRef = useRef<ReturnType<typeof createLatestRequestGate> | null>(null);
  const bindingsLoadGateRef = useRef<ReturnType<typeof createLatestRequestGate> | null>(null);
  const aiLoadGateRef = useRef<ReturnType<typeof createLatestRequestGate> | null>(null);
  const bindingsLoadAbortRef = useRef<AbortController | null>(null);
  const aiLoadAbortRef = useRef<AbortController | null>(null);
  const qrGenerateAbortRef = useRef<AbortController | null>(null);
  const passwordPollAbortRef = useRef<AbortController | null>(null);
  const qrCloseTimerRef = useRef<number | null>(null);
  const passwordPollTimerRef = useRef<number | null>(null);
  const passwordPollGenerationRef = useRef(0);
  const passwordPollAccountRef = useRef('');
  if (qrPollerRef.current === null) {
    qrPollerRef.current = createQRLoginPoller();
  }
  if (qrRequestGateRef.current === null) {
    qrRequestGateRef.current = createLatestRequestGate();
  }
  if (bindingsLoadGateRef.current === null) bindingsLoadGateRef.current = createLatestRequestGate();
  if (aiLoadGateRef.current === null) aiLoadGateRef.current = createLatestRequestGate();

  const clearQRCloseTimer = () => {
    if (qrCloseTimerRef.current !== null) {
      window.clearTimeout(qrCloseTimerRef.current);
      qrCloseTimerRef.current = null;
    }
  };

  const clearPasswordPollTimer = () => {
    if (passwordPollTimerRef.current !== null) {
      window.clearTimeout(passwordPollTimerRef.current);
      passwordPollTimerRef.current = null;
    }
  };

  const stopQRPolling = () => {
    qrPollerRef.current?.stop();
  };

  const closeQRModal = () => {
	qrGenerateAbortRef.current?.abort();
    qrRequestGateRef.current?.cancel();
    stopQRPolling();
    clearQRCloseTimer();
    setShowQRModal(false);
  };

  const scheduleQRModalClose = () => {
    clearQRCloseTimer();
    qrCloseTimerRef.current = window.setTimeout(() => {
      qrCloseTimerRef.current = null;
      setShowQRModal(false);
      loadAccounts();
    }, 1000);
  };

  useEffect(() => {
    // 页面卸载时取消二维码、通知绑定、AI 设置和密码登录的异步资源。
    return () => {
      stopQRPolling();
      qrRequestGateRef.current?.cancel();
      bindingsLoadGateRef.current?.cancel();
      aiLoadGateRef.current?.cancel();
      bindingsLoadAbortRef.current?.abort();
      aiLoadAbortRef.current?.abort();
      qrGenerateAbortRef.current?.abort();
      passwordPollAbortRef.current?.abort();
      clearQRCloseTimer();
      passwordPollGenerationRef.current += 1;
      clearPasswordPollTimer();
    };
  }, []);

  const handleToggle = async (id: string, currentStatus: boolean) => {
    await updateAccountStatus(id, !currentStatus);
    loadAccounts();
  };

  const openDeleteDialog = (account: AccountDetail) => {
    if (deletingAccountId) return;
    setDeleteError('');
    setDeleteDialogAccount(account);
  };

  const closeDeleteDialog = () => {
    if (deletingAccountId) return;
    setDeleteError('');
    setDeleteDialogAccount(null);
  };

  const confirmDeleteAccount = async () => {
    const account = deleteDialogAccount;
    if (!account || deletingAccountId) return;
    setDeletingAccountId(account.id);
    setDeleteError('');
    try {
      await deleteAccount(account.id);
      setAccounts(current => current.filter(item => item.id !== account.id));
      setDeleteDialogAccount(null);
    } catch (error: any) {
      console.error('删除账号失败:', error);
      setDeleteError(error?.message || '删除账号失败，请稍后重试');
    } finally {
      setDeletingAccountId('');
    }
  };

  const handleRefreshProfile = async (account: AccountDetail) => {
    setRefreshingProfileId(account.id);
    try {
      const res = await refreshAccountProfile(account.id);
      if (res?.profile_error) {
        alert('资料刷新失败：' + res.profile_error);
      }
      await loadAccounts();
    } catch (error: any) {
      console.error('刷新账号资料失败:', error);
      alert(error?.message || '刷新账号资料失败，请先重新授权该账号');
    } finally {
      setRefreshingProfileId('');
    }
  };

  const loadNotificationBindings = async (accountId: string) => {
	const generation = bindingsLoadGateRef.current!.next();
	bindingsLoadAbortRef.current?.abort();
	const controller = new AbortController();
	bindingsLoadAbortRef.current = controller;
    setBindingsLoading(true);
    setBindingsLoaded(false);
    setBindingsDirty(false);
    setBindingsLoadError('');
    const [channelsResult, bindingsResult] = await Promise.allSettled([
	  getNotificationChannels({ signal: controller.signal }),
	  getAccountBindings(accountId, { signal: controller.signal }),
    ]);
	if (!bindingsLoadGateRef.current?.isCurrent(generation)) return;
    if (channelsResult.status === 'fulfilled') {
      setNotifChannels(channelsResult.value.data || []);
    } else {
      setNotifChannels([]);
      setBindingsLoadError('通知渠道列表加载失败，请重试');
    }
    if (bindingsResult.status === 'fulfilled') {
      setSelectedChannelIds(bindingsResult.value || []);
      setBindingsLoaded(true);
    } else {
      setSelectedChannelIds([]);
      setBindingsLoadError('通知绑定加载失败；本次保存不会修改现有绑定');
    }
    setBindingsLoading(false);
  };

  // toggleNotificationChannel 使用函数式更新切换通知绑定，避免连续点击覆盖选择结果。
  const toggleNotificationChannel = (channelId: number) => {
    setSelectedChannelIds(current => current.includes(channelId)
      ? current.filter(id => id !== channelId)
      : [...current, channelId]);
  };

  const openEditModal = async (account: AccountDetail) => {
    passwordPollGenerationRef.current += 1;
	passwordPollAccountRef.current = account.id;
	passwordPollAbortRef.current?.abort();
    clearPasswordPollTimer();
    setPasswordLoginView({ sessionId: '', status: 'idle', message: '', qrCodeUrl: '' });
    setEditingAccount(account);
    setEditForm({
      remark: account.remark || account.note || '',
      cookie: account.cookie || account.value || '',
      auto_confirm: account.auto_confirm || false,
      pause_duration: account.pause_duration || 0,
      username: account.username || '',
      login_password: account.login_password || '',
      show_browser: account.show_browser || false,
      showLoginPassword: false,
      clear_password: false,
    });
    setActiveModal('edit');
    setLongLogin({ loading: true, saving: false, canOpen: false, enabled: false, error: '' });
    const [, longLoginResult] = await Promise.allSettled([
      loadNotificationBindings(account.id),
      getLongLoginSettings(account.id),
    ]);
    if (longLoginResult.status === 'fulfilled') {
      setLongLogin({ loading: false, saving: false, canOpen: longLoginResult.value.can_open_long_login, enabled: longLoginResult.value.enabled, error: '' });
    } else {
      setLongLogin({ loading: false, saving: false, canOpen: false, enabled: false, error: '无法读取闲鱼保存登录信息状态' });
    }
  };

  const handleLongLoginToggle = async () => {
    if (!editingAccount || longLogin.loading || longLogin.saving || !longLogin.canOpen) return;
    const enabled = !longLogin.enabled;
    setLongLogin(current => ({ ...current, saving: true, error: '' }));
    try {
      const result = await setLongLoginSettings(editingAccount.id, enabled);
      setLongLogin({ loading: false, saving: false, canOpen: result.can_open_long_login, enabled: result.enabled, error: '' });
    } catch (error) {
      setLongLogin(current => ({ ...current, saving: false, error: error instanceof Error ? error.message : '保存登录信息设置失败' }));
    }
  };

  const openAIModal = async (account: AccountDetail) => {
	const generation = aiLoadGateRef.current!.next();
	aiLoadAbortRef.current?.abort();
	const controller = new AbortController();
	aiLoadAbortRef.current = controller;
    setEditingAccount(account);
	setActiveModal('ai-settings');
    setSaving(true);
    try {
	  const settings = await getAccountAISettings(account.id, { signal: controller.signal });
	  if (!aiLoadGateRef.current?.isCurrent(generation)) return;
      setAiSettings({
        ai_enabled: settings.ai_enabled ?? false,
        max_discount_percent: settings.max_discount_percent ?? 10,
        max_discount_amount: settings.max_discount_amount ?? 100,
        max_bargain_rounds: settings.max_bargain_rounds ?? 3,
        custom_prompts: settings.custom_prompts ?? '',
      });
    } catch (e) {
	  if (aiLoadGateRef.current?.isCurrent(generation)) console.error('Failed to load AI settings:', e);
    } finally {
	  if (aiLoadGateRef.current?.isCurrent(generation)) setSaving(false);
    }
  };

  const handleSaveEdit = async () => {
    if (!editingAccount) return;
    setSaving(true);

    try {
	  const payload: Parameters<typeof updateAccountSettings>[1] = {};

      // 更新备注
      if (editForm.remark !== (editingAccount.remark || editingAccount.note || '')) {
		payload.remark = editForm.remark;
      }

      // 更新Cookie
      if (editForm.cookie && editForm.cookie !== (editingAccount.cookie || editingAccount.value || '')) {
		payload.cookie = editForm.cookie;
      }

      // 更新自动确认
      if (editForm.auto_confirm !== editingAccount.auto_confirm) {
		payload.auto_confirm = editForm.auto_confirm;
      }

      // 更新暂停时长
	  if (shouldUpdateAccountPause(editForm.pause_duration, editingAccount)) {
		payload.pause_duration = editForm.pause_duration;
	  }

      // 更新登录信息
      const loginInfo = buildAccountLoginInfoUpdate(editingAccount, editForm);
      if (loginInfo) {
		Object.assign(payload, loginInfo);
      }

      // 只有成功加载且用户确实改动后才覆盖，避免加载失败被误解释成解绑全部。
      if (shouldSaveNotificationBindings(bindingsLoaded, bindingsDirty)) {
		payload.channel_ids = selectedChannelIds;
      }

	  if (Object.keys(payload).length > 0) await updateAccountSettings(editingAccount.id, payload);
      setActiveModal(null);
      loadAccounts();
    } catch (error) {
      console.error('更新账号失败:', error);
      alert('更新失败，请重试');
    } finally {
      setSaving(false);
    }
  };

  const handleRestartPause = async () => {
    if (!editingAccount || editForm.pause_duration <= 0) return;
    setSaving(true);
    try {
      const result = await updateAccountPauseDuration(editingAccount.id, editForm.pause_duration);
      setEditingAccount({
        ...editingAccount,
        pause_duration: editForm.pause_duration,
        paused: result?.paused === true,
        paused_until: Number(result?.paused_until || 0),
      });
      await loadAccounts();
    } catch (error) {
      alert(error instanceof Error ? error.message : '重新暂停失败');
    } finally {
      setSaving(false);
    }
  };

  const handleSaveAISettings = async () => {
    if (!editingAccount) return;
    setSaving(true);

    try {
      await updateAccountAISettings(editingAccount.id, aiSettings);
      setActiveModal(null);
      loadAccounts();
    } catch (error) {
      console.error('更新AI设置失败:', error);
      alert('更新失败，请重试');
    } finally {
      setSaving(false);
    }
  };

  // pollPasswordLogin 轮询密码登录状态，并按账号和请求代次隔离过期响应。
  const pollPasswordLogin = async (sessionId: string, generation: number, accountId: string) => {
    passwordPollAbortRef.current?.abort();
    // controller 取消上一轮仍未完成的密码登录状态请求。
    const controller = new AbortController();
    passwordPollAbortRef.current = controller;
    try {
      // result 是后端返回的当前密码登录状态。
      const result = await checkPasswordLoginStatus(sessionId, controller.signal);
      if (!isCurrentAccountRequest(generation, passwordPollGenerationRef.current, accountId, passwordPollAccountRef.current)) return;
      // nextView 是统一转换后的密码登录展示状态。
      const nextView = { ...passwordLoginViewFromStatus(result), sessionId };
      if (result.status === 'success') {
        clearPasswordPollTimer();
        setPasswordLoginView(nextView);
        setEditForm(current => ({ ...current, login_password: '', showLoginPassword: false }));
        await loadAccounts();
        return;
      }
      if (result.status === 'processing' || result.status === 'verification_required') {
        setPasswordLoginView(nextView);
        clearPasswordPollTimer();
        passwordPollTimerRef.current = window.setTimeout(
          // pollNextStatus 在短暂间隔后继续查询当前密码登录会话。
          () => void pollPasswordLogin(sessionId, generation, accountId),
          1500,
        );
        return;
      }
      clearPasswordPollTimer();
      setPasswordLoginView(nextView);
    } catch (error /* 密码登录状态查询错误 */) {
      if (!isCurrentAccountRequest(generation, passwordPollGenerationRef.current, accountId, passwordPollAccountRef.current)) return;
      clearPasswordPollTimer();
      setPasswordLoginView({
        sessionId,
        status: 'failed',
        message: error instanceof Error ? error.message : '查询密码登录状态失败',
        qrCodeUrl: '',
      });
    }
  };


  const handlePasswordLogin = async () => {
    if (!editingAccount) return;
    const account = editForm.username.trim();
    if (!account || !editForm.login_password) {
      alert('请先填写登录账号和本次登录密码');
      return;
    }
    clearPasswordPollTimer();
	passwordPollAbortRef.current?.abort();
    const generation = ++passwordPollGenerationRef.current;
    passwordPollAccountRef.current = editingAccount.id;
    setPasswordLoginView({ sessionId: '', status: 'processing', message: '正在启动密码登录…', qrCodeUrl: '' });
    try {
      const result = await passwordLogin({
        account_id: editingAccount.id,
        account,
        password: editForm.login_password,
        show_browser: editForm.show_browser,
      });
      if (!isCurrentAccountRequest(generation, passwordPollGenerationRef.current, editingAccount.id, passwordPollAccountRef.current)) return;
      if (!result.success || !result.session_id) {
        throw new Error(result.message || '无法启动密码登录');
      }
      setPasswordLoginView({ sessionId: result.session_id, status: 'processing', message: result.message || '登录处理中', qrCodeUrl: '' });
      await pollPasswordLogin(result.session_id, generation, editingAccount.id);
    } catch (error) {
      if (!isCurrentAccountRequest(generation, passwordPollGenerationRef.current, editingAccount.id, passwordPollAccountRef.current)) return;
      setPasswordLoginView({
        sessionId: '',
        status: 'failed',
        message: error instanceof Error ? error.message : '启动密码登录失败',
        qrCodeUrl: '',
      });
    }
  };

  const handleCancelPasswordLogin = async () => {
    const sessionId = passwordLoginView.sessionId;
	passwordPollGenerationRef.current += 1;
	passwordPollAccountRef.current = '';
	passwordPollAbortRef.current?.abort();
    clearPasswordPollTimer();
    if (sessionId) {
      try {
        await cancelPasswordLogin(sessionId);
      } catch (error) {
        console.error('取消密码登录失败', error);
      }
    }
    setPasswordLoginView({ sessionId: '', status: 'idle', message: '', qrCodeUrl: '' });
  };

  const closeEditModal = async () => {
	bindingsLoadGateRef.current?.cancel();
	bindingsLoadAbortRef.current?.abort();
    if (passwordLoginView.status === 'processing' || passwordLoginView.status === 'verification_required') {
      await handleCancelPasswordLogin();
    }
    setActiveModal(null);
  };

  const closeAIModal = () => {
	aiLoadGateRef.current?.cancel();
	aiLoadAbortRef.current?.abort();
	setActiveModal(null);
  };

  const completeAndPersistQRSession = async (sessionId: string, target?: AccountDetail | null) => {
    const res = await completeQRVerification(sessionId, target?.id);
    if (!res.success || !res.account_id) {
      throw new Error(res.message || '保存扫码授权失败');
    }
    return res.account_id;
  };

  const startQRLogin = async (target?: AccountDetail) => {
    stopQRPolling();
	qrGenerateAbortRef.current?.abort();
	const controller = new AbortController();
	qrGenerateAbortRef.current = controller;
    const requestGeneration = qrRequestGateRef.current!.next();
    clearQRCloseTimer();
    const targetAccount = target || null;
    setQrReauthTarget(targetAccount);
    setShowQRModal(true);
    setQrStatus('loading');
    setQrErrorMessage('');
    setQrCodeUrl('');
    setVerificationScreenshot('');
    setFaceQrUrl('');
    try {
	  const res = await generateQRLogin({ signal: controller.signal });
      if (!qrRequestGateRef.current?.isCurrent(requestGeneration)) return;
      if (!res.success || !res.qr_code_url || !res.session_id) {
        throw new Error(res.message || '闲鱼未返回可用的登录二维码');
      }
      if (res.success && res.qr_code_url && res.session_id) {
        const generatedSessionID = res.session_id;
        setQrCodeUrl(res.qr_code_url);
        setQrStatus('waiting');

        qrPollerRef.current?.start(generatedSessionID, checkQRLoginStatus, {
          onSuccess: async () => {
            try {
              const accountId = await completeAndPersistQRSession(generatedSessionID, targetAccount);
              if (!accountId) {
                setQrStatus('error');
                return;
              }
            } catch (e) {
              console.error('保存扫码授权失败', e);
              setQrStatus('error');
              return;
            }
            setQrStatus('success');
            scheduleQRModalClose();
          },
          onScanned: () => {
            setQrStatus('waiting'); // 已扫描，继续等待确认
          },
          onTerminalError: () => {
            setQrStatus('error');
          },
          onPollError: (error) => {
            console.error('轮询扫码状态失败', error);
            setQrStatus('error');
          },
          onVerificationRequired: (statusRes) => {
            setQrStatus('verification');
            if (statusRes.face_qr_url) {
              setFaceQrUrl(statusRes.face_qr_url);
            }
            if (statusRes.verification_screenshot) {
              setVerificationScreenshot(statusRes.verification_screenshot);
            }
          },
        });
      }
    } catch (e) {
      if (!qrRequestGateRef.current?.isCurrent(requestGeneration)) return;
      setQrErrorMessage(e instanceof Error ? e.message : '二维码获取失败，请稍后重试');
      setQrStatus('error');
    } finally {
      if (qrGenerateAbortRef.current === controller) qrGenerateAbortRef.current = null;
    }
  };

  if (loading) return <div className="p-20 flex justify-center"><Loader2 className="w-8 h-8 text-brand animate-spin"/></div>;

  const filteredAccounts = accounts.filter(account => {
    const keyword = accountSearch.trim().toLowerCase();
    if (!keyword) return true;
    return [
      account.id,
      account.nickname,
      account.remark,
      account.note,
      account.username,
      account.runtime_message,
    ].some(value => (value || '').toLowerCase().includes(keyword));
  });

  return (
    <div className="space-y-8 animate-fade-in relative">
      <div className="flex flex-col xl:flex-row xl:justify-between xl:items-end gap-4">
        <div>
          <h2 className="text-4xl font-extrabold text-gray-900 tracking-tight">账号管理</h2>
          <p className="text-gray-500 mt-2 font-medium">管理您的闲鱼授权账号及设置。建议给账号填写备注，便于多账号区分。</p>
        </div>
        <div className="flex flex-col sm:flex-row gap-3">
          <input
            value={accountSearch}
            onChange={event => setAccountSearch(event.target.value)}
            placeholder="搜索昵称 / 备注 / 账号ID"
            className="ios-input px-4 py-3 rounded-2xl text-sm w-full sm:w-72"
          />
          <button
            onClick={() => startQRLogin()}
            className="ios-btn-primary flex items-center gap-2 px-6 py-3 rounded-2xl font-bold shadow-lg shadow-blue-200 transition-transform hover:scale-105 active:scale-95"
          >
            <QrCode className="w-5 h-5" />
            扫码添加新账号
          </button>
        </div>
      </div>

      <div className="rounded-2xl border border-blue-100 bg-blue-50 px-5 py-4 text-sm text-blue-900 flex flex-col md:flex-row md:items-center md:justify-between gap-2">
        <div className="font-bold">当前显示 {filteredAccounts.length} / {accounts.length} 个账号</div>
        <div className="text-blue-700">如果某个账号只显示 ID，点该账号右侧“刷新资料”；若刷新失败，先点二维码重新授权。</div>
      </div>

      {/* Account Grid */}
      <div className="grid grid-cols-1 gap-6">
        {filteredAccounts.map((account) => {
          const runtime = accountRuntimePresentation(account);
          const requiresLogin = account.runtime_state === 'auth_expired' || account.runtime_state === 'verification_required';
          return (
          <div key={account.id} className="ios-card rounded-xl p-6 group transition-all duration-300 hover:border-brand">
            <div className="flex min-w-0 items-start gap-5 sm:gap-8">
              <div className="relative">
                {account.avatar_url ? (
                  <img
                    src={account.avatar_url}
                    alt={account.nickname || '账号头像'}
                    className="w-20 h-20 rounded-full object-cover shadow-md ring-4 ring-white bg-gray-100"
                  />
                ) : (
                  <div className="w-20 h-20 rounded-full bg-gray-100 text-gray-400 shadow-md ring-4 ring-white flex items-center justify-center">
                    <User className="w-9 h-9" />
                  </div>
                )}
                <div className={`absolute -bottom-1 -right-1 w-6 h-6 rounded-full border-4 border-white flex items-center justify-center ${runtime.dot}`}>
                    {account.runtime_state === 'online' && <Check className="w-3 h-3 text-white" />}
                </div>
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2.5 mb-1">
                    <h3 className="text-xl font-extrabold text-gray-900 break-words">{account.nickname || account.remark || `账号 ${account.id.substring(0,6)}...`}</h3>
                    <span className={`px-2.5 py-0.5 rounded-lg text-xs font-bold ${runtime.badge}`}>{runtime.label}</span>
                    {account.ai_enabled && (
                        <span className="px-2.5 py-0.5 rounded-lg bg-purple-100 text-purple-700 text-xs font-bold flex items-center gap-1">
                          <Bot className="w-3 h-3" /> AI
                        </span>
                    )}
                    {account.auto_rate_enabled && (
                      <span className="flex items-center gap-1 rounded-lg bg-emerald-100 px-2.5 py-0.5 text-xs font-bold text-emerald-700">
                        <MessageCircle className="h-3 w-3" /> 自动评价
                      </span>
                    )}
                    {account.auto_polish_enabled && (
                      <span className="flex items-center gap-1 rounded-lg bg-amber-100 px-2.5 py-0.5 text-xs font-bold text-amber-700">
                        <Sparkles className="h-3 w-3" /> 每日擦亮
                      </span>
                    )}
                    {account.auto_confirm && <span className="flex items-center gap-1 rounded-lg bg-blue-50 px-2.5 py-0.5 text-xs font-bold text-blue-700"><Check className="h-3 w-3" /> 自动确认发货</span>}
                    {account.profile_error && (
                        <span
                          className="px-2.5 py-0.5 rounded-lg bg-amber-100 text-amber-700 text-xs font-bold flex items-center gap-1"
                          title={account.profile_error}
                        >
                          <AlertCircle className="w-3 h-3" /> 资料未同步
                        </span>
                    )}
                </div>
                <div className="mt-3 flex flex-wrap items-center justify-between gap-4">
                  <div className="text-sm font-medium text-gray-500">
                  <p>{account.remark || account.note || '暂无备注'}</p>
                  <p className="font-mono text-xs text-gray-400">ID: {account.id}</p>
                  </div>
                {account.runtime_message && account.runtime_state !== 'online' && account.runtime_state !== 'disabled' && (
                  <div className={`mb-3 flex flex-wrap items-center gap-2 text-sm font-medium ${requiresLogin ? 'text-red-700' : 'text-amber-700'}`}>
                    <AlertCircle className="w-4 h-4 flex-shrink-0" />
                    <span>{account.runtime_message}</span>
                    {requiresLogin && (
                      <button
                        type="button"
                        onClick={() => startQRLogin(account)}
                        className="inline-flex items-center gap-1.5 rounded-lg bg-red-50 px-2.5 py-1 text-xs font-bold text-red-700 hover:bg-red-100"
                      >
                        <QrCode className="w-3.5 h-3.5" /> 重新授权
                      </button>
                    )}
                  </div>
                )}
                  {account.paused && <span className="flex items-center gap-1.5 rounded-lg bg-blue-50 px-3 py-1.5 text-xs font-bold text-blue-700"><Clock className="h-3 w-3" /> 暂停处理中</span>}
                <div className="mt-3 flex flex-wrap items-center justify-end gap-2">
                <button
                    onClick={() => handleRefreshProfile(account)}
                    disabled={refreshingProfileId === account.id}
                    className="p-3 rounded-xl transition-colors text-gray-600 hover:bg-gray-100 disabled:opacity-50"
                    title="刷新昵称和头像"
                >
                    <RefreshCw className={`w-5 h-5 ${refreshingProfileId === account.id ? 'animate-spin' : ''}`} />
                </button>
                <button
                    onClick={() => startQRLogin(account)}
                    className={`p-3 rounded-xl transition-colors ${requiresLogin ? 'text-red-600 bg-red-50 hover:bg-red-100' : 'text-blue-600 hover:bg-blue-50'}`}
                    title="重新扫码授权当前账号"
                >
                    <QrCode className="w-5 h-5" />
                </button>
                <button
                    onClick={() => openEditModal(account)}
                    className="p-3 rounded-xl hover:bg-gray-100 transition-colors text-gray-600"
                    title="编辑账号"
                >
                    <Edit2 className="w-5 h-5" />
                </button>
                <button
                    onClick={() => openAIModal(account)}
                    className="p-3 rounded-xl hover:bg-purple-100 transition-colors text-purple-600"
                    title="AI设置"
                >
                    <Bot className="w-5 h-5" />
                </button>
                <button
                    onClick={() => setTaskAccount(account)}
                    className="p-3 rounded-xl hover:bg-amber-100 transition-colors text-amber-600"
                    title="自动评价与每日擦亮"
                >
                    <CalendarClock className="w-5 h-5" />
                </button>
                <button
                    onClick={() => handleToggle(account.id, account.enabled)}
                  className={`p-3 rounded-xl transition-colors ${account.enabled ? 'text-green-600 hover:bg-green-50' : 'text-gray-400 hover:bg-gray-100'}`}
                  title={account.enabled ? '停用账号' : '启用账号'}
                >
                    <Power className="w-5 h-5" />
                </button>
                <button
                    onClick={() => openDeleteDialog(account)}
                    disabled={deletingAccountId !== ''}
                    className="p-3 rounded-xl hover:bg-red-100 transition-colors text-red-500 disabled:cursor-not-allowed disabled:opacity-40"
                    title={deletingAccountId === account.id ? '删除中…' : `删除账号 ${account.nickname || account.remark || account.id}`}
                >
                    {deletingAccountId === account.id
                      ? <Loader2 className="w-5 h-5 animate-spin" />
                      : <Trash2 className="w-5 h-5" />}
                </button>
                </div>
              </div>
            </div>
          </div>
          </div>
        )})}

        {accounts.length === 0 && (
            <div className="ios-card p-12 text-center">
                <div className="w-20 h-20 bg-gray-100 rounded-full flex items-center justify-center mx-auto mb-4">
                    <User className="w-10 h-10 text-gray-400" />
                </div>
                <h3 className="text-lg font-bold text-gray-900">暂无账号</h3>
                <p className="text-gray-500 mt-1">请点击右上角扫码添加您的闲鱼账号</p>
            </div>
        )}
        {accounts.length > 0 && filteredAccounts.length === 0 && (
            <div className="ios-card p-12 text-center">
                <h3 className="text-lg font-bold text-gray-900">没有匹配的账号</h3>
                <p className="text-gray-500 mt-1">换一个关键词搜索昵称、备注或账号ID。</p>
            </div>
        )}
      </div>

      {taskAccount && createPortal(
        <AccountAutomationModal
          account={taskAccount}
          onClose={() => setTaskAccount(null)}
          onSaved={settings => {
            setAccounts(current => current.map(account => account.id === taskAccount.id ? {
              ...account,
              auto_rate_enabled: settings.auto_rate_enabled,
              rate_content: settings.rate_content,
              auto_polish_enabled: settings.auto_polish_enabled,
              polish_time: settings.polish_time,
              last_rate_scan_at: settings.last_rate_scan_at,
              last_polish_date: settings.last_polish_date,
              last_polish_at: settings.last_polish_at,
            } : account));
          }}
        />,
        document.body
      )}

      {/* 删除账号确认弹窗 */}
      {deleteDialogAccount && createPortal(
        <div className="modal-overlay-centered" role="presentation">
          <div
            className="h-fit w-full max-w-[400px] self-center overflow-hidden rounded-2xl border border-white/80 bg-white shadow-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="delete-account-title"
            aria-describedby="delete-account-description"
          >
            <div className="relative overflow-hidden border-b border-red-100 bg-gradient-to-br from-red-50 via-white to-orange-50 px-5 py-5">
              <div className="absolute -right-12 -top-16 h-36 w-36 rounded-full bg-red-200/35 blur-2xl" />
              <div className="relative flex items-start gap-4">
                <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl bg-red-600 text-white shadow-md shadow-red-200">
                  <Trash2 className="h-5 w-5" />
                </div>
                <div className="min-w-0 flex-1">
                  <h3 id="delete-account-title" className="text-lg font-extrabold tracking-tight text-gray-950">
                    删除这个账号？
                  </h3>
                  <p id="delete-account-description" className="mt-1 text-xs leading-5 text-gray-600">
                    删除后，该账号的关联配置也会一并清理，此操作无法撤销。
                  </p>
                </div>
                <button
                  type="button"
                  onClick={closeDeleteDialog}
                  disabled={deletingAccountId !== ''}
                  className="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-full text-gray-400 transition-colors hover:bg-white hover:text-gray-700 disabled:cursor-not-allowed disabled:opacity-40"
                  aria-label="关闭删除确认"
                >
                  <X className="h-5 w-5" />
                </button>
              </div>
            </div>

            <div className="px-5 py-4">
              <div className="rounded-xl border border-gray-100 bg-gray-50/80 px-4 py-3">
                <div className="text-sm font-extrabold text-gray-900">
                  {deleteDialogAccount.nickname || deleteDialogAccount.remark || '未命名账号'}
                </div>
                {deleteDialogAccount.remark && deleteDialogAccount.nickname && (
                  <div className="mt-1 text-sm text-gray-500">备注：{deleteDialogAccount.remark}</div>
                )}
                <div className="mt-1.5 break-all font-mono text-[11px] text-gray-400">ID: {deleteDialogAccount.id}</div>
              </div>

              {deletingAccountId === deleteDialogAccount.id && (
                <div
                  className="mt-4 flex items-center gap-3 rounded-xl border border-blue-100 bg-blue-50 px-4 py-3"
                  role="progressbar"
                  aria-label="正在删除账号"
                >
                  <Loader2 className="h-5 w-5 flex-shrink-0 animate-spin text-brand" />
                  <div>
                    <div className="text-sm font-extrabold text-blue-950">正在删除账号</div>
                    <div className="mt-0.5 text-xs text-blue-700">正在清理关联数据，请保持页面打开…</div>
                  </div>
                </div>
              )}

              {deleteError && (
                <div className="mt-4 flex items-start gap-3 rounded-xl border border-red-100 bg-red-50 px-4 py-3" role="alert">
                  <AlertCircle className="mt-0.5 h-5 w-5 flex-shrink-0 text-red-600" />
                  <div>
                    <div className="text-sm font-extrabold text-red-800">删除失败</div>
                    <div className="mt-1 text-xs leading-5 text-red-700">{deleteError}</div>
                  </div>
                </div>
              )}
            </div>

            <div className="flex gap-3 border-t border-gray-100 bg-gray-50/70 px-5 py-4">
              <button
                type="button"
                onClick={closeDeleteDialog}
                disabled={deletingAccountId !== ''}
                className="flex-1 rounded-xl bg-white px-5 py-3 text-sm font-extrabold text-gray-700 ring-1 ring-gray-200 transition-colors hover:bg-gray-100 disabled:cursor-not-allowed disabled:opacity-50"
              >
                取消
              </button>
              <button
                type="button"
                onClick={() => void confirmDeleteAccount()}
                disabled={deletingAccountId !== ''}
                className="flex flex-1 items-center justify-center gap-2 rounded-xl bg-red-600 px-5 py-3 text-sm font-extrabold text-white shadow-lg shadow-red-200 transition-colors hover:bg-red-700 disabled:cursor-not-allowed disabled:bg-red-400"
              >
                {deletingAccountId === deleteDialogAccount.id ? (
                  <><Loader2 className="h-4 w-4 animate-spin" />处理中</>
                ) : (
                  <><Trash2 className="h-4 w-4" />确认删除</>
                )}
              </button>
            </div>
          </div>
        </div>,
        document.body
      )}

      {/* QR Code Modal */}
      {showQRModal && createPortal(
          <div className="modal-overlay-centered">
			  <div className="modal-container relative" style={{maxWidth: '26rem'}}>
                  <button
                    onClick={closeQRModal}
                    className="absolute top-4 right-4 z-10 w-9 h-9 flex items-center justify-center bg-gray-100 hover:bg-gray-200 rounded-full transition-colors"
                  >
                    <X className="w-5 h-5 text-gray-600" />
                  </button>

                  <div className="modal-body">
                      <div className="text-center">
                          <h3 className="text-2xl font-extrabold text-gray-900 mb-2">
                            {qrReauthTarget ? '重新授权账号' : '扫码添加账号'}
                          </h3>
                          <p className="text-gray-500 mb-8 font-medium">
                            {qrReauthTarget
                              ? `请用闲鱼APP扫码，为「${qrReauthTarget.nickname || qrReauthTarget.remark || qrReauthTarget.id}」刷新授权`
                              : '请打开闲鱼APP扫描下方二维码'}
                          </p>

						  <div className={`w-full bg-surface-subtle rounded-xl mx-auto flex items-center justify-center overflow-hidden border-4 border-white shadow-inner mb-6 relative ${qrStatus === 'verification' ? 'max-w-72 min-h-64 h-auto p-2' : 'max-w-64 aspect-square'}`}>
                              {qrStatus === 'loading' && <Loader2 className="w-10 h-10 text-brand animate-spin" />}
                              {qrStatus === 'waiting' && <SquareQRCode src={qrCodeUrl} alt="闲鱼登录二维码" className="p-2" />}
                              {qrStatus === 'success' && (
                                  <div className="absolute inset-0 bg-white/95 flex flex-col items-center justify-center text-green-600 animate-fade-in">
                                      <div className="w-16 h-16 bg-green-100 rounded-full flex items-center justify-center mb-4">
                                         <Check className="w-8 h-8" />
                                      </div>
                                      <span className="font-bold text-lg">登录成功</span>
                                  </div>
                              )}
							  {qrStatus === 'error' && (
								  <div className="px-5 text-center">
									  <span className="block text-red-600 font-bold">二维码获取失败</span>
									  <span className="mt-2 block text-xs leading-5 text-gray-500">{qrErrorMessage || '请关闭窗口后重新发起扫码登录。'}</span>
								  </div>
							  )}
							  {qrStatus === 'verification' && (
									  <RiskVerificationPanel faceQrUrl={faceQrUrl} verificationScreenshot={verificationScreenshot} />
							  )}
                          </div>

					  {qrStatus !== 'verification' && <p className="text-xs text-gray-400 font-medium bg-gray-50 py-2 rounded-xl">二维码有效期为5分钟，请尽快扫码。</p>}
                      </div>
                  </div>
              </div>
          </div>,
          document.body
      )}

      {/* 编辑账号弹窗由 accounts feature 组件负责渲染和表单交互。 */}
      {activeModal === 'edit' && editingAccount && (
        <AccountEditModal
          account={editingAccount}
          editForm={editForm}
          setEditForm={setEditForm}
          saving={saving}
          onClose={closeEditModal}
          onSave={handleSaveEdit}
          onRestartPause={handleRestartPause}
          longLogin={longLogin}
          onToggleLongLogin={handleLongLoginToggle}
          passwordLoginView={passwordLoginView}
          onPasswordLogin={handlePasswordLogin}
          onCancelPasswordLogin={handleCancelPasswordLogin}
          notifChannels={notifChannels}
          selectedChannelIds={selectedChannelIds}
          bindingsLoaded={bindingsLoaded}
          bindingsLoading={bindingsLoading}
          bindingsLoadError={bindingsLoadError}
          onRetryBindings={() => loadNotificationBindings(editingAccount.id)}
          onToggleChannel={toggleNotificationChannel}
          onSettingsDirty={() => setBindingsDirty(true)}
        />
      )}


      {/* AI设置弹窗 */}
      {activeModal === 'ai-settings' && editingAccount && createPortal(
        <div className="modal-overlay-centered">
          <div className="modal-container" style={{maxWidth: '600px'}}>
            <div className="modal-header">
              <div>
                <h3 className="text-2xl font-extrabold text-gray-900 flex items-center gap-2">
                  <Bot className="w-6 h-6 text-purple-500" />
                  AI助手设置
                </h3>
                <p className="text-sm text-gray-500 mt-1">{editingAccount.nickname || editingAccount.remark || editingAccount.id}</p>
              </div>
              <button
				onClick={closeAIModal}
                className="p-2 rounded-xl hover:bg-gray-100 transition-colors flex-shrink-0"
              >
                <X className="w-5 h-5 text-gray-500" />
              </button>
            </div>

            <div className="modal-body space-y-6">
              {/* 启用AI */}
              <div className="flex items-center justify-between p-4 bg-purple-50 rounded-xl">
                <div>
                  <div className="font-bold text-gray-900 flex items-center gap-2">
                    <Bot className="w-4 h-4 text-purple-500" />
                    启用AI自动回复
                  </div>
                  <div className="text-xs text-gray-500">AI将自动处理买家的砍价消息</div>
                </div>
                <button
                  type="button"
                  onClick={() => setAiSettings({ ...aiSettings, ai_enabled: !aiSettings.ai_enabled })}
                  className={`w-14 h-8 rounded-full transition-colors duration-300 relative ${
                    aiSettings.ai_enabled ? 'bg-brand' : 'bg-gray-300'
                  }`}
                >
                  <span
                    className={`absolute left-1 top-1 w-6 h-6 bg-white rounded-full shadow-md transition-transform duration-300 ${
                      aiSettings.ai_enabled ? 'translate-x-6' : 'translate-x-0'
                    }`}
                  />
                </button>
              </div>

              {/* 砍价策略 */}
              <div className="border-t border-gray-200 pt-6">
                <h3 className="text-lg font-bold text-gray-900 mb-4">砍价策略</h3>
                <div className="grid grid-cols-3 gap-4">
                  <div>
                    <label className="block text-sm font-bold text-gray-700 mb-2">最大折扣比例 (%)</label>
                    <input
                      type="number"
                      value={aiSettings.max_discount_percent}
                      onChange={(e) => setAiSettings({ ...aiSettings, max_discount_percent: parseInt(e.target.value) || 0 })}
                      className="w-full ios-input px-4 py-3 rounded-xl"
                      min="0"
                      max="100"
                    />
                    <p className="text-xs text-gray-500 mt-1">例如：10 表示最多降价 10%；设为 0 表示不允许降价</p>
                  </div>
                  <div>
                    <label className="block text-sm font-bold text-gray-700 mb-2">最大折扣金额 (元)</label>
                    <input
                      type="number"
                      value={aiSettings.max_discount_amount}
                      onChange={(e) => setAiSettings({ ...aiSettings, max_discount_amount: parseInt(e.target.value) || 0 })}
                      className="w-full ios-input px-4 py-3 rounded-xl"
                      min="0"
                    />
                    <p className="text-xs text-gray-500 mt-1">例如：100 表示最多降价 100 元；设为 0 表示不允许降价</p>
                  </div>
                  <div>
                    <label className="block text-sm font-bold text-gray-700 mb-2">最大砍价轮次</label>
                    <input
                      type="number"
                      value={aiSettings.max_bargain_rounds}
                      onChange={(e) => setAiSettings({ ...aiSettings, max_bargain_rounds: parseInt(e.target.value) || 1 })}
                      className="w-full ios-input px-4 py-3 rounded-xl"
                      min="1"
                      max="10"
                    />
                    <p className="text-xs text-gray-500 mt-1">买家最多可以砍价的次数</p>
                  </div>
                </div>
              </div>

              {/* 自定义提示词 */}
              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">自定义提示词（可选）</label>
                <textarea
                  value={aiSettings.custom_prompts}
                  onChange={(e) => setAiSettings({ ...aiSettings, custom_prompts: e.target.value })}
                  placeholder="输入自定义的AI回复规则或风格指引...&#10;&#10;例如：回复时保持礼貌专业、使用简洁的语言、强调产品质量等"
                  className="w-full ios-input px-4 py-3 rounded-xl h-40 resize-none"
                />
              </div>

              {/* AI如何工作 */}
              <div className="bg-blue-50 border border-blue-200 rounded-xl p-4">
                <h4 className="font-bold text-blue-900 mb-2 flex items-center gap-2">
                  <Settings className="w-4 h-4" />
                  AI如何工作
                </h4>
                <ul className="text-xs text-blue-800 space-y-1">
                  <li>• 自动识别买家的砍价请求</li>
                  <li>• 根据设定的策略智能回复</li>
                  <li>• 在合理范围内同意降价或礼貌拒绝</li>
                  <li>• 保持专业友好的沟通风格</li>
                </ul>
              </div>
            </div>

            <div className="modal-footer">
              <div className="flex gap-3 w-full">
                <button
				  onClick={closeAIModal}
                  className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors"
                  disabled={saving}
                >
                  取消
                </button>
                <button
                  onClick={handleSaveAISettings}
                  className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2"
                  disabled={saving}
                >
                  {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
                  {saving ? '保存中...' : '保存'}
                </button>
              </div>
            </div>
          </div>
        </div>,
        document.body
      )}
    </div>
  );
};

export default AccountList;
