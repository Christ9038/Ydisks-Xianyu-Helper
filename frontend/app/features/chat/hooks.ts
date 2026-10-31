import type React from 'react';
import { useCallback,useEffect,useLayoutEffect,useMemo,useRef,useState } from 'react';
import { emojiURL,renderXianyuText,xianyuEmojis } from '../../../chatEmojis';
import type { AccountDetail,ChatMessage,ChatSession } from './api';
import { confirmedOutgoingMessageFromError,deleteChatSession,getAccountDetails,getAccountRuntimeStatuses,getChatMessagePage,getChatSessionPage,markChatRead,sendChatImage,sendChatLibraryImage,sendChatMessage,uncertainOutgoingMessageFromError } from './api';
import { publishChatUnreadStatus,subscribeToChatLiveEvents } from './liveEvents';
import { collectChatReadReceipts,filterChatSessions,formatClock,isChatAbortError,isCurrentChatRequest,markOutgoingMessagesReadByIncoming,mergeChatSessions,mergeLiveMessage,mergeOlderMessages,messageTime } from './state';
import type { ChatFeatureState,ChatLiveState,SessionsByAccount } from './types';
import { libraryImageContentUrl,type LibraryImageChoice } from '../../../shared/ui/imageSource';

/** ChatImageSource 是一次图片发送的来源：本地文件，或图片库图片（由服务端读取字节）。 */
type ChatImageSource = File | {
  /** libraryImageId 是待发送的图片库图片主键，字节由服务端读取。 */
  libraryImageId: number;
};

/** PendingImagePreview 描述等待用户确认的本地图片预览及其资源所有权。 */
type PendingImagePreview = {
  /** file 保存当前预览会话待发送的本地图片文件；选择图片库图片时为空。 */
  file?: File;
  /** libraryImage 保存待发送的图片库图片；选择本地文件时为空。 */
  libraryImage?: LibraryImageChoice;
  /** url 保存预览地址：本地文件为浏览器创建的临时地址，图片库图片为内容接口地址。 */
  url: string;
};

/** Chat Hook 对外暴露的状态、引用和交互动作。 */
export type UseChatResult = ChatFeatureState & {
  /** 待确认发送的图片预览；URL 仅在当前预览会话内有效。 */
  pendingImage: PendingImagePreview | null;
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
  /** 发送快捷回复且保留正在编辑的普通消息草稿。 */
  handleQuickReply: (content: string) => Promise<void>;
  /** 发送图片消息。 */
  handleImage: (file?: File) => Promise<void>;
  /** 从剪贴板候选文件中选择首张图片进入预览。 */
  handlePastedImages: (files: File[]) => Promise<void>;
  /** 选择图片库图片并进入与本地图片相同的确认预览。 */
  handleLibraryImage: (image: LibraryImageChoice) => void;
  /** 直接发送图片快捷回复引用的图片库图片，沿用文字快捷回复的发送保护。 */
  handleQuickReplyImage: (libraryImageId: number) => Promise<void>;
  /** 确认发送当前预览图片。 */
  confirmSendImage: () => Promise<void>;
  /** 取消当前图片预览并释放临时地址。 */
  closeImagePreview: () => void;
  /** 重试最近一次失败发送。 */
  retrySend: () => Promise<void>;
  /** 是否存在可重试的发送动作。 */
  retryAvailable: boolean;
	/** 当前正在删除的会话 ID；空字符串表示没有删除请求。 */
	deletingChatID: string;
	/** 最近一次会话删除失败的可重试错误。 */
	deleteError: string;
	/** 在本机隐藏并清空指定会话，成功时返回 true。 */
	deleteConversation: (accountID: string, chatID: string) => Promise<boolean>;
	/** 清除会话删除弹窗内的旧错误。 */
	clearDeleteError: () => void;
	/** 将独立发送流程返回的出站消息幂等合并到当前聊天。 */
	acceptOutgoingMessage: (message: ChatMessage, notice?: string) => void;
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
  // pendingImage 保存等待用户确认的本地图片和临时预览地址。
  const [pendingImage, setPendingImage] = useState<PendingImagePreview | null>(null);
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
  // storedContactCursors 保存各账号本地联系人键集分页游标。
  const [storedContactCursors, setStoredContactCursors] = useState<Record<string, string | undefined>>({});
  // platformHasMoreContacts 保存各账号平台联系人分页是否仍有下一页。
  const [platformHasMoreContacts, setPlatformHasMoreContacts] = useState<Record<string, boolean>>({});
  // storedHasMoreContacts 保存各账号本地缓存联系人分页是否仍有下一页。
  const [storedHasMoreContacts, setStoredHasMoreContacts] = useState<Record<string, boolean>>({});
  // contactsLoading 表示联系人分页状态。
  const [contactsLoading, setContactsLoading] = useState(false);
  // emojiOpen 控制表情选择器显示。
  const [emojiOpen, setEmojiOpen] = useState(false);
  // sending 表示当前是否正在发送消息。
  const [sending, setSending] = useState(false);
  // error 保存聊天页面最近错误。
  const [error, setError] = useState('');
  // sendNotice 保存远端已发送但本地状态收口失败时不可重试的黄色提示。
  const [sendNotice, setSendNotice] = useState('');
	// deletingChatID 保存当前删除请求对应的会话 ID，供确认框锁定重复提交。
	const [deletingChatID, setDeletingChatID] = useState('');
	// deleteError 保存会话删除失败原因，与聊天加载和发送错误分开展示。
	const [deleteError, setDeleteError] = useState('');
  // liveState 保存 WebSocket 连接状态。
  const [liveState, setLiveState] = useState<ChatLiveState>('connecting');
  // retryText 保存最近失败的文本消息。
  const [retryText, setRetryText] = useState<string | null>(null);
  // retryImage 保存最近失败的图片消息。
  const [retryImage, setRetryImage] = useState<ChatImageSource | null>(null);
  // activeAccountRef 供实时回调读取最新账号。
  const activeAccountRef = useRef('');
  // activeChatRef 供实时回调读取最新会话。
  const activeChatRef = useRef('');
  // sessionsByAccountRef 保存最近一次已提交的本地会话快照，实时消息只用它判断是否允许平台补齐。
  const sessionsByAccountRef = useRef<SessionsByAccount>({});
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
  // olderSequence 隔离同一会话中被关闭、切换或替换的历史消息分页请求。
  const olderSequence = useRef(0);
  // olderController 保存当前历史消息分页请求，切换会话或卸载时必须由本 Hook 取消。
  const olderController = useRef<AbortController | null>(null);
  // contactSequence 隔离联系人分页产生的旧响应。
  const contactSequence = useRef(0);
  // contactController 保存当前联系人分页控制器。
  const contactController = useRef<AbortController | null>(null);
  // sendSequence 隔离账号或会话切换产生的旧发送响应。
  const sendSequence = useRef(0);
  // sendController 保存当前消息发送控制器。
  const sendController = useRef<AbortController | null>(null);
	// deleteSequence 隔离账号切换、重复提交和卸载后的过期删除响应。
	const deleteSequence = useRef(0);
  // sessionReloadingRef 在首页替换期间阻止旧游标分页，只有最新首页请求可以解除。
  const sessionReloadingRef = useRef(false);
  // deleteController 保存当前会话删除请求控制器，生命周期由本 Hook 独占。
  const deleteController = useRef<AbortController | null>(null);
	// suppressAutoSelectAccountRef 保存删除当前会话后禁止自动选择的账号，直到用户主动选择新会话。
	const suppressAutoSelectAccountRef = useRef('');
	// deletingAccountIDRef 保存正在删除的账号，供实时事件同步判断删除隔离边界。
	const deletingAccountIDRef = useRef('');
	// deletingSessionIDRef 保存正在删除的会话，目标实时事件在删除收口前不得触发平台刷新。
	const deletingSessionIDRef = useRef('');
	// deletionNeedsLocalReloadRef 记录删除隔离期间是否收到目标事件，收口后只追加一次本地读取。
	const deletionNeedsLocalReloadRef = useRef(false);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => { activeAccountRef.current = activeAccountID; }, [activeAccountID]);
  useEffect(/* 当前回调同步当前会话并在用户主动选择后解除该账号的自动选择屏蔽。 */ () => {
    activeChatRef.current = activeChatID;
    if (activeChatID && suppressAutoSelectAccountRef.current === activeAccountID) suppressAutoSelectAccountRef.current = '';
  }, [activeAccountID, activeChatID]);
  useLayoutEffect(/* 当前回调在浏览器处理下一条实时事件前同步已经提交的会话列表快照。 */ () => { sessionsByAccountRef.current = sessionsByAccount; }, [sessionsByAccount]);
  useEffect(/* 当前回调在切换账号或会话时清理只针对旧会话的发送状态收口提示。 */ () => { setSendNotice(''); }, [activeAccountID, activeChatID]);

  /** 刷新指定账号的联系人列表，并丢弃过期响应。 */
  const reloadSessions = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (accountID: string): Promise<ChatSession[]> => {
    // sequence 请求序号。
    const sequence = ++sessionSequence.current;
    sessionReloadingRef.current = true;
    contactSequence.current += 1;
    contactController.current?.abort();
    setContactsLoading(false);
    sessionController.current?.abort();
    // controller 请求取消控制器。
    const controller = new AbortController();
    sessionController.current = controller;
    try {
      // page 页码。
      const page = await getChatSessionPage(accountID, undefined, { signal: controller.signal }, true);
      if (!isCurrentChatRequest(sessionSequence.current, sequence, controller.signal)) return [];
      // sessions 保留当前打开会话的本地已读状态，避免刷新响应迟到后重新展示未读徽标。
      const sessions = page.sessions.map(/* session 按当前活跃会话覆写已读计数。 */ session => (
        accountID === activeAccountRef.current && session.chat_id === activeChatRef.current
          ? { ...session, unread_count: 0 }
          : session
      ));
      setSessionsByAccount(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, [accountID]: sessions }));
      setContactCursors(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, [accountID]: page.next_cursor }));
      setStoredContactCursors(/* 当前回调写入本地联系人首页的下一键集游标。 */ current => ({ ...current, [accountID]: page.next_stored_cursor }));
      setPlatformHasMoreContacts(/* 当前回调写入平台联系人是否还有下一页。 */ current => ({ ...current, [accountID]: page.platform_has_more ?? page.has_more }));
      setStoredHasMoreContacts(/* 当前回调写入本地联系人是否还有下一页。 */ current => ({ ...current, [accountID]: page.stored_has_more ?? page.has_more }));
      setSendNotice('');
      return sessions;
    } catch (/* error 保存会话列表请求的失败原因；仅最新请求可以更新错误状态。 */ error) {
      if (isCurrentChatRequest(sessionSequence.current, sequence, controller.signal) && !isChatAbortError(error)) setError(error instanceof Error ? error.message : '同步会话失败');
      return [];
    } finally {
      if (sessionSequence.current === sequence) sessionReloadingRef.current = false;
    }
  }, []);

	/** 只读取本地联系人缓存，供删除收口和管理连接恢复使用；该函数永远不会触发平台同步。 */
	const reloadLocalSessions = useCallback(/* reloadLocalSessions 固定执行本地会话读取，不允许切换为平台刷新。 */ async (accountID: string): Promise<ChatSession[]> => {
		// sequence 隔离本地恢复请求与账号切换或删除产生的旧响应。
		const sequence = ++sessionSequence.current;
    sessionReloadingRef.current = true;
    contactSequence.current += 1;
    contactController.current?.abort();
    setContactsLoading(false);
		sessionController.current?.abort();
		// controller 保存本次本地读取请求的取消边界。
		const controller = new AbortController();
		sessionController.current = controller;
		try {
			// page 保存固定 refresh=false 的本地联系人分页结果。
			const page = await getChatSessionPage(accountID, undefined, { signal: controller.signal }, false);
			if (!isCurrentChatRequest(sessionSequence.current, sequence, controller.signal)) return [];
			// sessions 保存本地会话列表，并保留当前会话已读投影。
			const sessions = page.sessions.map(/* session 保存当前本地恢复遍历的会话。 */ session => accountID === activeAccountRef.current && session.chat_id === activeChatRef.current ? { ...session, unread_count: 0 } : session);
			setSessionsByAccount(/* current 保存本地恢复后的账号会话映射。 */ current => ({ ...current, [accountID]: sessions }));
			setStoredContactCursors(/* current 保存本地联系人下一页游标。 */ current => ({ ...current, [accountID]: page.next_stored_cursor }));
			setStoredHasMoreContacts(/* current 保存本地联系人是否仍有下一页。 */ current => ({ ...current, [accountID]: page.stored_has_more ?? page.has_more }));
			return sessions;
		} catch (/* error 保存本地会话读取失败原因。 */ error) {
			if (isCurrentChatRequest(sessionSequence.current, sequence, controller.signal) && !isChatAbortError(error)) setError(error instanceof Error ? error.message : '读取本地会话失败');
			return [];
		} finally {
      if (sessionSequence.current === sequence) sessionReloadingRef.current = false;
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
        setStoredContactCursors(Object.fromEntries(sessionPages.map(/* 当前回调记录每个账号本地联系人首页的下一游标。 */ ([id, page]) => [id, page.next_stored_cursor])));
        setPlatformHasMoreContacts(Object.fromEntries(sessionPages.map(/* 当前回调记录每个账号平台联系人分页状态。 */ ([id, page]) => [id, page.platform_has_more ?? page.has_more])));
        setStoredHasMoreContacts(Object.fromEntries(sessionPages.map(/* 当前回调记录每个账号本地联系人分页状态。 */ ([id, page]) => [id, page.stored_has_more ?? page.has_more])));
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
    if (suppressAutoSelectAccountRef.current === activeAccountID && !activeChatRef.current) return;
    setActiveChatID(/* 当前回调处理集合中的单个元素。 */ current => sessions.some(/* 当前回调处理集合中的单个元素。 */ session => session.chat_id === current) ? current : sessions[0]?.chat_id || '');
  }, [activeAccountID, sessionsByAccount]);

  useEffect(/* 当前副作用将所有已加载账号会话的未读聚合状态回传给应用壳，红点只能在没有任何未读时消失。 */ () => {
    // loadedAccountCount 保存已成功写入会话列表状态的账号数量；初始请求尚未完成或失败时不得错误发布无未读。
    const loadedAccountCount = Object.keys(sessionsByAccount).length;
    if (loading || (accounts.length > 0 && loadedAccountCount === 0)) return;
    // hasUnreadChatMessage 保存当前所有已加载会话是否至少存在一条未读消息。
    const hasUnreadChatMessage = Object.values(sessionsByAccount).some(/* sessions 保存当前账号的会话列表。 */ sessions => sessions.some(/* session 保存当前参与未读聚合判断的会话。 */ session => session.unread_count > 0));
    publishChatUnreadStatus(hasUnreadChatMessage);
  }, [accounts.length, loading, sessionsByAccount]);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    if (!activeAccountID || refreshedAccountsRef.current.has(activeAccountID)) return;
    refreshedAccountsRef.current.add(activeAccountID);
    void reloadSessions(activeAccountID);
  }, [activeAccountID, reloadSessions]);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    if (!activeAccountID || !activeChatID) {
      // 无有效会话时也推进消息与历史分页代次，避免晚到响应恢复已清空的消息列表。
      messageSequence.current += 1;
      olderSequence.current += 1;
      olderController.current?.abort();
      skipNextMessageScrollRef.current = false;
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
    void getChatMessagePage(activeAccountID, activeChatID, undefined, undefined, { signal: controller.signal }).then(/* 当前回调处理用户交互或异步状态变化。 */ page => {
      if (!isCurrentChatRequest(messageSequence.current, sequence, controller.signal)) return;
      // readReceipts 只确认可被平台接受的普通入站消息。
      const readReceipts = collectChatReadReceipts(page.messages, activeChatID);
      void markChatRead(activeAccountID, activeChatID, readReceipts, { signal: controller.signal });
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
    setContactsLoading(false);
  }, [activeAccountID]);

	useEffect(/* 当前回调在账号切换时取消旧账号的删除请求，防止迟到响应改写新账号列表。 */ () => {
		deleteSequence.current += 1;
		deleteController.current?.abort();
		deletingAccountIDRef.current = '';
		deletingSessionIDRef.current = '';
		deletionNeedsLocalReloadRef.current = false;
		setDeletingChatID('');
		setDeleteError('');
	}, [activeAccountID]);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    sendController.current?.abort();
    sendSequence.current += 1;
  }, [activeAccountID, activeChatID]);

  useEffect(/* 当前回调使会话切换前创建的图片预览失效。 */ () => {
    // 会话或账号切换后不得把旧预览发送到新会话。
    setPendingImage(null);
  }, [activeAccountID, activeChatID]);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    // 会话切换时取消历史分页；新的消息加载拥有独立控制器，不能复用这个低优先级请求。
    olderSequence.current += 1;
    olderController.current?.abort();
    skipNextMessageScrollRef.current = false;
  }, [activeAccountID, activeChatID]);

  useEffect(/* 当前回调同步 React 副作用和资源生命周期。 */ () => /* 当前回调同步 React 副作用和资源生命周期。 */ () => {
    sessionController.current?.abort();
    messageController.current?.abort();
    olderSequence.current += 1;
    olderController.current?.abort();
    contactController.current?.abort();
    sendController.current?.abort();
		deleteSequence.current += 1;
		deleteController.current?.abort();
		deletingAccountIDRef.current = '';
		deletingSessionIDRef.current = '';
		deletionNeedsLocalReloadRef.current = false;
  }, []);

  useEffect(/* 当前回调负责图片预览临时地址的生命周期清理。 */ () => {
    // preview 保存本次渲染仍然拥有的图片预览；状态替换或组件卸载时释放地址。
    const preview = pendingImage;
    return /* 当前回调释放已不再使用的图片对象地址。 */ () => {
      if (preview?.file) URL.revokeObjectURL(preview.url);
    };
  }, [pendingImage]);

  /** 加载当前会话更早消息并保持滚动位置。 */
  const loadOlderMessages = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (): Promise<void> => {
    if (!activeAccountID || !activeChatID || olderLoading || !hasOlder) return;
    // container 容器。
    const container = scrollRef.current;
    // previousHeight 上一项高度，负责当前功能中的对应处理。
    const previousHeight = container?.scrollHeight || 0;
    // sequence 请求序号。
    const sequence = messageSequence.current;
    // olderRequestSequence 标识本次历史分页，连续翻页、切换会话或卸载后旧分页不能写入状态。
    const olderRequestSequence = ++olderSequence.current;
    olderController.current?.abort();
    // controller 取消当前历史消息请求。
    const controller = new AbortController();
    olderController.current = controller;
    skipNextMessageScrollRef.current = true;
    setOlderLoading(true);
    setError('');
    try {
      // oldestID 最早标识，负责当前功能中的对应处理。
      const oldestID = messages[0]?.id;
      // page 页码。
      const page = await getChatMessagePage(activeAccountID, activeChatID, historyCursor, oldestID, { signal: controller.signal });
      if (!isCurrentChatRequest(messageSequence.current, sequence, controller.signal) || olderRequestSequence !== olderSequence.current) return;
      setMessages(/* 当前回调处理用户交互或异步状态变化。 */ current => mergeOlderMessages(current, page.messages));
      setHasOlder(page.has_more);
      setHistoryCursor(page.next_cursor);
      requestAnimationFrame(/* 当前回调处理用户交互或异步状态变化。 */ () => {
        if (olderRequestSequence === olderSequence.current && !controller.signal.aborted && container) {
          container.scrollTop += container.scrollHeight - previousHeight;
        }
      });
    } catch (/* loadError 表示加载错误。 */ loadError) {
      skipNextMessageScrollRef.current = false;
      if (!isChatAbortError(loadError)) setError(loadError instanceof Error ? loadError.message : '加载历史消息失败');
    } finally {
      if (olderRequestSequence === olderSequence.current) setOlderLoading(false);
    }
  }, [activeAccountID, activeChatID, hasOlder, historyCursor, messages, olderLoading]);

  /** handleLiveMessage 将应用壳唯一连接发布的消息同步到当前聊天页的会话、消息和已读状态。 */
  const handleLiveMessage = useCallback(/* 当前回调只处理类型化聊天消息，不解析原始 WebSocket 帧。 */ (message: ChatMessage): void => {
    // accountID 保存当前实时消息所属账号，用于隔离不同闲鱼账号的会话状态。
    const accountID = message.account_id;
    if (deletingAccountIDRef.current === accountID && deletingSessionIDRef.current === message.chat_id) {
      deletionNeedsLocalReloadRef.current = true;
      return;
    }
    // knownInLoadedSessions 从已提交的内存快照判断消息是否属于当前已加载会话，不依赖 React 更新器执行时机。
    const knownInLoadedSessions = (sessionsByAccountRef.current[accountID] || []).some(/* row 保存当前参与同步会话匹配的本地行。 */ row => row.chat_id === message.chat_id);
    setSessionsByAccount(/* 当前回调在对应账号会话列表中合并最新消息与未读计数。 */ current => {
      // rows 保存当前账号已有的会话行。
      const rows = current[accountID] || [];
      // found 表示推送消息是否能匹配当前已加载会话。
      const found = rows.some(/* row 保存当前参与会话匹配的联系人行。 */ row => row.chat_id === message.chat_id);
      if (!found) {
        return current;
      }
      return { ...current, [accountID]: rows.map(/* row 保存当前待合并实时消息的联系人行。 */ row => row.chat_id === message.chat_id ? {
        ...row,
        last_message: message.content,
        last_message_at: message.sent_at,
        unread_count: message.direction === 'incoming' && message.message_type !== 'system' && (activeAccountRef.current !== accountID || activeChatRef.current !== message.chat_id) ? row.unread_count + 1 : row.unread_count,
      } : row).sort(/* a、b 保存参与时间倒序比较的两条会话行。 */ (a, b) => b.last_message_at - a.last_message_at) };
    });
    if (activeAccountRef.current === accountID && activeChatRef.current === message.chat_id) {
      setMessages(/* 当前回调合并实时消息并同步后续入站消息确认的出站已读状态。 */ current => markOutgoingMessagesReadByIncoming(mergeLiveMessage(current, message), message));
      if (message.direction === 'incoming' && message.message_type !== 'system') {
        // readReceipts 为当前实时消息生成平台要求的会话读取回执。
        const readReceipts = collectChatReadReceipts([message], message.chat_id);
        void markChatRead(accountID, message.chat_id, readReceipts);
      }
    }
    if (!knownInLoadedSessions) void reloadSessions(accountID);
  }, [reloadSessions]);

  useEffect(/* 当前副作用订阅认证应用壳唯一连接发布的事件，聊天页卸载时取消订阅而不关闭全局连接。 */ () => {
    /** handleLiveEvent 根据事件类型分别同步连接状态或消息内容。 */
    const handleLiveEvent = (event: Parameters<Parameters<typeof subscribeToChatLiveEvents>[0]>[0]): void => {
      if (event.type === 'connection') {
        setLiveState(event.state);
        return;
      }
      handleLiveMessage(event.message);
    };
    return subscribeToChatLiveEvents(handleLiveEvent);
  }, [handleLiveMessage]);

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
    if (!activeAccountID || sessionReloadingRef.current || deletingAccountIDRef.current === activeAccountID || contactsLoading || !(platformHasMoreContacts[activeAccountID] || storedHasMoreContacts[activeAccountID])) return;
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
      const page = await getChatSessionPage(accountID, platformHasMoreContacts[accountID] ? contactCursors[accountID] : undefined, { signal: controller.signal }, platformHasMoreContacts[accountID] === true, storedContactCursors[accountID]);
      if (!isCurrentChatRequest(contactSequence.current, sequence, controller.signal)) return;
      setSessionsByAccount(/* 当前回调追加键集分页结果并按稳定会话排序去重。 */ current => ({ ...current, [accountID]: mergeChatSessions(current[accountID] || [], page.sessions).map(/* session 对当前打开会话保持本地已读状态。 */ session => (
        accountID === activeAccountRef.current && session.chat_id === activeChatRef.current ? { ...session, unread_count: 0 } : session
      )) }));
      setContactCursors(/* 当前回调处理用户交互或异步状态变化。 */ current => ({ ...current, [accountID]: page.next_cursor }));
      setStoredContactCursors(/* 当前回调写入下一本地联系人页游标。 */ current => ({ ...current, [accountID]: page.next_stored_cursor }));
      setPlatformHasMoreContacts(/* 当前回调写入新的平台联系人分页状态。 */ current => ({ ...current, [accountID]: page.platform_has_more ?? page.has_more }));
      setStoredHasMoreContacts(/* 当前回调写入新的本地联系人分页状态。 */ current => ({ ...current, [accountID]: page.stored_has_more ?? page.has_more }));
    } catch (/* loadError 表示加载错误。 */ loadError) {
      if (isCurrentChatRequest(contactSequence.current, sequence, controller.signal) && !isChatAbortError(loadError)) setError(loadError instanceof Error ? loadError.message : '加载历史联系人失败');
    } finally {
      if (isCurrentChatRequest(contactSequence.current, sequence, controller.signal)) setContactsLoading(false);
    }
  }, [activeAccountID, contactCursors, contactsLoading, platformHasMoreContacts, storedHasMoreContacts, storedContactCursors]);

  /** 发送文本消息并记录失败重试数据。 */
  const sendText = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (text: string, rememberRetry: boolean, clearDraft: boolean): Promise<void> => {
    if (!selectedSession || !activeAccountID || sending) return;
    // sequence 请求序号。
    const sequence = ++sendSequence.current;
    sendController.current?.abort();
    // controller 请求取消控制器。
    const controller = new AbortController();
    sendController.current = controller;
    setSending(true);
    setError('');
    setSendNotice('');
    try {
      // result 处理结果。
      const result = await sendChatMessage({ account_id: activeAccountID, chat_id: selectedSession.chat_id, peer_user_id: selectedSession.peer_user_id, peer_name: selectedSession.peer_name, item_id: selectedSession.item_id, item_title: selectedSession.item_title, text }, { signal: controller.signal });
      if (!isCurrentChatRequest(sendSequence.current, sequence, controller.signal)) return;
      if (clearDraft) setDraft('');
      setRetryText(null);
      setMessages(/* 当前回调处理用户交互或异步状态变化。 */ current => mergeLiveMessage(current, result.message));
    } catch (/* sendError 表示发送错误。 */ sendError) {
      if (isCurrentChatRequest(sendSequence.current, sequence, controller.signal)) {
        // confirmed 保存远端已确认投递、仅本地状态收口失败时返回的不可重试消息。
        const confirmed = confirmedOutgoingMessageFromError(sendError);
		// uncertain 保存未知发送结果错误中的待确认消息。
		const uncertain = uncertainOutgoingMessageFromError(sendError);
		// resolved 保存已确认或待确认的不可普通重试消息。
		const resolved = confirmed || uncertain;
        if (resolved) {
          if (clearDraft) setDraft('');
          setRetryText(null);
          setMessages(/* 当前回调把已确认或待确认的消息合并到当前会话。 */ current => mergeLiveMessage(current, resolved));
          setSendNotice(confirmed ? '消息已发送，但本地状态同步失败，请刷新会话确认状态。' : '发送结果待确认，请先到闲鱼核对，避免重复发送。');
        } else {
          if (rememberRetry) setRetryText(text);
          if (!isChatAbortError(sendError)) setError(sendError instanceof Error ? sendError.message : '消息发送失败');
        }
      }
    } finally {
      if (isCurrentChatRequest(sendSequence.current, sequence, controller.signal)) setSending(false);
    }
  }, [activeAccountID, selectedSession, sending]);

  /** 发送图片消息并记录失败重试数据。 */
  const sendImage = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (source: ChatImageSource, rememberRetry: boolean): Promise<void> => {
    if (!selectedSession || !activeAccountID || sending) return;
    // sequence 请求序号。
    const sequence = ++sendSequence.current;
    sendController.current?.abort();
    // controller 请求取消控制器。
    const controller = new AbortController();
    sendController.current = controller;
    setSending(true);
    setError('');
    setSendNotice('');
    try {
      // result 处理结果。
      // target 保存与图片来源无关的会话定位字段，本地文件与图片库图片共用。
      const target = { account_id: activeAccountID, chat_id: selectedSession.chat_id, peer_user_id: selectedSession.peer_user_id, peer_name: selectedSession.peer_name, peer_avatar_url: selectedSession.peer_avatar_url, item_id: selectedSession.item_id, item_title: selectedSession.item_title };
      // result 保存发送接口返回的出站消息，本地文件与图库图片返回同一形状。
      const result = source instanceof File
        ? await sendChatImage({ ...target, image: source }, { signal: controller.signal })
        : await sendChatLibraryImage({ ...target, library_image_id: source.libraryImageId }, { signal: controller.signal });
      if (!isCurrentChatRequest(sendSequence.current, sequence, controller.signal)) return;
      setRetryImage(null);
      setMessages(/* 当前回调处理用户交互或异步状态变化。 */ current => mergeLiveMessage(current, result.message));
    } catch (/* sendError 表示发送错误。 */ sendError) {
      if (isCurrentChatRequest(sendSequence.current, sequence, controller.signal)) {
        // confirmed 保存远端已确认投递、仅本地状态收口失败时返回的不可重试图片消息。
        const confirmed = confirmedOutgoingMessageFromError(sendError);
		// uncertain 保存图片未知发送结果错误中的待确认消息。
		const uncertain = uncertainOutgoingMessageFromError(sendError);
		// resolved 保存图片已确认或待确认的不可普通重试消息。
		const resolved = confirmed || uncertain;
        if (resolved) {
          setRetryText(null);
          setRetryImage(null);
          setMessages(/* 当前回调把已确认或待确认的图片消息合并到当前会话。 */ current => mergeLiveMessage(current, resolved));
          setSendNotice(confirmed ? '消息已发送，但本地状态同步失败，请刷新会话确认状态。' : '发送结果待确认，请先到闲鱼核对，避免重复发送。');
        } else {
          if (rememberRetry) setRetryImage(source);
          if (!isChatAbortError(sendError)) setError(sendError instanceof Error ? sendError.message : '图片发送失败');
        }
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
    await sendText(text, true, true);
  }, [activeAccountID, draft, selectedSession, sendText, sending]);

  /** 发送侧栏快捷回复，并在成功后保留用户尚未发送的普通草稿。 */
  const handleQuickReply = useCallback(/* 当前回调将账号级快捷回复交给既有可靠文本发送流程。 */ async (content: string): Promise<void> => {
    // text 保存去除首尾空白后的快捷回复正文，避免空模板触发发送请求。
    const text = content.trim();
    if (!text || !selectedSession || !activeAccountID || sending) return;
    await sendText(text, true, false);
  }, [activeAccountID, selectedSession, sendText, sending]);

  /** 处理图片选择并进入确认预览，不直接触发平台发送。 */
  const handleImage = useCallback(/* 当前回调接收文件选择结果并创建图片预览。 */ async (file?: File): Promise<void> => {
    if (!file || !selectedSession || !activeAccountID || sending) return;
    if (!file.type.startsWith('image/')) {
      setError('仅支持粘贴/发送图片文件');
      return;
    }
    setError('');
    setPendingImage({ file, url: URL.createObjectURL(file) });
  }, [activeAccountID, selectedSession, sending]);

  /** 选择图片库图片并进入确认预览，不直接触发平台发送。 */
  const handleLibraryImage = useCallback(/* 当前回调接收图片库选择结果并创建确认预览。 */ (image: LibraryImageChoice): void => {
    if (!selectedSession || !activeAccountID || sending) return;
    setError('');
    setPendingImage({ libraryImage: image, url: libraryImageContentUrl(image.id) });
  }, [activeAccountID, selectedSession, sending]);

  /** 直接发送图片快捷回复；失败时记录为可重试的图片。 */
  const handleQuickReplyImage = useCallback(/* 当前回调把图片快捷回复交给既有图片发送流程。 */ async (libraryImageId: number): Promise<void> => {
    if (libraryImageId <= 0 || !selectedSession || !activeAccountID || sending) return;
    await sendImage({ libraryImageId }, true);
  }, [activeAccountID, selectedSession, sendImage, sending]);

  /** 从剪贴板文件中选择首张图片，其余文件仍由原生文本粘贴流程处理。 */
  const handlePastedImages = useCallback(/* 当前回调从剪贴板候选文件中筛选图片。 */ async (files: File[]): Promise<void> => {
    // image 保存剪贴板中的首张图片，只有图片才会阻止原生文本粘贴。
    const image = files.find(/* 当前回调判断文件是否为图片。 */ file => file.type.startsWith('image/'));
    if (image) await handleImage(image);
  }, [handleImage]);

  /** 关闭图片预览并清空文件输入，使同一文件可以再次触发选择事件。 */
  const closeImagePreview = useCallback(/* 当前回调关闭预览并清理文件输入。 */ (): void => {
    setPendingImage(null);
    if (imageInputRef.current) imageInputRef.current.value = '';
  }, []);

  /** 确认发送当前预览图片，发送流程沿用已有请求代次和失败重试保护。 */
  const confirmSendImage = useCallback(/* 当前回调提交用户确认的图片预览。 */ async (): Promise<void> => {
    if (!pendingImage || !selectedSession || !activeAccountID || sending) return;
    // source 保存用户确认后要提交的图片来源：本地文件或图片库图片。
    const source: ChatImageSource | null = pendingImage.file ?? (pendingImage.libraryImage ? { libraryImageId: pendingImage.libraryImage.id } : null);
    setPendingImage(null);
    if (source) await sendImage(source, true);
  }, [activeAccountID, pendingImage, selectedSession, sendImage, sending]);

  /** 重试最近一次失败的文本或图片发送。 */
  const retrySend = useCallback(/* 当前回调封装可复用的交互处理逻辑。 */ async (): Promise<void> => {
    if (retryText) await sendText(retryText, false, true);
    else if (retryImage) await sendImage(retryImage, false);
  }, [retryImage, retryText, sendImage, sendText]);

	/** 在本机隐藏会话并清空消息；只有当前代次的成功响应能够改写聊天状态。 */
	const deleteConversation = useCallback(/* 当前回调提交确认框选中的账号与会话。 */ async (accountID: string, chatID: string): Promise<boolean> => {
		if (!accountID || !chatID || (deleteController.current && !deleteController.current.signal.aborted)) return false;
		// sequence 标识本次删除请求，账号切换或卸载会推进代次使其失效。
		const sequence = ++deleteSequence.current;
		deleteController.current?.abort();
		// controller 负责取消本次本地删除请求，避免过期响应覆盖新状态。
		const controller = new AbortController();
			deleteController.current = controller;
			deletingAccountIDRef.current = accountID;
			deletingSessionIDRef.current = chatID;
			deletionNeedsLocalReloadRef.current = false;
			setDeletingChatID(chatID);
		setDeleteError('');
		try {
			sessionSequence.current += 1;
      sessionReloadingRef.current = false;
			contactSequence.current += 1;
			sessionController.current?.abort();
			contactController.current?.abort();
			setContactsLoading(false);
				await deleteChatSession(accountID, chatID, { signal: controller.signal });
				if (!isCurrentChatRequest(deleteSequence.current, sequence, controller.signal)) return false;
				sessionSequence.current += 1;
      sessionReloadingRef.current = false;
				contactSequence.current += 1;
				sessionController.current?.abort();
				contactController.current?.abort();
				setContactsLoading(false);
				if (activeAccountRef.current === accountID && activeChatRef.current === chatID) suppressAutoSelectAccountRef.current = accountID;
			setSessionsByAccount(/* current 保存全部账号已加载会话，只从目标账号移除已删除行。 */ current => ({
				...current,
				[accountID]: (current[accountID] || []).filter(/* session 是目标账号当前参与删除筛选的会话。 */ session => session.chat_id !== chatID),
			}));
			if (activeAccountRef.current === accountID && activeChatRef.current === chatID) {
				messageSequence.current += 1;
				messageController.current?.abort();
				olderSequence.current += 1;
				olderController.current?.abort();
				sendSequence.current += 1;
				sendController.current?.abort();
				setActiveChatID('');
				setMessages([]);
				setMessagesLoading(false);
				setOlderLoading(false);
				setHasOlder(false);
				setHistoryCursor(undefined);
				setPendingImage(null);
				setRetryText(null);
				setRetryImage(null);
				setSending(false);
				setError('');
				setSendNotice('');
			}
				await reloadLocalSessions(accountID);
        if (!isCurrentChatRequest(deleteSequence.current, sequence, controller.signal)) return false;
				// needsFollowupLocalReload 保存首次本地读取期间是否又收到目标实时事件。
				const needsFollowupLocalReload = deletionNeedsLocalReloadRef.current;
				deletingAccountIDRef.current = '';
				deletingSessionIDRef.current = '';
				deletionNeedsLocalReloadRef.current = false;
				if (needsFollowupLocalReload && isCurrentChatRequest(deleteSequence.current, sequence, controller.signal)) await reloadLocalSessions(accountID);
        if (!isCurrentChatRequest(deleteSequence.current, sequence, controller.signal)) return false;
				return true;
		} catch (/* deleteRequestError 保存本次删除请求失败原因；取消和过期响应保持静默。 */ deleteRequestError) {
			if (isCurrentChatRequest(deleteSequence.current, sequence, controller.signal) && !isChatAbortError(deleteRequestError)) {
				setDeleteError(deleteRequestError instanceof Error ? deleteRequestError.message : '删除会话失败，请重试');
			}
				if (isCurrentChatRequest(deleteSequence.current, sequence, controller.signal)) {
					await reloadLocalSessions(accountID);
        if (!isCurrentChatRequest(deleteSequence.current, sequence, controller.signal)) return false;
					// needsFollowupLocalReload 保存失败恢复读取期间是否又收到目标实时事件。
					const needsFollowupLocalReload = deletionNeedsLocalReloadRef.current;
					deletingAccountIDRef.current = '';
					deletingSessionIDRef.current = '';
					deletionNeedsLocalReloadRef.current = false;
					if (needsFollowupLocalReload) await reloadLocalSessions(accountID);
        if (!isCurrentChatRequest(deleteSequence.current, sequence, controller.signal)) return false;
				}
			return false;
		} finally {
			if (isCurrentChatRequest(deleteSequence.current, sequence, controller.signal)) {
				setDeletingChatID('');
				deleteController.current = null;
			}
		}
	}, [reloadLocalSessions]);

	/** 清除确认框打开前遗留的删除错误。 */
	const clearDeleteError = useCallback(/* 当前回调只重置删除流程错误，不影响聊天页其他提示。 */ (): void => setDeleteError(''), []);

	/** 将商品卡片等独立发送器的结果合并到当前会话，不改动文本和图片重试状态。 */
	const acceptOutgoingMessage = useCallback(/* 当前回调接收已经过后端状态机确认的出站消息。 */ (message: ChatMessage, notice = ''): void => {
		if (message.account_id !== activeAccountRef.current || message.chat_id !== activeChatRef.current) return;
		setMessages(/* current 是当前会话消息集合，新结果按 message_key 幂等合并。 */ current => mergeLiveMessage(current, message));
		setSendNotice(notice);
	}, []);

  return {
    accounts, activeAccountID, activeSessions, selectedSession, activeAccount, messages, search, unreadOnly, draft, loading, messagesLoading, olderLoading, hasOlder, contactsLoading, hasMoreContacts: platformHasMoreContacts[activeAccountID] === true || storedHasMoreContacts[activeAccountID] === true, emojiOpen, sending, error, sendNotice, liveState, pendingImage,
    activeChatID, filteredSessions, scrollRef, imageInputRef, setActiveAccountID, setActiveChatID, setSearch, setUnreadOnly, setDraft, setEmojiOpen,
		reloadSessions, loadMoreContacts, loadOlderMessages, handleMessageScroll, handleSend, handleQuickReply, handleQuickReplyImage, handleLibraryImage, handleImage, handlePastedImages, confirmSendImage, closeImagePreview, retrySend, retryAvailable: Boolean(retryText || retryImage), deletingChatID, deleteError, deleteConversation, clearDeleteError, acceptOutgoingMessage, unreadForAccount,
    emojiURL, xianyuEmojis, renderXianyuText, formatClock, messageTime,
  };
};
