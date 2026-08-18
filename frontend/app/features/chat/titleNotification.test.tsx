// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { ChatMessage } from '../../../shared/api-contract/chat';
import { getAccountDetails,getChatSessionPage } from './api';
import { publishChatUnreadStatus } from './liveEvents';
import { formatChatNewMessageTitle,useChatTitleNotification,useChatTitleNotifier } from './titleNotification';

vi.mock('./api', /* chatApiMockFactory 提供标题通知初始化未读状态读取的确定性 API 替身。 */ () => ({
  getAccountDetails: vi.fn(),
  getChatSessionPage: vi.fn(),
}));

// getAccountDetailsMock 是标题通知初始化账号读取的可控替身。
const getAccountDetailsMock = vi.mocked(getAccountDetails);
// getChatSessionPageMock 是标题通知初始化会话未读读取的可控替身。
const getChatSessionPageMock = vi.mocked(getChatSessionPage);

// originalTitle 保存每个测试开始前的浏览器标题，测试结束后必须恢复以避免影响其他聊天 Hook 用例。
let originalTitle = '';

// incomingMessageFixture 是触发标题提醒的普通买家入站消息。
const incomingMessageFixture = { direction: 'incoming', message_type: 'text' } as ChatMessage;

// latestSocket 保存全局标题通知 Hook 创建的可控 WebSocket 替身。
let latestSocket: {
  // close 是关闭通知连接的替身方法。
  close: ReturnType<typeof vi.fn>;
  // onopen 是通知连接建立成功回调。
  onopen: (() => void) | null;
  // onmessage 是服务端广播聊天帧的回调。
  onmessage: ((event: { /* data 保存服务端推送的 JSON 文本。 */ data: string }) => void) | null;
  // onclose 是通知连接关闭回调。
  onclose: (() => void) | null;
  // onerror 是通知连接异常回调。
  onerror: (() => void) | null;
} | null = null;

describe('chat title notification', /* 当前测试组验证后台实时消息的浏览器标题提示行为。 */ () => {
  beforeEach(/* 当前回调重置标题和时间，隔离浏览器全局状态。 */ () => {
    originalTitle = document.title;
    document.title = 'Ydisks闲鱼助手';
    vi.useFakeTimers();
    latestSocket = null;
    getAccountDetailsMock.mockResolvedValue([]);
    getChatSessionPageMock.mockResolvedValue({ sessions: [], has_more: false });
  });

  afterEach(/* 当前回调恢复真实计时器、标题和浏览器焦点方法，避免泄漏到其他前端测试。 */ () => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    document.title = originalTitle;
  });

  test('格式化标题不会暴露新消息数量', /* 当前回调验证标题只展示固定的新消息标记。 */ () => {
    expect(formatChatNewMessageTitle('应用')).toBe('【新消息】应用');
  });

  test('新消息会累计闪烁标题并在重新聚焦后恢复', /* 当前回调验证前后台一致的实时计数、闪烁周期与用户确认后的清除行为。 */ () => {
    // hook 是标题通知 Hook 的渲染结果。
    const hook = renderHook(
      // titleNotificationHookFactory 创建浏览器标题通知 Hook。
      () => useChatTitleNotifier(),
    );
    act(
      // firstMessageAction 模拟第一条实时消息。
      () => hook.result.current.notifyIncomingMessage(incomingMessageFixture),
    );
    act(
      // secondMessageAction 模拟同一提醒周期收到第二条消息。
      () => hook.result.current.notifyIncomingMessage(incomingMessageFixture),
    );
    expect(document.title).toBe('【新消息】Ydisks闲鱼助手');
    expect(hook.result.current.hasUnreadChatMessage).toBe(true);
    act(
      // flashAction 推进一个闪烁周期以展示原始标题相位。
      () => vi.advanceTimersByTime(1_000),
    );
    expect(document.title).toBe('Ydisks闲鱼助手');
    expect(hook.result.current.hasUnreadChatMessage).toBe(true);
    act(
      // unreadClearedAction 模拟聊天页确认所有会话均无未读消息。
      () => publishChatUnreadStatus(false),
    );
    expect(hook.result.current.hasUnreadChatMessage).toBe(false);
    act(
      // focusAction 模拟用户重新聚焦浏览器窗口，确认已看到提醒。
      () => window.dispatchEvent(new Event('focus')),
    );
    expect(document.title).toBe('Ydisks闲鱼助手');
    hook.unmount();
  });

  test('系统消息和本机发出的消息不会改写浏览器标题', /* 当前回调验证不属于买家新消息的事件不会触发标题提醒。 */ () => {
    // hook 是消息方向过滤场景的标题通知 Hook 渲染结果。
    const hook = renderHook(
      // messageDirectionHookFactory 创建消息方向过滤场景。
      () => useChatTitleNotifier(),
    );
    act(
      // outgoingMessageAction 模拟本机发送的消息。
      () => hook.result.current.notifyIncomingMessage({ ...incomingMessageFixture, direction: 'outgoing' }),
    );
    act(
      // systemMessageAction 模拟后台收到不应通知用户的系统消息。
      () => hook.result.current.notifyIncomingMessage({ ...incomingMessageFixture, message_type: 'system' }),
    );
    expect(document.title).toBe('Ydisks闲鱼助手');
    hook.unmount();
  });

  test('认证应用壳挂载的通知连接会接收在线聊天页之外的消息', /* 当前回调验证标题提醒的 WebSocket 不依赖 Chat 页面是否已打开。 */ () => {
    // websocketFactory 创建不连接真实服务的全局通知连接替身。
    const websocketFactory = vi.fn(
      // websocketConstructor 创建可手动触发服务端消息的连接对象。
      function websocketConstructor() {
        // socket 是当前全局通知连接的可控替身。
        const socket = { close: vi.fn(), onopen: null, onmessage: null, onclose: null, onerror: null };
        latestSocket = socket;
        return socket;
      },
    );
    vi.stubGlobal('WebSocket', websocketFactory);
    // hook 是认证应用壳中全局标题通知连接的渲染结果。
    const hook = renderHook(
      // globalNotificationHookFactory 挂载不依赖 Chat 页面存在的全局通知 Hook。
      () => useChatTitleNotification(),
    );
    expect(websocketFactory).toHaveBeenCalledWith(expect.stringMatching(/\/api\/v1\/chat\/ws$/));
    act(
      // incomingMessageAction 模拟用户停留在仪表盘等其他页面时服务端推送买家消息。
      () => latestSocket?.onmessage?.({ data: JSON.stringify({ message: incomingMessageFixture }) }),
    );
    expect(document.title).toBe('【新消息】Ydisks闲鱼助手');
    expect(hook.result.current.hasUnreadChatMessage).toBe(true);
    hook.unmount();
    expect(latestSocket?.close).toHaveBeenCalledTimes(1);
  });

  test('应用壳启动时会根据已有未读会话点亮聊天红点', /* 当前回调验证红点不只依赖当前浏览器会话期间的新 WebSocket 消息。 */ async () => {
    // unreadSession 保存服务端返回的一条历史未读会话。
    const unreadSession = { account_id: 'account-1', chat_id: 'chat-1', buyer_id: 'buyer-1', buyer_name: '买家', last_message: '历史未读消息', last_message_at: 1, unread_count: 1 };
    getAccountDetailsMock.mockResolvedValue([{ id: 'account-1', enabled: true }] as never);
    getChatSessionPageMock.mockResolvedValue({ sessions: [unreadSession], has_more: false });
    // websocketFactory 创建不会连接真实服务的全局通知连接替身。
    const websocketFactory = vi.fn(
      // websocketConstructor 创建初始化未读读取测试所需的通知连接对象。
      function websocketConstructor() {
        // socket 是当前测试创建的通知连接替身。
        const socket = { close: vi.fn(), onopen: null, onmessage: null, onclose: null, onerror: null };
        latestSocket = socket;
        return socket;
      },
    );
    vi.stubGlobal('WebSocket', websocketFactory);
    // hook 是应用壳初始化阶段的全局标题通知 Hook。
    const hook = renderHook(
      // initialUnreadHookFactory 创建验证历史未读状态的通知 Hook。
      () => useChatTitleNotification(),
    );
    await act(
      // initialUnreadFlushAction 显式刷新初始化账号和会话读取 Promise，避免假定时器阻塞 waitFor 轮询。
      async () => { await Promise.resolve(); await Promise.resolve(); },
    );
    expect(hook.result.current.hasUnreadChatMessage).toBe(true);
    expect(getChatSessionPageMock).toHaveBeenCalledWith('account-1', undefined, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    hook.unmount();
  });

  test('延迟的初始未读快照不会覆盖期间收到的实时消息', /* 当前回调验证初始查询与 WebSocket 入站消息的先后竞态。 */ async () => {
    // resolveAccounts 用于在实时消息到达后才完成初始账号快照请求。
    let resolveAccounts: ((accounts: never[]) => void) | undefined;
    getAccountDetailsMock.mockImplementationOnce(
      // delayedAccountsRequest 维持初始账号读取未完成，模拟网络慢响应。
      () => new Promise<never[]>(/* delayedAccountsExecutor 保存延迟账号快照的完成函数。 */ resolve => { resolveAccounts = resolve; }),
    );
    // websocketFactory 创建可在初始快照未完成时接收消息的全局通知连接替身。
    const websocketFactory = vi.fn(
      // websocketConstructor 创建实时竞态测试所需的通知连接对象。
      function websocketConstructor() {
        // socket 是当前测试创建的通知连接替身。
        const socket = { close: vi.fn(), onopen: null, onmessage: null, onclose: null, onerror: null };
        latestSocket = socket;
        return socket;
      },
    );
    vi.stubGlobal('WebSocket', websocketFactory);
    // hook 是初始未读快照仍在加载时的全局标题通知 Hook。
    const hook = renderHook(
      // delayedSnapshotHookFactory 创建初始快照与实时消息竞态场景。
      () => useChatTitleNotification(),
    );
    act(
      // realtimeMessageAction 模拟慢快照期间先收到一条买家消息。
      () => latestSocket?.onmessage?.({ data: JSON.stringify({ message: incomingMessageFixture }) }),
    );
    await act(
      // staleSnapshotAction 在收到实时消息后才让旧快照返回无账号、无未读结果。
      async () => { resolveAccounts?.([]); await Promise.resolve(); },
    );
    expect(hook.result.current.hasUnreadChatMessage).toBe(true);
    hook.unmount();
  });
});
