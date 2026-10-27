import { Edit3, ImageIcon, Package, Plus, Rocket, Trash2 } from 'lucide-react';
import React, { useState } from 'react';
import { ImageGrid } from '../components/ImageGrid';
import { ImageUploadButton } from '../components/ImageUploadButton';
import { TemplateEditorDialog } from '../components/TemplateEditorDialog';
import { TemplatePublishDialog } from '../components/TemplatePublishDialog';
import { useAutomationOptions, useLibraryImages, useProductTemplates } from '../hooks';
import type { LibraryImage, ProductTemplate } from '../types';

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
  // publishing 表示发布弹窗是否打开。
  const [publishing, setPublishing] = useState(false);
  // notice 是上传摘要或操作结果。
  const [notice, setNotice] = useState('');
  // options 仅在编辑器或发布弹窗打开时读取，避免进入页面就请求。
  const options = useAutomationOptions(editing !== undefined || publishing);
  // selectedTemplates 由多选主键推导，不单独存状态。
  const selectedTemplates = templates.templates.filter(/* template 保留被勾选的模板。 */ template => selectedIds.includes(template.id));

  /** toggleSelected 切换模板多选状态。 */
  const toggleSelected = (id: number): void => setSelectedIds(/* previous 是切换前的选择。 */ previous => previous.includes(id) ? previous.filter(/* item 移除该模板。 */ item => item !== id) : [...previous, id]);

  /** removeImage 经确认后删除图片。 */
  const removeImage = async (image: LibraryImage): Promise<void> => {
    if (!confirm(`确定删除图片“${image.filename}”吗？仍被商品模板引用的图片无法删除。`)) return;
    if (await images.remove(image.id)) setNotice('');
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

      {notice && <div role="status" className="rounded-xl border border-emerald-100 bg-emerald-50 px-4 py-3 text-sm text-emerald-800">{notice}</div>}

      {tab === 'templates' ? (
        <section className="space-y-4" aria-label="商品模板">
          {templates.error && <div role="alert" className="rounded-xl border border-red-100 bg-red-50 px-4 py-3 text-sm text-red-700">{templates.error}</div>}
          <div className="flex flex-wrap gap-3">
            <button type="button" onClick={/* 打开空白编辑器。 */ () => setEditing(null)} className="inline-flex items-center gap-2 rounded-xl bg-slate-950 px-4 py-3 text-sm font-bold text-white hover:bg-slate-800"><Plus className="h-4 w-4" />新建模板</button>
            <button type="button" disabled={selectedTemplates.length === 0} onClick={/* 打开发布弹窗。 */ () => setPublishing(true)} className="inline-flex items-center gap-2 rounded-xl bg-emerald-600 px-4 py-3 text-sm font-bold text-white disabled:opacity-50"><Rocket className="h-4 w-4" />发布所选（{selectedTemplates.length}）</button>
          </div>
          <div className="grid gap-4 md:grid-cols-2">
            {templates.templates.map(/* template 渲染一张模板卡片。 */ template => (
              <article key={template.id} className="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm">
                <div className="flex items-start gap-3">
                  <input type="checkbox" aria-label={`选择模板 ${template.name}`} className="mt-1" checked={selectedIds.includes(template.id)} onChange={/* 切换多选。 */ () => toggleSelected(template.id)} />
                  <div className="min-w-0 flex-1">
                    <h2 className="truncate font-black text-slate-900">{template.name}</h2>
                    <p className="mt-1 truncate text-sm text-slate-600">{template.title}</p>
                    <p className="mt-1 text-xs text-slate-500">{template.specs.length ? `${template.skus.length} 个规格组合` : `¥${template.price} · 库存 ${template.quantity}`} · {template.imageIds.length} 张图 · 付款发货{template.automation.paidDelivery.enabled ? '已配置' : '未启用'}</p>
                  </div>
                </div>
                <div className="mt-4 flex gap-2">
                  <button type="button" onClick={/* 打开该模板编辑器。 */ () => setEditing(template)} className="inline-flex items-center gap-1.5 rounded-lg bg-slate-100 px-3 py-2 text-xs font-bold text-slate-700"><Edit3 className="h-3.5 w-3.5" />编辑</button>
                  <button type="button" onClick={/* 删除该模板。 */ () => void removeTemplate(template)} className="inline-flex items-center gap-1.5 rounded-lg bg-red-50 px-3 py-2 text-xs font-bold text-red-600"><Trash2 className="h-3.5 w-3.5" />删除</button>
                </div>
              </article>
            ))}
            {!templates.loading && templates.templates.length === 0 && <div className="rounded-3xl border border-dashed border-slate-300 bg-white p-12 text-center text-sm text-slate-500 md:col-span-2">还没有商品模板，先创建一份吧。</div>}
          </div>
        </section>
      ) : (
        <section className="space-y-4" aria-label="图片库">
          {images.error && <div role="alert" className="rounded-xl border border-red-100 bg-red-50 px-4 py-3 text-sm text-red-700">{images.error}</div>}
          <ImageUploadButton busy={images.busy} onUpload={images.upload} onResults={setNotice} />
          {!images.loading && images.images.length === 0 ? <div className="rounded-3xl border border-dashed border-slate-300 bg-white p-12 text-center text-sm text-slate-500">图片库还是空的，上传几张图片开始使用。</div> : <ImageGrid images={images.images} onDelete={/* 请求删除图片。 */ image => void removeImage(image)} disabled={images.busy} />}
        </section>
      )}

      {editing !== undefined && (
        <TemplateEditorDialog key={editing?.id ?? 'new'} template={editing} busy={templates.busy} options={options}
          onClose={/* 关闭并丢弃草稿。 */ () => setEditing(undefined)}
          onSave={/* 保存成功后关闭编辑器。 */ async body => { if (await templates.save(editing?.id ?? null, body)) setEditing(undefined); }} />
      )}
      {publishing && (
        <TemplatePublishDialog templates={selectedTemplates} accounts={options.accounts} onClose={/* 关闭发布弹窗。 */ () => setPublishing(false)}
          onStarted={/* 批次启动后关闭弹窗并通知应用壳。 */ batchId => { setPublishing(false); setSelectedIds([]); setNotice('发布已开始，可在商品列表的批量铺货中查看进度。'); onBatchStarted?.(batchId); }} />
      )}
    </div>
  );
};

export default ResourceLibrary;
