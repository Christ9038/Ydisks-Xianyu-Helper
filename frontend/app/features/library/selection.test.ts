import { describe, expect, test } from 'vitest';
import { countHidden, fillToLimit, selectionState, toggleAllVisible } from './selection';

describe('selection', /* 全选三态与可见范围内的选择运算。 */ () => {
  test('selectionState 只统计可见项并区分三态', /* 三态分支。 */ () => {
    expect(selectionState([], [1])).toBe('none');
    expect(selectionState([1, 2], [9])).toBe('none');
    expect(selectionState([1, 2], [2, 9])).toBe('some');
    expect(selectionState([1, 2], [2, 1, 9])).toBe('all');
  });

  test('toggleAllVisible 补齐可见项，全选时只取消可见项并保留被隐藏的已选项', /* 补齐与取消分支。 */ () => {
    expect(toggleAllVisible([9, 1], [1, 2, 3])).toEqual([9, 1, 2, 3]);
    expect(toggleAllVisible([9, 1, 2, 3], [1, 2, 3])).toEqual([9]);
    expect(toggleAllVisible<number>([], [])).toEqual([]);
  });

  test('fillToLimit 保持已选顺序，按可见顺序追加并截断到上限', /* 上限分支。 */ () => {
    expect(fillToLimit([5], [1, 5, 2, 3], 3)).toEqual([5, 1, 2]);
    expect(fillToLimit([5, 6, 7], [1], 3)).toEqual([5, 6, 7]);
    expect(fillToLimit<number>([], [1, 2], 10)).toEqual([1, 2]);
  });

  test('countHidden 统计不在可见范围内的已选项', /* 隐藏计数。 */ () => {
    expect(countHidden([1, 2, 9], [1, 2])).toBe(1);
    expect(countHidden([1], [1])).toBe(0);
  });
});
