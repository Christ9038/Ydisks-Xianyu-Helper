import { AlertCircle, Edit, Plus, Trash2, X } from 'lucide-react';
import React, { useEffect } from 'react';
import { createPortal } from 'react-dom';
import { useItemGreetings } from '../itemGreetings';
import type { ItemGreetingRule } from '../api';
import ItemMultiSelect from './ItemMultiSelect';
import Toast from './Toast';
import { ImageSourceField } from '../../../../shared/ui/ImageSourceField';
import type { LibraryImageChooserComponent } from '../../../../shared/ui/imageSource';

// ITEM_GREETING_TOAST_MS 是操作提示的自动消失时长（毫秒）。
const ITEM_GREETING_TOAST_MS = 2500;

/** ItemGreetingPanelProps 描述商品首次回复面板需要的账号上下文。 */
interface ItemGreetingPanelProps {
  /** accountID 是规则页当前选择的账号；空串表示全部账号，此时不能新建规则。 */
  accountID: string;
  /** libraryImageChooser 是应用壳注入的图片库选择器；缺省时只能填写外链图片。 */
  libraryImageChooser?: LibraryImageChooserComponent;
}

/** ItemGreetingPanel 展示并编辑“买家首次咨询指定商品时自动发送一次”的回复规则。 */
const ItemGreetingPanel: React.FC<ItemGreetingPanelProps> = ({ accountID, libraryImageChooser }) => {
  // greetings 汇总规则列表、编辑草稿和写操作；所有请求都由 Hook 负责取消与过期丢弃。
  const greetings = useItemGreetings(accountID);
  // draft、toast 是编辑中的草稿与操作提示。
  const { draft, toast, dismissToast } = greetings;

  useEffect(/* 当前副作用在出现提示后定时清除，卸载或提示变化时清理计时器。 */ () => {
    if (!toast) return;
    // timer 是提示自动消失的计时器。
    const timer = window.setTimeout(dismissToast, ITEM_GREETING_TOAST_MS);
    return /* 当前回调清理未触发的提示计时器。 */ () => window.clearTimeout(timer);
  }, [toast, dismissToast]);

  // confirmDelete 在用户确认后删除规则。
  const confirmDelete = (rule: ItemGreetingRule) => {
    if (window.confirm(`确定删除首次回复规则“${rule.name || rule.reply_content.slice(0, 12)}”吗？`)) void greetings.remove(rule.id);
  };

  return (
    <section className="bg-white rounded-xl border border-gray-100 p-6 shadow-sm space-y-5">
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-3">
        <div className="flex items-center gap-2 text-sm text-blue-700 bg-blue-50 px-4 py-2 rounded-xl w-fit">
          <AlertCircle className="w-4 h-4" />
          买家就所选商品发来的第一条消息，会先收到这里的回复，再按关键词、AI、默认回复照常处理；同一会话只发送一次。
        </div>
        <div className="flex gap-2">
          {accountID && (
            <button
              onClick={/* 当前回调清空该账号的首次回复发送记录。 */ () => { if (window.confirm('清空后，买家下次咨询会再次收到首次回复，确定吗？')) void greetings.clearRecords(accountID); }}
              className="px-4 py-2.5 rounded-2xl text-sm font-bold text-blue-600 hover:bg-blue-50 transition-colors"
            >
              清空发送记录
            </button>
          )}
          <button
            onClick={/* 当前回调为当前账号打开新建编辑器。 */ () => greetings.openCreate(accountID)}
            disabled={!accountID}
            title={accountID ? undefined : '请先在右上角选择账号'}
            className="ios-btn-primary px-5 py-2.5 rounded-2xl text-sm font-extrabold flex items-center gap-2 disabled:opacity-50"
          >
            <Plus className="w-4 h-4" />
            新增首次回复
          </button>
        </div>
      </div>

      {greetings.loadError && <div className="text-sm text-red-600 bg-red-50 px-4 py-2 rounded-xl">加载失败：{greetings.loadError}</div>}

      <div className="space-y-3">
        {greetings.rules.map(/* 当前回调渲染单条首次回复规则。 */ rule => (
          <div key={rule.id} className={`flex flex-col md:flex-row md:items-center justify-between p-5 rounded-2xl border gap-4 ${rule.enabled ? 'border-purple-100 bg-purple-50/50' : 'border-gray-100 bg-surface-subtle'}`}>
            <div className="min-w-0">
              <div className="flex items-center gap-2 mb-2">
                <h3 className="font-bold text-gray-900 text-lg truncate">{rule.name || '未命名规则'}</h3>
                <span className={`px-2 py-0.5 rounded-lg text-xs font-bold ${rule.enabled ? 'bg-green-100 text-green-700' : 'bg-gray-200 text-gray-500'}`}>{rule.enabled ? '已启用' : '已停用'}</span>
                <span className="px-2 py-0.5 rounded-lg text-xs font-bold bg-purple-100 text-purple-700">{rule.item_ids.length} 个商品</span>
              </div>
              <div className="text-sm text-gray-600 line-clamp-2">{rule.reply_content || rule.reply_image_url || (rule.reply_library_image_id ? `图片库图片 #${rule.reply_library_image_id}` : '')}</div>
            </div>
            <div className="flex items-center gap-3">
              <button onClick={/* 当前回调切换启停。 */ () => void greetings.toggle(rule)} className="px-3 py-2 text-xs font-bold text-gray-600 hover:bg-gray-100 rounded-xl transition-colors">{rule.enabled ? '停用' : '启用'}</button>
              <button onClick={/* 当前回调打开编辑器。 */ () => greetings.openEdit(rule)} className="p-2 text-gray-400 hover:text-black hover:bg-gray-100 rounded-xl transition-colors" title="编辑"><Edit className="w-4 h-4" /></button>
              <button onClick={/* 当前回调确认后删除。 */ () => confirmDelete(rule)} className="p-2 text-gray-400 hover:text-red-500 hover:bg-red-50 rounded-xl transition-colors" title="删除"><Trash2 className="w-5 h-5" /></button>
            </div>
          </div>
        ))}
        {!greetings.loading && !greetings.loadError && greetings.rules.length === 0 && <div className="text-center py-20 text-gray-400">暂无商品首次回复规则</div>}
      </div>

      {draft && createPortal(
        <div className="modal-overlay">
          <div className="modal-container">
            <div className="modal-header">
              <div className="flex items-center justify-between w-full">
                <h3 className="text-2xl font-extrabold text-gray-900">{draft.id ? '编辑首次回复' : '新增首次回复'}</h3>
                <button onClick={greetings.closeEditor} className="p-2 bg-gray-100 rounded-full hover:bg-gray-200 transition-colors" title="关闭"><X className="w-5 h-5 text-gray-600" /></button>
              </div>
            </div>
            <div className="modal-body space-y-5">
              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">规则名称（可选）</label>
                <input value={draft.name} onChange={/* 当前回调更新规则名称。 */ event => greetings.setDraft({ ...draft, name: event.target.value })} className="w-full ios-input px-4 py-3 rounded-xl" placeholder="例如：手机类商品欢迎语" />
              </div>
              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">适用商品</label>
                <ItemMultiSelect options={greetings.draftItems} value={draft.item_ids} onChange={/* 当前回调更新商品集合。 */ itemIDs => greetings.setDraft({ ...draft, item_ids: itemIDs })} placeholder="选择需要首次回复的商品" />
                <p className="mt-2 text-xs text-gray-400">同一账号下，一个商品只能属于一条首次回复规则。</p>
              </div>
              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">回复内容</label>
                <textarea value={draft.reply_content} onChange={/* 当前回调更新回复文字。 */ event => greetings.setDraft({ ...draft, reply_content: event.target.value })} placeholder="例如：你好{send_user_name}，欢迎咨询，在的~" className="w-full ios-input px-4 py-3 rounded-xl h-32 resize-none" />
                <p className="mt-2 text-xs text-gray-400">支持变量：{'{send_user_name}'} {'{send_user_id}'} {'{send_message}'} {'{item_id}'}</p>
              </div>
              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">回复图片（可选）</label>
                <ImageSourceField value={{ url: draft.reply_image_url, libraryImageId: draft.reply_library_image_id }} onChange={/* source 是用户选择的新图片来源；外链与图片库图片互斥。 */ source => greetings.setDraft({ ...draft, reply_image_url: source.url, reply_library_image_id: source.libraryImageId })} chooser={libraryImageChooser} />
              </div>
              <label className="flex items-center justify-between p-4 bg-gray-50 rounded-xl text-sm font-bold text-gray-800">
                <span>启用此规则</span>
                <input type="checkbox" checked={draft.enabled} onChange={/* 当前回调更新启用状态。 */ event => greetings.setDraft({ ...draft, enabled: event.target.checked })} className="w-5 h-5" />
              </label>
            </div>
            <div className="modal-footer">
              <button onClick={greetings.closeEditor} className="px-5 py-3 rounded-2xl font-bold bg-gray-100 hover:bg-gray-200 text-gray-700">取消</button>
              <button onClick={/* 当前回调保存草稿。 */ () => void greetings.save()} disabled={greetings.submitState.submitting} className="ios-btn-primary px-6 py-3 rounded-2xl font-extrabold disabled:opacity-50">{greetings.submitState.submitting ? '保存中…' : '保存'}</button>
            </div>
          </div>
        </div>,
        document.body,
      )}
      <Toast toast={toast} />
    </section>
  );
};

export default ItemGreetingPanel;
