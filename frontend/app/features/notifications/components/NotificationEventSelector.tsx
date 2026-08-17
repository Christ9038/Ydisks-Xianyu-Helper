import React from 'react';
import { Check } from 'lucide-react';
import { notificationEvents } from '../state';
import type { NotificationEventType } from '../../../../shared/api-contract';

// NotificationEventSelectorProps 描述通知事件绑定组件的输入状态。
export interface NotificationEventSelectorProps {
  // selectedEvents 是当前已选择的通知事件。
  selectedEvents: NotificationEventType[];
  // onToggleEvent 切换指定通知事件的绑定状态。
  onToggleEvent: (event: NotificationEventType) => void;
}

// NotificationEventSelector 渲染通知渠道可订阅的事件列表。
export const NotificationEventSelector: React.FC<NotificationEventSelectorProps> = ({ selectedEvents, onToggleEvent }) => {
  // handleEventClick 将用户点击的事件传回表单状态。
  const handleEventClick = (event: React.MouseEvent<HTMLButtonElement>) => {
    // eventValue 是按钮数据属性中的后端事件类型。
    const eventValue = event.currentTarget.dataset.event as NotificationEventType | undefined;
    if (eventValue) onToggleEvent(eventValue);
  };

  return (
    <div className="space-y-3">
      <div>
        <label className="block text-sm font-bold text-gray-800">通知内容</label>
        <p className="text-xs text-gray-500 mt-1">不选择表示接收全部通知；选择后仅接收勾选类型。</p>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
        {notificationEvents.map(
          // event 是当前通知事件定义。
          event => {
            // checked 表示当前事件是否已被选中。
            const checked = selectedEvents.includes(event.value);
            return (
              <button
                key={event.value}
                type="button"
                data-event={event.value}
                onClick={handleEventClick}
                className={`text-left rounded-xl border px-3 py-2.5 transition-colors ${checked ? 'border-brand bg-blue-50' : 'border-gray-100 hover:border-gray-300'}`}
              >
                <div className="flex items-center gap-2">
                  <span className={`w-4 h-4 rounded border flex items-center justify-center ${checked ? 'bg-brand border-brand' : 'border-gray-300'}`}>
                    {checked && <Check className="w-3 h-3 text-white" />}
                  </span>
                  <span className={`text-sm font-bold ${checked ? 'text-brand' : 'text-gray-800'}`}>{event.label}</span>
                </div>
                <p className="text-xs text-gray-500 mt-1 pl-6 leading-5">{event.description}</p>
              </button>
            );
          },
        )}
      </div>
    </div>
  );
};
