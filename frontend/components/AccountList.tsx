import React, { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { AccountDetail, AIReplySettings, NotificationChannel } from '../types';
import {
  getAccountDetails,
  getAccountRuntimeStatuses,
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
  getAllAISettings,
  getAccountAISettings,
  getNotificationChannels,
  getAccountBindings,
} from '../services/api';
import { shouldUpdateAccountPause } from './accountPause';
import {
  Plus, Power, Edit2, Trash2, QrCode, X, Check, Loader2,
  RefreshCw, Save, User, Clock, MessageCircle,
  Upload, Key, Eye, EyeOff, Bot, Settings, AlertCircle, Bell
} from 'lucide-react';
import { buildAccountLoginInfoUpdate } from './accountEdit';
import { shouldSaveNotificationBindings } from './accountBindings';
import { mergeAccountRuntimeStatuses } from './accountRuntimeState';
import { createLatestRequestGate, createQRLoginPoller } from './qrPolling';
import { RiskVerificationPanel } from './RiskVerificationPanel';
import { SquareQRCode } from './SquareQRCode';

type ModalType = 'edit' | 'ai-settings' | null;

const AccountList: React.FC = () => {
  const [accounts, setAccounts] = useState<AccountDetail[]>([]);
  const [loading, setLoading] = useState(true);
  const [accountSearch, setAccountSearch] = useState('');
  const [refreshingProfileId, setRefreshingProfileId] = useState<string>('');
  const [showQRModal, setShowQRModal] = useState(false);
  const [qrCodeUrl, setQrCodeUrl] = useState<string>('');
  const [qrStatus, setQrStatus] = useState<string>('pending');
  const [verificationScreenshot, setVerificationScreenshot] = useState<string>('');
  const [faceQrUrl, setFaceQrUrl] = useState<string>('');
  const [qrReauthTarget, setQrReauthTarget] = useState<AccountDetail | null>(null);
  const [activeModal, setActiveModal] = useState<ModalType>(null);
  const [editingAccount, setEditingAccount] = useState<AccountDetail | null>(null);

  // 通知渠道绑定（编辑弹窗用）
  const [notifChannels, setNotifChannels] = useState<NotificationChannel[]>([]);
  const [selectedChannelIds, setSelectedChannelIds] = useState<number[]>([]);
  const [bindingsLoaded, setBindingsLoaded] = useState(false);
  const [bindingsLoading, setBindingsLoading] = useState(false);
  const [bindingsDirty, setBindingsDirty] = useState(false);
  const [bindingsLoadError, setBindingsLoadError] = useState('');

  // 编辑表单状态
  const [editForm, setEditForm] = useState({
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
  const [passwordLoginView, setPasswordLoginView] = useState<{
    sessionId: string;
    status: 'idle' | 'processing' | 'verification_required' | 'success' | 'failed';
    message: string;
    qrCodeUrl: string;
  }>({ sessionId: '', status: 'idle', message: '', qrCodeUrl: '' });
  const qrPollerRef = useRef<ReturnType<typeof createQRLoginPoller> | null>(null);
  const qrRequestGateRef = useRef<ReturnType<typeof createLatestRequestGate> | null>(null);
  const accountLoadGateRef = useRef<ReturnType<typeof createLatestRequestGate> | null>(null);
  const bindingsLoadGateRef = useRef<ReturnType<typeof createLatestRequestGate> | null>(null);
  const aiLoadGateRef = useRef<ReturnType<typeof createLatestRequestGate> | null>(null);
  const accountLoadAbortRef = useRef<AbortController | null>(null);
  const bindingsLoadAbortRef = useRef<AbortController | null>(null);
  const aiLoadAbortRef = useRef<AbortController | null>(null);
  const qrGenerateAbortRef = useRef<AbortController | null>(null);
  const passwordPollAbortRef = useRef<AbortController | null>(null);
  const qrCloseTimerRef = useRef<number | null>(null);
  const passwordPollTimerRef = useRef<number | null>(null);
  const passwordPollGenerationRef = useRef(0);
  if (qrPollerRef.current === null) {
    qrPollerRef.current = createQRLoginPoller();
  }
  if (qrRequestGateRef.current === null) {
    qrRequestGateRef.current = createLatestRequestGate();
  }
  if (accountLoadGateRef.current === null) accountLoadGateRef.current = createLatestRequestGate();
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

  const loadAccounts = async () => {
	const generation = accountLoadGateRef.current!.next();
	accountLoadAbortRef.current?.abort();
	const controller = new AbortController();
	accountLoadAbortRef.current = controller;
    setLoading(true);
    try {
	  const options = { signal: controller.signal };
	  const [detailsResult, aiResult] = await Promise.allSettled([getAccountDetails(options), getAllAISettings(options)]);
	  if (!accountLoadGateRef.current?.isCurrent(generation)) return;
	  if (detailsResult.status === 'rejected') throw detailsResult.reason;
	  const data = detailsResult.value;
	  const allAISettings = aiResult.status === 'fulfilled' ? aiResult.value : {};
	  if (aiResult.status === 'rejected') console.error('Failed to load AI settings:', aiResult.reason);

      // 合并AI设置到账号数据
      const accountsWithAI = data.map(account => ({
        ...account,
        ai_enabled: allAISettings[account.id]?.ai_enabled ?? false,
        max_discount_percent: allAISettings[account.id]?.max_discount_percent ?? 10,
        max_discount_amount: allAISettings[account.id]?.max_discount_amount ?? 100,
        max_bargain_rounds: allAISettings[account.id]?.max_bargain_rounds ?? 3,
        custom_prompts: allAISettings[account.id]?.custom_prompts ?? '',
      }));

	  if (accountLoadGateRef.current?.isCurrent(generation)) setAccounts(accountsWithAI);
    } catch (error) {
	  if (accountLoadGateRef.current?.isCurrent(generation) && !controller.signal.aborted) {
		console.error('Failed to load accounts:', error);
	  }
    } finally {
	  if (accountLoadGateRef.current?.isCurrent(generation)) setLoading(false);
    }
  };

  useEffect(() => {
    let cancelled = false;
    let timer: number | null = null;
	const runtimeController = new AbortController();

    const pollRuntimeStatuses = async () => {
      try {
		const statuses = await getAccountRuntimeStatuses({ signal: runtimeController.signal, timeoutMs: 10_000 });
        if (!cancelled) {
          setAccounts(current => mergeAccountRuntimeStatuses(current, statuses));
        }
      } catch (error) {
        if (!cancelled) console.error('Failed to load account runtime statuses:', error);
      } finally {
        if (!cancelled) {
          // 运行时状态只读取本地内存；短轮询让风控恢复提示在约 2 秒内收敛。
          timer = window.setTimeout(() => void pollRuntimeStatuses(), 2_000);
        }
      }
    };

    void loadAccounts().finally(() => {
      if (!cancelled) void pollRuntimeStatuses();
    });
    return () => {
      cancelled = true;
	  runtimeController.abort();
      if (timer !== null) window.clearTimeout(timer);
    };
  }, []);

  useEffect(() => {
    return () => {
      stopQRPolling();
      qrRequestGateRef.current?.cancel();
	  accountLoadGateRef.current?.cancel();
	  bindingsLoadGateRef.current?.cancel();
	  aiLoadGateRef.current?.cancel();
	  accountLoadAbortRef.current?.abort();
	  bindingsLoadAbortRef.current?.abort();
	  aiLoadAbortRef.current?.abort();
	  qrGenerateAbortRef.current?.abort();
	  passwordPollAbortRef.current?.abort();
      clearQRCloseTimer();
      passwordPollGenerationRef.current += 1;
      clearPasswordPollTimer();
    };
  }, []);

  const runtimePresentation = (account: AccountDetail) => {
    if (!account.enabled || account.runtime_state === 'disabled') {
      return { label: '已停用', badge: 'bg-gray-100 text-gray-500', dot: 'bg-gray-300' };
    }
    switch (account.runtime_state) {
      case 'online':
        return { label: '在线', badge: 'bg-green-100 text-green-700', dot: 'bg-green-500' };
      case 'starting':
      case 'connecting':
        return { label: '连接中', badge: 'bg-blue-100 text-blue-700', dot: 'bg-blue-500' };
      case 'reconnecting':
        return { label: '重连中', badge: 'bg-amber-100 text-amber-700', dot: 'bg-amber-500' };
      case 'auth_expired':
        return { label: '登录已失效', badge: 'bg-red-100 text-red-700', dot: 'bg-red-500' };
      case 'verification_required':
        return { label: '需要验证', badge: 'bg-orange-100 text-orange-700', dot: 'bg-orange-500' };
      case 'error':
      case 'stopped':
        return { label: '运行异常', badge: 'bg-red-100 text-red-700', dot: 'bg-red-500' };
      default:
        return { label: '状态检测中', badge: 'bg-gray-100 text-gray-600', dot: 'bg-gray-400' };
    }
  };

  const handleToggle = async (id: string, currentStatus: boolean) => {
    await updateAccountStatus(id, !currentStatus);
    loadAccounts();
  };

  const handleDelete = async (id: string) => {
    if (confirm('确认删除该账号吗？')) {
      await deleteAccount(id);
      loadAccounts();
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

  const openEditModal = async (account: AccountDetail) => {
    passwordPollGenerationRef.current += 1;
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
    await loadNotificationBindings(account.id);
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

  const pollPasswordLogin = async (sessionId: string, generation: number) => {
	passwordPollAbortRef.current?.abort();
	const controller = new AbortController();
	passwordPollAbortRef.current = controller;
    try {
	  const result = await checkPasswordLoginStatus(sessionId, controller.signal);
      if (generation !== passwordPollGenerationRef.current) return;
      if (result.status === 'success') {
        clearPasswordPollTimer();
        setPasswordLoginView({
          sessionId,
          status: 'success',
          message: result.message || '账号密码登录成功，授权信息已更新',
          qrCodeUrl: '',
        });
        setEditForm(current => ({ ...current, login_password: '', showLoginPassword: false }));
        await loadAccounts();
        return;
      }
      if (result.status === 'processing' || result.status === 'verification_required') {
        setPasswordLoginView({
          sessionId,
          status: result.status,
          message: result.message || (result.status === 'verification_required' ? '账号触发风控，需要完成人脸识别' : '登录处理中'),
          qrCodeUrl: result.qr_code_url || '',
        });
        clearPasswordPollTimer();
        passwordPollTimerRef.current = window.setTimeout(() => void pollPasswordLogin(sessionId, generation), 1500);
        return;
      }
      clearPasswordPollTimer();
      setPasswordLoginView({
        sessionId,
        status: 'failed',
        message: result.error || result.message || '密码登录失败，请检查账号信息后重试',
        qrCodeUrl: '',
      });
    } catch (error) {
      if (generation !== passwordPollGenerationRef.current) return;
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
    setPasswordLoginView({ sessionId: '', status: 'processing', message: '正在启动密码登录…', qrCodeUrl: '' });
    try {
      const result = await passwordLogin({
        account_id: editingAccount.id,
        account,
        password: editForm.login_password,
        show_browser: editForm.show_browser,
      });
      if (generation !== passwordPollGenerationRef.current) return;
      if (!result.success || !result.session_id) {
        throw new Error(result.message || '无法启动密码登录');
      }
      setPasswordLoginView({ sessionId: result.session_id, status: 'processing', message: result.message || '登录处理中', qrCodeUrl: '' });
      await pollPasswordLogin(result.session_id, generation);
    } catch (error) {
      if (generation !== passwordPollGenerationRef.current) return;
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
    setQrCodeUrl('');
    setVerificationScreenshot('');
    setFaceQrUrl('');
    try {
	  const res = await generateQRLogin({ signal: controller.signal });
      if (!qrRequestGateRef.current?.isCurrent(requestGeneration)) return;
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
      setQrStatus('error');
    }
  };

  if (loading) return <div className="p-20 flex justify-center"><Loader2 className="w-8 h-8 text-[#0094f7] animate-spin"/></div>;

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
          const runtime = runtimePresentation(account);
          const requiresLogin = account.runtime_state === 'auth_expired' || account.runtime_state === 'verification_required';
          return (
          <div key={account.id} className="ios-card p-6 rounded-xl flex flex-col lg:flex-row lg:items-center justify-between gap-5 group hover:border-[#0094f7] transition-all duration-300">
            <div className="flex items-center gap-5 sm:gap-8 min-w-0">
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
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-3 mb-1">
                    <h3 className="text-xl font-extrabold text-gray-900 break-words">{account.nickname || account.remark || `账号 ${account.id.substring(0,6)}...`}</h3>
                    <span className={`px-2.5 py-0.5 rounded-lg text-xs font-bold ${runtime.badge}`}>{runtime.label}</span>
                    {account.ai_enabled && (
                        <span className="px-2.5 py-0.5 rounded-lg bg-purple-100 text-purple-700 text-xs font-bold flex items-center gap-1">
                          <Bot className="w-3 h-3" /> AI
                        </span>
                    )}
                    {account.profile_error && (
                        <span
                          className="px-2.5 py-0.5 rounded-lg bg-amber-100 text-amber-700 text-xs font-bold flex items-center gap-1"
                          title={account.profile_error}
                        >
                          <AlertCircle className="w-3 h-3" /> 资料未同步
                        </span>
                    )}
                </div>
                <div className="text-sm text-gray-500 font-medium mb-3 space-y-1">
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
                <div className="flex gap-2">
                   {account.auto_confirm && <span className="text-xs bg-blue-50 text-blue-700 px-3 py-1.5 rounded-lg font-bold flex items-center gap-1.5"><Check className="w-3 h-3"/> 自动确认发货</span>}
                   {account.paused && <span className="text-xs bg-blue-50 text-blue-700 px-3 py-1.5 rounded-lg font-bold flex items-center gap-1.5"><Clock className="w-3 h-3"/> 暂停处理中</span>}
                </div>
              </div>
            </div>
            <div className="flex items-center gap-3 self-end lg:self-auto flex-shrink-0">
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
                    onClick={() => handleToggle(account.id, account.enabled)}
                  className={`p-3 rounded-xl transition-colors ${account.enabled ? 'text-green-600 hover:bg-green-50' : 'text-gray-400 hover:bg-gray-100'}`}
                  title={account.enabled ? '停用账号' : '启用账号'}
                >
                    <Power className="w-5 h-5" />
                </button>
                <button
                    onClick={() => handleDelete(account.id)}
                    className="p-3 rounded-xl hover:bg-red-100 transition-colors text-red-500"
                >
                    <Trash2 className="w-5 h-5" />
                </button>
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

						  <div className={`w-full bg-[#F7F8FA] rounded-xl mx-auto flex items-center justify-center overflow-hidden border-4 border-white shadow-inner mb-6 relative ${qrStatus === 'verification' ? 'max-w-72 min-h-64 h-auto p-2' : 'max-w-64 aspect-square'}`}>
                              {qrStatus === 'loading' && <Loader2 className="w-10 h-10 text-[#0094f7] animate-spin" />}
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
									  <span className="mt-2 block text-xs leading-5 text-gray-500">请关闭窗口后重新发起扫码登录。</span>
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

      {/* 编辑账号弹窗 */}
      {activeModal === 'edit' && editingAccount && createPortal(
        <div className="modal-overlay-centered">
          <div className="modal-container" style={{maxWidth: '600px'}}>
            <div className="modal-header">
              <div>
                <h3 className="text-2xl font-extrabold text-gray-900">编辑账号</h3>
                <p className="text-sm text-gray-500 mt-1">{editingAccount.nickname || editingAccount.remark || editingAccount.id}</p>
              </div>
              <button
				onClick={() => void closeEditModal()}
                className="p-2 rounded-xl hover:bg-gray-100 transition-colors flex-shrink-0"
              >
                <X className="w-5 h-5 text-gray-500" />
              </button>
            </div>

            <div className="modal-body space-y-6">
              {/* 账号ID */}
              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">账号ID</label>
                <input
                  type="text"
                  value={editingAccount.id}
                  disabled
                  className="w-full ios-input px-4 py-3 rounded-xl bg-gray-50 text-gray-500"
                />
              </div>

              {/* 备注 */}
              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">备注</label>
                <input
                  type="text"
                  value={editForm.remark}
                  onChange={(e) => setEditForm({ ...editForm, remark: e.target.value })}
                  placeholder="为账号添加备注"
                  className="w-full ios-input px-4 py-3 rounded-xl"
                />
              </div>

              {/* Cookie */}
              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">Cookie</label>
                <textarea
                  value={editForm.cookie}
                  onChange={(e) => setEditForm({ ...editForm, cookie: e.target.value })}
                  placeholder="更新账号Cookie"
                  className="w-full ios-input px-4 py-3 rounded-xl h-32 resize-none font-mono text-xs"
                />
                <p className="text-xs text-gray-500 mt-1">当前Cookie长度: {editForm.cookie.length} 字符</p>
              </div>

              {/* 自动确认发货 */}
              <div className="flex items-center justify-between p-4 bg-gray-50 rounded-xl">
                <div>
                  <div className="font-bold text-gray-900 flex items-center gap-2">
                    <Check className="w-4 h-4 text-green-500" />
                    自动确认发货
                  </div>
                  <div className="text-xs text-gray-500">自动将闲鱼订单标记为已发货</div>
                </div>
                <button
                  type="button"
                  onClick={() => setEditForm({ ...editForm, auto_confirm: !editForm.auto_confirm })}
                  className={`w-14 h-8 rounded-full transition-colors duration-300 relative ${
                    editForm.auto_confirm ? 'bg-[#0094f7]' : 'bg-gray-300'
                  }`}
                >
                  <span
                    className={`absolute left-1 top-1 w-6 h-6 bg-white rounded-full shadow-md transition-transform duration-300 ${
                      editForm.auto_confirm ? 'translate-x-6' : 'translate-x-0'
                    }`}
                  />
                </button>
              </div>

              {/* 暂停时长 */}
              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2 flex items-center gap-2">
                  <Clock className="w-4 h-4 text-blue-500" />
                  暂停处理时长（分钟）
                </label>
                <input
                  type="number"
                  value={editForm.pause_duration}
                  onChange={(e) => setEditForm({ ...editForm, pause_duration: parseInt(e.target.value) || 0 })}
                  placeholder="0"
                  min="0"
                  max="1440"
                  className="w-full ios-input px-4 py-3 rounded-xl"
                />
                <p className="text-xs text-gray-500 mt-1">设置后会暂停处理该账号的订单，到时间后自动恢复</p>
                {editForm.pause_duration > 0 && !editingAccount.paused && editForm.pause_duration === (editingAccount.pause_duration || 0) && (
                  <button
                    type="button"
                    disabled={saving}
                    onClick={() => void handleRestartPause()}
                    className="mt-3 px-4 py-2 rounded-xl bg-amber-50 text-amber-700 hover:bg-amber-100 text-sm font-bold disabled:opacity-50"
                  >
                    立即按当前时长重新暂停
                  </button>
                )}
              </div>

              {/* 登录信息 */}
              <div className="border-t border-gray-200 pt-6">
                <h3 className="text-lg font-bold text-gray-900 mb-4 flex items-center gap-2">
                  <Key className="w-5 h-5 text-blue-500" />
                  登录信息
                </h3>
                <div className="space-y-4">
                  <div>
                    <label className="block text-sm font-bold text-gray-700 mb-2">用户名</label>
                    <input
                      type="text"
                      value={editForm.username}
                      onChange={(e) => setEditForm({ ...editForm, username: e.target.value })}
                      placeholder="闲鱼账号/手机号"
                      className="w-full ios-input px-4 py-3 rounded-xl"
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-bold text-gray-700 mb-2">登录密码</label>
                    <div className="relative">
                      <input
                        type={editForm.showLoginPassword ? 'text' : 'password'}
                        value={editForm.login_password}
                        onChange={(e) => setEditForm({ ...editForm, login_password: e.target.value, clear_password: false })}
                        placeholder="用于自动登录"
                        className="w-full ios-input px-4 py-3 rounded-xl pr-12"
                      />
                      <button
                        type="button"
                        onClick={() => setEditForm({ ...editForm, showLoginPassword: !editForm.showLoginPassword })}
                        className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600"
                      >
                        {editForm.showLoginPassword ? <EyeOff className="w-5 h-5" /> : <Eye className="w-5 h-5" />}
                      </button>
                    </div>
                    <label className="mt-3 flex items-center gap-3 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={editForm.clear_password}
                        onChange={(e) => setEditForm({ ...editForm, clear_password: e.target.checked, login_password: e.target.checked ? '' : editForm.login_password })}
                        className="w-4 h-4 accent-[#0094f7]"
                      />
                      <span className="text-sm font-bold text-gray-700">清空已保存密码</span>
                    </label>
                  </div>
                  <div className="flex items-center justify-between">
                    <div>
                      <div className="font-bold text-gray-900">登录时显示浏览器</div>
                      <div className="text-xs text-gray-500">调试时可开启查看登录过程</div>
                    </div>
                    <button
                      type="button"
                      onClick={() => setEditForm({ ...editForm, show_browser: !editForm.show_browser })}
                      className={`w-14 h-8 rounded-full transition-colors duration-300 relative ${
                        editForm.show_browser ? 'bg-[#0094f7]' : 'bg-gray-300'
                      }`}
                    >
                      <span
                        className={`absolute left-1 top-1 w-6 h-6 bg-white rounded-full shadow-md transition-transform duration-300 ${
                          editForm.show_browser ? 'translate-x-6' : 'translate-x-0'
                        }`}
                      />
                    </button>
                  </div>
                  <div className="rounded-xl border border-blue-100 bg-blue-50 p-4 space-y-3">
                    <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
                      <div>
                        <div className="font-bold text-blue-950">立即执行账号密码登录</div>
                        <div className="text-xs text-blue-700 mt-1">需要在上方重新输入本次登录密码；成功后后端会更新 Cookie 和保存的登录信息。</div>
                      </div>
                      {(passwordLoginView.status === 'processing' || passwordLoginView.status === 'verification_required') ? (
                        <button type="button" onClick={handleCancelPasswordLogin} className="px-4 py-2 rounded-xl bg-white text-red-600 font-bold text-sm border border-red-100">
                          取消登录
                        </button>
                      ) : (
                        <button type="button" onClick={handlePasswordLogin} className="px-4 py-2 rounded-xl bg-blue-600 text-white font-bold text-sm whitespace-nowrap">
                          密码登录刷新授权
                        </button>
                      )}
                    </div>
                    {passwordLoginView.message && (
                      <div className={`text-sm font-medium ${passwordLoginView.status === 'failed' ? 'text-red-700' : passwordLoginView.status === 'success' ? 'text-green-700' : 'text-blue-800'}`}>
                        {passwordLoginView.message}
                      </div>
                    )}
                    {passwordLoginView.status === 'verification_required' && (
                      <div className="rounded-xl border border-amber-200 bg-amber-50 p-3 text-amber-900">
                        <div className="font-extrabold">账号已触发平台风控，需要完成人脸识别</div>
                        <div className="text-xs mt-1 leading-5">请在闲鱼 App 或已打开的登录浏览器中按提示完成验证。本页面不会提供可直接打开的风控链接。</div>
                        {passwordLoginView.qrCodeUrl && (
                          <div className="mt-3 aspect-square w-48 overflow-hidden rounded-xl border bg-white">
                            <SquareQRCode src={passwordLoginView.qrCodeUrl} alt="密码登录风控二维码" className="p-2" />
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                </div>
              </div>

              {/* 通知渠道绑定 */}
              {(notifChannels.length > 0 || bindingsLoading || bindingsLoadError) && (
                <div className="border-t border-gray-200 pt-6">
                  <h3 className="text-lg font-bold text-gray-900 mb-1 flex items-center gap-2">
                    <Bell className="w-5 h-5 text-blue-500" />
                    通知渠道绑定
                  </h3>
                  <p className="text-xs text-gray-500 mb-4">勾选后，该账号的 token 失效、自动恢复失败、风控验证等事件会推送到这些渠道</p>
                  {bindingsLoading && (
                    <div className="flex items-center gap-2 text-sm text-gray-500"><Loader2 className="w-4 h-4 animate-spin" />正在加载通知绑定</div>
                  )}
                  {bindingsLoadError && !bindingsLoading && (
                    <div className="mb-3 rounded-xl bg-amber-50 p-3 text-sm text-amber-800 flex items-center justify-between gap-3">
                      <span>{bindingsLoadError}</span>
                      <button type="button" className="font-bold whitespace-nowrap" onClick={() => loadNotificationBindings(editingAccount.id)}>重试</button>
                    </div>
                  )}
                  <div className="space-y-2">
                    {notifChannels.map(ch => {
                      const checked = selectedChannelIds.includes(Number(ch.id));
                      return (
                        <label
                          key={ch.id}
                          className="flex items-center gap-3 p-3 rounded-xl border border-gray-200 hover:bg-gray-50 cursor-pointer transition-colors"
                        >
                          <button
                            type="button"
                            onClick={() => {
                              if (!bindingsLoaded) return;
                              setSelectedChannelIds(prev =>
                                checked ? prev.filter(id => id !== Number(ch.id)) : [...prev, Number(ch.id)]
                              );
                              setBindingsDirty(true);
                            }}
                            disabled={!bindingsLoaded}
                            className={`w-5 h-5 rounded-md border-2 flex items-center justify-center transition-colors flex-shrink-0 ${
                              checked ? 'bg-[#0094f7] border-[#0094f7]' : 'bg-white border-gray-300'
                            }`}
                          >
                            {checked && <Check className="w-3.5 h-3.5 text-white" />}
                          </button>
                          <div className="flex-1 min-w-0">
                            <div className="font-bold text-gray-900 text-sm">{ch.name}</div>
                            <div className="text-xs text-gray-500">{ch.type}{ch.enabled ? '' : ' · 已停用'}</div>
                          </div>
                        </label>
                      );
                    })}
                  </div>
                </div>
              )}
            </div>

            <div className="modal-footer">
              <div className="flex gap-3 w-full">
                <button
				  onClick={() => void closeEditModal()}
                  className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors"
                  disabled={saving}
                >
                  取消
                </button>
                <button
                  onClick={handleSaveEdit}
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
                    aiSettings.ai_enabled ? 'bg-[#0094f7]' : 'bg-gray-300'
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
