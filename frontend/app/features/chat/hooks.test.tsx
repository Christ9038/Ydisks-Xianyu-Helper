// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import type { AccountDetail, ChatMessage, ChatSession } from '../../../shared/api-contract';
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

// latestSocket 保存当前测试创建的 WebSocket 替身。
let latestSocket: {
  // close 是关闭 WebSocket 的替身方法。
  close: ReturnType<typeof vi.fn>;
  // onopen 是 WebSocket 建立连接回调。
  onopen: (() => void) | null;
  // onmessage 是 WebSocket 消息回调。
  onmessage: ((event: { /* data 是 WebSocket 事件数据。 */ data: string }) => void) | null;
  // onclose 是 WebSocket 断开回调。
  onclose: (() => void) | null;
  // onerror 是 WebSocket 错误回调。
  onerror: (() => void) | null;
} | null = null;

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
      // socket 是当前 WebSocket 连接的可控替身。
      const socket = { close: vi.fn(), onopen: null, onmessage: null, onclose: null, onerror: null };
      latestSocket = socket;
      return socket;
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
    expect(markReadMock).toHaveBeenCalledWith('account-1', 'chat-1', [
      { messageId: 'message-1', sessionId: 'chat-1', cid: 'chat-1@goofish', conversationType: 1 },
    ], expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.unreadForAccount('account-1')).toBe(0);
    await act(
      // emptySendAction 在没有草稿时阻止文字发送。
      async () => hook.result.current.handleSend(),
    );

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
    // imageInput 是图片发送成功后需要清空的输入框替身。
    const imageInput = { value: 'selected-file' } as HTMLInputElement;
    hook.result.current.imageInputRef.current = imageInput;
    await act(
      // secondImageAction 再次发送图片以验证输入框清理。
      async () => hook.result.current.handleImage(new File(['image'], 'second.png', { type: 'image/png' })),
    );
    expect(imageInput.value).toBe('');
    await act(
      // emptyImageAction 在没有图片文件时阻止发送。
      async () => hook.result.current.handleImage(),
    );
    // emptyScrollAction 在没有滚动容器时保持滚动策略稳定。
    hook.result.current.scrollRef.current = null;
    await act(
      // nullScrollAction 验证滚动容器缺失守卫。
      () => hook.result.current.handleMessageScroll(),
    );
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

  test('会话刷新不会让当前打开会话重新显示未读', /* 当前回调验证已读状态与慢刷新响应的竞态边界。 */ async () => {
    // hook 是当前会话已读后执行刷新的 Hook 渲染结果。
    const hook = renderHook(
      // refreshHookFactory 创建会话刷新竞态场景。
      () => useChat(),
    );
    await waitFor(
      // activeChatAssertion 等待默认会话完成消息读取。
      () => expect(hook.result.current.activeChatID).toBe('chat-1'),
    );
    await waitFor(
      // readAssertion 等待初始消息读取将本地未读数归零。
      () => expect(hook.result.current.unreadForAccount('account-1')).toBe(0),
    );
    // staleSessionPage 模拟服务端延迟返回的旧未读计数。
    const staleSessionPage = { ...sessionFixture, unread_count: 4 };
    getSessionPageMock.mockResolvedValueOnce({ sessions: [staleSessionPage], has_more: false });
    await act(
      // refreshAction 执行返回旧未读数据的会话刷新。
      async () => hook.result.current.reloadSessions('account-1'),
    );
    expect(hook.result.current.unreadForAccount('account-1')).toBe(0);
    hook.unmount();
  });

  test('WebSocket 连接和消息事件更新聊天状态', /* 当前回调验证实时连接和消息回调边界。 */ async () => {
    // secondSession 是用于覆盖实时会话排序比较器的第二条会话。
    const secondSession = { ...sessionFixture, chat_id: 'chat-2', last_message_at: 2, unread_count: 0 };
    getSessionPageMock.mockResolvedValue({ sessions: [sessionFixture, secondSession], has_more: true, next_cursor: 2 });
    // hook 是实时连接场景的聊天 Hook 渲染结果。
    const hook = renderHook(
      // socketHookFactory 创建实时连接场景的聊天 Hook。
      () => useChat(),
    );
    await waitFor(
      // loadingAssertion 等待聊天初始数据加载完成。
      () => expect(hook.result.current.loading).toBe(false),
    );
    await waitFor(
      // activeChatAssertion 等待实时消息目标会话选中。
      () => expect(hook.result.current.activeChatID).toBe('chat-1'),
    );
    expect(latestSocket).not.toBeNull();
    await act(
      // openAction 触发 WebSocket 建立连接事件。
      () => latestSocket?.onopen?.(),
    );
    expect(hook.result.current.liveState).toBe('online');
    // incomingMessage 是实时 WebSocket 推送的入站消息。
    const incomingMessage = { ...messageFixture, id: 3, message_key: 'message-3', sent_at: 3, content: '实时消息' };
    await act(
      // messageAction 触发合法的实时消息事件。
      () => latestSocket?.onmessage?.({ data: JSON.stringify({ message: incomingMessage }) }),
    );
    expect(hook.result.current.messages).toContainEqual(incomingMessage);
    expect(markReadMock).toHaveBeenCalledWith('account-1', 'chat-1', [
      { messageId: 'message-3', sessionId: 'chat-1', cid: 'chat-1@goofish', conversationType: 1 },
    ]);
    await act(
      // invalidMessageAction 触发非法消息帧并保持状态稳定。
      () => latestSocket?.onmessage?.({ data: '{broken' }),
    );
    await act(
      // closeAction 触发 WebSocket 断开事件。
      () => latestSocket?.onclose?.(),
    );
    expect(hook.result.current.liveState).toBe('offline');
    hook.unmount();
  });

  test('联系人、消息、历史分页和图片发送失败时保留可恢复错误', /* 当前回调验证聊天业务请求错误分支。 */ async () => {
    // hook 是聊天请求错误场景的 Hook 渲染结果。
    const hook = renderHook(
      // errorHookFactory 创建聊天请求错误场景的 Hook。
      () => useChat(),
    );
    await waitFor(
      // loadingAssertion 等待聊天初始数据加载完成。
      () => expect(hook.result.current.loading).toBe(false),
    );
    await waitFor(
      // activeChatAssertion 等待默认会话选中。
      () => expect(hook.result.current.activeChatID).toBe('chat-1'),
    );

    getSessionPageMock.mockRejectedValueOnce(new Error('联系人读取失败'));
    await act(
      // contactsErrorAction 触发联系人刷新错误。
      async () => hook.result.current.reloadSessions('account-1'),
    );
    expect(hook.result.current.error).toBe('联系人读取失败');

    getSessionPageMock.mockRejectedValueOnce(new Error('历史联系人失败'));
    await act(
      // moreContactsErrorAction 触发联系人分页错误。
      async () => hook.result.current.loadMoreContacts(),
    );
    expect(hook.result.current.error).toBe('历史联系人失败');

    // scrollContainer 是滚动策略测试使用的最小 DOM 容器。
    const scrollContainer = { scrollHeight: 100, scrollTop: 20, clientHeight: 50 } as HTMLDivElement;
    hook.result.current.scrollRef.current = scrollContainer;
    await act(
      // scrollAction 验证距离底部较远时不自动滚动。
      () => hook.result.current.handleMessageScroll(),
    );
    scrollContainer.scrollTop = 60;
    await act(
      // nearBottomScrollAction 验证接近底部时启用自动滚动。
      () => hook.result.current.handleMessageScroll(),
    );

    getMessagePageMock.mockRejectedValueOnce(new Error('消息读取失败'));
    await act(
      // chatSwitchAction 切换到不存在会话以触发消息读取错误。
      () => hook.result.current.setActiveChatID('chat-2'),
    );
    await waitFor(
      // messageErrorAssertion 等待消息读取错误收口。
      () => expect(hook.result.current.error).toBe('消息读取失败'),
    );

    await act(
      // chatRestoreAction 恢复默认会话以继续验证历史分页。
      () => hook.result.current.setActiveChatID('chat-1'),
    );
    await waitFor(
      // restoredMessageAssertion 等待默认会话消息恢复。
      () => expect(hook.result.current.messagesLoading).toBe(false),
    );
    getMessagePageMock.mockRejectedValueOnce(new Error('历史消息失败'));
    await act(
      // olderErrorAction 触发历史消息分页错误。
      async () => hook.result.current.loadOlderMessages(),
    );
    expect(hook.result.current.error).toBe('历史消息失败');

    sendImageMock.mockRejectedValueOnce(new Error('图片发送失败'));
    await act(
      // imageErrorAction 触发图片发送错误。
      async () => hook.result.current.handleImage(new File(['image'], 'error.png', { type: 'image/png' })),
    );
    expect(hook.result.current.error).toBe('图片发送失败');
    expect(hook.result.current.retryAvailable).toBe(true);
    sendImageMock.mockResolvedValueOnce({ message: sentMessageFixture });
    await act(
      // imageRetryAction 重试最近一次图片发送。
      async () => hook.result.current.retrySend(),
    );
    expect(sendImageMock).toHaveBeenCalledTimes(2);
    hook.unmount();
  });

  test('初始聊天数据加载失败时结束加载状态并保留错误', /* 当前回调验证聊天初始化失败的状态收口。 */ async () => {
    getDetailsMock.mockRejectedValueOnce(new Error('聊天初始化失败'));
    // hook 是初始化失败场景的聊天 Hook 渲染结果。
    const hook = renderHook(
      // failedLoadHookFactory 创建初始化失败场景的 Hook。
      () => useChat(),
    );
    await waitFor(
      // loadingAssertion 等待初始化失败后的加载状态收口。
      () => expect(hook.result.current.loading).toBe(false),
    );
    expect(hook.result.current.error).toBe('聊天初始化失败');
    hook.unmount();
  });

  test('运行状态轮询失败后仍继续调度下一次轮询', /* 当前回调验证运行状态轮询失败的容错与重试。 */ async () => {
    vi.useFakeTimers();
    try {
      // initialStatus 是初始化请求返回的运行状态。
      const initialStatus = { 'account-1': { state: 'online' as const, connected: true, failures: 0, updated_at: '2026-08-15T00:00:00Z' } };
      // refreshedStatus 是首次轮询返回的更新状态。
      const refreshedStatus = { 'account-1': { state: 'reconnecting' as const, connected: false, failures: 1, updated_at: '2026-08-15T00:00:01Z' } };
      getRuntimeMock.mockReset();
      getRuntimeMock.mockResolvedValueOnce(initialStatus);
      getRuntimeMock.mockResolvedValueOnce(refreshedStatus);
      getRuntimeMock.mockRejectedValueOnce(new Error('轮询失败'));
      // hook 是轮询失败场景的聊天 Hook 渲染结果。
      const hook = renderHook(
        // pollingHookFactory 创建轮询失败场景的 Hook。
        () => useChat(),
      );
      await act(
        // initialFlushAction 刷新初始化 Promise 和 React 状态更新。
        async () => { await Promise.resolve(); },
      );
      await act(
        // pollingTimerAction 推进首次轮询定时器。
        async () => { await vi.advanceTimersByTimeAsync(3_000); },
      );
      expect(hook.result.current.accounts[0]?.runtime_state).toBe('reconnecting');
      await act(
        // retryPollingTimerAction 推进失败后的下一次轮询定时器。
        async () => { await vi.advanceTimersByTimeAsync(3_000); },
      );
      expect(getRuntimeMock).toHaveBeenCalledTimes(3);
      hook.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  test('历史消息成功加载后调整滚动位置并处理未知实时会话', /* 当前回调验证历史分页滚动和实时未知会话刷新。 */ async () => {
    // hook 是历史分页成功场景的聊天 Hook 渲染结果。
    const hook = renderHook(
      // olderHookFactory 创建历史分页场景的 Hook。
      () => useChat(),
    );
    await waitFor(
      // activeChatAssertion 等待默认会话被选中。
      () => expect(hook.result.current.activeChatID).toBe('chat-1'),
    );
    await waitFor(
      // messagesAssertion 等待当前会话消息加载完成。
      () => expect(hook.result.current.messagesLoading).toBe(false),
    );
    // height 保存滚动容器当前高度，模拟历史消息插入后的高度变化。
    let height = 100;
    // container 是历史分页滚动位置测试使用的容器替身。
    const container = { clientHeight: 50, scrollTop: 0, get scrollHeight() { return height; } } as HTMLDivElement;
    hook.result.current.scrollRef.current = container;
    // olderMessage 是历史分页返回的更早消息。
    const olderMessage = { ...messageFixture, id: 0, message_key: 'message-0', sent_at: 0, content: '更早消息' };
    getMessagePageMock.mockResolvedValueOnce({ messages: [olderMessage], has_more: false, next_cursor: undefined });
    vi.stubGlobal('requestAnimationFrame', vi.fn(/* frameFactory 创建可控的滚动帧回调。 */ (callback: FrameRequestCallback) => {
      height = 180;
      callback(0);
      return 1;
    }));
    await act(
      // olderAction 请求更早的消息并恢复滚动位置。
      async () => hook.result.current.loadOlderMessages(),
    );
    expect(hook.result.current.messages).toEqual([olderMessage, messageFixture]);
    expect(container.scrollTop).toBe(80);
    await act(
      // noOlderAction 在没有更多历史消息时阻止重复分页请求。
      async () => hook.result.current.loadOlderMessages(),
    );
    expect(getMessagePageMock).toHaveBeenCalledTimes(2);

    // unknownMessage 是不在当前联系人列表中的实时消息。
    const unknownMessage = { ...messageFixture, chat_id: 'chat-unknown', message_key: 'message-unknown', content: '未知会话消息' };
    getSessionPageMock.mockResolvedValueOnce({ sessions: [sessionFixture], has_more: false, next_cursor: undefined });
    await act(
      // unknownMessageAction 触发未知会话的联系人刷新。
      () => latestSocket?.onmessage?.({ data: JSON.stringify({ message: unknownMessage }) }),
    );
    await waitFor(
      // reloadAssertion 等待未知会话触发联系人刷新。
      () => expect(getSessionPageMock).toHaveBeenCalledWith('account-1', undefined, expect.objectContaining({ signal: expect.any(AbortSignal) }), true),
    );
    await act(
      // emptyMessageAction 触发无消息载荷并保持状态稳定。
      () => latestSocket?.onmessage?.({ data: JSON.stringify({}) }),
    );
    await act(
      // errorAction 触发 WebSocket 错误并关闭连接。
      () => latestSocket?.onerror?.(),
    );
    expect(latestSocket?.close).toHaveBeenCalled();
    hook.unmount();
  });

  test('切换或清空会话时取消未完成的历史消息分页', /* 当前回调验证历史分页不会在会话上下文失效后写入新会话状态。 */ async () => {
    // historySignal 保存历史分页接口收到的取消信号。
    let historySignal: AbortSignal | undefined;
    // hook 是已加载默认账号和会话的聊天 Hook。
    const hook = renderHook(
      // chatHookFactory 创建聊天 Hook。
      () => useChat(),
    );
    await waitFor(
      // activeChatAssertion 等待默认会话成为当前选择。
      () => expect(hook.result.current.activeChatID).toBe('chat-1'),
    );
    await waitFor(
      // messagesLoadedAssertion 等待首次消息分页完成并允许继续加载历史消息。
      () => expect(hook.result.current.messagesLoading).toBe(false),
    );
    getMessagePageMock.mockImplementationOnce(
      // pendingHistory 保持历史分页未完成，以便验证会话切换时主动取消。
      (_accountID, _chatID, _cursor, _oldestID, requestOptions) => {
        historySignal = requestOptions?.signal;
        return new Promise(/* pendingHistoryExecutor 故意不完成历史分页 Promise，直到会话切换取消请求。 */ () => undefined);
      },
    );
    await act(
      // olderMessagesAction 触发需要保持未完成的历史消息分页。
      () => { void hook.result.current.loadOlderMessages(); },
    );
    await waitFor(
      // historyStartedAssertion 等待历史分页请求建立取消信号。
      () => expect(historySignal).toBeDefined(),
    );
    await act(
      // clearChatAction 清空当前会话，使历史分页上下文立即失效。
      () => hook.result.current.setActiveChatID(''),
    );
    expect(historySignal?.aborted).toBe(true);
    hook.unmount();
  });
});
