import { Check, Trash2 } from 'lucide-react';
import { libraryImageUrl } from '../api';
import type { LibraryImage } from '../types';

/** ImageGridProps 描述图片网格的展示与交互边界。 */
export interface ImageGridProps {
  /** images 是要展示的图片。 */
  images: LibraryImage[];
  /** selectedIds 是当前选中的图片主键，选择模式使用。 */
  selectedIds?: number[];
  /** onToggle 在选择模式下切换某张图片的选中状态。 */
  onToggle?: (image: LibraryImage) => void;
  /** onDelete 在管理模式下删除图片。 */
  onDelete?: (image: LibraryImage) => void;
  /** disabled 禁用所有交互。 */
  disabled?: boolean;
}

/** ImageGrid 以缩略图网格展示图片库图片，缩略图通过同源内容地址直接加载。 */
export const ImageGrid = ({ images, selectedIds = [], onToggle, onDelete, disabled = false }: ImageGridProps) => (
  <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-6">
    {images.map(/* image 渲染一张图片卡片。 */ image => {
      // order 是图片在选中列表中的序号，未选中为 -1。
      const order = selectedIds.indexOf(image.id);
      return (
        <article key={image.id} className={`group relative overflow-hidden rounded-2xl border bg-white shadow-sm ${order >= 0 ? 'border-emerald-400 ring-2 ring-emerald-200' : 'border-slate-200'}`}>
          <button type="button" disabled={disabled || !onToggle} aria-pressed={order >= 0} aria-label={`选择图片 ${image.filename}`} className="block w-full" onClick={/* 切换当前图片选中状态。 */ () => onToggle?.(image)}>
            <img src={libraryImageUrl(image.id)} alt={image.filename} loading="lazy" className="aspect-square w-full object-cover" />
          </button>
          {order >= 0 && <span className="absolute left-2 top-2 flex h-6 min-w-6 items-center justify-center gap-0.5 rounded-full bg-emerald-600 px-1.5 text-[11px] font-black text-white"><Check className="h-3 w-3" />{order + 1}</span>}
          <p className="truncate px-2 py-1.5 text-[11px] font-bold text-slate-600" title={image.filename}>{image.filename}</p>
          {onDelete && <button type="button" disabled={disabled} aria-label={`删除图片 ${image.filename}`} className="absolute right-2 top-2 rounded-lg bg-white/90 p-1.5 text-slate-600 opacity-0 shadow transition hover:text-red-600 focus:opacity-100 group-hover:opacity-100 disabled:opacity-30" onClick={/* 请求删除当前图片。 */ () => onDelete(image)}><Trash2 className="h-4 w-4" /></button>}
        </article>
      );
    })}
  </div>
);
