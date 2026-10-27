import { ChevronDown, ChevronUp, ImagePlus, Trash2, X } from 'lucide-react';
import { useMemo, useState } from 'react';
import { createPortal } from 'react-dom';
import { PublishSpecsEditor } from '../../../../shared/ui/publish/PublishSpecsEditor';
import { libraryImageUrl, type TemplateRequestTransport } from '../api';
import type { AutomationOptions } from '../hooks';
import { draftFromTemplate, emptyDraft, reconcileDraftSpecs, specScopes, toRequest, validateDraft, type TemplateDraft } from '../templateDraft';
import type { ProductTemplate } from '../types';
import { ImagePickerDialog } from './ImagePickerDialog';
import { TemplateAutomationEditor } from './TemplateAutomationEditor';

/** TemplateEditorDialogProps 描述模板编辑弹窗。 */
export interface TemplateEditorDialogProps {
  /** template 是被编辑的模板，空表示新建。 */
  template: ProductTemplate | null;
  /** busy 表示保存进行中。 */
  busy: boolean;
  /** options 是自动化可选项。 */
  options: AutomationOptions;
  /** onSave 提交请求体，成功后由父级关闭弹窗。 */
  onSave: (body: TemplateRequestTransport) => Promise<void>;
  /** onClose 关闭弹窗并丢弃草稿。 */
  onClose: () => void;
}

/** maxMainImages 是模板主图数量上限，与服务端一致。 */
const maxMainImages = 9;

/** fieldClass 是表单输入统一样式。 */
const fieldClass = 'ios-input w-full rounded-xl px-4 py-3';

/** TemplateEditorDialog 编辑商品模板；草稿只存在于弹窗内，关闭即丢弃。 */
export const TemplateEditorDialog = ({ template, busy, options, onSave, onClose }: TemplateEditorDialogProps) => {
  // draft 是表单草稿，初始值来自模板或空白。
  const [draft, setDraft] = useState<TemplateDraft>(() => template ? draftFromTemplate(template) : emptyDraft());
  // error 是提交前校验失败原因。
  const [error, setError] = useState('');
  // picker 记录正在选图的目标：主图、某个规格值或关闭。
  const [picker, setPicker] = useState<{ /** kind 区分主图与规格值图片。 */ kind: 'main' | 'spec'; /** valueId 是规格值标识。 */ valueId?: string } | null>(null);
  // scopes 由规格推导，无需存入状态。
  const scopes = useMemo(/* 规格变化时重新推导自动化范围。 */ () => specScopes(draft), [draft]);

  /** patch 合并修改草稿。 */
  const patch = (changes: Partial<TemplateDraft>): void => setDraft(/* previous 是修改前的草稿。 */ previous => ({ ...previous, ...changes }));

  /** moveImage 调整主图顺序。 */
  const moveImage = (from: number, to: number): void => {
    if (to < 0 || to >= draft.imageIds.length) return;
    // next 是交换后的主图列表。
    const next = [...draft.imageIds];
    // moved 是被移动的主图主键。
    const [moved] = next.splice(from, 1);
    next.splice(to, 0, moved);
    patch({ imageIds: next });
  };

  /** submit 先做基础校验，再交给父级保存。 */
  const submit = async (): Promise<void> => {
    // message 是校验失败原因，空串表示通过。
    const message = validateDraft(draft);
    setError(message);
    if (message) return;
    await onSave(toRequest(draft));
  };

  return createPortal(
    <div className="modal-overlay" role="presentation">
      <div className="modal-container" style={{ maxWidth: '64rem' }} role="dialog" aria-modal="true" aria-labelledby="template-editor-title">
        <div className="modal-header flex items-start justify-between gap-4">
          <h2 id="template-editor-title" className="text-2xl font-black text-gray-950">{template ? '编辑商品模板' : '新建商品模板'}</h2>
          <button type="button" onClick={onClose} aria-label="关闭编辑器" className="flex h-10 w-10 items-center justify-center rounded-2xl bg-gray-100 hover:bg-gray-200"><X className="h-5 w-5 text-gray-600" /></button>
        </div>
        <div className="modal-body space-y-6">
          {error && <div role="alert" className="rounded-xl bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>}
          <section className="grid gap-4 md:grid-cols-2">
            <label className="text-sm font-bold text-gray-800">模板名称<input className={`${fieldClass} mt-1`} value={draft.name} onChange={/* 修改模板名称。 */ event => patch({ name: event.target.value })} placeholder="仅用于识别模板" /></label>
            <label className="text-sm font-bold text-gray-800">商品标题<input className={`${fieldClass} mt-1`} value={draft.title} onChange={/* 修改商品标题。 */ event => patch({ title: event.target.value })} /></label>
            <label className="text-sm font-bold text-gray-800 md:col-span-2">商品描述<textarea className={`${fieldClass} mt-1 min-h-28`} value={draft.description} onChange={/* 修改商品描述。 */ event => patch({ description: event.target.value })} /></label>
            {draft.specs.length === 0 && <>
              <label className="text-sm font-bold text-gray-800">售价（元）<input inputMode="decimal" className={`${fieldClass} mt-1`} value={draft.price} onChange={/* 修改售价。 */ event => patch({ price: event.target.value })} /></label>
              <label className="text-sm font-bold text-gray-800">库存<input inputMode="numeric" className={`${fieldClass} mt-1`} value={draft.quantity} onChange={/* 修改库存。 */ event => patch({ quantity: event.target.value })} /></label>
            </>}
            <label className="text-sm font-bold text-gray-800">原价（元，可选）<input inputMode="decimal" className={`${fieldClass} mt-1`} value={draft.originalPrice} onChange={/* 修改原价。 */ event => patch({ originalPrice: event.target.value })} /></label>
            <div className="grid grid-cols-2 gap-3">
              <label className="text-sm font-bold text-gray-800">邮费<select className={`${fieldClass} mt-1`} value={draft.postageMode} onChange={/* 切换邮费模式。 */ event => patch({ postageMode: event.target.value === 'fixed' ? 'fixed' : 'free' })}><option value="free">包邮</option><option value="fixed">固定邮费</option></select></label>
              {draft.postageMode === 'fixed' && <label className="text-sm font-bold text-gray-800">邮费（元）<input inputMode="decimal" className={`${fieldClass} mt-1`} value={draft.postage} onChange={/* 修改邮费。 */ event => patch({ postage: event.target.value })} /></label>}
            </div>
            <p className="text-xs text-gray-500 md:col-span-2">类目：{draft.category.catName}（默认电子资料，与批量铺货兜底一致）；发货地使用各账号在平台上的默认值。</p>
          </section>

          <section className="space-y-3" aria-labelledby="template-images-title">
            <div className="flex items-center justify-between"><h3 id="template-images-title" className="text-sm font-extrabold text-gray-900">商品主图（1-{maxMainImages} 张，来自图片库）</h3>
              <button type="button" className="inline-flex items-center gap-2 rounded-xl bg-slate-950 px-3 py-2 text-xs font-bold text-white" onClick={/* 打开主图选择。 */ () => setPicker({ kind: 'main' })}><ImagePlus className="h-4 w-4" />从图片库选择</button></div>
            <div className="grid grid-cols-3 gap-3 sm:grid-cols-5">
              {draft.imageIds.map(/* id、index 渲染一张主图。 */ (id, index) => (
                <figure key={id} className="relative overflow-hidden rounded-2xl border border-slate-200 bg-white">
                  <img src={libraryImageUrl(id)} alt={`第 ${index + 1} 张主图`} className="aspect-square w-full object-cover" />
                  <figcaption className="absolute inset-x-0 top-0 bg-slate-950/70 px-2 py-1 text-[11px] font-bold text-white">{index === 0 ? '主图' : `第 ${index + 1} 张`}</figcaption>
                  <div className="flex items-center justify-between bg-white p-1">
                    <button type="button" aria-label={`第 ${index + 1} 张前移`} disabled={index === 0} className="rounded p-1 disabled:opacity-30" onClick={/* 前移。 */ () => moveImage(index, index - 1)}><ChevronUp className="h-4 w-4" /></button>
                    <button type="button" aria-label={`第 ${index + 1} 张后移`} disabled={index === draft.imageIds.length - 1} className="rounded p-1 disabled:opacity-30" onClick={/* 后移。 */ () => moveImage(index, index + 1)}><ChevronDown className="h-4 w-4" /></button>
                    <button type="button" aria-label={`移除第 ${index + 1} 张主图`} className="rounded p-1 text-red-600" onClick={/* 移除。 */ () => patch({ imageIds: draft.imageIds.filter(/* imageId 保留其余主图。 */ imageId => imageId !== id) })}><Trash2 className="h-4 w-4" /></button>
                  </div>
                </figure>
              ))}
            </div>
          </section>

          <PublishSpecsEditor specs={draft.specs} skuRows={draft.skuRows}
            onChange={/* 规格变化后同步 SKU、规格图片和自动化范围。 */ next => setDraft(/* previous 是变化前草稿。 */ previous => reconcileDraftSpecs(previous, next.specs, next.skuRows))}
            renderValueImage={/* 规格值图片从图片库选择。 */ value => (
              <button key={`img-${value.valueId}`} type="button" aria-label={`为规格值 ${value.value || ''} 选择图片`} title="从图片库选择规格图片" className={`flex h-10 w-10 shrink-0 items-center justify-center overflow-hidden rounded-xl border ${draft.specImageIds[value.valueId] ? 'border-emerald-300' : 'border-slate-200 bg-white text-slate-400'}`} onClick={/* 打开规格图选择。 */ () => setPicker({ kind: 'spec', valueId: value.valueId })}>
                {draft.specImageIds[value.valueId] ? <img src={libraryImageUrl(draft.specImageIds[value.valueId])} alt="规格图片" className="h-full w-full object-cover" /> : <ImagePlus className="h-4 w-4" />}
              </button>
            )} />

          <section className="space-y-3">
            <h3 className="text-sm font-extrabold text-gray-900">发布后自动化</h3>
            {options.error && <div role="alert" className="rounded-xl bg-amber-50 px-4 py-3 text-sm text-amber-800">{options.error}</div>}
            <TemplateAutomationEditor automation={draft.automation} scopes={scopes} cards={options.cards} deliveryTemplates={options.deliveryTemplates} onChange={/* 写回自动化配置。 */ automation => patch({ automation })} />
          </section>
        </div>
        <div className="modal-footer flex justify-end gap-3">
          <button type="button" onClick={onClose} className="rounded-xl bg-gray-100 px-4 py-2.5 text-sm font-bold text-gray-700">取消</button>
          <button type="button" disabled={busy} onClick={/* 提交保存。 */ () => void submit()} className="rounded-xl bg-emerald-600 px-4 py-2.5 text-sm font-bold text-white disabled:opacity-50">{busy ? '保存中...' : '保存模板'}</button>
        </div>
      </div>
      <ImagePickerDialog open={picker !== null} title={picker?.kind === 'spec' ? '选择规格图片' : '选择商品主图'} maxSelect={picker?.kind === 'spec' ? 1 : maxMainImages}
        initialSelected={picker?.kind === 'spec' ? (picker.valueId && draft.specImageIds[picker.valueId] ? [draft.specImageIds[picker.valueId]] : []) : draft.imageIds}
        onClose={/* 关闭选图。 */ () => setPicker(null)}
        onConfirm={/* 应用选图结果。 */ images => {
          if (picker?.kind === 'spec' && picker.valueId) {
            // valueId 是被设置图片的规格值。
            const valueId = picker.valueId;
            setDraft(/* previous 是设置前草稿。 */ previous => ({ ...previous, specImageIds: { ...previous.specImageIds, [valueId]: images[0]?.id ?? 0 } }));
          } else {
            patch({ imageIds: images.map(/* image 提取主键。 */ image => image.id) });
          }
          setPicker(null);
        }} />
    </div>,
    document.body,
  );
};
