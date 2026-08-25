import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe,expect,test } from 'vitest';

const source = (path: string) => readFileSync(resolve(__dirname, path), 'utf8'); /* source 表示source。 */

describe('online chat UI contract', () => {
	test('uses account tabs above a two-column buyer/chat layout', () => {
		const chat = source('app/features/chat/pages/Chat.tsx'); /* chat 表示chat。 */
		expect(chat).toContain('role="tablist"');
		expect(chat).toContain('grid-cols-[320px_minmax(0,1fr)]');
		expect(chat).toContain('min-h-0 min-w-0 flex-col overflow-hidden');
		expect(chat).not.toContain('grid-cols-[320px_minmax(0,1fr)_');
		expect(chat).toContain('activeAccountID');
		expect(chat).toContain('activeChatID');
	} /* 测试回调断言聊天页的账号标签和双栏布局契约。 */);

	test('uses neutral user labels instead of assuming buyer role', () => {
		const chat = source('app/features/chat/pages/Chat.tsx'); /* chat 表示chat。 */
		expect(chat).toContain('用户 ID：');
		expect(chat).not.toContain('买家 ID：');
		expect(chat).not.toContain('选择一个买家');
	} /* 测试回调断言用户标签不误用买家角色。 */);

	test('应用壳拥有唯一应用层聊天 WebSocket，聊天页仅订阅其事件', () => {
		const chatHook = source('app/features/chat/hooks.ts'); /* chatHook 保存聊天页状态 Hook 源码。 */
		const titleNotification = source('app/features/chat/titleNotification.ts'); /* titleNotification 保存认证应用壳实时连接 owner 源码。 */
		expect(titleNotification).toContain('/api/v1/chat/ws');
		expect(titleNotification).not.toContain('wss-goofish.dingtalk.com');
		expect(chatHook).toContain('subscribeToChatLiveEvents');
		expect(chatHook).not.toContain('new WebSocket(');
	} /* 测试回调断言前端唯一连接归属应用壳，Chat 页面不会重复建连。 */);

	test('renders peer/self identity and verified media capabilities', () => {
		const chat = source('app/features/chat/pages/Chat.tsx'); /* chat 表示chat。 */
		const chatHook = source('app/features/chat/hooks.ts'); /* chatHook 表示chatHook。 */
		expect(chat).toContain('selectedSession.buyer_avatar_url');
		expect(chat).toContain('activeAccount?.avatar_url');
		expect(chat).toContain("message.message_type === 'image'");
		expect(chat).toContain("message.message_type === 'video'");
		expect(chat).toContain("message.message_type === 'audio'");
		expect(chat).toContain('initialDuration={message.media_duration}');
		expect(chat).toContain('session.item_image_url');
		expect(chat).toContain('rounded-[4px]');
		expect(chat).toContain('<AudioMessage');
		expect(chatHook).toContain('sendChatImage');
	} /* 测试回调断言头像、图片、视频、语音及发送 API 的渲染能力。 */);

	test('renders official notices as neutral system messages', () => {
		const chat = source('app/features/chat/pages/Chat.tsx'); /* chat 表示chat。 */
		expect(chat).toContain("message.message_type === 'system'");
		expect(chat).toContain('justify-center py-1');
		expect(chat).toContain('whitespace-pre-wrap break-words rounded-xl border border-slate-200');
	} /* 测试回调断言平台通知以居中的系统消息呈现。 */);

	test('preserves line breaks and wraps long chat text', () => {
		const chat = source('app/features/chat/pages/Chat.tsx'); /* chat 表示聊天页源码，用于验证消息文本的换行样式不会回退。 */
		expect(chat).toContain('whitespace-pre-wrap break-words rounded-2xl px-4 py-2.5');
	} /* 测试回调断言普通消息保留服务端换行并避免超长文本撑破气泡。 */);

	test('keeps the active chat at the bottom when new messages arrive', () => {
		const chat = source('app/features/chat/pages/Chat.tsx'); /* chat 表示chat。 */
		const chatHook = source('app/features/chat/hooks.ts'); /* chatHook 表示chatHook。 */
		expect(chatHook).toContain('shouldScrollToBottomRef');
		expect(chatHook).toContain('skipNextMessageScrollRef');
		expect(chat).toContain('onScroll={handleMessageScroll}');
		expect(chatHook).toContain('container.scrollHeight - container.scrollTop - container.clientHeight');
		expect(chatHook).toContain('[activeAccountID, activeChatID, messages, messagesLoading]');
	} /* 测试回调断言新消息到达时当前会话保持滚动到底部。 */);

	test('sidebar exposes collapse control and chat primary navigation', () => {
		const sidebar = source('shared/ui/Sidebar.tsx'); /* sidebar 表示sidebar。 */
		expect(sidebar).toContain("id: 'chat'");
		expect(sidebar).toContain('onToggleCollapsed');
		expect(sidebar).toContain('collapsed ? \'w-16\' : \'w-64\'');
	} /* 测试回调断言侧边栏折叠控制和聊天主导航入口。 */);
} /* 测试套件回调汇总聊天页面结构契约。 */);
