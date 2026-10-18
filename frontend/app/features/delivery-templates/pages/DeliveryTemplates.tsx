import { Edit3, FileStack, Plus, Save, Trash2, X } from 'lucide-react';
import React, { useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { TemplateMessageEditor } from '../components/TemplateMessageEditor';
import { TemplateVariableGuide } from '../components/TemplateVariableGuide';
import { useDeliveryTemplates } from '../hooks';
import { deliveryTemplatePayload, emptyTemplateMessage, validateTemplateImages } from '../templateState';
import type { DeliveryTemplate, DeliveryTemplateDraft, DeliveryTemplateMessageDraft } from '../types';

// emptyDraft 创建一份可直接编辑的模板草稿。
const emptyDraft = (): DeliveryTemplateDraft => ({ name: '', enabled: true, messages: [emptyTemplateMessage()] });

/** 发货模板管理页面。 */
const DeliveryTemplates: React.FC = () => {
  // templates、loading、saving、requestError 由 Hook 统一管理请求生命周期和竞态保护。
  const { templates, loading, saving, error: requestError, loadTemplates, saveTemplate: persistTemplate, cancelSave, removeTemplate: deleteTemplate } = useDeliveryTemplates();
  // draft 保存弹窗中的模板编辑状态。
  const [draft, setDraft] = useState<DeliveryTemplateDraft>(emptyDraft);
  // editingID 保存正在编辑的模板 ID，空值表示新建。
  const [editingID, setEditingID] = useState<number | null>(null);
  // editorOpen 表示模板编辑器是否由用户明确打开，避免空白新建草稿被条件渲染误判为关闭。
  const [editorOpen, setEditorOpen] = useState(false);
  // editorGenerationRef 防止已关闭草稿的保存结果关闭后续打开的编辑器。
  const editorGenerationRef = useRef(0);
  // dialogRef 用于初始焦点与键盘循环。
  const dialogRef = useRef<HTMLDivElement>(null);
  // pendingMessageFocusRef 只记录本次添加待聚焦的草稿身份，节点提交后立即消费，不影响排序焦点。
  const pendingMessageFocusRef = useRef<number | undefined>(undefined);
  React.useEffect(/* 当前副作用在打开时聚焦名称、关闭时恢复触发按钮。 */ () => {
    if (!editorOpen) return;
    // previousFocus 是用户打开编辑器前的焦点。
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    dialogRef.current?.querySelector<HTMLInputElement>('input')?.focus();
    return /* 当前清理函数将焦点还给仍存在的页面按钮。 */ () => { if (previousFocus?.isConnected) previousFocus.focus(); };
  }, [editorOpen]);
  React.useEffect(/* 当前副作用登记卸载后的保存失效边界。 */ () => /* 清理函数阻止旧保存继续提示或关闭编辑器。 */ () => { ++editorGenerationRef.current; }, []);
  // 首屏加载由 Hook 自动触发；页面只负责展示真实错误。
  React.useEffect(/* 当前副作用在页面挂载时加载模板，并在卸载后由 Hook 取消请求。 */ () => { void loadTemplates().catch(/* error 是首屏列表请求失败原因，Hook 已保存用户可见错误。 */ () => undefined); }, [loadTemplates]);

  // openNewTemplate 打开空白模板编辑器。
  const openNewTemplate = (): void => {
    ++editorGenerationRef.current;
    cancelSave();
    setEditingID(null);
    setDraft(emptyDraft());
    setEditorOpen(true);
  };

  // openTemplate 打开现有模板的可编辑副本。
  const openTemplate = (template: DeliveryTemplate): void => {
    ++editorGenerationRef.current;
    cancelSave();
    setEditingID(template.id);
    setDraft({ name: template.name, enabled: template.enabled, messages: template.messages.map(/* message 复制独立草稿并兼容历史文本缺省类型。 */ message => ({ ...emptyTemplateMessage(message.type || 'text'), content: message.content, image_url: message.image_url || '', image_path: message.image_path || '' })) });
    setEditorOpen(true);
  };

  // closeEditor 关闭模板浮窗并丢弃尚未保存的表单修改。
  const closeEditor = (): void => {
    ++editorGenerationRef.current;
    cancelSave();
    setEditingID(null);
    setDraft(emptyDraft());
    pendingMessageFocusRef.current = undefined;
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

  // updateMessage 替换一条独立消息，不在图片字段中解析文本变量。
  const updateMessage = (index: number, message: DeliveryTemplateMessageDraft): void => {
    setDraft(/* current 是更新前的完整模板草稿。 */ current => ({
      ...current,
      messages: current.messages.map(/* currentMessage 是待保留或替换的消息。 */ (currentMessage, messageIndex) => messageIndex === index ? { ...message, editor_key: currentMessage.editor_key } : currentMessage),
    }));
  };

  // moveMessage 按发送顺序交换两条完整消息，图片来源随条目一起移动。
  const moveMessage = (index: number, direction: -1 | 1): void => {
    setDraft(/* current 用于避免连续移动使用旧顺序。 */ current => {
      // target 是相邻目标位置，禁止越界。
      const target = index + direction;
      if (target < 0 || target >= current.messages.length) return current;
      // messages 是独立顺序副本，不能修改原列表或后端模型。
      const messages = [...current.messages];
      [messages[index], messages[target]] = [messages[target], messages[index]];
      return { ...current, messages };
    });
  };

  // removeMessage 删除指定顺序的消息，至少保留一条消息输入框。
  const removeMessage = (index /* index 是待删除消息在模板中的顺序下标。 */: number): void => {
    setDraft(/* current 是更新前的模板草稿。 */ current => ({
      ...current,
      messages: current.messages.filter(/* currentMessageIndex 是待保留消息的顺序下标。 */ (_, currentMessageIndex) => currentMessageIndex !== index),
    }));
  };

  // addMessage 仅在未保存时追加文本草稿，记录身份让提交后的节点聚焦并滚入正文视口。
  const addMessage = (): void => {
    // message 是本次追加的独立草稿，稳定身份避免把排序或重新打开误判为添加。
    const message = emptyTemplateMessage();
    pendingMessageFocusRef.current = message.editor_key;
    setDraft(/* current 是更新前的模板草稿，函数式追加保留连续添加的每一条。 */ current => ({ ...current, messages: [...current.messages, message] }));
  };

  // saveTemplate 校验并保存模板草稿。
  const saveTemplate = async (): Promise<void> => {
    if (saving) return;
    // imageError 在过滤空文本前检查图片，避免把配置错误的图片静默删除。
    const imageError = validateTemplateImages(draft.messages);
    if (imageError) { alert(imageError); return; }
    // messages 只过滤空文本消息；纯图片模板同样有效。
    const messages = deliveryTemplatePayload(draft).messages.filter(/* message 是标准化后的消息。 */ message => message.type === 'image' || message.content.length > 0);
    if (!draft.name.trim() || messages.length === 0) {
      alert('请填写模板名称和至少一条消息');
      return;
    }
    // generation 是本次保存所属的编辑会话。
    const generation = editorGenerationRef.current;
    try {
      // nextDraft 保存清理后的模板提交草稿。
      const nextDraft = { ...draft, name: draft.name.trim(), messages };
      await persistTemplate(editingID, nextDraft);
      if (generation !== editorGenerationRef.current) return;
      setEditingID(null);
      setDraft(emptyDraft());
      setEditorOpen(false);
    } catch (/* error 是模板保存失败原因。 */ error) {
      if (generation !== editorGenerationRef.current) return;
      alert(`保存发货模板失败：${(error as Error).message}`);
    }
  };

  // removeTemplate 删除用户确认的模板。
  const removeTemplate = async (id: number): Promise<void> => {
    if (!confirm('确定删除这个发货模板吗？被自动化规则引用的模板无法删除。')) return;
    try {
      await deleteTemplate(id);
    } catch (/* error 是模板删除失败原因。 */ error) {
      alert(`删除发货模板失败：${(error as Error).message}`);
    }
  };

  return (
    <div className="space-y-8">
      {requestError && <div role="alert" className="rounded-xl border border-red-100 bg-red-50 px-4 py-3 text-sm text-red-700">{requestError}</div>}
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
              {template.messages.map(/* message 是模板中的消息预览。 */ message => <div key={message.id} className="rounded-xl bg-slate-50 px-3 py-2 text-sm text-slate-600">{message.type === 'image' ? `图片 · ${message.image_path ? `本地路径：${message.image_path}` : `URL：${message.image_url || ''}`}` : message.content}</div>)}
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
        <div className="modal-overlay" role="presentation" onKeyDown={/* event 支持 Esc 撤销和 Tab 焦点循环。 */ event => {
          if (event.key === 'Escape') { event.stopPropagation(); closeEditor(); }
          if (event.key !== 'Tab') return;
          // controls 只包括弹窗内仍可操作的控件。
          const controls = Array.from(dialogRef.current?.querySelectorAll<HTMLElement>('button,input,select,textarea') || []).filter(/* control 排除保存中被禁用的字段。 */ control => !control.matches(':disabled'));
          // first 是逆向焦点循环的起点。
          const first = controls[0];
          // last 是正向焦点循环的终点。
          const last = controls[controls.length - 1];
          if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
          else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
        }}>
          <div ref={dialogRef} className="modal-container delivery-template-editor" role="dialog" aria-modal="true" aria-labelledby="delivery-template-editor-title">
            <div className="modal-header delivery-template-editor__header">
              <div className="min-w-0">
                <p className="text-xs font-black uppercase tracking-[0.2em] text-sky-600">Delivery template editor</p>
                <h2 id="delivery-template-editor-title" className="mt-1 truncate text-2xl font-black tracking-tight text-gray-950">{editingID === null ? '新建发货模板' : '编辑发货模板'}</h2>
                <p className="mt-1 text-sm font-medium text-gray-500">消息按顺序发送，卡密变量在自动化规则中绑定库存。</p>
              </div>
              <button type="button" onClick={closeEditor} className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-2xl bg-gray-100 transition-colors hover:bg-gray-200" aria-label="关闭编辑器"><X className="h-5 w-5 text-gray-600" /></button>
            </div>

            {/* 普通块负责受限高度和滚动；fieldset 仅负责保存时禁用，避免其特殊布局吞掉滚动。 */}
            <div className="delivery-template-editor__body">
              <fieldset disabled={saving} className="delivery-template-editor__fields disabled:opacity-60">
                <div className="delivery-template-editor__main">
                  <section className="delivery-template-editor__content" aria-label="模板内容编辑">
                    <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_auto]">
                      <div className="space-y-2">
                        <label htmlFor="delivery-template-name" className="block text-sm font-bold text-gray-800">模板名称</label>
                        <input id="delivery-template-name" value={draft.name} onChange={updateDraftName} placeholder="例如：数字产品发货" className="ios-input w-full rounded-xl px-4 py-3" />
                      </div>
                      <label className="flex items-center gap-2 self-end rounded-xl bg-gray-50 px-4 py-3 text-sm font-bold text-gray-700"><input type="checkbox" checked={draft.enabled} onChange={updateDraftEnabled} />启用模板</label>
                    </div>

                    <div className="space-y-3">
                      <div className="flex items-center justify-between gap-3"><div><h3 className="text-sm font-black text-gray-900">发送消息</h3><p className="mt-1 text-xs text-gray-500">每一行消息都会独立发送，顺序从上到下。</p></div><span className="rounded-full bg-sky-50 px-2.5 py-1 text-[11px] font-bold text-sky-700">{draft.messages.length} 条消息</span></div>
                      <ol className="space-y-3" aria-label="消息列表">
                        {draft.messages.map(/* message 是独立草稿；index 是当前发送顺序，身份仍由 editor_key 保持。 */ (message, index) => (
                          <li key={message.editor_key} ref={/* element 是当前消息节点，只对用户刚添加的草稿聚焦并滚入视口，避免原生聚焦只露出部分输入框。 */ element => {
                            if (element && pendingMessageFocusRef.current === message.editor_key) {
                              pendingMessageFocusRef.current = undefined;
                              element.querySelector('textarea')?.focus();
                              element.scrollIntoView({ block: 'nearest' });
                            }
                          }}>
                            <TemplateMessageEditor message={message} index={index} count={draft.messages.length}
                              onChange={/* next 保留其他消息草稿。 */ next => updateMessage(index, next)}
                              onMove={/* direction 改变实际发送顺序。 */ direction => moveMessage(index, direction)}
                              onRemove={/* 当前回调删除当前顺序消息。 */ () => removeMessage(index)} />
                          </li>
                        ))}
                      </ol>
                    </div>
                  </section>

                  <aside className="delivery-template-editor__guide">
                    <TemplateVariableGuide />
                  </aside>
                </div>
              </fieldset>
            </div>

            <div className="modal-footer delivery-template-editor__footer">
              <button type="button" disabled={saving} onClick={addMessage} className="delivery-template-editor__add inline-flex items-center justify-center gap-2 rounded-xl border border-dashed border-gray-300 bg-white px-4 py-2.5 text-sm font-bold text-gray-600 transition-colors hover:border-sky-400 hover:text-sky-700 disabled:opacity-50"><Plus className="h-4 w-4" />添加消息</button>
              <button type="button" onClick={closeEditor} className="rounded-xl bg-gray-100 px-5 py-2.5 text-sm font-bold text-gray-700 transition-colors hover:bg-gray-200">取消</button>
              <button type="button" disabled={saving} onClick={/* callback 保存模板草稿。 */ () => void saveTemplate()} className="inline-flex items-center gap-2 rounded-xl bg-sky-600 px-5 py-2.5 text-sm font-bold text-white transition-colors hover:bg-sky-700 disabled:opacity-50"><Save className="h-4 w-4" />保存模板</button>
            </div>
          </div>
        </div>,
        document.body,
      )}
    </div>
  );
};

export default DeliveryTemplates;
