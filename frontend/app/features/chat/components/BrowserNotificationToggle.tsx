import { BellRing } from 'lucide-react';
import React from 'react';
import { useBrowserNotificationPreference } from '../../../../shared/browser/browserNotifications';

// BrowserNotificationToggle 让所有登录用户在在线聊天页管理当前浏览器的系统通知偏好。
const BrowserNotificationToggle: React.FC = () => {
  // preference 保存浏览器通知能力、权限和异步更新状态，不写入服务端设置。
  const preference = useBrowserNotificationPreference();
  // statusText 根据浏览器能力和权限给出当前开关的可见状态。
  const statusText = preference.updating
    ? '请求权限中'
    : preference.permission === 'denied'
      ? '权限已拒绝'
      : preference.supported
        ? preference.enabled ? '已开启' : '未开启'
        : '不支持通知';
  return (
    <div className="flex items-center gap-2 rounded-full border border-slate-200 bg-white px-3 py-1.5 text-xs">
      <BellRing className="h-3.5 w-3.5 text-slate-500" aria-hidden="true" />
      <span className="font-semibold text-slate-600">浏览器提醒</span>
      <button
        type="button"
        role="switch"
        aria-checked={preference.enabled}
        aria-label="开启新消息系统通知"
        disabled={preference.updating || !preference.supported}
        onClick={/* 当前回调由用户手势触发权限申请或关闭本地通知偏好。 */ () => void preference.setEnabled(!preference.enabled)}
        className={`relative h-5 w-9 shrink-0 rounded-full transition-colors ${preference.enabled ? 'bg-sky-500' : 'bg-slate-200'} disabled:cursor-not-allowed disabled:opacity-50`}
      >
        <span className={`absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-transform ${preference.enabled ? 'translate-x-4' : 'translate-x-0.5'}`} />
      </button>
      <span className="text-slate-400">{statusText}</span>
      {preference.error && <span className="sr-only" role="status">{preference.error}</span>}
    </div>
  );
};

export default BrowserNotificationToggle;
