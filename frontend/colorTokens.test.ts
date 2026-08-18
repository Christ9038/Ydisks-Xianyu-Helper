import { readdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, test } from 'vitest';

// featureRoot 是 feature 页面与专属组件所在的目录；根 components 已由阶段七迁移移除。
const featureRoot = resolve(__dirname, 'app/features');
// sharedUIRoot 是跨 feature 复用的纯展示组件目录。
const sharedUIRoot = resolve(__dirname, 'shared/ui');
// collectTSXSources 递归收集目标目录内非测试 TSX，供设计令牌契约审计使用。
const collectTSXSources = (directory: string): string[] => readdirSync(directory, { withFileTypes: true }).flatMap(/* sourceEntryMapper 处理当前目录的一个文件或子目录。 */ entry => {
  // entryPath 是当前文件系统条目的绝对路径。
  const entryPath = resolve(directory, entry.name);
  if (entry.isDirectory()) return collectTSXSources(entryPath);
  return entry.name.endsWith('.tsx') && !entry.name.endsWith('.test.tsx') ? [entryPath] : [];
});
// pageSources 是根应用、feature 页面和共享 UI 的生产 TSX 源码集合。
const pageSources = [
  resolve(__dirname, 'App.tsx'),
  ...collectTSXSources(featureRoot),
  ...collectTSXSources(sharedUIRoot),
];

// rgb(var(--color-*)), CSS classes and the central index.css are references.
// Literal colors in page code would bypass the design-token system.
const hardCodedColorPattern = /#[0-9a-f]{3,8}\b|rgba?\((?!var\(--color-)/gi; /* hardCodedColorPattern 表示hardCodedColorPattern。 */

describe('global color token contract', () => {
  test('page components do not contain hard-coded color values', () => {
    const violations = pageSources.flatMap((filePath) => {
      const source = readFileSync(filePath, 'utf8'); /* source 表示source。 */
      return [...source.matchAll(hardCodedColorPattern)].map((match) => `${filePath}:${match[0]}` /* match 是命中的硬编码颜色及其源码位置。 */);
    } /* flatMap 回调读取单个页面源码并收集其中的颜色令牌违规项。 */); /* violations 汇总每个页面绕过设计令牌的颜色值。 */

    expect(violations).toEqual([]);
  } /* 测试回调断言页面源码不直接写入色值。 */);

  test('the primary brand and highlight colors are defined in the central stylesheet', () => {
    const globalStyles = readFileSync(resolve(__dirname, 'index.css'), 'utf8'); /* globalStyles 表示globalStyles。 */

    expect(globalStyles).toContain('--color-brand: 0 148 247;');
    expect(globalStyles).toContain('--color-brand-highlight: 0 113 227;');
    expect(globalStyles).toContain('--color-success-500:');
    expect(globalStyles).toContain('--color-warning-500:');
    expect(globalStyles).toContain('--color-danger-500:');
  } /* 测试回调断言全局样式提供全部核心色彩令牌。 */);
} /* 测试套件回调汇总页面颜色令牌的静态契约。 */);
