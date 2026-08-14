import { readdirSync, readFileSync } from 'node:fs';
import { resolve, relative } from 'node:path';
import { describe, expect, test } from 'vitest';

// sourceRoot 是前端源码根目录，所有架构规则都基于生产源码扫描。
const sourceRoot = resolve(__dirname);

// collectSourceFiles 递归收集指定目录下的 TypeScript/TSX 源文件。
const collectSourceFiles = (directory: string): string[] => readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
  // entry 是当前目录中的文件或子目录条目。
  // filePath 是当前目录项的绝对路径。
  const filePath = resolve(directory, entry.name);
  if (entry.isDirectory()) {
    if (['node_modules', 'dist', 'coverage'].includes(entry.name)) return [];
    return collectSourceFiles(filePath);
  }
  return /\.(ts|tsx)$/.test(entry.name) ? [filePath] : [];
}) /* 回调函数负责当前业务流程。 */;

// productionSources 返回排除测试文件后的生产源码及其相对路径。
const productionSources = (): Array<{ /* relativePath 表示relative当前路径。 */ relativePath: string; /* source 表示source。 */ source: string }> => collectSourceFiles(sourceRoot)
  .filter(filePath => !filePath.endsWith('.test.ts') && !filePath.endsWith('.test.tsx') /* 回调函数负责当前业务流程。 */)
  .map(filePath => ({
    relativePath: relative(sourceRoot, filePath).split('/').join('/'),
    source: readFileSync(filePath, 'utf8'),
  }) /* 回调函数负责当前业务流程。 */);

describe('React feature dependency boundaries', () => {
  test('network clients are only imported by feature API adapters', () => {
    // violations 保存页面、组件或非 API feature 文件中的共享网络客户端依赖。
    const violations = productionSources()
      .filter(file => file.source.includes("from './services/api'") || file.source.includes("from '../services/api'") || file.source.includes("from '../../../services/api'") /* 回调函数负责当前业务流程。 */)
      .filter(file => file.relativePath !== 'services/api.ts' && !file.relativePath.endsWith('/api.ts') /* 回调函数负责当前业务流程。 */)
      .map(file => file.relativePath /* 回调函数负责当前业务流程。 */);

    expect(violations).toEqual([]);
  } /* 回调函数负责当前业务流程。 */);

  test('production React code does not call fetch or axios outside the request boundary', () => {
    // violations 保存页面或组件中直接发起网络请求的源码路径。
    const violations = productionSources()
      .filter(file => file.relativePath !== 'request.ts' && /\bfetch\s*\(|\baxios\b/.test(file.source) /* 回调函数负责当前业务流程。 */)
      .map(file => file.relativePath /* 回调函数负责当前业务流程。 */);

    expect(violations).toEqual([]);
  } /* 回调函数负责当前业务流程。 */);

  test('feature API adapters are the only feature files allowed to depend on shared API', () => {
    // violations 保存 feature 内部绕过 API 适配层的共享服务依赖。
    const violations = productionSources()
      .filter(file => file.relativePath.startsWith('app/features/') && file.source.includes("from '../../../services/api'") /* 回调函数负责当前业务流程。 */)
      .filter(file => !file.relativePath.endsWith('/api.ts') /* 回调函数负责当前业务流程。 */)
      .map(file => file.relativePath /* 回调函数负责当前业务流程。 */);

    expect(violations).toEqual([]);
  } /* 回调函数负责当前业务流程。 */);

  test('production API adapters use versioned HTTP paths', () => {
    // violations 保存 API 适配层中仍然指向未版本化前缀的生产源码路径。
    const violations = productionSources()
      .filter(file => file.relativePath === 'services/api.ts' || file.relativePath.endsWith('/api.ts') /* 回调函数负责当前业务流程。 */)
      .filter(file => /['"`]\/api\/(?!v1(?:\/|['"`]))/.test(file.source) /* 回调函数负责当前业务流程。 */)
      .map(file => file.relativePath /* 回调函数负责当前业务流程。 */);

    expect(violations).toEqual([]);
  } /* 回调函数负责当前业务流程。 */);

  test('legacy component state compatibility entrypoints have been removed', () => {
    // legacyEntrypoints 是不应重新引入页面层状态转发文件的旧路径。
    const legacyEntrypoints = [
      'components/accountEdit.ts',
      'components/accountPause.ts',
      'components/accountRuntimeState.ts',
      'components/cardListState.ts',
      'components/itemPublishBatchState.ts',
      'components/orderImportState.ts',
      'components/automationIssueState.ts',
    ];

    expect(legacyEntrypoints.filter(entry => {
      try {
        readFileSync(resolve(sourceRoot, entry));
        return true;
      } catch {
        return false;
      }
    } /* 回调函数负责当前业务流程。 */)).toEqual([]);
  } /* 回调函数负责当前业务流程。 */);
} /* 回调函数负责当前业务流程。 */);
