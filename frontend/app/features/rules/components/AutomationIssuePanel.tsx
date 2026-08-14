import { AlertCircle } from 'lucide-react';
import type { AutomationRunIssue, DeferredAutomationIssue } from '../api';
import { automationIssueKindLabel, canResolveAutomationIssue, type AutomationResolution } from '../issueState';

// AutomationIssuePanelProps 描述人工处理异常面板的输入和动作回调。
export interface AutomationIssuePanelProps {
  // runs 是需要处理的自动化运行异常。
  runs: AutomationRunIssue[];
  // pendingTasks 是需要重试或忽略的延迟任务异常。
  pendingTasks: DeferredAutomationIssue[];
  // onResolveRun 处理自动化运行的继续、重试或终止动作。
  onResolveRun: (id: number, resolution: AutomationResolution) => void;
  // onResolveDeferredTask 处理延迟任务的重试或忽略动作。
  onResolveDeferredTask: (id: number, resolution: 'retry' | 'dismiss') => void;
}

// AutomationIssuePanel 展示规则执行失败后需要用户确认的外部动作状态。
export const AutomationIssuePanel = ({
  runs,
  pendingTasks,
  onResolveRun,
  onResolveDeferredTask,
}: AutomationIssuePanelProps) => (
  <section className="rounded-2xl border border-red-200 bg-red-50 p-5 space-y-4">
    <div className="flex items-start gap-3">
      <AlertCircle className="w-5 h-5 text-red-600 mt-0.5" />
      <div>
        <h3 className="font-black text-red-900">需要人工处理的自动化任务</h3>
        <p className="text-sm text-red-700 mt-1">请先在闲鱼聊天、订单或商品列表中核对真实结果，再选择继续或重试。</p>
      </div>
    </div>
    <div className="space-y-3">
      {runs.map(
        // 运行异常渲染器展示外部动作状态和允许的处理按钮。
        issue => (
        <div key={`run-${issue.id}`} className="rounded-xl border border-red-100 bg-white p-4 flex flex-col lg:flex-row lg:items-center justify-between gap-3">
          <div className="min-w-0">
            <div className="font-bold text-gray-900">账号 {issue.cookie_id} · 订单 {issue.order_id || '-'} · 已记录发送 {issue.sent_count} 条</div>
            <div className="text-xs font-bold text-red-800 mt-1">{automationIssueKindLabel(issue.issue_kind)}</div>
            <div className="text-xs text-red-700 mt-1 break-words">{issue.error_message}</div>
          </div>
          <div className="flex flex-wrap gap-2 shrink-0">
            {canResolveAutomationIssue(issue, 'continue') && <button onClick={
              // 继续动作由父级统一执行并刷新规则状态。
              () => onResolveRun(issue.id, 'continue')
            } className="px-3 py-2 rounded-lg bg-emerald-100 text-emerald-800 text-xs font-bold">已执行，继续下一步</button>}
            {canResolveAutomationIssue(issue, 'retry') && <button onClick={
              // 重试动作由父级统一执行并刷新规则状态。
              () => onResolveRun(issue.id, 'retry')
            } className="px-3 py-2 rounded-lg bg-amber-100 text-amber-800 text-xs font-bold">未执行，安全重试</button>}
            {canResolveAutomationIssue(issue, 'cancel') && <button onClick={
              // 终止动作由父级统一执行并刷新规则状态。
              () => onResolveRun(issue.id, 'cancel')
            } className="px-3 py-2 rounded-lg bg-gray-100 text-gray-700 text-xs font-bold">终止</button>}
          </div>
        </div>
      ))}
      {pendingTasks.map(
        // 延迟任务渲染器展示重试和忽略按钮。
        issue => (
        <div key={`task-${issue.id}`} className="rounded-xl border border-red-100 bg-white p-4 flex flex-col lg:flex-row lg:items-center justify-between gap-3">
          <div className="min-w-0">
            <div className="font-bold text-gray-900">账号 {issue.cookie_id} · 延迟任务重试已达 {issue.attempt_count} 次</div>
            <div className="text-xs text-red-700 mt-1 break-words">{issue.error_message}</div>
          </div>
          <div className="flex gap-2 shrink-0">
            <button onClick={
              // 重试延迟任务由父级统一执行。
              () => onResolveDeferredTask(issue.id, 'retry')
            } className="px-3 py-2 rounded-lg bg-amber-100 text-amber-800 text-xs font-bold">重新入队</button>
            <button onClick={
              // 忽略延迟任务由父级统一执行。
              () => onResolveDeferredTask(issue.id, 'dismiss')
            } className="px-3 py-2 rounded-lg bg-gray-100 text-gray-700 text-xs font-bold">忽略</button>
          </div>
        </div>
      ))}
    </div>
  </section>
);
