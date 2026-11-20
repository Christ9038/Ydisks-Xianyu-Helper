import { readdirSync,readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe,expect,test } from 'vitest';

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
  test('dark theme keeps large background surfaces pure gray and leaves the brand blue untouched', /* 测试回调解析深色令牌块并断言纯灰背景与未改动的品牌蓝。 */ () => {
    const globalStyles = readFileSync(resolve(__dirname, 'index.css'), 'utf8'); /* globalStyles 是全局样式原文。 */
    // darkBlock 是 data-theme="dark" 令牌覆盖块的内容。
    const darkBlock = globalStyles.match(/:root\[data-theme="dark"\] \{([\s\S]*?)\n\}/)?.[1] ?? '';
    expect(darkBlock).not.toBe('');

    // pureGrayTokens 是深色下的大面积背景令牌，三个通道必须相等，避免出现色彩倾向。
    const pureGrayTokens = ['white', 'canvas', 'surface', 'surface-subtle', 'surface-muted', 'neutral-50', 'neutral-100', 'slate-50', 'slate-100'];
    for (const token /* token 是待检查的深色背景令牌名。 */ of pureGrayTokens) {
      // match 是该令牌在深色块中的 RGB 声明。
      const match = darkBlock.match(new RegExp(`--color-${token}: (\\d+) (\\d+) (\\d+);`));
      expect(match, token).not.toBeNull();
      expect(match![1] === match![2] && match![2] === match![3], `${token} 必须是纯灰`).toBe(true);
    }

    // 主题蓝不得在深色块中被覆盖。
    for (const token /* token 是不得被深色块覆盖的品牌蓝令牌名。 */ of ['brand', 'brand-highlight', 'brand-light', 'brand-500', 'brand-600', 'brand-700']) {
      expect(darkBlock, token).not.toContain(`--color-${token}:`);
    }
  });
} /* 测试套件回调汇总页面颜色令牌的静态契约。 */);
