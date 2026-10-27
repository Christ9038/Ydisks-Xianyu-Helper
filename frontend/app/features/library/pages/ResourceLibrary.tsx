import { ImageIcon, Package, Plus, Rocket, Trash2 } from 'lucide-react';
import React, { useEffect, useRef, useState } from 'react';
import { ImageGrid } from '../components/ImageGrid';
import { ImageUploadButton } from '../components/ImageUploadButton';
import { ListToolbar } from '../components/ListToolbar';
import { SelectAllCheckbox } from '../components/SelectAllCheckbox';
import { TemplateCard } from '../components/TemplateCard';
import { TemplateEditorDialog } from '../components/TemplateEditorDialog';
import { TemplatePublishDialog } from '../components/TemplatePublishDialog';
import { type BatchDeleteResult, useAutomationOptions, useLibraryImages, useProductTemplates } from '../hooks';
import { defaultImageFilters, imageUsage, isImageFilterActive, queryImages, type ImageFilters } from '../imageQuery';
import { countHidden, selectionState, toggleAllVisible } from '../selection';
import { defaultTemplateFilters, isTemplateFilterActive, queryTemplates, type TemplateFilters } from '../templateQuery';
import type { LibraryImage, ProductTemplate } from '../types';

/** imageSortOptions 是图片排序下拉的选项。 */
const imageSortOptions = [
  { value: 'newest', label: '最新上传' }, { value: 'oldest', label: '最早上传' }, { value: 'name-asc', label: '文件名 A→Z' },
  { value: 'name-desc', label: '文件名 Z→A' }, { value: 'size-desc', label: '文件大→小' }, { value: 'size-asc', label: '文件小→大' },
];

/** templateSortOptions 是模板排序下拉的选项。 */
const templateSortOptions = [
  { value: 'updated-desc', label: '最近更新' }, { value: 'updated-asc', label: '最早更新' }, { value: 'name-asc', label: '名称 A→Z' },
  { value: 'price-asc', label: '价格低→高' }, { value: 'price-desc', label: '价格高→低' }, { value: 'stock-desc', label: '库存多→少' },
];

/** summarizeBatch 把批量删除结果转换为提示文案：成功数与失败项（附带每项原因）。 */
const summarizeBatch = (result: BatchDeleteResult, nameOf: (id: number) => string, noun: string): { /** text 是提示文字。 */ text: string; /** tone 区分全部成功与存在失败。 */ tone: 'ok' | 'warn' } => {
  if (result.failures.length === 0) return { text: `已删除 ${result.deleted.length} ${noun}。`, tone: 'ok' };
  // detail 是逐项失败原因，最多展示前 5 项以免提示过长。
  const detail = result.failures.slice(0, 5).map(/* failure 转换为“名称：原因”。 */ failure => `${nameOf(failure.id)}：${failure.message}`).join('；');
  return { text: `已删除 ${result.deleted.length} ${noun}，${result.failures.length} ${noun}失败（${detail}${result.failures.length > 5 ? '…' : ''}）。`, tone: 'warn' };
};

/** ResourceLibraryProps 描述资源库页面与应用壳之间的导航边界。 */
export interface ResourceLibraryProps {
  /** onBatchStarted 在模板批次启动后通知应用壳，例如跳转到商品页查看进度。 */
  onBatchStarted?: (batchId: string) => void;
}

/** ResourceLibrary 是本地资源库页面：商品模板（批量铺货入口）与图片库。 */
const ResourceLibrary: React.FC<ResourceLibraryProps> = ({ onBatchStarted }) => {
  // tab 是当前标签页。
  const [tab, setTab] = useState<'templates' | 'images'>('templates');
  // images 是图片库状态；templates 是模板状态。
  const images = useLibraryImages();
  const templates = useProductTemplates();
  // editing 记录正在编辑的模板，undefined 表示编辑器关闭，null 表示新建。
  const [editing, setEditing] = useState<ProductTemplate | null | undefined>(undefined);
  // selectedIds 是用户多选的模板主键。
  const [selectedIds, setSelectedIds] = useState<number[]>([]);
  // templateFilters 是模板搜索、筛选和排序条件，可见模板由它推导。
  const [templateFilters, setTemplateFilters] = useState<TemplateFilters>(defaultTemplateFilters);
  // imageFilters 是图片搜索、筛选和排序条件，可见图片由它推导。
  const [imageFilters, setImageFilters] = useState<ImageFilters>(defaultImageFilters);
  // checkedImageIds 是图片库中被勾选用于批量删除的图片主键。
  const [checkedImageIds, setCheckedImageIds] = useState<number[]>([]);
  // publishing 表示发布弹窗是否打开。
  const [publishing, setPublishing] = useState(false);
  // notice 是上传摘要或操作结果，tone 为 warn 时表示存在失败项。
  const [notice, setNotice] = useState<{ /** text 是提示文字。 */ text: string; /** tone 是提示语气。 */ tone: 'ok' | 'warn' }>({ text: '', tone: 'ok' });
  // batchControllerRef 取消进行中的批量删除；页面卸载时停止剩余请求。
  const batchControllerRef = useRef<AbortController | null>(null);
  useEffect(/* 卸载时取消尚未完成的批量删除。 */ () => /* 卸载清理取消批量删除。 */ () => batchControllerRef.current?.abort(), []);
  // options 仅在编辑器或发布弹窗打开时读取，避免进入页面就请求。
  const options = useAutomationOptions(editing !== undefined || publishing);
  // selectedTemplates 由多选主键推导，不单独存状态。
  const selectedTemplates = templates.templates.filter(/* template 保留被勾选的模板。 */ template => selectedIds.includes(template.id));
  // visibleTemplates 是通过搜索、筛选并排序后的模板，全选只作用于它。
  const visibleTemplates = queryTemplates(templates.templates, templateFilters);
  // visibleTemplateIds 是可见模板主键。
  const visibleTemplateIds = visibleTemplates.map(/* template 取主键。 */ template => template.id);
  // usage 是图片被模板引用的次数，由已加载模板推导。
  const usage = imageUsage(templates.templates);
  // visibleImages 是通过搜索、筛选并排序后的图片。
  const visibleImages = queryImages(images.images, imageFilters, usage);
  // visibleImageIds 是可见图片主键。
  const visibleImageIds = visibleImages.map(/* image 取主键。 */ image => image.id);

  /** toggleSelected 切换模板多选状态。 */
  const toggleSelected = (id: number): void => setSelectedIds(/* previous 是切换前的选择。 */ previous => previous.includes(id) ? previous.filter(/* item 移除该模板。 */ item => item !== id) : [...previous, id]);

  /** removeImage 经确认后删除图片。 */
  const removeImage = async (image: LibraryImage): Promise<void> => {
    if (!confirm(`确定删除图片“${image.filename}”吗？仍被商品模板引用的图片无法删除。`)) return;
    if (await images.remove(image.id)) {
      setNotice({ text: '', tone: 'ok' });
      setCheckedImageIds(/* previous 是删除前的勾选。 */ previous => previous.filter(/* id 移除已删除图片。 */ id => id !== image.id));
    }
  };

  /** removeCheckedImages 确认后串行批量删除勾选的图片；被引用的图片由服务端拒绝并在汇总中列出。 */
  const removeCheckedImages = async (): Promise<void> => {
    // referenced 是勾选项中被模板引用、预计会被拒绝的数量。
    const referenced = checkedImageIds.filter(/* id 判断是否被模板引用。 */ id => (usage.get(id) ?? 0) > 0).length;
    if (!confirm(`确定删除选中的 ${checkedImageIds.length} 张图片吗？${referenced > 0 ? `其中 ${referenced} 张仍被商品模板引用，将无法删除。` : ''}`)) return;
    batchControllerRef.current?.abort();
    // controller 控制本次批量删除。
    const controller = new AbortController();
    batchControllerRef.current = controller;
    // result 是批量删除汇总。
    const result = await images.removeMany(checkedImageIds, controller.signal);
    if (controller.signal.aborted) return;
    setNotice(summarizeBatch(result, /* id 查找图片文件名。 */ id => images.images.find(/* image 按主键匹配。 */ image => image.id === id)?.filename ?? `#${id}`, '张'));
    setCheckedImageIds(/* previous 是删除前的勾选。 */ previous => previous.filter(/* id 移除已删除图片。 */ id => !result.deleted.includes(id)));
  };

  /** removeSelectedTemplates 确认后串行批量删除勾选的模板。 */
  const removeSelectedTemplates = async (): Promise<void> => {
    if (!confirm(`确定删除选中的 ${selectedTemplates.length} 个模板吗？已发布的商品不受影响。`)) return;
    batchControllerRef.current?.abort();
    // controller 控制本次批量删除。
    const controller = new AbortController();
    batchControllerRef.current = controller;
    // result 是批量删除汇总。
    const result = await templates.removeMany(selectedTemplates.map(/* template 取主键。 */ template => template.id), controller.signal);
    if (controller.signal.aborted) return;
    setNotice(summarizeBatch(result, /* id 查找模板名称。 */ id => templates.templates.find(/* template 按主键匹配。 */ template => template.id === id)?.name ?? `#${id}`, '个'));
    setSelectedIds(/* previous 是删除前的选择。 */ previous => previous.filter(/* id 移除已删除模板。 */ id => !result.deleted.includes(id)));
  };

  /** duplicateTemplate 复制模板并提示结果；失败原因由 templates.error 展示。 */
  const duplicateTemplate = async (template: ProductTemplate): Promise<void> => {
    if (await templates.duplicate(template)) setNotice({ text: `已复制模板“${template.name}”。`, tone: 'ok' });
  };

  /** removeTemplate 经确认后删除模板。 */
  const removeTemplate = async (template: ProductTemplate): Promise<void> => {
    if (!confirm(`确定删除模板“${template.name}”吗？已发布的商品不受影响。`)) return;
    if (await templates.remove(template.id)) setSelectedIds(/* previous 是删除前的选择。 */ previous => previous.filter(/* id 移除已删除模板。 */ id => id !== template.id));
  };

  return (
    <div className="space-y-8">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-xs font-black uppercase tracking-[0.24em] text-sky-600">Local library</p>
          <h1 className="mt-2 text-3xl font-black tracking-tight text-slate-950">本地资源库</h1>
          <p className="mt-2 text-sm text-slate-500">保存常用商品模板与图片，一键把同款商品铺到多个账号。</p>
        </div>
        <div className="flex gap-2" role="tablist" aria-label="资源库分类">
          <button type="button" role="tab" aria-selected={tab === 'templates'} onClick={/* 切换到商品模板。 */ () => setTab('templates')} className={`inline-flex items-center gap-2 rounded-xl px-4 py-2.5 text-sm font-bold ${tab === 'templates' ? 'bg-slate-950 text-white' : 'bg-slate-100 text-slate-700'}`}><Package className="h-4 w-4" />商品模板</button>
          <button type="button" role="tab" aria-selected={tab === 'images'} onClick={/* 切换到图片库。 */ () => setTab('images')} className={`inline-flex items-center gap-2 rounded-xl px-4 py-2.5 text-sm font-bold ${tab === 'images' ? 'bg-slate-950 text-white' : 'bg-slate-100 text-slate-700'}`}><ImageIcon className="h-4 w-4" />图片库</button>
        </div>
      </header>

      {notice.text && <div role="status" className={`rounded-xl border px-4 py-3 text-sm ${notice.tone === 'warn' ? 'border-amber-100 bg-amber-50 text-amber-800' : 'border-emerald-100 bg-emerald-50 text-emerald-800'}`}>{notice.text}</div>}

      {tab === 'templates' ? (
        <section className="space-y-4" aria-label="商品模板">
          {templates.error && <div role="alert" className="rounded-xl border border-red-100 bg-red-50 px-4 py-3 text-sm text-red-700">{templates.error}</div>}
          <div className="flex flex-wrap gap-3">
            <button type="button" onClick={/* 打开空白编辑器。 */ () => setEditing(null)} className="inline-flex items-center gap-2 rounded-xl bg-slate-950 px-4 py-3 text-sm font-bold text-white hover:bg-slate-800"><Plus className="h-4 w-4" />新建模板</button>
            <button type="button" disabled={selectedTemplates.length === 0} onClick={/* 打开发布弹窗。 */ () => setPublishing(true)} className="inline-flex items-center gap-2 rounded-xl bg-emerald-600 px-4 py-3 text-sm font-bold text-white disabled:opacity-50"><Rocket className="h-4 w-4" />发布所选（{selectedTemplates.length}）</button>
            <button type="button" disabled={selectedTemplates.length === 0 || templates.busy} onClick={/* 批量删除勾选的模板。 */ () => void removeSelectedTemplates()} className="inline-flex items-center gap-2 rounded-xl bg-red-50 px-4 py-3 text-sm font-bold text-red-600 disabled:opacity-50"><Trash2 className="h-4 w-4" />删除所选</button>
          </div>
          <ListToolbar searchLabel="搜索模板名称、标题、描述或类目" query={templateFilters.query} onQueryChange={/* 更新搜索词。 */ query => setTemplateFilters(/* previous 是更新前的条件。 */ previous => ({ ...previous, query }))}
            shown={visibleTemplates.length} total={templates.templates.length} active={isTemplateFilterActive(templateFilters)}
            onReset={/* 清除搜索与筛选，保留当前排序。 */ () => setTemplateFilters(/* previous 提供要保留的排序。 */ previous => ({ ...defaultTemplateFilters, sort: previous.sort }))}
            selects={[
              { label: '规格', value: templateFilters.specs, onChange: /* 切换规格筛选。 */ value => setTemplateFilters(/* previous 是更新前的条件。 */ previous => ({ ...previous, specs: value as TemplateFilters['specs'] })), options: [{ value: 'all', label: '全部' }, { value: 'single', label: '单规格' }, { value: 'multi', label: '多规格' }] },
              { label: '付款发货', value: templateFilters.delivery, onChange: /* 切换付款发货筛选。 */ value => setTemplateFilters(/* previous 是更新前的条件。 */ previous => ({ ...previous, delivery: value as TemplateFilters['delivery'] })), options: [{ value: 'all', label: '全部' }, { value: 'enabled', label: '已启用' }, { value: 'disabled', label: '未启用' }] },
              { label: '邮费', value: templateFilters.postage, onChange: /* 切换邮费筛选。 */ value => setTemplateFilters(/* previous 是更新前的条件。 */ previous => ({ ...previous, postage: value as TemplateFilters['postage'] })), options: [{ value: 'all', label: '全部' }, { value: 'free', label: '包邮' }, { value: 'distance', label: '按距离' }, { value: 'fixed', label: '固定邮费' }, { value: 'none', label: '无需邮寄' }] },
              { label: '排序', value: templateFilters.sort, onChange: /* 切换排序方式。 */ value => setTemplateFilters(/* previous 是更新前的条件。 */ previous => ({ ...previous, sort: value as TemplateFilters['sort'] })), options: templateSortOptions },
            ]} />
          <div className="flex flex-wrap items-center gap-3">
            <SelectAllCheckbox label="全选当前结果" state={selectionState(visibleTemplateIds, selectedIds)} disabled={visibleTemplateIds.length === 0} onToggle={/* 全选或取消当前可见模板。 */ () => setSelectedIds(/* previous 是切换前的选择。 */ previous => toggleAllVisible(previous, visibleTemplateIds))} />
            <span className="text-xs text-slate-500">已选 {selectedIds.length} 个{countHidden(selectedIds, visibleTemplateIds) > 0 ? `，其中 ${countHidden(selectedIds, visibleTemplateIds)} 个不在当前筛选中` : ''}</span>
            {selectedIds.length > 0 && <button type="button" onClick={/* 清空全部选择。 */ () => setSelectedIds([])} className="rounded-lg bg-slate-100 px-2.5 py-1.5 text-xs font-bold text-slate-700">清空选择</button>}
          </div>
          <div className="grid gap-4 md:grid-cols-2">
            {visibleTemplates.map(/* template 渲染一张模板卡片。 */ template => (
              <TemplateCard key={template.id} template={template} selected={selectedIds.includes(template.id)} disabled={templates.busy}
                onToggle={/* 切换多选。 */ () => toggleSelected(template.id)} onEdit={/* 打开该模板编辑器。 */ () => setEditing(template)}
                onDuplicate={/* 复制该模板。 */ () => void duplicateTemplate(template)} onDelete={/* 删除该模板。 */ () => void removeTemplate(template)} />
            ))}
            {!templates.loading && templates.templates.length === 0 && <div className="rounded-3xl border border-dashed border-slate-300 bg-white p-12 text-center text-sm text-slate-500 md:col-span-2">还没有商品模板，先创建一份吧。</div>}
            {!templates.loading && templates.templates.length > 0 && visibleTemplates.length === 0 && <div className="rounded-3xl border border-dashed border-slate-300 bg-white p-12 text-center text-sm text-slate-500 md:col-span-2">没有匹配的模板，可以调整搜索词或点击“重置筛选”。</div>}
          </div>
        </section>
      ) : (
        <section className="space-y-4" aria-label="图片库">
          {images.error && <div role="alert" className="rounded-xl border border-red-100 bg-red-50 px-4 py-3 text-sm text-red-700">{images.error}</div>}
          <div className="flex flex-wrap items-center gap-3">
            <ImageUploadButton busy={images.busy} onUpload={images.upload} onResults={/* 上传摘要作为普通提示展示。 */ text => setNotice({ text, tone: 'ok' })} />
            <button type="button" disabled={checkedImageIds.length === 0 || images.busy} onClick={/* 批量删除勾选的图片。 */ () => void removeCheckedImages()} className="inline-flex items-center gap-2 rounded-xl bg-red-50 px-4 py-3 text-sm font-bold text-red-600 disabled:opacity-50"><Trash2 className="h-4 w-4" />删除所选（{checkedImageIds.length}）</button>
          </div>
          <ListToolbar searchLabel="搜索图片文件名" query={imageFilters.query} onQueryChange={/* 更新搜索词。 */ query => setImageFilters(/* previous 是更新前的条件。 */ previous => ({ ...previous, query }))}
            shown={visibleImages.length} total={images.images.length} active={isImageFilterActive(imageFilters)}
            onReset={/* 清除搜索与筛选，保留当前排序。 */ () => setImageFilters(/* previous 提供要保留的排序。 */ previous => ({ ...defaultImageFilters, sort: previous.sort }))}
            selects={[
              { label: '格式', value: imageFilters.format, onChange: /* 切换格式筛选。 */ value => setImageFilters(/* previous 是更新前的条件。 */ previous => ({ ...previous, format: value as ImageFilters['format'] })), options: [{ value: 'all', label: '全部' }, { value: 'jpg', label: 'JPG' }, { value: 'png', label: 'PNG' }, { value: 'webp', label: 'WEBP' }, { value: 'gif', label: 'GIF' }] },
              { label: '引用', value: imageFilters.usage, onChange: /* 切换引用筛选。 */ value => setImageFilters(/* previous 是更新前的条件。 */ previous => ({ ...previous, usage: value as ImageFilters['usage'] })), options: [{ value: 'all', label: '全部' }, { value: 'used', label: '被模板引用' }, { value: 'unused', label: '未被引用' }] },
              { label: '排序', value: imageFilters.sort, onChange: /* 切换排序方式。 */ value => setImageFilters(/* previous 是更新前的条件。 */ previous => ({ ...previous, sort: value as ImageFilters['sort'] })), options: imageSortOptions },
            ]} />
          <div className="flex flex-wrap items-center gap-3">
            <SelectAllCheckbox label="全选当前结果" state={selectionState(visibleImageIds, checkedImageIds)} disabled={visibleImageIds.length === 0 || images.busy} onToggle={/* 全选或取消当前可见图片。 */ () => setCheckedImageIds(/* previous 是切换前的勾选。 */ previous => toggleAllVisible(previous, visibleImageIds))} />
            <span className="text-xs text-slate-500">已选 {checkedImageIds.length} 张{countHidden(checkedImageIds, visibleImageIds) > 0 ? `，其中 ${countHidden(checkedImageIds, visibleImageIds)} 张不在当前筛选中` : ''}；点击图片查看大图</span>
            {checkedImageIds.length > 0 && <button type="button" onClick={/* 清空全部勾选。 */ () => setCheckedImageIds([])} className="rounded-lg bg-slate-100 px-2.5 py-1.5 text-xs font-bold text-slate-700">清空选择</button>}
          </div>
          {!images.loading && images.images.length === 0 ? <div className="rounded-3xl border border-dashed border-slate-300 bg-white p-12 text-center text-sm text-slate-500">图片库还是空的，上传几张图片开始使用。</div> : visibleImages.length === 0 ? <div className="rounded-3xl border border-dashed border-slate-300 bg-white p-12 text-center text-sm text-slate-500">没有匹配的图片，可以调整搜索词或点击“重置筛选”。</div> : <ImageGrid images={visibleImages} usage={usage} checkedIds={checkedImageIds} onCheck={/* 切换该图片的批量勾选。 */ image => setCheckedImageIds(/* previous 是切换前的勾选。 */ previous => previous.includes(image.id) ? previous.filter(/* id 移除该图片。 */ id => id !== image.id) : [...previous, image.id])} onDelete={/* 请求删除图片。 */ image => void removeImage(image)} disabled={images.busy} />}
        </section>
      )}

      {editing !== undefined && (
        <TemplateEditorDialog key={editing?.id ?? 'new'} template={editing} busy={templates.busy} options={options}
          onClose={/* 关闭并丢弃草稿。 */ () => setEditing(undefined)}
          onSave={/* 保存成功后关闭编辑器。 */ async body => { if (await templates.save(editing?.id ?? null, body)) setEditing(undefined); }} />
      )}
      {publishing && (
        <TemplatePublishDialog templates={selectedTemplates} accounts={options.accounts} onClose={/* 关闭发布弹窗。 */ () => setPublishing(false)}
          onStarted={/* 批次启动后关闭弹窗并通知应用壳。 */ batchId => { setPublishing(false); setSelectedIds([]); setNotice({ text: '发布已开始，可在商品列表的批量铺货中查看进度。', tone: 'ok' }); onBatchStarted?.(batchId); }} />
      )}
    </div>
  );
};

export default ResourceLibrary;
