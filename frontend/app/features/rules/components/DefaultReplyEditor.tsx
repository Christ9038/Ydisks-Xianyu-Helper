import { Save,X } from 'lucide-react';
import { useEffect,useRef } from 'react';
import { createPortal } from 'react-dom';
import type { DefaultReplyActionsState } from '../defaultReplyActions';
import { defaultReplyImageSource } from '../defaultReplyState';
import type { AccountDetail,Item } from '../models';
import { accountLabel } from '../utils';

/** DefaultReplyEditorProps 仅接收默认回复业务状态和本地参考数据，不直接调用 HTTP。 */
interface DefaultReplyEditorProps {
  /** actions 拥有独立草稿及可取消代次的读写动作。 */
  actions: DefaultReplyActionsState;
  /** accounts 是当前用户有权操作的账号。 */
  accounts: AccountDetail[];
  /** items 是已经同步到本地的商品列表。 */
  items: Item[];
}

/** DefaultReplyEditor 编辑账号兜底或商品图文；本地图片只展示引用，不交给浏览器加载。 */
export default function DefaultReplyEditor({ actions,accounts,items }: DefaultReplyEditorProps) {
  /** dialogRef 用于打开后移动焦点和弹窗内部的键盘循环，不保存业务状态。 */
  const dialogRef = useRef<HTMLDivElement>(null);
  useEffect(/* 当前副作用只在打开和关闭时转移焦点，异步加载不抢回用户焦点。 */ () => {
    if (!actions.showDefaultModal) return;
    // previousFocus 是打开编辑器前的按钮，关闭时恢复键盘位置。
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    dialogRef.current?.querySelector<HTMLElement>('select')?.focus();
    return /* 当前 cleanup 将焦点还给仍存在的触发控件。 */ () => { if (previousFocus?.isConnected) previousFocus.focus(); };
  }, [actions.showDefaultModal]);
  useEffect(/* 加载、重试或保存移除/禁用焦点控件时，恢复到持续存在的范围选择器；不抢占有效焦点。 */ () => {
    if (!actions.showDefaultModal) return;
    // dialog 是当前仍挂载的编辑器；focused 是可能因重试按钮移除而落到 BODY 的焦点。
    const dialog = dialogRef.current;
    // focused 保存当前文档焦点，不能假设旧控件仍属于弹窗。
    const focused = document.activeElement;
    if (dialog && (!(focused instanceof HTMLElement) || !dialog.contains(focused) || focused.matches(':disabled'))) {
      dialog.querySelector<HTMLElement>('select')?.focus();
    }
  }, [actions.showDefaultModal, actions.defaultReplyLoading, actions.defaultReplyLoadFailed, actions.defaultReplySubmitState.submitting, actions.defaultReplyMutationBusy]);
  /** form 是当前范围的受控草稿，由动作 Hook 负责切换时重置。 */
  const form = actions.defaultForm;
  /** isItem 区分独立商品配置与带开关、会话去重的账号兜底。 */
  const isItem = form.scope === 'item';
  /** imageSource 是显式选择或从旧配置推断的图片输入方式。 */
  const imageSource = defaultReplyImageSource(form);
  /** accountItems 只允许当前账号的已同步商品进入选择器。 */
  const accountItems = items.filter(/* item 是当前待检查归属的本地商品。 */ item => item.cookie_id === form.cookie_id);
  /** locked 在加载、加载失败和保存期间锁住正文，避免提交尚未读取的空配置。 */
  const locked = actions.defaultReplyLoading || actions.defaultReplyLoadFailed || actions.defaultReplySubmitState.submitting || actions.defaultReplyMutationBusy;
  if (!actions.showDefaultModal) return null;
  return createPortal(
    <div className="modal-overlay" onKeyDown={/* event 是编辑器键盘操作；Tab 留在弹窗内，Escape 撤销当前编辑器。 */ event => {
      if (event.key === 'Escape') { event.stopPropagation(); actions.setShowDefaultModal(false); }
      if (event.key !== 'Tab') return;
      // controls 只包含当前可用的表单控件，fieldset 禁用的子控件同样排除。
      const controls = Array.from(dialogRef.current?.querySelectorAll<HTMLElement>('button,select,input,textarea') || []).filter(/* control 是待参与焦点循环的控件。 */ control => !control.matches(':disabled'));
      // first/last 是正向和反向循环的边界控件。
      const first = controls[0];
      // last 是正向 Tab 到达的末个可操作控件。
      const last = controls[controls.length - 1];
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
    }}>
      <div ref={dialogRef} className="modal-container" role="dialog" aria-modal="true" aria-labelledby="default-reply-title">
        <div className="modal-header flex items-center justify-between">
          <div>
            <h3 id="default-reply-title" className="text-2xl font-extrabold text-gray-900">{isItem ? '商品默认回复' : '账号默认回复'}</h3>
            <p className="text-sm text-gray-500 mt-1">关键词和 AI 均未处理时，商品专属配置优先于账号兜底。</p>
          </div>
          <button type="button" aria-label="关闭默认回复编辑器" onClick={/* 当前回调取消编辑并隔离在途请求。 */ () => actions.setShowDefaultModal(false)} className="p-2 bg-gray-100 rounded-full hover:bg-gray-200"><X className="w-5 h-5" /></button>
        </div>
        <div className="modal-body space-y-5">
          <label className="block text-sm font-bold text-gray-700">
            配置范围
            <select value={isItem ? 'item' : 'account'} onChange={/* event 指定新的配置范围，必须重新读取而非复制旧草稿。 */ event => void (event.target.value === 'item' ? actions.openItemDefaultReplyModal(form.cookie_id) : actions.openDefaultReplyModal(form.cookie_id))} className="w-full ios-input px-4 py-3 rounded-xl mt-2">
              <option value="account">账号兜底</option><option value="item">商品专属</option>
            </select>
          </label>
          <label className="block text-sm font-bold text-gray-700">
            闲鱼账号
            <select value={form.cookie_id} onChange={/* event 指定新账号，商品范围切换时同时清空旧商品选择。 */ event => void (isItem ? actions.openItemDefaultReplyModal(event.target.value) : actions.openDefaultReplyModal(event.target.value))} className="w-full ios-input px-4 py-3 rounded-xl mt-2">
              {accounts.map(/* account 是当前用户拥有的账号摘要。 */ account => <option key={account.id} value={account.id}>{accountLabel(account)}</option>)}
            </select>
          </label>
          {isItem && <>
            <label className="block text-sm font-bold text-gray-700">
              关联商品
              <select value={form.item_id || ''} onChange={/* event 指定当前账号下要读取配置的商品。 */ event => void actions.openItemDefaultReplyModal(form.cookie_id, event.target.value)} className="w-full ios-input px-4 py-3 rounded-xl mt-2">
                <option value="">选择已同步的商品</option>
                {form.item_id && !accountItems.some(/* item 用于判断历史配置的商品是否仍在本地列表。 */ item => item.item_id === form.item_id) && <option value={form.item_id}>{form.item_id}（不在本地商品列表）</option>}
                {accountItems.map(/* item 是当前账号已同步的商品候选。 */ item => <option key={item.item_id} value={item.item_id}>{item.item_title || item.item_id} · {item.item_id}</option>)}
              </select>
            </label>
            <p className="text-xs leading-5 text-blue-700 bg-blue-50 rounded-xl p-3">商品配置独立生效，不继承账号启用或去重开关。可勾选“单个会话只回复一次”，同一商品在同一会话只回复一次。正文和图片都留空，或删除配置，即恢复账号兜底；不会附加账号的图片。</p>
          </>}
          {actions.defaultReplyLoading && <p role="status" className="text-sm text-gray-500">正在读取默认回复…</p>}
          {actions.defaultReplyError && <div role="alert" className="text-sm text-red-700 bg-red-50 p-3 rounded-xl">
            {actions.defaultReplyError}
            {actions.defaultReplyLoadFailed && <button type="button" className="ml-3 font-bold underline" onClick={/* 当前回调重新读取失败目标，不把空草稿当成新建配置。 */ () => void (isItem ? actions.openItemDefaultReplyModal(form.cookie_id, form.item_id) : actions.openDefaultReplyModal(form.cookie_id))}>重新加载</button>}
          </div>}
          <fieldset disabled={locked || (isItem && !form.item_id)} className="space-y-5 disabled:opacity-60">
            {!isItem && <label className="flex items-center justify-between p-4 bg-gray-50 rounded-xl text-sm font-bold text-gray-800">
              启用账号默认回复
              <input type="checkbox" checked={form.enabled} onChange={/* event 是用户对账号兜底开关的选择。 */ event => actions.setDefaultForm(/* current 是当前作用域草稿，不修改其他商品或账号设置。 */ current => ({ ...current, enabled: event.target.checked }))} className="w-4 h-4" />
            </label>}
            <label className="block text-sm font-bold text-gray-700">
              回复内容
              <textarea value={form.reply_content} onChange={/* event 提供未裁剪的回复正文。 */ event => actions.setDefaultForm(/* current 是尚未保存的图文草稿。 */ current => ({ ...current, reply_content: event.target.value }))} placeholder="输入默认回复内容，也可以只发送图片" className="w-full ios-input px-4 py-3 rounded-xl h-32 resize-none mt-2" />
            </label>
            <label className="block text-sm font-bold text-gray-700">
              图片来源
              <select value={imageSource} onChange={/* event 是用户选中的图片输入方式，切换后清除两类旧输入。 */ event => actions.setDefaultForm(/* current 是当前图文草稿。 */ current => ({ ...current, image_source: event.target.value as 'none' | 'url' | 'local', reply_image_url: '', reply_image_path: '' }))} className="w-full ios-input px-4 py-3 rounded-xl mt-2">
                <option value="none">不发送图片</option><option value="url">图片 URL</option><option value="local">服务器本地图片</option>
              </select>
            </label>
            {imageSource === 'url' && <label className="block text-sm font-bold text-gray-700">
              回复图片 URL
              <input type="url" value={form.reply_image_url} onChange={/* event 提供远程图片地址，不发起浏览器预览请求。 */ event => actions.setDefaultForm(/* current 是当前图文草稿。 */ current => ({ ...current, reply_image_url: event.target.value }))} placeholder="https://example.com/image.jpg" className="w-full ios-input px-4 py-3 rounded-xl mt-2" />
            </label>}
            {imageSource === 'local' && <div className="space-y-2">
              <label className="block text-sm font-bold text-gray-700">
                本地图片相对路径
                <input type="text" value={form.reply_image_path || ''} onChange={/* event 提供程序所在机器的相对文件引用。 */ event => actions.setDefaultForm(/* current 是当前图文草稿。 */ current => ({ ...current, reply_image_path: event.target.value }))} placeholder="product-a/reply.jpg" className="w-full ios-input px-4 py-3 rounded-xl mt-2" aria-describedby="default-reply-image-help" />
              </label>
              <p id="default-reply-image-help" className="text-xs leading-5 text-gray-500 break-all">把图片放到运行程序机器的 <code>XIANYU_UPLOAD_DIR/reply-images/{form.cookie_id}/</code> 内；未设置上传目录时使用 <code>data/uploads</code>。这里只填相对路径，使用 / 分隔。Docker 需挂载该目录；不是选择浏览器电脑的文件。每次实际发送时读取一张 PNG、JPEG 或 GIF 图片，最大 10 MiB，读取失败不会只发文字。</p>
            </div>}
            <label className="flex items-center justify-between p-4 bg-gray-50 rounded-xl text-sm font-bold text-gray-800">
              <span>{isItem ? '单个会话只回复一次' : '只回复一次'}<span className="block text-xs text-gray-500 font-medium mt-1">{isItem ? '同一账号、商品、会话独立去重，不影响账号兜底或其他商品；修改内容、关闭再开启均不会重置已发送记录。' : '同一账号、同一会话只发送一次账号兜底；修改内容不会自动清空记录。'}</span></span>
              <input type="checkbox" checked={form.reply_once} onChange={/* event 是用户对当前商品或账号会话去重的独立选择。 */ event => actions.setDefaultForm(/* current 是当前作用域草稿，不修改其他商品或账号设置。 */ current => ({ ...current, reply_once: event.target.checked }))} className="w-4 h-4" />
            </label>
          </fieldset>
          <div className="flex gap-3 pt-4">
            <button type="button" onClick={/* 当前回调取消编辑，不触发保存。 */ () => actions.setShowDefaultModal(false)} className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200">取消</button>
            <button type="button" disabled={locked || (isItem && !form.item_id)} onClick={/* 当前回调保存当前账号或商品的独立草稿。 */ () => void actions.handleSaveDefaultReply()} className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2 disabled:opacity-50"><Save className="w-4 h-4" />{actions.defaultReplySubmitState.submitting ? '正在保存…' : '保存默认回复'}</button>
          </div>
        </div>
      </div>
    </div>, document.body,
  );
}
