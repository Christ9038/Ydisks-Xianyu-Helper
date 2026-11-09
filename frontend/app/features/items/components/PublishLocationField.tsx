import { LocateFixed, MapPin } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { getPublishLocations, itemErrorMessage } from '../api';
import type { PublishLocation } from '../models';
import { ManualLocationPicker } from './ManualLocationPicker';

/** PublishLocationFieldProps 描述发货地选择字段的受控输入。 */
export interface PublishLocationFieldProps {
  /** value 是当前选中的发货地；为空表示不指定，由平台沿用账号默认。 */
  value: PublishLocation | null;
  /** onChange 在用户定位、改选、手动选点或清除后回传新的完整发货地（含坐标与 POI）。 */
  onChange: (location: PublishLocation | null) => void;
}

/** locationText 生成发货地的完整展示文本。 */
const locationText = (location: PublishLocation): string => [location.province, location.city, location.area, location.poi_name].filter(Boolean).join(' ');

/**
 * PublishLocationField 复用商品管理发布使用的定位与高德手动选点流程，
 * 保证选出的发货地包含行政区划、坐标和 POI，满足闲鱼发布接口的完整性要求。
 * 定位与地点查询由同一个 AbortController 约束，卸载或重复触发时取消，晚到响应不会覆盖新结果。
 */
export const PublishLocationField = ({ value, onChange }: PublishLocationFieldProps) => {
  // candidates 是当前位置附近的候选发货地，选中项由 value 决定。
  const [candidates, setCandidates] = useState<PublishLocation[]>([]);
  // loading 表示定位或附近地点查询进行中。
  const [loading, setLoading] = useState(false);
  // error 是最近一次定位失败的提示。
  const [error, setError] = useState('');
  // manualOpen 表示手动选点弹窗是否打开。
  const [manualOpen, setManualOpen] = useState(false);
  // controllerRef 保存进行中的定位请求取消器。
  const controllerRef = useRef<AbortController | null>(null);
  useEffect(/* 卸载时取消未完成的定位与地点查询。 */ () => /* 卸载清理。 */ () => controllerRef.current?.abort(), []);

  /** locate 使用浏览器定位查询附近高德发货地，并默认选中第一项。 */
  const locate = async (): Promise<void> => {
    if (!navigator.geolocation) { setError('当前浏览器不支持定位'); return; }
    controllerRef.current?.abort();
    // controller 控制本次定位与地点查询。
    const controller = new AbortController();
    controllerRef.current = controller;
    setLoading(true);
    setError('');
    try {
      // position 是浏览器返回的当前坐标。
      const position = await new Promise<GeolocationPosition>(/* executor 把回调式定位转为 Promise。 */ (resolve, reject) => {
        navigator.geolocation.getCurrentPosition(resolve, reject, { enableHighAccuracy: true, timeout: 15000, maximumAge: 60000 });
      });
      if (controller.signal.aborted) return;
      // locations 是附近可用发货地。
      const locations = await getPublishLocations(position.coords.longitude, position.coords.latitude, { signal: controller.signal });
      if (controller.signal.aborted) return;
      if (!locations.length) throw new Error('当前位置附近没有可用的高德发货地，请稍后重试');
      setCandidates(locations);
      onChange(locations[0]);
    } catch (failure /* failure 是定位或查询失败原因。 */) {
      if (controller.signal.aborted) return;
      if (typeof failure === 'object' && failure !== null && 'code' in failure) {
        // positionError 是浏览器定位失败对象，用于区分权限拒绝。
        const positionError = failure as GeolocationPositionError;
        setError(positionError.code === positionError.PERMISSION_DENIED ? '定位权限被拒绝，请在浏览器设置中允许定位' : '无法获取当前位置，请稍后重试');
      } else {
        setError(itemErrorMessage(failure, '获取发货地失败'));
      }
    } finally {
      if (!controller.signal.aborted) setLoading(false);
    }
  };

  /** confirmManual 接收手动选中的完整高德 POI。 */
  const confirmManual = (location: PublishLocation): void => {
    setCandidates([location]);
    onChange(location);
    setManualOpen(false);
    setError('');
  };

  // selectedIndex 由选中值在候选中的位置推导，找不到时为 -1。
  const selectedIndex = value ? candidates.indexOf(value) : -1;
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <button type="button" disabled={loading} onClick={/* 获取设备当前位置附近的发货地。 */ () => void locate()} className="ios-btn-primary flex items-center gap-2 rounded-xl px-4 py-2 text-sm font-bold disabled:opacity-50"><LocateFixed className="h-4 w-4" />{loading ? '定位中...' : '获取当前位置'}</button>
        <button type="button" onClick={/* 打开高德手动选点。 */ () => setManualOpen(true)} className="flex items-center gap-2 rounded-xl border border-sky-200 bg-white px-4 py-2 text-sm font-bold text-sky-700 hover:bg-sky-50"><MapPin className="h-4 w-4" />手动选择</button>
        {value && <button type="button" onClick={/* 清除后不指定发货地。 */ () => { setCandidates([]); onChange(null); }} className="text-xs font-bold text-gray-500 hover:text-red-600">清除</button>}
      </div>
      {error && <p role="alert" className="text-xs text-red-600">{error}</p>}
      {candidates.length > 1 && (
        <select aria-label="选择发货地" className="ios-input w-full rounded-xl bg-white px-4 py-3" value={String(Math.max(0, selectedIndex))} onChange={/* 改选附近候选发货地。 */ event => onChange(candidates[Number(event.target.value)] ?? null)}>
          {candidates.map(/* item、index 渲染一个候选发货地。 */ (item, index) => <option key={`${item.division_id}-${item.poi_id}-${index}`} value={String(index)}>{locationText(item)}</option>)}
        </select>
      )}
      {value && <p className="rounded-xl bg-sky-50 px-3 py-2 text-xs text-sky-800">已选发货地：{locationText(value)}</p>}
      <ManualLocationPicker open={manualOpen} onClose={/* 关闭手动选点。 */ () => setManualOpen(false)} onConfirm={confirmManual} />
    </div>
  );
};

export default PublishLocationField;
