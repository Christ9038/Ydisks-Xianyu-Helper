import { useCallback,useEffect,useRef,useState } from 'react';
import type { ChatMessage } from './api';
import { getAccountDetails,getChatSessionPage } from './api';
import { publishChatConnectionState,publishChatLiveMessage,subscribeToChatUnreadStatus } from './liveEvents';
import { showBrowserNotification } from '../../../shared/browser/browserNotifications';
import type { BrowserNotificationPayload } from '../../../shared/browser/browserNotifications';

/** titleFlashIntervalMs 定义浏览器标题在提示文本与原始标题间切换的周期，单位为毫秒。 */
const titleFlashIntervalMs = 1_000;

/** formatChatNewMessageTitle 根据原始标题生成不暴露消息数量的浏览器新消息提示文本。 */
export const formatChatNewMessageTitle = (baseTitle: string): string => {
  return `【新消息】${baseTitle}`;
};

/** formatChatBrowserNotification 将入站聊天消息转换为不包含凭证的系统通知内容。 */
export const formatChatBrowserNotification = (message: ChatMessage): BrowserNotificationPayload => {
  // senderName 保存平台提供的发送者名称；缺失时使用稳定的通用标题。
  const senderName = String(message.sender_name || '').trim() || '闲鱼买家';
  // content 保存文本消息正文；缺失内容时使用稳定的占位文案而不是展示 undefined。
  const content = String(message.content || '').trim();
  // body 保存适合系统通知栏的短文本，媒体消息不把远程地址直接展示给用户。
  const body = message.message_type === 'text'
    ? content.slice(0, 120) || '发来了一条新消息'
    : message.message_type === 'image'
      ? '[图片]'
      : message.message_type === 'video'
        ? '[视频]'
        : message.message_type === 'audio'
          ? '[语音]'
          : '发来了一条新消息';
  return {
    title: `${senderName}发来新消息`,
    body,
    tag: `chat-${message.account_id}-${message.chat_id}`,
  };
};

/** ChatTitleNotifierResult 描述标题闪烁与侧边栏新消息标记的受控状态。 */
export type ChatTitleNotifierResult = {
  /** notifyIncomingMessage 接收实时消息并决定是否产生标题和红点提醒。 */
  notifyIncomingMessage: (message: ChatMessage) => void;
  /** hasUnreadChatMessage 表示所有已知聊天会话中是否仍存在未读普通消息。 */
  hasUnreadChatMessage: boolean;
};

/** useChatTitleNotifier 管理浏览器标题闪烁，并返回由实时消息连接调用的通知入口。 */
export const useChatTitleNotifier = (): ChatTitleNotifierResult => {
  // newMessageCount 保存当前应用会话收到、尚未被浏览器重新聚焦确认的入站消息数量。
  const [newMessageCount, setNewMessageCount] = useState(0);
  // hasUnreadChatMessage 保存在线聊天入口的持久红点状态；只有所有会话未读数归零时才清除。
  const [hasUnreadChatMessage, setHasUnreadChatMessage] = useState(false);
  // baseTitleRef 保存挂载通知服务前的标题；本 Hook 卸载时必须恢复该标题，不覆盖其他页面的标题策略。
  const baseTitleRef = useRef(document.title);
  // realtimeUnreadVersionRef 记录初始未读快照开始后收到的实时消息次数，阻止旧快照覆盖新消息提醒。
  const realtimeUnreadVersionRef = useRef(0);

  useEffect(/* 当前副作用在用户回到页面时清除标题提示，并在卸载时移除全局监听。 */ () => {
    /** clearTitleNotification 在用户重新看到聊天页面后确认所有临时标题提醒。 */
    const clearTitleNotification = (): void => setNewMessageCount(0);
    /** handleVisibilityChange 只在页面重新可见时清除提醒，后台切换不影响未读计数。 */
    const handleVisibilityChange = (): void => {
      if (document.visibilityState === 'visible') clearTitleNotification();
    };
    window.addEventListener('focus', clearTitleNotification);
    document.addEventListener('visibilitychange', handleVisibilityChange);
    return /* 当前清理函数释放浏览器事件监听，避免注销后仍修改标题状态。 */ () => {
      window.removeEventListener('focus', clearTitleNotification);
      document.removeEventListener('visibilitychange', handleVisibilityChange);
    };
  }, []);

  useEffect(/* 当前副作用将未确认消息数同步到浏览器标题，并负责闪烁定时器的创建和销毁。 */ () => {
    if (newMessageCount <= 0) {
      document.title = baseTitleRef.current;
      return;
    }
    // showNotificationTitle 表示本次闪烁周期是否展示带计数的提醒标题。
    let showNotificationTitle = true;
    /** updateTitle 将当前闪烁相位写入浏览器标签标题。 */
    const updateTitle = (): void => {
      document.title = showNotificationTitle
        ? formatChatNewMessageTitle(baseTitleRef.current)
        : baseTitleRef.current;
    };
    updateTitle();
    // flashTimer 由此 effect 独占；计数变化或 Hook 卸载时必须清除，避免后台残留定时器。
    const flashTimer = window.setInterval(/* 当前回调切换标题相位以形成新消息闪烁提示。 */ () => {
      showNotificationTitle = !showNotificationTitle;
      updateTitle();
    }, titleFlashIntervalMs);
    return /* 当前清理函数停止旧计数对应的定时器，并将标题恢复为进入聊天页前的值。 */ () => {
      window.clearInterval(flashTimer);
      document.title = baseTitleRef.current;
    };
  }, [newMessageCount]);

  useEffect(/* 当前副作用在认证应用壳启动时读取各启用账号的会话未读状态，取消后不写入页面状态。 */ () => {
    // controller 取消应用壳卸载后尚未完成的账号和会话读取请求。
    const controller = new AbortController();
    // snapshotVersion 保存本次未读快照启动时的实时消息版本，响应只有版本未变化时才可写入红点状态。
    const snapshotVersion = realtimeUnreadVersionRef.current;
    /** loadUnreadStatus 并行读取启用账号的会话首页，以服务端 unread_count 初始化侧边栏红点。 */
    const loadUnreadStatus = async (): Promise<void> => {
      try {
        // accounts 保存当前用户可见的账号摘要，敏感凭证不会进入前端状态。
        const accounts = await getAccountDetails({ signal: controller.signal });
        // enabledAccounts 保存需要查询会话未读数的启用账号。
        const enabledAccounts = accounts.filter(/* account 保存当前判断启用状态的账号摘要。 */ account => account.enabled);
        // pages 保存各启用账号并行读取的会话首页结果。
        const pages = await Promise.all(enabledAccounts.map(/* account 保存当前需要读取会话首页的启用账号。 */ account => getChatSessionPage(account.id, undefined, { signal: controller.signal })));
        if (controller.signal.aborted || realtimeUnreadVersionRef.current !== snapshotVersion) return;
        // hasUnread 保存所有账号会话中是否仍有服务端标记的未读消息。
        const hasUnread = pages.some(/* page 保存当前账号会话首页，用于检查其未读计数。 */ page => page.sessions.some(/* session 保存当前参与未读判断的会话。 */ session => session.unread_count > 0));
        setHasUnreadChatMessage(hasUnread);
      } catch {
        // 初始未读读取失败时保留实时消息已点亮的红点，避免网络暂态错误错误清除提醒。
      }
    };
    void loadUnreadStatus();
    return /* 当前清理函数取消初始未读读取，避免注销后响应覆盖新会话状态。 */ () => controller.abort();
  }, []);

  useEffect(/* 当前副作用接收 Chat 页面已读确认后的会话未读聚合状态，只有为 false 才清除红点。 */ () => subscribeToChatUnreadStatus(setHasUnreadChatMessage), []);

  /** notifyIncomingMessage 在收到普通入站消息时累加标题提示并标记全局仍有未读聊天消息。 */
  const notifyIncomingMessage = useCallback(/* 当前回调由实时 WebSocket 消息入口调用，使用户在当前浏览器窗口也能看见标题变化。 */ (message: ChatMessage): void => {
    if (message.direction !== 'incoming' || message.message_type === 'system') return;
    realtimeUnreadVersionRef.current += 1;
    setNewMessageCount(/* 当前状态更新基于上一条实时消息计数累计，避免同一批 WebSocket 帧丢失数量。 */ current => current + 1);
    setHasUnreadChatMessage(true);
    showBrowserNotification(formatChatBrowserNotification(message));
  }, []);

  return { notifyIncomingMessage, hasUnreadChatMessage };
};

/** ChatTitleNotificationResult 描述应用壳向侧边栏提供的聊天新消息标记控制。 */
export type ChatTitleNotificationResult = Pick<ChatTitleNotifierResult, 'hasUnreadChatMessage'>;

/** useChatTitleNotification 在认证应用壳内维持全局聊天 WebSocket，并把普通入站消息转为标题提醒。 */
export const useChatTitleNotification = (): ChatTitleNotificationResult => {
  // titleNotifier 保存标题和侧边栏红点状态；其消息处理函数保持稳定，重连 effect 不会因计数更新而重建 WebSocket。
  const titleNotifier = useChatTitleNotifier();
  // notifyIncomingMessage 保存标题提醒的稳定入口；hasUnreadChatMessage 保存侧边栏红点应展示的全局未读状态。
  const { notifyIncomingMessage, hasUnreadChatMessage } = titleNotifier;

  useEffect(/* 当前副作用拥有全局通知 WebSocket 的连接、退避重连和卸载清理生命周期。 */ () => {
    // disposed 标记应用壳是否已经卸载；卸载后禁止关闭回调重新创建连接。
    let disposed = false;
    // reconnectTimer 保存当前退避重连定时器；仅此 effect 可以清理它。
    let reconnectTimer = 0;
    // retryCount 保存连续断开次数，用于限制退避延迟的最大值。
    let retryCount = 0;
    // socket 保存本轮通知连接；卸载时由该 Hook 关闭。
    let socket: WebSocket | null = null;
    /** connect 建立认证 Cookie 自动携带的通知连接，并在异常关闭后安排有限退避重连。 */
    const connect = (): void => {
      if (disposed) return;
      publishChatConnectionState('connecting');
      // protocol 根据当前页面协议选择对应安全级别的 WebSocket 方案，避免 HTTPS 页面混合内容失败。
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      socket = new WebSocket(`${protocol}//${window.location.host}/api/v1/chat/ws`);
      socket.onopen = /* 当前回调在连接成功后重置后续异常关闭的退避次数并通知聊天页。 */ () => {
        retryCount = 0;
        publishChatConnectionState('online');
      };
      socket.onmessage = /* 当前回调只解析服务端广播的聊天消息，损坏帧不影响后续连接。 */ event => {
        try {
          // payload 保存服务端推送的 JSON 对象。
          const payload = JSON.parse(event.data) as {
            /** message 保存服务端广播的可选实时聊天消息。 */
            message?: ChatMessage;
          };
          // message 保存可能存在的实时聊天消息；其他 WebSocket 帧无需产生标题提醒。
          const message = payload.message;
          if (message) {
            notifyIncomingMessage(message);
            publishChatLiveMessage(message);
          }
        } catch {
          // 忽略非聊天格式帧，下一条合法广播仍可正常触发通知。
        }
      };
      socket.onclose = /* 当前回调在连接非主动关闭时以有限指数退避重新订阅通知。 */ () => {
        if (disposed) return;
        publishChatConnectionState('offline');
        // delayMs 保存下一次重连等待时间，单位为毫秒，最长不超过十五秒。
        const delayMs = Math.min(15_000, 1_000 * 2 ** Math.min(retryCount++, 4));
        reconnectTimer = window.setTimeout(connect, delayMs);
      };
      socket.onerror = /* 当前回调将错误统一转换为关闭事件，以复用唯一的重连策略。 */ () => socket?.close();
    };
    connect();
    return /* 当前清理函数停止待执行重连并关闭当前连接，确保应用壳注销后不保留后台通知任务。 */ () => {
      disposed = true;
      window.clearTimeout(reconnectTimer);
      socket?.close();
      publishChatConnectionState('offline');
    };
  }, [notifyIncomingMessage]);

  return { hasUnreadChatMessage };
};
