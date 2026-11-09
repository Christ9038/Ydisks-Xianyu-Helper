// @vitest-environment jsdom
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { previewTemplatePublish } from './api';

// post 记录合约客户端收到的 POST 调用。
const post = vi.hoisted(/* 创建可在 mock 工厂中引用的 POST 替身。 */ () => vi.fn());

vi.mock('../../../shared/api-contract/client', /* importOriginal 保留原模块，只替换请求执行。 */ async importOriginal => ({
  ...(await importOriginal<typeof import('../../../shared/api-contract/client')>()),
  contractClient: { POST: post },
  runContractRequest: /* 直接执行请求回调，不经过超时与重试。 */ async (call: (signal: AbortSignal) => Promise<unknown>) => call(new AbortController().signal),
}));

describe('previewTemplatePublish 发货地', /* 发货地必须带齐坐标与 POI。 */ () => {
  beforeEach(/* 重置请求替身。 */ () => { post.mockReset(); post.mockResolvedValue({ preview_id: 'p', total: 0, valid: 0, invalid: 0, rows: [] }); });

  test('提交完整发货地字段（含经纬度与 POI）', /* 回归：此前只发送省市区与 division_id，发布阶段报缺少坐标。 */ async () => {
    await previewTemplatePublish({ templateIds: [1], cookieIds: ['a'], intervalSeconds: 5, location: { province: '浙江省', city: '杭州市', area: '西湖区', division_id: '330106', longitude: 120.1, latitude: 30.2, poi_id: 'B1', poi_name: '某地点' } });
    expect(post.mock.calls[0][1].body.location).toEqual({ province: '浙江省', city: '杭州市', area: '西湖区', division_id: '330106', longitude: 120.1, latitude: 30.2, poi_id: 'B1', poi_name: '某地点' });
  });

  test('不选发货地时请求体不含 location', /* 缺省沿用平台默认。 */ async () => {
    await previewTemplatePublish({ templateIds: [1], cookieIds: ['a'], intervalSeconds: 5 });
    expect(post.mock.calls[0][1].body).not.toHaveProperty('location');
  });
});
