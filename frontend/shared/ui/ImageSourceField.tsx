import { useState } from 'react';
import {
  emptyImageSource,
  libraryImageContentUrl,
  withImageUrl,
  withLibraryImage,
  type ImageSourceValue,
  type LibraryImageChooserComponent,
} from './imageSource';

/** ImageSourceFieldProps 描述图片来源输入控件。 */
export interface ImageSourceFieldProps {
  /** value 是当前图片来源。 */
  value: ImageSourceValue;
  /** onChange 返回用户修改后的图片来源。 */
  onChange: (next: ImageSourceValue) => void;
  /** chooser 是应用壳注入的图片库选择器；缺省时只能填写外链。 */
  chooser?: LibraryImageChooserComponent;
  /** urlPlaceholder 是外链输入框的占位提示。 */
  urlPlaceholder?: string;
  /** allowUrl 为 false 时隐藏外链输入，只允许图片库图片。 */
  allowUrl?: boolean;
}

/** ImageSourceField 让用户在“外链 URL”和“图片库图片”之间二选一，选择一种会清除另一种。 */
export const ImageSourceField = ({ value, onChange, chooser: Chooser, urlPlaceholder = 'https://example.com/image.jpg', allowUrl = true }: ImageSourceFieldProps) => {
  // open 表示图片库选择器是否打开，属于瞬时界面状态。
  const [open, setOpen] = useState(false);

  return (
    <div className="space-y-2">
      {value.libraryImageId > 0 ? (
        <div className="flex items-center gap-3 rounded-xl border border-slate-200 p-2">
          <img src={libraryImageContentUrl(value.libraryImageId)} alt="已选图片库图片" className="h-14 w-14 rounded-lg bg-slate-100 object-cover" />
          <span className="flex-1 text-sm text-slate-600">图片库图片 #{value.libraryImageId}</span>
          {Chooser && <button type="button" onClick={/* 打开选择器更换图片。 */ () => setOpen(true)} className="text-sm text-blue-600">更换</button>}
          <button type="button" onClick={/* 清除当前图片来源。 */ () => onChange(emptyImageSource)} className="text-sm text-red-600">清除</button>
        </div>
      ) : (
        <div className="flex gap-2">
          {allowUrl && <input value={value.url} onChange={/* 输入外链会清除图片库引用。 */ event => onChange(withImageUrl(event.target.value))} placeholder={urlPlaceholder} className="w-full ios-input px-4 py-3 rounded-xl" />}
          {Chooser && <button type="button" onClick={/* 打开图片库选择器。 */ () => setOpen(true)} className="shrink-0 rounded-xl border border-slate-200 px-3 text-sm text-blue-600">从图片库选择</button>}
        </div>
      )}
      {Chooser && <Chooser open={open} onClose={/* 关闭选择器。 */ () => setOpen(false)} onSelect={/* 选中后切换为图片库图片并关闭。 */ image => { onChange(withLibraryImage(image.id)); setOpen(false); }} />}
    </div>
  );
};
