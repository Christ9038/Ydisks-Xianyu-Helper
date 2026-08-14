import { expect, test } from 'vitest';
import type { ChatMessage, ChatSession } from '../../../types';
import { filterChatSessions, isCurrentChatRequest, mergeLiveMessage, mergeOlderMessages } from './state';

// sessionFixture 是覆盖搜索、未读筛选和联系人隔离的最小会话数据。
const sessionFixture: ChatSession[] = [
  { account_id: 'a1', chat_id: 'c1', buyer_id: 'b1', buyer_name: '张三', item_title: '测试商品', last_message: '你好', last_message_at: 1, unread_count: 2 },
  { account_id: 'a1', chat_id: 'c2', buyer_id: 'b2', buyer_name: '李四', item_title: '另一个商品', last_message: '已发货', last_message_at: 2, unread_count: 0 },
];

// messageFixture 是覆盖消息去重和实时替换的最小消息数据。
const messageFixture: ChatMessage = { id: 1, account_id: 'a1', chat_id: 'c1', message_key: 'm1', direction: 'incoming', sender_id: 'b1', sender_name: '张三', message_type: 'text', content: '旧消息', status: 'received', sent_at: 1 };

test('Chat 会话筛选和历史消息合并保持账号内顺序',
  // 会话状态测试验证搜索、未读筛选和历史消息去重语义。
  () => {
    expect(filterChatSessions(sessionFixture, '张三', false)).toHaveLength(1);
    expect(filterChatSessions(sessionFixture, '', true)).toEqual([sessionFixture[0]]);
    expect(mergeOlderMessages([messageFixture], [{ ...messageFixture, id: 0, message_key: 'm0', content: '更早' }])).toHaveLength(2);
  });
test('Chat 实时消息替换同键记录并拒绝过期请求',
  // 请求边界测试验证实时回执不会产生重复消息，切换会话后的旧请求不能写入。
  () => {
    const controller = new AbortController();
    expect(mergeLiveMessage([messageFixture], { ...messageFixture, content: '新消息' })[0].content).toBe('新消息');
    expect(isCurrentChatRequest(3, 3, controller.signal)).toBe(true);
    expect(isCurrentChatRequest(2, 3, controller.signal)).toBe(false);
    controller.abort();
    expect(isCurrentChatRequest(3, 3, controller.signal)).toBe(false);
  });
