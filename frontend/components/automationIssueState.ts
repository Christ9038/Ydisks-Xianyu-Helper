// 旧目录保留兼容入口，实际实现统一归属 Rules feature。
export {
  automationIssueKindLabel,
  canResolveAutomationIssue,
  filterAutomationIssues,
  loadAutomationPageData,
} from '../app/features/rules/issueState';
export type { AutomationResolution } from '../app/features/rules/issueState';
