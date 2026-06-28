import React, { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { AccountDetail, AIReplySettings } from '../types';
import {
  getAccountDetails,
  getAccountRuntimeStatuses,
  addAccount,
  updateAccountStatus,
  deleteAccount,
  generateQRLogin,
  checkQRLoginStatus,
  completeQRVerification,
  updateAccountRemark,
  updateAccountAutoConfirm,
  updateAccountPauseDuration,
  updateAccountCookie,
  refreshAccountProfile,
  updateAccountLoginInfo,
  updateAccountAISettings,
  getAllAISettings,
  getAccountAISettings
} from '../services/api';
import {
  Plus, Power, Edit2, Trash2, QrCode, X, Check, Loader2,
  RefreshCw, Save, User, Clock, MessageCircle,
  Upload, Key, Eye, EyeOff, Bot, Settings, AlertCircle, ExternalLink
} from 'lucide-react';

type ModalType = 'edit' | 'ai-settings' | null;

const AccountList: React.FC = () => {
  const [accounts, setAccounts] = useState<AccountDetail[]>([]);
  const [loading, setLoading] = useState(true);
  const [accountSearch, setAccountSearch] = useState('');
  const [refreshingProfileId, setRefreshingProfileId] = useState<string>('');
  const [showQRModal, setShowQRModal] = useState(false);
  const [qrCodeUrl, setQrCodeUrl] = useState<string>('');
  const [qrStatus, setQrStatus] = useState<string>('pending');
  const [verificationUrl, setVerificationUrl] = useState<string>('');
  const [verificationScreenshot, setVerificationScreenshot] = useState<string>('');
  const [qrSessionId, setQrSessionId] = useState<string>('');
  const [qrReauthTarget, setQrReauthTarget] = useState<AccountDetail | null>(null);
  const [activeModal, setActiveModal] = useState<ModalType>(null);
  const [editingAccount, setEditingAccount] = useState<AccountDetail | null>(null);

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

  const refreshRuntimeStatuses = async () => {
    try {
      const statuses = await getAccountRuntimeStatuses();
      setAccounts(current => current.map(account => {
        const status = statuses[account.id];
        if (!status) return account;
        return {
          ...account,
          runtime_state: status.state,
          runtime_message: status.message || '',
          runtime_connected: status.connected,
          runtime_updated_at: status.updated_at,
        };
      }));
    } catch (error) {
      console.error('Failed to load account runtime statuses:', error);
    }
  };

  const loadAccounts = async () => {
    setLoading(true);
    try {
      const data = await getAccountDetails();

      // 获取所有账号的AI设置
      let allAISettings: Record<string, AIReplySettings> = {};
      try {
        allAISettings = await getAllAISettings();
      } catch (e) {
        console.error('Failed to load AI settings:', e);
      }

      // 合并AI设置到账号数据
      const accountsWithAI = data.map(account => ({
        ...account,
        ai_enabled: allAISettings[account.id]?.ai_enabled ?? false,
        max_discount_percent: allAISettings[account.id]?.max_discount_percent ?? 10,
        max_discount_amount: allAISettings[account.id]?.max_discount_amount ?? 100,
        max_bargain_rounds: allAISettings[account.id]?.max_bargain_rounds ?? 3,
        custom_prompts: allAISettings[account.id]?.custom_prompts ?? '',
      }));

      setAccounts(accountsWithAI);
      window.setTimeout(refreshRuntimeStatuses, 0);
    } catch (error) {
      console.error('Failed to load accounts:', error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadAccounts();
    const timer = window.setInterval(refreshRuntimeStatuses, 10_000);
    return () => window.clearInterval(timer);
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

  const openEditModal = (account: AccountDetail) => {
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
    });
    setActiveModal('edit');
  };

  const openAIModal = async (account: AccountDetail) => {
    setEditingAccount(account);
    setSaving(true);
    try {
      const settings = await getAccountAISettings(account.id);
      setAiSettings({
        ai_enabled: settings.ai_enabled ?? false,
        max_discount_percent: settings.max_discount_percent ?? 10,
        max_discount_amount: settings.max_discount_amount ?? 100,
        max_bargain_rounds: settings.max_bargain_rounds ?? 3,
        custom_prompts: settings.custom_prompts ?? '',
      });
    } catch (e) {
      console.error('Failed to load AI settings:', e);
    } finally {
      setSaving(false);
    }
    setActiveModal('ai-settings');
  };

  const handleSaveEdit = async () => {
    if (!editingAccount) return;
    setSaving(true);

    try {
      const promises: Promise<any>[] = [];

      // 更新备注
      if (editForm.remark !== (editingAccount.remark || editingAccount.note || '')) {
        promises.push(updateAccountRemark(editingAccount.id, editForm.remark));
      }

      // 更新Cookie
      if (editForm.cookie && editForm.cookie !== (editingAccount.cookie || editingAccount.value || '')) {
        promises.push(updateAccountCookie(editingAccount.id, editForm.cookie));
      }

      // 更新自动确认
      if (editForm.auto_confirm !== editingAccount.auto_confirm) {
        promises.push(updateAccountAutoConfirm(editingAccount.id, editForm.auto_confirm));
      }

      // 更新暂停时长
      if (editForm.pause_duration !== (editingAccount.pause_duration || 0)) {
        promises.push(updateAccountPauseDuration(editingAccount.id, editForm.pause_duration));
      }

      // 更新登录信息
      if (
        editForm.username !== (editingAccount.username || '') ||
        editForm.login_password !== (editingAccount.login_password || '') ||
        editForm.show_browser !== (editingAccount.show_browser || false)
      ) {
        promises.push(updateAccountLoginInfo(editingAccount.id, {
          username: editForm.username,
          login_password: editForm.login_password,
          show_browser: editForm.show_browser,
        }));
      }

      await Promise.all(promises);
      setActiveModal(null);
      loadAccounts();
    } catch (error) {
      console.error('更新账号失败:', error);
      alert('更新失败，请重试');
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

  const persistQRLoginResult = async (cookies: string, unb?: string, target?: AccountDetail | null) => {
    if (target) {
      if (unb && unb !== target.id) {
        const ok = confirm(`扫码返回的账号ID是 ${unb}，当前要重新授权的是 ${target.id}。确认用本次扫码结果覆盖当前账号授权吗？`);
        if (!ok) {
          throw new Error('已取消覆盖当前账号授权');
        }
      }
      await updateAccountCookie(target.id, cookies);
      return target.id;
    }
    if (!unb) {
      throw new Error('扫码结果缺少账号ID，无法添加账号');
    }
    await addAccount(unb, cookies);
    return unb;
  };

  const startQRLogin = async (target?: AccountDetail) => {
    const targetAccount = target || null;
    setQrReauthTarget(targetAccount);
    setShowQRModal(true);
    setQrStatus('loading');
    setQrCodeUrl('');
    setQrSessionId('');
    setVerificationUrl('');
    setVerificationScreenshot('');
    try {
      const res = await generateQRLogin();
      if (res.success && res.qr_code_url && res.session_id) {
        setQrCodeUrl(res.qr_code_url);
        setQrSessionId(res.session_id);
        setQrStatus('waiting');

        const interval = setInterval(async () => {
          const statusRes = await checkQRLoginStatus(res.session_id!);
          if (statusRes.status === 'success') {
            clearInterval(interval);
            setQrStatus('success');
            if (statusRes.cookies && statusRes.unb) {
              try {
                await persistQRLoginResult(statusRes.cookies, statusRes.unb, targetAccount);
              } catch (e) {
                console.error('保存扫码授权失败', e);
                setQrStatus('error');
              }
            }
            setTimeout(() => {
              setShowQRModal(false);
              loadAccounts();
            }, 1000);
          } else if (statusRes.status === 'scanned') {
            setQrStatus('waiting'); // 已扫描，继续等待确认
          } else if (statusRes.status === 'expired' || statusRes.status === 'cancelled' || statusRes.status === 'error' || statusRes.status === 'not_found') {
            clearInterval(interval);
            setQrStatus('error');
          } else if (statusRes.status === 'verification_required') {
            setQrStatus('verification');
            setVerificationUrl(statusRes.verification_url || '');
            if (statusRes.verification_screenshot) {
              setVerificationScreenshot(statusRes.verification_screenshot);
            }
          }
        }, 2000);
      }
    } catch (e) {
      setQrStatus('error');
    }
  };

  // 用户完成风控验证后，点"我已完成验证"调用后端提取真实 cookie。
  const handleCompleteVerification = async () => {
    if (!qrSessionId) return;
    setQrStatus('loading');
    try {
      const res = await completeQRVerification(qrSessionId);
      if (res.success && res.cookies && res.unb) {
        await persistQRLoginResult(res.cookies, res.unb, qrReauthTarget);
        setQrStatus('success');
        setTimeout(() => {
          setShowQRModal(false);
          loadAccounts();
        }, 1000);
      } else {
        setQrStatus('verification');
        alert('验证未完成：' + (res.message || '可能验证尚未通过，请先在验证页面完成验证'));
      }
    } catch (e) {
      setQrStatus('verification');
      alert('处理失败：' + (e instanceof Error ? e.message : String(e)));
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
          <div key={account.id} className="ios-card p-6 rounded-[2rem] flex flex-col lg:flex-row lg:items-center justify-between gap-5 group hover:border-[#0094f7] transition-all duration-300">
            <div className="flex items-center gap-5 sm:gap-8 min-w-0">
              <div className="relative">
                {account.avatar_url ? (
                  <img
                    src={account.avatar_url}
                    alt={account.nickname || '账号头像'}
                    className="w-20 h-20 rounded-3xl object-cover shadow-md ring-4 ring-white bg-gray-100"
                  />
                ) : (
                  <div className="w-20 h-20 rounded-3xl bg-gray-100 text-gray-400 shadow-md ring-4 ring-white flex items-center justify-center">
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
                   {account.pause_duration > 0 && <span className="text-xs bg-blue-50 text-blue-700 px-3 py-1.5 rounded-lg font-bold flex items-center gap-1.5"><Clock className="w-3 h-3"/> 暂停{account.pause_duration}分钟</span>}
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
              <div className="modal-container relative" style={{maxWidth: '24rem'}}>
                  <button
                    onClick={() => setShowQRModal(false)}
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

                          <div className="w-64 h-64 bg-[#F7F8FA] rounded-[2rem] mx-auto flex items-center justify-center overflow-hidden border-4 border-white shadow-inner mb-8 relative">
                              {qrStatus === 'loading' && <Loader2 className="w-10 h-10 text-[#0094f7] animate-spin" />}
                              {qrStatus === 'waiting' && <img src={qrCodeUrl} alt="QR Code" className="w-full h-full p-2" />}
                              {qrStatus === 'success' && (
                                  <div className="absolute inset-0 bg-white/95 flex flex-col items-center justify-center text-green-600 animate-fade-in">
                                      <div className="w-16 h-16 bg-green-100 rounded-full flex items-center justify-center mb-4">
                                         <Check className="w-8 h-8" />
                                      </div>
                                      <span className="font-bold text-lg">登录成功</span>
                                  </div>
                              )}
                              {qrStatus === 'error' && (
                                  <div className="flex flex-col items-center">
                                      <span className="text-red-500 font-bold mb-2">获取失败</span>
                                      <button onClick={() => startQRLogin(qrReauthTarget || undefined)} className="text-xs bg-gray-200 px-3 py-1 rounded-full flex items-center gap-1 hover:bg-gray-300"><RefreshCw className="w-3 h-3"/> 重试</button>
                                  </div>
                              )}
                              {qrStatus === 'verification' && (
                                  <div className="flex flex-col items-center px-4">
                                      <span className="font-bold text-gray-900 mb-2">需要安全验证</span>
                                      <span className="text-xs text-gray-500 mb-3 text-center">
                                          程序已在后台打开验证页面，请用手机完成验证，验证通过后将自动登录。
                                      </span>
                                      {verificationScreenshot ? (
                                          <img src={verificationScreenshot} alt="验证页面" className="w-full rounded-xl border mb-2" style={{maxHeight: 200, objectFit: 'contain'}} />
                                      ) : (
                                          <div className="w-full h-32 bg-gray-100 rounded-xl flex items-center justify-center mb-2">
                                              <Loader2 className="w-6 h-6 animate-spin text-gray-400" />
                                          </div>
                                      )}
                                      <span className="text-xs text-gray-400">等待手机验证完成，无需手动操作...</span>
                                  </div>
                              )}
                          </div>

                          <p className="text-xs text-gray-400 font-medium bg-gray-50 py-2 rounded-xl">二维码有效期为5分钟，请尽快扫码。</p>
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
                onClick={() => setActiveModal(null)}
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
                        onChange={(e) => setEditForm({ ...editForm, login_password: e.target.value })}
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
                </div>
              </div>
            </div>

            <div className="modal-footer">
              <div className="flex gap-3 w-full">
                <button
                  onClick={() => setActiveModal(null)}
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
                onClick={() => setActiveModal(null)}
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
                    <p className="text-xs text-gray-500 mt-1">例如：10表示最多降价10%</p>
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
                    <p className="text-xs text-gray-500 mt-1">例如：100表示最多降价100元</p>
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
                  onClick={() => setActiveModal(null)}
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
