import { existsSync,readdirSync,readFileSync } from 'node:fs';
import { dirname,relative,resolve } from 'node:path';
import { describe,expect,test } from 'vitest';

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
}) /* flatMap 回调递归汇总当前目录下的生产 TypeScript/TSX 文件。 */;

// productionSources 返回排除测试文件后的生产源码及其相对路径。
const productionSources = (): Array<{ /* relativePath 表示relative当前路径。 */ relativePath: string; /* source 表示source。 */ source: string }> => collectSourceFiles(sourceRoot)
  .filter(filePath => !filePath.endsWith('.test.ts') && !filePath.endsWith('.test.tsx') /* filePath 为待排除测试文件的源码路径。 */)
  .map(filePath => ({
    relativePath: relative(sourceRoot, filePath).split('/').join('/'),
    source: readFileSync(filePath, 'utf8'),
  }) /* map 回调读取单个生产文件并保留稳定相对路径。 */);

// extractModuleSpecifiers 提取静态 import、re-export 与动态 import 的模块路径，用于检查目录边界而不依赖编译器私有 AST。
const extractModuleSpecifiers = (source: string): string[] => {
  // modulePattern 同时匹配带 from 的导入、仅副作用导入和 import() 懒加载路径。
  const modulePattern = /(?:\b(?:import|export)\s+(?:type\s+)?(?:[\s\S]*?\s+from\s+)?|\bimport\s*\()\s*['"]([^'"]+)['"]/g;
  // specifiers 保存源码中按书写顺序找到的模块路径。
  const specifiers: string[] = [];
  // match 是当前正则匹配到的单个模块声明。
  for (const match /* match 是当前静态模块声明的完整正则匹配结果。 */ of source.matchAll(modulePattern)) {
    // specifier 是当前导入的相对或包模块路径。
    const specifier = match[1];
    if (specifier) specifiers.push(specifier);
  }
  return specifiers;
};

// resolveImportPath 将相对导入转换为前端源码根目录下的标准路径；包名导入不属于本地架构图。
const resolveImportPath = (sourcePath: string, specifier: string): string | null => {
  // isRelativeImport 表示当前模块是否使用相对路径而不是 npm 包名或 Vite 别名。
  const isRelativeImport = specifier.startsWith('.');
  if (!isRelativeImport) return null;
  // absoluteImportPath 是依据当前源码文件目录解析出的无扩展名绝对路径。
  const absoluteImportPath = resolve(dirname(resolve(sourceRoot, sourcePath)), specifier);
  // normalizedPath 是相对于源码根目录的跨平台标准路径。
  const normalizedPath = relative(sourceRoot, absoluteImportPath).split('/').join('/');
  return normalizedPath.startsWith('..') ? null : normalizedPath;
};

// featureNameFromPath 从 feature 内任意源码路径提取其直属 feature 名称。
const featureNameFromPath = (relativePath: string): string | null => {
  // featureMatch 是 app/features 下第一个目录名的匹配结果。
  const featureMatch = relativePath.match(/^app\/features\/([^/]+)\//);
  return featureMatch?.[1] || null;
};

// canonicalPageEntrypoints 是当前应用由路由壳按需加载的业务页和认证页清单。
const canonicalPageEntrypoints = [
  'app/features/accounts/pages/AccountList.tsx',
  'app/features/cards/pages/CardList.tsx',
  'app/features/chat/pages/Chat.tsx',
  'app/features/dashboard/pages/Dashboard.tsx',
  'app/features/items/pages/ItemList.tsx',
  'app/features/notifications/pages/Notifications.tsx',
  'app/features/orders/pages/OrderList.tsx',
  'app/features/rules/pages/Rules.tsx',
  'app/features/session/pages/SessionGate.tsx',
  'app/features/settings/pages/Settings.tsx',
];

describe('React feature dependency boundaries', () => {
  test('legacy centralized API entrypoint has been removed', () => {
    // legacyEntrypoint 是不应重新引入的集中业务 API 文件。
    const legacyEntrypoint = resolve(sourceRoot, 'services/api.ts');
    // exists 标识旧集中 API 文件是否仍然存在。
    const exists = (/* legacyEntrypointReader 检查旧入口是否仍保留在源码树。 */ () => {
      try {
        readFileSync(legacyEntrypoint);
        return true;
      } catch {
        return false;
      }
    })();
    expect(exists).toBe(false);
  } /* 测试回调断言旧集中式 API 入口已从源码树移除。 */);

  test('network clients are only imported by feature API adapters', () => {
    // violations 保存页面、组件或非 API feature 文件中的共享网络客户端依赖。
    const violations = productionSources()
      .filter(file => file.source.includes("from '../../../shared/http/client'") || file.source.includes("from '../../../../shared/http/client'") /* file 是当前待检查的生产模块。 */)
      .filter(file => !/(?:\/api|Api)\.ts$/.test(file.relativePath) /* 仅保留不属于特性 API 适配器的模块。 */)
      .map(file => file.relativePath /* 输出违规模块的相对路径以便定位。 */);

    expect(violations).toEqual([]);
  } /* 测试回调断言共享 HTTP 客户端只由特性 API 适配器导入。 */);

  test('production React code does not call fetch or axios outside the request boundary', () => {
    // violations 保存页面或组件中直接发起网络请求的源码路径。
    const violations = productionSources()
      .filter(file => file.relativePath !== 'shared/http/client.ts' && /\bfetch\s*\(|\baxios\b/.test(file.source) /* file 是当前待审计直接网络调用的生产模块。 */)
      .map(file => file.relativePath /* 输出越过 HTTP 客户端的模块路径。 */);

    expect(violations).toEqual([]);
  } /* 测试回调断言生产组件不会绕过共享 HTTP 边界。 */);

  test('production dynamic imports are limited to route-level page loading', () => {
    // violations 保存业务模块内部通过动态导入隐藏依赖或绕过路由边界的源码路径。
    const violations = productionSources()
      .filter(file => /\bimport\s*\(/.test(file.source) /* 回调检查当前文件是否包含动态模块导入。 */)
      .filter(file => file.relativePath !== 'app/shell/AuthenticatedShell.tsx' /* 路由壳允许按页面拆分懒加载。 */)
      .map(file => file.relativePath /* 将违规文件转换为稳定的断言值。 */);

    expect(violations).toEqual([]);
  } /* 测试回调断言动态导入仅用于路由级页面懒加载。 */);

  test('feature API adapters are the only feature files allowed to depend on shared HTTP', () => {
    // violations 保存 feature 内部绕过 API 适配层的共享服务依赖。
    const violations = productionSources()
      .filter(file => file.relativePath.startsWith('app/features/') && file.source.includes("shared/http/client") /* file 是使用共享 HTTP 客户端的特性模块。 */)
      .filter(file => !/(?:\/api|Api)\.ts$/.test(file.relativePath) /* 仅保留越过 API 适配层的模块。 */)
      .map(file => file.relativePath /* 输出违规特性模块的路径。 */);

    expect(violations).toEqual([]);
  } /* 测试回调断言特性内部网络访问经过 API 适配层。 */);

  test('production API adapters use versioned HTTP paths', () => {
    // violations 保存 API 适配层中仍然指向未版本化前缀的生产源码路径。
    const violations = productionSources()
      .filter(file => /(?:\/api|Api)\.ts$/.test(file.relativePath) /* file 是待检查路径前缀的 API 适配器。 */)
      .filter(file => /['"`]\/api\/(?!v1(?:\/|['"`]))/.test(file.source) /* 保留仍请求未版本化 API 的适配器。 */)
      .map(file => file.relativePath /* 输出使用旧 API 路径的适配器路径。 */);

    expect(violations).toEqual([]);
  } /* 测试回调断言生产 API 适配器统一使用 v1 路径。 */);

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
    } /* filter 回调探测每个历史状态入口是否仍可被读取。 */)).toEqual([]);
  } /* 测试回调断言旧页面状态转发入口全部移除。 */);

  test('feature source files do not import another feature implementation', () => {
    // violations 保存 feature 源文件解析后指向其他 feature 目录的导入关系。
    const violations = productionSources().flatMap(/* featureDependencyScanner 扫描单个 feature 源文件的本地依赖方向。 */ file => {
      // owningFeature 是当前源码文件所属 feature；非 feature 文件不受本规则约束。
      const owningFeature = featureNameFromPath(file.relativePath);
      if (!owningFeature) return [];
      // importedPaths 是当前 feature 文件解析后的本地相对导入路径。
      const importedPaths = extractModuleSpecifiers(file.source)
        .map(/* importPathResolver 将单个模块路径解析为源码根目录路径。 */ specifier => resolveImportPath(file.relativePath, specifier))
        .filter(/* localImportFilter 忽略包名、别名和源码根目录外的导入。 */ (importPath): importPath is string => importPath !== null);
      return importedPaths
        .filter(/* crossFeatureFilter 仅保留明确落在另一 feature 目录的导入。 */ importPath => {
          // importedFeature 是被当前导入路径指向的直属 feature 名称。
          const importedFeature = featureNameFromPath(importPath);
          return importedFeature !== null && importedFeature !== owningFeature;
        })
        .map(/* violationFormatter 输出可定位的跨 feature 依赖。 */ importPath => `${file.relativePath} -> ${importPath}`);
    });

    expect(violations).toEqual([]);
  } /* 测试回调断言 feature 之间没有实现层依赖。 */);

  test('shared source files do not import application or feature implementations', () => {
    // violations 保存 shared 代码反向依赖 app 或 feature 的本地导入关系。
    const violations = productionSources()
      .filter(/* sharedSourceFilter 只检查 shared 目录生产源码。 */ file => file.relativePath.startsWith('shared/'))
      .flatMap(file => extractModuleSpecifiers(file.source)
        .map(/* sharedImportResolver 解析 shared 文件的本地模块路径。 */ specifier => resolveImportPath(file.relativePath, specifier))
        .filter(/* reverseDependencyFilter 保留 shared 反向依赖 app 的路径。 */ (importPath): importPath is string => importPath?.startsWith('app/') === true)
        .map(/* reverseViolationFormatter 输出可定位的反向依赖。 */ importPath => `${file.relativePath} -> ${importPath}`));

    expect(violations).toEqual([]);
  } /* 测试回调断言 shared 不会反向依赖应用或特性实现。 */);

  test('application root only composes global boundaries and no legacy root components remain', () => {
    // appRootEntries 是 app 目录允许存在的顶层架构职责目录。
    const appRootEntries = ['errors', 'features', 'providers', 'router', 'shell'];
    // actualAppEntries 是当前 app 目录的顶层文件与目录名称。
    const actualAppEntries = readdirSync(resolve(sourceRoot, 'app')).sort();
    // appSource 是应用根组件源码，只允许装配错误边界、Provider 和路由。
    const appSource = readFileSync(resolve(sourceRoot, 'App.tsx'), 'utf8');

    expect(actualAppEntries).toEqual(appRootEntries);
    expect(appSource).toContain("from './app/errors/AppErrorBoundary'");
    expect(appSource).toContain("from './app/providers/SessionProvider'");
    expect(appSource).toContain("from './app/router/AppRouter'");
    expect(appSource).not.toMatch(/app\/features|services\/|shared\/http/);
    expect(existsSync(resolve(sourceRoot, 'components'))).toBe(false);
  } /* 测试回调断言应用根目录只保留全局装配职责。 */);

  test('all canonical page entrypoints live under their owning feature pages directory', () => {
    // actualPageEntrypoints 是全部位于 feature pages 目录的生产页面入口；认证页可使用具名导出，其余页面由路由壳懒加载默认导出。
    const actualPageEntrypoints = productionSources()
      .filter(/* pageSourceFilter 只保留 feature 的 pages 目录 TSX 文件。 */ file => /^app\/features\/[^/]+\/pages\/[^/]+\.tsx$/.test(file.relativePath))
      .map(/* pagePathMapper 提取页面入口相对路径。 */ file => file.relativePath)
      .sort();

    expect(actualPageEntrypoints).toEqual([...canonicalPageEntrypoints].sort());
  } /* 测试回调断言页面入口归属各自 feature 的 pages 目录。 */);
} /* 测试套件回调汇总前端依赖方向与目录边界门禁。 */);
