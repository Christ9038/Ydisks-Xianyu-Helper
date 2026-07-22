import { describe, expect, test } from 'vitest';
import type { Card } from '../types';
import { filterCards } from './cardListState';

const cards = [
  { id: 1, name: '年终总结 PPT', type: 'text' },
  { id: 2, name: '年终总结兑换码', type: 'data' },
  { id: 3, name: '产品介绍 PPT', type: 'text' },
] as Card[];

describe('filterCards', () => {
  test('combines type and name filters with AND semantics', () => {
    expect(filterCards(cards, 'text', '年终')).toEqual([cards[0]]);
  });

  test('matches names case-insensitively and ignores surrounding whitespace', () => {
    expect(filterCards(cards, '', '  ppt ')).toEqual([cards[0], cards[2]]);
  });
});
