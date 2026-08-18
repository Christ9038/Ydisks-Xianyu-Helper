import type { PublishLocation } from './api';

const AMAP_SCRIPT_ID = 'ydisks-amap-js-api'; /* AMAP_SCRIPT_ID 表示AMAPSCRIPTID。 */
// 高德 JS API 的 Key 是前端公开 Key；部署时可通过 VITE_AMAP_JS_KEY 覆盖。
const DEFAULT_AMAP_JS_KEY = 'c9b68d4ce9a2a97f22a4a439404488ca';

export interface AMapLocationValue {
// lng 表示lng。
    lng: number;
// lat 表示lat。
    lat: number;
}

export interface AMapPOI {
// id 表示id。
    id?: string;
// name 表示name。
    name?: string;
// address 表示address。
    address?: string;
// adname 表示adname。
    adname?: string;
// cityname 表示cityname。
    cityname?: string;
// adcode 表示adcode。
    adcode?: string | number;
// pname 表示pname。
    pname?: string;
// location 表示location。
    location?: AMapLocationValue;
}

interface AMapPlaceSearchResult {
// poiList 表示poiList。
    poiList?: {
// pois 表示pois。
        pois?: AMapPOI[];
  };
}

interface AMapPlaceSearch {
// searchNearBy 表示searchNearBy。
    searchNearBy(
    keyword: string,
    center: [number, number],
    radius: number,
    callback: (status: string, result: AMapPlaceSearchResult) => void,
  ): void;
}

interface AMapAPI {
// PlaceSearch 表示PlaceSearch。
    PlaceSearch: new (options: { /* extensions 表示extensions。 */ extensions: 'all'; /* pageSize 表示pageSize。 */ pageSize: number }) => AMapPlaceSearch;
}

// PublishLocationRequestOptions 控制高德地点查询的取消和最长等待时间。
export interface PublishLocationRequestOptions {
// signal 是调用方在组件卸载、切换账号或关闭弹窗时触发的取消信号。
  signal?: AbortSignal;
// timeoutMs 是高德地点回调允许占用的最长毫秒数。
  timeoutMs?: number;
}

declare global {
  interface Window {
// AMap 表示AMap。
        AMap?: AMapAPI;
// __ydisksAmapLoaded 表示ydisksAmapLoaded。
        __ydisksAmapLoaded?: () => void;
  }

  interface ImportMetaEnv {
// VITE_AMAP_JS_KEY 表示VITEAMAPJSKEY。
        readonly VITE_AMAP_JS_KEY?: string;
  }

  interface ImportMeta {
// env 表示env。
        readonly env: ImportMetaEnv;
  }
}

let amapLoadPromise: Promise<AMapAPI> | null = null; /* amapLoadPromise 表示amapLoadPromise。 */

const configuredAmapKey = (): string => {
  const key = import.meta.env.VITE_AMAP_JS_KEY?.trim(); /* key 表示key。 */
  return key || DEFAULT_AMAP_JS_KEY;
}; /* configuredAmapKey 表示configuredAmapKey。 */

const loadAMap = (): Promise<AMapAPI> => {
  if (window.AMap) return Promise.resolve(window.AMap);
  if (amapLoadPromise) return amapLoadPromise;

  amapLoadPromise = new Promise<AMapAPI>((resolve, reject) => {
    const existing = document.getElementById(AMAP_SCRIPT_ID) as HTMLScriptElement | null; /* existing 表示existing。 */
    const script = existing || document.createElement('script'); /* script 表示script。 */
    const cleanup = () => {
      window.__ydisksAmapLoaded = undefined;
      window.clearTimeout(timeout);
    }; /* cleanup 表示cleanup。 */
    const finish = () => {
      if (window.AMap) {
        cleanup();
        resolve(window.AMap);
      } else {
        cleanup();
        reject(new Error('高德地图 API 加载完成但未找到 AMap 对象'));
      }
    }; /* finish 表示finish。 */
    const timeout = window.setTimeout(() => {
      cleanup();
      reject(new Error('高德地图 API 加载超时，请检查网络或 VITE_AMAP_JS_KEY 配置'));
    } /* 定时器到期时终止脚本加载并返回网络配置错误。 */, 15_000); /* timeout 是高德脚本加载的毫秒上限。 */

    window.__ydisksAmapLoaded = finish;
    script.id = AMAP_SCRIPT_ID;
    script.async = true;
    script.src = `https://webapi.amap.com/maps?v=2.0&key=${encodeURIComponent(configuredAmapKey())}&plugin=AMap.PlaceSearch&callback=__ydisksAmapLoaded`;
    script.onerror = () => {
      cleanup();
      reject(new Error('高德地图 API 加载失败，请检查网络或 VITE_AMAP_JS_KEY 配置'));
    } /* 脚本节点触发 error 时释放监听并拒绝加载 Promise。 */;
    if (!existing) document.head.appendChild(script);
  } /* 单例加载过程只创建一次脚本节点并复用完成 Promise。 */).catch(error => {
    amapLoadPromise = null;
    throw error;
  } /* 失败时清空单例 Promise，允许后续调用重新加载脚本。 */);

  return amapLoadPromise;
}; /* loadAMap 表示loadAMap。 */

const validCoordinate = (value: number): boolean => Number.isFinite(value) && value !== 0; /* validCoordinate 表示validCoordinate。 */

// 字段映射与闲鱼网页版发布页一致：adcode → divisionId，name → poi，pname/cityname/adname → 行政区。
export const amapPOIToPublishLocation = (poi: AMapPOI): PublishLocation | null => {
  const longitude = Number(poi.location?.lng); /* longitude 表示longitude。 */
  const latitude = Number(poi.location?.lat); /* latitude 表示latitude。 */
  const location: PublishLocation = {
    area: String(poi.adname || '').trim(),
    city: String(poi.cityname || '').trim(),
    division_id: String(poi.adcode || '').trim(),
    longitude,
    latitude,
    poi_id: String(poi.id || '').trim(),
    poi_name: String(poi.name || '').trim(),
    province: String(poi.pname || '').trim(),
  }; /* location 表示location。 */
  if (!location.division_id || !location.province || !location.city || !location.poi_id || !location.poi_name) {
    return null;
  }
  if (!validCoordinate(longitude) || !validCoordinate(latitude) || longitude < -180 || longitude > 180 || latitude < -90 || latitude > 90) {
    return null;
  }
  return location;
};

export const getPublishLocations = async (longitude: number, latitude: number, options: PublishLocationRequestOptions = {}): Promise<PublishLocation[]> => {
  if (!validCoordinate(longitude) || !validCoordinate(latitude) || longitude < -180 || longitude > 180 || latitude < -90 || latitude > 90) {
    throw new Error('经纬度无效');
  }
  const amap = await loadAMap(); /* amap 表示amap。 */
  return new Promise<PublishLocation[]>((resolve, reject) => {
    // settled 标记本次查询是否已经结束，阻止超时、取消和 SDK 回调重复收口。
    let settled = false;
    // timeoutMs 是地点搜索回调的有限等待预算，防止 AMap 永不回调时永久 loading。
    const timeoutMs = Math.max(1, options.timeoutMs ?? 10_000);
    // finish 统一清理本次搜索的定时器和取消监听，确保 Promise 只完成一次。
    const finish = (callback: () => void) => {
      if (settled) return;
      settled = true;
      globalThis.clearTimeout(timer);
      options.signal?.removeEventListener('abort', abortSearch);
      callback();
    };
    // abortSearch 将调用方取消转换为 Promise 拒绝，SDK 晚到回调随后只会被忽略。
    const abortSearch = () => finish(() => reject(new DOMException('aborted', 'AbortError')));
    // timer 是地点搜索超时定时器，成功、失败和取消时都会清理。
    const timer = globalThis.setTimeout(/* searchTimeoutAction 在 SDK 未回调时统一结束地点查询。 */ () => finish(/* timeoutRejectAction 将超时原因传回调用方。 */ () => reject(new Error('高德地图附近地址查询超时，请稍后重试'))), timeoutMs);
    if (options.signal?.aborted) {
      abortSearch();
      return;
    }
    options.signal?.addEventListener('abort', abortSearch, { once: true });
    const placeSearch = new amap.PlaceSearch({ extensions: 'all', pageSize: 10 }); /* placeSearch 表示placeSearch。 */
    placeSearch.searchNearBy('', [longitude, latitude], 1_000, (status, result) => {
      if (settled) return;
      if (status !== 'complete') {
        if (status === 'no_data') {
          finish(/* noDataResolveAction 将高德无数据状态作为正常空地点列表返回。 */ () => resolve([]));
          return;
        }
        finish(/* searchFailureRejectAction 将高德失败状态转换为用户可见错误。 */ () => reject(new Error('高德地图附近地址查询失败，请稍后重试')));
        return;
      }
      // locations 是过滤无效坐标后的发布地点列表，供商品发布表单使用。
      const locations = (result?.poiList?.pois || [])
        .map(amapPOIToPublishLocation)
        .filter((location): location is PublishLocation => location !== null /* location 是转换后仍具备有效坐标的地点。 */);
      finish(/* searchSuccessResolveAction 将已过滤的有效地点列表交给调用方。 */ () => resolve(locations));
    } /* 搜索完成后将地点结果交给调用方并结束本次高德回调。 */);
  } /* 高德回调只处理当前请求，组件卸载后由 cleanup 移除监听。 */);
}; /* getPublishLocations 表示getPublishLocations。 */
