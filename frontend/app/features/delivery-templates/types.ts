/** 发货模板编辑器使用的单条消息草稿。 */
export interface DeliveryTemplateMessageDraft {
  /** 消息正文，可使用 cards.key 和 custom.key 变量。 */
  content: string;
}

/** 发货模板编辑器的完整草稿。 */
export interface DeliveryTemplateDraft {
  /** 模板名称。 */
  name: string;
  /** 模板是否允许规则使用。 */
  enabled: boolean;
  /** 按顺序发送的消息。 */
  messages: DeliveryTemplateMessageDraft[];
}

/** 发货模板页面使用的服务端模型。 */
export interface DeliveryTemplate {
  /** 模板主键。 */
  id: number;
  /** 模板名称。 */
  name: string;
  /** 模板是否启用。 */
  enabled: boolean;
  /** 模板消息。 */
  messages: Array<{
    /** 消息主键。 */
    id: number;
    /** 消息顺序。 */
    sort_order: number;
    /** 消息正文。 */
    content: string;
  }>;
  /** 模板变量键。 */
  keys: string[];
  /** 模板正文中引用的自定义变量键。 */
  custom_keys?: string[];
  /** 创建时间。 */
  created_at: string;
  /** 更新时间。 */
  updated_at: string;
}
