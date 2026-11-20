import React from 'react';
import type { NotificationChannel } from '../api';
import { useFeishuChannelSettings } from '../feishuChannelSettings';
import { FeishuNotificationSettingsModal } from './FeishuNotificationSettingsModal';

/** FeishuSettingsPanelProps 描述应用壳按需注入的飞书渠道分类设置面板数据。 */
export interface FeishuSettingsPanelProps {
  /** channel 是当前需要调整分类开关的飞书应用渠道。 */
  channel: NotificationChannel;
  /** onSaved 在保存成功后接收渠道标识，供页面刷新列表并收起面板。 */
  onSaved?: (channelID: string) => void;
  /** onClose 在用户取消或关闭后释放页面侧选中的渠道。 */
  onClose: () => void;
}

/** FeishuSettingsPanel 在用户点击渠道设置时才加载分类 Hook 与弹窗，避免通知设置页首屏承担这组交互代码。 */
export const FeishuSettingsPanel: React.FC<FeishuSettingsPanelProps> = ({ channel, onSaved, onClose }) => {
  // feishuSettings 管理分类开关的加载、保存和取消边界。
  const feishuSettings = useFeishuChannelSettings({ onSaved });
  // openedChannelRef 记录已发起读取的渠道标识，防止重复渲染时反复请求。
  const openedChannelRef = React.useRef('');

  React.useEffect(/* 当前副作用在选中渠道变化时读取一次分类设置。 */ () => {
    if (!channel.id || openedChannelRef.current === channel.id) return;
    openedChannelRef.current = channel.id;
    void feishuSettings.openSettings(channel);
  }, [channel, feishuSettings.openSettings]);

  /** handleClose 先取消分类设置流程，再通知页面卸载当前面板。 */
  const handleClose = (): void => {
    feishuSettings.close();
    onClose();
  };

  return <FeishuNotificationSettingsModal open channelName={feishuSettings.channelName || channel.name} loading={feishuSettings.loading} saving={feishuSettings.saving} warning={feishuSettings.warning} info={feishuSettings.info} error={feishuSettings.error} loadFailed={feishuSettings.loadFailed} onWarningChange={feishuSettings.setWarning} onInfoChange={feishuSettings.setInfo} onSave={feishuSettings.save} onRetry={feishuSettings.reload} onClose={handleClose} />;
};
