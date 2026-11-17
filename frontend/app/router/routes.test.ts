import { afterEach, describe, expect, test } from 'vitest';
import { pathByRoute, routeByPath, routeFromLocation } from './routes';

describe('应用路由映射', /* 路由映射测试覆盖地址解析、反向映射与未知地址回退。 */ () => {
  afterEach(/* 当前回调还原被测试替换的全局 window。 */ () => {
    Reflect.deleteProperty(globalThis, 'window');
  });

  test('反向映射为每个页面返回规范地址', /* 当前回调验证 pathByRoute 与 routeByPath 互为逆映射。 */ () => {
    // path 是规范地址；route 是该地址对应的业务页面标识。
    for (const /* path 与 route 分别是规范地址及其业务页面标识。 */ [path, route] of Object.entries(routeByPath)) {
      expect(pathByRoute[route]).toBe(path);
    }
  });

  test('已知地址解析为对应业务页面', /* 当前回调验证 routeFromLocation 按 pathname 查表。 */ () => {
    Object.defineProperty(globalThis, 'window', { value: { location: { pathname: '/app/rules' } }, configurable: true });
    expect(routeFromLocation()).toBe('rules');
  });

  test('API 管理地址解析为独立业务页面', /* 当前回调验证普通登录用户可直接进入 API 令牌管理页。 */ () => {
    Object.defineProperty(globalThis, 'window', { value: { location: { pathname: '/app/api-access' } }, configurable: true });
    expect(routeFromLocation()).toBe('api-access');
  });

  test('未知地址安全回退到仪表盘', /* 当前回调验证非法地址不会产生未定义路由。 */ () => {
    Object.defineProperty(globalThis, 'window', { value: { location: { pathname: '/unknown' } }, configurable: true });
    expect(routeFromLocation()).toBe('dashboard');
  });
});
