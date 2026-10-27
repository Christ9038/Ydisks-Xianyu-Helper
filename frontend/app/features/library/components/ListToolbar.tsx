import { RotateCcw, Search } from 'lucide-react';

/** ToolbarOption 是下拉框的一个选项。 */
export interface ToolbarOption {
  /** value 是提交给父级的取值。 */
  value: string;
  /** label 是展示文字。 */
  label: string;
}

/** ToolbarSelect 描述工具条中的一个筛选或排序下拉框。 */
export interface ToolbarSelect {
  /** label 是下拉框的可访问名称，同时作为可见前缀。 */
  label: string;
  /** value 是当前取值。 */
  value: string;
  /** options 是可选项。 */
  options: ToolbarOption[];
  /** onChange 接收新取值，由父级转换为具体筛选类型。 */
  onChange: (value: string) => void;
}

/** ListToolbarProps 描述搜索、筛选、排序和计数的通用工具条。 */
export interface ListToolbarProps {
  /** searchLabel 是搜索框的可访问名称与占位文字。 */
  searchLabel: string;
  /** query 是当前搜索词。 */
  query: string;
  /** onQueryChange 接收新的搜索词。 */
  onQueryChange: (query: string) => void;
  /** selects 是筛选与排序下拉框。 */
  selects: ToolbarSelect[];
  /** shown 是当前可见条数；total 是总条数。 */
  shown: number;
  /** total 是列表总条数，用于“显示 X / 共 Y”。 */
  total: number;
  /** active 表示有搜索或筛选生效，此时显示重置按钮。 */
  active: boolean;
  /** onReset 清除搜索和筛选（保留排序）。 */
  onReset: () => void;
}

/** ListToolbar 是模板与图片列表共用的工具条，不持有状态，全部由父级推导。 */
export const ListToolbar = ({ searchLabel, query, onQueryChange, selects, shown, total, active, onReset }: ListToolbarProps) => (
  <div className="flex flex-wrap items-center gap-3">
    <div className="relative min-w-56">
      <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
      <input type="search" aria-label={searchLabel} placeholder={searchLabel} className="ios-input w-full rounded-xl py-2.5 pl-9 pr-3 text-sm" value={query} onChange={/* 更新搜索词。 */ event => onQueryChange(event.target.value)} />
    </div>
    {selects.map(/* select 渲染一个筛选或排序下拉框。 */ select => (
      <label key={select.label} className="inline-flex items-center gap-1.5 text-xs font-bold text-slate-500">
        {select.label}
        <select aria-label={select.label} className="ios-input rounded-xl px-2.5 py-2 text-sm font-medium text-slate-800" value={select.value} onChange={/* 切换取值。 */ event => select.onChange(event.target.value)}>
          {select.options.map(/* option 渲染一个选项。 */ option => <option key={option.value} value={option.value}>{option.label}</option>)}
        </select>
      </label>
    ))}
    <span className="text-xs text-slate-500">显示 {shown} / 共 {total}</span>
    {active && <button type="button" onClick={onReset} className="inline-flex items-center gap-1 rounded-lg bg-slate-100 px-2.5 py-1.5 text-xs font-bold text-slate-700"><RotateCcw className="h-3.5 w-3.5" />重置筛选</button>}
  </div>
);
