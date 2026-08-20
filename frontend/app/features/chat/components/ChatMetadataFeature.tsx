import { Clipboard,FilePenLine,Plus,Send,Trash2,X } from 'lucide-react';
import React from 'react';
import { useChatMetadata } from '../metadata';
import type { ChatSession } from '../models';

/** ChatMetadataFeatureProps 描述独立构建的聊天元数据控件所需上下文与外部发送能力。 */
type ChatMetadataFeatureProps = {
  /** activeAccountID 是当前选中的闲鱼账号标识。 */
  activeAccountID: string;
  /** selectedSession 是当前正在查看的聊天会话。 */
  selectedSession: ChatSession | undefined;
  /** quickReplyPanelOpen 控制右侧快捷回复抽屉是否可见。 */
  quickReplyPanelOpen: boolean;
  /** closeQuickReplyPanel 收起右侧快捷回复抽屉。 */
  closeQuickReplyPanel: () => void;
  /** sendQuickReply 将模板文本交给既有可靠聊天发送流程。 */
  sendQuickReply: (content: string) => Promise<void>;
  /** sending 表示当前聊天发送请求是否进行中。 */
  sending: boolean;
  /** accountOnline 表示当前账号是否可以向平台发送消息。 */
  accountOnline: boolean;
};

/** ChatMetadataFeature 提供账号快捷回复和买家备注交互，并由独立构建分片控制聊天页面体积。 */
const ChatMetadataFeature: React.FC<ChatMetadataFeatureProps> = ({ activeAccountID, selectedSession, quickReplyPanelOpen, closeQuickReplyPanel, sendQuickReply, sending, accountOnline }) => {
  // chatMetadata 保存账号快捷回复和当前买家备注的独立数据、取消控制及 UI 交互状态。
  const chatMetadata = useChatMetadata(activeAccountID, selectedSession);
  // buyerNoteContent 保存未经字符截断的完整备注；展示区域仅依标题栏的两行空间裁切可见文本。
  const buyerNoteContent = chatMetadata.buyerNote?.content.trim() || '';
  // quickReplyPanelRef 指向快捷回复抽屉根节点，用于区分抽屉内交互与页面外点击。
  const quickReplyPanelRef = React.useRef<HTMLElement | null>(null);

  React.useEffect(/* 当前副作用在抽屉展开期间监听页面级指针事件，点击抽屉外部时自动收起并在 cleanup 中移除监听。 */ () => {
    if (!quickReplyPanelOpen) return undefined;
    /** closeWhenClickOutside 仅在指针目标不属于快捷回复抽屉时收起面板。 */
    const closeWhenClickOutside = (event: PointerEvent): void => {
      // eventTarget 保存当前指针事件的目标节点；非 DOM 节点不参与抽屉内外判断。
      const eventTarget = event.target;
      if (!(eventTarget instanceof Node) || quickReplyPanelRef.current?.contains(eventTarget)) return;
      closeQuickReplyPanel();
    };
    document.addEventListener('pointerdown', closeWhenClickOutside);
    return /* 当前 cleanup 在抽屉收起或组件卸载时移除页面级监听。 */ () => document.removeEventListener('pointerdown', closeWhenClickOutside);
  }, [closeQuickReplyPanel, quickReplyPanelOpen]);

  return <>
    {selectedSession && <div className="absolute left-40 right-[5.5rem] top-2 z-10 flex min-h-16 items-center justify-center">
      <button type="button" onClick={/* 当前回调打开当前买家的完整备注弹窗。 */ chatMetadata.openNoteDialog} className="inline-flex w-fit max-w-full items-center gap-2 rounded-lg border border-transparent px-2.5 py-1.5 text-left text-xs font-medium leading-5 text-slate-500 transition hover:border-sky-100 hover:bg-sky-50 hover:text-sky-700" title={buyerNoteContent || '添加用户备注'}>
        <FilePenLine className="h-4 w-4 shrink-0 self-center text-slate-400" />
        <span className="min-w-0 break-words line-clamp-2">{chatMetadata.noteLoading ? '正在读取备注…' : buyerNoteContent || '添加用户备注'}</span>
      </button>
    </div>}
    {quickReplyPanelOpen && activeAccountID && <aside ref={quickReplyPanelRef} aria-label="账号快捷回复" className="absolute inset-y-0 right-0 z-20 flex w-[304px] max-w-[88%] flex-col border-l border-slate-200 bg-white shadow-2xl">
      <div className="flex items-center justify-between border-b border-slate-100 px-4 py-3">
        <div>
          <div className="text-sm font-black text-slate-900">快捷回复</div>
          <div className="mt-0.5 text-[11px] font-medium text-slate-400">当前账号 · {chatMetadata.quickReplies.length}/50</div>
        </div>
        <button type="button" onClick={/* 当前回调收起右侧快捷回复抽屉。 */ closeQuickReplyPanel} className="rounded-lg p-1.5 text-slate-400 transition hover:bg-slate-100 hover:text-slate-700" aria-label="收起快捷回复"><X className="h-4 w-4" /></button>
      </div>
      <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-3">
        {chatMetadata.quickReplies.map(/* reply 保存当前待展示的账号级快捷回复。 */ reply => <article key={reply.id} className="rounded-xl border border-slate-200 bg-slate-50/70 p-3 transition hover:border-sky-200 hover:bg-sky-50/40">
          <p className="whitespace-pre-wrap break-words text-xs leading-5 text-slate-700">{reply.content}</p>
          <div className="mt-2 flex items-center justify-end gap-1">
            <button type="button" onClick={/* 当前回调请求确认删除本条快捷回复。 */ () => chatMetadata.requestQuickReplyDelete(reply)} disabled={chatMetadata.quickReplyBusy} className="rounded-lg p-1 text-slate-400 transition hover:bg-red-50 hover:text-red-600 disabled:opacity-40" aria-label="删除快捷回复"><Trash2 className="h-3.5 w-3.5" /></button>
            <button type="button" onClick={/* 当前回调复制本条快捷回复后收起抽屉。 */ () => { closeQuickReplyPanel(); void chatMetadata.copyQuickReply(reply); }} className="rounded-lg px-2 py-1 text-[11px] font-bold text-slate-500 transition hover:bg-white hover:text-sky-700" title="复制快捷回复"><Clipboard className="mr-1 inline h-3 w-3" />{chatMetadata.copiedQuickReplyID === reply.id ? '已复制' : '复制'}</button>
            <button type="button" onClick={/* 当前回调发送本条快捷回复后收起抽屉。 */ () => { closeQuickReplyPanel(); void sendQuickReply(reply.content); }} disabled={!selectedSession || sending || !accountOnline} className="rounded-lg bg-sky-500 px-2 py-1 text-[11px] font-bold text-white transition hover:bg-sky-600 disabled:cursor-not-allowed disabled:bg-slate-300" title="发送快捷回复"><Send className="mr-1 inline h-3 w-3" />发送</button>
          </div>
        </article>)}
        {chatMetadata.quickReplies.length === 0 && <div className="px-5 py-12 text-center text-xs leading-5 text-slate-400">还没有快捷回复。把常用话术添加在下方，当前账号的所有聊天都可以使用。</div>}
      </div>
      <div className="border-t border-slate-200 bg-slate-50/80 p-3">
        {chatMetadata.metadataError && <div className="mb-2 flex items-start justify-between gap-2 rounded-lg bg-red-50 px-2.5 py-2 text-[11px] leading-4 text-red-700"><span>{chatMetadata.metadataError}</span><button type="button" onClick={/* 当前回调关闭当前快捷回复或备注错误提示。 */ chatMetadata.clearMetadataError} className="shrink-0" aria-label="关闭错误提示"><X className="h-3.5 w-3.5" /></button></div>}
        <textarea value={chatMetadata.quickReplyDraft} onChange={/* 当前回调更新新增快捷回复表单文本。 */ event => chatMetadata.setQuickReplyDraft(event.target.value)} onKeyDown={/* 当前回调支持 Enter 添加、Shift Enter 换行的快捷回复输入。 */ event => { if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); void chatMetadata.addQuickReply(); } }} rows={2} maxLength={2000} placeholder="添加常用快捷回复…" className="min-h-14 w-full resize-none rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs leading-5 outline-none transition focus:border-sky-400 focus:ring-2 focus:ring-sky-100" />
        <button type="button" onClick={/* 当前回调保存底部输入框中的快捷回复。 */ () => void chatMetadata.addQuickReply()} disabled={!chatMetadata.quickReplyDraft.trim() || chatMetadata.quickReplyBusy || chatMetadata.quickReplies.length >= 50} className="mt-2 flex w-full items-center justify-center gap-1.5 rounded-xl bg-slate-900 py-2 text-xs font-bold text-white transition hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-300"><Plus className="h-3.5 w-3.5" />添加快捷回复</button>
      </div>
    </aside>}
    {chatMetadata.noteDialogOpen && <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/50 p-4" onClick={/* 当前回调点击遮罩关闭买家备注弹窗。 */ chatMetadata.closeNoteDialog}>
      <div role="dialog" aria-modal="true" aria-labelledby="buyer-note-title" className="flex max-h-[82vh] w-full max-w-lg flex-col overflow-hidden rounded-2xl bg-white shadow-2xl" onClick={/* 当前回调阻止备注内容点击冒泡到遮罩。 */ event => event.stopPropagation()}>
        <div className="flex items-center justify-between border-b border-slate-200 px-5 py-4"><div className="min-w-0"><div id="buyer-note-title" className="text-sm font-black text-slate-900">用户备注</div><div className="mt-0.5 truncate text-xs text-slate-400">{selectedSession?.buyer_name || selectedSession?.buyer_id || '当前用户'} · ID {selectedSession?.buyer_id || '-'}</div></div><button type="button" onClick={/* 当前回调关闭买家备注弹窗。 */ chatMetadata.closeNoteDialog} className="rounded-lg p-1.5 text-slate-400 transition hover:bg-slate-100 hover:text-slate-700" aria-label="关闭备注"><X className="h-5 w-5" /></button></div>
        <div className="min-h-0 flex-1 overflow-y-auto p-5">{chatMetadata.metadataError && <div className="mb-4 flex items-start justify-between gap-2 rounded-xl bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-700"><span>{chatMetadata.metadataError}</span><button type="button" onClick={/* 当前回调关闭当前快捷回复或备注错误提示。 */ chatMetadata.clearMetadataError} aria-label="关闭错误提示"><X className="h-4 w-4" /></button></div>}{chatMetadata.noteEditing ? <textarea autoFocus value={chatMetadata.noteDraft} onChange={/* 当前回调更新买家备注编辑表单。 */ event => chatMetadata.setNoteDraft(event.target.value)} maxLength={2000} rows={10} placeholder="记录该用户的沟通偏好、需求或重要事项…" className="min-h-52 w-full resize-y rounded-xl border border-slate-200 bg-slate-50 px-3 py-3 text-sm leading-6 outline-none transition focus:border-sky-400 focus:bg-white focus:ring-2 focus:ring-sky-100" /> : <div className="min-h-40 whitespace-pre-wrap break-words rounded-xl border border-slate-100 bg-slate-50/80 px-4 py-3 text-sm leading-6 text-slate-700">{chatMetadata.buyerNote?.content || '尚未添加备注。点击“编辑”即可记录该用户的信息。'}</div>}</div>
        <div className="flex items-center justify-end gap-2 border-t border-slate-200 px-5 py-3">{chatMetadata.noteEditing ? <><button type="button" onClick={/* 当前回调关闭备注弹窗并放弃未保存变更。 */ chatMetadata.closeNoteDialog} disabled={chatMetadata.noteSaving} className="rounded-xl px-4 py-2 text-sm font-bold text-slate-600 transition hover:bg-slate-100 disabled:opacity-50">取消</button><button type="button" onClick={/* 当前回调保存当前买家备注表单。 */ () => void chatMetadata.saveBuyerNote()} disabled={chatMetadata.noteSaving} className="rounded-xl bg-sky-500 px-4 py-2 text-sm font-bold text-white shadow-sm transition hover:bg-sky-600 disabled:cursor-not-allowed disabled:bg-slate-300">{chatMetadata.noteSaving ? '保存中…' : '保存'}</button></> : <button type="button" onClick={/* 当前回调将备注弹窗切换为编辑模式。 */ chatMetadata.beginNoteEditing} className="rounded-xl bg-sky-500 px-4 py-2 text-sm font-bold text-white shadow-sm transition hover:bg-sky-600">编辑</button>}</div>
      </div>
    </div>}
    {chatMetadata.pendingDeleteQuickReply && <div className="fixed inset-0 z-[60] flex items-center justify-center bg-slate-950/50 p-4" onClick={/* 当前回调点击遮罩放弃快捷回复删除。 */ chatMetadata.cancelQuickReplyDelete}><div role="alertdialog" aria-modal="true" aria-labelledby="quick-reply-delete-title" className="w-full max-w-sm rounded-2xl bg-white p-5 shadow-2xl" onClick={/* 当前回调阻止删除确认内容点击冒泡到遮罩。 */ event => event.stopPropagation()}><div id="quick-reply-delete-title" className="text-base font-black text-slate-900">删除快捷回复？</div><p className="mt-2 whitespace-pre-wrap break-words text-sm leading-6 text-slate-500">“{chatMetadata.pendingDeleteQuickReply.content}” 将从当前账号的快捷回复中移除。</p><div className="mt-5 flex justify-end gap-2"><button type="button" onClick={/* 当前回调关闭快捷回复删除确认框。 */ chatMetadata.cancelQuickReplyDelete} disabled={chatMetadata.quickReplyBusy} className="rounded-xl px-4 py-2 text-sm font-bold text-slate-600 transition hover:bg-slate-100 disabled:opacity-50">取消</button><button type="button" onClick={/* 当前回调确认并删除快捷回复。 */ () => void chatMetadata.confirmQuickReplyDelete()} disabled={chatMetadata.quickReplyBusy} className="rounded-xl bg-red-500 px-4 py-2 text-sm font-bold text-white transition hover:bg-red-600 disabled:cursor-not-allowed disabled:bg-red-300">{chatMetadata.quickReplyBusy ? '删除中…' : '删除'}</button></div></div></div>}
  </>;
};

export default ChatMetadataFeature;
