// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { getPublishLocations, amapPOIToPublishLocation } from './amapLocation';

// placeSearchFactory 创建不访问高德网络的地点搜索替身。
const placeSearchFactory = (status: string, pois: unknown[] = []) => vi.fn(function placeSearchConstructor() {
  return {
    // searchNearBy 根据测试指定的状态回调结果。
    searchNearBy: createSearchNearByStub(status, pois),
  };
});

// createSearchNearByStub 创建向业务代码返回可控结果的地点搜索方法。
function createSearchNearByStub(status: string, pois: unknown[]): (_keyword: string, _center: [number, number], _radius: number, callback: (status: string, result: unknown) => void) => void {
  // stub 是保存测试状态和地点列表的搜索方法对象。
  const stub = new SearchNearByStub(status, pois);
  return stub.searchNearBy.bind(stub);
}

// SearchNearByStub 保存测试状态并实现高德地点搜索方法。
class SearchNearByStub {
  // status 是高德搜索回调状态。
  private readonly status: string;

  // pois 是高德搜索返回的地点列表。
  private readonly pois: unknown[];

  // constructor 保存可控的搜索结果。
  constructor(status: string, pois: unknown[]) {
    this.status = status;
    this.pois = pois;
  }

  // searchNearBy 向业务代码返回测试指定的地点搜索结果。
  searchNearBy(_keyword: string, _center: [number, number], _radius: number, callback: (status: string, result: unknown) => void): void {
    callback(this.status, { poiList: { pois: this.pois } });
  }
}

describe('AMap 地点业务适配', /* 当前回调覆盖坐标校验和地点搜索结果转换。 */ () => {
  beforeEach(/* 当前回调安装可控的 AMap API 替身。 */ () => {
    // placeSearch 是高德 PlaceSearch 构造器的浏览器替身。
    const placeSearch = placeSearchFactory('complete', [{ id: 'poi-1', name: '地点', adname: '区域', cityname: '城市', adcode: '100', pname: '省份', location: { lng: 120, lat: 30 } }, { id: 'bad', name: '缺字段', location: { lng: 120, lat: 30 } }]);
    // AMap 是高德地图全局对象的测试替身。
    Object.defineProperty(window, 'AMap', { configurable: true, value: { PlaceSearch: placeSearch } });
  });

  afterEach(/* 当前回调清理浏览器全局状态。 */ () => {
    // AMap 是高德地图全局对象的测试替身。
    Reflect.deleteProperty(window, 'AMap');
    vi.restoreAllMocks();
  });

  test('无效坐标在触发外部 API 前直接拒绝', /* 当前回调验证坐标输入守卫。 */ async () => {
    await expect(getPublishLocations(0, 30)).rejects.toThrow('经纬度无效');
    await expect(getPublishLocations(181, 30)).rejects.toThrow('经纬度无效');
  });

  test('完成状态过滤无效 POI 并转换有效地点', /* 当前回调验证高德完整响应映射。 */ async () => {
    // locations 是地点搜索业务转换后的结果集合。
    const locations = await getPublishLocations(120, 30);
    expect(locations).toHaveLength(1);
    expect(locations[0]).toMatchObject({ division_id: '100', poi_id: 'poi-1', longitude: 120, latitude: 30 });
  });

  test('无数据状态返回空数组，失败状态返回业务错误', /* 当前回调验证地点搜索的两类非成功响应。 */ async () => {
    // placeSearch 是返回无数据状态的构造器替身。
    const placeSearch = placeSearchFactory('no_data');
    Object.defineProperty(window, 'AMap', { configurable: true, value: { PlaceSearch: placeSearch } });
    await expect(getPublishLocations(120, 30)).resolves.toEqual([]);

    // failedPlaceSearch 是返回失败状态的构造器替身。
    const failedPlaceSearch = placeSearchFactory('error');
    Object.defineProperty(window, 'AMap', { configurable: true, value: { PlaceSearch: failedPlaceSearch } });
    await expect(getPublishLocations(120, 30)).rejects.toThrow('附近地址查询失败');
  });

  test('POI 映射拒绝越界坐标和缺失行政字段', /* 当前回调验证领域地点模型的完整性守卫。 */ () => {
    expect(amapPOIToPublishLocation({ id: 'poi', name: '地点', adcode: '1', pname: '省', cityname: '市', location: { lng: 181, lat: 30 } })).toBeNull();
    expect(amapPOIToPublishLocation({ id: 'poi', name: '地点', adcode: '1', pname: '省', cityname: '市', location: { lng: 120, lat: 30 } })).not.toBeNull();
  });
});
