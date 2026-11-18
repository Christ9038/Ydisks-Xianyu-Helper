import { AlertCircle,Bot,CalendarClock,Check,ClipboardPaste,Clock,ExternalLink,Edit2,Loader2,MessageCircle,Power,QrCode,RefreshCw,Sparkles,Trash2,User } from 'lucide-react';
import React from 'react';
import type { AccountDetail } from '../api';
import { accountRuntimePresentation } from '../runtime';

// RISK_GUIDE_URL 是风控处理教程（issue 92）的公开链接，说明如何获取过风控的 Cookie。
const RISK_GUIDE_URL = 'https://github.com/Christ9038/Ydisks-Xianyu-Helper/issues/92';

// AccountCardProps 描述账号卡片展示所需的数据和动作。
export interface AccountCardProps {
  // account 是当前账号的非敏感展示数据。
  account: AccountDetail;
  // refreshing 表示当前账号是否正在刷新资料。
  refreshing: boolean;
  // deleting 表示当前账号是否正在删除。
  deleting: boolean;
  // onRefreshProfile 刷新账号昵称和头像。
  onRefreshProfile: (account: AccountDetail) => void | Promise<void>;
  // onReauthorize 启动当前账号二维码重新授权。
  onReauthorize: (account: AccountDetail) => void | Promise<void>;
  // onVerify 为续期被要求安全验证的账号启动人工验证；登记过期时由调用方回退到扫码授权。
  onVerify: (account: AccountDetail) => void | Promise<void>;
  // onEdit 打开账号编辑弹窗。
  onEdit: (account: AccountDetail) => void | Promise<void>;
  // onAI 打开账号 AI 设置弹窗。
  onAI: (account: AccountDetail) => void | Promise<void>;
  // onTasks 打开账号自动化任务弹窗。
  onTasks: (account: AccountDetail) => void;
  // onToggle 切换账号启用状态。
  onToggle: (id: string, currentStatus: boolean) => void | Promise<void>;
  // onDelete 打开账号删除确认框。
  onDelete: (account: AccountDetail) => void;
  // onOpenChat 跳转到在线聊天并优先选中当前账号；未注入时聊天按钮仍展示但不产生跳转。
  onOpenChat?: (accountID: string) => void;
}

// AccountCard 渲染单个账号的状态摘要、在线聊天入口和其余操作入口。
export const AccountCard = React.memo(/* AccountCard 负责渲染单个账号卡片及其操作。 */ function AccountCard({
  account,
  refreshing,
  deleting,
  onRefreshProfile,
  onReauthorize,
  onVerify,
  onEdit,
  onAI,
  onTasks,
  onToggle,
  onDelete,
  onOpenChat,
}: AccountCardProps) {
  // runtime 保存账号运行状态的展示样式。
  const runtime = accountRuntimePresentation(account);
  // requiresLogin 表示当前账号是否需要重新授权。
  const requiresLogin = account.runtime_state === 'auth_expired' || account.runtime_state === 'verification_required';

  return (
    <div className="ios-card rounded-xl p-4 group transition-all duration-300 hover:border-brand sm:p-6">
      <div className="flex min-w-0 items-start gap-4 sm:gap-8">
        <div className="relative">
          {account.avatar_url ? (
            <img
              src={account.avatar_url}
              alt={account.nickname || '账号头像'}
              className="w-16 h-16 rounded-full object-cover shadow-md ring-4 ring-white bg-gray-100 sm:w-20 sm:h-20"
            />
          ) : (
            <div className="w-16 h-16 rounded-full bg-gray-100 text-gray-400 shadow-md ring-4 ring-white flex items-center justify-center sm:w-20 sm:h-20">
              <User className="w-9 h-9" />
            </div>
          )}
          <div className={`absolute -bottom-1 -right-1 w-6 h-6 rounded-full border-4 border-white flex items-center justify-center ${runtime.dot}`}>
            {account.runtime_state === 'online' && <Check className="w-3 h-3 text-white" />}
          </div>
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2.5 mb-1">
            <h3 className="text-xl font-extrabold text-gray-900 break-words">{account.nickname || account.remark || `账号 ${account.id.substring(0, 6)}...`}</h3>
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
            {account.auto_confirm && <span className="flex items-center gap-1 rounded-lg bg-blue-50 px-2.5 py-0.5 text-xs font-bold text-blue-700"><Check className="h-3 w-3" /> 自动发货</span>}
            {account.profile_error && (
              <span className="px-2.5 py-0.5 rounded-lg bg-amber-100 text-amber-700 text-xs font-bold flex items-center gap-1" title={account.profile_error}>
                <AlertCircle className="w-3 h-3" /> 资料未同步
              </span>
            )}
          </div>
          <div className="mt-3 flex flex-wrap items-center justify-between gap-4">
            <div className="text-sm font-medium text-gray-500">
              <p>{account.remark || '暂无备注'}</p>
              <p className="font-mono text-xs text-gray-400">ID: {account.id}</p>
            </div>
            {account.verification_pending && account.enabled && (
              <div role="alert" className="w-full rounded-xl bg-orange-50 px-4 py-3 text-sm font-medium text-orange-800">
                <div className="flex items-start gap-2">
                  <AlertCircle className="mt-0.5 h-4 w-4 flex-shrink-0" />
                  <span className="min-w-0 break-words leading-relaxed">闲鱼要求完成安全验证，自动续期未完成。请尽快验证，否则订单、发货等接口可能在数分钟后失效。</span>
                </div>
                <div className="mt-3 flex flex-wrap items-center gap-2">
                  <button type="button" onClick={/* 发起续期安全验证，登记过期时由上层回退到扫码授权。 */ () => onVerify(account)} className="inline-flex items-center gap-1.5 rounded-lg bg-white px-2.5 py-1 text-xs font-bold text-orange-700 hover:bg-orange-100">
                    <QrCode className="w-3.5 h-3.5" /> 完成安全验证
                  </button>
                </div>
              </div>
            )}
            {account.runtime_message && account.runtime_state !== 'online' && account.runtime_state !== 'disabled' && (
              <div className={`w-full rounded-xl px-4 py-3 text-sm font-medium ${requiresLogin ? 'bg-red-50 text-red-700' : 'bg-amber-50 text-amber-700'}`}>
                <div className="flex items-start gap-2">
                  <AlertCircle className="mt-0.5 h-4 w-4 flex-shrink-0" />
                  <span className="min-w-0 break-words leading-relaxed">{account.runtime_message}</span>
                </div>
                {requiresLogin && (
                  <div className="mt-3 flex flex-wrap items-center gap-2">
                    <button type="button" onClick={/* 风控账号打开编辑弹窗粘贴 Cookie，登录失效账号仍走二维码授权。 */ () => (account.runtime_state === 'verification_required' ? onEdit(account) : onReauthorize(account))} className="inline-flex items-center gap-1.5 rounded-lg bg-white px-2.5 py-1 text-xs font-bold text-red-700 hover:bg-red-100">
                      {account.runtime_state === 'verification_required' ? <><ClipboardPaste className="w-3.5 h-3.5" /> 粘贴无风控 Cookie</> : <><QrCode className="w-3.5 h-3.5" /> 重新授权</>}
                    </button>
                    {account.runtime_state === 'verification_required' && (
                      <a href={RISK_GUIDE_URL} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1.5 rounded-lg bg-white px-2.5 py-1 text-xs font-bold text-blue-700 hover:bg-blue-100">
                        <ExternalLink className="w-3.5 h-3.5" /> 查看操作教程
                      </a>
                    )}
                  </div>
                )}
              </div>
            )}
            {account.paused && <span className="flex items-center gap-1.5 rounded-lg bg-blue-50 px-3 py-1.5 text-xs font-bold text-blue-700"><Clock className="h-3 w-3" /> 暂停处理中</span>}
            <div className="mt-3 flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:items-center">
              <button
                type="button"
                onClick={/* 用户点击在线聊天时把当前账号标识交给应用壳打开对应会话。 */ () => onOpenChat?.(account.id)}
                disabled={!account.enabled}
                className="ios-btn-primary flex h-11 w-full items-center justify-center gap-2 rounded-xl px-4 text-sm sm:mr-auto sm:w-auto"
                title={account.enabled ? '打开当前账号的在线聊天' : '账号已停用，请先启用后再进入在线聊天'}
              >
                <MessageCircle className="h-4 w-4" />
                在线聊天
              </button>
              {/* 图标操作在手机端独立成行并保持 44px 触控尺寸，桌面端与主聊天按钮同排右对齐。 */}
              <div className="flex w-full items-center justify-end gap-2 sm:w-auto">
              <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => onRefreshProfile(account)} disabled={refreshing} className="flex h-11 w-11 items-center justify-center rounded-xl transition-colors text-gray-600 hover:bg-gray-100 disabled:opacity-50" aria-label="刷新昵称和头像" title="刷新昵称和头像">
                <RefreshCw className={`w-5 h-5 ${refreshing ? 'animate-spin' : ''}`} />
              </button>
              <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => onReauthorize(account)} className={`flex h-11 w-11 items-center justify-center rounded-xl transition-colors ${requiresLogin ? 'text-red-600 bg-red-50 hover:bg-red-100' : 'text-blue-600 hover:bg-blue-50'}`} aria-label="重新扫码授权当前账号" title="重新扫码授权当前账号">
                <QrCode className="w-5 h-5" />
              </button>
              <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => onEdit(account)} className="flex h-11 w-11 items-center justify-center rounded-xl hover:bg-gray-100 transition-colors text-gray-600" aria-label="编辑账号" title="编辑账号">
                <Edit2 className="w-5 h-5" />
              </button>
              <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => onAI(account)} className="flex h-11 w-11 items-center justify-center rounded-xl hover:bg-purple-100 transition-colors text-purple-600" aria-label="AI设置" title="AI设置">
                <Bot className="w-5 h-5" />
              </button>
              <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => onTasks(account)} className="flex h-11 w-11 items-center justify-center rounded-xl hover:bg-amber-100 transition-colors text-amber-600" aria-label="自动评价与每日擦亮" title="自动评价与每日擦亮">
                <CalendarClock className="w-5 h-5" />
              </button>
              <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => onToggle(account.id, account.enabled)} className={`flex h-11 w-11 items-center justify-center rounded-xl transition-colors ${account.enabled ? 'text-green-600 hover:bg-green-50' : 'text-gray-400 hover:bg-gray-100'}`} aria-label={account.enabled ? '停用账号' : '启用账号'} title={account.enabled ? '停用账号' : '启用账号'}>
                <Power className="w-5 h-5" />
              </button>
              <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => onDelete(account)} disabled={deleting} className="flex h-11 w-11 items-center justify-center rounded-xl hover:bg-red-100 transition-colors text-red-500 disabled:cursor-not-allowed disabled:opacity-40" aria-label={deleting ? '删除中…' : `删除账号 ${account.nickname || account.remark || account.id}`} title={deleting ? '删除中…' : `删除账号 ${account.nickname || account.remark || account.id}`}>
                {deleting ? <Loader2 className="w-5 h-5 animate-spin" /> : <Trash2 className="w-5 h-5" />}
              </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
});
