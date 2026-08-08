import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, test } from 'vitest';

const source = (path: string) => readFileSync(resolve(__dirname, path), 'utf8');

describe('responsive rules layout', () => {
  test('allows the rules page to shrink inside the sidebar layout', () => {
    const app = source('App.tsx');
    const rules = source('components/Rules.tsx');
    expect(app).toContain('h-screen min-w-0 flex-1 overflow-x-hidden');
    expect(rules).toContain('min-w-0 space-y-8');
    expect(rules).toContain('xl:grid-cols-[minmax(270px,0.72fr)_minmax(0,1.28fr)]');
    expect(rules).not.toContain('2xl:grid-cols-[360px_1fr]');
  });
});
