import type { AccountDetail, ChatMessage, ChatSession } from '../../../shared/api-contract';

/** 按账号保存会话列表。 */
export type SessionsByAccount = Record<string, ChatSession[]>;

/** Chat 实时连接状态。 */
export type ChatLiveState = 'connecting' | 'online' | 'offline';

/** 平台已读接口要求的单条消息回执。 */
export interface ChatReadReceipt {
  /** 平台消息去重键。 */
  messageId: string;
  /** 所属平台会话标识。 */
  sessionId: string;
  /** 平台会话复合标识。 */
  cid: string;
  /** 平台定义的单聊会话类型。 */
  conversationType: number;
}

/** Chat Hook 的状态与交互能力。 */
export type ChatFeatureState = {
  /** 当前启用账号列表。 */
  accounts: AccountDetail[];
  /** 当前选中的账号 ID。 */
  activeAccountID: string;
  /** 当前账号的会话列表。 */
  activeSessions: ChatSession[];
  /** 当前选中的会话。 */
  selectedSession?: ChatSession;
  /** 当前选中的账号。 */
  activeAccount?: AccountDetail;
  /** 当前会话消息。 */
  messages: ChatMessage[];
  /** 会话搜索文本。 */
  search: string;
  /** 是否只显示未读会话。 */
  unreadOnly: boolean;
  /** 消息输入草稿。 */
  draft: string;
  /** 初始数据加载状态。 */
  loading: boolean;
  /** 当前会话消息加载状态。 */
  messagesLoading: boolean;
  /** 历史消息加载状态。 */
  olderLoading: boolean;
  /** 是否还有更早消息。 */
  hasOlder: boolean;
  /** 历史联系人加载状态。 */
  contactsLoading: boolean;
  /** 当前账号是否还有更多联系人。 */
  hasMoreContacts: boolean;
  /** 表情选择器展开状态。 */
  emojiOpen: boolean;
  /** 消息发送状态。 */
  sending: boolean;
  /** 当前错误信息。 */
  error: string;
  /** WebSocket 实时连接状态。 */
  liveState: ChatLiveState;
};
