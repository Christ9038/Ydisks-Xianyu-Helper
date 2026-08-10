import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, test } from 'vitest';

const source = (path: string) => readFileSync(resolve(__dirname, path), 'utf8');

describe('online chat UI contract', () => {
	test('uses account tabs above a two-column buyer/chat layout', () => {
		const chat = source('components/Chat.tsx');
		expect(chat).toContain('role="tablist"');
		expect(chat).toContain('grid-cols-[320px_minmax(0,1fr)]');
		expect(chat).toContain('min-h-0 min-w-0 flex-col overflow-hidden');
		expect(chat).not.toContain('grid-cols-[320px_minmax(0,1fr)_');
		expect(chat).toContain('activeAccountID');
		expect(chat).toContain('activeChatID');
	});

	test('uses neutral user labels instead of assuming buyer role', () => {
		const chat = source('components/Chat.tsx');
		expect(chat).toContain('用户 ID：');
		expect(chat).not.toContain('买家 ID：');
		expect(chat).not.toContain('选择一个买家');
	});

	test('frontend connects only to the application chat websocket', () => {
		const chat = source('components/Chat.tsx');
		expect(chat).toContain('/api/chat/ws');
		expect(chat).not.toContain('wss-goofish.dingtalk.com');
	});

	test('renders peer/self identity and verified media capabilities', () => {
		const chat = source('components/Chat.tsx');
		expect(chat).toContain('selectedSession.buyer_avatar_url');
		expect(chat).toContain('activeAccount?.avatar_url');
		expect(chat).toContain("message.message_type === 'image'");
		expect(chat).toContain("message.message_type === 'video'");
		expect(chat).toContain('sendChatImage');
	});

	test('renders official notices as neutral system messages', () => {
		const chat = source('components/Chat.tsx');
		expect(chat).toContain("message.message_type === 'system'");
		expect(chat).toContain('justify-center py-1');
	});

	test('keeps the active chat at the bottom when new messages arrive', () => {
		const chat = source('components/Chat.tsx');
		expect(chat).toContain('shouldScrollToBottomRef');
		expect(chat).toContain('skipNextMessageScrollRef');
		expect(chat).toContain('onScroll={handleMessageScroll}');
		expect(chat).toContain('container.scrollHeight - container.scrollTop - container.clientHeight');
		expect(chat).toContain('[activeAccountID, activeChatID, messages, messagesLoading]');
	});

	test('sidebar exposes collapse control and chat primary navigation', () => {
		const sidebar = source('components/Sidebar.tsx');
		expect(sidebar).toContain("id: 'chat'");
		expect(sidebar).toContain('onToggleCollapsed');
		expect(sidebar).toContain('collapsed ? \'w-16\' : \'w-64\'');
	});
});
