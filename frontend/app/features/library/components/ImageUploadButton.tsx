import { UploadCloud } from 'lucide-react';
import type { ChangeEvent } from 'react';
import type { LibraryUploadResult } from '../types';

/** ImageUploadButtonProps 描述上传按钮的行为。 */
export interface ImageUploadButtonProps {
  /** busy 表示上传或删除进行中。 */
  busy: boolean;
  /** onUpload 上传所选文件并返回逐文件结果。 */
  onUpload: (files: File[]) => Promise<LibraryUploadResult[]>;
  /** onResults 接收失败/重复摘要，由父级展示。 */
  onResults: (summary: string) => void;
}

/** summarizeUpload 汇总一次上传的成功、重复与失败文件。 */
export const summarizeUpload = (results: LibraryUploadResult[]): string => {
  // failed 是失败文件说明。
  const failed = results.filter(/* result 保留失败项。 */ result => result.error).map(/* result 转换为“文件：原因”。 */ result => `${result.filename}：${result.error}`);
  // added 是新入库数量，duplicate 是已存在数量。
  const added = results.filter(/* result 保留新入库项。 */ result => !result.error && !result.duplicate).length;
  const duplicate = results.filter(/* result 保留重复项。 */ result => !result.error && result.duplicate).length;
  // parts 是摘要片段。
  const parts = [`新增 ${added} 张`];
  if (duplicate) parts.push(`${duplicate} 张已存在`);
  if (failed.length) parts.push(`失败 ${failed.length} 张（${failed.join('；')}）`);
  return parts.join('，');
};

/** ImageUploadButton 选择多张图片并上传到图片库。 */
export const ImageUploadButton = ({ busy, onUpload, onResults }: ImageUploadButtonProps) => {
  /** handleChange 读取所选文件、清空控件并上传。 */
  const handleChange = async (event: ChangeEvent<HTMLInputElement>): Promise<void> => {
    // files 是所选文件快照。
    const files = Array.from(event.currentTarget.files ?? []);
    event.currentTarget.value = '';
    if (files.length === 0) return;
    onResults(summarizeUpload(await onUpload(files)));
  };
  return (
    <label className={`inline-flex cursor-pointer items-center gap-2 rounded-xl bg-slate-950 px-4 py-2.5 text-sm font-bold text-white hover:bg-slate-800 ${busy ? 'pointer-events-none opacity-50' : ''}`}>
      <UploadCloud className="h-4 w-4" />{busy ? '处理中...' : '上传图片'}
      <input className="hidden" type="file" accept="image/png,image/jpeg,image/gif,image/webp" multiple disabled={busy} onChange={/* 选择文件后立即上传。 */ event => void handleChange(event)} />
    </label>
  );
};
