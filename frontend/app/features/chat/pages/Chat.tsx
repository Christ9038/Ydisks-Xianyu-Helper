import React from 'react';
import {
  AlertCircle, Check, CheckCheck, ImagePlus, Loader2, MessageCircleMore, RefreshCw,
  Search, Send, Smile, UserRound, Wifi, WifiOff,
} from 'lucide-react';
import { useChat } from '../hooks';

// Chat 展示实时会话、消息分页和消息发送界面。
const Chat: React.FC = () => {
  // chatState 是 Chat feature Hook 提供的状态、引用和交互动作。
  const {
    accounts, activeAccountID, activeChatID, activeAccount, selectedSession, filteredSessions,
    messages, search, unreadOnly, draft, loading, messagesLoading, olderLoading, hasOlder, contactsLoading,
    hasMoreContacts, emojiOpen, sending, error, liveState, scrollRef, imageInputRef, setActiveAccountID,
    setActiveChatID, setSearch, setUnreadOnly, setDraft, setEmojiOpen, reloadSessions, loadMoreContacts,
    loadOlderMessages, handleMessageScroll, handleSend, handleImage, retrySend, retryAvailable,
    unreadForAccount, emojiURL, xianyuEmojis, renderXianyuText, formatClock, messageTime,
  } = useChat();

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
          {accounts.map(/* 当前回调处理集合中的单个元素。 */ account => {
            // active 当前状态。
            const active = account.id === activeAccountID;
            // unread unread，负责当前功能中的对应处理。
            const unread = unreadForAccount(account.id);
            // online 响应当前用户操作（line）。
            const online = account.runtime_state === 'online';
            return (
              <button key={account.id} type="button" role="tab" aria-selected={active} onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setActiveAccountID(account.id)}
                className={`relative flex h-11 shrink-0 items-center gap-2 border-b-2 px-3 text-sm font-extrabold transition-colors ${active ? 'border-sky-500 text-sky-700' : 'border-transparent text-slate-500 hover:text-slate-900'}`}>
                <span className={`h-2 w-2 rounded-full ${online ? 'bg-emerald-500' : 'bg-slate-300'}`} />
                <span className="max-w-36 truncate">{account.nickname || account.remark || account.id}</span>
                {unread > 0 && <span className="min-w-5 rounded-full bg-red-500 px-1.5 py-0.5 text-[10px] text-white">{unread > 99 ? '99+' : unread}</span>}
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
                <button key={session.chat_id} type="button" onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setActiveChatID(session.chat_id)}
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
                      {session.unread_count > 0 && <span className="ml-auto min-w-5 shrink-0 rounded-full bg-red-500 px-1.5 py-0.5 text-center text-[10px] font-bold text-white">{session.unread_count}</span>}
                    </div>
                    {session.item_title && <div className="mt-1.5 truncate text-[10px] font-medium text-sky-700">商品 · {session.item_title}</div>}
                  </div>
                </button>
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
                          <div className="mb-1 px-1 text-[10px] font-semibold text-slate-400">{outgoing ? (activeAccount?.nickname || activeAccount?.remark || '我') : (selectedSession.buyer_name || message.sender_name || selectedSession.buyer_id)}</div>
                          {message.message_type === 'image' ? (
                            <a href={message.content} target="_blank" rel="noreferrer" className={`block overflow-hidden rounded-2xl border bg-white p-1 shadow-sm ${outgoing ? 'rounded-br-md border-sky-200' : 'rounded-bl-md border-slate-200'}`}>
                              <img src={message.content} alt="聊天图片" className="max-h-80 max-w-full rounded-xl object-contain" />
                            </a>
                          ) : message.message_type === 'video' ? (
                            <video src={message.content} controls preload="metadata" className="max-h-80 max-w-full rounded-2xl bg-black" />
                          ) : (
                            <div className={`rounded-2xl px-4 py-2.5 text-sm leading-6 shadow-sm ${outgoing ? 'rounded-br-md bg-sky-500 text-white' : 'rounded-bl-md border border-slate-200 bg-white text-slate-800'}`}>{renderXianyuText(message.content)}</div>
                          )}
                          <div className="mt-1 flex items-center gap-1 text-[10px] text-slate-400">
                            {messageTime(message.sent_at)}
                            {outgoing && (message.status === 'failed' ? <AlertCircle className="h-3 w-3 text-red-500" /> : message.status === 'sent' ? <CheckCheck className="h-3 w-3 text-sky-500" /> : <Check className="h-3 w-3" />)}
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
                  </div>
                  <div className="flex items-end gap-3 rounded-2xl border border-slate-200 bg-slate-50 p-2 transition focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100">
                    <textarea value={draft} onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setDraft(event.target.value)} rows={2} maxLength={2000}
                      onKeyDown={/* 当前回调处理用户交互或异步状态变化。 */ event => { if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); void handleSend(); } }}
                      disabled={activeAccount?.runtime_state !== 'online'} placeholder={activeAccount?.runtime_state === 'online' ? '输入消息，Enter 发送，Shift + Enter 换行' : '账号离线，暂时无法发送'}
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
          </main>
        </div>
      )}
    </section>
  );
};

export default Chat;
