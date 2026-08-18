import type { ChatMessage,ChatSession } from './api';
import type { ChatReadReceipt } from './types';

/** 将未读数规范为徽标可展示的文本，超过两位数时统一显示 99+。 */
export const unreadBadgeLabel = (count: number): string => {
  // normalized 保存排除 NaN、负数与小数后的未读数量。
  const normalized = Number.isFinite(count) ? Math.max(0, Math.floor(count)) : 0;
  return normalized > 99 ? '99+' : String(normalized);
};

/** 根据未读文本长度返回稳定徽标尺寸，单数字必须为正圆，双数字及 99+ 才可横向扩展。 */
export const unreadBadgeClassName = (count: number): string => {
  // normalized 保存用于区分单数字与多数字徽标的规范未读数量。
  const normalized = Number.isFinite(count) ? Math.max(0, Math.floor(count)) : 0;
  return normalized < 10
    ? 'inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-red-500 text-[10px] font-bold leading-none text-white'
    : 'inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-red-500 px-1.5 text-[10px] font-bold leading-none text-white';
};

/** 将可确认的普通入站消息转换为平台已读回执。 */
export const collectChatReadReceipts = (messages: ChatMessage[], chatID: string): ChatReadReceipt[] => messages
  .filter(/* currentMessage 只保留平台可接受的普通入站消息。 */ currentMessage => (
    currentMessage.direction === 'incoming'
    && currentMessage.message_type !== 'system'
    && !currentMessage.message_key.startsWith('in-')
  ))
  .map(/* currentMessage 为每条消息构造其所属会话的已读确认。 */ currentMessage => ({
    messageId: currentMessage.message_key,
    sessionId: chatID,
    cid: `${chatID}@goofish`,
    conversationType: 1,
  }));

/** 按搜索条件筛选会话列表。 */
export const filterChatSessions = (sessions: ChatSession[], search: string, unreadOnly: boolean): ChatSession[] => {
  // keyword 搜索关键词。
  const keyword = search.trim().toLowerCase();
  return sessions.filter(/* 当前回调处理集合中的单个元素。 */ session => {
    if (unreadOnly && session.unread_count <= 0) return false;
    if (!keyword) return true;
    return [session.buyer_name, session.buyer_id, session.item_title, session.last_message]
      .some(/* 当前回调处理集合中的单个元素。 */ value => (value || '').toLowerCase().includes(keyword));
  });
};

/** 合并历史消息并按消息键去重。 */
export const mergeOlderMessages = (current: ChatMessage[], older: ChatMessage[]): ChatMessage[] => {
  // keys keys，负责当前功能中的对应处理。
  const keys = new Set(current.map(/* 当前回调处理集合中的单个元素。 */ message => message.message_key));
  return [...older.filter(/* 当前回调处理集合中的单个元素。 */ message => !keys.has(message.message_key)), ...current];
};

/** 合并实时消息并替换同消息键的临时记录。 */
export const mergeLiveMessage = (current: ChatMessage[], incoming: ChatMessage): ChatMessage[] => {
  // index 当前索引。
  const index = current.findIndex(/* 当前回调处理用户交互或异步状态变化。 */ message => message.message_key === incoming.message_key);
  if (index < 0) return [...current, incoming];
  return current.map(/* 当前回调处理集合中的单个元素。 */ (message, currentIndex) => currentIndex === index ? incoming : message);
};

/** 买家普通入站消息到达时，把此前同会话的已发送出站消息同步为已读。 */
export const markOutgoingMessagesReadByIncoming = (current: ChatMessage[], incoming: ChatMessage): ChatMessage[] => {
  if (incoming.direction !== 'incoming' || incoming.message_type === 'system') return current;
  // readAt 保存平台入站消息时间，缺失时使用本机时间作为 UI 增量更新回退值。
  const readAt = incoming.sent_at > 0 ? incoming.sent_at : Date.now();
  return current.map(/* 当前回调把已被买家后续消息确认的出站消息更新为已读。 */ message => (
    message.chat_id === incoming.chat_id
    && message.direction === 'outgoing'
    && message.status === 'sent'
    && message.sent_at <= incoming.sent_at
      ? { ...message, read_status: 2, read_at: readAt }
      : message
  ));
};

/** 判断 Chat 请求响应是否仍属于当前账号和会话。 */
export const isCurrentChatRequest = (currentSequence: number, requestSequence: number, signal: AbortSignal): boolean => (
  currentSequence === requestSequence && !signal.aborted
);

/** 判断错误是否来自请求主动取消。 */
export const isChatAbortError = (error: unknown): boolean => error instanceof Error && error.message === '请求已取消';

/** 将聊天时间戳格式化为列表时间。 */
export const formatClock = (value: number): string => {
  if (!value) return '';
  // date 日期。
  const date = new Date(value < 10_000_000_000 ? value * 1000 : value);
  // today 今天日期。
  const today = new Date();
  if (date.toDateString() === today.toDateString()) {
    return date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false });
  }
  return date.toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' });
};

/** 将聊天时间戳格式化为消息详情时间。 */
export const messageTime = (value: number): string => {
  // date 日期。
  const date = new Date(value < 10_000_000_000 ? value * 1000 : value);
  return date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false });
};
