import { Edit3, FileStack, Plus, Save, Trash2, X } from 'lucide-react';
import React, { useCallback, useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { createDeliveryTemplate, deleteDeliveryTemplate, listDeliveryTemplates, updateDeliveryTemplate } from '../api';
import type { DeliveryTemplate, DeliveryTemplateDraft } from '../types';

// emptyDraft 创建一份可直接编辑的模板草稿。
const emptyDraft = (): DeliveryTemplateDraft => ({ name: '', enabled: true, messages: [{ content: '' }] });

/** 发货模板管理页面。 */
const DeliveryTemplates: React.FC = () => {
  // templates 保存当前用户的模板列表。
  const [templates, setTemplates] = useState<DeliveryTemplate[]>([]);
  // draft 保存弹窗中的模板编辑状态。
  const [draft, setDraft] = useState<DeliveryTemplateDraft>(emptyDraft);
  // editingID 保存正在编辑的模板 ID，空值表示新建。
  const [editingID, setEditingID] = useState<number | null>(null);
  // editorOpen 表示模板编辑器是否由用户明确打开，避免空白新建草稿被条件渲染误判为关闭。
  const [editorOpen, setEditorOpen] = useState(false);
  // loading 表示列表或保存请求是否进行中。
  const [loading, setLoading] = useState(true);

  // loadTemplates 读取模板列表并处理页面加载状态。
  const loadTemplates = useCallback(/* loadTemplatesCallback 在页面挂载和保存后刷新模板列表。 */ async () => {
    setLoading(true);
    try {
      setTemplates(await listDeliveryTemplates());
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(/* effect 在页面挂载时读取模板列表。 */ () => {
    void loadTemplates().catch(/* error 是模板列表读取失败原因。 */ error => alert(`读取发货模板失败：${(error as Error).message}`));
  }, [loadTemplates]);

  // openNewTemplate 打开空白模板编辑器。
  const openNewTemplate = (): void => {
    setEditingID(null);
    setDraft(emptyDraft());
    setEditorOpen(true);
  };

  // openTemplate 打开现有模板的可编辑副本。
  const openTemplate = (template: DeliveryTemplate): void => {
    setEditingID(template.id);
    setDraft({ name: template.name, enabled: template.enabled, messages: template.messages.map(/* message 是模板中的消息记录。 */ message => ({ content: message.content })) });
    setEditorOpen(true);
  };

  // closeEditor 关闭模板浮窗并丢弃尚未保存的表单修改。
  const closeEditor = (): void => {
    setEditingID(null);
    setDraft(emptyDraft());
    setEditorOpen(false);
  };

  // updateDraftName 更新模板名称表单值。
  const updateDraftName = (event /* event 是模板名称输入框的最新编辑事件。 */: React.ChangeEvent<HTMLInputElement>): void => {
    setDraft(/* current 是更新前的模板草稿。 */ current => ({ ...current, name: event.target.value }));
  };

  // updateDraftEnabled 更新模板是否允许自动化规则引用的开关。
  const updateDraftEnabled = (event /* event 是模板启用开关的最新编辑事件。 */: React.ChangeEvent<HTMLInputElement>): void => {
    setDraft(/* current 是更新前的模板草稿。 */ current => ({ ...current, enabled: event.target.checked }));
  };

  // updateMessageContent 更新指定顺序消息的正文并保留其他消息。
  const updateMessageContent = (index /* index 是待更新消息在模板中的顺序下标。 */: number, event /* event 是消息输入框的最新编辑事件。 */: React.ChangeEvent<HTMLTextAreaElement>): void => {
    setDraft(/* current 是更新前的模板草稿。 */ current => ({
      ...current,
      messages: current.messages.map(/* currentMessage 是待检查的模板消息。 */ (currentMessage, messageIndex) => messageIndex === index ? { content: event.target.value } : currentMessage),
    }));
  };

  // removeMessage 删除指定顺序的消息，至少保留一条消息输入框。
  const removeMessage = (index /* index 是待删除消息在模板中的顺序下标。 */: number): void => {
    setDraft(/* current 是更新前的模板草稿。 */ current => ({
      ...current,
      messages: current.messages.filter(/* currentMessageIndex 是待保留消息的顺序下标。 */ (_, currentMessageIndex) => currentMessageIndex !== index),
    }));
  };

  // addMessage 在模板末尾追加一条空白消息输入框。
  const addMessage = (): void => {
    setDraft(/* current 是更新前的模板草稿。 */ current => ({ ...current, messages: [...current.messages, { content: '' }] }));
  };

  // saveTemplate 校验并保存模板草稿。
  const saveTemplate = async (): Promise<void> => {
    // messages 保存去除空白消息后的提交内容。
    const messages = draft.messages.map(/* message 是待清理的模板消息草稿。 */ message => ({ content: message.content.trim() })).filter(/* message 是清理后的非空消息。 */ message => message.content.length > 0);
    if (!draft.name.trim() || messages.length === 0) {
      alert('请填写模板名称和至少一条消息');
      return;
    }
    setLoading(true);
    try {
      // nextDraft 保存清理后的模板提交草稿。
      const nextDraft = { ...draft, name: draft.name.trim(), messages };
      if (editingID === null) await createDeliveryTemplate(nextDraft);
      else await updateDeliveryTemplate(editingID, nextDraft);
      setEditingID(null);
      setDraft(emptyDraft());
      setEditorOpen(false);
      await loadTemplates();
    } catch (/* error 是模板保存失败原因。 */ error) {
      alert(`保存发货模板失败：${(error as Error).message}`);
      setLoading(false);
    }
  };

  // removeTemplate 删除用户确认的模板。
  const removeTemplate = async (id: number): Promise<void> => {
    if (!confirm('确定删除这个发货模板吗？被自动化规则引用的模板无法删除。')) return;
    try {
      await deleteDeliveryTemplate(id);
      await loadTemplates();
    } catch (/* error 是模板删除失败原因。 */ error) {
      alert(`删除发货模板失败：${(error as Error).message}`);
    }
  };

  return (
    <div className="space-y-8">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-xs font-black uppercase tracking-[0.24em] text-sky-600">Delivery templates</p>
          <h1 className="mt-2 text-3xl font-black tracking-tight text-slate-950">发货模板</h1>
          <p className="mt-2 text-sm text-slate-500">把多条消息和卡密变量组合成可复用的自动化发货内容。</p>
        </div>
        <button type="button" onClick={openNewTemplate} className="inline-flex items-center gap-2 rounded-xl bg-slate-950 px-4 py-3 text-sm font-bold text-white hover:bg-slate-800"><Plus className="h-4 w-4" />新建模板</button>
      </header>

      <div className="grid gap-4 md:grid-cols-2">
        {templates.map(/* template 是当前列表中的发货模板。 */ template => (
          <article key={template.id} className="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm">
            <div className="flex items-start justify-between gap-4">
              <div>
                <h2 className="font-black text-slate-900">{template.name}</h2>
                <p className="mt-1 text-xs text-slate-500">{template.messages.length} 条消息 · {template.enabled ? '已启用' : '已停用'}</p>
              </div>
              <FileStack className="h-5 w-5 text-sky-500" />
            </div>
            <div className="mt-4 space-y-2">
              {template.messages.map(/* message 是模板中的消息预览。 */ message => <div key={message.id} className="rounded-xl bg-slate-50 px-3 py-2 text-sm text-slate-600">{message.content}</div>)}
            </div>
            {((template.keys.length > 0) || (template.custom_keys || []).length > 0) && <p className="mt-3 text-xs leading-5 text-sky-700">变量：{template.keys.map(/* key 是模板中的变量键。 */ key => `{{cards.${key}}}`).concat((template.custom_keys || []).map(/* key 是模板中的自定义变量键。 */ key => `{{custom.${key}}}`)).join('、')}</p>}
            <div className="mt-5 flex gap-2">
              <button type="button" onClick={/* callback 打开当前模板编辑器。 */ () => openTemplate(template)} className="inline-flex items-center gap-1.5 rounded-lg bg-slate-100 px-3 py-2 text-xs font-bold text-slate-700"><Edit3 className="h-3.5 w-3.5" />编辑</button>
              <button type="button" onClick={/* callback 删除当前模板。 */ () => void removeTemplate(template.id)} className="inline-flex items-center gap-1.5 rounded-lg bg-red-50 px-3 py-2 text-xs font-bold text-red-600"><Trash2 className="h-3.5 w-3.5" />删除</button>
            </div>
          </article>
        ))}
        {!loading && templates.length === 0 && <div className="rounded-3xl border border-dashed border-slate-300 bg-white p-12 text-center text-sm text-slate-500 md:col-span-2">还没有发货模板，先创建一份吧。</div>}
      </div>

      {editorOpen && createPortal(
        <div className="modal-overlay" role="presentation">
          <div className="modal-container" style={{ maxWidth: '64rem' }} role="dialog" aria-modal="true" aria-labelledby="delivery-template-editor-title">
            <div className="modal-header flex items-start justify-between gap-4">
              <div>
                <p className="text-xs font-black uppercase tracking-[0.2em] text-sky-600">Delivery template editor</p>
                <h2 id="delivery-template-editor-title" className="mt-1 text-2xl font-black tracking-tight text-gray-950">{editingID === null ? '新建发货模板' : '编辑发货模板'}</h2>
                <p className="mt-1 text-sm font-medium text-gray-500">消息按顺序发送，卡密变量在自动化规则中绑定库存。</p>
              </div>
              <button type="button" onClick={closeEditor} className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-2xl bg-gray-100 transition-colors hover:bg-gray-200" aria-label="关闭编辑器"><X className="h-5 w-5 text-gray-600" /></button>
            </div>

            <div className="modal-body space-y-5">
              <div className="grid gap-5 lg:grid-cols-[minmax(0,1.15fr)_minmax(18rem,0.85fr)]">
                <section className="space-y-5" aria-label="模板内容编辑">
                  <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_auto]">
                    <div className="space-y-2">
                      <label htmlFor="delivery-template-name" className="block text-sm font-bold text-gray-800">模板名称</label>
                      <input id="delivery-template-name" value={draft.name} onChange={updateDraftName} placeholder="例如：数字产品发货" className="ios-input w-full rounded-xl px-4 py-3" />
                    </div>
                    <label className="flex items-center gap-2 self-end rounded-xl bg-gray-50 px-4 py-3 text-sm font-bold text-gray-700"><input type="checkbox" checked={draft.enabled} onChange={updateDraftEnabled} />启用模板</label>
                  </div>

                  <div className="space-y-3">
                    <div className="flex items-center justify-between gap-3"><div><h3 className="text-sm font-black text-gray-900">发送消息</h3><p className="mt-1 text-xs text-gray-500">每一行消息都会独立发送，顺序从上到下。</p></div><span className="rounded-full bg-sky-50 px-2.5 py-1 text-[11px] font-bold text-sky-700">{draft.messages.length} 条消息</span></div>
                    {draft.messages.map(/* message 是正在编辑的模板消息。 */ (message, index) => (
                      <div key={index} className="flex gap-2 rounded-2xl border border-gray-200 bg-gray-50/70 p-3">
                        <div className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-xl bg-white text-xs font-black text-gray-400">{index + 1}</div>
                        <textarea value={message.content} onChange={/* callback 更新当前消息正文。 */ event => updateMessageContent(index, event)} placeholder="例如：感谢购买，您的卡密是 {{cards.main}}" className="ios-input min-h-24 flex-1 resize-y rounded-xl bg-white px-4 py-3" />
                        <button type="button" disabled={draft.messages.length === 1} onClick={/* callback 删除当前消息。 */ () => removeMessage(index)} aria-label={`删除第 ${index + 1} 条消息`} className="self-start rounded-lg p-2 text-red-500 transition-colors hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-30"><Trash2 className="h-4 w-4" /></button>
                      </div>
                    ))}
                  </div>
                  <button type="button" onClick={addMessage} className="inline-flex items-center rounded-xl bg-gray-100 px-3 py-2 text-xs font-bold text-gray-700 transition-colors hover:bg-gray-200">+ 添加消息</button>
                </section>

                <aside className="space-y-4 rounded-2xl border border-sky-100 bg-sky-50/60 p-4" aria-labelledby="delivery-template-variable-guide">
                  <div>
                    <p className="text-[11px] font-black uppercase tracking-[0.18em] text-sky-600">Placeholder guide</p>
                    <h3 id="delivery-template-variable-guide" className="mt-1 text-base font-black text-sky-950">变量和占位符</h3>
                  </div>
                  <div className="rounded-xl border border-sky-100 bg-white/80 p-3">
                    <p className="text-xs font-bold text-gray-700">内置、卡密、自定义变量均用双大括号</p>
                    <p className="mt-2 text-xs leading-5 text-gray-600">直接写变量名即可，例如 <code className="rounded bg-white px-1 py-0.5 font-mono text-[11px] text-sky-700">{'{{order_id}}'}</code>；保存后系统会校验拼写。不支持空格、中文变量名或未闭合的大括号。</p>
                  </div>
                  <div className="space-y-2 rounded-xl border border-sky-100 bg-white/80 p-3 text-xs leading-5 text-gray-700">
                    <p><code className="font-mono text-sky-700">{'{{buyer_nickname}}'}</code>：购买用户昵称。</p>
                    <p><code className="font-mono text-sky-700">{'{{order_id}}'}</code>：订单号。</p>
                    <p><code className="font-mono text-sky-700">{'{{buyer_id}}'}</code>：买家 ID。</p>
                    <p><code className="font-mono text-sky-700">{'{{card_name}}'}</code>：当前模板绑定的卡密库存名称。</p>
                    <p><code className="font-mono text-sky-700">{'{{cards.<变量名>}}'}</code>：卡密内容；变量名只能使用英文字母、数字、下划线或短横线。</p>
                    <p><code className="font-mono text-sky-700">{'{{custom.<变量名>}}'}</code>：发货规则传入的自定义字符串；变量名与规则页 key 对应。</p>
                  </div>
                  <div className="space-y-2 text-xs leading-5 text-gray-600">
                    <p className="font-bold text-gray-800">如何声明和使用</p>
                    <ol className="list-decimal space-y-1.5 pl-5">
                      <li>直接在消息正文中写入占位符，例如 <code className="rounded bg-white px-1 py-0.5 font-mono text-[11px] text-sky-700">{'{{order_id}}'}</code>，不用填写 <code>delivery.</code> 前缀。</li>
                      <li>保存模板后，在自动化规则中选择该模板。</li>
                      <li>模板中的卡密变量需要在规则页分别绑定库存；自定义变量需要在规则页填写对应的 key 和字符串 value。</li>
                      <li>发货时系统会替换订单、库存、卡密和自定义字符串。</li>
                    </ol>
                  </div>
                  <div className="rounded-xl border border-amber-200 bg-amber-50 p-3 text-xs leading-5 text-amber-900">
                    <p className="font-bold">示例</p>
                    <pre className="mt-2 whitespace-pre-wrap font-mono text-[11px]">{'感谢购买！\n订单：{{order_id}}\n主卡：{{cards.main}}\n备注：{{custom.remark}}'}</pre>
                    <p className="mt-2">上例会要求在规则页绑定 <code className="font-mono">main</code> 卡密库存，并填写 <code className="font-mono">remark</code> 对应的字符串。</p>
                  </div>
                  <p className="text-[11px] leading-5 text-gray-500">买家昵称缺失时替换为空字符串；模板变量仅用于发货模板。</p>
                </aside>
              </div>
            </div>

            <div className="modal-footer flex items-center justify-end gap-3">
              <button type="button" onClick={closeEditor} className="rounded-xl bg-gray-100 px-5 py-2.5 text-sm font-bold text-gray-700 transition-colors hover:bg-gray-200">取消</button>
              <button type="button" onClick={/* callback 保存模板草稿。 */ () => void saveTemplate()} className="inline-flex items-center gap-2 rounded-xl bg-sky-600 px-5 py-2.5 text-sm font-bold text-white transition-colors hover:bg-sky-700"><Save className="h-4 w-4" />保存模板</button>
            </div>
          </div>
        </div>,
        document.body,
      )}
    </div>
  );
};

export default DeliveryTemplates;
