import { readdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, test } from 'vitest';

const componentsDir = resolve(__dirname, 'components');
const pageSources = [
  resolve(__dirname, 'App.tsx'),
  ...readdirSync(componentsDir)
    .filter((fileName) => fileName.endsWith('.tsx') && !fileName.endsWith('.test.tsx'))
    .map((fileName) => resolve(componentsDir, fileName)),
];

// rgb(var(--color-*)), CSS classes and the central index.css are references.
// Literal colors in page code would bypass the design-token system.
const hardCodedColorPattern = /#[0-9a-f]{3,8}\b|rgba?\((?!var\(--color-)/gi;

describe('global color token contract', () => {
  test('page components do not contain hard-coded color values', () => {
    const violations = pageSources.flatMap((filePath) => {
      const source = readFileSync(filePath, 'utf8');
      return [...source.matchAll(hardCodedColorPattern)].map((match) => `${filePath}:${match[0]}`);
    });

    expect(violations).toEqual([]);
  });

  test('the primary brand and highlight colors are defined in the central stylesheet', () => {
    const globalStyles = readFileSync(resolve(__dirname, 'index.css'), 'utf8');

    expect(globalStyles).toContain('--color-brand: 0 148 247;');
    expect(globalStyles).toContain('--color-brand-highlight: 0 113 227;');
    expect(globalStyles).toContain('--color-success-500:');
    expect(globalStyles).toContain('--color-warning-500:');
    expect(globalStyles).toContain('--color-danger-500:');
  });
});
