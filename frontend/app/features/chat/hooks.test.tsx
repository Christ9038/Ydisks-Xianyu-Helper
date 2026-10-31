// @vitest-environment jsdom
import { act,renderHook,waitFor } from '@testing-library/react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import type { AccountDetail,ChatMessage,ChatSession } from './api';
import { confirmedOutgoingMessageFromError,deleteChatSession,getAccountDetails,getAccountRuntimeStatuses,getChatMessagePage,getChatSessionPage,markChatRead,sendChatImage,sendChatLibraryImage,sendChatMessage,uncertainOutgoingMessageFromError } from './api';
import { useChat } from './hooks';
import { publishChatConnectionState,publishChatLiveMessage } from './liveEvents';

vi.mock('./api', /* chatApiMockFactory 提供聊天 Hook 的确定性 API 替身。 */ () => ({
  getAccountDetails: vi.fn(),
  getAccountRuntimeStatuses: vi.fn(),
  getChatMessagePage: vi.fn(),
  getChatSessionPage: vi.fn(),
	deleteChatSession: vi.fn(),
  markChatRead: vi.fn(),
  sendChatImage: vi.fn(),
  sendChatLibraryImage: vi.fn(),
  sendChatMessage: vi.fn(),
	confirmedOutgoingMessageFromError: vi.fn(),
	uncertainOutgoingMessageFromError: vi.fn(),
}));

// getDetailsMock 是聊天账号详情请求的可控替身。
const getDetailsMock = vi.mocked(getAccountDetails);
// getRuntimeMock 是聊天账号运行状态请求的可控替身。
const getRuntimeMock = vi.mocked(getAccountRuntimeStatuses);
// getMessagePageMock 是聊天消息分页请求的可控替身。
const getMessagePageMock = vi.mocked(getChatMessagePage);
// getSessionPageMock 是聊天会话分页请求的可控替身。
const getSessionPageMock = vi.mocked(getChatSessionPage);
// deleteSessionMock 是本地会话删除请求的可控替身。
const deleteSessionMock = vi.mocked(deleteChatSession);
// markReadMock 是聊天已读请求的可控替身。
const markReadMock = vi.mocked(markChatRead);
// sendImageMock 是聊天图片发送请求的可控替身。
const sendImageMock = vi.mocked(sendChatImage);
// sendLibraryImageMock 是聊天图库图片发送请求的可控替身。
const sendLibraryImageMock = vi.mocked(sendChatLibraryImage);
// sendMessageMock 是聊天文字发送请求的可控替身。
const sendMessageMock = vi.mocked(sendChatMessage);
// confirmedOutgoingMock 是远端已发送状态收口失败错误的可控适配器替身。
const confirmedOutgoingMock = vi.mocked(confirmedOutgoingMessageFromError);
// uncertainOutgoingMock 是未知发送结果状态的可控适配器替身。
const uncertainOutgoingMock = vi.mocked(uncertainOutgoingMessageFromError);

// accountFixture 是聊天 Hook 使用的启用账号对象。
const accountFixture: AccountDetail = { id: 'account-1', enabled: true, auto_confirm: false, nickname: '测试账号' };
// sessionFixture 是聊天会话列表中的当前会话。
const sessionFixture: ChatSession = { account_id: 'account-1', chat_id: 'chat-1', peer_user_id: 'buyer-1', peer_name: '买家', item_title: '商品', last_message: '你好', last_message_at: 1, unread_count: 1 };
// messageFixture 是当前会话中的历史消息。
const messageFixture = { id: 1, account_id: 'account-1', chat_id: 'chat-1', message_key: 'message-1', direction: 'incoming', sender_id: 'buyer-1', sender_name: '买家', message_type: 'text', content: '你好', status: 'received', sent_at: 1 } as never as ChatMessage;
// sentMessageFixture 是文字发送成功后返回的消息。
const sentMessageFixture = { ...messageFixture, id: 2, message_key: 'message-2', direction: 'outgoing', content: '回复内容' } as ChatMessage;

/** deferredChatReview 让测试精确控制 Promise 完成顺序，验证忽略取消信号的迟到响应。 */
const deferredChatReview = <T,>(): { /** promise 是待完成请求。 */ promise: Promise<T>; /** resolve 使用 value 完成请求。 */ resolve: (value: T) => void } => {
  // resolve 保存由测试拥有的完成回调。
  let resolve!: (value: T) => void;
  // promise 由测试显式完成，不使用真实网络或计时等待。
  const promise = new Promise<T>(/* complete 是 Promise 的完成函数。 */ complete => { resolve = complete; });
  return { promise, resolve };
};

describe('useChat', /* 当前回调处理聊天加载、分页、发送和实时连接状态。 */ () => {
  beforeEach(/* 当前回调重置聊天 API 替身和全局实时连接状态。 */ () => {
    vi.clearAllMocks();
    getDetailsMock.mockResolvedValue([accountFixture]);
    getRuntimeMock.mockResolvedValue({ 'account-1': { state: 'online', connected: true, failures: 0, updated_at: '2026-08-15T00:00:00Z' } });
    getSessionPageMock.mockResolvedValue({ sessions: [sessionFixture], has_more: true, next_cursor: 2 });
    getMessagePageMock.mockResolvedValue({ messages: [messageFixture], has_more: true, next_cursor: 2, session: sessionFixture });
		deleteSessionMock.mockResolvedValue({ success: true });
    markReadMock.mockResolvedValue({ success: true });
    sendMessageMock.mockResolvedValue({ message: sentMessageFixture });
    sendImageMock.mockResolvedValue({ message: sentMessageFixture });
    sendLibraryImageMock.mockResolvedValue({ message: sentMessageFixture });
	confirmedOutgoingMock.mockReturnValue(undefined);
	uncertainOutgoingMock.mockReturnValue(undefined);
    publishChatConnectionState('connecting');
    // localStorageStub 是聊天 Hook 记忆账号选择所需的浏览器存储替身。
    Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: vi.fn().mockReturnValue(''), setItem: vi.fn(), removeItem: vi.fn() } });
    // createObjectURLMock 和 revokeObjectURLMock 模拟浏览器图片预览地址的创建与释放。
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn(/* 当前回调为测试图片生成稳定的临时地址。 */ (file: File) => `blob:${file.name}`) });
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() });
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
      // retainedDraftAction 写入一段不应被快捷回复发送清空的普通消息草稿。
      () => hook.result.current.setDraft('仍在编辑的草稿'),
    );
    await act(
      // quickReplyAction 发送账号快捷回复，复用可靠发送但保留普通草稿。
      async () => hook.result.current.handleQuickReply('快捷回复内容'),
    );
    expect(sendMessageMock).toHaveBeenLastCalledWith(expect.objectContaining({ text: '快捷回复内容', chat_id: 'chat-1' }), expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.draft).toBe('仍在编辑的草稿');

    await act(
      // imageAction 提交图片消息。
      async () => hook.result.current.handleImage(new File(['image'], 'image.png', { type: 'image/png' })),
    );
    expect(hook.result.current.pendingImage?.url).toBe('blob:image.png');
    await act(
      // confirmImageAction 确认预览后提交图片消息。
      async () => hook.result.current.confirmSendImage(),
    );
    expect(sendImageMock).toHaveBeenCalledWith(expect.objectContaining({ chat_id: 'chat-1', image: expect.any(File) }), expect.objectContaining({ signal: expect.any(AbortSignal) }));
    // imageInput 是图片发送成功后需要清空的输入框替身。
    const imageInput = { value: 'selected-file' } as HTMLInputElement;
    hook.result.current.imageInputRef.current = imageInput;
    await act(
      // secondImageAction 再次发送图片以验证输入框清理。
      async () => hook.result.current.handleImage(new File(['image'], 'second.png', { type: 'image/png' })),
    );
    await act(
      // confirmSecondImageAction 确认第二张图片并验证发送后的输入清理。
      async () => hook.result.current.confirmSendImage(),
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

	test('独立发送器只允许把结果合入当前账号和会话', /* 当前回调验证旧商品请求不能污染切换后的消息列表。 */ async () => {
		// secondSession 是切换后成为当前上下文的另一条会话。
		const secondSession: ChatSession = { ...sessionFixture, chat_id: 'chat-2', peer_user_id: 'buyer-2', peer_name: '买家二', last_message: '第二会话' };
		getSessionPageMock.mockResolvedValue({ sessions: [sessionFixture, secondSession], has_more: false });
		// hook 是验证独立发送结果归属门禁的聊天状态。
		const hook = renderHook(/* chatHookFactory 创建包含两个会话的聊天 Hook。 */ () => useChat());
		await waitFor(/* activeAssertion 等待默认会话选中。 */ () => expect(hook.result.current.activeChatID).toBe('chat-1'));
		await act(/* switchAction 切换到第二条会话。 */ () => hook.result.current.setActiveChatID('chat-2'));
		await waitFor(/* switchedAssertion 等待最新会话引用同步。 */ () => expect(hook.result.current.activeChatID).toBe('chat-2'));
		// staleMessage 模拟第一条会话商品弹窗在切换后返回的迟到结果。
		const staleMessage: ChatMessage = { ...sentMessageFixture, chat_id: 'chat-1', message_key: 'stale-item', message_type: 'item' };
		await act(/* staleResultAction 尝试合并旧会话发送结果。 */ () => hook.result.current.acceptOutgoingMessage(staleMessage));
		expect(hook.result.current.messages.some(/* message 是当前列表中待检查的消息。 */ message => message.message_key === 'stale-item')).toBe(false);
		// currentMessage 是与当前会话一致的合法独立发送结果。
		const currentMessage: ChatMessage = { ...staleMessage, chat_id: 'chat-2', message_key: 'current-item' };
		await act(/* currentResultAction 合并当前会话发送结果。 */ () => hook.result.current.acceptOutgoingMessage(currentMessage));
		expect(hook.result.current.messages).toContainEqual(currentMessage);
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

  test('应用壳唯一实时连接发布的状态和消息会更新聊天状态', /* 当前回调验证 Chat 页面不再自行创建 WebSocket。 */ async () => {
    // secondSession 是用于覆盖实时会话排序比较器的第二条会话。
    const secondSession = { ...sessionFixture, chat_id: 'chat-2', last_message_at: 2, unread_count: 0 };
    getSessionPageMock.mockResolvedValue({ sessions: [sessionFixture, secondSession], has_more: true, next_cursor: 2 });
    // outgoingFixture 是当前会话中等待后续买家消息确认的出站消息。
    const outgoingFixture = { ...messageFixture, id: 2, message_key: 'outgoing-1', direction: 'outgoing' as const, status: 'sent' as const, read_status: 0, read_at: 0, sent_at: 2, content: '我发出的消息' };
    getMessagePageMock.mockResolvedValue({ messages: [outgoingFixture, messageFixture], has_more: true, next_cursor: 2, session: sessionFixture });
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
    await act(
      // onlineAction 触发应用壳全局连接建立成功事件。
      () => publishChatConnectionState('online'),
    );
    expect(hook.result.current.liveState).toBe('online');
    await waitFor(
      // initialPlatformRefreshAssertion 等待账号首次选中允许的平台联系人刷新完成。
      () => expect(getSessionPageMock.mock.calls.some(/* call 是用于识别 refresh=true 的会话请求参数。 */ call => call[3] === true)).toBe(true),
    );
    // platformRefreshCountBeforeMessage 记录已知会话收到消息前的平台刷新次数。
    const platformRefreshCountBeforeMessage = getSessionPageMock.mock.calls.filter(/* call 是参与平台刷新计数的会话请求参数。 */ call => call[3] === true).length;
    // incomingMessage 是实时 WebSocket 推送的入站消息。
    const incomingMessage = { ...messageFixture, id: 3, message_key: 'message-3', sent_at: 3, content: '实时消息' };
    await act(
      // messageAction 触发应用壳发布的合法实时消息事件。
      () => publishChatLiveMessage(incomingMessage),
    );
    expect(hook.result.current.messages).toContainEqual(incomingMessage);
    expect(hook.result.current.messages.find(/* 当前回调定位需要验证已读状态的出站消息。 */ message => message.message_key === 'outgoing-1')).toMatchObject({ read_status: 2, read_at: 3 });
    expect(markReadMock).toHaveBeenCalledWith('account-1', 'chat-1', [
      { messageId: 'message-3', sessionId: 'chat-1', cid: 'chat-1@goofish', conversationType: 1 },
    ]);
    expect(getSessionPageMock.mock.calls.filter(/* call 是实时消息处理后参与平台刷新计数的会话请求参数。 */ call => call[3] === true)).toHaveLength(platformRefreshCountBeforeMessage);
    await act(
      // closeAction 触发应用壳全局连接断开事件。
      () => publishChatConnectionState('offline'),
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
    await act(
      // confirmImageErrorAction 确认预览并进入图片发送错误分支。
      async () => hook.result.current.confirmSendImage(),
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

  test('图片库图片经预览确认发送，快捷回复图片直接发送，失败后可原样重试', /* 当前回调验证图库图片复用既有发送、预览与重试流程。 */ async () => {
    // hook 是聊天 Hook 的渲染结果。
    const hook = renderHook(
      // chatHookFactory 创建聊天 Hook。
      () => useChat(),
    );
    await waitFor(
      // readyAssertion 等待会话与消息加载完成。
      () => expect(hook.result.current.messagesLoading).toBe(false),
    );
    await waitFor(
      // activeChatAssertion 等待默认会话被选中。
      () => expect(hook.result.current.activeChatID).toBe('chat-1'),
    );
    await act(
      // pickAction 选择图片库图片，只进入预览，不触发任何发送请求。
      async () => hook.result.current.handleLibraryImage({ id: 7, filename: 'a.png' }),
    );
    expect(hook.result.current.pendingImage).toMatchObject({ libraryImage: { id: 7 }, url: '/api/v1/library/images/7/content' });
    expect(sendLibraryImageMock).not.toHaveBeenCalled();
    await act(
      // confirmAction 确认预览后提交图库图片。
      async () => hook.result.current.confirmSendImage(),
    );
    expect(sendLibraryImageMock).toHaveBeenCalledWith(expect.objectContaining({ account_id: 'account-1', chat_id: 'chat-1', peer_user_id: 'buyer-1', library_image_id: 7 }), expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(sendImageMock).not.toHaveBeenCalled();
    expect(hook.result.current.pendingImage).toBeNull();

    sendLibraryImageMock.mockRejectedValueOnce(new Error('图库图片发送失败'));
    await act(
      // quickImageAction 直接发送图片快捷回复引用的图片，失败时记录重试来源。
      async () => hook.result.current.handleQuickReplyImage(9),
    );
    expect(hook.result.current.error).toBe('图库图片发送失败');
    expect(hook.result.current.retryAvailable).toBe(true);
    await act(
      // retryAction 重试时仍使用同一张图库图片，而不是文件接口。
      async () => hook.result.current.retrySend(),
    );
    expect(sendLibraryImageMock).toHaveBeenLastCalledWith(expect.objectContaining({ library_image_id: 9 }), expect.anything());
    expect(sendImageMock).not.toHaveBeenCalled();
    expect(hook.result.current.retryAvailable).toBe(false);

    await act(
      // invalidAction 非法主键不会触发任何请求。
      async () => hook.result.current.handleQuickReplyImage(0),
    );
    expect(sendLibraryImageMock).toHaveBeenCalledTimes(3);
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

  test('图片预览取消和卸载都会释放临时对象地址', /* 当前回调验证图片预览资源不会跨会话泄漏。 */ async () => {
    // hook 是图片预览资源生命周期测试使用的聊天 Hook。
    const hook = renderHook(
      // chatHookFactory 创建图片预览测试 Hook。
      () => useChat(),
    );
    await waitFor(
      // loadingAssertion 等待图片预览测试完成初始加载。
      () => expect(hook.result.current.activeChatID).toBe('chat-1'),
    );
    await act(
      // previewAction 创建第一张图片预览。
      async () => hook.result.current.handleImage(new File(['image'], 'cancel.png', { type: 'image/png' })),
    );
    await act(
      // closeAction 取消预览并释放第一张图片地址。
      () => hook.result.current.closeImagePreview(),
    );
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:cancel.png');
    await act(
      // secondPreviewAction 通过剪贴板入口创建第二张图片预览。
      async () => hook.result.current.handlePastedImages([new File(['image'], 'unmount.png', { type: 'image/png' })]),
    );
    hook.unmount();
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:unmount.png');
  });

  test('切换会话会使旧图片预览失效', /* 当前回调验证旧会话图片不能误发到新会话。 */ async () => {
    // hook 是会话切换预览隔离测试使用的聊天 Hook。
    const hook = renderHook(
      // chatHookFactory 创建会话切换测试 Hook。
      () => useChat(),
    );
    await waitFor(
      // chatAssertion 等待默认会话准备完成。
      () => expect(hook.result.current.activeChatID).toBe('chat-1'),
    );
    await act(
      // previewAction 在原会话创建图片预览。
      async () => hook.result.current.handleImage(new File(['image'], 'switch.png', { type: 'image/png' })),
    );
    await act(
      // switchAction 切换到另一个会话，使原预览不可再发送。
      () => hook.result.current.setActiveChatID('chat-2'),
    );
    await waitFor(
      // previewAssertion 等待旧会话预览被清理。
      () => expect(hook.result.current.pendingImage).toBeNull(),
    );
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:switch.png');
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
    // unknownSession 是刷新接口返回的新会话，必须自动出现在联系人列表中。
    const unknownSession = { ...sessionFixture, chat_id: 'chat-unknown', peer_user_id: 'buyer-unknown', last_message: '未知会话消息', last_message_at: 9 };
    getSessionPageMock.mockResolvedValueOnce({ sessions: [unknownSession, sessionFixture], has_more: false, next_cursor: undefined });
    await act(
      // unknownMessageAction 触发未知会话的联系人刷新。
      () => publishChatLiveMessage(unknownMessage),
    );
    await waitFor(
      // reloadAssertion 等待未知会话触发联系人刷新。
      () => expect(getSessionPageMock).toHaveBeenCalledWith('account-1', undefined, expect.objectContaining({ signal: expect.any(AbortSignal) }), true),
    );
    await waitFor(
      // unknownSessionAssertion 等待实时事件关联的新会话写入联系人列表。
      () => expect(hook.result.current.activeSessions).toContainEqual(unknownSession),
    );
    await act(
      // offlineAction 触发全局连接断开状态并保持消息列表稳定。
      () => publishChatConnectionState('offline'),
    );
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

	test('删除非当前会话保持聊天上下文，删除当前会话清空消息并选择剩余会话', /* 当前回调验证会话删除成功后的局部状态收口。 */ async () => {
		// secondSession 是用于先验证非当前会话删除的联系人。
		const secondSession = { ...sessionFixture, chat_id: 'chat-2', peer_user_id: 'buyer-2', peer_name: '第二位买家', unread_count: 3 };
		getSessionPageMock.mockResolvedValue({ sessions: [sessionFixture, secondSession], has_more: false });
		// hook 是会话删除成功场景的聊天 Hook。
		const hook = renderHook(
			// deletionHookFactory 创建包含两条会话的聊天 Hook。
			() => useChat(),
		);
		await waitFor(
			// activeChatAssertion 等待默认会话完成选择和消息加载。
			() => expect(hook.result.current.activeChatID).toBe('chat-1'),
		);
		getSessionPageMock.mockResolvedValueOnce({ sessions: [sessionFixture], has_more: false, next_cursor: undefined });
		await act(
			// deleteInactiveAction 删除非当前会话，不应清空正在查看的聊天。
			async () => { expect(await hook.result.current.deleteConversation('account-1', 'chat-2')).toBe(true); },
		);
		expect(hook.result.current.activeChatID).toBe('chat-1');
		expect(hook.result.current.activeSessions.map(/* session 是删除非当前会话后保留的列表行。 */ session => session.chat_id)).toEqual(['chat-1']);
		expect(hook.result.current.messages).toEqual([messageFixture]);
		await act(
			// deleteActiveAction 删除当前且最后一条会话，页面应进入空态。
			async () => { getSessionPageMock.mockResolvedValueOnce({ sessions: [], has_more: false, next_cursor: undefined }); expect(await hook.result.current.deleteConversation('account-1', 'chat-1')).toBe(true); },
		);
		await waitFor(
			// emptyChatAssertion 等待删除后的选中会话和消息列表完成清理。
			() => expect(hook.result.current.activeChatID).toBe(''),
		);
		expect(hook.result.current.activeSessions).toEqual([]);
		expect(hook.result.current.messages).toEqual([]);
		expect(hook.result.current.retryAvailable).toBe(false);
		expect(deleteSessionMock).toHaveBeenNthCalledWith(1, 'account-1', 'chat-2', expect.objectContaining({ signal: expect.any(AbortSignal) }));
		expect(deleteSessionMock).toHaveBeenNthCalledWith(2, 'account-1', 'chat-1', expect.objectContaining({ signal: expect.any(AbortSignal) }));
			hook.unmount();
		});

		test('删除期间目标实时消息只触发本地恢复且不会自动选择其他会话', /* 当前回调验证删除交错不会进入平台联系人或历史读取链路。 */ async () => {
			// secondSession 是删除当前会话后仍保留但不能自动选中的联系人。
			const secondSession = { ...sessionFixture, chat_id: 'chat-2', peer_user_id: 'buyer-2', peer_name: '第二位买家', unread_count: 0 };
			// deletionCommitted 表示服务端删除替身是否已经完成，用于控制本地读取返回值。
			let deletionCommitted = false;
			getSessionPageMock.mockImplementation(/* _accountID、_cursor、_options、refresh 分别是会话请求定位、取消和读取目的。 */ async (_accountID, _cursor, _options, refresh) => ({
				sessions: deletionCommitted && refresh !== true ? [secondSession] : [sessionFixture, secondSession],
				has_more: false,
			}));
			// resolveDeletion 在目标实时事件进入隔离后释放删除请求。
			let resolveDeletion: (() => void) | undefined;
			deleteSessionMock.mockImplementationOnce(/* _accountID、_chatID、_options 是延迟删除替身无需读取的调用参数。 */ (_accountID, _chatID, _options) => new Promise(/* resolve 保存删除请求的可控完成函数。 */ resolve => {
				resolveDeletion = /* 当前回调提交服务端删除结果并释放等待。 */ () => {
					deletionCommitted = true;
					resolve({ success: true });
				};
			}));
			// hook 是删除与实时消息交错场景的聊天 Hook。
			const hook = renderHook(
				// deletingLiveMessageHookFactory 创建包含当前和备用会话的聊天 Hook。
				() => useChat(),
			);
			await waitFor(
				// activeChatAssertion 等待删除目标成为当前会话。
				() => expect(hook.result.current.activeChatID).toBe('chat-1'),
			);
			await waitFor(
				// initialPlatformRefreshAssertion 等待本次挂载唯一允许的平台刷新完成。
				() => expect(getSessionPageMock.mock.calls.some(/* call 是用于识别 refresh=true 的会话请求参数。 */ call => call[3] === true)).toBe(true),
			);
			// platformRefreshCountBeforeDeletion 记录删除开始前的平台刷新次数。
			const platformRefreshCountBeforeDeletion = getSessionPageMock.mock.calls.filter(/* call 是删除前参与平台刷新计数的会话请求参数。 */ call => call[3] === true).length;
			// deletionPromise 保存仍在等待服务端结果的删除操作。
			let deletionPromise: Promise<boolean> | undefined;
			await act(
				// startDeletionAction 启动删除但保留可控的完成时机。
				() => { deletionPromise = hook.result.current.deleteConversation('account-1', 'chat-1'); },
			);
			await act(
				// targetLiveMessageAction 在删除窗口发布目标会话实时消息。
				() => publishChatLiveMessage({ ...messageFixture, id: 9, message_key: 'during-delete', content: '删除期间消息' }),
			);
			expect(getSessionPageMock.mock.calls.filter(/* call 是删除窗口内参与平台刷新计数的会话请求参数。 */ call => call[3] === true)).toHaveLength(platformRefreshCountBeforeDeletion);
			await act(
				// finishDeletionAction 释放删除结果并等待本地恢复读完。
				async () => {
					resolveDeletion?.();
					expect(await deletionPromise).toBe(true);
				},
			);
			expect(hook.result.current.activeSessions.map(/* session 是删除后仍可见但不应自动打开的会话。 */ session => session.chat_id)).toEqual(['chat-2']);
			expect(hook.result.current.activeChatID).toBe('');
			expect(getSessionPageMock.mock.calls.filter(/* call 是删除收口后参与平台刷新计数的会话请求参数。 */ call => call[3] === true)).toHaveLength(platformRefreshCountBeforeDeletion);
			expect(getSessionPageMock.mock.calls.some(/* call 是验证删除恢复固定使用 refresh=false 的会话请求参数。 */ call => call[3] === false)).toBe(true);
			hook.unmount();
		});

	test('删除失败保留会话并提供独立可清除错误', /* 当前回调验证确认框可在失败后保留并重试。 */ async () => {
		deleteSessionMock.mockRejectedValueOnce(new Error('数据库暂时不可用'));
		// hook 是会话删除失败场景的聊天 Hook。
		const hook = renderHook(
			// failedDeletionHookFactory 创建删除失败场景的聊天 Hook。
			() => useChat(),
		);
		await waitFor(
			// activeChatAssertion 等待默认会话加载完成。
			() => expect(hook.result.current.activeChatID).toBe('chat-1'),
		);
		await act(
			// failedDeleteAction 提交会失败的本地会话删除。
			async () => { expect(await hook.result.current.deleteConversation('account-1', 'chat-1')).toBe(false); },
		);
		expect(hook.result.current.activeSessions).toHaveLength(1);
		expect(hook.result.current.deleteError).toBe('数据库暂时不可用');
		await act(
			// clearDeleteErrorAction 模拟重新打开确认框前清除旧错误。
			() => hook.result.current.clearDeleteError(),
		);
		expect(hook.result.current.deleteError).toBe('');
		hook.unmount();
	});

	test('删除会话取消联系人分页后能够再次加载', /* 当前回调验证删除不会遗留联系人分页忙碌状态或阻断后续请求。 */ async () => {
		// pendingPage 表示第一次联系人分页是否仍在等待，用于制造删除期间的迟到响应。
		let pendingPage = true;
		// contactSignal 保存被删除流程取消的联系人分页信号。
		let contactSignal: AbortSignal | undefined;
		// releasePendingPage 保存迟到分页的可控完成函数，验证其完成不会改写新状态。
		let releasePendingPage: (() => void) | undefined;
		getSessionPageMock.mockImplementation(/* accountID、cursor、options、refresh 分别表示请求归属、游标、取消信号和是否刷新平台。 */ async (_accountID, _cursor, options, _refresh) => {
			if (pendingPage && getSessionPageMock.mock.calls.length >= 3) {
				pendingPage = false;
				contactSignal = options?.signal;
				return new Promise(/* resolve 保存迟到联系人页的完成入口。 */ resolve => {
					releasePendingPage = /* 当前回调释放被取消的联系人页响应。 */ () => resolve({ sessions: [], has_more: false });
				});
			}
			return { sessions: [sessionFixture], has_more: true, next_cursor: 2 };
		});
		// hook 是联系人分页与删除交错场景的聊天 Hook。
		const hook = renderHook(
			// pendingContactsHookFactory 创建联系人请求可控的 Hook。
			() => useChat(),
		);
		await waitFor(
			// activeChatAssertion 等待默认会话完成选择。
			() => expect(hook.result.current.activeChatID).toBe('chat-1'),
		);
		await act(
			// startContactPageAction 启动一个保持未完成的联系人分页。
			() => { void hook.result.current.loadMoreContacts(); },
		);
		await waitFor(
			// contactLoadingAssertion 等待联系人分页建立取消边界。
			() => {
				expect(contactSignal).toBeDefined();
				expect(hook.result.current.contactsLoading).toBe(true);
			},
		);
		await act(
			// deleteAction 删除会话并取消正在进行的联系人分页。
			async () => { expect(await hook.result.current.deleteConversation('account-1', 'chat-1')).toBe(true); },
		);
		expect(contactSignal?.aborted).toBe(true);
		expect(hook.result.current.contactsLoading).toBe(false);
		await act(
			// lateContactPageAction 释放旧响应，确认不会重新占用分页状态。
			async () => { releasePendingPage?.(); },
		);
		await act(
			// reloadContactPageAction 验证删除完成后新的联系人分页仍可启动。
			async () => { await hook.result.current.loadMoreContacts(); },
		);
		expect(hook.result.current.contactsLoading).toBe(false);
		hook.unmount();
	});

	test('切换账号会取消删除请求并丢弃过期成功响应', /* 当前回调验证旧账号删除响应不能改写新账号聊天状态。 */ async () => {
		// secondAccount 是切换后的目标账号，保持在线以完整加载其会话状态。
		const secondAccount: AccountDetail = { ...accountFixture, id: 'account-2', nickname: '第二账号' };
		// secondSession 是目标账号必须在过期删除响应后继续保留的会话。
		const secondSession: ChatSession = { ...sessionFixture, account_id: 'account-2', chat_id: 'chat-2', peer_user_id: 'buyer-2', peer_name: '第二位买家' };
		getDetailsMock.mockResolvedValue([accountFixture, secondAccount]);
		getRuntimeMock.mockResolvedValue({
			'account-1': { state: 'online', connected: true, failures: 0, updated_at: '2026-08-15T00:00:00Z' },
			'account-2': { state: 'online', connected: true, failures: 0, updated_at: '2026-08-15T00:00:00Z' },
		});
		getSessionPageMock.mockImplementation(/* accountID 决定当前初始化请求返回哪个账号的隔离会话。 */ async accountID => ({ sessions: accountID === 'account-2' ? [secondSession] : [sessionFixture], has_more: false }));
		// deletionSignal 保存删除 API 收到的信号，用于断言账号切换已主动取消旧请求。
		let deletionSignal: AbortSignal | undefined;
		// resolveDeletion 在测试完成账号切换后释放模拟的迟到成功响应。
		let resolveDeletion: (() => void) | undefined;
		deleteSessionMock.mockImplementationOnce(/* _accountID、_chatID 和 options 分别是本次延迟删除的定位参数与取消选项。 */ (_accountID, _chatID, options) => {
			deletionSignal = options?.signal;
			return new Promise(/* resolve 保存延迟响应的完成函数，直到账号切换后才调用。 */ resolve => { resolveDeletion = /* 当前回调用成功响应释放延迟删除 Promise。 */ () => resolve({ success: true }); });
		});
		// hook 是同时加载两个账号的聊天 Hook。
		const hook = renderHook(
			// staleDeletionHookFactory 创建用于验证账号隔离的 Hook。
			() => useChat(),
		);
		await waitFor(
			// initialChatAssertion 等待默认账号会话完成选择。
			() => expect(hook.result.current.activeChatID).toBe('chat-1'),
		);
		// deletionPromise 保存仍在等待后端响应的删除结果。
		let deletionPromise: Promise<boolean> | undefined;
		await act(
			// startDeletionAction 启动旧账号删除，但不等待被测试控制的延迟响应。
			() => { deletionPromise = hook.result.current.deleteConversation('account-1', 'chat-1'); },
		);
		await waitFor(
			// deletionStartedAssertion 等待删除请求建立可取消信号。
			() => expect(deletionSignal).toBeDefined(),
		);
		await act(
			// switchAccountAction 在删除完成前切换到另一个账号。
			() => hook.result.current.setActiveAccountID('account-2'),
		);
		expect(deletionSignal?.aborted).toBe(true);
		await act(
			// finishStaleDeletionAction 释放旧请求并确认调用方收到失败结果而非提交旧状态。
			async () => {
				resolveDeletion?.();
				expect(await deletionPromise).toBe(false);
			},
		);
		await waitFor(
			// secondAccountAssertion 等待新账号会话成为当前聊天。
			() => expect(hook.result.current.activeChatID).toBe('chat-2'),
		);
		expect(hook.result.current.activeSessions.map(/* session 是过期响应完成后新账号仍保留的会话。 */ session => session.chat_id)).toEqual(['chat-2']);
		expect(hook.result.current.deleteError).toBe('');
		hook.unmount();
	});

  test('删除后本地首页还有数据时可以继续分页', /* 当前回调覆盖全部加载后删除导致分页重新开始的完整路径。 */ async () => {
    getSessionPageMock.mockResolvedValue({ sessions: [sessionFixture], has_more: false, platform_has_more: false, stored_has_more: false });
    // hook 保存测试页面，初始联系人分页已经耗尽。
    const hook = renderHook(/* 当前回调构造隔离聊天状态。 */ () => useChat());
    await waitFor(/* 当前回调等待初始会话选择。 */ () => expect(hook.result.current.activeChatID).toBe('chat-1'));
    await waitFor(/* 当前回调等待启动时平台首页读取完成。 */ () => expect(getSessionPageMock.mock.calls.some(/* call 表示一个历史会话查询。 */ call => call[3] === true)).toBe(true));
    // remaining 表示删除后本地首页，后面仍有联系人。
    const remaining = { ...sessionFixture, chat_id: 'chat-2' };
    getSessionPageMock.mockResolvedValue({ sessions: [remaining], has_more: true, platform_has_more: false, stored_has_more: true, next_stored_cursor: 'next' });
    await act(/* 当前回调执行删除和本地首页恢复。 */ async () => { expect(await hook.result.current.deleteConversation('account-1', 'chat-1')).toBe(true); });
    expect(hook.result.current.hasMoreContacts).toBe(true);
    // before 保存点击前的查询次数。
    const before = getSessionPageMock.mock.calls.length;
    await act(/* 当前回调请求删除后的下一本地页。 */ async () => { await hook.result.current.loadMoreContacts(); });
    expect(getSessionPageMock.mock.calls.length).toBe(before + 1);
    expect(getSessionPageMock).toHaveBeenLastCalledWith('account-1', undefined, expect.anything(), false, 'next');
    hook.unmount();
  });

  test('刷新首页阻断在途分页且迟到分页不能污染新首页', /* 当前回调验证替换列表与追加列表共用失效边界。 */ async () => {
    // hook 保存已完成启动加载的聊天页面。
    const hook = renderHook(/* 当前回调创建待测 Hook。 */ () => useChat());
    await waitFor(/* 当前回调等待首页准备完成。 */ () => expect(hook.result.current.activeChatID).toBe('chat-1'));
    await act(/* 当前回调明确等待完整首页刷新结束。 */ async () => { await hook.result.current.reloadSessions('account-1'); });
    // pagePending、homePending 分别控制旧分页和新首页的响应时机。
    const pagePending = deferredChatReview<Awaited<ReturnType<typeof getChatSessionPage>>>();
    // homePending 控制新首页返回时机，必须晚于旧分页启动。
    const homePending = deferredChatReview<Awaited<ReturnType<typeof getChatSessionPage>>>();
    getSessionPageMock.mockImplementationOnce(/* 当前回调保留旧分页未完成。 */ () => pagePending.promise);
    // pageRequest 保存旧分页调用的完成结果。
    let pageRequest: Promise<void> | undefined;
    await act(/* 当前回调启动旧分页。 */ () => { pageRequest = hook.result.current.loadMoreContacts(); });
    getSessionPageMock.mockImplementationOnce(/* 当前回调保留新首页未完成。 */ () => homePending.promise);
    // homeRequest 保存新首页调用的完成结果。
    let homeRequest: Promise<ChatSession[]> | undefined;
    await act(/* 当前回调启动首页替换，旧分页此时必须失效。 */ () => { homeRequest = hook.result.current.reloadSessions('account-1'); });
    // before 保存首页在途时的请求数，加载更多不得抢用旧游标。
    const before = getSessionPageMock.mock.calls.length;
    await act(/* 当前回调尝试在首页加载中追加分页。 */ async () => { await hook.result.current.loadMoreContacts(); });
    expect(getSessionPageMock.mock.calls.length).toBe(before);
    await act(/* 当前回调完成新首页。 */ async () => { homePending.resolve({ sessions: [sessionFixture], has_more: false }); await homeRequest; });
    await act(/* 当前回调模拟不理会 AbortSignal 的旧分页迟到。 */ async () => { pagePending.resolve({ sessions: [{ ...sessionFixture, chat_id: 'stale-chat' }], has_more: true }); await pageRequest; });
    expect(hook.result.current.activeSessions.map(/* session 表示最终可见联系人。 */ session => session.chat_id)).toEqual(['chat-1']);
    expect(hook.result.current.hasMoreContacts).toBe(false);
    expect(hook.result.current.contactsLoading).toBe(false);
    hook.unmount();
  });

  test.each([false, true])('旧删除恢复晚到不能解除新账号删除隔离，旧请求失败=%s', /* failed 控制旧删除走成功或错误恢复路径。 */ async failed => {
    // secondAccount、secondSession 分别表示切换目标账号与其待删除会话。
    const secondAccount = { ...accountFixture, id: 'account-2' };
    // secondSession 是第二账号独立的删除目标。
    const secondSession = { ...sessionFixture, account_id: 'account-2', chat_id: 'chat-2' };
    getDetailsMock.mockResolvedValue([accountFixture, secondAccount]);
    getSessionPageMock.mockImplementation(/* accountID 指定本次查询账号，默认返回其当前会话。 */ async accountID => ({ sessions: [accountID === 'account-1' ? sessionFixture : secondSession], has_more: false }));
    // hook 保存可以跨账号切换的聊天页面。
    const hook = renderHook(/* 当前回调构造双账号 Hook。 */ () => useChat());
    await waitFor(/* 当前回调等待第一账号初始化。 */ () => expect(hook.result.current.activeChatID).toBe('chat-1'));
    await act(/* 当前回调等待第一账号平台刷新完成。 */ async () => { await hook.result.current.reloadSessions('account-1'); });
    // oldHome 保存旧删除后的本地恢复请求，可在新账号删除开始后迟到。
    const oldHome = deferredChatReview<Awaited<ReturnType<typeof getChatSessionPage>>>();
    getSessionPageMock.mockImplementationOnce(/* 当前回调阻塞旧删除后的本地首页。 */ () => oldHome.promise);
    if (failed) deleteSessionMock.mockRejectedValueOnce(new Error('删除失败'));
    // oldDelete 保存旧删除的最终结果。
    let oldDelete: Promise<boolean> | undefined;
    await act(/* 当前回调启动旧账号删除并等待到本地恢复阶段。 */ () => { oldDelete = hook.result.current.deleteConversation('account-1', 'chat-1'); });
    await act(/* 当前回调切换账号，取消旧删除上下文。 */ () => { hook.result.current.setActiveAccountID('account-2'); });
    await waitFor(/* 当前回调等待第二账号会话选择。 */ () => expect(hook.result.current.activeChatID).toBe('chat-2'));
    // newDeletePending 阻塞第二账号删除以验证保护标记仍有效。
    const newDeletePending = deferredChatReview<{ /** success 表示服务端删除已完成。 */ success: boolean }>();
    deleteSessionMock.mockImplementationOnce(/* 当前回调保留新删除未完成。 */ () => newDeletePending.promise);
    // newDelete 保存新删除的完成结果。
    let newDelete: Promise<boolean> | undefined;
    await act(/* 当前回调启动第二账号删除。 */ () => { newDelete = hook.result.current.deleteConversation('account-2', 'chat-2'); });
    await act(/* 当前回调释放第一账号的旧本地恢复结果。 */ async () => { oldHome.resolve({ sessions: [], has_more: false }); expect(await oldDelete).toBe(false); });
    // before 保存旧请求收口后查询次数；目标实时事件必须仍被新删除隔离。
    const before = getSessionPageMock.mock.calls.length;
    await act(/* 当前回调发布新删除目标消息。 */ () => { publishChatLiveMessage({ ...messageFixture, account_id: 'account-2', chat_id: 'chat-2', content: '不应显示' }); });
    expect(getSessionPageMock.mock.calls.length).toBe(before);
    expect(hook.result.current.messages.some(/* message 表示当前会话可见消息。 */ message => message.content === '不应显示')).toBe(false);
    expect(hook.result.current.deletingChatID).toBe('chat-2');
    await act(/* 当前回调完成新删除并释放所有 Promise。 */ async () => { newDeletePending.resolve({ success: true }); await newDelete; });
    hook.unmount();
  });
});
