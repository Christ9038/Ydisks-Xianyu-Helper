import { readdirSync,readFileSync,statSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe,expect,test } from 'vitest';

// staticRoot 是 Go 服务嵌入的前端生产资源目录。
const staticRoot = resolve(__dirname, '../internal/webui/static');
// indexHtml 是生产构建生成的入口 HTML。
const indexHtml = readFileSync(resolve(staticRoot, 'index.html'), 'utf8');
// PAGE_CHUNK_BUDGETS 定义每个业务页面动态分片允许的原始字节上限。
const PAGE_CHUNK_BUDGETS: Record<string, number> = {
  Dashboard: 30 * 1024,
  AccountList: 70 * 1024,
  OrderList: 40 * 1024,
  // cURL 请求解析与安全导入能力随卡密页按路由加载，52 KiB 覆盖该完整交互而仍限制页面总下载量。
  // 卡密页新增库存记录弹窗（已售/未售记录、导出）与导出下载逻辑，实际约 59.3 KiB，60 KiB 预算容纳该功能并保留少量余量。
  CardList: 60 * 1024,
  // ItemList 页面新增列表视图与规则筛选，实际约 65.05 KiB，66 KiB 预算容纳该功能并保留少量余量。
  ItemList: 66 * 1024,
  Settings: 30 * 1024,
  // Rules 页面包含模板发货模式入口、模板变量编辑器、规则筛选和商品首次回复面板，实际约 80 KiB，81 KiB 预算保留少量余量。
  Rules: 81 * 1024,
  // DeliveryTemplates 页面承载模板 CRUD 编辑器。
  DeliveryTemplates: 12 * 1024,
  Notifications: 45 * 1024,
  Chat: 50 * 1024,
  // RuntimeLogs 页面承载运行日志过滤与日志流展示。
  RuntimeLogs: 20 * 1024,
};

describe('frontend production bundle boundary', /* 当前回调验证生产入口包体和页面分片。 */ () => {
  test('入口脚本保持轻量且业务页面不被预加载', /* 当前回调验证首屏入口不同步载入所有页面。 */ () => {
    // entryMatch 保存入口脚本的静态资源匹配结果。
    const entryMatch = indexHtml.match(/src="\/static\/assets\/(index-[^"]+\.js)"/);
    expect(entryMatch).not.toBeNull();
    // entryBytes 保存入口 JavaScript 的原始字节数。
    const entryBytes = statSync(resolve(staticRoot, 'assets', entryMatch![1])).size;
    expect(entryBytes).toBeLessThan(100 * 1024);
    // preloadedAssets 保存 HTML 声明的模块预加载资源。
    const preloadedAssets = [...indexHtml.matchAll(/modulepreload[^>]+href="\/static\/assets\/([^"]+)"/g)].map(/* 当前回调提取模块预加载文件名。 */ match => match[1]);
    expect(preloadedAssets.some(/* 当前回调判断 React 运行时是否被预加载。 */ asset => asset.startsWith('react-vendor-'))).toBe(true);
    expect(preloadedAssets.some(/* 当前回调判断图表依赖是否被首屏预加载。 */ asset => asset.startsWith('charts-vendor-'))).toBe(false);
  });

  test('十一个业务页面都生成独立页面 chunk', /* 当前回调验证各页面可按路由延迟下载。 */ () => {
    // pageChunkNames 保存 Vite 输出的业务页面 chunk 文件名。
    const pageChunkNames = readdirSync(resolve(staticRoot, 'assets')).filter(/* 当前回调筛选业务页面分片文件。 */ fileName => /^(Dashboard|AccountList|OrderList|CardList|ItemList|Settings|Rules|Notifications|Chat|DeliveryTemplates|RuntimeLogs)-.+\.js$/.test(fileName));
    expect(pageChunkNames).toHaveLength(11);
  });

  test('每个业务页面 chunk 都保持在独立预算内', /* 当前回调验证单个页面不会重新膨胀首屏后的按需下载。 */ () => {
    // assetNames 保存当前生产构建输出的所有静态资源文件名。
    const assetNames = readdirSync(resolve(staticRoot, 'assets'));
    for (const budget /* budget 表示一个页面分片的预算条目。 */ of Object.entries(PAGE_CHUNK_BUDGETS)) {
      // pageName 表示预算对应的页面名称。
      const pageName = budget[0];
      // maxBytes 表示该页面分片允许的最大原始字节数。
      const maxBytes = budget[1];
      // pageChunkNames 保存该页面匹配到的生产分片文件。
      const pageChunkNames = assetNames.filter(/* 当前回调筛选当前页面的动态分片。 */ fileName => fileName.startsWith(`${pageName}-`) && fileName.endsWith('.js'));
      expect(pageChunkNames, `${pageName} 应只有一个动态分片`).toHaveLength(1);
      // actualBytes 保存当前页面分片的实际原始字节数。
      const actualBytes = statSync(resolve(staticRoot, 'assets', pageChunkNames[0])).size;
      expect(actualBytes, `${pageName} 分片超过 ${maxBytes} 字节预算`).toBeLessThanOrEqual(maxBytes);
    }
  });
});
