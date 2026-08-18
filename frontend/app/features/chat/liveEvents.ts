import type { ChatMessage } from '../../../shared/api-contract';
import type { ChatLiveState } from './types';

/** ChatLiveEvent 描述全局聊天连接向已挂载聊天页面发布的最小实时事件集合。 */
export type ChatLiveEvent =
  | { /** type 标识事件为连接状态变更。 */ type: 'connection'; /** state 保存连接的最新可展示状态。 */ state: ChatLiveState }
  | { /** type 标识事件为一条实时聊天消息。 */ type: 'message'; /** message 保存已通过服务端权限过滤的非敏感聊天消息。 */ message: ChatMessage };

/** ChatLiveEventListener 描述聊天页订阅全局连接事件时提供的接收回调。 */
export type ChatLiveEventListener = (event: ChatLiveEvent) => void;

// listeners 保存当前已挂载聊天页面的事件订阅者；应用壳 WebSocket 是唯一发布者，订阅者自行注销。
const listeners = new Set<ChatLiveEventListener>();
// currentConnectionState 保存最近一次全局聊天连接状态，使晚进入聊天页能立即渲染准确状态。
let currentConnectionState: ChatLiveState = 'connecting';
// unreadStatusListeners 保存全局通知 owner 的未读状态监听者；Chat 页面是唯一汇总并发布未读状态的消费者。
const unreadStatusListeners = new Set<(hasUnreadChatMessage: boolean) => void>();

/** subscribeToChatLiveEvents 注册聊天页实时事件监听，并立即回放当前连接状态，返回取消订阅函数。 */
export const subscribeToChatLiveEvents = (/** listener 保存当前聊天页接收全局实时事件的回调。 */ listener: ChatLiveEventListener): (() => void) => {
  listener({ type: 'connection', state: currentConnectionState });
  listeners.add(listener);
  return /* 当前清理函数在聊天页卸载时移除订阅，避免保留失效 React 状态写入。 */ () => listeners.delete(listener);
};

/** publishChatConnectionState 由认证应用壳唯一 WebSocket 所有者发布连接状态给当前聊天页。 */
export const publishChatConnectionState = (state: ChatLiveState): void => {
  currentConnectionState = state;
  // listener 保存当前遍历到的聊天页面订阅回调。
  for (const listener /* listener 保存当前遍历到的聊天页面订阅回调。 */ of listeners) listener({ type: 'connection', state });
};

/** publishChatLiveMessage 由认证应用壳唯一 WebSocket 所有者发布一条服务端聊天消息给当前聊天页。 */
export const publishChatLiveMessage = (message: ChatMessage): void => {
  // listener 保存当前遍历到的聊天页面订阅回调。
  for (const listener /* listener 保存当前遍历到的聊天页面订阅回调。 */ of listeners) listener({ type: 'message', message });
};

/** subscribeToChatUnreadStatus 注册全局未读状态监听，返回由通知 owner 卸载时调用的取消订阅函数。 */
export const subscribeToChatUnreadStatus = (/** listener 保存接收全部聊天会话未读聚合结果的回调。 */ listener: (hasUnreadChatMessage: boolean) => void): (() => void) => {
  unreadStatusListeners.add(listener);
  return /* 当前清理函数在应用壳卸载时释放未读状态监听，避免保留失效状态 setter。 */ () => unreadStatusListeners.delete(listener);
};

/** publishChatUnreadStatus 由 Chat 页面按所有已加载会话的 unread_count 聚合结果更新侧边栏红点。 */
export const publishChatUnreadStatus = (hasUnreadChatMessage: boolean): void => {
  // listener 保存当前遍历到的未读状态监听回调。
  for (const listener /* listener 保存当前遍历到的未读状态监听回调。 */ of unreadStatusListeners) listener(hasUnreadChatMessage);
};
