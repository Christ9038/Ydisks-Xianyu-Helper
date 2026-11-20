import { AlertCircle,Loader2,Trash2 } from 'lucide-react';
import React from 'react';

/** DeleteConversationDialogProps 描述本地会话删除确认框的展示状态和交互回调。 */
export interface DeleteConversationDialogProps {
  /** buyerName 是正文中展示的联系人名称。 */
  buyerName: string;
  /** deleting 表示删除请求正在执行，此时禁止关闭和重复提交。 */
  deleting: boolean;
  /** error 是最近一次删除失败的可重试提示。 */
  error: string;
  /** onCancel 在安全可关闭时退出确认框。 */
  onCancel: () => void;
  /** onConfirm 提交不可撤销的本地消息清空操作。 */
  onConfirm: () => void;
}

/** DeleteConversationDialog 用居中模态框确认本机会话隐藏与消息清空。 */
export const DeleteConversationDialog: React.FC<DeleteConversationDialogProps> = ({ buyerName, deleting, error, onCancel, onConfirm }) => {
  // dialogRef 指向确认框主体，用于约束键盘焦点。
  const dialogRef = React.useRef<HTMLDivElement | null>(null);
  // cancelButtonRef 指向默认聚焦的安全操作按钮。
  const cancelButtonRef = React.useRef<HTMLButtonElement | null>(null);
  // previousFocusRef 保存弹窗打开前的焦点元素，关闭后恢复会话列表上下文。
  const previousFocusRef = React.useRef<HTMLElement | null>(null);
	// deletingRef 让只挂载一次的键盘监听读取最新提交锁定状态。
	const deletingRef = React.useRef(deleting);
	// onCancelRef 让只挂载一次的键盘监听调用最新关闭回调。
	const onCancelRef = React.useRef(onCancel);
	deletingRef.current = deleting;
	onCancelRef.current = onCancel;

  React.useEffect(/* 当前副作用管理默认焦点、Escape、Tab 环绕和关闭后的焦点恢复。 */ () => {
    previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    // focusFrame 等待确认框 DOM 完成挂载后聚焦“取消”。
    const focusFrame = window.requestAnimationFrame(/* 当前回调把默认焦点放到不会误删数据的按钮。 */ () => cancelButtonRef.current?.focus());
    /** handleKeyDown 处理确认框键盘关闭和焦点约束。 */
    const handleKeyDown = (event: KeyboardEvent): void => {
      if (event.key === 'Escape') {
        if (!deletingRef.current) {
          event.preventDefault();
          onCancelRef.current();
        }
        return;
      }
      if (event.key !== 'Tab' || !dialogRef.current) return;
      // focusable 保存确认框内当前未禁用的键盘交互元素。
      const focusable = [...dialogRef.current.querySelectorAll<HTMLElement>('button:not([disabled])')];
      if (focusable.length === 0) {
        event.preventDefault();
        return;
      }
      // first 是焦点循环的起点。
      const [first] = focusable;
      // last 是焦点循环的终点。
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
    return /* cleanup 取消延迟聚焦、移除监听并恢复此前焦点。 */ () => {
      window.cancelAnimationFrame(focusFrame);
      document.removeEventListener('keydown', handleKeyDown);
      previousFocusRef.current?.focus();
    };
  }, []);

  return (
    <div className="fixed inset-0 z-[80] flex items-center justify-center bg-black/50 p-4 backdrop-blur-[2px]" onMouseDown={/* 当前回调只在未提交时允许点击遮罩关闭。 */ event => { if (!deleting && event.target === event.currentTarget) onCancel(); }}>
      <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby="delete-conversation-title" aria-describedby="delete-conversation-description" className="w-full max-w-md overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl">
        <div className="flex items-start gap-4 px-6 pb-4 pt-6">
          <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl bg-red-50 text-red-600"><Trash2 className="h-5 w-5" /></div>
          <div className="min-w-0">
            <h2 id="delete-conversation-title" className="text-lg font-black tracking-tight text-slate-950">删除会话</h2>
            <p id="delete-conversation-description" className="mt-2 text-sm leading-6 text-slate-600">将从本机清空与「{buyerName}」的聊天消息。订单、自动化和 AI 记忆不会删除；收到新消息后会话会重新出现。此操作无法撤销。</p>
          </div>
        </div>
        {error && <div role="alert" className="mx-6 flex items-start gap-2 rounded-xl bg-red-50 px-3 py-2.5 text-xs font-medium leading-5 text-red-700"><AlertCircle className="mt-0.5 h-4 w-4 shrink-0" /><span>{error}</span></div>}
        <div className="mt-2 flex justify-end gap-2 border-t border-slate-100 bg-slate-50/70 px-6 py-4">
          <button ref={cancelButtonRef} type="button" onClick={onCancel} disabled={deleting} className="rounded-xl border border-slate-200 bg-white px-4 py-2 text-sm font-bold text-slate-700 transition hover:bg-slate-50 focus:outline-none focus:ring-4 focus:ring-slate-200 disabled:cursor-not-allowed disabled:opacity-50">取消</button>
          <button type="button" onClick={onConfirm} disabled={deleting} className="flex min-w-24 items-center justify-center gap-2 rounded-xl bg-red-600 px-4 py-2 text-sm font-bold text-white transition hover:bg-red-700 focus:outline-none focus:ring-4 focus:ring-red-100 disabled:cursor-not-allowed disabled:bg-red-400">
            {deleting && <Loader2 className="h-4 w-4 animate-spin" />}{deleting ? '删除中' : '确认删除'}
          </button>
        </div>
      </div>
    </div>
  );
};

export default DeleteConversationDialog;
