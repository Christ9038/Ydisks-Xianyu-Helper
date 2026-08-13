import type { PublishLocation } from './api';

const AMAP_SCRIPT_ID = 'ydisks-amap-js-api';
// 高德 JS API 的 Key 是前端公开 Key；部署时可通过 VITE_AMAP_JS_KEY 覆盖。
const DEFAULT_AMAP_JS_KEY = 'c9b68d4ce9a2a97f22a4a439404488ca';

export interface AMapLocationValue {
  lng: number;
  lat: number;
}

export interface AMapPOI {
  id?: string;
  name?: string;
  address?: string;
  adname?: string;
  cityname?: string;
  adcode?: string | number;
  pname?: string;
  location?: AMapLocationValue;
}

interface AMapPlaceSearchResult {
  poiList?: {
    pois?: AMapPOI[];
  };
}

interface AMapPlaceSearch {
  searchNearBy(
    keyword: string,
    center: [number, number],
    radius: number,
    callback: (status: string, result: AMapPlaceSearchResult) => void,
  ): void;
}

interface AMapAPI {
  PlaceSearch: new (options: { extensions: 'all'; pageSize: number }) => AMapPlaceSearch;
}

declare global {
  interface Window {
    AMap?: AMapAPI;
    __ydisksAmapLoaded?: () => void;
  }

  interface ImportMetaEnv {
    readonly VITE_AMAP_JS_KEY?: string;
  }

  interface ImportMeta {
    readonly env: ImportMetaEnv;
  }
}

let amapLoadPromise: Promise<AMapAPI> | null = null;

const configuredAmapKey = (): string => {
  const key = import.meta.env.VITE_AMAP_JS_KEY?.trim();
  return key || DEFAULT_AMAP_JS_KEY;
};

const loadAMap = (): Promise<AMapAPI> => {
  if (window.AMap) return Promise.resolve(window.AMap);
  if (amapLoadPromise) return amapLoadPromise;

  amapLoadPromise = new Promise<AMapAPI>((resolve, reject) => {
    const existing = document.getElementById(AMAP_SCRIPT_ID) as HTMLScriptElement | null;
    const script = existing || document.createElement('script');
    const cleanup = () => {
      window.__ydisksAmapLoaded = undefined;
      window.clearTimeout(timeout);
    };
    const finish = () => {
      if (window.AMap) {
        cleanup();
        resolve(window.AMap);
      } else {
        cleanup();
        reject(new Error('高德地图 API 加载完成但未找到 AMap 对象'));
      }
    };
    const timeout = window.setTimeout(() => {
      cleanup();
      reject(new Error('高德地图 API 加载超时，请检查网络或 VITE_AMAP_JS_KEY 配置'));
    }, 15_000);

    window.__ydisksAmapLoaded = finish;
    script.id = AMAP_SCRIPT_ID;
    script.async = true;
    script.src = `https://webapi.amap.com/maps?v=2.0&key=${encodeURIComponent(configuredAmapKey())}&plugin=AMap.PlaceSearch&callback=__ydisksAmapLoaded`;
    script.onerror = () => {
      cleanup();
      reject(new Error('高德地图 API 加载失败，请检查网络或 VITE_AMAP_JS_KEY 配置'));
    };
    if (!existing) document.head.appendChild(script);
  }).catch(error => {
    amapLoadPromise = null;
    throw error;
  });

  return amapLoadPromise;
};

const validCoordinate = (value: number): boolean => Number.isFinite(value) && value !== 0;

// 字段映射与闲鱼网页版发布页一致：adcode → divisionId，name → poi，pname/cityname/adname → 行政区。
export const amapPOIToPublishLocation = (poi: AMapPOI): PublishLocation | null => {
  const longitude = Number(poi.location?.lng);
  const latitude = Number(poi.location?.lat);
  const location: PublishLocation = {
    area: String(poi.adname || '').trim(),
    city: String(poi.cityname || '').trim(),
    division_id: String(poi.adcode || '').trim(),
    longitude,
    latitude,
    poi_id: String(poi.id || '').trim(),
    poi_name: String(poi.name || '').trim(),
    province: String(poi.pname || '').trim(),
  };
  if (!location.division_id || !location.province || !location.city || !location.poi_id || !location.poi_name) {
    return null;
  }
  if (!validCoordinate(longitude) || !validCoordinate(latitude) || longitude < -180 || longitude > 180 || latitude < -90 || latitude > 90) {
    return null;
  }
  return location;
};

export const getPublishLocations = async (longitude: number, latitude: number): Promise<PublishLocation[]> => {
  if (!validCoordinate(longitude) || !validCoordinate(latitude) || longitude < -180 || longitude > 180 || latitude < -90 || latitude > 90) {
    throw new Error('经纬度无效');
  }
  const amap = await loadAMap();
  return new Promise<PublishLocation[]>((resolve, reject) => {
    const placeSearch = new amap.PlaceSearch({ extensions: 'all', pageSize: 10 });
    placeSearch.searchNearBy('', [longitude, latitude], 1_000, (status, result) => {
      if (status !== 'complete') {
        if (status === 'no_data') {
          resolve([]);
          return;
        }
        reject(new Error('高德地图附近地址查询失败，请稍后重试'));
        return;
      }
      const locations = (result?.poiList?.pois || [])
        .map(amapPOIToPublishLocation)
        .filter((location): location is PublishLocation => location !== null);
      resolve(locations);
    });
  });
};
