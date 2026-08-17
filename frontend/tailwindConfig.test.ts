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
