import type { Dispatch,ElementType,SetStateAction } from 'react';
import type {
AccountDetail,
  AutomationAction,
  AutomationTriggerType,
  Card,
  DefaultReply,
  DeliveryTemplate,
Item,
ReplyRule,
ShippingRule,
ShippingVariant,
} from './api';
import type { AutomationRunIssue,DeferredAutomationIssue } from './api';

// Rules feature 通过此类型入口重导出共享领域模型，避免页面直接依赖全局类型文件。
export type {
AccountDetail,
  AutomationAction,
  AutomationTriggerType,
  Card,
  DefaultReply,
  DeliveryTemplate,
Item,
ReplyRule,
ShippingRule,
ShippingVariant
} from './api';

// RulesTab 表示规则页面的三个业务页签。
export type RulesTab = 'automation' | 'reply' | 'default';

// RulesProps 描述规则页面从父级接收的联动参数。
export interface RulesProps {
  // initialDeliveryTarget 表示商品页跳转过来的待配置发货目标。
  initialDeliveryTarget?: {
    // cookieId 表示目标账号标识。
    cookieId: string;
    // itemId 表示目标商品标识。
    itemId: string;
    // requestId 用于区分连续的外部跳转请求。
    requestId: number;
  };
  // onDeliveryTargetHandled 表示页面完成外部跳转处理后的回调。
  onDeliveryTargetHandled?: () => void;
}

// DefaultReplyForm 表示账号默认回复弹窗的可编辑字段。
export interface DefaultReplyForm {
  // cookie_id 表示默认回复所属账号。
  cookie_id: string;
  // enabled 表示是否启用默认回复。
  enabled: boolean;
  // reply_content 表示默认文字回复内容。
  reply_content: string;
  // reply_once 表示是否只对每个会话回复一次。
  reply_once: boolean;
  // reply_image_url 表示可选的默认图片地址。
  reply_image_url: string;
}

// TriggerMeta 描述自动化触发类型在页面中的展示元数据。
export interface TriggerMeta {
  // label 是完整的触发类型名称。
  label: string;
  // shortLabel 是列表筛选中使用的短名称。
  shortLabel: string;
  // description 是创建规则时展示的说明。
  description: string;
  // flow 是该触发类型的步骤摘要。
  flow: string[];
  // accent 是页面使用的主题色名称。
  accent: 'blue' | 'emerald' | 'amber' | 'violet';
  // icon 是触发类型对应的图标组件。
  icon: ElementType;
}

// AutomationIssueState 保存规则页需要人工处理的自动化异常。
export interface AutomationIssueState {
  // runs 是暂停中的自动化运行记录。
  runs: AutomationRunIssue[];
  // pending_tasks 是等待重试的延迟任务记录。
  pending_tasks: DeferredAutomationIssue[];
}

// RulesReferenceData 保存规则编辑器依赖的账号、卡密和商品参考数据。
export interface RulesReferenceData {
  // accounts 是可选择的闲鱼账号摘要。
  accounts: AccountDetail[];
  // cards 是可用于动作配置的卡密库存。
  cards: Card[];
  // items 是可绑定自动化规则的商品列表。
  items: Item[];
  // deliveryTemplates 是可用于模板动作的用户发货模板。
  deliveryTemplates: DeliveryTemplate[];
  // defaultReplies 是按账号索引的默认回复配置。
  defaultReplies: Record<string, DefaultReply>;
}

// RulesDataSet 保存规则页服务端数据和分页元数据。
export interface RulesDataSet extends RulesReferenceData {
  // automationRules 是当前自动化规则分页数据。
  automationRules: ShippingRule[];
  // automationIssues 是当前账号筛选下的异常源数据。
  automationIssues: AutomationIssueState;
  // replyRules 是当前账号的关键词回复规则。
  replyRules: ReplyRule[];
  // automationTotal 是自动化规则总数。
  automationTotal: number;
  // automationTotalPages 是自动化规则总页数。
  automationTotalPages: number;
  // automationTriggerCounts 是服务端返回的触发类型聚合数量。
  automationTriggerCounts: Record<string, number>;
}

// RulesDataOptions 描述规则数据 Hook 的筛选条件和状态回调。
export interface RulesDataOptions {
  // activeTab 是当前规则页签。
  activeTab: RulesTab;
  // selectedAccountId 是当前账号筛选值。
  selectedAccountId: string;
  // automationTriggerFilter 是自动化触发类型筛选值。
  automationTriggerFilter: AutomationTriggerType | '';
  // automationStatusFilter 是自动化启用状态筛选值。
  automationStatusFilter: 'all' | 'enabled' | 'disabled';
  // debouncedAutomationSearch 是已去抖的规则搜索词。
  debouncedAutomationSearch: string;
  // automationPage 是当前自动化规则页码。
  automationPage: number;
  // automationPageSize 是当前自动化规则分页大小。
  automationPageSize: number;
  // setSelectedAccountId 用于在参考数据加载后选择首个账号。
  setSelectedAccountId: Dispatch<SetStateAction<string>>;
  // onAutomationPageChange 接收服务端修正后的有效页码。
  onAutomationPageChange?: (page: number) => void;
}

// RulesDataResult 暴露规则页需要的服务端状态、更新器和请求动作。
export interface RulesDataResult extends RulesDataSet {
  // loading 表示当前是否存在规则数据请求。
  loading: boolean;
  // setLoading 用于外部联动加载场景暂时接管页面加载指示器。
  setLoading: Dispatch<SetStateAction<boolean>>;
  // setAutomationRules 用于外部跳转场景写入规则列表。
  setAutomationRules: Dispatch<SetStateAction<ShippingRule[]>>;
  // setCards 用于外部跳转场景写入卡密参考数据。
  setCards: Dispatch<SetStateAction<Card[]>>;
  // setItems 用于外部跳转场景写入商品参考数据。
  setItems: Dispatch<SetStateAction<Item[]>>;
  // loadReferenceData 加载规则编辑器所需的共享参考数据。
  loadReferenceData: () => Promise<void>;
  // loadAutomationRules 加载当前筛选条件下的自动化规则和异常。
  loadAutomationRules: () => Promise<void>;
  // loadReplyRules 加载当前账号的关键词回复规则。
  loadReplyRules: () => Promise<void>;
  // loadDefaultReplies 加载所有账号的默认回复。
  loadDefaultReplies: () => Promise<void>;
  // refresh 根据当前页签刷新对应的服务端数据。
  refresh: () => Promise<void>;
}

// RuleDraftFields 汇总规则编辑器中需要按动作类型读取的领域字段。
export type RuleDraftFields = Pick<ShippingRule, 'actions' | 'variants' | 'config_json'>;

// AutomationActionPatch 表示规则页面对自动化动作的局部更新。
export type AutomationActionPatch = Partial<AutomationAction>;

// ShippingVariantPatch 表示规则页面对发货规格的局部更新。
export type ShippingVariantPatch = Partial<ShippingVariant>;
