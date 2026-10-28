import { X } from 'lucide-react';
import React from 'react';
import { createPortal } from 'react-dom';
import { useRunTimeline } from '../auditLog';
import { auditActionLabel,auditEventTypeLabel,auditStatusMeta } from '../auditState';
import DeliveredCardsReveal from './DeliveredCardsReveal';

// AuditRunDrawerProps 描述运行时间线弹层需要的运行与订单信息。
export interface AuditRunDrawerProps {
  // runId 是要查看时间线的自动化运行标识。
  runId: number;
  // orderId 是该运行关联的订单号，存在发卡动作时用于按需查看卡密。
  orderId: string;
  // onClose 关闭弹层；关闭后已展示的卡密明文随组件卸载清空。
  onClose: () => void;
}

// AuditRunDrawer 展示一次运行的完整行为时间线，并提供按需查看已发卡密。
const AuditRunDrawer: React.FC<AuditRunDrawerProps> = ({ runId, orderId, onClose }) => {
  // timeline 保存该运行的事件时间线与读取状态。
  const timeline = useRunTimeline(runId);
  // events 是时间线事件，读取完成前为空。
  const events = timeline.page?.events ?? [];
  // delivered 表示该运行已成功执行过发卡或发货模板动作，此时才提供卡密查看入口。
  const delivered = events.some(/* event 是时间线中的一条事件。 */ event => event.status === 'success' && (event.actionType === 'send_card' || event.actionType === 'send_template'));
  return createPortal(
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-container" style={{ maxWidth: '40rem', maxHeight: '90vh' }} onClick={/* 当前回调阻止点击内容区时关闭弹层。 */ event => event.stopPropagation()}>
        <div className="px-6 py-5 border-b border-gray-100 flex items-center justify-between">
          <div>
            <h3 className="text-xl font-black text-gray-900">运行时间线 #{runId}</h3>
            {orderId ? <p className="text-sm text-gray-500 mt-1">订单 {orderId}</p> : null}
          </div>
          <button type="button" onClick={onClose} aria-label="关闭" className="p-2 rounded-xl hover:bg-gray-100">
            <X className="w-5 h-5" />
          </button>
        </div>
        <div className="p-6 space-y-4 overflow-y-auto">
          {timeline.loading ? <p className="text-sm text-gray-500">加载中…</p> : null}
          {timeline.error ? <p className="text-sm text-red-600">{timeline.error}</p> : null}
          {events.map(/* event 是时间线中的一条事件。 */ event => {
            // meta 是该事件状态的徽标信息。
            const meta = auditStatusMeta(event.status);
            return (
              <div key={event.id} className="rounded-2xl border border-gray-100 p-4 space-y-1">
                <div className="flex items-center justify-between gap-3">
                  <p className="text-sm font-bold text-gray-900">
                    {auditEventTypeLabel(event.eventType)}
                    {event.actionType ? ` · ${auditActionLabel(event.actionType)}` : ''}
                  </p>
                  <span className={`px-2.5 py-1 rounded-full text-xs font-bold ${meta.badgeClass}`}>{meta.label}</span>
                </div>
                <p className="text-xs text-gray-400">{new Date(event.createdAtMs).toLocaleString()}</p>
                {event.summary ? <p className="text-sm text-gray-700">{event.summary}</p> : null}
                {event.reason ? <p className="text-sm text-red-600">{event.reason}</p> : null}
              </div>
            );
          })}
          {delivered && orderId ? <DeliveredCardsReveal orderId={orderId} /> : null}
        </div>
      </div>
    </div>,
    document.body,
  );
};

export default AuditRunDrawer;
