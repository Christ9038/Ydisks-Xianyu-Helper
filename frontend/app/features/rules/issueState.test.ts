import { expect, test } from 'vitest';
import type { AutomationRunIssue } from './api';
import { canResolveAutomationIssue, filterAutomationIssues, loadAutomationPageData } from './issueState';

// issue 创建规则异常测试使用的最小异常对象。
const issue = (cookieID: string, allowed: AutomationRunIssue['allowed_resolutions']): AutomationRunIssue => ({
  id: cookieID === 'a' ? 1 : 2,
  cookie_id: cookieID,
  order_id: 'o',
  trigger_type: 'buyer_reviewed',
  error_message: 'error',
  issue_kind: 'invalid_snapshot',
  allowed_resolutions: allowed,
  action_cursor: 0,
  sent_count: 0,
  updated_at: '',
});

test('规则异常处理动作遵循后端允许列表',
  // 只有后端明确允许的人工动作才能被页面提交。
  () => {
    // invalid 无效数据。
    const invalid = issue('a', ['cancel']);
    expect(canResolveAutomationIssue(invalid, 'cancel')).toBe(true);
    expect(canResolveAutomationIssue(invalid, 'continue')).toBe(false);
    expect(canResolveAutomationIssue(invalid, 'retry')).toBe(false);
  });

test('规则异常按账号筛选后再决定是否展示面板',
  // 异常面板只展示当前账号的运行异常和延迟任务。
  () => {
    // visible 可见数据。
    const visible = filterAutomationIssues({ runs: [issue('a', ['cancel']), issue('b', ['retry', 'cancel'])], pending_tasks: [] }, 'missing');
    expect(visible.runs).toEqual([]);
    expect(visible.pending_tasks).toEqual([]);
  });

test('异常面板支持空筛选和延迟任务账号过滤',
  // 空筛选应保留全部异常，账号筛选只保留对应记录。
  () => {
    // pendingTask 是延迟任务筛选使用的最小异常对象。
    const pendingTask = { cookie_id: 'a' } as never;
    // issues 是包含运行异常和延迟任务的异常状态。
    const issues = { runs: [issue('a', ['cancel'])], pending_tasks: [pendingTask] };
    expect(filterAutomationIssues(issues, '').pending_tasks).toEqual([pendingTask]);
    expect(filterAutomationIssues(issues, 'a').pending_tasks).toEqual([pendingTask]);
  });

test('规则列表不因异常接口失败而停止加载', /* 当前回调处理用户交互或异步状态变化。 */ async () => {
  // receivedRules 保存主规则列表加载器收到的结果。
  const receivedRules: string[][] = [];
  // errors 保存异常接口失败后的错误回调。
  const errors: unknown[] = [];
  await loadAutomationPageData({
    loadRules: /* 当前回调处理用户交互或异步状态变化。 */ async () => ['rule'],
    loadIssues: /* 当前回调处理用户交互或异步状态变化。 */ async () => { throw new Error('issues unavailable'); },
    onRules: /* 当前回调处理用户交互或异步状态变化。 */ rules => receivedRules.push(rules),
    onIssues: /* 当前回调处理用户交互或异步状态变化。 */ () => { throw new Error('must not receive issues'); },
    onIssuesError: /* 当前回调处理用户交互或异步状态变化。 */ error => errors.push(error),
  });
  expect(receivedRules).toEqual([['rule']]);
  expect(errors).toHaveLength(1);
});
