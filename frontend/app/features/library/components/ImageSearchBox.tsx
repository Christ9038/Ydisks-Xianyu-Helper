import { Search } from 'lucide-react';

/** ImageSearchBoxProps 描述图片搜索框。 */
export interface ImageSearchBoxProps {
  /** value 是当前搜索词。 */
  value: string;
  /** onChange 接收新的搜索词。 */
  onChange: (value: string) => void;
}

/** ImageSearchBox 按文件名搜索图片。 */
export const ImageSearchBox = ({ value, onChange }: ImageSearchBoxProps) => (
  <div className="relative min-w-56">
    <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
    <input type="search" aria-label="搜索图片文件名" placeholder="搜索图片文件名" className="ios-input w-full rounded-xl py-2.5 pl-9 pr-3 text-sm" value={value} onChange={/* 更新搜索词。 */ event => onChange(event.target.value)} />
  </div>
);
