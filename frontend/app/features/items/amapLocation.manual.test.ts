// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { getPublishDistricts, searchPublishLocationsByKeyword } from './amapLocation';

// province 是高德省级行政区测试样本。
const province = { adcode: 'p1', name: '浙江省', level: 'province' };
// city 是高德城市级行政区测试样本。
const city = { adcode: 'c1', name: '杭州市', level: 'city' };
// district 是高德区县级行政区测试样本。
const district = { adcode: 'd1', name: '西湖区', level: 'district' };
// poi 是高德返回的完整地点测试样本。
const poi = { id: 'poi-1', name: '西湖文化广场', adname: '西湖区', cityname: '杭州市', adcode: 'd1', pname: '浙江省', location: { lng: 120.12, lat: 30.26 } };

// installAmapStub 安装可控的高德行政区和 POI 查询替身。
const installAmapStub = () => {
  // districtSearch 保存对行政区查询构造器的调用记录。
  const districtSearch = vi.fn(function districtSearchConstructor() {
    return {
      // search 根据父级编码返回对应的直接下级行政区。
      search: vi.fn(/* districtSearchCallbackFactory 为每次行政区测试调用返回确定性结果。 */ (keyword: string, callback: (status: string, result: unknown) => void) => {
        // result 是当前父级编码对应的高德行政区响应样本。
        const result = keyword === '中国'
          ? { districtList: [{ adcode: '100000', name: '中华人民共和国', districtList: [province] }] }
          : keyword === 'p1'
            ? { districtList: [{ ...province, districtList: [city] }] }
            : { districtList: [{ ...city, districtList: [district] }] };
        callback('complete', result);
      }),
    };
  });
  // placeSearch 保存对 POI 搜索构造器的调用记录。
  const placeSearch = vi.fn(function placeSearchConstructor() {
    return {
      // search 返回指定区县内的高德地点样本。
      search: vi.fn((_keyword: string, callback: (status: string, result: unknown) => void) => callback('complete', { poiList: { pois: [poi] } })),
      // searchNearBy 保留现有自动定位测试需要的插件接口。
      searchNearBy: vi.fn(),
    };
  });
  // amapStub 是供高德加载器直接读取的浏览器全局替身。
  window.AMap = { DistrictSearch: districtSearch, PlaceSearch: placeSearch } as unknown as NonNullable<Window['AMap']>;
  return { districtSearch, placeSearch };
};

describe('高德手动发货地查询', /* manualLocationDescribeCallback 组织省市区和 POI 查询边界。 */ () => {
  beforeEach(/* manualLocationBeforeEach 清理高德模块状态和浏览器全局。 */ () => {
    vi.clearAllMocks();
    Reflect.deleteProperty(window, 'AMap');
  });

  afterEach(/* manualLocationAfterEach 删除高德测试替身，避免污染其他用例。 */ () => {
    Reflect.deleteProperty(window, 'AMap');
  });

  test('从高德逐级加载省市区，并按区县搜索 POI', /* cascadeAndSearchTestCallback 验证手动地点的完整数据链路。 */ async () => {
    // amapStub 是当前用例使用的高德接口替身及调用记录。
    const amapStub = installAmapStub();
    // provinces 是高德根查询返回的省级选项。
    const provinces = await getPublishDistricts();
    // cities 是高德省级查询返回的城市选项。
    const cities = await getPublishDistricts('p1');
    // districts 是高德城市查询返回的区县选项。
    const districts = await getPublishDistricts('c1');
    // locations 是高德按区县限制返回的 POI 选项。
    const locations = await searchPublishLocationsByKeyword('文化广场', 'd1');
    expect(provinces).toEqual([{ ...province }]);
    expect(cities).toEqual([{ ...city }]);
    expect(districts).toEqual([{ ...district }]);
    expect(locations).toEqual([expect.objectContaining({ division_id: 'd1', poi_id: 'poi-1', poi_name: '西湖文化广场' })]);
    expect(amapStub.placeSearch).toHaveBeenCalledWith({ city: 'd1', citylimit: true, extensions: 'all', pageSize: 10 });
  });

  test('空关键词和空区县会在请求前被拒绝', /* invalidManualSearchTestCallback 验证手动搜索的输入边界。 */ async () => {
    // amapStub 是确保测试未绕过高德加载边界的接口替身。
    const amapStub = installAmapStub();
    await expect(searchPublishLocationsByKeyword(' ', 'd1')).rejects.toThrow('请输入具体位置');
    await expect(searchPublishLocationsByKeyword('地点', ' ')).rejects.toThrow('请先选择区县');
    expect(amapStub.placeSearch).not.toHaveBeenCalled();
  });

  test('高德行政区查询失败会返回明确错误', /* districtFailureTestCallback 验证级联数据源异常不会伪造前端选项。 */ async () => {
    // districtSearch 是返回失败状态的行政区查询构造器替身。
    const districtSearch = vi.fn(function districtFailureConstructor() {
      return { search: vi.fn(/* districtFailureCallback 交付高德失败状态。 */ (_keyword: string, callback: (status: string, result: unknown) => void) => callback('error', {})) };
    });
    // placeSearch 是满足高德对象完整形状的地点查询替身。
    const placeSearch = vi.fn(function placeSearchConstructor() {
      return { search: vi.fn(), searchNearBy: vi.fn() };
    });
    // amapStub 是当前失败用例使用的高德全局替身。
    window.AMap = { DistrictSearch: districtSearch, PlaceSearch: placeSearch } as unknown as NonNullable<Window['AMap']>;
    await expect(getPublishDistricts()).rejects.toThrow('高德行政区查询失败');
  });

  test('取消行政区查询后忽略高德晚到回调', /* districtCancellationTestCallback 验证取消不会让旧行政区结果重新生效。 */ async () => {
    // lateCallback 保存高德稍后才会调用的行政区回调。
    let lateCallback: ((status: string, result: unknown) => void) | undefined;
    // districtSearch 是不会立即完成的行政区查询构造器替身。
    const districtSearch = vi.fn(function delayedDistrictConstructor() {
      return { search: vi.fn(/* delayedDistrictCallback 保存晚到高德行政区回调。 */ (_keyword: string, callback: (status: string, result: unknown) => void) => { lateCallback = callback; }) };
    });
    // placeSearch 是满足高德对象完整形状的地点查询替身。
    const placeSearch = vi.fn(function placeSearchConstructor() {
      return { search: vi.fn(), searchNearBy: vi.fn() };
    });
    // amapStub 是当前取消用例使用的高德全局替身。
    window.AMap = { DistrictSearch: districtSearch, PlaceSearch: placeSearch } as unknown as NonNullable<Window['AMap']>;
    // controller 是本次行政区请求的外部取消器。
    const controller = new AbortController();
    // pending 是等待高德回调的行政区请求。
    const pending = getPublishDistricts(undefined, { signal: controller.signal });
    controller.abort();
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
    lateCallback?.('complete', { districtList: [] });
  });
});
