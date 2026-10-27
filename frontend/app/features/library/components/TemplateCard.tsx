import { Copy, Edit3, Trash2 } from 'lucide-react';
import { libraryImageUrl } from '../api';
import type { ProductTemplate } from '../types';

/** TemplateCardProps 描述单张模板卡片的数据与操作。 */
export interface TemplateCardProps {
  /** template 是要展示的模板。 */
  template: ProductTemplate;
  /** selected 表示是否被勾选用于批量操作。 */
  selected: boolean;
  /** disabled 在保存、删除或复制进行中禁用变更操作。 */
  disabled: boolean;
  /** onToggle 切换勾选。 */
  onToggle: () => void;
  /** onEdit 打开编辑器。 */
  onEdit: () => void;
  /** onDuplicate 复制模板。 */
  onDuplicate: () => void;
  /** onDelete 请求删除模板。 */
  onDelete: () => void;
}

/** TemplateCard 展示模板主图缩略图、关键字段和单项操作。 */
export const TemplateCard = ({ template, selected, disabled, onToggle, onEdit, onDuplicate, onDelete }: TemplateCardProps) => (
  <article className={`ios-card rounded-xl bg-white p-5 ${selected ? 'ring-2 ring-emerald-300' : ''}`}>
    <div className="flex items-start gap-3">
      <input type="checkbox" aria-label={`选择模板 ${template.name}`} className="mt-1" checked={selected} onChange={onToggle} />
      {template.imageIds.length > 0 && <img src={libraryImageUrl(template.imageIds[0])} alt="" loading="lazy" className="h-16 w-16 shrink-0 rounded-xl object-cover" />}
      <div className="min-w-0 flex-1">
        <h2 className="truncate font-black text-gray-900">{template.name}</h2>
        <p className="mt-1 truncate text-sm text-gray-600">{template.title}</p>
        <p className="mt-1 text-xs text-gray-500">{template.specs.length ? `${template.skus.length} 个规格组合` : `¥${template.price} · 库存 ${template.quantity}`} · {template.imageIds.length} 张图 · 付款发货{template.automation.paidDelivery.enabled ? '已配置' : '未启用'}</p>
        {template.updatedAt && <p className="mt-1 text-[11px] text-gray-400">更新于 {new Date(template.updatedAt).toLocaleString()}</p>}
      </div>
    </div>
    <div className="mt-4 flex gap-2">
      <button type="button" onClick={onEdit} className="inline-flex items-center gap-1.5 rounded-lg ios-btn-secondary px-3 py-2 text-xs font-bold "><Edit3 className="h-3.5 w-3.5" />编辑</button>
      <button type="button" disabled={disabled} onClick={onDuplicate} className="inline-flex items-center gap-1.5 rounded-lg ios-btn-secondary px-3 py-2 text-xs font-bold  disabled:opacity-50"><Copy className="h-3.5 w-3.5" />复制</button>
      <button type="button" disabled={disabled} onClick={onDelete} className="inline-flex items-center gap-1.5 rounded-lg ios-btn-danger px-3 py-2 text-xs font-bold  disabled:opacity-50"><Trash2 className="h-3.5 w-3.5" />删除</button>
    </div>
  </article>
);
