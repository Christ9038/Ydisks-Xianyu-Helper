import { expect,test } from 'vitest';
import type { ChatMessage,ChatSession } from '../../../shared/api-contract/chat';
import { collectChatReadReceipts,filterChatSessions,formatClock,isChatAbortError,isCurrentChatRequest,markOutgoingMessagesReadByIncoming,mergeLiveMessage,mergeOlderMessages,messageTime,unreadBadgeClassName,unreadBadgeLabel } from './state';

// sessionFixture 是覆盖搜索、未读筛选和联系人隔离的最小会话数据。
const sessionFixture: ChatSession[] = [
  { account_id: 'a1', chat_id: 'c1', buyer_id: 'b1', buyer_name: '张三', item_title: '测试商品', last_message: '你好', last_message_at: 1, unread_count: 2 },
  { account_id: 'a1', chat_id: 'c2', buyer_id: 'b2', buyer_name: '李四', item_title: '另一个商品', last_message: '已发货', last_message_at: 2, unread_count: 0 },
];

// messageFixture 是覆盖消息去重和实时替换的最小消息数据。
const messageFixture: ChatMessage = { id: 1, account_id: 'a1', chat_id: 'c1', message_key: 'm1', direction: 'incoming', sender_id: 'b1', sender_name: '张三', message_type: 'text', content: '旧消息', status: 'received', sent_at: 1 };

test('Chat 已读回执排除系统消息和平台内部消息',
  // 已读回执测试确保接口仅接收可确认的普通入站消息。
  () => {
    // messages 覆盖普通消息、系统通知、内部标记及出站消息。
    const messages: ChatMessage[] = [
      messageFixture,
      { ...messageFixture, id: 2, message_key: 'in-local', message_type: 'text' },
      { ...messageFixture, id: 3, message_key: 'system-1', message_type: 'system' },
      { ...messageFixture, id: 4, message_key: 'outgoing-1', direction: 'outgoing' },
    ];
    expect(collectChatReadReceipts(messages, 'c1')).toEqual([
      { messageId: 'm1', sessionId: 'c1', cid: 'c1@goofish', conversationType: 1 },
    ]);
  });

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
    // controller 请求取消控制器。
    const controller = new AbortController();
    expect(mergeLiveMessage([messageFixture], { ...messageFixture, content: '新消息' })[0].content).toBe('新消息');
    expect(isCurrentChatRequest(3, 3, controller.signal)).toBe(true);
    expect(isCurrentChatRequest(2, 3, controller.signal)).toBe(false);
    controller.abort();
    expect(isCurrentChatRequest(3, 3, controller.signal)).toBe(false);
  });

test('Chat 后续入站消息确认此前已发送出站消息为已读',
  // 已读推导测试验证实时状态与数据库事务中的会话级确认语义一致。
  () => {
    // outgoing 是尚未收到已读回执的本地出站消息。
    const outgoing: ChatMessage = { ...messageFixture, direction: 'outgoing', status: 'sent', read_status: 0, read_at: 0, sent_at: 10, message_key: 'outgoing-1' };
    // incoming 是买家随后发来的普通消息。
    const incoming: ChatMessage = { ...messageFixture, direction: 'incoming', sent_at: 20, message_key: 'incoming-1' };
    // updated 保存后续入站消息确认后的出站消息状态。
    const updated = markOutgoingMessagesReadByIncoming([outgoing], incoming);
    expect(updated[0]).toMatchObject({ read_status: 2, read_at: 20 });
  });

test('Chat 状态工具覆盖追加消息、搜索字段和时间格式化',
  // 边界场景测试验证新消息追加、不同搜索字段和取消错误识别。
  () => {
    expect(filterChatSessions(sessionFixture, 'B2', false)).toEqual([sessionFixture[1]]);
    expect(filterChatSessions(sessionFixture, '已发货', false)).toEqual([sessionFixture[1]]);
    expect(filterChatSessions(sessionFixture, '不存在', false)).toEqual([]);
    // incoming 是没有出现在当前列表中的实时消息。
    const incoming = { ...messageFixture, message_key: 'm2', content: '新消息' };
    expect(mergeLiveMessage([messageFixture], incoming)).toEqual([messageFixture, incoming]);
    expect(isChatAbortError(new Error('请求已取消'))).toBe(true);
    expect(isChatAbortError(new Error('网络失败'))).toBe(false);
    expect(isChatAbortError('请求已取消')).toBe(false);
    expect(formatClock(0)).toBe('');
    expect(formatClock(Date.now())).toMatch(/^\d{2}:\d{2}$/);
    expect(formatClock(Date.now() - 86_400_000)).toMatch(/\d{2}\/\d{2}/);
    expect(messageTime(Date.now())).toMatch(/\d{2}\/\d{2}/);
  });

test('Chat 未读徽标为单数字保留正圆尺寸，多数字才横向扩展',
  // 未读徽标测试验证账号页签和会话列表共享的形状边界。
  () => {
    expect(unreadBadgeLabel(1)).toBe('1');
    expect(unreadBadgeLabel(99)).toBe('99');
    expect(unreadBadgeLabel(100)).toBe('99+');
    expect(unreadBadgeClassName(1)).toContain('w-5');
    expect(unreadBadgeClassName(1)).not.toContain('min-w-5');
    expect(unreadBadgeClassName(10)).toContain('min-w-5');
    expect(unreadBadgeClassName(99)).toContain('min-w-5');
  });
