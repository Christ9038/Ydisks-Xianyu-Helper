// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import Chat from './Chat';
import { useChat } from '../app/features/chat/hooks';

vi.mock('../app/features/chat/hooks', /* chatHookMockFactory 提供聊天页面的可控状态。 */ () => ({ useChat: vi.fn() }));

// useChatMock 是聊天页面 Hook 的可控替身。
const useChatMock = vi.mocked(useChat);
// baseChatState 是覆盖聊天页面所有必需字段的最小状态。
const baseChatState = {
  accounts: [], activeAccountID: '', activeChatID: '', activeAccount: null, selectedSession: null, filteredSessions: [], messages: [], search: '', unreadOnly: false, draft: '', loading: false, messagesLoading: false, olderLoading: false, hasOlder: false, contactsLoading: false, hasMoreContacts: false, emojiOpen: false, sending: false, error: '', liveState: 'offline', scrollRef: { current: null }, imageInputRef: { current: null }, setActiveAccountID: vi.fn(), setActiveChatID: vi.fn(), setSearch: vi.fn(), setUnreadOnly: vi.fn(), setDraft: vi.fn(), setEmojiOpen: vi.fn(), reloadSessions: vi.fn(), loadMoreContacts: vi.fn(), loadOlderMessages: vi.fn(), handleMessageScroll: vi.fn(), handleSend: vi.fn(), handleImage: vi.fn(), retrySend: vi.fn(), retryAvailable: false, unreadForAccount: vi.fn().mockReturnValue(0), emojiURL: vi.fn(), xianyuEmojis: [], renderXianyuText: vi.fn(), formatClock: vi.fn().mockReturnValue('刚刚'), messageTime: vi.fn().mockReturnValue('刚刚'),
};

describe('Chat 页面加载边界', /* 当前回调覆盖聊天页面加载和账号空状态。 */ () => {
  test('加载中展示旋转指示器', /* 当前回调验证聊天页面初始加载分支。 */ () => {
    useChatMock.mockReturnValue({ ...baseChatState, loading: true } as never);
    render(<Chat />);
    expect(document.querySelector('.animate-spin')).toBeTruthy();
  });

  test('没有启用账号时展示引导并可切换离线状态', /* 当前回调验证账号空列表和实时连接状态。 */ () => {
    useChatMock.mockReturnValue({ ...baseChatState, liveState: 'offline' } as never);
    render(<Chat />);
    expect(screen.getByText('暂无启用账号')).toBeTruthy();
    expect(screen.getByText('连接已断开')).toBeTruthy();
  });
});
