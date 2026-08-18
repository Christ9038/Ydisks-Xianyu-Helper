import { expect,test } from 'vitest';
import { amapPOIToPublishLocation } from './amapLocation';

test('maps AMap POI fields to the publish location shape', () => {
  expect(amapPOIToPublishLocation({
    id: 'B000A74398',
    name: '人民广场',
    address: '人民大道',
    adname: '黄浦区',
    cityname: '上海市',
    adcode: '310101',
    pname: '上海市',
    location: { lng: 121.4737, lat: 31.2304 },
  })).toEqual({
    area: '黄浦区',
    city: '上海市',
    division_id: '310101',
    longitude: 121.4737,
    latitude: 31.2304,
    poi_id: 'B000A74398',
    poi_name: '人民广场',
    province: '上海市',
  });
} /* 测试回调断言高德 POI 字段能够转换为发布地点 DTO。 */);

test('drops incomplete or invalid AMap POIs', () => {
  expect(amapPOIToPublishLocation({
    id: 'poi',
    name: '无坐标',
    adcode: '310101',
    pname: '上海市',
    cityname: '上海市',
    location: { lng: 0, lat: 31.2 },
  })).toBeNull();
} /* 测试回调断言缺少或非法坐标的 POI 会被丢弃。 */);
