import type { AutomationAuditStatus } from './auditModels';

// AuditStatusMeta 描述状态徽标的中文文案和样式。
export interface AuditStatusMeta {
  // label 是状态中文名。
  label: string;
  // badgeClass 是徽标的 Tailwind 类名。
  badgeClass: string;
}

// auditStatusOptions 是筛选下拉使用的状态列表，顺序即展示顺序。
export const auditStatusOptions: ReadonlyArray<{ /** value 是状态取值。 */ value: AutomationAuditStatus; /** label 是中文名。 */ label: string }> = [
  { value: 'success', label: '成功' },
  { value: 'failed', label: '失败' },
  { value: 'queued', label: '排队中' },
  { value: 'running', label: '执行中' },
  { value: 'skipped', label: '已跳过' },
  { value: 'needs_review', label: '待人工' },
  { value: 'canceled', label: '已取消' },
];

// auditStatusStyles 是各状态的徽标样式。
const auditStatusStyles: Record<AutomationAuditStatus, string> = {
  success: 'bg-emerald-50 text-emerald-700',
  failed: 'bg-red-50 text-red-700',
  queued: 'bg-amber-50 text-amber-700',
  running: 'bg-blue-50 text-blue-700',
  skipped: 'bg-gray-100 text-gray-600',
  needs_review: 'bg-orange-50 text-orange-700',
  canceled: 'bg-gray-100 text-gray-500',
};

// auditStatusMeta 返回状态的徽标信息；未知状态原样显示并使用中性样式，避免后端新增状态后页面空白。
export const auditStatusMeta = (status: string): AuditStatusMeta => {
  // known 是命中已知状态的选项。
  const known = auditStatusOptions.find(/* option 是当前比较的状态选项。 */ option => option.value === status);
  return known
    ? { label: known.label, badgeClass: auditStatusStyles[known.value] }
    : { label: status || '未知', badgeClass: 'bg-gray-100 text-gray-600' };
};

// auditEventTypeLabels 是行为分类的中文名。
const auditEventTypeLabels: Record<string, string> = {
  skipped: '跳过',
  queued: '排队',
  run_started: '开始执行',
  action_succeeded: '动作成功',
  action_failed: '动作失败',
  run_finished: '运行结束',
  bargain_free_ship: '二人小刀免拼',
  dead_letter: '延期任务转人工',
};

// auditEventTypeLabel 返回行为分类的中文名，未知分类原样显示。
export const auditEventTypeLabel = (eventType: string): string => auditEventTypeLabels[eventType] ?? eventType;

// auditTriggerLabels 是触发类型的中文名。
const auditTriggerLabels: Record<string, string> = {
  order_created: '订单创建',
  order_paid: '买家付款',
  order_completed: '交易完成',
  buyer_reviewed: '买家评价',
  review_missing_timeout: '催评价',
  bargain_pending: '待刀成',
};

// auditTriggerOptions 是筛选下拉使用的触发类型列表。
export const auditTriggerOptions: ReadonlyArray<{ /** value 是触发类型取值。 */ value: string; /** label 是中文名。 */ label: string }> = Object.entries(auditTriggerLabels).map(
  /* entry 是触发类型与中文名的键值对。 */ ([value, label]) => ({ value, label }),
);

// auditTriggerLabel 返回触发类型的中文名，未知类型原样显示。
export const auditTriggerLabel = (triggerType: string): string => auditTriggerLabels[triggerType] ?? triggerType;

// auditActionLabels 是动作类型的中文名。
const auditActionLabels: Record<string, string> = {
  send_card: '发卡',
  send_template: '发货模板',
  send_text: '文本消息',
  confirm_shipment: '确认发货',
  adjust_price: '改价',
};

// auditActionLabel 返回动作类型的中文名；运行级事件没有动作时返回空字符串。
export const auditActionLabel = (actionType: string): string => auditActionLabels[actionType] ?? actionType;

// auditSourceLabels 是事件来源的中文名。
const auditSourceLabels: Record<string, string> = {
  ws: '实时消息',
  scheduler: '定时扫描',
  recovery: '延期/恢复',
  manual: '手动',
};

// auditSourceLabel 返回事件来源的中文名，未知来源原样显示。
export const auditSourceLabel = (source: string): string => auditSourceLabels[source] ?? source;
