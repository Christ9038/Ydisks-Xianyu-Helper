import { AlertCircle,Check,Loader2,QrCode,RefreshCw,X } from 'lucide-react';
import React from 'react';
import { createPortal } from 'react-dom';
import type { FeishuBindingStatus } from '../feishuBinding';
import { FeishuSwitchRow } from './FeishuSwitchRow';

/** FeishuAppBindingModalProps 描述扫码绑定弹窗的展示状态和用户动作。 */
export interface FeishuAppBindingModalProps {
  /** status 是当前绑定阶段，closed 时弹窗不渲染。 */
  status: FeishuBindingStatus;
  /** qrCodeUrl 是后端生成的飞书扫码二维码 data URL。 */
  qrCodeUrl: string;
  /** message 是后端返回的阶段说明或本地错误文案。 */
  message: string;
  /** bindAllAccounts 表示绑定成功后是否把渠道绑定到当前用户全部账号。 */
  bindAllAccounts: boolean;
  /** notifyWarning 表示新渠道是否推送警告类通知。 */
  notifyWarning: boolean;
  /** notifyInfo 表示新渠道是否推送普通提示类通知。 */
  notifyInfo: boolean;
  /** requesting 表示创建扫码会话的请求是否仍在进行。 */
  requesting: boolean;
  /** onBindAllAccountsChange 更新“绑定全部账号”开关，只在开始绑定前可用。 */
  onBindAllAccountsChange: (checked: boolean) => void;
  /** onNotifyWarningChange 更新警告类通知开关，只在开始绑定前可用。 */
  onNotifyWarningChange: (checked: boolean) => void;
  /** onNotifyInfoChange 更新普通提示类通知开关，只在开始绑定前可用。 */
  onNotifyInfoChange: (checked: boolean) => void;
  /** onStart 创建新的扫码绑定会话，用于首次开始或失败重试。 */
  onStart: () => void | Promise<void>;
  /** onClose 关闭弹窗并取消进行中的绑定流程。 */
  onClose: () => void;
}

/** FeishuAppBindingModal 通过 Portal 渲染二维码/状态卡片、步骤说明和三行通知设置。 */
export const FeishuAppBindingModal: React.FC<FeishuAppBindingModalProps> = ({ status, qrCodeUrl, message, bindAllAccounts, notifyWarning, notifyInfo, requesting, onBindAllAccountsChange, onNotifyWarningChange, onNotifyInfoChange, onStart, onClose }) => {
  // settingsLocked 表示绑定开始后不能再修改设置，避免会话参数与界面不一致。
  const settingsLocked = requesting || status !== 'starting';
  // handleStart 触发扫码会话创建，不等待异步结果以保持按钮禁用由 Hook 状态驱动。
  const handleStart = () => { void onStart(); };

  if (status === 'closed') return null;

  return createPortal(
    <div className="modal-overlay">
      <div className="modal-container" style={{ maxWidth: '36rem' }}>
        <div className="flex items-center justify-between gap-3 border-b border-gray-100 px-6 py-5">
          <div className="flex min-w-0 items-center gap-3">
            <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl bg-blue-50"><QrCode className="h-5 w-5 text-brand" /></div>
            <div className="min-w-0">
              <h3 className="text-xl font-extrabold text-gray-900">扫码绑定飞书机器人</h3>
              <p className="mt-1 text-xs leading-5 text-gray-500">扫码授权后自动创建飞书应用渠道，无需手动填写密钥。</p>
            </div>
          </div>
          <button type="button" onClick={onClose} className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-2xl bg-gray-100 hover:bg-gray-200" aria-label="关闭飞书绑定弹窗"><X className="h-5 w-5 text-gray-600" /></button>
        </div>
        <div className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto px-6 py-5">
          <div className="grid grid-cols-1 gap-5 sm:grid-cols-2 sm:items-start">
            <section className="flex min-h-56 min-w-0 flex-col items-center justify-center rounded-2xl border border-gray-100 bg-surface-subtle p-4 text-center sm:min-h-64">
              {status === 'waiting' && (
                <>
                  <div className="w-full rounded-xl bg-white p-3 shadow-sm" style={{ maxWidth: '15rem' }}>
                    {qrCodeUrl
                      ? <img src={qrCodeUrl} alt="飞书扫码绑定二维码" className="mx-auto block h-auto max-w-full" />
                      : <div className="flex aspect-square items-center justify-center"><Loader2 className="h-8 w-8 text-brand animate-spin" /></div>}
                  </div>
                  <p className="mt-4 flex items-center gap-2 text-sm font-bold text-gray-900"><Loader2 className="h-4 w-4 text-brand animate-spin" />等待扫码确认</p>
                  <p role="status" className="mt-1 text-xs leading-5 text-gray-500">{message || '请使用飞书扫描二维码，并在飞书客户端确认绑定。'}</p>
                </>
              )}
              {status === 'starting' && (
                <>
                  <div className="flex h-14 w-14 items-center justify-center rounded-xl bg-white shadow-sm">{requesting ? <Loader2 className="h-7 w-7 text-brand animate-spin" /> : <QrCode className="h-7 w-7 text-brand" />}</div>
                  <p className="mt-4 text-sm font-bold text-gray-900">{requesting ? '正在生成二维码' : '准备生成二维码'}</p>
                  <p className="mt-1 text-xs leading-5 text-gray-500">{requesting ? '请稍候，二维码生成后会自动展示。' : '确认右侧设置后，点击下方按钮生成二维码。'}</p>
                </>
              )}
              {status === 'success' && (
                <>
                  <div className="flex h-14 w-14 items-center justify-center rounded-full bg-green-100"><Check className="h-7 w-7 text-green-700" /></div>
                  <p className="mt-4 text-base font-extrabold text-gray-900">绑定成功</p>
                  <p role="status" className="mt-1 text-xs leading-5 text-gray-500">{message || '飞书机器人渠道已创建并保存。'}</p>
                </>
              )}
              {(status === 'error' || status === 'expired') && (
                <>
                  <div className="flex h-14 w-14 items-center justify-center rounded-full bg-red-50"><AlertCircle className="h-7 w-7 text-red-600" /></div>
                  <p className="mt-4 text-base font-extrabold text-gray-900">{status === 'expired' ? '二维码已过期' : '绑定未完成'}</p>
                  <p role="alert" className="mt-1 text-xs leading-5 text-gray-500">{message || (status === 'expired' ? '请重新生成二维码后再扫码。' : '请检查飞书授权后重试。')}</p>
                </>
              )}
            </section>

            <div className="min-w-0 space-y-4">
              <section className="rounded-xl border border-blue-100 bg-blue-50/60 p-4">
                <p className="text-sm font-bold text-gray-800">扫码绑定步骤</p>
                <ol className="mt-3 space-y-3 text-xs leading-5 text-gray-600">
                  <li className="flex items-start gap-3"><span className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-white text-xs font-extrabold text-brand">1</span><div className="min-w-0"><p className="font-bold text-gray-800">生成二维码</p><p>点击下方按钮生成飞书扫码二维码。</p></div></li>
                  <li className="flex items-start gap-3"><span className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-white text-xs font-extrabold text-brand">2</span><div className="min-w-0"><p className="font-bold text-gray-800">飞书扫码确认</p><p>使用飞书扫描二维码，并在飞书客户端确认授权。</p></div></li>
                  <li className="flex items-start gap-3"><span className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-white text-xs font-extrabold text-brand">3</span><div className="min-w-0"><p className="font-bold text-gray-800">自动创建渠道</p><p>系统创建飞书应用渠道，并按下方开关绑定账号。</p></div></li>
                </ol>
              </section>

              <section className="space-y-2 rounded-xl border border-gray-100 p-3">
                <div className="px-1">
                  <p className="text-sm font-bold text-gray-800">创建后的通知设置</p>
                  <p className="mt-1 text-xs leading-5 text-gray-500">分类开关决定该飞书渠道接收哪些通知；绑定开始后不可修改。</p>
                </div>
                <FeishuSwitchRow label="警告类通知" description="账号异常、风控验证、需要人工处理、失败结果等告警。" checked={notifyWarning} disabled={settingsLocked} onChange={onNotifyWarningChange} />
                <FeishuSwitchRow label="普通提示类通知" description="买家新消息、发货结果、自动化成功结果等日常提示。" checked={notifyInfo} disabled={settingsLocked} onChange={onNotifyInfoChange} />
                <FeishuSwitchRow label="绑定全部账号" description="开启后扫码成功即自动绑定到全部账号；关闭后可稍后在账号管理中手动绑定。" checked={bindAllAccounts} disabled={settingsLocked} onChange={onBindAllAccountsChange} />
              </section>

            </div>
          </div>
        </div>
        {/* 底部操作栏固定在弹窗内，手机滚动设置内容时主操作始终可见。 */}
        <div className="flex shrink-0 items-center justify-end gap-3 border-t border-gray-100 bg-white px-6 py-4">
          {status === 'waiting' && <button type="button" onClick={onClose} className="rounded-xl px-4 py-2 text-sm font-bold text-gray-600 transition hover:bg-gray-100">关闭</button>}
          {(status === 'starting' || status === 'error' || status === 'expired') && <button type="button" onClick={handleStart} disabled={requesting} className="ios-btn-primary inline-flex items-center justify-center gap-2 rounded-xl px-5 py-2.5 font-bold disabled:opacity-50">{requesting ? <Loader2 className="h-4 w-4 animate-spin" /> : status === 'starting' ? <QrCode className="h-4 w-4" /> : <RefreshCw className="h-4 w-4" />}{requesting ? '正在生成二维码...' : status === 'starting' ? '生成二维码' : '重新生成二维码'}</button>}
          {status === 'success' && <button type="button" onClick={onClose} className="ios-btn-primary rounded-xl px-6 py-2.5 font-bold">完成</button>}
        </div>
      </div>
    </div>,
    document.body,
  );
};
