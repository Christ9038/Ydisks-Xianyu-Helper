import { useEffect, useRef, useState } from 'react';
import { itemErrorMessage, recommendPublishCategory } from '../api';
import { categoryCandidatesOf } from '../batchState';
import type { PublishCategory } from '../types';
import { CategoryCandidatePicker } from './CategoryCandidatePicker';

/** PublishCategoryFieldProps 描述类目选择字段的受控输入。 */
export interface PublishCategoryFieldProps {
  /** cookieId 是用于调用平台类目推荐的账号；为空时不能获取类目。 */
  cookieId: string;
  /** value 是当前选中的类目；为空表示发布时由平台自动识别。 */
  value: PublishCategory | null;
  /** onChange 在推荐、改选候选或清除后回传完整类目（含频道类目 ID）。 */
  onChange: (category: PublishCategory | null) => void;
}

/**
 * PublishCategoryField 复用商品管理发布的“关键词 → 平台类目推荐 → 候选改选”流程，
 * 保证类目 ID、名称和频道类目 ID 同时来自平台，而不是手填。
 * 请求由 AbortController 约束，重复触发或卸载时取消旧请求，晚到响应不会写回。
 */
export const PublishCategoryField = ({ cookieId, value, onChange }: PublishCategoryFieldProps) => {
  // keyword 是类目关键词输入。
  const [keyword, setKeyword] = useState('');
  // candidates 是平台返回的候选类目，首项是主推荐。
  const [candidates, setCandidates] = useState<PublishCategory[]>([]);
  // loading 表示推荐请求进行中。
  const [loading, setLoading] = useState(false);
  // error 是最近一次推荐失败提示。
  const [error, setError] = useState('');
  // controllerRef 保存进行中的推荐请求取消器。
  const controllerRef = useRef<AbortController | null>(null);
  useEffect(/* 卸载时取消未完成的推荐请求。 */ () => /* 卸载清理。 */ () => controllerRef.current?.abort(), []);

  /** recommend 按关键词请求平台推荐类目并选中主推荐。 */
  const recommend = async (): Promise<void> => {
    // trimmed 是去除首尾空白的关键词。
    const trimmed = keyword.trim();
    if (!cookieId) { setError('请先选择用于获取类目的账号'); return; }
    if (!trimmed) { setError('请输入类目关键词'); return; }
    controllerRef.current?.abort();
    // controller 控制本次请求。
    const controller = new AbortController();
    controllerRef.current = controller;
    setLoading(true);
    setError('');
    try {
      // result 是推荐响应。
      const result = await recommendPublishCategory(cookieId, trimmed, { signal: controller.signal });
      if (controller.signal.aborted) return;
      setCandidates(categoryCandidatesOf(result));
      onChange(result.category);
    } catch (failure /* failure 是推荐失败原因。 */) {
      if (!controller.signal.aborted) setError(itemErrorMessage(failure, '没有匹配到类目，请换一个更具体的关键词'));
    } finally {
      if (!controller.signal.aborted) setLoading(false);
    }
  };

  return (
    <div className="space-y-2">
      <div className="flex gap-2">
        <input aria-label="类目关键词" className="ios-input w-full rounded-xl bg-white px-4 py-3" placeholder="例如：课程资料、电子书" value={keyword} onChange={/* 修改关键词。 */ event => setKeyword(event.target.value)} onKeyDown={/* 回车提交推荐，避免触发外层表单。 */ event => { if (event.key === 'Enter') { event.preventDefault(); void recommend(); } }} />
        <button type="button" disabled={loading || !cookieId || !keyword.trim()} onClick={/* 请求平台推荐类目。 */ () => void recommend()} className="shrink-0 rounded-xl bg-amber-500 px-4 py-3 text-sm font-bold text-white hover:bg-amber-600 disabled:opacity-50">{loading ? '匹配中...' : '获取类目'}</button>
      </div>
      {error && <p role="alert" className="text-xs text-red-600">{error}</p>}
      <CategoryCandidatePicker candidates={candidates} selectedCatId={value?.cat_id} onSelect={onChange} />
      {value && (
        <div className="flex items-center justify-between gap-3 rounded-xl bg-amber-50 px-3 py-2 text-sm">
          <div><span className="font-bold text-gray-900">{value.cat_name}</span><span className="ml-2 font-mono text-xs text-gray-500">{value.cat_id} · 频道 {value.channel_cat_id}</span></div>
          <button type="button" className="text-xs font-bold text-gray-500 hover:text-red-600" onClick={/* 清除后发布时自动识别。 */ () => { setCandidates([]); onChange(null); }}>清除，使用自动识别</button>
        </div>
      )}
    </div>
  );
};

export default PublishCategoryField;
