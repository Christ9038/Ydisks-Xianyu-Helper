import React from 'react';
import { AlertCircle, Bot, CalendarClock, Check, Clock, Loader2, MessageCircle, Power, QrCode, RefreshCw, Sparkles, Trash2, User, Edit2 } from 'lucide-react';
import type { AccountDetail } from '../../../../shared/api-contract';
import { accountRuntimePresentation } from '../runtime';

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
}

// AccountCard 渲染单个账号的状态摘要与操作入口。
export const AccountCard = React.memo(/* AccountCard 负责渲染单个账号卡片及其操作。 */ function AccountCard({
  account,
  refreshing,
  deleting,
  onRefreshProfile,
  onReauthorize,
  onEdit,
  onAI,
  onTasks,
  onToggle,
  onDelete,
}: AccountCardProps) {
  // runtime 保存账号运行状态的展示样式。
  const runtime = accountRuntimePresentation(account);
  // requiresLogin 表示当前账号是否需要重新授权。
  const requiresLogin = account.runtime_state === 'auth_expired' || account.runtime_state === 'verification_required';

  return (
    <div className="ios-card rounded-xl p-6 group transition-all duration-300 hover:border-brand">
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
            {account.auto_confirm && <span className="flex items-center gap-1 rounded-lg bg-blue-50 px-2.5 py-0.5 text-xs font-bold text-blue-700"><Check className="h-3 w-3" /> 自动确认发货</span>}
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
            {account.runtime_message && account.runtime_state !== 'online' && account.runtime_state !== 'disabled' && (
              <div className={`mb-3 flex flex-wrap items-center gap-2 text-sm font-medium ${requiresLogin ? 'text-red-700' : 'text-amber-700'}`}>
                <AlertCircle className="w-4 h-4 flex-shrink-0" />
                <span>{account.runtime_message}</span>
                {requiresLogin && (
                  <button type="button" onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => onReauthorize(account)} className="inline-flex items-center gap-1.5 rounded-lg bg-red-50 px-2.5 py-1 text-xs font-bold text-red-700 hover:bg-red-100">
                    <QrCode className="w-3.5 h-3.5" /> 重新授权
                  </button>
                )}
              </div>
            )}
            {account.paused && <span className="flex items-center gap-1.5 rounded-lg bg-blue-50 px-3 py-1.5 text-xs font-bold text-blue-700"><Clock className="h-3 w-3" /> 暂停处理中</span>}
            <div className="mt-3 flex flex-wrap items-center justify-end gap-2">
              <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => onRefreshProfile(account)} disabled={refreshing} className="p-3 rounded-xl transition-colors text-gray-600 hover:bg-gray-100 disabled:opacity-50" title="刷新昵称和头像">
                <RefreshCw className={`w-5 h-5 ${refreshing ? 'animate-spin' : ''}`} />
              </button>
              <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => onReauthorize(account)} className={`p-3 rounded-xl transition-colors ${requiresLogin ? 'text-red-600 bg-red-50 hover:bg-red-100' : 'text-blue-600 hover:bg-blue-50'}`} title="重新扫码授权当前账号">
                <QrCode className="w-5 h-5" />
              </button>
              <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => onEdit(account)} className="p-3 rounded-xl hover:bg-gray-100 transition-colors text-gray-600" title="编辑账号">
                <Edit2 className="w-5 h-5" />
              </button>
              <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => onAI(account)} className="p-3 rounded-xl hover:bg-purple-100 transition-colors text-purple-600" title="AI设置">
                <Bot className="w-5 h-5" />
              </button>
              <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => onTasks(account)} className="p-3 rounded-xl hover:bg-amber-100 transition-colors text-amber-600" title="自动评价与每日擦亮">
                <CalendarClock className="w-5 h-5" />
              </button>
              <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => onToggle(account.id, account.enabled)} className={`p-3 rounded-xl transition-colors ${account.enabled ? 'text-green-600 hover:bg-green-50' : 'text-gray-400 hover:bg-gray-100'}`} title={account.enabled ? '停用账号' : '启用账号'}>
                <Power className="w-5 h-5" />
              </button>
              <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => onDelete(account)} disabled={deleting} className="p-3 rounded-xl hover:bg-red-100 transition-colors text-red-500 disabled:cursor-not-allowed disabled:opacity-40" title={deleting ? '删除中…' : `删除账号 ${account.nickname || account.remark || account.id}`}>
                {deleting ? <Loader2 className="w-5 h-5 animate-spin" /> : <Trash2 className="w-5 h-5" />}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
});
