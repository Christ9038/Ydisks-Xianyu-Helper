import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, test } from 'vitest';

// tailwindConfigPath 指向生产构建读取的 Tailwind 配置，测试以文本方式审计其静态扫描范围。
const tailwindConfigPath = resolve(__dirname, 'tailwind.config.js');

/** verifyTailwindFeatureSourceGlobs 验证阶段七迁移后的 app/shared 源码不会被 Tailwind 生产构建遗漏。 */
function verifyTailwindFeatureSourceGlobs(): void {
  // configSource 是当前 Tailwind 配置原文，避免 TypeScript 为 JavaScript 配置要求声明文件。
  const configSource = readFileSync(tailwindConfigPath, 'utf8');

  expect(configSource).toContain('"./app/**/*.{js,ts,jsx,tsx}"');
  expect(configSource).toContain('"./shared/**/*.{js,ts,jsx,tsx}"');
}

test('Tailwind scans feature and shared UI source trees', verifyTailwindFeatureSourceGlobs);

/** verifyVioletFollowsAccentTokens 验证 violet 色阶指向 accent 设计令牌，且深色模式为 accent 提供了深紫底，避免紫色卡片在深色下退回 Tailwind 默认的近白淡紫。 */
function verifyVioletFollowsAccentTokens(): void {
  // configSource 是当前 Tailwind 配置原文。
  const configSource = readFileSync(tailwindConfigPath, 'utf8');
  // globalStyles 是全局样式原文，深色令牌覆盖块位于其中。
  const globalStyles = readFileSync(resolve(__dirname, 'index.css'), 'utf8');
  // darkBlock 是 data-theme="dark" 令牌覆盖块的内容。
  const darkBlock = globalStyles.match(/:root\[data-theme="dark"\] \{([\s\S]*?)\n\}/)?.[1] ?? '';

  expect(configSource).toContain("violet: palette('accent')");
  expect(darkBlock).toContain('--color-accent-50:');
  expect(darkBlock).toContain('--color-accent-100:');
  expect(darkBlock).toContain('--color-accent-700:');
}

test('violet shares the accent tokens so purple cards follow the dark theme', verifyVioletFollowsAccentTokens);
