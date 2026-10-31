import {
AlertCircle,Check,CheckCheck,ImagePlus,Images,Loader2,MessageCircleMore,PackageSearch,PanelRightClose,PanelRightOpen,
RefreshCw,Search,Send,Smile,UserRound,Wifi,WifiOff,X,
} from 'lucide-react';
import React from 'react';
import Lightbox from 'yet-another-react-lightbox';
import 'yet-another-react-lightbox/styles.css';
import { AudioMessage } from '../components/AudioMessage';
import BrowserNotificationToggle from '../components/BrowserNotificationToggle';
import { ChatItemPickerDialog } from '../components/ChatItemPickerDialog';
import ChatMetadataFeature from '../components/ChatMetadataFeature';
import { ConversationListItem } from '../components/ConversationListItem';
import { DeleteConversationDialog } from '../components/DeleteConversationDialog';
import { ItemMessageCard } from '../components/ItemMessageCard';
import { useChat } from '../hooks';
import type { ChatSession } from '../models';
import type { ChatProps } from '../types';
import { unreadBadgeClassName,unreadBadgeLabel } from '../state';

// Chat 展示实时会话、消息分页和消息发送界面。
const Chat: React.FC<ChatProps> = ({ libraryImageChooser: LibraryChooser }) => {
  // chatState 是 Chat feature Hook 提供的状态、引用和交互动作。
  const {
    accounts, activeAccountID, activeChatID, activeAccount, selectedSession, filteredSessions,
    messages, search, unreadOnly, draft, loading, messagesLoading, olderLoading, hasOlder, contactsLoading,
    hasMoreContacts, emojiOpen, sending, error, sendNotice, liveState, pendingImage, scrollRef, imageInputRef, setActiveAccountID,
    setActiveChatID, setSearch, setUnreadOnly, setDraft, setEmojiOpen, reloadSessions, loadMoreContacts,
    loadOlderMessages, handleMessageScroll, handleSend, handleQuickReply, handleQuickReplyImage, handleLibraryImage, handleImage, handlePastedImages, confirmSendImage, closeImagePreview, retrySend, retryAvailable,
    deletingChatID, deleteError, deleteConversation, clearDeleteError, acceptOutgoingMessage, unreadForAccount, emojiURL, xianyuEmojis, renderXianyuText, formatClock, messageTime,
  } = useChat();


  // imageMessages 保存当前会话中的图片消息，供灯箱按消息顺序浏览。
  const imageMessages = React.useMemo(/* 当前回调筛选当前会话中的图片消息。 */ () => messages.filter(/* 当前回调判断消息是否为图片类型。 */ message => message.message_type === 'image'), [messages]);
  // imageSlides 将聊天图片转换为灯箱组件所需的展示模型。
  const imageSlides = React.useMemo(/* 当前回调构造灯箱图片展示数据。 */ () => imageMessages.map(/* 当前回调转换单条图片消息。 */ message => ({ src: message.content, alt: '聊天图片' })), [imageMessages]);
  // lightboxIndex 保存当前灯箱图片下标；负值表示灯箱关闭。
  const [lightboxIndex, setLightboxIndex] = React.useState(-1);
  // openLightbox 根据消息键打开对应的图片灯箱。
  const openLightbox = React.useCallback(/* 当前回调定位并打开指定聊天图片。 */ (messageKey: string): void => {
    setLightboxIndex(imageMessages.findIndex(/* 当前回调匹配图片消息键。 */ item => item.message_key === messageKey));
  }, [imageMessages]);
  // quickReplyPanelOpen 保存右侧快捷回复抽屉的展开状态；页面卸载后恢复默认收起。
  const [quickReplyPanelOpen, setQuickReplyPanelOpen] = React.useState(false);
  // libraryPickerOpen 保存输入框“图片库”选择器的开关，属于瞬时界面状态。
  const [libraryPickerOpen, setLibraryPickerOpen] = React.useState(false);
  // itemPickerOpen 控制当前会话的居中商品选择弹窗。
  const [itemPickerOpen, setItemPickerOpen] = React.useState(false);
	// deleteTarget 保存等待用户确认清空的会话，空值表示确认框关闭。
	const [deleteTarget, setDeleteTarget] = React.useState<ChatSession | null>(null);

  React.useEffect(/* 当前副作用在账号或会话变化时关闭旧上下文中的商品弹窗。 */ () => {
    setItemPickerOpen(false);
  }, [activeAccountID, activeChatID]);

	React.useEffect(/* 当前副作用在账号切换时关闭旧账号的删除确认框。 */ () => {
		setDeleteTarget(null);
	}, [activeAccountID]);

	/** 打开指定会话的删除确认框，不改变当前选中会话。 */
	const openDeleteDialog = React.useCallback(/* 当前回调记录用户通过独立垃圾桶按钮选中的会话。 */ (session: ChatSession): void => {
		clearDeleteError();
		setDeleteTarget(session);
	}, [clearDeleteError]);

	/** 确认清空当前弹窗会话，失败时保留上下文供用户重试。 */
	const confirmDeleteConversation = React.useCallback(/* 当前回调只在后端完成事务后关闭确认框。 */ async (): Promise<void> => {
		if (!deleteTarget) return;
		// deleted 表示后端已经完成会话隐藏和消息物理清空。
		const deleted = await deleteConversation(deleteTarget.account_id, deleteTarget.chat_id);
		if (deleted) setDeleteTarget(null);
	}, [deleteConversation, deleteTarget]);

  if (loading) return <div className="flex h-[calc(100vh-4rem)] items-center justify-center"><Loader2 className="h-8 w-8 animate-spin text-sky-500" /></div>;

  return (
    <section className="flex h-[calc(100vh-4rem)] min-h-[560px] flex-col overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-chat">
      <header className="border-b border-slate-200 bg-slate-50/70 px-5 pt-4">
        <div className="mb-3 flex items-center justify-between gap-4">
          <div>
            <h2 className="text-xl font-black tracking-tight text-slate-950">在线聊天</h2>
            <p className="mt-0.5 text-xs font-medium text-slate-500">复用账号实时连接，消息按账号完全隔离</p>
          </div>
          <div className="flex items-center gap-2">
            <BrowserNotificationToggle />
            <div className={`flex items-center gap-2 rounded-full px-3 py-1.5 text-xs font-bold ${liveState === 'online' ? 'bg-emerald-50 text-emerald-700' : liveState === 'connecting' ? 'bg-amber-50 text-amber-700' : 'bg-red-50 text-red-700'}`}>
              {liveState === 'online' ? <Wifi className="h-3.5 w-3.5" /> : <WifiOff className="h-3.5 w-3.5" />}
              {liveState === 'online' ? '实时同步中' : liveState === 'connecting' ? '正在连接' : '连接已断开'}
            </div>
          </div>
        </div>
        <div className="flex gap-1 overflow-x-auto pb-0" role="tablist" aria-label="聊天账号">
          {accounts.map(/* 当前回调处理集合中的单个元素。 */ account => {
            // active 当前状态。
            const active = account.id === activeAccountID;
            // unread unread，负责当前功能中的对应处理。
            const unread = unreadForAccount(account.id);
            // unreadLabel 保存账号未读徽标的展示文本。
            const unreadLabel = unreadBadgeLabel(unread);
            // online 响应当前用户操作（line）。
            const online = account.runtime_state === 'online';
            return (
              <button key={account.id} type="button" role="tab" aria-selected={active} onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setActiveAccountID(account.id)}
                className={`relative flex h-11 shrink-0 items-center gap-2 border-b-2 px-3 text-sm font-extrabold transition-colors ${active ? 'border-sky-500 text-sky-700' : 'border-transparent text-slate-500 hover:text-slate-900'}`}>
                <span className={`h-2 w-2 rounded-full ${online ? 'bg-emerald-500' : 'bg-slate-300'}`} />
                <span className="max-w-36 truncate">{account.nickname || account.remark || account.id}</span>
                {unread > 0 && <span aria-label={`未读消息 ${unreadLabel} 条`} className={unreadBadgeClassName(unread)}>{unreadLabel}</span>}
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
                <input value={search} onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setSearch(event.target.value)} placeholder="搜索用户、商品或消息"
                  className="h-10 w-full rounded-xl border border-slate-200 bg-white pl-9 pr-3 text-sm outline-none transition focus:border-sky-400 focus:ring-2 focus:ring-sky-100" />
              </div>
              <div className="flex items-center justify-between">
                <button type="button" onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setUnreadOnly(/* 当前回调处理用户交互或异步状态变化。 */ value => !value)}
                  className={`rounded-lg px-2.5 py-1.5 text-xs font-bold ${unreadOnly ? 'bg-sky-100 text-sky-700' : 'text-slate-500 hover:bg-slate-100'}`}>
                  {unreadOnly ? '只看未读' : '全部会话'}
                </button>
                <button type="button" title="刷新会话" onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => void reloadSessions(activeAccountID)} className="rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700">
                  <RefreshCw className="h-4 w-4" />
                </button>
              </div>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto">
              {filteredSessions.map(/* 当前回调处理集合中的单个元素。 */ session => (
				<ConversationListItem key={session.chat_id} session={session} active={session.chat_id === activeChatID} formatClock={formatClock} onSelect={/* 当前回调切换当前聊天。 */ () => setActiveChatID(session.chat_id)} onDelete={/* 当前回调打开会话删除确认框且不触发选择按钮。 */ () => openDeleteDialog(session)} deleteDisabled={sending && session.chat_id === activeChatID} />
              ))}
              {filteredSessions.length === 0 && <div className="px-6 py-16 text-center text-sm text-slate-400">当前账号暂无匹配会话</div>}
              {hasMoreContacts && !search && !unreadOnly && <div className="flex justify-center p-4">
                <button type="button" onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => void loadMoreContacts()} disabled={contactsLoading}
                  className="flex items-center gap-2 rounded-full border border-slate-200 bg-white px-4 py-2 text-xs font-bold text-slate-500 shadow-sm hover:border-sky-200 hover:text-sky-600 disabled:opacity-50">
                  {contactsLoading && <Loader2 className="h-3.5 w-3.5 animate-spin" />}{contactsLoading ? '正在加载' : '加载更多历史联系人'}
                </button>
              </div>}
            </div>
          </aside>

          <main className="relative flex min-h-0 min-w-0 flex-col overflow-hidden bg-surface-subtle">
            {selectedSession ? (
              <>
                <div className="flex h-20 shrink-0 items-start border-b border-slate-200 bg-white px-5 pt-4">
                  <div className="min-w-0">
                    <div className="truncate text-sm font-black text-slate-950">{selectedSession.peer_name || selectedSession.peer_user_id}</div>
                    <div className="mt-0.5 flex flex-col text-xs text-slate-500"><span>用户 ID：</span><span className="truncate">{selectedSession.peer_user_id}</span></div>
                  </div>
                  <span className={`ml-auto rounded-full px-2.5 py-1 text-[10px] font-bold ${activeAccount?.runtime_state === 'online' ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>
                    {activeAccount?.runtime_state === 'online' ? '账号在线' : '账号离线'}
                  </span>
                </div>
                <div ref={scrollRef} onScroll={handleMessageScroll} className="min-h-0 flex-1 space-y-5 overflow-y-auto px-6 py-5">
                  {messagesLoading ? <div className="flex justify-center py-12"><Loader2 className="h-6 w-6 animate-spin text-sky-500" /></div> : <>
                    {hasOlder && <div className="flex justify-center pb-1">
                      <button type="button" onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => void loadOlderMessages()} disabled={olderLoading}
                        className="flex items-center gap-2 rounded-full border border-slate-200 bg-white px-4 py-1.5 text-xs font-bold text-slate-500 shadow-sm transition hover:border-sky-200 hover:text-sky-600 disabled:opacity-50">
                        {olderLoading && <Loader2 className="h-3.5 w-3.5 animate-spin" />}{olderLoading ? '正在加载' : '加载更早消息'}
                      </button>
                    </div>}
                    {messages.map(/* 当前回调处理集合中的单个元素。 */ message => {
                    // outgoing 是否为发送方消息。
                    const outgoing = message.direction === 'outgoing';
                    // system 系统。
                    const system = message.message_type === 'system';
                    if (system) {
                      return (
                        <div key={message.message_key} className="flex justify-center py-1">
                          <div className="max-w-[82%] whitespace-pre-wrap break-words rounded-xl border border-slate-200 bg-slate-100 px-4 py-2 text-center text-xs leading-5 text-slate-500">
                            {renderXianyuText(message.content)}
                            <div className="mt-1 text-[10px] text-slate-400">{messageTime(message.sent_at)}</div>
                          </div>
                        </div>
                      );
                    }
                    return (
                      <div key={message.message_key} className={`flex items-end gap-2.5 ${outgoing ? 'justify-end' : 'justify-start'}`}>
                        {!outgoing && <div className="h-9 w-9 shrink-0 overflow-hidden rounded-full bg-slate-200 ring-2 ring-white">
                          {selectedSession.peer_avatar_url ? <img src={selectedSession.peer_avatar_url} alt={selectedSession.peer_name || '用户'} className="h-full w-full object-cover" /> : <UserRound className="m-2 h-5 w-5 text-slate-500" />}
                        </div>}
                        <div className={`max-w-[72%] ${outgoing ? 'items-end' : 'items-start'} flex flex-col`}>
                          <div className="mb-1 px-1 text-[10px] font-semibold text-slate-400">{outgoing ? (activeAccount?.nickname || activeAccount?.remark || '我') : (selectedSession.peer_name || message.sender_name || selectedSession.peer_user_id)}</div>
                          {message.message_type === 'item' ? (
                            <ItemMessageCard content={message.content} outgoing={outgoing} />
                          ) : message.message_type === 'image' ? (
                            <button type="button" title="点击预览大图" onClick={openLightbox.bind(null, message.message_key)} className={`block cursor-zoom-in overflow-hidden rounded-2xl border bg-white p-1 text-left shadow-sm ${outgoing ? 'rounded-br-md border-sky-200' : 'rounded-bl-md border-slate-200'}`}>
                              <img src={message.content} alt="聊天图片" className="max-h-80 max-w-full rounded-xl object-contain" />
                            </button>
                          ) : message.message_type === 'video' ? (
                            <video src={message.content} controls preload="metadata" className="max-h-80 max-w-full rounded-2xl bg-black" />
                          ) : message.message_type === 'audio' ? (
                            <AudioMessage messageKey={message.message_key} src={message.content} outgoing={outgoing} initialDuration={message.media_duration} />
                          ) : (
                            <div className={`whitespace-pre-wrap break-words rounded-2xl px-4 py-2.5 text-sm leading-6 shadow-sm ${outgoing ? 'rounded-br-md bg-sky-500 text-white' : 'rounded-bl-md border border-slate-200 bg-white text-slate-800'}`}>{renderXianyuText(message.content)}</div>
                          )}
                          <div className="mt-1 flex items-center gap-1 text-[10px] text-slate-400">
                            {messageTime(message.sent_at)}
                            {outgoing && (message.status === 'failed' ? <AlertCircle className="h-3 w-3 text-red-500" aria-label="发送失败" /> : message.read_status === 2 ? <CheckCheck className="h-3 w-3 text-sky-500" aria-label="对方已读" /> : message.status === 'sent' ? <Check className="h-3 w-3 text-sky-500" aria-label="已发送未读" /> : <Check className="h-3 w-3" aria-label="发送中" />)}
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
                {error && <div className="flex items-center justify-between gap-3 border-t border-red-100 bg-red-50 px-5 py-2 text-xs font-medium text-red-700"><span>{error}</span>{retryAvailable && <button type="button" className="font-bold underline" onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => void retrySend()}>重试发送</button>}</div>}
                {sendNotice && <div className="border-t border-amber-100 bg-amber-50 px-5 py-2 text-xs font-medium text-amber-800">{sendNotice}</div>}
                <div className="relative z-10 shrink-0 border-t border-slate-200 bg-white p-4 shadow-chat-input">
                  <div className="mb-2 flex items-center gap-1">
                    <div className="relative">
                      <button type="button" onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setEmojiOpen(/* 当前回调处理用户交互或异步状态变化。 */ value => !value)} disabled={sending || activeAccount?.runtime_state !== 'online'} className="rounded-lg p-2 text-slate-500 hover:bg-sky-50 hover:text-sky-600 disabled:opacity-40" title="闲鱼表情"><Smile className="h-5 w-5" /></button>
                      {emojiOpen && <div className="absolute bottom-11 left-0 z-30 w-[360px] rounded-2xl border border-slate-200 bg-white p-3 shadow-2xl">
                        <div className="mb-2 text-xs font-bold text-slate-500">全部表情</div>
                        <div className="grid max-h-72 grid-cols-8 gap-1 overflow-y-auto">
                          {xianyuEmojis.map(/* 当前回调处理集合中的单个元素。 */ ([name, file]) => <button key={name} type="button" title={`[${name}]`} onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => { setDraft(/* 当前回调处理用户交互或异步状态变化。 */ value => value + `[${name}]`); setEmojiOpen(false); }} className="flex h-10 w-10 items-center justify-center rounded-lg hover:bg-slate-100"><img src={emojiURL(file)} alt={`[${name}]`} className="h-8 w-8 object-contain" /></button>)}
                        </div>
                      </div>}
                    </div>
                    <input ref={imageInputRef} type="file" accept="image/*" className="hidden" onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => void handleImage(event.target.files?.[0])} />
                    <button type="button" onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => imageInputRef.current?.click()} disabled={sending || activeAccount?.runtime_state !== 'online'} className="rounded-lg p-2 text-slate-500 transition hover:bg-sky-50 hover:text-sky-600 disabled:opacity-40" title="发送图片（最大 10MB）"><ImagePlus className="h-5 w-5" /></button>
                    {LibraryChooser && <button type="button" onClick={/* 当前回调打开图片库选择器，选中后进入与本地图片相同的发送预览。 */ () => setLibraryPickerOpen(true)} disabled={sending || !selectedSession || activeAccount?.runtime_state !== 'online'} className="rounded-lg p-2 text-slate-500 transition hover:bg-sky-50 hover:text-sky-600 disabled:opacity-40" title="从图片库发送图片" aria-label="从图片库发送图片"><Images className="h-5 w-5" /></button>}
                    <button type="button" onClick={/* 当前回调打开居中商品选择弹窗并收起表情面板。 */ () => { setEmojiOpen(false); setItemPickerOpen(true); }} disabled={sending || !selectedSession || activeAccount?.runtime_state !== 'online'} className="rounded-lg p-2 text-slate-500 transition hover:bg-sky-50 hover:text-sky-600 focus:bg-sky-50 focus:text-sky-600 focus:outline-none focus:ring-2 focus:ring-sky-100 active:bg-sky-100 disabled:cursor-not-allowed disabled:opacity-40" title="发送宝贝" aria-label="发送宝贝"><PackageSearch className="h-5 w-5" /></button>
                    <button type="button" onClick={/* 当前回调依当前渲染状态切换右侧账号快捷回复抽屉，避免页面级外部点击监听与按钮切换相互抵消。 */ () => setQuickReplyPanelOpen(!quickReplyPanelOpen)} className="ml-auto rounded-lg p-2 text-slate-500 transition hover:bg-sky-50 hover:text-sky-600" title={quickReplyPanelOpen ? '收起快捷回复' : '展开快捷回复'} aria-label={quickReplyPanelOpen ? '收起快捷回复' : '展开快捷回复'}>
                      {quickReplyPanelOpen ? <PanelRightClose className="h-5 w-5" /> : <PanelRightOpen className="h-5 w-5" />}
                    </button>
                  </div>
                  <div className="flex items-end gap-3 rounded-2xl border border-slate-200 bg-slate-50 p-2 transition focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100">
                    <textarea value={draft} onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setDraft(event.target.value)} rows={2} maxLength={2000}
                      onKeyDown={/* 当前回调处理用户交互或异步状态变化。 */ event => { if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); void handleSend(); } }}
                      onPaste={/* 当前回调识别剪贴板中的图片并进入预览流程。 */ event => {
                        // files 保存剪贴板提供的文件候选列表。
                        const files = Array.from(event.clipboardData?.files || []);
                        // image 保存候选列表中的首张图片。
                        const image = files.find(/* 当前回调判断剪贴板文件是否为图片。 */ file => file.type.startsWith('image/'));
                        if (image) {
                          event.preventDefault();
                          void handlePastedImages(files);
                        }
                      }}
                      disabled={activeAccount?.runtime_state !== 'online'} placeholder={activeAccount?.runtime_state === 'online' ? '输入消息，Enter 发送，Shift + Enter 换行，Ctrl + V 粘贴图片' : '账号离线，暂时无法发送'}
                      className="max-h-32 min-h-12 flex-1 resize-none bg-transparent px-2 py-2 text-sm leading-6 outline-none disabled:cursor-not-allowed" />
                    <button type="button" onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => void handleSend()} disabled={!draft.trim() || sending || activeAccount?.runtime_state !== 'online'} className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-sky-500 text-white shadow-md shadow-sky-100 transition hover:bg-sky-600 disabled:cursor-not-allowed disabled:bg-slate-300 disabled:shadow-none" aria-label="发送消息">
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
            <ChatMetadataFeature activeAccountID={activeAccountID} selectedSession={selectedSession} quickReplyPanelOpen={quickReplyPanelOpen} closeQuickReplyPanel={/* 当前回调收起右侧快捷回复抽屉。 */ () => setQuickReplyPanelOpen(false)} sendQuickReply={handleQuickReply} sendQuickReplyImage={handleQuickReplyImage} libraryImageChooser={LibraryChooser} sending={sending} accountOnline={activeAccount?.runtime_state === 'online'} />
          </main>
        </div>
      )}

      {pendingImage && <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/60 p-4" onClick={/* 当前回调点击遮罩取消图片预览。 */ closeImagePreview}>
        <div className="flex max-h-full w-full max-w-2xl flex-col overflow-hidden rounded-2xl bg-white shadow-2xl" onClick={/* 当前回调阻止预览内容点击冒泡到遮罩。 */ event => event.stopPropagation()}>
          <div className="flex shrink-0 items-center justify-between border-b border-slate-200 px-5 py-3">
            <div className="text-sm font-black text-slate-900">发送图片预览</div>
            <button type="button" title="取消" onClick={/* 当前回调关闭图片预览。 */ closeImagePreview} className="rounded-lg p-1.5 text-slate-400 transition hover:bg-slate-100 hover:text-slate-700"><X className="h-5 w-5" /></button>
          </div>
          <div className="flex min-h-0 flex-1 items-center justify-center overflow-hidden bg-slate-100 p-4"><img src={pendingImage.url} alt="待发送图片预览" className="max-h-[55vh] max-w-full rounded-xl object-contain" /></div>
          <div className="flex shrink-0 items-center justify-between gap-3 border-t border-slate-200 px-5 py-3">
            <div className="min-w-0 truncate text-xs text-slate-500">{pendingImage.libraryImage ? `图片库 · ${pendingImage.libraryImage.filename}` : `${pendingImage.file?.name || '粘贴的图片'} · ${((pendingImage.file?.size ?? 0) / 1024).toFixed(0)} KB`}</div>
            <div className="flex shrink-0 items-center gap-2">
              <button type="button" onClick={/* 当前回调取消图片发送。 */ closeImagePreview} className="rounded-xl px-4 py-2 text-sm font-bold text-slate-600 transition hover:bg-slate-100">取消</button>
              <button type="button" onClick={/* 当前回调确认发送预览图片。 */ () => void confirmSendImage()} disabled={sending || activeAccount?.runtime_state !== 'online'} className="flex items-center gap-2 rounded-xl bg-sky-500 px-4 py-2 text-sm font-bold text-white shadow-md shadow-sky-100 transition hover:bg-sky-600 disabled:cursor-not-allowed disabled:opacity-50">{sending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}发送</button>
            </div>
          </div>
        </div>
      </div>}

      {LibraryChooser && <LibraryChooser open={libraryPickerOpen} onClose={/* 当前回调关闭图片库选择器。 */ () => setLibraryPickerOpen(false)} onSelect={/* image 是用户选中的图片库图片，进入发送预览后关闭选择器。 */ image => { handleLibraryImage(image); setLibraryPickerOpen(false); }} />}

      {itemPickerOpen && <ChatItemPickerDialog open accountID={activeAccountID} chatID={activeChatID} onSent={acceptOutgoingMessage} onClose={/* 当前回调关闭当前会话的商品选择弹窗。 */ () => setItemPickerOpen(false)} />}

		{deleteTarget && <DeleteConversationDialog buyerName={deleteTarget.peer_name || `用户 ${deleteTarget.peer_user_id}`} deleting={deletingChatID === deleteTarget.chat_id} error={deleteError} onCancel={/* 当前回调在未提交时关闭会话删除确认框。 */ () => setDeleteTarget(null)} onConfirm={/* 当前回调提交当前确认框中的会话删除。 */ () => void confirmDeleteConversation()} />}

      <Lightbox open={lightboxIndex >= 0} index={Math.max(lightboxIndex, 0)} close={/* 当前回调关闭图片灯箱。 */ () => setLightboxIndex(-1)} slides={imageSlides} />
    </section>
  );
};

export default Chat;
