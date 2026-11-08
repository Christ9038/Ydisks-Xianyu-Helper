import type { PublishCategory } from '../types';

/** CategoryCandidatePickerProps 描述类目候选选择列表的输入。 */
export interface CategoryCandidatePickerProps {
  /** candidates 是平台按推荐优先级返回的候选类目；少于两项时没有可改选的内容，组件不渲染。 */
  candidates: PublishCategory[];
  /** selectedCatId 是当前已选中的类目 ID，用于高亮；为空表示用户尚未或不再选择类目，此时不展示候选。 */
  selectedCatId?: string;
  /** onSelect 在用户点选某个候选类目时触发。 */
  onSelect: (category: PublishCategory) => void;
}

/** CategoryCandidatePicker 展示类目推荐返回的全部候选，让用户改选“游戏软件平台”等非首选类目。 */
export const CategoryCandidatePicker = ({ candidates, selectedCatId, onSelect }: CategoryCandidatePickerProps) => candidates.length < 2 || !selectedCatId ? null : (
  <div className="flex flex-wrap gap-2">
    {candidates.map(/* candidate 表示当前渲染的候选类目。 */ candidate => (
      <button key={candidate.cat_id} type="button" aria-pressed={candidate.cat_id === selectedCatId} onClick={/* 点选候选类目并交给调用方写回表单。 */ () => onSelect(candidate)} className="rounded-full border border-amber-300 bg-white px-3 py-1 text-xs font-bold aria-pressed:bg-amber-500 aria-pressed:text-white">
        {candidate.cat_name}
      </button>
    ))}
  </div>
);
