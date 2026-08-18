import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import {
  AlertCircle, Check, CheckCheck, ImagePlus, Loader2, MessageCircleMore, RefreshCw,
  Search, Send, Smile, UserRound, Wifi, WifiOff, X,
} from 'lucide-react';
import { AccountDetail, ChatMessage, ChatSession } from '../types';
import {
  getAccountDetails, getAccountRuntimeStatuses, getChatMessagePage, getChatMessages, getChatSessionPage, getChatSessions,
  markChatRead, sendChatImage, sendChatMessage,
} from '../services/api';
import { emojiURL, renderXianyuText, xianyuEmojis } from '../chatEmojis';
import Lightbox from 'yet-another-react-lightbox';
import 'yet-another-react-lightbox/styles.css';

type SessionsByAccount = Record<string, ChatSession[]>;

const accountStorageKey = 'ydisks.chat.account.v1';

const unreadLabel = (count: number): string => {
  const normalized = Number.isFinite(count) ? Math.max(0, Math.floor(count)) : 0;
  return normalized > 99 ? '99+' : String(normalized);
};

const UnreadBadge: React.FC<{ count: number; className?: string }> = ({ count, className = '' }) => {
  if (!Number.isFinite(count) || count <= 0) return null;
  return (
    <span aria-label={`未读消息 ${unreadLabel(count)} 条`} className={`inline-flex h-5 min-w-5 shrink-0 items-center justify-center whitespace-nowrap rounded-full bg-red-500 px-1.5 text-[10px] font-bold leading-none text-white ${className}`}>
      {unreadLabel(count)}
    </span>
  );
};

const formatClock = (value: number): string => {
  if (!value) return '';
  const date = new Date(value < 10_000_000_000 ? value * 1000 : value);
  const today = new Date();
  if (date.toDateString() === today.toDateString()) {
    return date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false });
  }
  return date.toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' });
};

const messageTime = (value: number): string => {
  const date = new Date(value < 10_000_000_000 ? value * 1000 : value);
  return date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false });
};

const Chat: React.FC = () => {
  const [accounts, setAccounts] = useState<AccountDetail[]>([]);
  const [activeAccountID, setActiveAccountID] = useState('');
  const [sessionsByAccount, setSessionsByAccount] = useState<SessionsByAccount>({});
  const [activeChatID, setActiveChatID] = useState('');
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [search, setSearch] = useState('');
  const [unreadOnly, setUnreadOnly] = useState(false);
  const [draft, setDraft] = useState('');
  const [loading, setLoading] = useState(true);
  const [messagesLoading, setMessagesLoading] = useState(false);
  const [olderLoading, setOlderLoading] = useState(false);
  const [hasOlder, setHasOlder] = useState(false);
  const [historyCursor, setHistoryCursor] = useState<number | undefined>();
  const [contactCursors, setContactCursors] = useState<Record<string, number | undefined>>({});
  const [hasMoreContacts, setHasMoreContacts] = useState<Record<string, boolean>>({});
  const [contactsLoading, setContactsLoading] = useState(false);
  const [emojiOpen, setEmojiOpen] = useState(false);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState('');
  const [pendingImage, setPendingImage] = useState<{ file: File; url: string } | null>(null);
  const [lightboxIndex, setLightboxIndex] = useState(-1);
  const [liveState, setLiveState] = useState<'connecting' | 'online' | 'offline'>('connecting');
  const activeAccountRef = useRef('');
  const activeChatRef = useRef('');
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const scrollContextRef = useRef({ accountID: '', chatID: '' });
  const shouldScrollToBottomRef = useRef(true);
  const skipNextMessageScrollRef = useRef(false);
  const imageInputRef = useRef<HTMLInputElement | null>(null);
  const refreshedAccountsRef = useRef(new Set<string>());

  useEffect(() => { activeAccountRef.current = activeAccountID; }, [activeAccountID]);
  useEffect(() => { activeChatRef.current = activeChatID; }, [activeChatID]);

  const reloadSessions = async (accountID: string) => {
    const page = await getChatSessionPage(accountID, undefined, undefined, true);
    // The session list request can finish after markChatRead during a refresh.
    // Keep the currently open conversation locally read so a stale response
    // cannot resurrect its red badge.
    const sessions = page.sessions.map(session =>
      accountID === activeAccountRef.current && session.chat_id === activeChatRef.current
        ? { ...session, unread_count: 0 }
        : session);
    setSessionsByAccount(current => ({ ...current, [accountID]: sessions }));
    setContactCursors(current => ({ ...current, [accountID]: page.next_cursor }));
    setHasMoreContacts(current => ({ ...current, [accountID]: page.has_more }));
    return sessions;
  };

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      setLoading(true);
      try {
        const [details, statuses] = await Promise.all([getAccountDetails(), getAccountRuntimeStatuses()]);
        const withRuntime = details.map(account => ({
          ...account,
          runtime_state: statuses[account.id]?.state || (account.enabled ? 'connecting' : 'disabled'),
          runtime_connected: statuses[account.id]?.connected === true,
        }));
        const enabled = withRuntime.filter(account => account.enabled);
        const sessionPages = await Promise.all(enabled.map(async account => [account.id, await getChatSessionPage(account.id)] as const));
        if (cancelled) return;
        setAccounts(enabled);
        setSessionsByAccount(Object.fromEntries(sessionPages.map(([id, page]) => [id, page.sessions])));
        setContactCursors(Object.fromEntries(sessionPages.map(([id, page]) => [id, page.next_cursor])));
        setHasMoreContacts(Object.fromEntries(sessionPages.map(([id, page]) => [id, page.has_more])));
        const stored = window.localStorage.getItem(accountStorageKey) || '';
        const first = enabled.some(account => account.id === stored) ? stored : enabled[0]?.id || '';
        setActiveAccountID(first);
      } catch (loadError) {
        if (!cancelled) setError(loadError instanceof Error ? loadError.message : '加载聊天数据失败');
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    void load();
    return () => { cancelled = true; };
  }, []);

  useEffect(() => {
    let disposed = false;
    let timer = 0;
    let controller: AbortController | null = null;
    const poll = async () => {
      controller = new AbortController();
      try {
        const statuses = await getAccountRuntimeStatuses({ signal: controller.signal, timeoutMs: 10_000 });
        if (!disposed) {
          setAccounts(current => current.map(account => ({
            ...account,
            runtime_state: statuses[account.id]?.state || account.runtime_state,
            runtime_connected: statuses[account.id]?.connected ?? account.runtime_connected,
          })));
        }
      } catch {
        // The app WebSocket has its own visible state; a transient status poll
        // failure does not invalidate the loaded conversations.
      } finally {
        if (!disposed) timer = window.setTimeout(poll, 3_000);
      }
    };
    timer = window.setTimeout(poll, 3_000);
    return () => {
      disposed = true;
      window.clearTimeout(timer);
      controller?.abort();
    };
  }, []);

  useEffect(() => {
    if (!activeAccountID) return;
    window.localStorage.setItem(accountStorageKey, activeAccountID);
    const sessions = sessionsByAccount[activeAccountID] || [];
    setActiveChatID(current => sessions.some(session => session.chat_id === current) ? current : sessions[0]?.chat_id || '');
  }, [activeAccountID, sessionsByAccount]);

  useEffect(() => {
    if (!activeAccountID || refreshedAccountsRef.current.has(activeAccountID)) return;
    refreshedAccountsRef.current.add(activeAccountID);
    void reloadSessions(activeAccountID).catch(loadError => {
      setError(loadError instanceof Error ? loadError.message : '同步会话失败');
    });
  }, [activeAccountID]);

  useEffect(() => {
    if (!activeAccountID || !activeChatID) {
      setMessages([]);
      return;
    }
    const controller = new AbortController();
    setSessionsByAccount(current => ({
      ...current,
      [activeAccountID]: (current[activeAccountID] || []).map(session =>
        session.chat_id === activeChatID ? { ...session, unread_count: 0 } : session),
    }));
    setMessagesLoading(true);
    void getChatMessagePage(activeAccountID, activeChatID, undefined, undefined, { signal: controller.signal }).then(page => {
      const readMessages = page.messages
        .filter(message => message.direction === 'incoming' && message.message_type !== 'system' && !message.message_key.startsWith('in-'))
        .map(message => ({ messageId: message.message_key, sessionId: activeChatID, cid: `${activeChatID}@goofish`, conversationType: 1 } as any));
      console.info('[聊天已读] 打开会话上报', { accountID: activeAccountID, chatID: activeChatID, messageCount: readMessages.length, messageIds: readMessages.map(item => item.messageId) });
      void markChatRead(activeAccountID, activeChatID, readMessages);
      setMessages(page.messages);
      setHasOlder(page.has_more);
      setHistoryCursor(page.next_cursor);
      if (page.session) {
        setSessionsByAccount(current => ({ ...current, [activeAccountID]: (current[activeAccountID] || []).map(session => session.chat_id === page.session?.chat_id ? page.session! : session) }));
      }
      setSessionsByAccount(current => ({
        ...current,
        [activeAccountID]: (current[activeAccountID] || []).map(session =>
          session.chat_id === activeChatID ? { ...session, unread_count: 0 } : session),
      }));
    }).catch(loadError => {
      if (!controller.signal.aborted) setError(loadError instanceof Error ? loadError.message : '加载消息失败');
    }).finally(() => {
      if (!controller.signal.aborted) setMessagesLoading(false);
    });
    return () => controller.abort();
  }, [activeAccountID, activeChatID]);

  const loadOlderMessages = async () => {
    if (!activeAccountID || !activeChatID || olderLoading || !hasOlder) return;
    const container = scrollRef.current;
    const previousHeight = container?.scrollHeight || 0;
    skipNextMessageScrollRef.current = true;
    setOlderLoading(true);
    setError('');
    try {
      const oldestID = messages[0]?.id;
      const page = await getChatMessagePage(activeAccountID, activeChatID, historyCursor, oldestID);
      setMessages(current => {
        const keys = new Set(current.map(message => message.message_key));
        return [...page.messages.filter(message => !keys.has(message.message_key)), ...current];
      });
      setHasOlder(page.has_more);
      setHistoryCursor(page.next_cursor);
      requestAnimationFrame(() => {
        if (container) container.scrollTop += container.scrollHeight - previousHeight;
      });
    } catch (loadError) {
      skipNextMessageScrollRef.current = false;
      setError(loadError instanceof Error ? loadError.message : '加载历史消息失败');
    } finally {
      setOlderLoading(false);
    }
  };

  useEffect(() => {
    let disposed = false;
    let reconnectTimer = 0;
    let retry = 0;
    let socket: WebSocket | null = null;
    const connect = () => {
      if (disposed) return;
      setLiveState('connecting');
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      socket = new WebSocket(`${protocol}//${window.location.host}/api/chat/ws`);
      socket.onopen = () => { retry = 0; setLiveState('online'); };
      socket.onmessage = event => {
        try {
          const payload = JSON.parse(event.data);
          const message = payload.message as ChatMessage | undefined;
          if (!message) return;
          const accountID = message.account_id;
          setSessionsByAccount(current => {
            const rows = current[accountID] || [];
            const found = rows.some(row => row.chat_id === message.chat_id);
            if (!found) {
              void reloadSessions(accountID);
              return current;
            }
            return {
              ...current,
              [accountID]: rows.map(row => row.chat_id === message.chat_id ? {
                ...row,
                last_message: message.content,
                last_message_at: message.sent_at,
                unread_count: message.direction === 'incoming' && message.message_type !== 'system' && (activeAccountRef.current !== accountID || activeChatRef.current !== message.chat_id)
                  ? row.unread_count + 1 : row.unread_count,
              } : row).sort((a, b) => b.last_message_at - a.last_message_at),
            };
          });
          if (activeAccountRef.current === accountID && activeChatRef.current === message.chat_id) {
            setMessages(current => {
              const index = current.findIndex(item => item.message_key === message.message_key);
              if (index >= 0) return current.map((item, i) => i === index ? message : item);
              return [...current, message];
            });
            if (message.direction === 'incoming' && message.message_type !== 'system') {
              const readMessage = [{ messageId: message.message_key, sessionId: message.chat_id, cid: `${message.chat_id}@goofish`, conversationType: 1 } as any];
              console.info('[聊天已读] 收到实时消息上报', { accountID, chatID: message.chat_id, messageKey: message.message_key, messageType: message.message_type });
              void markChatRead(accountID, message.chat_id, readMessage);
            }
          }
        } catch {
          // Ignore malformed non-chat frames and recover from authoritative REST state.
        }
      };
      socket.onclose = () => {
        if (disposed) return;
        setLiveState('offline');
        const delay = Math.min(15_000, 1_000 * 2 ** Math.min(retry++, 4));
        reconnectTimer = window.setTimeout(connect, delay);
      };
      socket.onerror = () => socket?.close();
    };
    connect();
    return () => {
      disposed = true;
      window.clearTimeout(reconnectTimer);
      socket?.close();
    };
  }, []);

  const handleMessageScroll = () => {
    const container = scrollRef.current;
    if (!container) return;
    const distanceFromBottom = container.scrollHeight - container.scrollTop - container.clientHeight;
    shouldScrollToBottomRef.current = distanceFromBottom <= 48;
  };

  useLayoutEffect(() => {
    const contextChanged = scrollContextRef.current.accountID !== activeAccountID
      || scrollContextRef.current.chatID !== activeChatID;
    scrollContextRef.current = { accountID: activeAccountID, chatID: activeChatID };
    if (contextChanged) shouldScrollToBottomRef.current = true;

    const container = scrollRef.current;
    if (!container) return;

    if (skipNextMessageScrollRef.current) {
      skipNextMessageScrollRef.current = false;
      return;
    }
    if (messagesLoading || shouldScrollToBottomRef.current) container.scrollTop = container.scrollHeight;
  }, [activeAccountID, activeChatID, messages, messagesLoading]);

  const activeAccount = accounts.find(account => account.id === activeAccountID);
  const activeSessions = sessionsByAccount[activeAccountID] || [];
  const selectedSession = activeSessions.find(session => session.chat_id === activeChatID);
  const imageMessages = useMemo(() =>
    messages.filter(message => message.message_type === 'image'),
    [messages]);
  const imageSlides = useMemo(() =>
    imageMessages.map(message => ({ src: message.content, alt: '聊天图片' })),
    [imageMessages]);
  const filteredSessions = useMemo(() => {
    const keyword = search.trim().toLowerCase();
    return activeSessions.filter(session => {
      if (unreadOnly && session.unread_count <= 0) return false;
      if (!keyword) return true;
      return [session.buyer_name, session.buyer_id, session.item_title, session.last_message]
        .some(value => (value || '').toLowerCase().includes(keyword));
    });
  }, [activeSessions, search, unreadOnly]);

  const unreadForAccount = (accountID: string) =>
    (sessionsByAccount[accountID] || []).reduce((sum, session) => sum + session.unread_count, 0);

  const loadMoreContacts = async () => {
    if (!activeAccountID || contactsLoading || !hasMoreContacts[activeAccountID]) return;
    setContactsLoading(true);
    setError('');
    try {
      const page = await getChatSessionPage(activeAccountID, contactCursors[activeAccountID], undefined, true);
      setSessionsByAccount(current => ({ ...current, [activeAccountID]: page.sessions }));
      setContactCursors(current => ({ ...current, [activeAccountID]: page.next_cursor }));
      setHasMoreContacts(current => ({ ...current, [activeAccountID]: page.has_more }));
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : '加载历史联系人失败');
    } finally { setContactsLoading(false); }
  };

  useEffect(() => {
    const preview = pendingImage;
    return () => { if (preview) URL.revokeObjectURL(preview.url); };
  }, [pendingImage]);

  const handleSend = async () => {
    const text = draft.trim();
    if (!text || !selectedSession || !activeAccountID || sending) return;
    setSending(true);
    setError('');
    try {
      const result = await sendChatMessage({
        account_id: activeAccountID, chat_id: selectedSession.chat_id, buyer_id: selectedSession.buyer_id,
        buyer_name: selectedSession.buyer_name, item_id: selectedSession.item_id,
        item_title: selectedSession.item_title, text,
      });
      setDraft('');
      setMessages(current => current.some(item => item.message_key === result.message.message_key)
        ? current.map(item => item.message_key === result.message.message_key ? result.message : item)
        : [...current, result.message]);
    } catch (sendError) {
      setError(sendError instanceof Error ? sendError.message : '消息发送失败');
      void getChatMessages(activeAccountID, selectedSession.chat_id).then(setMessages);
    } finally {
      setSending(false);
    }
  };

  // 选择/粘贴图片后先弹窗预览，确认后才真正发送。
  const previewImage = (file?: File) => {
    if (!file || !selectedSession || !activeAccountID || sending) return;
    if (!file.type.startsWith('image/')) {
      setError('仅支持粘贴/发送图片文件');
      return;
    }
    setError('');
    setPendingImage({ file, url: URL.createObjectURL(file) });
  };

  // 关闭预览：取消后清空文件输入框，保证同一文件可再次触发 onChange 重新预览。
  const closePreview = () => {
    setPendingImage(null);
    if (imageInputRef.current) imageInputRef.current.value = '';
  };

  const confirmSendImage = async () => {
    if (!pendingImage || !selectedSession || !activeAccountID || sending) return;
    const file = pendingImage.file;
    setPendingImage(null);
    setSending(true);
    setError('');
    try {
      const result = await sendChatImage({
        account_id: activeAccountID, chat_id: selectedSession.chat_id, buyer_id: selectedSession.buyer_id,
        buyer_name: selectedSession.buyer_name, buyer_avatar_url: selectedSession.buyer_avatar_url,
        item_id: selectedSession.item_id, item_title: selectedSession.item_title, image: file,
      });
      setMessages(current => current.some(item => item.message_key === result.message.message_key)
        ? current.map(item => item.message_key === result.message.message_key ? result.message : item)
        : [...current, result.message]);
    } catch (sendError) {
      setError(sendError instanceof Error ? sendError.message : '图片发送失败');
    } finally {
      setSending(false);
      if (imageInputRef.current) imageInputRef.current.value = '';
    }
  };

  if (loading) return <div className="flex h-[calc(100vh-4rem)] items-center justify-center"><Loader2 className="h-8 w-8 animate-spin text-sky-500" /></div>;

  return (
    <section className="flex h-[calc(100vh-4rem)] min-h-[560px] flex-col overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-chat">
      <header className="border-b border-slate-200 bg-slate-50/70 px-5 pt-4">
        <div className="mb-3 flex items-center justify-between gap-4">
          <div>
            <h2 className="text-xl font-black tracking-tight text-slate-950">在线聊天</h2>
            <p className="mt-0.5 text-xs font-medium text-slate-500">复用账号实时连接，消息按账号完全隔离</p>
          </div>
          <div className={`flex items-center gap-2 rounded-full px-3 py-1.5 text-xs font-bold ${liveState === 'online' ? 'bg-emerald-50 text-emerald-700' : liveState === 'connecting' ? 'bg-amber-50 text-amber-700' : 'bg-red-50 text-red-700'}`}>
            {liveState === 'online' ? <Wifi className="h-3.5 w-3.5" /> : <WifiOff className="h-3.5 w-3.5" />}
            {liveState === 'online' ? '实时同步中' : liveState === 'connecting' ? '正在连接' : '连接已断开'}
          </div>
        </div>
        <div className="flex gap-1 overflow-x-auto pb-0" role="tablist" aria-label="聊天账号">
          {accounts.map(account => {
            const active = account.id === activeAccountID;
            const unread = unreadForAccount(account.id);
            const online = account.runtime_state === 'online';
            return (
              <button key={account.id} type="button" role="tab" aria-selected={active} onClick={() => setActiveAccountID(account.id)}
                className={`relative flex h-11 shrink-0 items-center gap-2 border-b-2 px-3 text-sm font-extrabold transition-colors ${active ? 'border-sky-500 text-sky-700' : 'border-transparent text-slate-500 hover:text-slate-900'}`}>
                <span className={`h-2 w-2 rounded-full ${online ? 'bg-emerald-500' : 'bg-slate-300'}`} />
                <span className="max-w-36 truncate">{account.nickname || account.remark || account.id}</span>
                <UnreadBadge count={unread} />
              </button>
            );
          })}
        </div>
      </header>

      {accounts.length === 0 ? (
        <div className="flex flex-1 flex-col items-center justify-center text-center">
          <MessageCircleMore className="h-12 w-12 text-slate-300" />
          <h3 className="mt-4 font-black text-slate-800">暂无启用账号</h3>
          <p className="mt-1 text-sm text-slate-500">先在账号管理中启用账号，聊天会话会自动出现。</p>
        </div>
      ) : (
        <div className="grid min-h-0 flex-1 overflow-hidden grid-cols-[320px_minmax(0,1fr)]">
          <aside className="flex min-h-0 flex-col border-r border-slate-200 bg-slate-50/40">
            <div className="space-y-3 border-b border-slate-200 p-3">
              <div className="relative">
                <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
                <input value={search} onChange={event => setSearch(event.target.value)} placeholder="搜索用户、商品或消息"
                  className="h-10 w-full rounded-xl border border-slate-200 bg-white pl-9 pr-3 text-sm outline-none transition focus:border-sky-400 focus:ring-2 focus:ring-sky-100" />
              </div>
              <div className="flex items-center justify-between">
                <button type="button" onClick={() => setUnreadOnly(value => !value)}
                  className={`rounded-lg px-2.5 py-1.5 text-xs font-bold ${unreadOnly ? 'bg-sky-100 text-sky-700' : 'text-slate-500 hover:bg-slate-100'}`}>
                  {unreadOnly ? '只看未读' : '全部会话'}
                </button>
                <button type="button" title="刷新会话" onClick={() => void reloadSessions(activeAccountID)} className="rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700">
                  <RefreshCw className="h-4 w-4" />
                </button>
              </div>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto">
              {filteredSessions.map(session => (
                <button key={session.chat_id} type="button" onClick={() => setActiveChatID(session.chat_id)}
                  className={`flex w-full gap-3 border-b border-slate-100 p-4 text-left transition-colors ${session.chat_id === activeChatID ? 'bg-white shadow-chat-active' : 'hover:bg-white/80'}`}>
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center overflow-hidden rounded-full bg-slate-200 text-slate-500">
                    {session.buyer_avatar_url ? <img src={session.buyer_avatar_url} alt="" className="h-full w-full object-cover" /> : <UserRound className="h-5 w-5" />}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-sm font-extrabold text-slate-900">{session.buyer_name || `用户 ${session.buyer_id}`}</span>
                      <span className="ml-auto shrink-0 text-[10px] font-medium text-slate-400">{formatClock(session.last_message_at)}</span>
                    </div>
                    <div className="mt-1 flex items-center gap-2">
                      <span className="truncate text-xs text-slate-500">{session.last_message || '暂无消息'}</span>
                      <UnreadBadge count={session.unread_count} className="ml-auto" />
                    </div>
                    {session.item_title && <div className="mt-1.5 truncate text-[10px] font-medium text-sky-700">商品 · {session.item_title}</div>}
                  </div>
                </button>
              ))}
              {filteredSessions.length === 0 && <div className="px-6 py-16 text-center text-sm text-slate-400">当前账号暂无匹配会话</div>}
              {hasMoreContacts[activeAccountID] && !search && !unreadOnly && <div className="flex justify-center p-4">
                <button type="button" onClick={() => void loadMoreContacts()} disabled={contactsLoading}
                  className="flex items-center gap-2 rounded-full border border-slate-200 bg-white px-4 py-2 text-xs font-bold text-slate-500 shadow-sm hover:border-sky-200 hover:text-sky-600 disabled:opacity-50">
                  {contactsLoading && <Loader2 className="h-3.5 w-3.5 animate-spin" />}{contactsLoading ? '正在加载' : '加载更多历史联系人'}
                </button>
              </div>}
            </div>
          </aside>

          <main className="flex min-h-0 min-w-0 flex-col overflow-hidden bg-surface-subtle">
            {selectedSession ? (
              <>
                <div className="flex h-16 shrink-0 items-center border-b border-slate-200 bg-white px-5">
                  <div className="min-w-0">
                    <div className="truncate text-sm font-black text-slate-950">{selectedSession.buyer_name || selectedSession.buyer_id}</div>
                    <div className="mt-0.5 truncate text-xs text-slate-500">用户 ID：{selectedSession.buyer_id}</div>
                  </div>
                  <span className={`ml-auto rounded-full px-2.5 py-1 text-[10px] font-bold ${activeAccount?.runtime_state === 'online' ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>
                    {activeAccount?.runtime_state === 'online' ? '账号在线' : '账号离线'}
                  </span>
                </div>
                <div ref={scrollRef} onScroll={handleMessageScroll} className="min-h-0 flex-1 space-y-5 overflow-y-auto px-6 py-5">
                  {messagesLoading ? <div className="flex justify-center py-12"><Loader2 className="h-6 w-6 animate-spin text-sky-500" /></div> : <>
                    {hasOlder && <div className="flex justify-center pb-1">
                      <button type="button" onClick={() => void loadOlderMessages()} disabled={olderLoading}
                        className="flex items-center gap-2 rounded-full border border-slate-200 bg-white px-4 py-1.5 text-xs font-bold text-slate-500 shadow-sm transition hover:border-sky-200 hover:text-sky-600 disabled:opacity-50">
                        {olderLoading && <Loader2 className="h-3.5 w-3.5 animate-spin" />}{olderLoading ? '正在加载' : '加载更早消息'}
                      </button>
                    </div>}
                    {messages.map(message => {
                    const outgoing = message.direction === 'outgoing';
                    const system = message.message_type === 'system';
                    if (system) {
                      return (
                        <div key={message.message_key} className="flex justify-center py-1">
                          <div className="max-w-[82%] rounded-xl border border-slate-200 bg-slate-100 px-4 py-2 text-center text-xs leading-5 text-slate-500">
                            {renderXianyuText(message.content)}
                            <div className="mt-1 text-[10px] text-slate-400">{messageTime(message.sent_at)}</div>
                          </div>
                        </div>
                      );
                    }
                    return (
                      <div key={message.message_key} className={`flex items-end gap-2.5 ${outgoing ? 'justify-end' : 'justify-start'}`}>
                        {!outgoing && <div className="h-9 w-9 shrink-0 overflow-hidden rounded-full bg-slate-200 ring-2 ring-white">
                          {selectedSession.buyer_avatar_url ? <img src={selectedSession.buyer_avatar_url} alt={selectedSession.buyer_name || '用户'} className="h-full w-full object-cover" /> : <UserRound className="m-2 h-5 w-5 text-slate-500" />}
                        </div>}
                        <div className={`max-w-[72%] ${outgoing ? 'items-end' : 'items-start'} flex flex-col`}>
                          <div className={`mb-1 px-1 text-[10px] font-semibold text-slate-400`}>{outgoing ? (activeAccount?.nickname || activeAccount?.remark || '我') : (selectedSession.buyer_name || message.sender_name || selectedSession.buyer_id)}</div>
                          {message.message_type === 'image' ? (
                            <button type="button" onClick={() => setLightboxIndex(imageMessages.findIndex(item => item.message_key === message.message_key))} title="点击预览大图"
                              className={`block cursor-zoom-in overflow-hidden rounded-2xl border bg-white p-1 text-left shadow-sm ${outgoing ? 'rounded-br-md border-sky-200' : 'rounded-bl-md border-slate-200'}`}>
                              <img src={message.content} alt="聊天图片" className="max-h-80 max-w-full rounded-xl object-contain" />
                            </button>
                          ) : message.message_type === 'video' ? (
                            <video src={message.content} controls preload="metadata" className="max-h-80 max-w-full rounded-2xl bg-black" />
                          ) : (
                            <div className={`rounded-2xl px-4 py-2.5 text-sm leading-6 shadow-sm ${outgoing ? 'rounded-br-md bg-sky-500 text-white' : 'rounded-bl-md border border-slate-200 bg-white text-slate-800'}`}>{renderXianyuText(message.content)}</div>
                          )}
                          <div className="mt-1 flex items-center gap-1 text-[10px] text-slate-400">
                            {messageTime(message.sent_at)}
                            {outgoing && (message.status === 'failed' ? <AlertCircle className="h-3 w-3 text-red-500" /> : message.read_status === 2 ? <CheckCheck className="h-3 w-3 text-sky-500" /> : message.status === 'sent' ? <Check className="h-3 w-3 text-sky-500" /> : <Check className="h-3 w-3" />)}
                          </div>
                        </div>
                        {outgoing && <div className="h-9 w-9 shrink-0 overflow-hidden rounded-full bg-sky-100 ring-2 ring-white">
                          {activeAccount?.avatar_url ? <img src={activeAccount.avatar_url} alt="我" className="h-full w-full object-cover" /> : <UserRound className="m-2 h-5 w-5 text-sky-600" />}
                        </div>}
                      </div>
                    );
                    })}
                  </>}
                </div>
                {error && <div className="border-t border-red-100 bg-red-50 px-5 py-2 text-xs font-medium text-red-700">{error}</div>}
                <div className="relative z-10 shrink-0 border-t border-slate-200 bg-white p-4 shadow-chat-input">
                  <div className="mb-2 flex items-center gap-1">
                    <div className="relative">
                      <button type="button" onClick={() => setEmojiOpen(value => !value)} disabled={sending || activeAccount?.runtime_state !== 'online'} className="rounded-lg p-2 text-slate-500 hover:bg-sky-50 hover:text-sky-600 disabled:opacity-40" title="闲鱼表情"><Smile className="h-5 w-5" /></button>
                      {emojiOpen && <div className="absolute bottom-11 left-0 z-30 w-[360px] rounded-2xl border border-slate-200 bg-white p-3 shadow-2xl">
                        <div className="mb-2 text-xs font-bold text-slate-500">全部表情</div>
                        <div className="grid max-h-72 grid-cols-8 gap-1 overflow-y-auto">
                          {xianyuEmojis.map(([name,file]) => <button key={name} type="button" title={`[${name}]`} onClick={() => { setDraft(value => value + `[${name}]`); setEmojiOpen(false); }} className="flex h-10 w-10 items-center justify-center rounded-lg hover:bg-slate-100"><img src={emojiURL(file)} alt={`[${name}]`} className="h-8 w-8 object-contain" /></button>)}
                        </div>
                      </div>}
                    </div>
                    <input ref={imageInputRef} type="file" accept="image/*" className="hidden" onChange={event => previewImage(event.target.files?.[0])} />
                    <button type="button" onClick={() => imageInputRef.current?.click()} disabled={sending || activeAccount?.runtime_state !== 'online'}
                      className="rounded-lg p-2 text-slate-500 transition hover:bg-sky-50 hover:text-sky-600 disabled:opacity-40" title="发送图片（最大 10MB）"><ImagePlus className="h-5 w-5" /></button>
                  </div>
                  <div className="flex items-end gap-3 rounded-2xl border border-slate-200 bg-slate-50 p-2 transition focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100">
                    <textarea value={draft} onChange={event => setDraft(event.target.value)} rows={2} maxLength={2000}
                      onKeyDown={event => { if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); void handleSend(); } }}
                      onPaste={event => {
                        const files = Array.from(event.clipboardData?.files || []);
                        const image = files.find(file => file.type.startsWith('image/'));
                        if (image) {
                          event.preventDefault();
                          previewImage(image);
                        }
                      }}
                      disabled={activeAccount?.runtime_state !== 'online'} placeholder={activeAccount?.runtime_state === 'online' ? '输入消息，Enter 发送，Shift + Enter 换行，Ctrl + V 粘贴图片' : '账号离线，暂时无法发送'}
                      className="max-h-32 min-h-12 flex-1 resize-none bg-transparent px-2 py-2 text-sm leading-6 outline-none disabled:cursor-not-allowed" />
                    <button type="button" onClick={() => void handleSend()} disabled={!draft.trim() || sending || activeAccount?.runtime_state !== 'online'}
                      className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-sky-500 text-white shadow-md shadow-sky-100 transition hover:bg-sky-600 disabled:cursor-not-allowed disabled:bg-slate-300 disabled:shadow-none" aria-label="发送消息">
                      {sending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
                    </button>
                  </div>
                </div>
              </>
            ) : (
              <div className="flex flex-1 flex-col items-center justify-center text-center">
                <MessageCircleMore className="h-12 w-12 text-slate-300" />
                <h3 className="mt-4 font-black text-slate-700">选择一个用户开始聊天</h3>
                <p className="mt-1 text-sm text-slate-400">该账号的新消息会实时出现在左侧列表。</p>
              </div>
            )}
          </main>
        </div>
      )}

      {pendingImage && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/60 p-4" onClick={closePreview}>
          <div className="flex max-h-full w-full max-w-2xl flex-col overflow-hidden rounded-2xl bg-white shadow-2xl" onClick={event => event.stopPropagation()}>
            <div className="flex shrink-0 items-center justify-between border-b border-slate-200 px-5 py-3">
              <div className="text-sm font-black text-slate-900">发送图片预览</div>
              <button type="button" onClick={closePreview} className="rounded-lg p-1.5 text-slate-400 transition hover:bg-slate-100 hover:text-slate-700" title="取消">
                <X className="h-5 w-5" />
              </button>
            </div>
            <div className="flex min-h-0 flex-1 items-center justify-center overflow-hidden bg-slate-100 p-4">
              <img src={pendingImage.url} alt="待发送图片预览" className="max-h-[55vh] max-w-full rounded-xl object-contain" />
            </div>
            <div className="flex shrink-0 items-center justify-between gap-3 border-t border-slate-200 px-5 py-3">
              <div className="min-w-0 truncate text-xs text-slate-500">{pendingImage.file.name || '粘贴的图片'} · {(pendingImage.file.size / 1024).toFixed(0)} KB</div>
              <div className="flex shrink-0 items-center gap-2">
                <button type="button" onClick={closePreview} className="rounded-xl px-4 py-2 text-sm font-bold text-slate-600 transition hover:bg-slate-100">取消</button>
                <button type="button" onClick={() => void confirmSendImage()} disabled={sending || activeAccount?.runtime_state !== 'online'}
                  className="flex items-center gap-2 rounded-xl bg-sky-500 px-4 py-2 text-sm font-bold text-white shadow-md shadow-sky-100 transition hover:bg-sky-600 disabled:cursor-not-allowed disabled:opacity-50">
                  {sending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}发送
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      <Lightbox
        open={lightboxIndex >= 0}
        index={Math.max(lightboxIndex, 0)}
        close={() => setLightboxIndex(-1)}
        slides={imageSlides}
      />
    </section>
  );
};

export default Chat;
