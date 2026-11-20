import { Bell,Check,Loader2,Plus,QrCode,RefreshCw,X } from 'lucide-react';
import React,{ Suspense,useCallback,useState } from 'react';
import type { NotificationChannel } from '../api';
import type { FeishuBindingPanelProps } from '../components/FeishuBindingPanel';
import type { FeishuSettingsPanelProps } from '../components/FeishuSettingsPanel';
import { NotificationChannelList } from '../components/NotificationChannelList';
import { NotificationChannelModal } from '../components/NotificationChannelModal';
import { NotificationSmtpSettings } from '../components/NotificationSmtpSettings';
import { useNotifications } from '../hooks';
import { PageHeader } from '../../../../shared/ui/PageHeader';

// NotificationsProps 描述通知页面接收的权限上下文和按需注入的飞书交互面板。
interface NotificationsProps {
  // isAdmin 表示当前用户是否可以查看和保存系统 SMTP 配置。
  isAdmin?: boolean;
  // feishuBindingPanel 是应用壳按需加载的扫码绑定面板，只有用户打开弹窗时才下载。
  feishuBindingPanel?: React.ComponentType<FeishuBindingPanelProps>;
  // feishuSettingsPanel 是应用壳按需加载的飞书渠道分类设置面板，只有打开设置时才下载。
  feishuSettingsPanel?: React.ComponentType<FeishuSettingsPanelProps>;
}

// Notifications 渲染通知渠道列表、SMTP 配置和渠道编辑边界。
const Notifications: React.FC<NotificationsProps> = ({ isAdmin = false, feishuBindingPanel: FeishuBindingPanel, feishuSettingsPanel: FeishuSettingsPanel }) => {
  // notificationState 统一提供通知页面的数据、表单和异步动作。
  const notificationState = useNotifications(isAdmin);
  // feishuBindingOpen 保存扫码绑定面板是否应由页面延迟渲染。
  const [feishuBindingOpen, setFeishuBindingOpen] = useState(false);
  // feishuSettingsChannel 保存当前请求打开分类设置的飞书渠道；空值表示设置面板未打开。
  const [feishuSettingsChannel, setFeishuSettingsChannel] = useState<NotificationChannel | null>(null);
  // handleFeishuBound 在扫码绑定成功后刷新通知渠道列表并提示结果。
  const handleFeishuBound = useCallback(
    // channelID 是后端创建或更新的飞书渠道主键；页面以刷新后的渠道列表作为展示来源。
    (channelID: number) => {
      void channelID;
      void notificationState.loadChannels();
      notificationState.showToast('success', '飞书机器人绑定成功');
    },
    [notificationState.loadChannels, notificationState.showToast],
  );
  // handleFeishuSettingsSaved 在分类设置保存成功后刷新列表、收起面板并提示结果。
  const handleFeishuSettingsSaved = useCallback(
    // channelID 是刚保存设置的飞书渠道主键；页面以刷新后的渠道列表作为展示来源。
    (channelID: string) => {
      void channelID;
      setFeishuSettingsChannel(null);
      void notificationState.loadChannels();
      notificationState.showToast('success', '飞书通知设置已保存');
    },
    [notificationState.loadChannels, notificationState.showToast],
  );

  return (
    <div className="min-w-0 space-y-8 animate-fade-in">
      <PageHeader title="通知设置" description="配置通知渠道，账号异常时主动推送告警">
        <div className="flex flex-wrap items-center gap-3">
          <button onClick={notificationState.loadChannels} className="px-4 py-2 bg-gray-100 hover:bg-gray-200 rounded-xl font-bold text-gray-700 flex items-center gap-2 transition-colors"><RefreshCw className="w-4 h-4" />刷新</button>
          {/* 扫码绑定渠道对所有登录用户开放；弹窗与扫码 Hook 仅在点击后才按需加载。 */}
          <button onClick={/* 用户点击后只打开延迟面板，不在通知页首屏加载扫码交互代码。 */ () => setFeishuBindingOpen(true)} className="px-4 py-2 bg-blue-50 hover:bg-blue-100 text-brand rounded-xl font-bold flex items-center gap-2 transition-colors"><QrCode className="w-4 h-4" />扫码绑定飞书机器人</button>
          <button onClick={notificationState.openCreate} className="ios-btn-primary px-5 py-2.5 rounded-xl font-bold flex items-center gap-2"><Plus className="w-4 h-4" />新建渠道</button>
        </div>
      </PageHeader>

      <div className="ios-card rounded-xl p-5 bg-blue-50/50 border border-blue-100">
        <div className="flex items-start gap-3"><Bell className="w-5 h-5 text-brand mt-0.5 flex-shrink-0" /><div className="text-sm text-gray-700 leading-6">配置通知渠道并在「账号管理 → 编辑」里绑定后，以下事件会主动推送到该账号绑定的渠道；自动化任务通知已拆分为四类，可按需单独关闭：<ul className="mt-2 space-y-1 text-gray-600"><li>• <strong>拍下改价、付款发货、评价赠品、求评价</strong>：分别控制对应自动化任务结果</li><li>• <strong>账号 session 失效 / 自动恢复失败</strong>：账号状态告警</li><li>• <strong>触发风控验证</strong>：可能需要扫码完成验证</li></ul></div></div>
      </div>

      {notificationState.loading ? <div className="flex justify-center py-20"><Loader2 className="w-8 h-8 text-brand animate-spin" /></div> : <NotificationChannelList channels={notificationState.channels} testingId={notificationState.testingId} onEdit={notificationState.openEdit} onDelete={notificationState.handleDelete} onToggleEnabled={notificationState.handleToggleEnabled} onTest={notificationState.handleTest} onOpenFeishuSettings={/* 用户点击飞书渠道设置时只记录目标渠道，面板按需加载。 */ setFeishuSettingsChannel} />}

      {isAdmin && <NotificationSmtpSettings smtp={notificationState.smtp} setSmtp={notificationState.setSmtp} smtpSaving={notificationState.smtpSaving} showPassword={notificationState.showSmtpPassword} setShowPassword={notificationState.setShowSmtpPassword} onSave={notificationState.handleSaveSmtp} />}

      <NotificationChannelModal showModal={notificationState.showModal} editing={notificationState.editing} form={notificationState.form} setForm={notificationState.setForm} smtp={notificationState.smtp} showChannelSmtpPassword={notificationState.showChannelSmtpPassword} setShowChannelSmtpPassword={notificationState.setShowChannelSmtpPassword} saving={notificationState.saving} onClose={notificationState.closeModal} onSave={notificationState.handleSave} />

      {/* 飞书面板由应用壳注入并按需下载；嵌套 Suspense 只影响面板加载，不替换整个通知页面。 */}
      <Suspense fallback={null}>
        {feishuBindingOpen && FeishuBindingPanel && <FeishuBindingPanel onSuccess={handleFeishuBound} onClose={/* 用户关闭扫码弹窗后释放页面侧打开状态。 */ () => setFeishuBindingOpen(false)} />}
        {feishuSettingsChannel && FeishuSettingsPanel && <FeishuSettingsPanel channel={feishuSettingsChannel} onSaved={handleFeishuSettingsSaved} onClose={/* 用户取消或关闭设置后释放选中渠道。 */ () => setFeishuSettingsChannel(null)} />}
      </Suspense>

      {notificationState.toast && <div className={`fixed bottom-8 left-1/2 -translate-x-1/2 z-[10000] px-5 py-3 rounded-xl shadow-lg font-bold text-sm flex items-center gap-2 animate-fade-in text-white ${notificationState.toast.type === 'success' ? 'bg-success-500' : 'bg-danger-500'}`}>{notificationState.toast.type === 'success' ? <Check className="w-4 h-4" /> : <X className="w-4 h-4" />}{notificationState.toast.text}</div>}
    </div>
  );
};

export default Notifications;
