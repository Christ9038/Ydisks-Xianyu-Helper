/** SelectionState 是“全选”复选框的三态：没有可见项被选中、部分被选中、全部被选中。 */
export type SelectionState = 'none' | 'some' | 'all';

/** selectionState 只统计当前可见项，判断全选复选框应显示的状态；可见列表为空时恒为 none。 */
export const selectionState = <T extends number | string>(visibleIds: T[], selectedIds: T[]): SelectionState => {
  // selectedSet 用于 O(1) 判断可见项是否已被选中。
  const selectedSet = new Set(selectedIds);
  // selectedVisible 是可见项中已被选中的数量。
  const selectedVisible = visibleIds.filter(/* id 保留已被选中的可见项。 */ id => selectedSet.has(id)).length;
  if (selectedVisible === 0) return 'none';
  return selectedVisible === visibleIds.length ? 'all' : 'some';
};

/**
 * toggleAllVisible 切换“全选当前结果”：可见项已全部选中时仅取消可见项，否则把可见项补齐。
 * 不在可见范围内的已选项（被筛选隐藏）保持不变，补齐的项追加在末尾以保持既有选择顺序。
 */
export const toggleAllVisible = <T extends number | string>(selectedIds: T[], visibleIds: T[]): T[] => {
  if (selectionState(visibleIds, selectedIds) === 'all') {
    // visibleSet 是需要取消选择的可见项集合。
    const visibleSet = new Set(visibleIds);
    return selectedIds.filter(/* id 保留不在可见范围内的已选项。 */ id => !visibleSet.has(id));
  }
  // selectedSet 是已选项集合，用于只追加尚未选中的可见项。
  const selectedSet = new Set(selectedIds);
  return [...selectedIds, ...visibleIds.filter(/* id 只追加尚未选中的可见项。 */ id => !selectedSet.has(id))];
};

/** fillToLimit 按可见顺序把未选中的可见项追加到选择末尾，直到总数达到 max 上限；已选项顺序不变。 */
export const fillToLimit = <T extends number | string>(selectedIds: T[], visibleIds: T[], max: number): T[] => {
  // result 是在原选择基础上逐项追加的结果。
  const result = [...selectedIds];
  // selectedSet 记录已选项，避免重复追加。
  const selectedSet = new Set(selectedIds);
  for (const /* id 是按可见顺序遍历到的候选项。 */ id of visibleIds) {
    if (result.length >= max) break;
    if (!selectedSet.has(id)) result.push(id);
  }
  return result;
};

/** countHidden 统计已选项中不在当前可见范围内（被搜索或筛选隐藏）的数量。 */
export const countHidden = <T extends number | string>(selectedIds: T[], visibleIds: T[]): number => {
  // visibleSet 是当前可见项集合。
  const visibleSet = new Set(visibleIds);
  return selectedIds.filter(/* id 保留不在可见范围内的已选项。 */ id => !visibleSet.has(id)).length;
};
