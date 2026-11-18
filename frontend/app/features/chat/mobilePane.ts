import { useCallback,useEffect,useRef,useState } from 'react';

/** ChatMobilePane 表示移动端聊天页当前显示的单栏面板；桌面端仍由断点同时展示会话列表与消息区。 */
export type ChatMobilePane = 'list' | 'chat';

/** ChatMobilePaneState 描述移动端主从单栏切换所需的状态和动作。 */
export type ChatMobilePaneState = {
  /** mobilePane 保存移动端当前显示的面板，初始为会话列表。 */
  mobilePane: ChatMobilePane;
  /** openChat 在用户选择会话后切换到消息详情面板。 */
  openChat: () => void;
  /** showList 在用户返回、切换账号或删除当前会话后切换到会话列表面板。 */
  showList: () => void;
};

/** useChatMobilePane 管理移动端聊天页的单栏主从视图，并在账号切换或会话上下文清空时回到列表。 */
export const useChatMobilePane = (activeAccountID: string, activeChatID: string): ChatMobilePaneState => {
  // mobilePane 保存移动端当前显示的面板；桌面端由 CSS 断点同时展示两栏，不消费该值。
  const [mobilePane, setMobilePane] = useState<ChatMobilePane>('list');
  // previousAccountIDRef 保存上一次已观察的账号标识，用于区分账号切换与同一账号内选择会话。
  const previousAccountIDRef = useRef(activeAccountID);

  useEffect(/* 当前副作用在账号切换或会话被清空时回到会话列表，避免残留旧账号的消息详情面板。 */ () => {
    // accountChanged 表示当前账号与上一次观察的账号不同，此时旧账号的面板状态必须丢弃。
    const accountChanged = previousAccountIDRef.current !== activeAccountID;
    previousAccountIDRef.current = activeAccountID;
    if (accountChanged || !activeChatID) setMobilePane('list');
  }, [activeAccountID, activeChatID]);

  /** openChat 在用户点击会话行后显示消息详情面板。 */
  const openChat = useCallback(/* 当前回调只切换移动端面板，会话选择的持久状态由调用方同步更新。 */ (): void => {
    setMobilePane('chat');
  }, []);

  /** showList 在用户返回列表、切换账号或删除当前会话后显示会话列表面板。 */
  const showList = useCallback(/* 当前回调收起消息详情并回到会话列表。 */ (): void => {
    setMobilePane('list');
  }, []);

  return { mobilePane, openChat, showList };
};
