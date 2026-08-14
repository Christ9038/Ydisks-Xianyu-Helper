import { expect, test } from 'vitest';
import type { Card } from '../../../types';
import { canSubmitAppend, filterCards, isCurrentCardRequest, previewAppendContent } from './batchState';

// cards 是卡密筛选测试使用的最小卡密组列表。
const cards = [
  { id: 1, name: '年终总结 PPT', type: 'text', enabled: true },
  { id: 2, name: '年终总结兑换码', type: 'data', enabled: true },
  { id: 3, name: '产品介绍 PPT', type: 'text', enabled: false },
] as Card[];

test('卡密筛选同时满足类型和名称条件',
  // 类型和名称筛选必须使用 AND 语义，避免展示无关库存。
  () => {
    expect(filterCards(cards, 'text', '年终')).toEqual([cards[0]]);
    expect(filterCards(cards, '', ' ppt ')).toEqual([cards[0], cards[2]]);
  });

test('追加预览会去除空行并裁剪每行空白',
  // 预览数量与最终提交内容共用同一套规范化规则。
  () => {
    expect(previewAppendContent(' VIP-001\n\nVIP-002  \n  ')).toEqual(['VIP-001', 'VIP-002']);
    expect(canSubmitAppend('2', ' VIP-001\n', false)).toBe(true);
    expect(canSubmitAppend('2', '\n', false)).toBe(false);
    expect(canSubmitAppend('2', 'VIP-001', true)).toBe(false);
  });

test('追加目标切换后拒绝旧请求响应和旧代次响应',
  // 用户切换卡密组或关闭弹窗后，旧追加请求不能覆盖当前状态。
  () => {
    expect(isCurrentCardRequest(3, 3, '2', '2')).toBe(true);
    expect(isCurrentCardRequest(2, 3, '2', '2')).toBe(false);
    expect(isCurrentCardRequest(3, 3, '2', '3')).toBe(false);
  });
