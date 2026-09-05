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

// AMapDistrict 表示高德行政区查询返回的省、市或区县节点。
export interface AMapDistrict {
// adcode 是高德行政区划编码，也是后续 POI 搜索的区域边界标识。
  adcode?: string | number;
// name 是高德返回的行政区展示名称。
  name?: string;
// level 是高德返回的行政区层级，用于级联选择和诊断。
  level?: string;
// districtList 是当前行政区的直接下级节点，由高德按需返回。
  districtList?: AMapDistrict[];
}

interface AMapPlaceSearchResult {
// poiList 表示poiList。
    poiList?: {
// pois 表示pois。
        pois?: AMapPOI[];
  };
}

// AMapDistrictSearchResult 描述高德行政区查询的回调结果。
interface AMapDistrictSearchResult {
// districtList 保存匹配到的行政区及其下级节点。
    districtList?: AMapDistrict[];
// info 是高德返回的附加状态信息。
    info?: string;
}

interface AMapPlaceSearch {
// search 根据关键词和行政区范围搜索 POI。
    search(keyword: string, callback: (status: string, result: AMapPlaceSearchResult) => void): void;
// searchNearBy 表示searchNearBy。
    searchNearBy(
    keyword: string,
    center: [number, number],
    radius: number,
    callback: (status: string, result: AMapPlaceSearchResult) => void,
  ): void;
}

// AMapDistrictSearch 负责从高德动态读取行政区级联数据。
interface AMapDistrictSearch {
// search 查询指定行政区的直接下级节点。
    search(keyword: string, callback: (status: string, result: AMapDistrictSearchResult) => void): void;
}

interface AMapAPI {
// PlaceSearch 表示PlaceSearch。
    PlaceSearch: new (options: { /* city 是搜索限定的高德城市或区县编码。 */ city?: string; /* citylimit 是是否强制限定搜索区域。 */ citylimit?: boolean; /* extensions 表示extensions。 */ extensions: 'all'; /* pageSize 表示pageSize。 */ pageSize: number }) => AMapPlaceSearch;
// DistrictSearch 是高德行政区划查询插件构造器。
    DistrictSearch: new (options: { /* subdistrict 是返回直接下级行政区的层级数。 */ subdistrict: number; /* extensions 是返回基础行政区字段。 */ extensions: 'base' }) => AMapDistrictSearch;
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
    script.src = `https://webapi.amap.com/maps?v=2.0&key=${encodeURIComponent(configuredAmapKey())}&plugin=AMap.PlaceSearch,AMap.DistrictSearch&callback=__ydisksAmapLoaded`;
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

// PublishDistrictRequestOptions 控制行政区查询的取消和最长等待时间。
export interface PublishDistrictRequestOptions {
// signal 是弹窗关闭或组件卸载时终止行政区查询的取消信号。
  signal?: AbortSignal;
// timeoutMs 是行政区回调允许占用的最长毫秒数。
  timeoutMs?: number;
}

// amapDistrictToOption 将高德行政区节点转换为可供下拉框使用的稳定选项。
const amapDistrictToOption = (district: AMapDistrict): AMapDistrict | null => {
  // adcode 是后续 PlaceSearch 区域限制和下一级查询所需的行政区编码。
  const adcode = String(district.adcode || '').trim();
  // name 是用户在级联选择器中看到的行政区名称。
  const name = String(district.name || '').trim();
  if (!adcode || !name) return null;
  return { ...district, adcode, name };
};

// districtChildren 从高德查询响应中提取指定父级的直接下级行政区。
const districtChildren = (result: AMapDistrictSearchResult, parentAdcode?: string): AMapDistrict[] => {
  // districts 是高德返回的顶层匹配节点集合。
  const districts = result.districtList || [];
  // parent 是当前查询对应的父级节点；根查询没有父级编码时使用首个国家节点。
  const parent = parentAdcode ? districts.find(/* parentMatchCallback 定位当前父级行政区节点。 */ district => String(district.adcode || '') === parentAdcode) : districts[0];
  // children 是高德返回的直接下级节点，避免把多级行政区混入当前下拉框。
  const children = parent?.districtList || (parentAdcode ? districts : []);
  return children.map(/* childOptionCallback 转换单个高德下级行政区。 */ amapDistrictToOption).filter(/* validChildCallback 保留具备名称和编码的行政区。 */ (district): district is AMapDistrict => district !== null);
};

// getPublishDistricts 从高德按父级行政区动态读取下级省市区选项。
export const getPublishDistricts = async (parentAdcode?: string, options: PublishDistrictRequestOptions = {}): Promise<AMapDistrict[]> => {
  // amap 是已加载的高德 JS API，用于行政区和 POI 两类查询。
  const amap = await loadAMap();
  return new Promise<AMapDistrict[]>(/* districtPromiseExecutor 将高德回调转换为可取消 Promise。 */ (resolve, reject) => {
    // settled 标记当前行政区查询是否已经完成，防止超时和 SDK 回调重复收口。
    let settled = false;
    // timeoutMs 是行政区查询的有限等待预算，避免级联下拉框永久显示加载状态。
    const timeoutMs = Math.max(1, options.timeoutMs ?? 10_000);
    // finish 统一释放定时器和取消监听，并确保 Promise 只完成一次。
    const finish = (callback: () => void) => {
      if (settled) return;
      settled = true;
      globalThis.clearTimeout(timer);
      options.signal?.removeEventListener('abort', abortSearch);
      callback();
    };
    // abortSearch 将弹窗关闭转换为可识别的取消错误。
    const abortSearch = () => finish(() => reject(new DOMException('aborted', 'AbortError')));
    // timer 是行政区查询的超时定时器，成功、失败和取消时都会清理。
    const timer = globalThis.setTimeout(/* districtTimeoutCallback 在高德未回调时结束行政区查询。 */ () => finish(/* districtTimeoutRejectCallback 返回行政区超时错误。 */ () => reject(new Error('高德行政区查询超时，请稍后重试'))), timeoutMs);
    if (options.signal?.aborted) {
      abortSearch();
      return;
    }
    options.signal?.addEventListener('abort', abortSearch, { once: true });
    // districtSearch 是当前级联选择使用的高德行政区查询实例。
    const districtSearch = new amap.DistrictSearch({ subdistrict: 1, extensions: 'base' });
    districtSearch.search(parentAdcode || '中国', /* districtSearchCallback 处理当前级联层级的高德结果。 */ (status, result) => {
      if (settled) return;
      if (status !== 'complete') {
        finish(/* districtFailureCallback 返回行政区查询失败。 */ () => reject(new Error('高德行政区查询失败，请稍后重试')));
        return;
      }
      finish(/* districtSuccessCallback 返回当前父级的高德下级行政区。 */ () => resolve(districtChildren(result || {}, parentAdcode)));
    });
  });
};

// searchPublishLocationsByKeyword 在指定区县内查询可用于发货地的 POI。
export const searchPublishLocationsByKeyword = async (keyword: string, districtAdcode: string, options: PublishDistrictRequestOptions = {}): Promise<PublishLocation[]> => {
  // normalizedKeyword 是去除首尾空白后的搜索词，避免空关键词触发无意义请求。
  const normalizedKeyword = keyword.trim();
  // normalizedAdcode 是高德区县编码，确保查询不会退化为全国搜索。
  const normalizedAdcode = districtAdcode.trim();
  if (!normalizedKeyword) throw new Error('请输入具体位置');
  if (!normalizedAdcode) throw new Error('请先选择区县');
  // amap 是已加载的高德 JS API，用于创建区域限定的 POI 搜索实例。
  const amap = await loadAMap();
  return new Promise<PublishLocation[]>(/* poiPromiseExecutor 将高德 POI 回调转换为可取消 Promise。 */ (resolve, reject) => {
    // settled 标记当前 POI 查询是否已经完成，防止超时和 SDK 回调重复收口。
    let settled = false;
    // timeoutMs 是 POI 搜索的有限等待预算，防止搜索按钮永久处于加载状态。
    const timeoutMs = Math.max(1, options.timeoutMs ?? 10_000);
    // finish 统一释放定时器和取消监听，并确保 Promise 只完成一次。
    const finish = (callback: () => void) => {
      if (settled) return;
      settled = true;
      globalThis.clearTimeout(timer);
      options.signal?.removeEventListener('abort', abortSearch);
      callback();
    };
    // abortSearch 将弹窗关闭转换为可识别的取消错误。
    const abortSearch = () => finish(() => reject(new DOMException('aborted', 'AbortError')));
    // timer 是 POI 查询的超时定时器，成功、失败和取消时都会清理。
    const timer = globalThis.setTimeout(/* poiTimeoutCallback 在高德未回调时结束 POI 查询。 */ () => finish(/* poiTimeoutRejectCallback 返回 POI 超时错误。 */ () => reject(new Error('高德地点搜索超时，请稍后重试'))), timeoutMs);
    if (options.signal?.aborted) {
      abortSearch();
      return;
    }
    options.signal?.addEventListener('abort', abortSearch, { once: true });
    // placeSearch 是按区县行政编码严格限定范围的高德 POI 搜索实例。
    const placeSearch = new amap.PlaceSearch({ city: normalizedAdcode, citylimit: true, extensions: 'all', pageSize: 10 });
    placeSearch.search(normalizedKeyword, /* poiSearchCallback 处理区县内的高德 POI 搜索结果。 */ (status, result) => {
      if (settled) return;
      if (status === 'no_data') {
        finish(/* poiNoDataCallback 返回空的 POI 候选列表。 */ () => resolve([]));
        return;
      }
      if (status !== 'complete') {
        finish(/* poiFailureCallback 返回 POI 查询失败。 */ () => reject(new Error('高德地点搜索失败，请稍后重试')));
        return;
      }
      // locations 是过滤后具备完整行政区、坐标和 POI 标识的候选发货地。
      const locations = (result?.poiList?.pois || [])
        .map(/* poiMappingCallback 转换单个高德 POI 为发货地模型。 */ amapPOIToPublishLocation)
        .filter(/* validPoiCallback 保留具备完整发货字段的 POI。 */ (location): location is PublishLocation => location !== null);
      finish(/* poiSuccessCallback 返回过滤后的发货地候选。 */ () => resolve(locations));
    });
  });
};
