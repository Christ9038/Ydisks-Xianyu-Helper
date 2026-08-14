import React from 'react';
import { Bell, Edit2, Loader2, Send, Trash2 } from 'lucide-react';
import type { NotificationChannel } from '../../../../types';
import { notificationChannelTypes, notificationEventSummary } from '../state';

// NotificationChannelListProps 描述通知渠道列表的操作边界。
export interface NotificationChannelListProps {
  // channels 是当前可展示的通知渠道。
  channels: NotificationChannel[];
  // testingId 是当前正在测试发送的渠道 ID。
  testingId: string;
  // onEdit 打开指定渠道的编辑弹窗。
  onEdit: (channel: NotificationChannel) => void;
  // onDelete 删除指定渠道。
  onDelete: (channel: NotificationChannel) => void | Promise<void>;
  // onToggleEnabled 切换指定渠道的启用状态。
  onToggleEnabled: (channel: NotificationChannel) => void | Promise<void>;
  // onTest 发送指定渠道的测试通知。
  onTest: (channel: NotificationChannel) => void | Promise<void>;
}

// NotificationChannelList 渲染渠道摘要、启用状态和操作按钮。
export const NotificationChannelList: React.FC<NotificationChannelListProps> = ({ channels, testingId, onEdit, onDelete, onToggleEnabled, onTest }) => {
  // findChannel 从按钮数据属性中查找对应渠道。
  const findChannel = (event: React.MouseEvent<HTMLButtonElement>): NotificationChannel | null => channels.find(
    // channel 是当前待匹配的渠道数据。
    channel => channel.id === event.currentTarget.dataset.channelId,
  ) || null;
  // handleToggleClick 处理渠道启用状态按钮点击。
  const handleToggleClick = (event: React.MouseEvent<HTMLButtonElement>) => {
    // channel 是按钮对应的渠道数据。
    const channel = findChannel(event);
    if (channel) void onToggleEnabled(channel);
  };
  // handleTestClick 处理渠道测试按钮点击。
  const handleTestClick = (event: React.MouseEvent<HTMLButtonElement>) => {
    // channel 是按钮对应的渠道数据。
    const channel = findChannel(event);
    if (channel) void onTest(channel);
  };
  // handleEditClick 处理渠道编辑按钮点击。
  const handleEditClick = (event: React.MouseEvent<HTMLButtonElement>) => {
    // channel 是按钮对应的渠道数据。
    const channel = findChannel(event);
    if (channel) onEdit(channel);
  };
  // handleDeleteClick 处理渠道删除按钮点击。
  const handleDeleteClick = (event: React.MouseEvent<HTMLButtonElement>) => {
    // channel 是按钮对应的渠道数据。
    const channel = findChannel(event);
    if (channel) void onDelete(channel);
  };
  // renderChannel 渲染单个通知渠道卡片。
  const renderChannel = (channel: NotificationChannel) => {
    // meta 是当前渠道类型的静态展示配置。
    const meta = notificationChannelTypes[channel.type] || notificationChannelTypes.webhook;
    // Icon 是当前渠道类型对应的图标组件。
    const Icon = meta.icon;
    return (
      <div key={channel.id} className="ios-card rounded-xl p-5 bg-white flex items-center gap-4">
        <div className="w-11 h-11 rounded-xl bg-gray-100 flex items-center justify-center flex-shrink-0"><Icon className="w-5 h-5 text-gray-600" /></div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="font-bold text-gray-900 truncate">{channel.name}</span>
            <span className="text-xs px-2 py-0.5 rounded-full bg-gray-100 text-gray-600 font-medium">{meta.label}</span>
            {!channel.enabled && <span className="text-xs px-2 py-0.5 rounded-full bg-gray-100 text-gray-400 font-medium">已停用</span>}
          </div>
          <div className="text-xs text-gray-500 mt-1 truncate">
            {meta.fields.map(
              // field 是当前渠道摘要需要读取的配置字段。
              field => channel.config?.[field.key],
            ).filter(Boolean).map(
              // value 是渠道配置中非空的摘要值。
              (value, index) => <span key={index} className="mr-3">{String(value).length > 40 ? `${String(value).slice(0, 40)}…` : String(value)}</span>,
            )}
          </div>
          <div className="text-xs text-gray-400 mt-1 truncate">订阅：{notificationEventSummary(channel.event_types)}</div>
        </div>
        <div className="flex items-center gap-2 flex-shrink-0">
          <button data-channel-id={channel.id} onClick={handleToggleClick} className={`px-3 py-1.5 rounded-lg text-xs font-bold transition-colors ${channel.enabled ? 'bg-green-50 text-green-700 hover:bg-green-100' : 'bg-gray-100 text-gray-500 hover:bg-gray-200'}`}>
            {channel.enabled ? '启用中' : '已停用'}
          </button>
          <button data-channel-id={channel.id} onClick={handleTestClick} disabled={testingId === channel.id} className="px-3 py-1.5 rounded-lg text-xs font-bold bg-blue-50 text-brand hover:bg-blue-100 transition-colors flex items-center gap-1 disabled:opacity-50">
            {testingId === channel.id ? <Loader2 className="w-3 h-3 animate-spin" /> : <Send className="w-3 h-3" />}测试
          </button>
          <button data-channel-id={channel.id} onClick={handleEditClick} className="w-8 h-8 rounded-lg bg-gray-100 hover:bg-gray-200 flex items-center justify-center text-gray-600 transition-colors" title="编辑"><Edit2 className="w-4 h-4" /></button>
          <button data-channel-id={channel.id} onClick={handleDeleteClick} className="w-8 h-8 rounded-lg bg-red-50 hover:bg-red-100 flex items-center justify-center text-red-500 transition-colors" title="删除"><Trash2 className="w-4 h-4" /></button>
        </div>
      </div>
    );
  };

  if (channels.length === 0) {
    return <div className="ios-card rounded-xl p-12 bg-white text-center"><Bell className="w-12 h-12 text-gray-300 mx-auto mb-4" /><p className="text-gray-500 font-medium">还没有配置任何通知渠道</p><p className="text-gray-400 text-sm mt-1">点击右上角「新建渠道」开始配置</p></div>;
  }
  return <div className="space-y-3">{channels.map(renderChannel)}</div>;
};
