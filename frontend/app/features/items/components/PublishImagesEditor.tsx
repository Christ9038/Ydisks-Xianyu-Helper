import { ChevronDown,ChevronUp,GripVertical,Trash2,UploadCloud } from 'lucide-react';
import { useState,type ChangeEvent,type DragEvent } from 'react';

/** PublishImagePreview 描述一张发布图片的临时预览资源。 */
export interface PublishImagePreview {
  /** key 是图片文件和当前顺序组成的渲染键。 */
  key: string;
  /** url 是浏览器生成的本地预览地址。 */
  url: string;
}

/** PublishImagesEditorProps 描述商品图片列表与发布表单之间的状态边界。 */
export interface PublishImagesEditorProps {
  /** images 是当前已经加入发布草稿的图片文件，顺序决定平台主图顺序。 */
  images: File[];
  /** previews 是与 images 同序的本地预览地址。 */
  previews: PublishImagePreview[];
  /** maxImages 是平台允许的商品主图数量上限。 */
  maxImages?: number;
  /** onChange 接收追加、删除或排序后的完整图片列表。 */
  onChange: (images: File[]) => void;
  /** onPickFromLibrary 打开图片库选择器；缺省时不显示入口。 */
  onPickFromLibrary?: () => void;
}

/** PublishImagesEditor 提供可追加、删除和排序的商品主图管理区。 */
export const PublishImagesEditor = ({ images, previews, maxImages = 9, onChange, onPickFromLibrary }: PublishImagesEditorProps) => {
  // dragIndex 保存当前正在拖拽的图片位置。
  const [dragIndex, setDragIndex] = useState<number | null>(null);

  /** appendImages 将本次选择的图片追加到已有列表，而不是覆盖已有选择。 */
  const appendImages = (event: ChangeEvent<HTMLInputElement>) => {
    // selectedFiles 保存本次文件控件选择的图片快照。
    const selectedFiles = Array.from(event.currentTarget.files || []);
    // 原生控件清空后可以再次选择同一张图片并触发 change。
    event.currentTarget.value = '';
    if (selectedFiles.length === 0 || images.length >= maxImages) return;
    onChange([...images, ...selectedFiles].slice(0, maxImages));
  };

  /** removeImage 删除指定位置的商品图片。 */
  const removeImage = (index: number) => onChange(images.filter(/* imageIndex 保留未被删除的图片位置。 */ (_, imageIndex) => imageIndex !== index));

  /** moveImage 通过箭头按钮调整图片顺序，第一张图片始终是主图。 */
  const moveImage = (from: number, to: number) => {
    if (to < 0 || to >= images.length || from === to) return;
    // nextImages 保存交换顺序后的商品图片列表。
    const nextImages = [...images];
    // movedImage 保存即将移动到目标位置的图片文件。
    const [movedImage] = nextImages.splice(from, 1);
    nextImages.splice(to, 0, movedImage);
    onChange(nextImages);
  };

  /** dropImage 处理拖拽释放并完成图片排序。 */
  const dropImage = (event: DragEvent<HTMLElement>, targetIndex: number) => {
    event.preventDefault();
    if (dragIndex === null) return;
    moveImage(dragIndex, targetIndex);
    setDragIndex(null);
  };

  return (
    <section className="space-y-3" aria-labelledby="publish-images-title">
      <div className="flex items-end justify-between gap-3">
        <div>
          <h4 id="publish-images-title" className="text-sm font-extrabold text-gray-900">商品图片（1-{maxImages} 张）</h4>
          <p className="mt-1 text-xs leading-5 text-gray-600">第一张为主图；可继续添加、删除，或拖拽和使用箭头调整顺序。</p>
        </div>
        <div className="flex items-center gap-2">
          {onPickFromLibrary && images.length < maxImages && <button type="button" className="rounded-xl border border-emerald-200 bg-white px-3 py-1.5 text-xs font-bold text-emerald-700 hover:bg-emerald-50" onClick={onPickFromLibrary}>从图片库选择</button>}
          <span className="rounded-full bg-slate-200 px-3 py-1 text-xs font-bold text-slate-700">{images.length}/{maxImages}</span>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-5">
        {previews.map(/* preview、index 渲染一个可排序的商品图片卡片。 */ (preview, index) => (
          <article
            key={preview.key}
            draggable
            onDragStart={/* dragStartAction 记录当前开始拖拽的图片位置。 */ () => setDragIndex(index)}
            onDragOver={/* dragOverAction 允许图片卡片接收拖拽释放。 */ event => event.preventDefault()}
            onDrop={/* dropAction 将拖拽图片移动到目标位置。 */ event => dropImage(event, index)}
            onDragEnd={/* dragEndAction 清理已结束或取消的拖拽状态。 */ () => setDragIndex(null)}
            className={`group relative overflow-hidden rounded-2xl border bg-white shadow-sm transition ${dragIndex === index ? 'border-emerald-300 opacity-60' : 'border-slate-200 hover:border-emerald-300'}`}
          >
            <img src={preview.url} alt={`第 ${index + 1} 张商品图片`} className="aspect-square w-full object-cover" />
            <div className="absolute inset-x-0 top-0 flex items-center justify-between bg-slate-950/70 px-2 py-1.5 text-[11px] font-bold text-white">
              <span>{index === 0 ? '主图' : `第 ${index + 1} 张`}</span>
              <GripVertical className="h-3.5 w-3.5" aria-hidden="true" />
            </div>
            <div className="absolute inset-x-0 bottom-0 flex items-center justify-between gap-1 bg-white/95 p-1.5">
              <button type="button" disabled={index === 0} aria-label={`将第 ${index + 1} 张图片前移`} className="rounded-lg p-1.5 text-slate-700 hover:bg-emerald-50 hover:text-emerald-700 disabled:cursor-not-allowed disabled:opacity-30" onClick={/* moveUpAction 将图片前移一位。 */ () => moveImage(index, index - 1)}>
                <ChevronUp className="h-4 w-4" />
              </button>
              <button type="button" disabled={index === images.length - 1} aria-label={`将第 ${index + 1} 张图片后移`} className="rounded-lg p-1.5 text-slate-700 hover:bg-emerald-50 hover:text-emerald-700 disabled:cursor-not-allowed disabled:opacity-30" onClick={/* moveDownAction 将图片后移一位。 */ () => moveImage(index, index + 1)}>
                <ChevronDown className="h-4 w-4" />
              </button>
              <button type="button" aria-label={`删除第 ${index + 1} 张图片`} className="ml-auto rounded-lg p-1.5 text-slate-700 hover:bg-red-50 hover:text-red-600" onClick={/* removeAction 删除当前图片。 */ () => removeImage(index)}>
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
          </article>
        ))}

        {images.length < maxImages && <label className="flex aspect-square cursor-pointer flex-col items-center justify-center rounded-2xl border border-dashed border-slate-300 bg-slate-100 px-3 text-center text-slate-700 transition hover:border-emerald-400 hover:bg-emerald-50 hover:text-emerald-700">
          <UploadCloud className="mb-2 h-7 w-7 text-emerald-600" />
          <span className="text-sm font-extrabold">继续添加</span>
          <span className="mt-1 text-[11px] text-slate-600">支持 JPG / PNG / GIF</span>
          <input className="hidden" type="file" accept="image/*" multiple onChange={/* fileChangeAction 追加用户新选择的图片并重置控件。 */ appendImages} />
        </label>}
      </div>
    </section>
  );
};
