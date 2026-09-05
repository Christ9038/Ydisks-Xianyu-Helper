// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import type { AMapDistrict } from './amapLocation';
import { useManualLocation } from './manualLocation';

// province 是手动选择 Hook 使用的省级高德样本。
const province: AMapDistrict = { adcode: 'p1', name: '浙江省', districtList: [{ adcode: 'c1', name: '杭州市' }] };
// city 是手动选择 Hook 使用的城市级高德样本。
const city: AMapDistrict = { adcode: 'c1', name: '杭州市', districtList: [{ adcode: 'd1', name: '西湖区' }] };
// district 是手动选择 Hook 使用的区县级高德样本。
const district: AMapDistrict = { adcode: 'd1', name: '西湖区' };
// location 是手动选择 Hook 使用的完整高德 POI 样本。
const location = { area: '西湖区', city: '杭州市', division_id: 'd1', longitude: 120.12, latitude: 30.26, poi_id: 'poi-1', poi_name: '西湖文化广场', province: '浙江省' };

// installAmapStub 安装同步返回级联和 POI 结果的高德替身。
const installAmapStub = () => {
  // districtSearch 是行政区查询构造器替身。
  const districtSearch = vi.fn(function districtSearchConstructor() {
    return {
      // search 根据父级编码返回下级行政区。
      search: vi.fn(/* districtSearchCallbackFactory 为每次行政区测试调用返回确定性结果。 */ (keyword: string, callback: (status: string, result: unknown) => void) => {
        // result 是当前父级编码对应的高德行政区响应样本。
        const result = keyword === '中国'
          ? { districtList: [{ adcode: '100000', districtList: [province] }] }
          : keyword === 'p1'
            ? { districtList: [{ ...province, districtList: [city] }] }
            : { districtList: [{ ...city, districtList: [district] }] };
        callback('complete', result);
      }),
    };
  });
  // placeSearch 是区县内 POI 搜索构造器替身。
  const placeSearch = vi.fn(function placeSearchConstructor() {
    return {
      // search 返回当前区县内的完整发货地候选。
      search: vi.fn((_keyword: string, callback: (status: string, result: unknown) => void) => callback('complete', { poiList: { pois: [{ id: 'poi-1', name: '西湖文化广场', adname: '西湖区', cityname: '杭州市', adcode: 'd1', pname: '浙江省', location: { lng: 120.12, lat: 30.26 } }] } })),
      // searchNearBy 保留高德地点服务的完整接口形状。
      searchNearBy: vi.fn(),
    };
  });
  // amapStub 是浏览器全局高德对象替身。
  window.AMap = { DistrictSearch: districtSearch, PlaceSearch: placeSearch } as unknown as NonNullable<Window['AMap']>;
};

describe('useManualLocation', /* manualLocationHookDescribeCallback 验证手动地点级联和生命周期。 */ () => {
  beforeEach(/* manualLocationHookBeforeEach 安装每个用例独立的高德替身。 */ () => {
    vi.clearAllMocks();
    installAmapStub();
  });

  afterEach(/* manualLocationHookAfterEach 清理高德全局替身。 */ () => {
    Reflect.deleteProperty(window, 'AMap');
  });

  test('级联选择区县并搜索后可确认高德 POI', /* manualLocationSuccessTestCallback 验证手动地点的完整用户路径。 */ async () => {
    // hook 是手动地点状态的真实 Hook 实例。
    const hook = renderHook(/* manualLocationHookFactory 创建手动地点状态的真实 Hook。 */ () => useManualLocation(true));
    await act(/* rootDistrictFlushAction 等待省级行政区查询完成。 */ async () => Promise.resolve());
    expect(hook.result.current.provinces).toHaveLength(1);

    act(/* provinceSelectionAction 选择高德省份并触发城市查询。 */ () => hook.result.current.selectProvince(hook.result.current.provinces[0]));
    await act(/* cityDistrictFlushAction 等待城市查询完成。 */ async () => Promise.resolve());
    act(/* citySelectionAction 选择高德城市并触发区县查询。 */ () => hook.result.current.selectCity(hook.result.current.cities[0]));
    await act(/* districtFlushAction 等待区县查询完成。 */ async () => Promise.resolve());
    act(/* districtSelectionAction 选择高德区县并准备 POI 搜索。 */ () => hook.result.current.selectDistrict(hook.result.current.districts[0]));
    act(/* keywordInputAction 写入具体地点关键词。 */ () => hook.result.current.setKeyword('文化广场'));
    await act(/* poiSearchAction 等待区县内 POI 搜索完成。 */ async () => hook.result.current.searchLocations());
    expect(hook.result.current.locations).toHaveLength(1);
    act(/* poiSelectionAction 选中高德 POI。 */ () => hook.result.current.selectLocation(hook.result.current.locations[0]));
    expect(hook.result.current.selectedLocation).toEqual(location);
    hook.unmount();
  });

  test('切换上级行政区会清除旧区县和旧 POI', /* manualLocationSwitchTestCallback 验证切换选择链路不会复用旧地点。 */ async () => {
    // hook 是手动地点状态的真实 Hook 实例。
    const hook = renderHook(/* manualLocationHookFactory 创建手动地点状态的真实 Hook。 */ () => useManualLocation(true));
    await act(/* rootDistrictFlushAction 等待省级行政区查询完成。 */ async () => Promise.resolve());
    act(/* provinceSelectionAction 选择高德省份并触发城市查询。 */ () => hook.result.current.selectProvince(hook.result.current.provinces[0]));
    await act(/* cityFlushAction 等待城市查询完成。 */ async () => Promise.resolve());
    act(/* citySelectionAction 选择高德城市并触发区县查询。 */ () => hook.result.current.selectCity(hook.result.current.cities[0]));
    await act(/* districtFlushAction 等待区县查询完成。 */ async () => Promise.resolve());
    act(/* districtSelectionAction 选择高德区县。 */ () => hook.result.current.selectDistrict(hook.result.current.districts[0]));
    act(/* keywordInputAction 写入旧区县的搜索词。 */ () => hook.result.current.setKeyword('旧地点'));
    await act(/* poiSearchAction 等待旧区县 POI 搜索完成。 */ async () => hook.result.current.searchLocations());
    act(/* provinceSwitchAction 重新选择省份并使旧城市、区县和 POI 失效。 */ () => hook.result.current.selectProvince(hook.result.current.provinces[0]));
    expect(hook.result.current.selectedCity).toBeNull();
    expect(hook.result.current.selectedDistrict).toBeNull();
    expect(hook.result.current.locations).toEqual([]);
    expect(hook.result.current.selectedLocation).toBeNull();
    hook.unmount();
  });
});
