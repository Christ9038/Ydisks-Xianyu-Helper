import { AlertCircle,ExternalLink,Loader2,PackageSearch,Search,SendHorizontal,X } from 'lucide-react';
import React from 'react';
import type { ChatMessage } from '../models';
import { useChatItemPicker } from '../useChatItemPicker';

/** ChatItemPickerDialogProps 描述商品选择弹窗的会话上下文和结果回调。 */
export interface ChatItemPickerDialogProps {
  /** open 控制弹窗是否挂载到页面。 */
  open: boolean;
  /** accountID 是当前在线账号标识。 */
  accountID: string;
  /** chatID 是当前个人会话标识。 */
  chatID: string;
  /** onSent 将发送成功的商品消息合入主聊天列表。 */
  onSent: (message: ChatMessage, notice?: string) => void;
  /** onClose 关闭弹窗但不改变快捷回复抽屉。 */
  onClose: () => void;
}

/** ChatItemPickerDialog 在居中弹窗内查询并发送对方或自己的在售商品。 */
export const ChatItemPickerDialog: React.FC<ChatItemPickerDialogProps> = ({ open, accountID, chatID, onSent, onClose }) => {
  // picker 保存弹窗商品查询、分页和发送状态。
  const picker = useChatItemPicker({ open, accountID, chatID, onSent, onClose });
	// sendingAny 表示当前存在唯一一笔商品卡片发送；发送完成前锁定关闭、切换和其他发送按钮。
	const sendingAny = picker.sendingItemID !== '';
	// sendingAnyRef 让稳定的键盘监听读取最新发送状态，避免状态变化时重复恢复弹窗外焦点。
	const sendingAnyRef = React.useRef(sendingAny);
	sendingAnyRef.current = sendingAny;
  // dialogRef 指向弹窗主体，用于约束键盘焦点。
  const dialogRef = React.useRef<HTMLDivElement | null>(null);
  // searchInputRef 指向搜索框，弹窗打开后优先获得焦点。
  const searchInputRef = React.useRef<HTMLInputElement | null>(null);
  // previousFocusRef 保存打开前焦点元素，关闭后恢复用户上下文。
  const previousFocusRef = React.useRef<HTMLElement | null>(null);

  React.useEffect(/* 当前副作用处理 Escape、焦点进入、焦点环绕和关闭后的焦点恢复。 */ () => {
    if (!open) return;
    previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    // focusFrame 延迟到弹窗 DOM 挂载后聚焦搜索框。
    const focusFrame = window.requestAnimationFrame(/* focusSearchInput 将键盘焦点移入商品搜索框。 */ () => searchInputRef.current?.focus());
    /** handleKeyDown 关闭弹窗或在首尾可聚焦元素间循环 Tab。 */
    const handleKeyDown = (event: KeyboardEvent): void => {
      if (event.key === 'Escape') {
        event.preventDefault();
		if (!sendingAnyRef.current) onClose();
        return;
      }
      if (event.key !== 'Tab' || !dialogRef.current) return;
      // focusable 是弹窗内当前未禁用且可见的键盘交互元素。
      const focusable = [...dialogRef.current.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), a[href]')];
      if (focusable.length === 0) return;
      // first 是焦点环绕的起点。
      const [first] = focusable;
      // last 是焦点环绕的终点。
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener('keydown', handleKeyDown);
    return /* cleanup 取消延迟聚焦、移除键盘监听并恢复此前焦点。 */ () => {
      window.cancelAnimationFrame(focusFrame);
      document.removeEventListener('keydown', handleKeyDown);
      previousFocusRef.current?.focus();
    };
	}, [onClose, open]);

  if (!open) return null;

  /** handleListScroll 在接近列表底部时请求下一页。 */
  const handleListScroll = (event: React.UIEvent<HTMLDivElement>): void => {
    // target 是当前可滚动商品列表元素。
    const target = event.currentTarget;
    if (target.scrollHeight - target.scrollTop - target.clientHeight < 160) void picker.loadMore();
  };

  return (
	<div className="fixed inset-0 z-[70] flex items-center justify-center bg-black/55 p-3 backdrop-blur-[2px] sm:p-6" onMouseDown={/* 当前回调仅在没有发送任务且点击遮罩本身时关闭商品弹窗。 */ event => { if (!sendingAny && event.target === event.currentTarget) onClose(); }}>
      <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby="chat-item-picker-title" className="flex h-[min(720px,88vh)] w-full max-w-2xl flex-col overflow-hidden rounded-[24px] bg-white shadow-2xl">
        <header className="relative shrink-0 overflow-hidden bg-sky-600 px-5 pb-4 pt-5 sm:px-6">
          <div className="pointer-events-none absolute -right-16 -top-20 h-52 w-52 rounded-full bg-on-accent/20 blur-2xl" />
          <div className="pointer-events-none absolute right-8 -top-8 h-28 w-28 rounded-full bg-on-accent/10 blur-xl" />
          <div className="relative flex items-start justify-between gap-4">
            <div>
              <div className="flex items-center gap-2">
                <span className="flex h-9 w-9 items-center justify-center rounded-xl bg-on-accent text-sky-600 shadow-sm"><PackageSearch className="h-5 w-5" /></span>
                <div>
                  <h2 id="chat-item-picker-title" className="text-lg font-black tracking-tight text-white">发送宝贝</h2>
                  <p className="text-xs font-medium text-white/75">选择商品卡片，直接发到当前聊天</p>
                </div>
              </div>
            </div>
			<button type="button" onClick={/* 当前回调关闭商品选择弹窗。 */ onClose} disabled={sendingAny} className="relative rounded-xl p-2 text-white/80 transition hover:bg-on-accent/15 hover:text-white disabled:cursor-not-allowed disabled:opacity-40" aria-label="关闭商品选择"><X className="h-5 w-5" /></button>
          </div>
        </header>

        <div className="shrink-0 border-b border-slate-200 bg-white px-4 pt-3 sm:px-6">
          <div className="flex gap-6" role="tablist" aria-label="商品归属">
			<button type="button" role="tab" aria-selected={picker.role === 'peer'} disabled={sendingAny} onClick={/* 当前回调切换到对方商品并清空搜索。 */ () => picker.selectRole('peer')} className={`relative px-1 pb-3 text-sm font-black transition disabled:cursor-not-allowed ${picker.role === 'peer' ? 'text-slate-950' : 'text-slate-400 hover:text-slate-700'}`}>TA 的宝贝{picker.role === 'peer' && <span className="absolute inset-x-0 bottom-0 h-1 rounded-full bg-sky-500" />}</button>
			<button type="button" role="tab" aria-selected={picker.role === 'self'} disabled={sendingAny} onClick={/* 当前回调切换到当前账号商品并清空搜索。 */ () => picker.selectRole('self')} className={`relative px-1 pb-3 text-sm font-black transition disabled:cursor-not-allowed ${picker.role === 'self' ? 'text-slate-950' : 'text-slate-400 hover:text-slate-700'}`}>我的宝贝{picker.role === 'self' && <span className="absolute inset-x-0 bottom-0 h-1 rounded-full bg-sky-500" />}</button>
          </div>
        </div>

        <form onSubmit={/* 当前回调阻止页面提交并按 Enter 应用搜索条件。 */ event => { event.preventDefault(); picker.submitSearch(); }} className="shrink-0 border-b border-slate-100 bg-slate-50/70 p-3 sm:px-6 sm:py-4">
          <div className="flex gap-2">
            <label className="relative min-w-0 flex-1">
              <span className="sr-only">搜索宝贝</span>
              <Search className="absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
			<input ref={searchInputRef} value={picker.queryDraft} onChange={/* 当前回调只更新搜索输入，不立即访问平台。 */ event => picker.setQueryDraft(event.target.value)} disabled={sendingAny} maxLength={100} placeholder={picker.role === 'peer' ? '搜索 TA 在卖的宝贝' : '搜索我在卖的宝贝'} className="h-11 w-full rounded-xl border border-slate-200 bg-white pl-10 pr-3 text-sm text-slate-900 outline-none transition placeholder:text-slate-400 focus:border-sky-400 focus:ring-4 focus:ring-sky-100 disabled:cursor-not-allowed disabled:bg-slate-50" />
            </label>
			<button type="submit" disabled={sendingAny} className="flex h-11 shrink-0 items-center gap-1.5 rounded-xl bg-contrast px-4 text-sm font-bold text-on-contrast transition hover:bg-contrast/90 focus:outline-none focus:ring-4 focus:ring-slate-200 disabled:cursor-not-allowed disabled:bg-slate-300"><Search className="h-4 w-4" />搜索</button>
          </div>
        </form>

        <div onScroll={handleListScroll} className="min-h-0 flex-1 overflow-y-auto bg-white px-3 py-2 sm:px-5 sm:py-3">
          {picker.loading ? <div className="flex h-full min-h-52 flex-col items-center justify-center text-slate-400"><Loader2 className="h-7 w-7 animate-spin text-sky-500" /><span className="mt-3 text-sm font-semibold">正在翻找宝贝…</span></div> : picker.items.length === 0 ? <div className="flex h-full min-h-52 flex-col items-center justify-center px-6 text-center"><div className="flex h-16 w-16 items-center justify-center rounded-3xl bg-sky-50 text-sky-500"><PackageSearch className="h-8 w-8" /></div><div className="mt-4 text-sm font-black text-slate-700">没有找到宝贝</div><p className="mt-1 text-xs leading-5 text-slate-400">换个关键词试试，或切换到另一个商品列表。</p></div> : <div className="divide-y divide-slate-100">
            {picker.items.map(/* item 是当前商品列表中的单条快照。 */ item => {
              // sending 表示当前商品卡片正在发送。
              const sending = picker.sendingItemID === item.item_id;
              // detailURL 只由商品标识构造官方详情地址。
              const detailURL = `https://www.goofish.com/item?id=${encodeURIComponent(item.item_id)}`;
              return <article key={item.item_id} className="group flex items-center gap-3 rounded-xl px-1 py-3 transition hover:bg-slate-50 sm:gap-4 sm:px-2">
                <a href={detailURL} target="_blank" rel="noreferrer" className="flex min-w-0 flex-1 items-center gap-3 rounded-lg outline-none focus:ring-4 focus:ring-sky-100 sm:gap-4" aria-label={`查看商品：${item.title}`}>
                  <img src={item.image_url} alt="" loading="lazy" className="h-20 w-20 shrink-0 rounded-xl bg-slate-100 object-cover ring-1 ring-slate-200 sm:h-24 sm:w-24" />
                  <div className="min-w-0 flex-1 self-stretch py-1">
                    <div className="line-clamp-2 text-sm font-bold leading-5 text-slate-900 sm:text-[15px]">{item.title}</div>
                    {item.description && <p className="mt-1 line-clamp-1 text-xs text-slate-400">{item.description}</p>}
                    <div className="mt-2 flex items-center gap-1 text-amber-500"><span className="text-xs font-black">¥</span><span className="text-lg font-black tracking-tight">{item.price}</span><ExternalLink className="ml-1 h-3.5 w-3.5 text-slate-300 opacity-0 transition group-hover:opacity-100" /></div>
                  </div>
                </a>
			<button type="button" onClick={/* 当前回调发送对应商品卡片并在完成前锁定全部发送入口。 */ () => void picker.sendItem(item)} disabled={sendingAny} className="flex h-9 shrink-0 items-center gap-1.5 rounded-full bg-sky-500 px-3 text-xs font-black text-white shadow-sm transition hover:bg-sky-600 focus:outline-none focus:ring-4 focus:ring-sky-100 disabled:cursor-not-allowed disabled:bg-slate-200 disabled:text-slate-400 sm:px-4">
                  {sending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <SendHorizontal className="h-3.5 w-3.5" />}{sending ? '发送中' : '发送'}
                </button>
              </article>;
            })}
          </div>}
          {picker.loadingMore && <div className="flex items-center justify-center gap-2 py-4 text-xs font-semibold text-slate-400"><Loader2 className="h-4 w-4 animate-spin text-sky-500" />正在加载更多</div>}
          {!picker.loading && picker.items.length > 0 && !picker.hasMore && <div className="py-4 text-center text-[11px] font-medium text-slate-300">已经到底啦</div>}
        </div>

        {picker.error && <div className="flex shrink-0 items-start gap-2 border-t border-red-100 bg-red-50 px-4 py-3 text-xs font-medium leading-5 text-red-700 sm:px-6"><AlertCircle className="mt-0.5 h-4 w-4 shrink-0" /><span>{picker.error}</span></div>}
      </div>
    </div>
  );
};

export default ChatItemPickerDialog;
