import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, test } from 'vitest';

const itemList = readFileSync(resolve(__dirname, 'components/ItemList.tsx'), 'utf8');

describe('item list primary action colors', () => {
  test('uses the shared primary blue for batch publishing', () => {
    expect(itemList).toContain('bg-brand text-white hover:bg-brand-highlight');
    expect(itemList).not.toContain('bg-blue-600 text-white hover:bg-blue-700');
  });

  test('uses the lighter emerald tone for publishing actions', () => {
    expect(itemList).toContain('bg-emerald-500 text-white hover:bg-emerald-600');
    expect(itemList).not.toContain('bg-emerald-600 text-white hover:bg-emerald-700');
  });
});
