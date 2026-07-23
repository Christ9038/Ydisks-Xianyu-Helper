import type { AutomationRunIssue, DeferredAutomationIssue } from '../services/api';

export type AutomationResolution = 'continue' | 'retry' | 'cancel';

export const canResolveAutomationIssue = (
  issue: AutomationRunIssue,
  resolution: AutomationResolution,
): boolean => issue.allowed_resolutions.includes(resolution);

export const filterAutomationIssues = (
  issues: { runs: AutomationRunIssue[]; pending_tasks: DeferredAutomationIssue[] },
  cookieID: string,
) => ({
  runs: issues.runs.filter(issue => !cookieID || issue.cookie_id === cookieID),
  pending_tasks: issues.pending_tasks.filter(issue => !cookieID || issue.cookie_id === cookieID),
});

export const automationIssueKindLabel = (kind: AutomationRunIssue['issue_kind']): string => ({
  external_result_unknown: '外部动作结果未知',
  invalid_snapshot: '历史数据无法恢复',
  rule_unavailable: '规则不可用',
  partial_failure: '部分动作失败',
  execution_failed: '动作执行失败',
}[kind] || '自动化异常');

export const loadAutomationPageData = async <TRule>(options: {
  loadRules: () => Promise<TRule[]>;
  loadIssues: () => Promise<{ runs: AutomationRunIssue[]; pending_tasks: DeferredAutomationIssue[] }>;
  onRules: (rules: TRule[]) => void;
  onIssues: (issues: { runs: AutomationRunIssue[]; pending_tasks: DeferredAutomationIssue[] }) => void;
  onIssuesError: (error: unknown) => void;
}): Promise<void> => {
  const issuesPromise = options.loadIssues().then(options.onIssues).catch(options.onIssuesError);
  const rules = await options.loadRules();
  options.onRules(rules);
  await issuesPromise;
};
