import type { ChatMessage, ChatSession } from '../../../types';

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
