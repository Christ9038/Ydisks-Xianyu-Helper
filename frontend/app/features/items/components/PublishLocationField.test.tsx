// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import * as api from '../api';
import type { PublishLocation } from '../models';
import { PublishLocationField } from './PublishLocationField';

vi.mock('../api', /* importOriginal 保留原模块，只替换地点查询。 */ async importOriginal => ({ ...(await importOriginal<typeof import('../api')>()), getPublishLocations: vi.fn() }));
vi.mock('./ManualLocationPicker', /* factory 提供手动选点弹窗替身，避免请求高德。 */ () => ({ ManualLocationPicker: /* 替身弹窗在打开时渲染确认按钮。 */ ({ open, onConfirm }: { /** open 是弹窗开关。 */ open: boolean; /** onConfirm 回传手动选中的地点。 */ onConfirm: (location: PublishLocation) => void }) => open ? <button type="button" onClick={/* 确认手动地点。 */ () => onConfirm(manual)}>确认手动地点</button> : null }));

// near、manual 是附近候选与手动选中的完整发货地（含坐标与 POI）。
const near: PublishLocation = { province: '浙江省', city: '杭州市', area: '西湖区', division_id: '330106', longitude: 120.1, latitude: 30.2, poi_id: 'B1', poi_name: '地点A' };
// nearSecond 是附近第二个候选发货地，用于验证改选。
const nearSecond: PublishLocation = { ...near, poi_id: 'B2', poi_name: '地点B' };
const manual: PublishLocation = { ...near, poi_id: 'B3', poi_name: '手动地点' };

/** stubGeolocation 安装浏览器定位替身，success 为真时返回固定坐标，否则返回指定错误码。 */
const stubGeolocation = (success: boolean, code = 1): void => {
  Object.defineProperty(navigator, 'geolocation', { configurable: true, value: { getCurrentPosition: /* 按 success 立即回调成功或失败。 */ (ok: PositionCallback, fail: PositionErrorCallback) => success ? ok({ coords: { longitude: 120.1, latitude: 30.2 } } as GeolocationPosition) : fail({ code, PERMISSION_DENIED: 1 } as GeolocationPositionError) } });
};

describe('PublishLocationField', /* 定位、手动选点、候选改选与失败。 */ () => {
  beforeEach(/* 重置请求替身。 */ () => vi.clearAllMocks());
  afterEach(/* 卸载渲染树。 */ () => cleanup());

  test('定位成功默认选中第一项并可改选；手动选点回传完整地点；可清除', /* 成功路径。 */ async () => {
    stubGeolocation(true);
    vi.mocked(api.getPublishLocations).mockResolvedValue([near, nearSecond]);
    // onChange 记录回传的发货地。
    const onChange = vi.fn();
    render(<PublishLocationField value={null} onChange={onChange} />);
    fireEvent.click(screen.getByText('获取当前位置'));
    await waitFor(/* 等待默认选中第一项。 */ () => expect(onChange).toHaveBeenCalledWith(near));
    fireEvent.change(screen.getByLabelText('选择发货地'), { target: { value: '1' } });
    expect(onChange).toHaveBeenLastCalledWith(nearSecond);
    fireEvent.click(screen.getByText('手动选择'));
    fireEvent.click(screen.getByText('确认手动地点'));
    expect(onChange).toHaveBeenLastCalledWith(manual);
  });

  test('已选值展示并可清除', /* 清除回传空值。 */ () => {
    // onChange 记录回传。
    const onChange = vi.fn();
    render(<PublishLocationField value={near} onChange={onChange} />);
    expect(screen.getByText(/已选发货地：浙江省 杭州市 西湖区 地点A/)).toBeTruthy();
    fireEvent.click(screen.getByText('清除'));
    expect(onChange).toHaveBeenCalledWith(null);
  });

  test('权限被拒绝、无附近地点和不支持定位都展示原因且不回传', /* 失败路径。 */ async () => {
    // onChange 记录回传。
    const onChange = vi.fn();
    render(<PublishLocationField value={null} onChange={onChange} />);
    stubGeolocation(false, 1);
    fireEvent.click(screen.getByText('获取当前位置'));
    await waitFor(/* 等待权限提示。 */ () => expect(screen.getByRole('alert').textContent).toContain('定位权限被拒绝'));
    stubGeolocation(true);
    vi.mocked(api.getPublishLocations).mockResolvedValue([]);
    fireEvent.click(screen.getByText('获取当前位置'));
    await waitFor(/* 等待无地点提示。 */ () => expect(screen.getByRole('alert').textContent).toContain('没有可用的高德发货地'));
    Object.defineProperty(navigator, 'geolocation', { configurable: true, value: undefined });
    fireEvent.click(screen.getByText('获取当前位置'));
    await waitFor(/* 等待不支持提示。 */ () => expect(screen.getByRole('alert').textContent).toContain('不支持定位'));
    expect(onChange).not.toHaveBeenCalled();
  });

  test('卸载后晚到的地点查询不会回传', /* 取消语义。 */ async () => {
    stubGeolocation(true);
    // resolveLater 在卸载后才完成地点查询。
    let resolveLater: (value: PublishLocation[]) => void = () => undefined;
    vi.mocked(api.getPublishLocations).mockReturnValue(new Promise(/* executor 暂存 resolve。 */ resolve => { resolveLater = resolve; }));
    // onChange 记录回传。
    const onChange = vi.fn();
    // view 保存渲染结果。
    const view = render(<PublishLocationField value={null} onChange={onChange} />);
    fireEvent.click(screen.getByText('获取当前位置'));
    await act(/* 让定位回调完成并发出地点查询。 */ async () => { await Promise.resolve(); });
    view.unmount();
    resolveLater([near]);
    await Promise.resolve();
    expect(onChange).not.toHaveBeenCalled();
  });
});
