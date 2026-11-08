// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test, vi } from 'vitest';
import SearchableSelect, { filterSearchableOptions, MAX_OPTION_LABEL_LENGTH, truncateOptionLabel } from './SearchableSelect';

// optionsFixture 是包含大小写混合名称的卡密库存候选项。
const optionsFixture = [
  { value: 1, label: 'Netflix 月卡' },
  { value: 2, label: '会员兑换码' },
  { value: 3, label: 'netflix 年卡' },
];

afterEach(cleanup);

describe('filterSearchableOptions', /* 覆盖关键字过滤的全部分支。 */ () => {
  test('returns every option for a blank keyword', /* 空白关键字不过滤。 */ () => {
    expect(filterSearchableOptions(optionsFixture, '   ', undefined)).toEqual(optionsFixture);
  });

  test('matches names case-insensitively', /* 名称匹配不区分大小写。 */ () => {
    expect(filterSearchableOptions(optionsFixture, 'NETFLIX', undefined).map(/* option 取主键。 */ option => option.value)).toEqual([1, 3]);
  });

  test('keeps the selected option even when it does not match', /* 已选项始终保留。 */ () => {
    expect(filterSearchableOptions(optionsFixture, 'netflix', 2).map(/* option 取主键。 */ option => option.value)).toEqual([1, 2, 3]);
  });
});

describe('SearchableSelect', /* 覆盖组件交互。 */ () => {
  test('filters options by typed keyword and reports the selected id', /* 输入关键字后选择匹配项。 */ () => {
    // onChange 记录组件回传的选中主键。
    const onChange = vi.fn();
    render(<SearchableSelect options={optionsFixture} value={undefined} onChange={onChange} placeholder="请选择" ariaLabel="卡密库存" />);
    fireEvent.change(screen.getByLabelText('卡密库存搜索'), { target: { value: '兑换' } });
    expect(screen.queryByRole('option', { name: 'Netflix 月卡' })).toBeNull();
    fireEvent.change(screen.getByLabelText('卡密库存'), { target: { value: '2' } });
    expect(onChange).toHaveBeenCalledWith(2);
  });

  test('reports zero when the placeholder is chosen and shows an empty hint', /* 选择占位项回传 0，无匹配时提示。 */ () => {
    // onChange 记录组件回传的选中主键。
    const onChange = vi.fn();
    render(<SearchableSelect options={optionsFixture} value={1} onChange={onChange} placeholder="请选择" ariaLabel="卡密库存" />);
    fireEvent.change(screen.getByLabelText('卡密库存'), { target: { value: '' } });
    expect(onChange).toHaveBeenCalledWith(0);
    cleanup();
    render(<SearchableSelect options={optionsFixture} value={undefined} onChange={onChange} placeholder="请选择" ariaLabel="卡密库存" />);
    fireEvent.change(screen.getByLabelText('卡密库存搜索'), { target: { value: '不存在' } });
    expect(screen.getByText(/没有匹配/)).toBeTruthy();
  });
});

describe('truncateOptionLabel', /* 覆盖超长名称截断与短名称原样展示。 */ () => {
  test('keeps short labels and truncates long multi-line labels', /* 短名称不变，长名称折叠空白并加省略号。 */ () => {
    expect(truncateOptionLabel('Netflix 月卡')).toBe('Netflix 月卡');
    // longLabel 是模拟整段文案被用作卡密名称的超长名称。
    const longLabel = `开店\n选品${'上架'.repeat(100)}`;
    // shown 是截断后的展示文本。
    const shown = truncateOptionLabel(longLabel);
    expect(shown.length).toBe(MAX_OPTION_LABEL_LENGTH + 1);
    expect(shown.endsWith('…')).toBe(true);
    expect(shown.includes('\n')).toBe(false);
  });

  test('renders truncated text with full title and still searches the full name', /* 展示截断但搜索与 title 使用完整名称。 */ () => {
    // fullLabel 是超长名称，关键字位于被截断的尾部。
    const fullLabel = `${'前缀'.repeat(40)}尾部关键字`;
    render(<SearchableSelect options={[{ value: 9, label: fullLabel }]} value={undefined} onChange={vi.fn()} placeholder="请选择" ariaLabel="卡密库存" />);
    fireEvent.change(screen.getByLabelText('卡密库存搜索'), { target: { value: '尾部关键字' } });
    // option 是渲染出的候选项元素。
    const option = screen.getByTitle(fullLabel);
    expect(option.textContent?.endsWith('…')).toBe(true);
  });
});
