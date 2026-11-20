import { AlertCircle,Check,Loader2,QrCode,RefreshCw } from 'lucide-react';
import React from 'react';
import type { FeishuBindingStatus } from '../feishuBinding';

/** FeishuAppBindingViewProps 描述内嵌于「新建渠道」弹窗的扫码授权视图的展示状态和用户动作。 */
export interface FeishuAppBindingViewProps {
  /** status 是当前绑定阶段，closed 时视图不渲染。 */
  status: FeishuBindingStatus;
  /** qrCodeUrl 是后端生成的飞书扫码二维码 data URL。 */
  qrCodeUrl: string;
  /** message 是后端返回的阶段说明或本地错误文案。 */
  message: string;
  /** requesting 表示创建扫码会话的请求是否仍在进行。 */
  requesting: boolean;
  /** onStart 创建新的扫码绑定会话，用于首次开始或失败重试。 */
  onStart: () => void | Promise<void>;
  /** onClose 绑定成功后的“完成”或等待扫码时的“关闭”动作，由所在弹窗负责关闭并取消进行中的绑定流程。 */
  onClose: () => void;
}

// primaryButtonClass 是二维码卡片下方等宽主按钮的样式。
const primaryButtonClass = 'ios-btn-primary inline-flex w-full items-center justify-center gap-2 rounded-xl px-5 py-2.5 font-bold disabled:opacity-50';
// secondaryButtonClass 是二维码卡片下方等宽次按钮的样式。
const secondaryButtonClass = 'inline-flex w-full items-center justify-center rounded-xl bg-gray-100 px-5 py-2.5 font-bold text-gray-700 transition hover:bg-gray-200';

/**
 * FeishuAppBindingView 以内嵌方式渲染扫码授权视图：左列是二维码/状态卡片和与其等宽的操作按钮，右列是步骤说明。
 * 两列放在同一网格行内并拉伸，因此“二维码卡片 + 按钮”的总高度恒等于右侧步骤卡片的高度。
 */
export const FeishuAppBindingView: React.FC<FeishuAppBindingViewProps> = ({ status, qrCodeUrl, message, requesting, onStart, onClose }) => {
  // handleStart 触发扫码会话创建，不等待异步结果以保持按钮禁用由 Hook 状态驱动。
  const handleStart = () => { void onStart(); };

  if (status === 'closed') return null;

  return (
    <div className="min-w-0 space-y-4">
      <p className="text-xs leading-5 text-gray-500">扫码授权代替手动填写 Webhook 链接：系统自动创建飞书应用机器人，并以私聊方式把通知发给授权人，无需填写密钥。</p>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 sm:items-stretch">
        <div className="flex min-w-0 flex-col gap-3">
          <section className="flex min-h-56 min-w-0 flex-1 flex-col items-center justify-center rounded-2xl border border-gray-100 bg-surface-subtle p-4 text-center">
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
                <p className="mt-1 text-xs leading-5 text-gray-500">{requesting ? '请稍候，二维码生成后会自动展示。' : '确认上方名称和通知内容后，点击下方按钮生成二维码。'}</p>
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
          {/* 操作按钮固定在二维码卡片正下方，与卡片等宽（w-full），随阶段切换生成、重试、关闭或完成。 */}
          {status === 'waiting' && <button type="button" onClick={onClose} className={secondaryButtonClass}>关闭</button>}
          {(status === 'starting' || status === 'error' || status === 'expired') && <button type="button" onClick={handleStart} disabled={requesting} className={primaryButtonClass}>{requesting ? <Loader2 className="h-4 w-4 animate-spin" /> : status === 'starting' ? <QrCode className="h-4 w-4" /> : <RefreshCw className="h-4 w-4" />}{requesting ? '正在生成二维码...' : status === 'starting' ? '生成二维码' : '重新生成二维码'}</button>}
          {status === 'success' && <button type="button" onClick={onClose} className={primaryButtonClass}>完成</button>}
        </div>

        {/* 步骤卡片作为网格另一列自然拉伸到与左列同高。 */}
        <section className="flex min-w-0 flex-col justify-center rounded-xl border border-blue-100 bg-blue-50/60 p-4">
          <p className="text-sm font-bold text-gray-800">扫码授权步骤</p>
          <ol className="mt-3 space-y-3 text-xs leading-5 text-gray-600">
            <li className="flex items-start gap-3"><span className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-white text-xs font-extrabold text-brand">1</span><div className="min-w-0"><p className="font-bold text-gray-800">生成二维码</p><p>点击二维码下方的按钮，生成飞书扫码二维码。</p></div></li>
            <li className="flex items-start gap-3"><span className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-white text-xs font-extrabold text-brand">2</span><div className="min-w-0"><p className="font-bold text-gray-800">飞书扫码确认</p><p>使用飞书扫描二维码，并在飞书客户端确认授权。</p></div></li>
            <li className="flex items-start gap-3"><span className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-white text-xs font-extrabold text-brand">3</span><div className="min-w-0"><p className="font-bold text-gray-800">自动创建渠道</p><p>系统按上方填写的名称和通知内容创建渠道；在「账号管理 → 编辑」里绑定该渠道后，账号的通知才会推送到飞书。</p></div></li>
          </ol>
        </section>
      </div>
    </div>
  );
};
