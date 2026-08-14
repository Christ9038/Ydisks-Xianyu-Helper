// 规则 feature 只通过本适配层访问共享 API，组件不直接依赖网络客户端。
export {
  clearDefaultReplyRecords,
  deleteDefaultReply,
  deleteReplyRule,
  deleteShippingRule,
  getAccountDetails,
  getCards,
  getDefaultReplies,
  getDefaultReply,
  getItems,
  getAutomationIssues,
  getReplyRules,
  getShippingRules,
  getShippingRulesPage,
  updateDefaultReply,
  updateReplyRule,
  updateShippingRule,
  resolveAutomationRun,
  resolveDeferredAutomationTask,
} from '../../../services/api';

// 规则异常类型由共享 API 层定义，feature 通过类型重导出保持边界稳定。
export type { AutomationRunIssue, DeferredAutomationIssue } from '../../../services/api';
