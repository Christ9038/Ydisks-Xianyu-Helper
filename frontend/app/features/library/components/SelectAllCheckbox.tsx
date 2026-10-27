import { useEffect, useRef } from 'react';
import type { SelectionState } from '../selection';

/** SelectAllCheckboxProps 描述三态全选复选框。 */
export interface SelectAllCheckboxProps {
  /** state 是当前可见项的选择状态：none 未选、some 部分选中（显示横线）、all 全部选中。 */
  state: SelectionState;
  /** label 是复选框的可访问名称与文字说明。 */
  label: string;
  /** disabled 在没有可见项或操作进行中时禁用。 */
  disabled?: boolean;
  /** onToggle 在用户点击时触发，由父级决定全选或取消。 */
  onToggle: () => void;
}

/** SelectAllCheckbox 渲染带文字的三态复选框；indeterminate 只能通过 DOM 属性设置，所以用 ref 同步。 */
export const SelectAllCheckbox = ({ state, label, disabled = false, onToggle }: SelectAllCheckboxProps) => {
  // inputRef 指向原生复选框，用于同步 indeterminate 属性。
  const inputRef = useRef<HTMLInputElement>(null);
  useEffect(/* 状态变化后同步部分选中的视觉状态。 */ () => {
    if (inputRef.current) inputRef.current.indeterminate = state === 'some';
  }, [state]);
  return (
    <label className="inline-flex cursor-pointer items-center gap-2 text-sm font-bold text-slate-700">
      <input ref={inputRef} type="checkbox" aria-label={label} checked={state === 'all'} disabled={disabled} onChange={onToggle} />
      {label}
    </label>
  );
};
