// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';

// SCRIPT_ID 是高德脚本加载器使用的固定 DOM 标识。
const SCRIPT_ID = 'ydisks-amap-js-api';

// createAmapStub 创建可完成地点查询的高德 API 替身。
function createAmapStub() {
  // placeSearch 是地点查询构造器替身。
  const placeSearch = vi.fn(function placeSearchConstructor() {
    return {
      // searchNearBy 返回一个可映射的地点结果。
      searchNearBy: /* searchCallback 返回一个可映射的地点结果。 */ (_keyword: string, _center: [number, number], _radius: number, callback: (status: string, result: unknown) => void) => callback('complete', {
        poiList: {
          pois: [{ id: 'poi-1', name: '地点', adname: '区域', cityname: '城市', adcode: '100', pname: '省份', location: { lng: 120, lat: 30 } }],
        },
      }),
    };
  });
  // amapStub 是浏览器全局高德 API 替身。
  return { PlaceSearch: placeSearch };
}

describe('AMap 脚本加载边界', /* describeCallback 组织高德脚本加载测试。 */ () => {
  beforeEach(/* beforeEachCallback 清理高德脚本全局状态。 */ () => {
    // script 是上一用例遗留的高德脚本节点。
    document.getElementById(SCRIPT_ID)?.remove();
    // AMap 是浏览器全局高德对象。
    Reflect.deleteProperty(window, 'AMap');
    // loadedCallback 是上一用例遗留的脚本回调。
    Reflect.deleteProperty(window, '__ydisksAmapLoaded');
  });

  afterEach(/* afterEachCallback 恢复高德脚本测试环境。 */ () => {
    vi.useRealTimers();
    document.getElementById(SCRIPT_ID)?.remove();
    Reflect.deleteProperty(window, 'AMap');
    Reflect.deleteProperty(window, '__ydisksAmapLoaded');
  });

  test('脚本回调完成后解析高德对象', /* successCallback 验证脚本成功加载。 */ async () => {
    // amapModule 是每个用例独立加载的业务模块实例。
    vi.resetModules();
    const amapModule = await import('./amapLocation');
    // pending 是等待脚本回调完成的地点查询。
    const pending = amapModule.getPublishLocations(120, 30);
    // script 是业务代码插入的高德脚本节点。
    const script = document.getElementById(SCRIPT_ID) as HTMLScriptElement;
    expect(script.src).toContain('webapi.amap.com/maps');
    // amapStub 是脚本加载完成后暴露的高德对象。
    window.AMap = createAmapStub() as NonNullable<Window['AMap']>;
    window.__ydisksAmapLoaded?.();
    await expect(pending).resolves.toMatchObject([{ poi_id: 'poi-1' }]);
  });

  test('已有脚本节点触发错误时返回加载失败', /* errorCallback 验证脚本错误加载。 */ async () => {
    // existingScript 是预先插入页面的高德脚本节点。
    const existingScript = document.createElement('script');
    existingScript.id = SCRIPT_ID;
    document.head.appendChild(existingScript);
    vi.resetModules();
    const amapModule = await import('./amapLocation');
    // pending 是等待脚本错误回调的地点查询。
    const pending = amapModule.getPublishLocations(120, 30);
    existingScript.onerror?.(new Event('error'));
    await expect(pending).rejects.toThrow('高德地图 API 加载失败');
  });

  test('脚本超时后返回网络配置错误', /* timeoutCallback 验证脚本超时加载。 */ async () => {
    vi.useFakeTimers();
    vi.resetModules();
    const amapModule = await import('./amapLocation');
    // pending 是等待超时结果的地点查询。
    const pending = amapModule.getPublishLocations(120, 30);
    // rejection 是等待脚本超时错误的断言。
    const rejection = expect(pending).rejects.toThrow('高德地图 API 加载超时');
    await vi.advanceTimersByTimeAsync(15_000);
    await rejection;
  });

  test('脚本回调完成但未暴露对象时返回对象缺失错误', /* missingApiCallback 验证脚本对象缺失。 */ async () => {
    vi.resetModules();
    const amapModule = await import('./amapLocation');
    // pending 是等待无对象回调结果的地点查询。
    const pending = amapModule.getPublishLocations(120, 30);
    window.__ydisksAmapLoaded?.();
    await expect(pending).rejects.toThrow('未找到 AMap 对象');
  });
});
