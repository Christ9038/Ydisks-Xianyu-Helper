import { X } from 'lucide-react';
import { useState } from 'react';
import { createPortal } from 'react-dom';
import { useLibraryImages } from '../hooks';
import type { LibraryImage, LibraryImagePickerProps } from '../types';
import { ImageGrid } from './ImageGrid';
import { ImageUploadButton } from './ImageUploadButton';

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

  /** toggle 切换选中状态，达到上限后忽略新增。 */
  const toggle = (image: LibraryImage): void => setSelected(/* previous 是切换前的选择。 */ previous => previous.includes(image.id) ? previous.filter(/* id 移除当前图片。 */ id => id !== image.id) : previous.length >= maxSelect ? previous : [...previous, image.id]);

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
            {notice && <span role="status" className="text-xs text-slate-600">{notice}</span>}
          </div>
          {library.error && <div role="alert" className="rounded-xl bg-red-50 px-4 py-3 text-sm text-red-700">{library.error}</div>}
          {library.loading && library.images.length === 0 ? <p className="text-sm text-slate-500">加载中...</p> : library.images.length === 0 ? <p className="rounded-2xl border border-dashed border-slate-300 p-8 text-center text-sm text-slate-500">图片库还是空的，先上传几张图片。</p> : <ImageGrid images={library.images} selectedIds={selected} onToggle={toggle} disabled={library.busy} />}
        </div>
        <div className="modal-footer flex justify-end gap-3">
          <button type="button" onClick={onClose} className="rounded-xl bg-gray-100 px-4 py-2.5 text-sm font-bold text-gray-700">取消</button>
          <button type="button" disabled={selected.length === 0} onClick={confirm} className="rounded-xl bg-emerald-600 px-4 py-2.5 text-sm font-bold text-white disabled:opacity-50">确认选择</button>
        </div>
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
