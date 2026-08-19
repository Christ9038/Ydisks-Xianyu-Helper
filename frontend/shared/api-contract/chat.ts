// chat 只公开 OpenAPI 生成的聊天传输类型；聊天状态模型属于 chat feature。
import type { components } from './generated/schema';

/** ChatMessageTransport 表示生成的单条聊天消息。 */
export type ChatMessageTransport = components['schemas']['ChatMessage'];
/** ChatSessionTransport 表示生成的聊天会话摘要。 */
export type ChatSessionTransport = components['schemas']['ChatSession'];
/** ChatWebSocketTransport 表示生成的 WebSocket 事件联合。 */
export type ChatWebSocketTransport = components['schemas']['ChatWebSocketEvent'];
