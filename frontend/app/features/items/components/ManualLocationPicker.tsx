import { CheckCircle2, ChevronRight, LoaderCircle, Search, X } from 'lucide-react';
import React from 'react';
import { createPortal } from 'react-dom';
import type { PublishLocation } from '../models';
import { useManualLocation } from '../manualLocation';
import type { AMapDistrict } from '../amapLocation';

// ManualLocationPickerProps 描述纯高德手动选择发货地弹窗的边界。
export interface ManualLocationPickerProps {
  // open 表示弹窗是否属于当前页面并应接收用户操作。
  open: boolean;
  // onClose 负责关闭弹窗并终止其拥有的高德请求。
  onClose: () => void;
  // onConfirm 将用户选中的完整高德 POI 交回普通或批量发布表单。
  onConfirm: (location: PublishLocation) => void;
}

// districtLabel 生成行政区下拉框使用的展示文本。
const districtLabel = (district: AMapDistrict): string => district.name || '未命名行政区';

// locationLabel 生成 POI 候选的完整地址文本。
const locationLabel = (location: PublishLocation): string => [location.province, location.city, location.area, location.poi_name].filter(Boolean).join(' ');

// ManualLocationPicker 渲染省市区级联、区县内 POI 搜索和确认操作。
export const ManualLocationPicker: React.FC<ManualLocationPickerProps> = ({ open, onClose, onConfirm }) => {
  // locationState 保存弹窗拥有的高德行政区、POI 和异步状态。
  const locationState = useManualLocation(open);
  // handleConfirm 只允许提交用户明确选中的高德 POI。
  const handleConfirm = () => {
    if (locationState.selectedLocation) onConfirm(locationState.selectedLocation);
  };
  if (!open) return null;

  return createPortal(
    <div className="modal-overlay-centered" role="presentation">
      <div className="modal-container" style={{ maxWidth: '720px' }} role="dialog" aria-modal="true" aria-labelledby="manual-location-title">
        <div className="modal-header flex items-start justify-between gap-4">
          <div>
            <div className="mb-2 flex items-center gap-2 text-[11px] font-extrabold uppercase tracking-[0.18em] text-sky-600">
              <span className="h-1.5 w-1.5 rounded-full bg-sky-500" />
              AMap POI
            </div>
            <h3 id="manual-location-title" className="text-xl font-extrabold text-gray-900">手动选择发货地</h3>
            <p className="mt-1 text-xs leading-5 text-gray-500">省市区和具体地点均来自高德，选择后会作为闲鱼发货地提交。</p>
          </div>
          <button type="button" onClick={onClose} className="rounded-xl p-2 transition-colors hover:bg-gray-100" title="关闭" aria-label="关闭手动选择发货地">
            <X className="h-5 w-5 text-gray-500" />
          </button>
        </div>

        <div className="modal-body space-y-5">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <label className="space-y-2">
              <span className="text-xs font-extrabold uppercase tracking-wide text-gray-500">01 · 省份</span>
              <select className="w-full ios-input rounded-xl bg-white px-3 py-3" value={locationState.selectedProvince?.adcode ? String(locationState.selectedProvince.adcode) : ''} disabled={locationState.districtLoading && locationState.provinces.length === 0} onChange={/* provinceChangeAction 加载选中省份下的城市。 */ event => {
                // province 是当前下拉值对应的高德省级行政区。
                const province = locationState.provinces.find(/* provinceMatchCallback 定位当前选中的高德省份。 */ option => String(option.adcode) === event.target.value);
                if (province) locationState.selectProvince(province);
              }}>
                <option value="">{locationState.districtLoading && locationState.provinces.length === 0 ? '正在加载...' : '选择省份'}</option>
                {locationState.provinces.map(/* provinceOptionRenderer 渲染高德省级行政区选项。 */ province => <option key={String(province.adcode)} value={String(province.adcode)}>{districtLabel(province)}</option>)}
              </select>
            </label>
            <label className="space-y-2">
              <span className="text-xs font-extrabold uppercase tracking-wide text-gray-500">02 · 城市</span>
              <select className="w-full ios-input rounded-xl bg-white px-3 py-3" value={locationState.selectedCity?.adcode ? String(locationState.selectedCity.adcode) : ''} disabled={!locationState.selectedProvince || locationState.districtLoading} onChange={/* cityChangeAction 加载选中城市下的区县。 */ event => {
                // city 是当前下拉值对应的高德城市行政区。
                const city = locationState.cities.find(/* cityMatchCallback 定位当前选中的高德城市。 */ option => String(option.adcode) === event.target.value);
                if (city) locationState.selectCity(city);
              }}>
                <option value="">{locationState.districtLoading && locationState.selectedProvince ? '正在加载...' : '选择城市'}</option>
                {locationState.cities.map(/* cityOptionRenderer 渲染高德城市行政区选项。 */ city => <option key={String(city.adcode)} value={String(city.adcode)}>{districtLabel(city)}</option>)}
              </select>
            </label>
            <label className="space-y-2">
              <span className="text-xs font-extrabold uppercase tracking-wide text-gray-500">03 · 区县</span>
              <select className="w-full ios-input rounded-xl bg-white px-3 py-3" value={locationState.selectedDistrict?.adcode ? String(locationState.selectedDistrict.adcode) : ''} disabled={!locationState.selectedCity || locationState.districtLoading} onChange={/* districtChangeAction 选择 POI 搜索边界。 */ event => {
                // district 是当前下拉值对应的高德区县行政区。
                const district = locationState.districts.find(/* districtMatchCallback 定位当前选中的高德区县。 */ option => String(option.adcode) === event.target.value);
                if (district) locationState.selectDistrict(district);
              }}>
                <option value="">{locationState.districtLoading && locationState.selectedCity ? '正在加载...' : '选择区县'}</option>
                {locationState.districts.map(/* districtOptionRenderer 渲染高德区县行政区选项。 */ district => <option key={String(district.adcode)} value={String(district.adcode)}>{districtLabel(district)}</option>)}
              </select>
            </label>
          </div>

          <div className="rounded-2xl border border-sky-100 bg-gradient-to-br from-sky-50 to-white p-4 shadow-sm">
            <div className="mb-3 flex items-center gap-2">
              <div className="flex h-8 w-8 items-center justify-center rounded-xl bg-sky-600 text-white shadow-sm"><Search className="h-4 w-4" /></div>
              <div>
                <div className="text-sm font-extrabold text-gray-900">搜索具体位置</div>
                <div className="text-xs text-gray-500">例如：小区、街道、公司、商场或园区</div>
              </div>
            </div>
            <div className="flex flex-col gap-2 sm:flex-row">
              <input className="ios-input min-w-0 flex-1 rounded-xl bg-white px-4 py-3" value={locationState.keyword} disabled={!locationState.selectedDistrict || locationState.searchLoading} placeholder={locationState.selectedDistrict ? '请输入地点名称' : '请先选择区县'} onChange={/* keywordChangeAction 更新地点搜索词。 */ event => locationState.setKeyword(event.target.value)} onKeyDown={/* keywordKeyAction 支持按 Enter 搜索高德 POI。 */ event => {
                if (event.key === 'Enter') {
                  event.preventDefault();
                  void locationState.searchLocations();
                }
              }} />
              <button type="button" className="ios-btn-primary flex min-h-[46px] items-center justify-center gap-2 rounded-xl px-5 text-sm font-bold disabled:opacity-50" disabled={!locationState.selectedDistrict || !locationState.keyword.trim() || locationState.searchLoading} onClick={/* searchClickAction 提交区县内 POI 搜索。 */ () => void locationState.searchLocations()}>
                {locationState.searchLoading ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Search className="h-4 w-4" />}
                {locationState.searchLoading ? '搜索中...' : '搜索地点'}
              </button>
            </div>
          </div>

          {locationState.error && <div className="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm leading-6 text-rose-700">{locationState.error}</div>}

          <div className="space-y-2">
            <div className="flex items-center justify-between gap-3">
              <div className="text-sm font-extrabold text-gray-900">高德地点结果</div>
              {locationState.locations.length > 0 && <div className="text-xs font-bold text-gray-400">{locationState.locations.length} 个结果</div>}
            </div>
            {locationState.locations.length > 0 ? (
              <div className="max-h-64 space-y-2 overflow-y-auto pr-1">
                {locationState.locations.map(/* locationOptionRenderer 渲染可确认的高德 POI 候选。 */ location => {
                  // selected 标记当前候选是否已被用户选中。
                  const selected = locationState.selectedLocation?.poi_id === location.poi_id;
                  return <button type="button" key={`${location.poi_id}-${location.division_id}`} onClick={/* locationClickAction 选择当前高德 POI。 */ () => locationState.selectLocation(location)} className={`flex w-full items-center justify-between gap-3 rounded-xl border px-4 py-3 text-left transition-colors ${selected ? 'border-sky-400 bg-sky-50 shadow-sm' : 'border-gray-100 bg-white hover:border-sky-200 hover:bg-sky-50/50'}`}>
                    <span className="min-w-0">
                      <span className="flex items-center gap-2 text-sm font-extrabold text-gray-900">{selected && <CheckCircle2 className="h-4 w-4 shrink-0 text-sky-600" />}{location.poi_name}</span>
                      <span className="mt-1 block truncate text-xs text-gray-500">{locationLabel(location)}</span>
                    </span>
                    <ChevronRight className="h-4 w-4 shrink-0 text-gray-300" />
                  </button>;
                })}
              </div>
            ) : (
              <div className="rounded-xl border border-dashed border-gray-200 bg-gray-50 px-4 py-8 text-center text-sm text-gray-400">选择区县并搜索后，地点结果会显示在这里</div>
            )}
          </div>
        </div>

        <div className="modal-footer flex gap-3">
          <button type="button" onClick={onClose} className="flex-1 rounded-xl border border-gray-200 bg-white px-5 py-3.5 font-bold text-gray-700 transition-colors hover:bg-gray-50">取消</button>
          <button type="button" onClick={handleConfirm} disabled={!locationState.selectedLocation} className="ios-btn-primary flex flex-[1.5] items-center justify-center gap-2 rounded-xl px-5 py-3.5 font-bold disabled:cursor-not-allowed disabled:opacity-50"><CheckCircle2 className="h-4 w-4" />确认此发货地</button>
        </div>
      </div>
    </div>,
    document.body,
  );
};
