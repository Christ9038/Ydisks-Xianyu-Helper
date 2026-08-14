import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type React from 'react';
import type { AccountDetail, ChatMessage, ChatSession } from '../../../types';
import { emojiURL, renderXianyuText, xianyuEmojis } from '../../../chatEmojis';
import { getAccountDetails, getAccountRuntimeStatuses, getChatMessagePage, getChatSessionPage, markChatRead, sendChatImage, sendChatMessage } from './api';
import { filterChatSessions, formatClock, isChatAbortError, isCurrentChatRequest, mergeLiveMessage, mergeOlderMessages, messageTime } from './state';
import type { ChatFeatureState, ChatLiveState, SessionsByAccount } from './types';

/** Chat Hook 对外暴露的状态、引用和交互动作。 */
export type UseChatResult = ChatFeatureState & {
  /** 当前选中的会话 ID。 */
  activeChatID: string;
  /** 当前账号过滤后的会话。 */
  filteredSessions: ChatSession[];
  /** 消息滚动容器引用。 */
  scrollRef: React.MutableRefObject<HTMLDivElement | null>;
  /** 图片文件输入引用。 */
  imageInputRef: React.MutableRefObject<HTMLInputElement | null>;
  /** 更新当前账号。 */
  setActiveAccountID: React.Dispatch<React.SetStateAction<string>>;
  /** 更新当前会话。 */
  setActiveChatID: React.Dispatch<React.SetStateAction<string>>;
  /** 更新搜索文本。 */
  setSearch: React.Dispatch<React.SetStateAction<string>>;
  /** 更新未读筛选。 */
  setUnreadOnly: React.Dispatch<React.SetStateAction<boolean>>;
  /** 更新消息草稿。 */
  setDraft: React.Dispatch<React.SetStateAction<string>>;
  /** 更新表情选择器状态。 */
  setEmojiOpen: React.Dispatch<React.SetStateAction<boolean>>;
  /** 刷新当前账号会话。 */
  reloadSessions: (accountID: string) => Promise<ChatSession[]>;
  /** 加载更早的联系人。 */
  loadMoreContacts: () => Promise<void>;
  /** 加载更早的消息。 */
  loadOlderMessages: () => Promise<void>;
  /** 根据滚动位置更新自动滚动策略。 */
  handleMessageScroll: () => void;
  /** 发送文本消息。 */
  handleSend: () => Promise<void>;
  /** 发送图片消息。 */
  handleImage: (file?: File) => Promise<void>;
  /** 重试最近一次失败发送。 */
  retrySend: () => Promise<void>;
  /** 是否存在可重试的发送动作。 */
  retryAvailable: boolean;
  /** 列出指定账号的未读总数。 */
  unreadForAccount: (accountID: string) => number;
  /** 表情资源导出，保持页面兼容入口。 */
  emojiURL: typeof emojiURL;
  /** 闲鱼表情列表导出，保持页面兼容入口。 */
  xianyuEmojis: typeof xianyuEmojis;
  /** 闲鱼文本渲染器导出，保持页面兼容入口。 */
  renderXianyuText: typeof renderXianyuText;
  /** 时间格式化函数导出，保持页面兼容入口。 */
  formatClock: typeof formatClock;
  /** 消息时间格式化函数导出，保持页面兼容入口。 */
  messageTime: typeof messageTime;
};

/** 统一管理聊天账号、会话、消息分页、实时连接和发送重试状态。 */
export const useChat = (): UseChatResult => {
  // accounts 保存启用账号及其运行状态。
  const [accounts, setAccounts] = useState<AccountDetail[]>([]);
  // activeAccountID 保存当前选中的账号。
  const [activeAccountID, setActiveAccountID] = useState('');
  // sessionsByAccount 按账号隔离会话列表。
  const [sessionsByAccount, setSessionsByAccount] = useState<SessionsByAccount>({});
  // activeChatID 保存当前选中的会话。
  const [activeChatID, setActiveChatID] = useState('');
  // messages 保存当前会话消息。
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  // search 保存会话搜索文本。
  const [search, setSearch] = useState('');
  // unreadOnly 控制是否仅展示未读会话。
  const [unreadOnly, setUnreadOnly] = useState(false);
  // draft 保存待发送文本。
  const [draft, setDraft] = useState('');
  // loading 表示聊天初始数据加载状态。
  const [loading, setLoading] = useState(true);
  // messagesLoading 表示当前会话消息加载状态。
  const [messagesLoading, setMessagesLoading] = useState(false);
  // olderLoading 表示历史消息分页状态。
  const [olderLoading, setOlderLoading] = useState(false);
  // hasOlder 表示当前会话是否还有历史消息。
  const [hasOlder, setHasOlder] = useState(false);
  // historyCursor 保存历史消息分页游标。
  const [historyCursor, setHistoryCursor] = useState<number | undefined>();
  // contactCursors 保存各账号联系人分页游标。
  const [contactCursors, setContactCursors] = useState<Record<string, number | undefined>>({});
  // hasMoreContacts 保存各账号是否还有联系人。
  const [hasMoreContacts, setHasMoreContacts] = useState<Record<string, boolean>>({});
  // contactsLoading 表示联系人分页状态。
  const [contactsLoading, setContactsLoading] = useState(false);
  // emojiOpen 控制表情选择器显示。
  const [emojiOpen, setEmojiOpen] = useState(false);
  // sending 表示当前是否正在发送消息。
  const [sending, setSending] = useState(false);
  // error 保存聊天页面最近错误。
  const [error, setError] = useState('');
  // liveState 保存 WebSocket 连接状态。
  const [liveState, setLiveState] = useState<ChatLiveState>('connecting');
  // retryText 保存最近失败的文本消息。
  const [retryText, setRetryText] = useState<string | null>(null);
  // retryImage 保存最近失败的图片消息。
  const [retryImage, setRetryImage] = useState<File | null>(null);
  // activeAccountRef 供实时回调读取最新账号。
  const activeAccountRef = useRef('');
  // activeChatRef 供实时回调读取最新会话。
  const activeChatRef = useRef('');
  // scrollRef 指向消息滚动容器。
  const scrollRef = useRef<HTMLDivElement | null>(null);
  // scrollContextRef 保存滚动上下文。
  const scrollContextRef = useRef({ accountID: '', chatID: '' });
  // shouldScrollToBottomRef 控制新消息是否自动滚到底部。
  const shouldScrollToBottomRef = useRef(true);
  // skipNextMessageScrollRef 防止加载历史消息后跳到底部。
  const skipNextMessageScrollRef = useRef(false);
  // imageInputRef 指向图片文件输入框。
  const imageInputRef = useRef<HTMLInputElement | null>(null);
  // refreshedAccountsRef 防止同一账号重复刷新联系人。
  const refreshedAccountsRef = useRef(new Set<string>());
  // sessionSequence 隔离联系人刷新请求。
  const sessionSequence = useRef(0);
  // sessionController 保存当前联系人请求控制器。
  const sessionController = useRef<AbortController | null>(null);
  // messageSequence 隔离会话切换产生的旧消息响应。
  const messageSequence = useRef(0);
  // messageController 保存当前消息请求控制器。
  const messageController = useRef<AbortController | null>(null);
  // contactSequence 隔离联系人分页产生的旧响应。
  const contactSequence = useRef(0);
  // contactController 保存当前联系人分页控制器。
  const contactController = useRef<AbortController | null>(null);
  // sendSequence 隔离账号或会话切换产生的旧发送响应。
  const sendSequence = useRef(0);
  // sendController 保存当前消息发送控制器。
  const sendController = useRef<AbortController | null>(null);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => { activeAccountRef.current = activeAccountID; }, [activeAccountID]);
  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => { activeChatRef.current = activeChatID; }, [activeChatID]);

  /** 刷新指定账号的联系人列表，并丢弃过期响应。 */
  const reloadSessions = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (accountID: string): Promise<ChatSession[]> => {
    // sequence 请求序号。
    const sequence = ++sessionSequence.current;
    sessionController.current?.abort();
    // controller 请求取消控制器。
    const controller = new AbortController();
    sessionController.current = controller;
    try {
      // page 页码。
      const page = await getChatSessionPage(accountID, undefined, { signal: controller.signal }, true);
      if (!isCurrentChatRequest(sessionSequence.current, sequence, controller.signal)) return [];
      setSessionsByAccount(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, [accountID]: page.sessions }));
      setContactCursors(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, [accountID]: page.next_cursor }));
      setHasMoreContacts(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, [accountID]: page.has_more }));
      return page.sessions;
    } catch (/* error 表示错误。 */ error) {
      if (isCurrentChatRequest(sessionSequence.current, sequence, controller.signal) && !isChatAbortError(error)) setError(error instanceof Error ? error.message : '同步会话失败');
      return [];
    }
  }, []);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    // controller 请求取消控制器。
    const controller = new AbortController();
    // load 加载当前数据。
    const load = async (): Promise<void> => {
      setLoading(true);
      try {
        // [details, 解构得到当前 Hook 返回的状态和操作函数。
        const [details, statuses] = await Promise.all([
          getAccountDetails({ signal: controller.signal }),
          getAccountRuntimeStatuses({ signal: controller.signal }),
        ]);
        // withRuntime with运行状态，负责当前功能中的对应处理。
        const withRuntime = details.map(/* 当前回调处理集合中的单个元素。 */ account => ({
          ...account,
          runtime_state: statuses[account.id]?.state || (account.enabled ? 'connecting' : 'disabled'),
          runtime_connected: statuses[account.id]?.connected === true,
        }));
        // enabled 启用状态。
        const enabled = withRuntime.filter(/* 当前回调处理集合中的单个元素。 */ account => account.enabled);
        // sessionPages 会话Pages，负责当前功能中的对应处理。
        const sessionPages = await Promise.all(enabled.map(/* 当前回调处理集合中的单个元素。 */ async account => [account.id, await getChatSessionPage(account.id, undefined, { signal: controller.signal })] as const));
        if (controller.signal.aborted) return;
        setAccounts(enabled);
        setSessionsByAccount(Object.fromEntries(sessionPages.map(/* 当前回调处理集合中的单个元素。 */ ([id, page]) => [id, page.sessions])));
        setContactCursors(Object.fromEntries(sessionPages.map(/* 当前回调处理集合中的单个元素。 */ ([id, page]) => [id, page.next_cursor])));
        setHasMoreContacts(Object.fromEntries(sessionPages.map(/* 当前回调处理集合中的单个元素。 */ ([id, page]) => [id, page.has_more])));
        // stored 已保存数据。
        const stored = window.localStorage.getItem('ydisks.chat.account.v1') || '';
        // first 首项。
        const first = enabled.some(/* 当前回调处理集合中的单个元素。 */ account => account.id === stored) ? stored : enabled[0]?.id || '';
        setActiveAccountID(first);
      } catch (/* loadError 表示加载错误。 */ loadError) {
        if (!controller.signal.aborted) setError(loadError instanceof Error ? loadError.message : '加载聊天数据失败');
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    };
    void load();
    return /* 当前回调处理用户交互或异步状态变化。 */ () => controller.abort();
  }, []);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    // disposed disposed，负责当前功能中的对应处理。
    let disposed = false;
    // timer 定时器。
    let timer = 0;
    // controller 请求取消控制器。
    let controller: AbortController | null = null;
    // poll 轮询函数。
    const poll = async (): Promise<void> => {
      controller = new AbortController();
      try {
        // statuses statuses，负责当前功能中的对应处理。
        const statuses = await getAccountRuntimeStatuses({ signal: controller.signal, timeoutMs: 10_000 });
        if (!disposed) setAccounts(/* 当前回调处理集合中的单个元素。 */ current => current.map(/* 当前回调处理集合中的单个元素。 */ account => ({
          ...account,
          runtime_state: statuses[account.id]?.state || account.runtime_state,
          runtime_connected: statuses[account.id]?.connected ?? account.runtime_connected,
        })));
      } catch {
        // WebSocket 拥有独立的可见状态，短暂轮询失败不清除已加载会话。
      } finally {
        if (!disposed) timer = window.setTimeout(poll, 3_000);
      }
    };
    timer = window.setTimeout(poll, 3_000);
    return /* 当前回调处理用户交互或异步状态变化。 */ () => {
      disposed = true;
      window.clearTimeout(timer);
      controller?.abort();
    };
  }, []);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    if (!activeAccountID) return;
    window.localStorage.setItem('ydisks.chat.account.v1', activeAccountID);
    // sessions 会话列表。
    const sessions = sessionsByAccount[activeAccountID] || [];
    setActiveChatID(/* 当前回调处理集合中的单个元素。 */ current => sessions.some(/* 当前回调处理集合中的单个元素。 */ session => session.chat_id === current) ? current : sessions[0]?.chat_id || '');
  }, [activeAccountID, sessionsByAccount]);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    if (!activeAccountID || refreshedAccountsRef.current.has(activeAccountID)) return;
    refreshedAccountsRef.current.add(activeAccountID);
    void reloadSessions(activeAccountID);
  }, [activeAccountID, reloadSessions]);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    if (!activeAccountID || !activeChatID) {
      setMessages([]);
      return;
    }
    // sequence 请求序号。
    const sequence = ++messageSequence.current;
    messageController.current?.abort();
    // controller 请求取消控制器。
    const controller = new AbortController();
    messageController.current = controller;
    setMessagesLoading(true);
    void Promise.all([
      getChatMessagePage(activeAccountID, activeChatID, undefined, undefined, { signal: controller.signal }),
      markChatRead(activeAccountID, activeChatID, { signal: controller.signal }),
    ]).then(/* 当前回调处理用户交互或异步状态变化。 */ ([page]) => {
      if (!isCurrentChatRequest(messageSequence.current, sequence, controller.signal)) return;
      setMessages(page.messages);
      setHasOlder(page.has_more);
      setHistoryCursor(page.next_cursor);
      if (page.session) setSessionsByAccount(/* 当前回调处理集合中的单个元素。 */ current => ({ ...current, [activeAccountID]: (current[activeAccountID] || []).map(/* 当前回调处理集合中的单个元素。 */ session => session.chat_id === page.session?.chat_id ? page.session! : session) }));
      setSessionsByAccount(/* 当前回调处理集合中的单个元素。 */ current => ({ ...current, [activeAccountID]: (current[activeAccountID] || []).map(/* 当前回调处理集合中的单个元素。 */ session => session.chat_id === activeChatID ? { ...session, unread_count: 0 } : session) }));
    }).catch(/* 当前回调处理用户交互或异步状态变化。 */ loadError => {
      if (isCurrentChatRequest(messageSequence.current, sequence, controller.signal) && !isChatAbortError(loadError)) setError(loadError instanceof Error ? loadError.message : '加载消息失败');
    }).finally(/* 当前回调处理用户交互或异步状态变化。 */ () => {
      if (isCurrentChatRequest(messageSequence.current, sequence, controller.signal)) setMessagesLoading(false);
    });
    return /* 当前回调处理用户交互或异步状态变化。 */ () => controller.abort();
  }, [activeAccountID, activeChatID]);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    contactController.current?.abort();
    contactSequence.current += 1;
  }, [activeAccountID]);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    sendController.current?.abort();
    sendSequence.current += 1;
  }, [activeAccountID, activeChatID]);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => /* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    sessionController.current?.abort();
    messageController.current?.abort();
    contactController.current?.abort();
    sendController.current?.abort();
  }, []);

  /** 加载当前会话更早消息并保持滚动位置。 */
  const loadOlderMessages = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (): Promise<void> => {
    if (!activeAccountID || !activeChatID || olderLoading || !hasOlder) return;
    // container 容器。
    const container = scrollRef.current;
    // previousHeight 上一项高度，负责当前功能中的对应处理。
    const previousHeight = container?.scrollHeight || 0;
    // sequence 请求序号。
    const sequence = messageSequence.current;
    // controller 请求取消控制器。
    const controller = new AbortController();
    skipNextMessageScrollRef.current = true;
    setOlderLoading(true);
    setError('');
    try {
      // oldestID 最早标识，负责当前功能中的对应处理。
      const oldestID = messages[0]?.id;
      // page 页码。
      const page = await getChatMessagePage(activeAccountID, activeChatID, historyCursor, oldestID, { signal: controller.signal });
      if (!isCurrentChatRequest(messageSequence.current, sequence, controller.signal)) return;
      setMessages(/* 当前回调处理用户交互或异步状态变化。 */ current => mergeOlderMessages(current, page.messages));
      setHasOlder(page.has_more);
      setHistoryCursor(page.next_cursor);
      requestAnimationFrame(/* 当前回调处理用户交互或异步状态变化。 */ () => {
        if (container) container.scrollTop += container.scrollHeight - previousHeight;
      });
    } catch (/* loadError 表示加载错误。 */ loadError) {
      skipNextMessageScrollRef.current = false;
      if (!isChatAbortError(loadError)) setError(loadError instanceof Error ? loadError.message : '加载历史消息失败');
    } finally {
      setOlderLoading(false);
    }
  }, [activeAccountID, activeChatID, hasOlder, historyCursor, messages, olderLoading]);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    // disposed disposed，负责当前功能中的对应处理。
    let disposed = false;
    // reconnectTimer reconnect定时器，负责当前功能中的对应处理。
    let reconnectTimer = 0;
    // retry 重试当前操作。
    let retry = 0;
    // socket WebSocket 连接。
    let socket: WebSocket | null = null;
    // connect 连接。
    const connect = (): void => {
      if (disposed) return;
      setLiveState('connecting');
      // protocol 连接协议。
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      socket = new WebSocket(`${protocol}//${window.location.host}/api/v1/chat/ws`);
      socket.onopen = /* 当前回调处理用户交互或异步状态变化。 */ () => { retry = 0; setLiveState('online'); };
      socket.onmessage = /* 当前回调处理用户交互或异步状态变化。 */ event => {
        try {
          // payload 请求载荷。
          const payload = JSON.parse(event.data);
          // message 消息。
          const message = payload.message as ChatMessage | undefined;
          if (!message) return;
          // accountID 账号标识。
          const accountID = message.account_id;
          setSessionsByAccount(/* 当前回调处理用户交互或异步状态变化。 */ current => {
            // rows 行数据。
            const rows = current[accountID] || [];
            // found 匹配结果。
            const found = rows.some(/* 当前回调处理集合中的单个元素。 */ row => row.chat_id === message.chat_id);
            if (!found) {
              void reloadSessions(accountID);
              return current;
            }
            return { ...current, [accountID]: rows.map(/* 当前回调处理集合中的单个元素。 */ row => row.chat_id === message.chat_id ? {
              ...row,
              last_message: message.content,
              last_message_at: message.sent_at,
              unread_count: message.direction === 'incoming' && (activeAccountRef.current !== accountID || activeChatRef.current !== message.chat_id) ? row.unread_count + 1 : row.unread_count,
            } : row).sort(/* 当前回调处理集合中的单个元素。 */ (a, b) => b.last_message_at - a.last_message_at) };
          });
          if (activeAccountRef.current === accountID && activeChatRef.current === message.chat_id) {
            setMessages(/* 当前回调处理用户交互或异步状态变化。 */ current => mergeLiveMessage(current, message));
            if (message.direction === 'incoming') void markChatRead(accountID, message.chat_id);
          }
        } catch {
          // 忽略非聊天格式的 WebSocket 帧，后续 REST 查询会恢复权威状态。
        }
      };
      socket.onclose = /* 当前回调处理用户交互或异步状态变化。 */ () => {
        if (disposed) return;
        setLiveState('offline');
        // delay 延迟。
        const delay = Math.min(15_000, 1_000 * 2 ** Math.min(retry++, 4));
        reconnectTimer = window.setTimeout(connect, delay);
      };
      socket.onerror = /* 当前回调处理用户交互或异步状态变化。 */ () => socket?.close();
    };
    connect();
    return /* 当前回调处理用户交互或异步状态变化。 */ () => {
      disposed = true;
      window.clearTimeout(reconnectTimer);
      socket?.close();
    };
  }, [reloadSessions]);

  /** 根据滚动位置决定新消息是否自动滚到底部。 */
  const handleMessageScroll = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ () => {
    // container 容器。
    const container = scrollRef.current;
    if (!container) return;
    // distanceFromBottom 距离FromBottom，负责当前功能中的对应处理。
    const distanceFromBottom = container.scrollHeight - container.scrollTop - container.clientHeight;
    shouldScrollToBottomRef.current = distanceFromBottom <= 48;
  }, []);

  useLayoutEffect(/* 当前回调处理用户交互或异步状态变化。 */ () => {
    // contextChanged 上下文Changed，负责当前功能中的对应处理。
    const contextChanged = scrollContextRef.current.accountID !== activeAccountID || scrollContextRef.current.chatID !== activeChatID;
    scrollContextRef.current = { accountID: activeAccountID, chatID: activeChatID };
    if (contextChanged) shouldScrollToBottomRef.current = true;
    // container 容器。
    const container = scrollRef.current;
    if (!container) return;
    if (skipNextMessageScrollRef.current) {
      skipNextMessageScrollRef.current = false;
      return;
    }
    if (messagesLoading || shouldScrollToBottomRef.current) container.scrollTop = container.scrollHeight;
  }, [activeAccountID, activeChatID, messages, messagesLoading]);

  // activeAccount 当前状态账号，负责当前功能中的对应处理。
  const activeAccount = accounts.find(/* 当前回调处理集合中的单个元素。 */ account => account.id === activeAccountID);
  // activeSessions 当前状态会话列表，负责当前功能中的对应处理。
  const activeSessions = sessionsByAccount[activeAccountID] || [];
  // selectedSession 处理当前选择（ed会话）。
  const selectedSession = activeSessions.find(/* 当前回调处理集合中的单个元素。 */ session => session.chat_id === activeChatID);
  // filteredSessions 过滤后的会话列表，负责当前功能中的对应处理。
  const filteredSessions = useMemo(/* 当前回调计算并缓存派生数据。 */ () => filterChatSessions(activeSessions, search, unreadOnly), [activeSessions, search, unreadOnly]);
  // unreadForAccount unreadFor账号，负责当前功能中的对应处理。
  const unreadForAccount = useCallback(/* 当前回调处理集合中的单个元素。 */ (accountID: string) => (sessionsByAccount[accountID] || []).reduce(/* 当前回调处理集合中的单个元素。 */ (sum, session) => sum + session.unread_count, 0), [sessionsByAccount]);

  /** 加载当前账号下一页联系人。 */
  const loadMoreContacts = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (): Promise<void> => {
    if (!activeAccountID || contactsLoading || !hasMoreContacts[activeAccountID]) return;
    // sequence 请求序号。
    const sequence = ++contactSequence.current;
    contactController.current?.abort();
    // controller 请求取消控制器。
    const controller = new AbortController();
    contactController.current = controller;
    // accountID 账号标识。
    const accountID = activeAccountID;
    setContactsLoading(true);
    setError('');
    try {
      // page 页码。
      const page = await getChatSessionPage(accountID, contactCursors[accountID], { signal: controller.signal }, true);
      if (!isCurrentChatRequest(contactSequence.current, sequence, controller.signal)) return;
      setSessionsByAccount(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, [accountID]: page.sessions }));
      setContactCursors(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, [accountID]: page.next_cursor }));
      setHasMoreContacts(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, [accountID]: page.has_more }));
    } catch (/* loadError 表示加载错误。 */ loadError) {
      if (isCurrentChatRequest(contactSequence.current, sequence, controller.signal) && !isChatAbortError(loadError)) setError(loadError instanceof Error ? loadError.message : '加载历史联系人失败');
    } finally {
      if (isCurrentChatRequest(contactSequence.current, sequence, controller.signal)) setContactsLoading(false);
    }
  }, [activeAccountID, contactCursors, contactsLoading, hasMoreContacts]);

  /** 发送文本消息并记录失败重试数据。 */
  const sendText = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (text: string, rememberRetry: boolean): Promise<void> => {
    if (!selectedSession || !activeAccountID || sending) return;
    // sequence 请求序号。
    const sequence = ++sendSequence.current;
    sendController.current?.abort();
    // controller 请求取消控制器。
    const controller = new AbortController();
    sendController.current = controller;
    setSending(true);
    setError('');
    try {
      // result 处理结果。
      const result = await sendChatMessage({ account_id: activeAccountID, chat_id: selectedSession.chat_id, buyer_id: selectedSession.buyer_id, buyer_name: selectedSession.buyer_name, item_id: selectedSession.item_id, item_title: selectedSession.item_title, text }, { signal: controller.signal });
      if (!isCurrentChatRequest(sendSequence.current, sequence, controller.signal)) return;
      setDraft('');
      setRetryText(null);
      setMessages(/* 当前回调处理用户交互或异步状态变化。 */ current => mergeLiveMessage(current, result.message));
    } catch (/* sendError 表示发送错误。 */ sendError) {
      if (isCurrentChatRequest(sendSequence.current, sequence, controller.signal)) {
        if (rememberRetry) setRetryText(text);
        if (!isChatAbortError(sendError)) setError(sendError instanceof Error ? sendError.message : '消息发送失败');
      }
    } finally {
      if (isCurrentChatRequest(sendSequence.current, sequence, controller.signal)) setSending(false);
    }
  }, [activeAccountID, selectedSession, sending]);

  /** 发送图片消息并记录失败重试数据。 */
  const sendImage = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (file: File, rememberRetry: boolean): Promise<void> => {
    if (!selectedSession || !activeAccountID || sending) return;
    // sequence 请求序号。
    const sequence = ++sendSequence.current;
    sendController.current?.abort();
    // controller 请求取消控制器。
    const controller = new AbortController();
    sendController.current = controller;
    setSending(true);
    setError('');
    try {
      // result 处理结果。
      const result = await sendChatImage({ account_id: activeAccountID, chat_id: selectedSession.chat_id, buyer_id: selectedSession.buyer_id, buyer_name: selectedSession.buyer_name, buyer_avatar_url: selectedSession.buyer_avatar_url, item_id: selectedSession.item_id, item_title: selectedSession.item_title, image: file }, { signal: controller.signal });
      if (!isCurrentChatRequest(sendSequence.current, sequence, controller.signal)) return;
      setRetryImage(null);
      setMessages(/* 当前回调处理用户交互或异步状态变化。 */ current => mergeLiveMessage(current, result.message));
    } catch (/* sendError 表示发送错误。 */ sendError) {
      if (isCurrentChatRequest(sendSequence.current, sequence, controller.signal)) {
        if (rememberRetry) setRetryImage(file);
        if (!isChatAbortError(sendError)) setError(sendError instanceof Error ? sendError.message : '图片发送失败');
      }
    } finally {
      if (isCurrentChatRequest(sendSequence.current, sequence, controller.signal)) {
        setSending(false);
        if (imageInputRef.current) imageInputRef.current.value = '';
      }
    }
  }, [activeAccountID, selectedSession, sending]);

  /** 处理文本发送按钮和 Enter 快捷键。 */
  const handleSend = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (): Promise<void> => {
    // text 文本。
    const text = draft.trim();
    if (!text || !selectedSession || !activeAccountID || sending) return;
    await sendText(text, true);
  }, [activeAccountID, draft, selectedSession, sendText, sending]);

  /** 处理图片选择并发送。 */
  const handleImage = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (file?: File): Promise<void> => {
    if (!file || !selectedSession || !activeAccountID || sending) return;
    await sendImage(file, true);
  }, [activeAccountID, selectedSession, sendImage, sending]);

  /** 重试最近一次失败的文本或图片发送。 */
  const retrySend = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (): Promise<void> => {
    if (retryText) await sendText(retryText, false);
    else if (retryImage) await sendImage(retryImage, false);
  }, [retryImage, retryText, sendImage, sendText]);

  return {
    accounts, activeAccountID, activeSessions, selectedSession, activeAccount, messages, search, unreadOnly, draft, loading, messagesLoading, olderLoading, hasOlder, contactsLoading, hasMoreContacts: hasMoreContacts[activeAccountID] === true, emojiOpen, sending, error, liveState,
    activeChatID, filteredSessions, scrollRef, imageInputRef, setActiveAccountID, setActiveChatID, setSearch, setUnreadOnly, setDraft, setEmojiOpen,
    reloadSessions, loadMoreContacts, loadOlderMessages, handleMessageScroll, handleSend, handleImage, retrySend, retryAvailable: Boolean(retryText || retryImage), unreadForAccount,
    emojiURL, xianyuEmojis, renderXianyuText, formatClock, messageTime,
  };
};
