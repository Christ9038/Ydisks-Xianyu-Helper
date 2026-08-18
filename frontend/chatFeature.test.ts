import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, test } from 'vitest';

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

	test('frontend connects only to the application chat websocket', () => {
		const chatHook = source('app/features/chat/hooks.ts'); /* chatHook 表示chatHook。 */
		expect(chatHook).toContain('/api/v1/chat/ws');
		expect(chatHook).not.toContain('wss-goofish.dingtalk.com');
	} /* 测试回调断言前端只连接应用层聊天 WebSocket。 */);

	test('renders peer/self identity and verified media capabilities', () => {
		const chat = source('app/features/chat/pages/Chat.tsx'); /* chat 表示chat。 */
		const chatHook = source('app/features/chat/hooks.ts'); /* chatHook 表示chatHook。 */
		expect(chat).toContain('selectedSession.buyer_avatar_url');
		expect(chat).toContain('activeAccount?.avatar_url');
		expect(chat).toContain("message.message_type === 'image'");
		expect(chat).toContain("message.message_type === 'video'");
		expect(chatHook).toContain('sendChatImage');
	} /* 测试回调断言头像、媒体类型及发送 API 的渲染能力。 */);

	test('renders official notices as neutral system messages', () => {
		const chat = source('app/features/chat/pages/Chat.tsx'); /* chat 表示chat。 */
		expect(chat).toContain("message.message_type === 'system'");
		expect(chat).toContain('justify-center py-1');
	} /* 测试回调断言平台通知以居中的系统消息呈现。 */);

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
