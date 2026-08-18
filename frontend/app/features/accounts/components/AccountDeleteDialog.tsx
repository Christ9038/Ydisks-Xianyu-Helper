import { AlertCircle,Loader2,Trash2,X } from 'lucide-react';
import React from 'react';
import { createPortal } from 'react-dom';
import type { AccountDetail } from '../api';

// AccountDeleteDialogProps 描述账号删除确认框需要的状态和回调。
export interface AccountDeleteDialogProps {
  // account 是待删除的账号摘要。
  account: AccountDetail;
  // deleting 表示删除请求是否正在执行。
  deleting: boolean;
  // error 是删除失败后的用户可见提示。
  error: string;
  // onClose 关闭删除确认框。
  onClose: () => void;
  // onConfirm 确认删除账号。
  onConfirm: () => void | Promise<void>;
}

// AccountDeleteDialog 渲染账号删除确认框并通过 Portal 隔离页面布局。
export const AccountDeleteDialog: React.FC<AccountDeleteDialogProps> = ({ account, deleting, error, onClose, onConfirm }) => createPortal(
  <div className="modal-overlay-centered" role="presentation">
    <div className="h-fit w-full max-w-[400px] self-center overflow-hidden rounded-2xl border border-white/80 bg-white shadow-modal" role="dialog" aria-modal="true" aria-labelledby="delete-account-title" aria-describedby="delete-account-description">
      <div className="relative overflow-hidden border-b border-red-100 bg-gradient-to-br from-red-50 via-white to-orange-50 px-5 py-5">
        <div className="absolute -right-12 -top-16 h-36 w-36 rounded-full bg-red-200/35 blur-2xl" />
        <div className="relative flex items-start gap-4">
          <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl bg-red-600 text-white shadow-md shadow-red-200"><Trash2 className="h-5 w-5" /></div>
          <div className="min-w-0 flex-1">
            <h3 id="delete-account-title" className="text-lg font-extrabold tracking-tight text-gray-950">删除这个账号？</h3>
            <p id="delete-account-description" className="mt-1 text-xs leading-5 text-gray-600">删除后，该账号的关联配置也会一并清理，此操作无法撤销。</p>
          </div>
          <button type="button" onClick={onClose} disabled={deleting} className="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-full text-gray-400 transition-colors hover:bg-white hover:text-gray-700 disabled:cursor-not-allowed disabled:opacity-40" aria-label="关闭删除确认"><X className="h-5 w-5" /></button>
        </div>
      </div>

      <div className="px-5 py-4">
        <div className="rounded-xl border border-gray-100 bg-gray-50/80 px-4 py-3">
          <div className="text-sm font-extrabold text-gray-900">{account.nickname || account.remark || '未命名账号'}</div>
          {account.remark && account.nickname && <div className="mt-1 text-sm text-gray-500">备注：{account.remark}</div>}
          <div className="mt-1.5 break-all font-mono text-[11px] text-gray-400">ID: {account.id}</div>
        </div>
        {deleting && (
          <div className="mt-4 flex items-center gap-3 rounded-xl border border-blue-100 bg-blue-50 px-4 py-3" role="progressbar" aria-label="正在删除账号">
            <Loader2 className="h-5 w-5 flex-shrink-0 animate-spin text-brand" />
            <div><div className="text-sm font-extrabold text-blue-950">正在删除账号</div><div className="mt-0.5 text-xs text-blue-700">正在清理关联数据，请保持页面打开…</div></div>
          </div>
        )}
        {error && (
          <div className="mt-4 flex items-start gap-3 rounded-xl border border-red-100 bg-red-50 px-4 py-3" role="alert">
            <AlertCircle className="mt-0.5 h-5 w-5 flex-shrink-0 text-red-600" />
            <div><div className="text-sm font-extrabold text-red-800">删除失败</div><div className="mt-1 text-xs leading-5 text-red-700">{error}</div></div>
          </div>
        )}
      </div>

      <div className="flex gap-3 border-t border-gray-100 bg-gray-50/70 px-5 py-4">
        <button type="button" onClick={onClose} disabled={deleting} className="flex-1 rounded-xl bg-white px-5 py-3 text-sm font-extrabold text-gray-700 ring-1 ring-gray-200 transition-colors hover:bg-gray-100 disabled:cursor-not-allowed disabled:opacity-50">取消</button>
        <button type="button" onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => void onConfirm()} disabled={deleting} className="flex flex-1 items-center justify-center gap-2 rounded-xl bg-red-600 px-5 py-3 text-sm font-extrabold text-white shadow-lg shadow-red-200 transition-colors hover:bg-red-700 disabled:cursor-not-allowed disabled:bg-red-400">
          {deleting ? <><Loader2 className="h-4 w-4 animate-spin" />处理中</> : <><Trash2 className="h-4 w-4" />确认删除</>}
        </button>
      </div>
    </div>
  </div>,
  document.body,
);
