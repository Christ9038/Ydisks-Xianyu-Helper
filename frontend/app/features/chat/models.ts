// models 保存本 feature adapter 输出的 UI 模型；这些模型不直接代表 HTTP DTO。
/** 由当前 feature adapter 归一后的 AccountDetail UI 模型；不直接暴露 HTTP DTO。 */
export interface AccountDetail {
  /** 闲鱼账号稳定标识。 */
  id: string;
  /** 账号是否已配置平台凭证；摘要接口只返回状态，不返回 Cookie 明文。 */
  cookie_configured?: boolean;
  /** 账号是否允许运行。 */
  enabled: boolean;
  /** 是否自动确认订单。 */
  auto_confirm: boolean;
  /** 用户为账号设置的备注。 */
  remark?: string;
  /** 自动回复暂停时长，单位为分钟。 */
  pause_duration?: number;
  /** 暂停结束时间的 Unix 秒。 */
  paused_until?: number;
  /** 当前是否处于暂停状态。 */
  paused?: boolean;
  // 登录信息
  /** 用于密码登录的闲鱼用户名。 */
  username?: string;
  /** 是否已保存密码登录秘密；摘要接口只返回状态，不返回密码明文。 */
  login_password_configured?: boolean;
  /** 是否在密码登录时显示浏览器。 */
  show_browser?: boolean;
  // Frontend helpers
  /** 平台账号昵称。 */
  nickname?: string;
  /** 平台账号头像地址。 */
  avatar_url?: string;
  /** 资料刷新失败时的说明。 */
  profile_error?: string;
  /** 当前账号运行状态。 */
  runtime_state?: 'starting' | 'connecting' | 'online' | 'reconnecting' | 'auth_expired' | 'verification_required' | 'runtime_conflict' | 'error' | 'stopped' | 'disabled';
  /** 当前运行状态的用户可见说明。 */
  runtime_message?: string;
  /** 当前运行实例是否已连接。 */
  runtime_connected?: boolean;
  /** 运行状态快照的更新时间。 */
  runtime_updated_at?: string;
  // AI设置
  /** 是否启用账号 AI 回复。 */
  ai_enabled?: boolean;
  /** 允许的最大折扣比例。 */
  max_discount_percent?: number;
  /** 允许的最大折扣金额。 */
  max_discount_amount?: number;
  /** 允许的最大砍价轮次。 */
  max_bargain_rounds?: number;
  /** 账号自定义提示词。 */
  custom_prompts?: string;
	// 账号级计划任务
	/** 是否启用自动评价。 */
	auto_rate_enabled?: boolean;
	/** 自动评价使用的文案。 */
	rate_content?: string;
	/** 是否启用每日擦亮。 */
	auto_polish_enabled?: boolean;
	/** 每日擦亮执行时间。 */
	polish_time?: string;
	/** 最近一次自动评价扫描时间。 */
	last_rate_scan_at?: number;
	/** 最近一次擦亮日期。 */
	last_polish_date?: string;
	/** 最近一次擦亮时间。 */
	last_polish_at?: number;
}

/** 由当前 feature adapter 归一后的 ChatSession UI 模型；不直接暴露 HTTP DTO。 */
export interface ChatSession {
	/** 账号稳定标识。 */
	account_id: string;
	/** 会话稳定标识。 */
	chat_id: string;
	/** 买家平台标识。 */
	buyer_id: string;
	/** 买家昵称。 */
	buyer_name: string;
	/** 买家头像地址。 */
	buyer_avatar_url?: string;
	/** 会话关联商品标识。 */
	item_id?: string;
	/** 会话关联商品标题。 */
	item_title?: string;
	/** 会话关联商品主图的公开地址。 */
	item_image_url?: string;
	/** 最近一条消息内容。 */
	last_message: string;
	/** 最近一条消息的 Unix 秒时间戳。 */
	last_message_at: number;
	/** 未读消息数量。 */
	unread_count: number;
}

/** 由当前 feature adapter 归一后的 ChatMessage UI 模型；不直接暴露 HTTP DTO。 */
export interface ChatMessage {
	/** 消息数据库主键。 */
	id: number;
	/** 消息所属账号标识。 */
	account_id: string;
	/** 消息所属会话标识。 */
	chat_id: string;
	/** 平台消息去重键。 */
	message_key: string;
	/** 消息方向。 */
	direction: 'incoming' | 'outgoing';
	/** 发送者平台标识。 */
	sender_id: string;
	/** 发送者名称。 */
	sender_name: string;
  /** 消息类型；audio 表示需在浏览器解码的语音，system 表示平台通知或交易卡片。 */
	message_type: 'text' | 'image' | 'video' | 'audio' | 'system';
	/** 消息正文或媒体地址。 */
	content: string;
	/** 语音消息的秒级时长；非语音或平台未提供时省略。 */
	media_duration?: number;
	/** 消息发送状态。 */
	status: 'received' | 'sending' | 'sent' | 'failed';
	/** 平台已读状态；旧消息可能没有该字段。 */
	read_status?: number;
	/** 平台确认已读的 Unix 秒时间戳；未确认时省略。 */
	read_at?: number;
	/** 消息发送时间的 Unix 秒。 */
	sent_at: number;
}

/** 简单资源创建接口的数值主键响应。 */
/** 由当前 feature adapter 归一后的 MutationIDResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface MutationIDResponse {
  /** 资源创建是否完成。 */
  success: boolean;
  /** 新资源数值主键。 */
  id: number;
}

/** 简单变更接口的统一成功响应。 */
/** 由当前 feature adapter 归一后的 OperationResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface OperationResponse {
  /** 操作是否完成。 */
  success: boolean;
  /** 可选的操作说明。 */
  message?: string;
  /** 操作完成后是否需要重新登录。 */
  requires_relogin?: boolean;
}
