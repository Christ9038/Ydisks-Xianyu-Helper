import React, { useState } from 'react';

/** SearchableSelectOption 描述可搜索下拉框中的一个可选项。 */
export interface SearchableSelectOption {
  /** value 是选项的数值主键，提交给业务层；0 不会作为有效选项出现。 */
  value: number;
  /** label 是展示并参与搜索匹配的名称。 */
  label: string;
}

/** SearchableSelectProps 描述可搜索下拉框的输入。 */
export interface SearchableSelectProps {
  /** options 是全部候选项；过滤只发生在组件内部，不修改该数组。 */
  options: SearchableSelectOption[];
  /** value 是当前选中项的主键；0 或空表示未选择。 */
  value: number | undefined;
  /** onChange 在用户选择选项时回传主键；选择占位项时回传 0。 */
  onChange: (value: number) => void;
  /** placeholder 是未选择时占位项的文案。 */
  placeholder: string;
  /** ariaLabel 是搜索框与下拉框共用的无障碍名称前缀。 */
  ariaLabel: string;
  /** className 是下拉框自身的样式类名，搜索框使用同源的紧凑样式。 */
  className?: string;
}

/**
 * filterSearchableOptions 按名称做不区分大小写的包含匹配，并始终保留当前选中项，
 * 避免用户输入关键字后已选值从下拉框消失而被误认为被清空。
 */
export const filterSearchableOptions = (options: SearchableSelectOption[], keyword: string, selected: number | undefined): SearchableSelectOption[] => {
  // normalized 是去除首尾空白并小写后的搜索词。
  const normalized = keyword.trim().toLowerCase();
  if (!normalized) return options;
  return options.filter(/* option 是待判断的候选项；命中名称或为当前选中项时保留。 */ option => option.value === selected || option.label.toLowerCase().includes(normalized));
};

/** MAX_OPTION_LABEL_LENGTH 是下拉项展示名称的最大字符数；原生下拉弹层宽度随最长选项增长，超长名称会撑出屏幕。 */
export const MAX_OPTION_LABEL_LENGTH = 40;

/** truncateOptionLabel 把过长名称压缩为单行省略展示（含换行折叠），完整名称仍保留在 title 与搜索中。 */
export const truncateOptionLabel = (label: string): string => {
  // flat 是把连续空白（含换行）折叠为单个空格后的名称。
  const flat = label.replace(/\s+/g, ' ').trim();
  return flat.length > MAX_OPTION_LABEL_LENGTH ? `${flat.slice(0, MAX_OPTION_LABEL_LENGTH)}…` : flat;
};

/** SearchableSelect 在原生下拉框上方提供名称搜索，适合卡密库存较多的选择场景。 */
const SearchableSelect: React.FC<SearchableSelectProps> = ({ options, value, onChange, placeholder, ariaLabel, className = 'w-full ios-input px-3 py-2.5 rounded-lg' }) => {
  // keyword 保存用户在搜索框中输入的临时关键字，仅属于本组件的瞬时界面状态。
  const [keyword, setKeyword] = useState('');
  // visibleOptions 是由关键字和当前选中项推导出的可见候选项，不存入状态。
  const visibleOptions = filterSearchableOptions(options, keyword, value);
  return (
    <div className="space-y-1.5">
      <input
        type="search"
        value={keyword}
        onChange={/* event 是搜索框输入事件，只更新本地关键字。 */ event => setKeyword(event.target.value)}
        placeholder="搜索名称..."
        aria-label={`${ariaLabel}搜索`}
        className={className}
      />
      <select
        value={value || ''}
        onChange={/* event 是下拉框选择事件；空值统一回传 0 表示未选择。 */ event => onChange(Number(event.target.value) || 0)}
        aria-label={ariaLabel}
        className={className}
      >
        <option value="">{placeholder}</option>
        {visibleOptions.map(/* option 是待渲染的候选项。 */ option => <option key={option.value} value={option.value} title={option.label}>{truncateOptionLabel(option.label)}</option>)}
      </select>
      {keyword.trim() && visibleOptions.length === 0 && <p className="text-xs text-gray-400">没有匹配“{keyword.trim()}”的选项</p>}
    </div>
  );
};

export default SearchableSelect;
