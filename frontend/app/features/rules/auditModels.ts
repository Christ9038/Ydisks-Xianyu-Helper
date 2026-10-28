// AutomationAuditStatus 是规则日志事件的状态集合，与后端审计状态一一对应。
export type AutomationAuditStatus = 'queued' | 'running' | 'success' | 'failed' | 'skipped' | 'needs_review' | 'canceled';

// AutomationAuditEvent 是规则日志中一条自动化行为的 UI 模型，只含非敏感摘要。
export interface AutomationAuditEvent {
  // id 是事件标识，也是列表渲染 key。
  id: number;
  // cookieId 是触发事件的账号标识。
  cookieId: string;
  // orderId 与 itemId 是关联订单和商品，可为空字符串。
  orderId: string;
  itemId: string;
  // ruleId 与 runId 是关联规则和运行，没有关联时为 0。
  ruleId: number;
  runId: number;
  // triggerType 是触发类型，source 是事件来源（ws、scheduler、recovery、manual）。
  triggerType: string;
  source: string;
  // eventType 是行为分类，status 是事件状态，actionType 是动作类型。
  eventType: string;
  // status 是事件状态，取值见 AutomationAuditStatus，未知值原样保留。
  status: string;
  // actionType 是动作类型，运行级事件为空字符串。
  actionType: string;
  // summary 是不含卡密正文的发货内容摘要，reason 是跳过或失败原因。
  summary: string;
  reason: string;
  // createdAtMs 是事件发生时刻（毫秒），由服务端秒级时间换算。
  createdAtMs: number;
}

// AutomationAuditQuery 是规则日志的筛选与分页条件，属于瞬时 UI 状态。
export interface AutomationAuditQuery {
  // cookieId 为空表示全部账号。
  cookieId: string;
  // status 为空表示全部状态。
  status: AutomationAuditStatus | '';
  // triggerType 为空表示全部触发类型。
  triggerType: string;
  // search 按订单号子串搜索。
  search: string;
  // runId 大于 0 时只看该运行的时间线。
  runId: number;
  // page 从 1 开始，pageSize 是每页条数。
  page: number;
  // pageSize 是每页条数。
  pageSize: number;
}

// AutomationAuditPage 是一页规则日志及分页信息。
export interface AutomationAuditPage {
  // events 是当前页事件。
  events: AutomationAuditEvent[];
  // total 是符合条件的事件总数。
  total: number;
  // totalPages 至少为 1。
  totalPages: number;
}

// AuditDeliveredCard 是订单已发出的一条卡密记录，卡密明文只在用户主动查看时才请求。
export interface AuditDeliveredCard {
  // id 是售出记录标识。
  id: number;
  // cardName 是卡密组名称快照。
  cardName: string;
  // content 是实际发给买家的卡密正文，仅用于所有者查看。
  content: string;
  // deliveredAtMs 是发出时刻（毫秒）。
  deliveredAtMs: number;
}
