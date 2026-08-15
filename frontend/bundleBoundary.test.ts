import { readdirSync, readFileSync, statSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, test } from 'vitest';

// staticRoot 是 Go 服务嵌入的前端生产资源目录。
const staticRoot = resolve(__dirname, '../internal/webui/static');
// indexHtml 是生产构建生成的入口 HTML。
const indexHtml = readFileSync(resolve(staticRoot, 'index.html'), 'utf8');

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

  test('九个业务页面都生成独立页面 chunk', /* 当前回调验证各页面可按路由延迟下载。 */ () => {
    // pageChunkNames 保存 Vite 输出的业务页面 chunk 文件名。
    const pageChunkNames = readdirSync(resolve(staticRoot, 'assets')).filter(/* 当前回调筛选业务页面分片文件。 */ fileName => /^(Dashboard|AccountList|OrderList|CardList|ItemList|Settings|Rules|Notifications|Chat)-.+\.js$/.test(fileName));
    expect(pageChunkNames).toHaveLength(9);
  });
});
