import { X } from 'lucide-react';
import { useState } from 'react';
import { createPortal } from 'react-dom';
import { useLibraryImages } from '../hooks';
import { defaultImageFilters, isImageFilterActive, queryImages, type ImageFilters } from '../imageQuery';
import { fillToLimit } from '../selection';
import type { LibraryImage, LibraryImagePickerProps } from '../types';
import { ImageGrid } from './ImageGrid';
import { ImageUploadButton } from './ImageUploadButton';
import { ListToolbar } from './ListToolbar';

/** ImagePickerBodyProps 描述图片选择弹窗内容。 */
interface ImagePickerBodyProps {
  /** title 是弹窗标题。 */
  title: string;
  /** maxSelect 是最多可选张数。 */
  maxSelect: number;
  /** initialSelected 是初始选中的图片主键。 */
  initialSelected: number[];
  /** onClose 关闭弹窗。 */
  onClose: () => void;
  /** onConfirm 返回按选择顺序排列的图片。 */
  onConfirm: (images: LibraryImage[]) => void;
}

/** ImagePickerBody 在弹窗打开期间才挂载，保证只在需要时读取图片库。 */
const ImagePickerBody = ({ title, maxSelect, initialSelected, onClose, onConfirm }: ImagePickerBodyProps) => {
  // library 是图片库列表与上传能力。
  const library = useLibraryImages();
  // selected 是按选择顺序排列的图片主键。
  const [selected, setSelected] = useState<number[]>(initialSelected);
  // notice 是最近一次上传摘要。
  const [notice, setNotice] = useState('');
  // filters 是搜索、格式筛选和排序条件，可见图片由它推导；弹窗不加载模板，所以没有引用筛选。
  const [filters, setFilters] = useState<ImageFilters>(defaultImageFilters);
  // visible 是通过搜索、筛选并排序后的图片。
  const visible = queryImages(library.images, filters);

  /** toggle 切换选中状态，达到上限后忽略新增。 */
  const toggle = (image: LibraryImage): void => setSelected(/* previous 是切换前的选择。 */ previous => previous.includes(image.id) ? previous.filter(/* id 移除当前图片。 */ id => id !== image.id) : previous.length >= maxSelect ? previous : [...previous, image.id]);

  /** fillVisible 按当前可见顺序把图片补选到上限，已选图片顺序保持不变。 */
  const fillVisible = (): void => setSelected(/* previous 是补选前的选择。 */ previous => fillToLimit(previous, visible.map(/* image 取主键。 */ image => image.id), maxSelect));

  /** confirm 把选中的主键解析为图片对象；已被删除的图片被忽略。 */
  const confirm = (): void => onConfirm(selected.flatMap(/* id 查找对应图片。 */ id => library.images.find(/* image 按主键匹配。 */ image => image.id === id) ?? []));

  return createPortal(
    <div className="modal-overlay" role="presentation">
      <div className="modal-container" style={{ maxWidth: '60rem' }} role="dialog" aria-modal="true" aria-labelledby="image-picker-title">
        <div className="modal-header flex items-start justify-between gap-4">
          <div>
            <h2 id="image-picker-title" className="text-xl font-black text-gray-950">{title}</h2>
            <p className="mt-1 text-sm text-gray-500">已选 {selected.length}/{maxSelect} 张；按点击顺序排列，第一张为主图。</p>
          </div>
          <button type="button" onClick={onClose} aria-label="关闭图片选择" className="flex h-10 w-10 items-center justify-center rounded-2xl bg-gray-100 hover:bg-gray-200"><X className="h-5 w-5 text-gray-600" /></button>
        </div>
        <div className="modal-body space-y-4">
          <div className="flex flex-wrap items-center gap-3">
            <ImageUploadButton busy={library.busy} onUpload={library.upload} onResults={setNotice} />
            <button type="button" disabled={visible.length === 0 || selected.length >= maxSelect} onClick={fillVisible} className="rounded-xl ios-btn-secondary px-3 py-2.5 text-sm font-bold  disabled:opacity-50">全选当前结果（最多 {maxSelect} 张）</button>
            <button type="button" disabled={selected.length === 0} onClick={/* 清空已选图片。 */ () => setSelected([])} className="rounded-xl ios-btn-secondary px-3 py-2.5 text-sm font-bold  disabled:opacity-50">清空选择</button>
            {notice && <span role="status" className="text-xs text-gray-600">{notice}</span>}
          </div>
          <ListToolbar searchLabel="搜索图片文件名" query={filters.query} onQueryChange={/* 更新搜索词。 */ query => setFilters(/* previous 是更新前的条件。 */ previous => ({ ...previous, query }))}
            shown={visible.length} total={library.images.length} active={isImageFilterActive(filters)}
            onReset={/* 清除搜索与筛选，保留当前排序。 */ () => setFilters(/* previous 提供要保留的排序。 */ previous => ({ ...defaultImageFilters, sort: previous.sort }))}
            selects={[
              { label: '格式', value: filters.format, onChange: /* 切换格式筛选。 */ value => setFilters(/* previous 是更新前的条件。 */ previous => ({ ...previous, format: value as ImageFilters['format'] })), options: [{ value: 'all', label: '全部' }, { value: 'jpg', label: 'JPG' }, { value: 'png', label: 'PNG' }, { value: 'webp', label: 'WEBP' }, { value: 'gif', label: 'GIF' }] },
              { label: '排序', value: filters.sort, onChange: /* 切换排序方式。 */ value => setFilters(/* previous 是更新前的条件。 */ previous => ({ ...previous, sort: value as ImageFilters['sort'] })), options: [{ value: 'newest', label: '最新上传' }, { value: 'oldest', label: '最早上传' }, { value: 'name-asc', label: '文件名 A→Z' }, { value: 'name-desc', label: '文件名 Z→A' }, { value: 'size-desc', label: '文件大→小' }, { value: 'size-asc', label: '文件小→大' }] },
            ]} />
          {library.error && <div role="alert" className="rounded-xl bg-red-50 px-4 py-3 text-sm text-red-700">{library.error}</div>}
          {library.loading && library.images.length === 0 ? <p className="text-sm text-gray-500">加载中...</p> : library.images.length === 0 ? <p className="rounded-2xl border border-dashed border-gray-300 p-8 text-center text-sm text-gray-500">图片库还是空的，先上传几张图片。</p> : visible.length === 0 ? <p className="p-6 text-center text-sm text-gray-500">没有匹配的图片。</p> : <ImageGrid images={visible} selectedIds={selected} onToggle={toggle} disabled={library.busy} />}
        </div>
        <div className="modal-footer"><div className="flex gap-3 w-full">
          <button type="button" onClick={onClose} className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors">取消</button>
          <button type="button" disabled={selected.length === 0} onClick={confirm} className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold disabled:opacity-50">确认选择</button>
        </div></div>
      </div>
    </div>,
    document.body,
  );
};

/** ImagePickerDialogProps 描述通用图片选择弹窗。 */
export interface ImagePickerDialogProps extends Omit<LibraryImagePickerProps, 'onPick'> {
  /** title 是弹窗标题。 */
  title?: string;
  /** initialSelected 是初始选中的图片主键。 */
  initialSelected?: number[];
  /** onConfirm 返回按选择顺序排列的图片。 */
  onConfirm: (images: LibraryImage[]) => void;
}

/** ImagePickerDialog 从图片库中选择若干图片；未打开时不发请求。 */
export const ImagePickerDialog = ({ open, title = '从图片库选择', maxSelect, initialSelected = [], onClose, onConfirm }: ImagePickerDialogProps) =>
  open ? <ImagePickerBody title={title} maxSelect={maxSelect} initialSelected={initialSelected} onClose={onClose} onConfirm={onConfirm} /> : null;
