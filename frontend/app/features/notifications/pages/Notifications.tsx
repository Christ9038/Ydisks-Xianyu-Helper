import { Bell,Check,Loader2,Plus,RefreshCw,X } from 'lucide-react';
import React from 'react';
import { NotificationChannelList } from '../components/NotificationChannelList';
import { NotificationChannelModal } from '../components/NotificationChannelModal';
import { NotificationSmtpSettings } from '../components/NotificationSmtpSettings';
import { useNotifications } from '../hooks';
import { PageHeader } from '../../../../shared/ui/PageHeader';

// NotificationsProps 描述通知页面接收的权限上下文。
interface NotificationsProps {
  // isAdmin 表示当前用户是否可以查看和保存系统 SMTP 配置。
  isAdmin?: boolean;
}

// Notifications 渲染通知渠道列表、SMTP 配置和渠道编辑边界。
const Notifications: React.FC<NotificationsProps> = ({ isAdmin = false }) => {
  // notificationState 统一提供通知页面的数据、表单和异步动作。
  const notificationState = useNotifications(isAdmin);

  return (
    <div className="min-w-0 space-y-8 animate-fade-in">
      <PageHeader title="通知设置" description="配置通知渠道，账号异常时主动推送告警">
        <div className="flex flex-wrap items-center gap-3">
          <button onClick={notificationState.loadChannels} className="px-4 py-2 bg-gray-100 hover:bg-gray-200 rounded-xl font-bold text-gray-700 flex items-center gap-2 transition-colors"><RefreshCw className="w-4 h-4" />刷新</button>
          <button onClick={notificationState.openCreate} className="ios-btn-primary px-5 py-2.5 rounded-xl font-bold flex items-center gap-2"><Plus className="w-4 h-4" />新建渠道</button>
        </div>
      </PageHeader>

      <div className="ios-card rounded-xl p-5 bg-blue-50/50 border border-blue-100">
        <div className="flex items-start gap-3"><Bell className="w-5 h-5 text-brand mt-0.5 flex-shrink-0" /><div className="text-sm text-gray-700 leading-6">配置通知渠道并在「账号管理 → 编辑」里绑定后，以下事件会主动推送到该账号绑定的渠道；自动化任务通知已拆分为四类，可按需单独关闭：<ul className="mt-2 space-y-1 text-gray-600"><li>• <strong>拍下改价、付款发货、评价赠品、求评价</strong>：分别控制对应自动化任务结果</li><li>• <strong>账号 session 失效 / 自动恢复失败</strong>：账号状态告警</li><li>• <strong>触发风控验证</strong>：可能需要扫码完成验证</li></ul></div></div>
      </div>

      {notificationState.loading ? <div className="flex justify-center py-20"><Loader2 className="w-8 h-8 text-brand animate-spin" /></div> : <NotificationChannelList channels={notificationState.channels} testingId={notificationState.testingId} onEdit={notificationState.openEdit} onDelete={notificationState.handleDelete} onToggleEnabled={notificationState.handleToggleEnabled} onTest={notificationState.handleTest} />}

      {isAdmin && <NotificationSmtpSettings smtp={notificationState.smtp} setSmtp={notificationState.setSmtp} smtpSaving={notificationState.smtpSaving} showPassword={notificationState.showSmtpPassword} setShowPassword={notificationState.setShowSmtpPassword} onSave={notificationState.handleSaveSmtp} />}

      <NotificationChannelModal showModal={notificationState.showModal} editing={notificationState.editing} form={notificationState.form} setForm={notificationState.setForm} smtp={notificationState.smtp} showChannelSmtpPassword={notificationState.showChannelSmtpPassword} setShowChannelSmtpPassword={notificationState.setShowChannelSmtpPassword} saving={notificationState.saving} onClose={notificationState.closeModal} onSave={notificationState.handleSave} />

      {notificationState.toast && <div className={`fixed bottom-8 left-1/2 -translate-x-1/2 z-[10000] px-5 py-3 rounded-xl shadow-lg font-bold text-sm flex items-center gap-2 animate-fade-in text-white ${notificationState.toast.type === 'success' ? 'bg-success-500' : 'bg-danger-500'}`}>{notificationState.toast.type === 'success' ? <Check className="w-4 h-4" /> : <X className="w-4 h-4" />}{notificationState.toast.text}</div>}
    </div>
  );
};

export default Notifications;
