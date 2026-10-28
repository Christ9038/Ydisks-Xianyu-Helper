import { ChevronLeft,ChevronRight,RefreshCw,Search } from 'lucide-react';
import React,{ useState } from 'react';
import { useAutomationAuditLog } from '../auditLog';
import type { AutomationAuditEvent,AutomationAuditStatus } from '../auditModels';
import type { AutomationAuditPanelProps } from '../types';
import { auditActionLabel,auditEventTypeLabel,auditSourceLabel,auditStatusMeta,auditStatusOptions,auditTriggerLabel,auditTriggerOptions } from '../auditState';
import AuditRunDrawer from './AuditRunDrawer';

// AutomationAuditPanel 展示自动化规则的全部行为日志：成功、失败、排队、跳过、待人工和已取消。
const AutomationAuditPanel: React.FC<AutomationAuditPanelProps> = ({ accountId, accountLabel }) => {
  // log 保存规则日志当前页、筛选条件和操作。
  const log = useAutomationAuditLog(accountId);
  // searchDraft 是订单号搜索输入框的草稿，提交后才应用到查询。
  const [searchDraft, setSearchDraft] = useState('');
  // selected 是当前打开时间线的事件；为 null 表示未打开。
  const [selected, setSelected] = useState<AutomationAuditEvent | null>(null);
  // events 是当前页事件。
  const events = log.page?.events ?? [];
  // totalPages 是总页数，首次加载完成前按 1 处理。
  const totalPages = log.page?.totalPages ?? 1;
  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <select value={log.query.status} onChange={/* 当前回调切换状态筛选。 */ event => log.setStatus(event.target.value as AutomationAuditStatus | '')} className="ios-input px-4 py-2.5 rounded-2xl text-sm" aria-label="状态筛选">
          <option value="">全部状态</option>
          {auditStatusOptions.map(/* option 是一个状态选项。 */ option => <option key={option.value} value={option.value}>{option.label}</option>)}
        </select>
        <select value={log.query.triggerType} onChange={/* 当前回调切换触发类型筛选。 */ event => log.setTriggerType(event.target.value)} className="ios-input px-4 py-2.5 rounded-2xl text-sm" aria-label="触发类型筛选">
          <option value="">全部触发</option>
          {auditTriggerOptions.map(/* option 是一个触发类型选项。 */ option => <option key={option.value} value={option.value}>{option.label}</option>)}
        </select>
        <form className="flex items-center gap-2" onSubmit={/* 当前回调提交订单号搜索。 */ event => { event.preventDefault(); log.applySearch(searchDraft); }}>
          <input value={searchDraft} onChange={/* 当前回调更新搜索草稿。 */ event => setSearchDraft(event.target.value)} placeholder="按订单号搜索" className="ios-input px-4 py-2.5 rounded-2xl text-sm w-48" aria-label="订单号搜索" />
          <button type="submit" className="p-2.5 rounded-2xl bg-gray-100 hover:bg-gray-200" aria-label="搜索"><Search className="w-4 h-4" /></button>
        </form>
        <label className="inline-flex items-center gap-2 text-sm text-gray-600">
          <input type="checkbox" checked={log.autoRefresh} onChange={/* 当前回调切换自动刷新。 */ event => log.setAutoRefresh(event.target.checked)} />
          自动刷新
        </label>
        <button type="button" onClick={log.reload} className="inline-flex items-center gap-2 px-4 py-2.5 rounded-2xl bg-gray-100 hover:bg-gray-200 text-sm font-bold text-gray-700">
          <RefreshCw className={`w-4 h-4 ${log.loading ? 'animate-spin' : ''}`} />
          刷新日志
        </button>
      </div>

      {log.error ? <p className="text-sm text-red-600">{log.error}</p> : null}

      <div className="overflow-x-auto rounded-2xl border border-gray-100 bg-white">
        <table className="w-full text-left text-sm">
          <thead className="bg-gray-50 text-xs text-gray-500">
            <tr>
              <th className="px-4 py-3">时间</th>
              <th className="px-4 py-3">账号</th>
              <th className="px-4 py-3">订单</th>
              <th className="px-4 py-3">触发</th>
              <th className="px-4 py-3">行为</th>
              <th className="px-4 py-3">状态</th>
              <th className="px-4 py-3">内容 / 原因</th>
            </tr>
          </thead>
          <tbody>
            {events.map(/* event 是当前页的一条事件。 */ event => {
              // meta 是该事件状态的徽标信息。
              const meta = auditStatusMeta(event.status);
              return (
                <tr key={event.id} className={`border-t border-gray-50 ${event.runId > 0 ? 'cursor-pointer hover:bg-gray-50' : ''}`} onClick={/* 当前回调在事件关联运行时打开时间线。 */ () => { if (event.runId > 0) setSelected(event); }}>
                  <td className="px-4 py-3 whitespace-nowrap text-gray-500">{new Date(event.createdAtMs).toLocaleString()}</td>
                  <td className="px-4 py-3">{accountLabel(event.cookieId)}</td>
                  <td className="px-4 py-3 font-mono text-xs">{event.orderId || '-'}</td>
                  <td className="px-4 py-3">{auditTriggerLabel(event.triggerType)}<span className="block text-xs text-gray-400">{auditSourceLabel(event.source)}</span></td>
                  <td className="px-4 py-3">{auditEventTypeLabel(event.eventType)}{event.actionType ? ` · ${auditActionLabel(event.actionType)}` : ''}</td>
                  <td className="px-4 py-3"><span className={`px-2.5 py-1 rounded-full text-xs font-bold ${meta.badgeClass}`}>{meta.label}</span></td>
                  <td className="px-4 py-3 max-w-xs">
                    {event.summary ? <p className="text-gray-800">{event.summary}</p> : null}
                    {event.reason ? <p className={event.status === 'success' ? 'text-gray-500' : 'text-red-600'}>{event.reason}</p> : null}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
        {events.length === 0 && !log.loading ? <div className="text-center py-16 text-gray-400">暂无规则日志</div> : null}
      </div>

      <div className="flex items-center justify-between text-sm text-gray-500">
        <span>共 {log.page?.total ?? 0} 条</span>
        <div className="flex items-center gap-2">
          <button type="button" disabled={log.query.page <= 1} onClick={/* 当前回调跳转上一页。 */ () => log.setPage(log.query.page - 1)} className="p-2 rounded-xl bg-gray-100 disabled:opacity-40" aria-label="上一页"><ChevronLeft className="w-4 h-4" /></button>
          <span>第 {log.query.page} 页 / 共 {totalPages} 页</span>
          <button type="button" disabled={log.query.page >= totalPages} onClick={/* 当前回调跳转下一页。 */ () => log.setPage(log.query.page + 1)} className="p-2 rounded-xl bg-gray-100 disabled:opacity-40" aria-label="下一页"><ChevronRight className="w-4 h-4" /></button>
        </div>
      </div>

      {selected ? <AuditRunDrawer key={selected.runId} runId={selected.runId} orderId={selected.orderId} onClose={/* 当前回调关闭时间线弹层。 */ () => setSelected(null)} /> : null}
    </section>
  );
};

export default AutomationAuditPanel;
