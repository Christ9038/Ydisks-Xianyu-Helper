import { AlertCircle,Check,Loader2,RefreshCw,Settings2,X } from 'lucide-react';
import React from 'react';
import { createPortal } from 'react-dom';
import { FeishuSwitchRow } from './FeishuSwitchRow';

/** FeishuNotificationSettingsModalProps 描述飞书渠道分类设置弹窗的展示状态和用户动作。 */
export interface FeishuNotificationSettingsModalProps {
  /** open 表示设置弹窗是否打开。 */
  open: boolean;
  /** channelName 是弹窗标题中展示的飞书渠道名称。 */
  channelName: string;
  /** loading 表示分类设置是否正在读取。 */
  loading: boolean;
  /** saving 表示分类设置是否正在保存。 */
  saving: boolean;
  /** warning 是警告类通知开关的当前值。 */
  warning: boolean;
  /** info 是普通提示类通知开关的当前值。 */
  info: boolean;
  /** error 是读取或保存失败的用户可读说明。 */
  error: string;
  /** loadFailed 表示读取失败，弹窗应展示重试而不是默认草稿。 */
  loadFailed: boolean;
  /** onWarningChange 更新警告类通知草稿。 */
  onWarningChange: (value: boolean) => void;
  /** onInfoChange 更新普通提示类通知草稿。 */
  onInfoChange: (value: boolean) => void;
  /** onSave 保存当前分类开关。 */
  onSave: () => void | Promise<void>;
  /** onRetry 重新读取当前渠道的分类开关。 */
  onRetry: () => void | Promise<void>;
  /** onClose 关闭弹窗并取消进行中的请求。 */
  onClose: () => void;
}

/** FeishuNotificationSettingsModal 通过 Portal 展示飞书渠道的警告类与普通提示类推送开关。 */
export const FeishuNotificationSettingsModal: React.FC<FeishuNotificationSettingsModalProps> = ({ open, channelName, loading, saving, warning, info, error, loadFailed, onWarningChange, onInfoChange, onSave, onRetry, onClose }) => {
  // handleSave 触发保存动作，不等待异步结果以保持按钮状态由 Hook 驱动。
  const handleSave = () => { void onSave(); };
  // handleRetry 触发重新读取，不等待异步结果以保持重试按钮状态由 Hook 驱动。
  const handleRetry = () => { void onRetry(); };

  if (!open) return null;

  return createPortal(
    <div className="modal-overlay">
      <div className="modal-container" style={{ maxWidth: '28rem' }}>
        <div className="flex items-center justify-between gap-3 border-b border-gray-100 px-6 py-5">
          <div className="flex min-w-0 items-center gap-3">
            <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl bg-blue-50"><Settings2 className="h-5 w-5 text-brand" /></div>
            <div className="min-w-0">
              <h3 className="text-xl font-extrabold text-gray-900">飞书通知设置</h3>
              <p className="mt-1 truncate text-xs text-gray-500">{channelName || '飞书机器人（扫码）'}</p>
            </div>
          </div>
          <button type="button" onClick={onClose} className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-2xl bg-gray-100 hover:bg-gray-200" aria-label="关闭飞书通知设置"><X className="h-5 w-5 text-gray-600" /></button>
        </div>
        <div className="min-h-0 flex-1 space-y-4 overflow-x-hidden overflow-y-auto px-6 py-5">
          {loading && <div className="flex items-center justify-center gap-3 py-10 text-sm font-medium text-gray-500"><Loader2 className="h-5 w-5 text-brand animate-spin" />正在读取通知设置...</div>}
          {!loading && loadFailed && (
            <div role="alert" className="space-y-3 rounded-xl border border-red-100 bg-red-50 p-4 text-sm text-red-700">
              <div className="flex items-start gap-2"><AlertCircle className="mt-0.5 h-4 w-4 flex-shrink-0" /><span className="leading-5">{error || '读取飞书通知设置失败'}</span></div>
              <button type="button" onClick={handleRetry} className="inline-flex items-center gap-2 rounded-xl bg-white px-4 py-2 font-bold text-gray-700 hover:bg-gray-100">
                <RefreshCw className="h-4 w-4" />重试
              </button>
            </div>
          )}
          {!loading && !loadFailed && (
            <>
              {error && <div role="alert" className="flex items-start gap-2 rounded-xl border border-red-100 bg-red-50 p-3 text-xs leading-5 text-red-700"><AlertCircle className="mt-0.5 h-4 w-4 flex-shrink-0" /><span>{error}</span></div>}
              <div className="space-y-2">
                <FeishuSwitchRow label="警告类通知" description="账号异常、风控验证、需要人工处理、失败结果等告警。" checked={warning} disabled={saving} onChange={onWarningChange} />
                <FeishuSwitchRow label="普通提示类通知" description="买家新消息、发货结果、自动化成功结果等日常提示。" checked={info} disabled={saving} onChange={onInfoChange} />
              </div>
              <p className="text-xs leading-5 text-gray-400">两个开关分别控制该飞书渠道接收的通知类别；关闭后对应类别不会推送到此渠道。</p>
            </>
          )}
        </div>
        <div className="flex items-center justify-end gap-3 border-t border-gray-100 px-6 py-4">
          <button type="button" onClick={onClose} className="rounded-xl bg-gray-100 px-5 py-2.5 font-bold text-gray-700 transition-colors hover:bg-gray-200">取消</button>
          {!loading && !loadFailed && <button type="button" onClick={handleSave} disabled={saving} className="ios-btn-primary flex items-center gap-2 rounded-xl px-6 py-2.5 font-bold disabled:opacity-50">{saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}{saving ? '保存中...' : '保存'}</button>}
        </div>
      </div>
    </div>,
    document.body,
  );
};
