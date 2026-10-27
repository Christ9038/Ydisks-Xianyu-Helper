import { useRef, useState } from 'react';
import { fetchLibraryImageFile } from '../api';
import type { LibraryImage, LibraryImagePickerProps } from '../types';
import { ImagePickerDialog } from './ImagePickerDialog';

/** LibraryImagePicker 供单品发布使用：选择图片库图片后取回字节并交给调用方作为普通文件。 */
export const LibraryImagePicker = ({ open, maxSelect, onClose, onPick }: LibraryImagePickerProps) => {
  // error 是取回图片字节失败时的提示。
  const [error, setError] = useState('');
  // controllerRef 保存当前取回请求的取消控制器，重复确认时先取消旧请求。
  const controllerRef = useRef<AbortController | null>(null);

  /** confirm 并行取回所选图片文件；任何一张失败都保留弹窗并提示。 */
  const confirm = async (images: LibraryImage[]): Promise<void> => {
    controllerRef.current?.abort();
    // controller 控制本次取回。
    const controller = new AbortController();
    controllerRef.current = controller;
    try {
      // files 是按选择顺序排列的文件。
      const files = await Promise.all(images.map(/* image 取回对应字节。 */ image => fetchLibraryImageFile(image, controller.signal)));
      if (controller.signal.aborted) return;
      setError('');
      onPick(files);
    } catch (failure /* failure 是取回失败原因。 */) {
      if (!controller.signal.aborted) setError(failure instanceof Error ? failure.message : '读取图片失败');
    }
  };

  return (
    <>
      <ImagePickerDialog open={open} maxSelect={maxSelect} onClose={onClose} onConfirm={/* 确认后异步取回文件。 */ images => void confirm(images)} />
      {open && error && <div role="alert" className="fixed bottom-6 left-1/2 z-[10000] -translate-x-1/2 rounded-xl bg-red-600 px-4 py-2 text-sm text-white shadow-lg">{error}</div>}
    </>
  );
};

export default LibraryImagePicker;
