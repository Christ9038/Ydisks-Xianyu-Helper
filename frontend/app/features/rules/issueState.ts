import type { AutomationRunIssue,DeferredAutomationIssue } from './api';

// AutomationResolution 表示自动化运行异常可执行的人工处理动作。
export type AutomationResolution = 'continue' | 'retry' | 'cancel';

// AutomationIssueState 保存规则页异常面板所需的两类异常记录。
export interface AutomationIssueState {
  // runs 是需要人工确认的自动化运行记录。
  runs: AutomationRunIssue[];
  // pending_tasks 是达到重试上限的延迟任务记录。
  pending_tasks: DeferredAutomationIssue[];
}

// canResolveAutomationIssue 判断后端策略是否允许指定处理动作。
export const canResolveAutomationIssue = (
  issue: AutomationRunIssue,
  resolution: AutomationResolution,
): boolean => issue.allowed_resolutions.includes(resolution);

// filterAutomationIssues 按账号筛选异常，保持异常面板与规则列表一致。
export const filterAutomationIssues = (issues: AutomationIssueState, cookieID: string): AutomationIssueState => ({
  runs: issues.runs.filter(
    // 运行异常筛选器只保留当前账号的记录。
    issue => !cookieID || issue.cookie_id === cookieID,
  ),
  pending_tasks: issues.pending_tasks.filter(
    // 延迟任务筛选器只保留当前账号的记录。
    issue => !cookieID || issue.cookie_id === cookieID,
  ),
});

// automationIssueKindLabel 将后端异常类别转换成用户可读的中文标签。
export const automationIssueKindLabel = (kind: AutomationRunIssue['issue_kind']): string => ({
  external_result_unknown: '外部动作结果未知',
  invalid_snapshot: '历史数据无法恢复',
  rule_unavailable: '规则不可用',
  partial_failure: '部分动作失败',
  execution_failed: '动作执行失败',
}[kind] || '自动化异常');

// AutomationPageDataLoaders 描述规则页规则列表与异常列表的独立加载器。
export interface AutomationPageDataLoaders<TRule> {
  // loadRules 加载自动化规则列表。
  loadRules: () => Promise<TRule[]>;
  // loadIssues 加载自动化异常列表。
  loadIssues: () => Promise<AutomationIssueState>;
  // onRules 接收规则列表成功结果。
  onRules: (rules: TRule[]) => void;
  // onIssues 接收异常列表成功结果。
  onIssues: (issues: AutomationIssueState) => void;
  // onIssuesError 接收异常列表失败结果。
  onIssuesError: (error: unknown) => void;
}

// loadAutomationPageData 并行加载规则与异常，且允许异常接口单独失败。
export const loadAutomationPageData = async <TRule>(options: AutomationPageDataLoaders<TRule>): Promise<void> => {
  // issuesPromise 先启动异常请求，避免阻塞主要规则列表。
  const issuesPromise = options.loadIssues().then(options.onIssues).catch(options.onIssuesError);
  // rules 是主要规则请求的成功结果。
  const rules = await options.loadRules();
  options.onRules(rules);
  await issuesPromise;
};
