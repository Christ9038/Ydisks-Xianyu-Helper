import { Check, Trash2, ZoomIn } from 'lucide-react';
import { useState } from 'react';
import Lightbox from 'yet-another-react-lightbox';
import 'yet-another-react-lightbox/styles.css';
import { libraryImageUrl } from '../api';
import { formatBytes } from '../imageQuery';
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
  /** checkedIds 是管理模式下被勾选用于批量操作的图片主键；与选择模式的序号选择相互独立。 */
  checkedIds?: number[];
  /** onCheck 提供时显示左上角复选框，切换该图片的批量勾选状态。 */
  onCheck?: (image: LibraryImage) => void;
  /** usage 是图片主键到被模板引用数的映射，大于 0 时显示引用角标。 */
  usage?: Map<number, number>;
  /** disabled 禁用所有交互。 */
  disabled?: boolean;
}

/** ImageGrid 以缩略图网格展示图片库图片，缩略图通过同源内容地址直接加载。 */
export const ImageGrid = ({ images, selectedIds = [], onToggle, onDelete, checkedIds = [], onCheck, usage, disabled = false }: ImageGridProps) => {
  // previewIndex 是灯箱当前图片下标，负值表示关闭。
  const [previewIndex, setPreviewIndex] = useState(-1);
  return (
  <>
  <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-6">
    {images.map(/* image、index 渲染一张图片卡片。 */ (image, index) => {
      // order 是图片在选中列表中的序号，未选中为 -1。
      const order = selectedIds.indexOf(image.id);
      // checked 表示该图片已被勾选用于批量操作。
      const checked = checkedIds.includes(image.id);
      // usedBy 是引用该图片的模板数。
      const usedBy = usage?.get(image.id) ?? 0;
      return (
        <article key={image.id} className={`group relative overflow-hidden rounded-2xl border bg-white shadow-sm ${order >= 0 || checked ? 'border-emerald-400 ring-2 ring-emerald-200' : 'border-slate-200'}`}>
          <button type="button" disabled={disabled} aria-pressed={onToggle ? order >= 0 : undefined} aria-label={onToggle ? `选择图片 ${image.filename}` : `预览图片 ${image.filename}`} className={`block w-full ${onToggle ? '' : 'cursor-zoom-in'}`} onClick={/* 选择模式切换选中，管理模式打开大图预览。 */ () => onToggle ? onToggle(image) : setPreviewIndex(index)}>
            <img src={libraryImageUrl(image.id)} alt={image.filename} loading="lazy" className="aspect-square w-full object-cover" />
          </button>
          {order >= 0 && <span className="absolute left-2 top-2 flex h-6 min-w-6 items-center justify-center gap-0.5 rounded-full bg-emerald-600 px-1.5 text-[11px] font-black text-white"><Check className="h-3 w-3" />{order + 1}</span>}
          {onCheck && <input type="checkbox" aria-label={`勾选图片 ${image.filename}`} className="absolute left-2 top-2 h-4 w-4" checked={checked} disabled={disabled} onChange={/* 切换该图片的批量勾选。 */ () => onCheck(image)} />}
          {usedBy > 0 && <span className="absolute bottom-10 left-2 rounded-full bg-sky-600 px-2 py-0.5 text-[10px] font-bold text-white">被 {usedBy} 个模板引用</span>}
          <p className="truncate px-2 pt-1.5 text-[11px] font-bold text-slate-600" title={image.filename}>{image.filename}</p>
          <p className="px-2 pb-1.5 text-[10px] text-slate-400">{formatBytes(image.sizeBytes)}</p>
          {onToggle && <button type="button" aria-label={`预览大图 ${image.filename}`} className="absolute bottom-10 right-2 rounded-lg bg-white/90 p-1.5 text-slate-600 shadow hover:text-emerald-700" onClick={/* 选择模式下单独打开大图预览。 */ () => setPreviewIndex(index)}><ZoomIn className="h-4 w-4" /></button>}
          {onDelete && <button type="button" disabled={disabled} aria-label={`删除图片 ${image.filename}`} className="absolute right-2 top-2 rounded-lg bg-white/90 p-1.5 text-slate-600 opacity-0 shadow transition hover:text-red-600 focus:opacity-100 group-hover:opacity-100 disabled:opacity-30" onClick={/* 请求删除当前图片。 */ () => onDelete(image)}><Trash2 className="h-4 w-4" /></button>}
        </article>
      );
    })}
  </div>
  <Lightbox open={previewIndex >= 0} index={Math.max(previewIndex, 0)} close={/* 关闭大图预览。 */ () => setPreviewIndex(-1)} slides={images.map(/* image 转换为灯箱幻灯片。 */ image => ({ src: libraryImageUrl(image.id), alt: image.filename }))} />
  </>
  );
};
