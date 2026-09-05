import { useCallback, useEffect, useRef, useState } from 'react';
import type { PublishLocation } from './models';
import { getPublishDistricts, searchPublishLocationsByKeyword, type AMapDistrict } from './amapLocation';

// ManualLocationState 描述高德手动选择发货地的级联数据、搜索结果和异步状态。
export interface ManualLocationState {
  // provinces 保存高德返回的省级行政区选项。
  provinces: AMapDistrict[];
  // cities 保存当前省份下由高德返回的城市选项。
  cities: AMapDistrict[];
  // districts 保存当前城市下由高德返回的区县选项。
  districts: AMapDistrict[];
  // selectedProvince 保存用户当前选中的省份。
  selectedProvince: AMapDistrict | null;
  // selectedCity 保存用户当前选中的城市。
  selectedCity: AMapDistrict | null;
  // selectedDistrict 保存用户当前选中的区县。
  selectedDistrict: AMapDistrict | null;
  // keyword 保存用户输入的具体位置搜索词。
  keyword: string;
  // setKeyword 更新具体位置搜索词，不触发自动搜索。
  setKeyword: (keyword: string) => void;
  // locations 保存当前区县内高德 PlaceSearch 返回的有效 POI。
  locations: PublishLocation[];
  // selectedLocation 保存用户最终确认的发货地 POI。
  selectedLocation: PublishLocation | null;
  // districtLoading 表示省市区级联请求是否正在执行。
  districtLoading: boolean;
  // searchLoading 表示具体位置 POI 搜索是否正在执行。
  searchLoading: boolean;
  // error 保存当前弹窗中可直接展示给用户的高德查询错误。
  error: string;
  // selectProvince 选择省份并加载其城市。
  selectProvince: (province: AMapDistrict) => void;
  // selectCity 选择城市并加载其区县。
  selectCity: (city: AMapDistrict) => void;
  // selectDistrict 选择区县并清空旧的 POI 搜索结果。
  selectDistrict: (district: AMapDistrict) => void;
  // searchLocations 按选中区县和关键词搜索 POI。
  searchLocations: () => Promise<void>;
  // selectLocation 选择一个高德 POI 作为发货地。
  selectLocation: (location: PublishLocation) => void;
}

// isAbortError 判断请求错误是否来自弹窗关闭或新请求取消。
const isAbortError = (error: unknown): boolean => error instanceof DOMException && error.name === 'AbortError';

// districtAdcode 读取高德行政区编码并保证可用于下一次查询。
const districtAdcode = (district: AMapDistrict): string => String(district.adcode || '').trim();

// useManualLocation 管理纯高德来源的省市区级联和 POI 选择生命周期。
export const useManualLocation = (open: boolean): ManualLocationState => {
  // provinces 保存当前弹窗的高德省级行政区数据。
  const [provinces, setProvinces] = useState<AMapDistrict[]>([]);
  // cities 保存当前选中省份的高德城市数据。
  const [cities, setCities] = useState<AMapDistrict[]>([]);
  // districts 保存当前选中城市的高德区县数据。
  const [districts, setDistricts] = useState<AMapDistrict[]>([]);
  // selectedProvince 保存当前省级选择。
  const [selectedProvince, setSelectedProvince] = useState<AMapDistrict | null>(null);
  // selectedCity 保存当前城市选择。
  const [selectedCity, setSelectedCity] = useState<AMapDistrict | null>(null);
  // selectedDistrict 保存当前区县选择。
  const [selectedDistrict, setSelectedDistrict] = useState<AMapDistrict | null>(null);
  // keyword 保存具体位置关键词表单值。
  const [keyword, setKeyword] = useState('');
  // locations 保存当前搜索请求返回的候选发货地。
  const [locations, setLocations] = useState<PublishLocation[]>([]);
  // selectedLocation 保存用户点击选中的候选发货地。
  const [selectedLocation, setSelectedLocation] = useState<PublishLocation | null>(null);
  // districtLoading 表示行政区查询是否正在加载。
  const [districtLoading, setDistrictLoading] = useState(false);
  // searchLoading 表示 POI 查询是否正在加载。
  const [searchLoading, setSearchLoading] = useState(false);
  // error 保存最近一次未被取消的高德查询错误。
  const [error, setError] = useState('');
  // requestController 保存当前高德请求的取消器，切换级联项时终止旧请求。
  const requestController = useRef<AbortController | null>(null);
  // requestGeneration 让晚到的高德回调不能覆盖当前选择。
  const requestGeneration = useRef(0);

  // cancelRequest 终止当前高德请求并推进响应所有权代次。
  const cancelRequest = useCallback(/* cancelRequestCallback 中止当前高德请求并清理加载状态。 */ () => {
    requestController.current?.abort();
    requestController.current = null;
    requestGeneration.current += 1;
    setDistrictLoading(false);
    setSearchLoading(false);
  }, []);

  // resetSelection 清理弹窗上一次使用的行政区、搜索结果和错误状态。
  const resetSelection = useCallback(/* resetSelectionCallback 清空弹窗上一次的高德选择结果。 */ () => {
    setProvinces([]);
    setCities([]);
    setDistricts([]);
    setSelectedProvince(null);
    setSelectedCity(null);
    setSelectedDistrict(null);
    setKeyword('');
    setLocations([]);
    setSelectedLocation(null);
    setError('');
  }, []);

  // loadRootDistricts 在弹窗打开时从高德加载省级行政区，避免前端维护固定列表。
  useEffect(/* rootDistrictEffectCallback 在弹窗打开时加载高德省级行政区。 */ () => {
    if (!open) {
      cancelRequest();
      return;
    }
    cancelRequest();
    resetSelection();
    // controller 是本次省级行政区查询的取消器。
    const controller = new AbortController();
    // generation 是本次省级行政区查询的响应所有权代次。
    const generation = ++requestGeneration.current;
    requestController.current = controller;
    setDistrictLoading(true);
    void getPublishDistricts(undefined, { signal: controller.signal })
      .then(/* rootDistrictSuccess 处理高德省级行政区结果。 */ result => {
        if (controller.signal.aborted || generation !== requestGeneration.current) return;
        if (!result.length) throw new Error('高德没有返回省级行政区');
        setProvinces(result);
      })
      .catch(/* rootDistrictFailure 处理省级行政区查询失败。 */ loadError => {
        if (!controller.signal.aborted && generation === requestGeneration.current && !isAbortError(loadError)) setError(loadError instanceof Error ? loadError.message : '省级行政区加载失败');
      })
      .finally(/* rootDistrictFinally 收口省级行政区加载状态。 */ () => {
        if (generation === requestGeneration.current) {
          requestController.current = null;
          setDistrictLoading(false);
        }
      });
    return /* rootDistrictCleanup 关闭弹窗时取消省级行政区查询。 */ () => controller.abort();
  }, [cancelRequest, open, resetSelection]);

  // loadDistrictChildren 根据父级行政区加载城市或区县，并隔离晚到响应。
  const loadDistrictChildren = useCallback(/* childDistrictCallback 加载当前父级的高德下级行政区。 */ async (parent: AMapDistrict, target: 'cities' | 'districts'): Promise<void> => {
    cancelRequest();
    setError('');
    // controller 是本次下级行政区查询的取消器。
    const controller = new AbortController();
    // generation 是本次下级行政区查询的响应所有权代次。
    const generation = ++requestGeneration.current;
    requestController.current = controller;
    setDistrictLoading(true);
    try {
      // children 是高德根据父级 adcode 返回的直接下级行政区。
      const children = await getPublishDistricts(districtAdcode(parent), { signal: controller.signal });
      if (controller.signal.aborted || generation !== requestGeneration.current) return;
      if (target === 'cities') setCities(children);
      else setDistricts(children);
    } catch (/* loadError 保存下级行政区查询失败信息。 */ loadError: unknown) {
      if (!controller.signal.aborted && generation === requestGeneration.current && !isAbortError(loadError)) setError(loadError instanceof Error ? loadError.message : '行政区加载失败');
    } finally {
      if (generation === requestGeneration.current) {
        requestController.current = null;
        setDistrictLoading(false);
      }
    }
  }, [cancelRequest]);

  // selectProvince 写入省份并从高德加载城市，旧的城市、区县和 POI 结果立即失效。
  const selectProvince = useCallback(/* selectProvinceCallback 处理省份选择并刷新城市列表。 */ (province: AMapDistrict) => {
    setSelectedProvince(province);
    setSelectedCity(null);
    setSelectedDistrict(null);
    setCities([]);
    setDistricts([]);
    setKeyword('');
    setLocations([]);
    setSelectedLocation(null);
    void loadDistrictChildren(province, 'cities');
  }, [loadDistrictChildren]);

  // selectCity 写入城市并从高德加载区县，旧的区县和 POI 结果立即失效。
  const selectCity = useCallback(/* selectCityCallback 处理城市选择并刷新区县列表。 */ (city: AMapDistrict) => {
    setSelectedCity(city);
    setSelectedDistrict(null);
    setDistricts([]);
    setKeyword('');
    setLocations([]);
    setSelectedLocation(null);
    void loadDistrictChildren(city, 'districts');
  }, [loadDistrictChildren]);

  // selectDistrict 写入区县并要求用户重新搜索具体地点。
  const selectDistrict = useCallback(/* selectDistrictCallback 处理区县选择并重置 POI 候选。 */ (district: AMapDistrict) => {
    cancelRequest();
    setSelectedDistrict(district);
    setKeyword('');
    setLocations([]);
    setSelectedLocation(null);
    setError('');
  }, [cancelRequest]);

  // searchLocations 按区县 adcode 调用高德 PlaceSearch 搜索具体发货地。
  const searchLocations = useCallback(/* searchLocationsCallback 按当前区县关键词查询高德 POI。 */ async (): Promise<void> => {
    if (!selectedDistrict) {
      setError('请先选择区县');
      return;
    }
    if (!keyword.trim()) {
      setError('请输入小区、街道、公司或商场名称');
      return;
    }
    cancelRequest();
    setError('');
    setLocations([]);
    setSelectedLocation(null);
    // controller 是本次区县内 POI 搜索的取消器。
    const controller = new AbortController();
    // generation 是本次 POI 搜索的响应所有权代次。
    const generation = ++requestGeneration.current;
    requestController.current = controller;
    setSearchLoading(true);
    try {
      // result 是高德按区县编码返回的有效 POI 候选。
      const result = await searchPublishLocationsByKeyword(keyword, districtAdcode(selectedDistrict), { signal: controller.signal });
      if (controller.signal.aborted || generation !== requestGeneration.current) return;
      setLocations(result);
      if (!result.length) setError('当前位置没有匹配的高德地点，请换一个关键词');
    } catch (/* searchError 保存 POI 查询失败信息。 */ searchError: unknown) {
      if (!controller.signal.aborted && generation === requestGeneration.current && !isAbortError(searchError)) setError(searchError instanceof Error ? searchError.message : '地点搜索失败');
    } finally {
      if (generation === requestGeneration.current) {
        requestController.current = null;
        setSearchLoading(false);
      }
    }
  }, [cancelRequest, keyword, selectedDistrict]);

  // selectLocation 写入用户选中的高德 POI，等待弹窗确认后交给发布表单。
  const selectLocation = useCallback(/* selectLocationCallback 记录用户确认前选中的高德 POI。 */ (location: PublishLocation) => {
    setSelectedLocation(location);
    setError('');
  }, []);

  return { provinces, cities, districts, selectedProvince, selectedCity, selectedDistrict, keyword, setKeyword, locations, selectedLocation, districtLoading, searchLoading, error, selectProvince, selectCity, selectDistrict, searchLocations, selectLocation };
};
