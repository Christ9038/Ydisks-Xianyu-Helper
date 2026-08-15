// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import type { AccountDetail, ChatMessage, ChatSession } from '../../../types';
import { getAccountDetails, getAccountRuntimeStatuses, getChatMessagePage, getChatSessionPage, markChatRead, sendChatImage, sendChatMessage } from './api';
import { useChat } from './hooks';

vi.mock('./api', /* chatApiMockFactory 提供聊天 Hook 的确定性 API 替身。 */ () => ({
  getAccountDetails: vi.fn(),
  getAccountRuntimeStatuses: vi.fn(),
  getChatMessagePage: vi.fn(),
  getChatSessionPage: vi.fn(),
  markChatRead: vi.fn(),
  sendChatImage: vi.fn(),
  sendChatMessage: vi.fn(),
}));

// getDetailsMock 是聊天账号详情请求的可控替身。
const getDetailsMock = vi.mocked(getAccountDetails);
// getRuntimeMock 是聊天账号运行状态请求的可控替身。
const getRuntimeMock = vi.mocked(getAccountRuntimeStatuses);
// getMessagePageMock 是聊天消息分页请求的可控替身。
const getMessagePageMock = vi.mocked(getChatMessagePage);
// getSessionPageMock 是聊天会话分页请求的可控替身。
const getSessionPageMock = vi.mocked(getChatSessionPage);
// markReadMock 是聊天已读请求的可控替身。
const markReadMock = vi.mocked(markChatRead);
// sendImageMock 是聊天图片发送请求的可控替身。
const sendImageMock = vi.mocked(sendChatImage);
// sendMessageMock 是聊天文字发送请求的可控替身。
const sendMessageMock = vi.mocked(sendChatMessage);

// accountFixture 是聊天 Hook 使用的启用账号对象。
const accountFixture: AccountDetail = { id: 'account-1', enabled: true, auto_confirm: false, nickname: '测试账号' };
// sessionFixture 是聊天会话列表中的当前会话。
const sessionFixture: ChatSession = { account_id: 'account-1', chat_id: 'chat-1', buyer_id: 'buyer-1', buyer_name: '买家', item_title: '商品', last_message: '你好', last_message_at: 1, unread_count: 1 };
// messageFixture 是当前会话中的历史消息。
const messageFixture = { id: 1, account_id: 'account-1', chat_id: 'chat-1', message_key: 'message-1', direction: 'incoming', sender_id: 'buyer-1', sender_name: '买家', message_type: 'text', content: '你好', status: 'received', sent_at: 1 } as never as ChatMessage;
// sentMessageFixture 是文字发送成功后返回的消息。
const sentMessageFixture = { ...messageFixture, id: 2, message_key: 'message-2', direction: 'outgoing', content: '回复内容' } as ChatMessage;

describe('useChat', /* 当前回调处理聊天加载、分页、发送和实时连接状态。 */ () => {
  beforeEach(/* 当前回调重置聊天 API 替身和 WebSocket。 */ () => {
    vi.clearAllMocks();
    getDetailsMock.mockResolvedValue([accountFixture]);
    getRuntimeMock.mockResolvedValue({ 'account-1': { state: 'online', connected: true, failures: 0, updated_at: '2026-08-15T00:00:00Z' } });
    getSessionPageMock.mockResolvedValue({ sessions: [sessionFixture], has_more: true, next_cursor: 2 });
    getMessagePageMock.mockResolvedValue({ messages: [messageFixture], has_more: true, next_cursor: 2, session: sessionFixture });
    markReadMock.mockResolvedValue({ success: true });
    sendMessageMock.mockResolvedValue({ message: sentMessageFixture });
    sendImageMock.mockResolvedValue({ message: sentMessageFixture });
    // websocketFactory 是不连接真实服务器的 WebSocket 构造替身。
    const websocketFactory = vi.fn(
      // websocketConstructor 创建不连接真实服务器的 WebSocket 实例。
      function websocketConstructor() {
      return { close: vi.fn(), onopen: null, onmessage: null, onclose: null, onerror: null };
      },
    );
    vi.stubGlobal('WebSocket', websocketFactory);
    // localStorageStub 是聊天 Hook 记忆账号选择所需的浏览器存储替身。
    Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: vi.fn().mockReturnValue(''), setItem: vi.fn(), removeItem: vi.fn() } });
  });

  test('加载账号、会话和消息后可以发送文字与图片', /* 当前回调验证聊天 Hook 成功加载和发送路径。 */ async () => {
    // hook 是聊天 Hook 的渲染结果。
    const hook = renderHook(
      // chatHookFactory 创建聊天 Hook。
      () => useChat(),
    );
    await waitFor(
      // loadingAssertion 等待账号和会话加载完成。
      () => expect(hook.result.current.loading).toBe(false),
    );
    await waitFor(
      // activeChatAssertion 等待默认会话被选中。
      () => expect(hook.result.current.activeChatID).toBe('chat-1'),
    );
    await waitFor(
      // messagesAssertion 等待当前会话消息加载完成。
      () => expect(hook.result.current.messagesLoading).toBe(false),
    );
    expect(hook.result.current.accounts[0]).toMatchObject(accountFixture);
    expect(hook.result.current.activeChatID).toBe('chat-1');
    expect(hook.result.current.messages).toEqual([messageFixture]);
    expect(markReadMock).toHaveBeenCalledWith('account-1', 'chat-1', expect.objectContaining({ signal: expect.any(AbortSignal) }));

    await act(
      // draftAction 写入文字消息草稿。
      () => hook.result.current.setDraft('回复内容'),
    );
    await act(
      // sendAction 提交文字消息。
      async () => hook.result.current.handleSend(),
    );
    expect(sendMessageMock).toHaveBeenCalledWith(expect.objectContaining({ text: '回复内容', chat_id: 'chat-1' }), expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.messages).toContainEqual(sentMessageFixture);

    await act(
      // imageAction 提交图片消息。
      async () => hook.result.current.handleImage(new File(['image'], 'image.png', { type: 'image/png' })),
    );
    expect(sendImageMock).toHaveBeenCalledWith(expect.objectContaining({ chat_id: 'chat-1', image: expect.any(File) }), expect.objectContaining({ signal: expect.any(AbortSignal) }));
    hook.unmount();
  });

  test('联系人分页和发送失败都提供可重试状态', /* 当前回调验证聊天分页和错误重试路径。 */ async () => {
    // hook 是聊天失败场景的 Hook 渲染结果。
    const hook = renderHook(
      // failedChatHookFactory 创建聊天错误场景的 Hook。
      () => useChat(),
    );
    await waitFor(
      // loadingAssertion 等待错误场景的账号加载完成。
      () => expect(hook.result.current.loading).toBe(false),
    );
    await waitFor(
      // activeChatAssertion 等待错误场景的默认会话被选中。
      () => expect(hook.result.current.activeChatID).toBe('chat-1'),
    );
    await waitFor(
      // contactsAssertion 等待联系人分页标记生效。
      () => expect(hook.result.current.hasMoreContacts).toBe(true),
    );
    await waitFor(
      // messagesAssertion 等待错误场景的消息加载完成。
      () => expect(hook.result.current.messagesLoading).toBe(false),
    );
    await act(
      // contactsAction 请求更早的联系人。
      async () => hook.result.current.loadMoreContacts(),
    );
    expect(getSessionPageMock).toHaveBeenCalledWith('account-1', undefined, expect.objectContaining({ signal: expect.any(AbortSignal) }), true);

    sendMessageMock.mockRejectedValueOnce(new Error('发送失败'));
    await act(
      // draftAction 写入会失败的文字草稿。
      () => hook.result.current.setDraft('失败消息'),
    );
    await act(
      // failedSendAction 提交会失败的文字消息。
      async () => hook.result.current.handleSend(),
    );
    expect(hook.result.current.error).toBe('发送失败');
    expect(hook.result.current.retryAvailable).toBe(true);
    sendMessageMock.mockResolvedValueOnce({ message: sentMessageFixture });
    await act(
      // retryAction 重试最近一次失败的文字消息。
      async () => hook.result.current.retrySend(),
    );
    expect(sendMessageMock).toHaveBeenCalledTimes(2);
    hook.unmount();
  });
});
