import { ArrowDown, ArrowUp, Plus, Trash2 } from 'lucide-react';
import React from 'react';
import type { ReviewCopyPlan, ReviewCopySource, ReviewCopyStyle, ReviewPlan } from '../reviewPlan';
import { reviewIntervalLimit, reviewPoolLimit } from '../reviewPlan';

/** 求评价三段消息编辑器的输入参数。 */
export interface ReviewPlanEditorProps {
  /** 当前编辑的三段消息模型。 */
  plan: ReviewPlan;
  /** 任一字段变化时回调完整的新模型；父组件负责写回规则动作。 */
  onChange: (plan: ReviewPlan) => void;
}

/** sourceOptions 是好评文案来源的选项及说明。 */
const sourceOptions: Array<{
  /** value 是提交给规则配置的来源取值。 */
  value: ReviewCopySource;
  /** label 是界面展示的来源名称。 */
  label: string;
}> = [
  { value: 'pool', label: '固定模板' },
  { value: 'ai', label: 'AI 生成' },
  { value: 'mixed', label: '混合' },
];

/** styleOptions 是 AI 评价风格的选项。 */
const styleOptions: Array<{
  /** value 是提交给规则配置的风格取值。 */
  value: ReviewCopyStyle;
  /** label 是界面展示的风格名称。 */
  label: string;
}> = [
  { value: 'sincere', label: '真诚自然' },
  { value: 'warm', label: '热情洋溢' },
  { value: 'concise', label: '简洁务实' },
];

/** clampNumber 把输入框的值转成不小于下限的整数，无法解析时返回回退值。 */
const clampNumber = (raw: string, min: number, fallback: number): number => {
  // parsed 是输入框解析出的数字。
  const parsed = Math.floor(Number(raw));
  return Number.isFinite(parsed) ? Math.max(min, parsed) : fallback;
};

/** 渲染求评价规则的三段消息：引导语、好评文案（模板池/AI/混合）、复制提示，以及消息间隔。 */
export const ReviewPlanEditor: React.FC<ReviewPlanEditorProps> = ({ plan, onChange }) => {
  // patchCopy 更新好评文案配置的局部字段。
  const patchCopy = (patch: Partial<ReviewCopyPlan>) => onChange({ ...plan, copy: { ...plan.copy, ...patch } });
  // setPoolItem 替换模板池中指定位置的内容。
  const setPoolItem = (index: number, value: string) => patchCopy({ pool: plan.copy.pool.map(/* 当前回调替换目标模板。 */ (item, itemIndex) => itemIndex === index ? value : item) });
  // movePoolItem 把模板上移或下移一位，越界时忽略。
  const movePoolItem = (index: number, offset: number) => {
    // target 是移动后的位置。
    const target = index + offset;
    if (target < 0 || target >= plan.copy.pool.length) return;
    // next 是调整顺序后的模板池副本。
    const next = plan.copy.pool.slice();
    [next[index], next[target]] = [next[target], next[index]];
    patchCopy({ pool: next });
  };
  // showPool 表示当前来源是否需要展示模板池；showAI 表示是否需要展示 AI 配置。
  const showPool = plan.copy.source !== 'ai';
  // showAI 在来源不是纯模板池时展示 AI 风格、字数与附加要求。
  const showAI = plan.copy.source !== 'pool';

  return (
    <div className="md:col-span-3 space-y-4" data-testid="review-plan-editor">
      <div className="rounded-2xl border border-gray-100 p-4">
        <label className="flex items-center gap-2 text-sm font-bold text-gray-700 mb-2">
          <input type="checkbox" checked={plan.lead.enabled} onChange={/* 当前回调切换引导语启用状态。 */ event => onChange({ ...plan, lead: { ...plan.lead, enabled: event.target.checked } })} />
          消息 1：引导语
        </label>
        <textarea
          aria-label="引导语"
          value={plan.lead.text}
          disabled={!plan.lead.enabled}
          onChange={/* 当前回调更新引导语正文。 */ event => onChange({ ...plan, lead: { ...plan.lead, text: event.target.value } })}
          className="w-full ios-input px-4 py-3 rounded-xl h-20 resize-none disabled:opacity-50"
        />
      </div>

      <div className="rounded-2xl border border-gray-100 p-4 space-y-3">
        <label className="flex items-center gap-2 text-sm font-bold text-gray-700">
          <input type="checkbox" checked={plan.copy.enabled} onChange={/* 当前回调切换好评文案启用状态。 */ event => patchCopy({ enabled: event.target.checked })} />
          消息 2：好评文案（帮买家写好评，买家直接复制）
        </label>
        {plan.copy.enabled && (
          <>
            <div className="flex flex-wrap gap-4" role="radiogroup" aria-label="好评文案来源">
              {sourceOptions.map(/* 当前回调渲染一个来源单选项。 */ option => (
                <label key={option.value} className="flex items-center gap-2 text-sm text-gray-700">
                  <input type="radio" name="review-copy-source" checked={plan.copy.source === option.value} onChange={/* 当前回调切换文案来源。 */ () => patchCopy({ source: option.value })} />
                  {option.label}
                </label>
              ))}
            </div>
            {showPool && (
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-bold text-gray-600">模板池（{plan.copy.pool.length}/{reviewPoolLimit}）</span>
                  <label className="flex items-center gap-2 text-xs text-gray-600">
                    <input type="checkbox" checked={plan.copy.random} onChange={/* 当前回调切换随机选取。 */ event => patchCopy({ random: event.target.checked })} />
                    随机选取（关闭则按顺序循环）
                  </label>
                </div>
                {plan.copy.pool.map(/* 当前回调渲染一条可编辑模板。 */ (item, index) => (
                  <div key={index} className="flex items-start gap-2">
                    <textarea aria-label={`好评模板 ${index + 1}`} value={item} onChange={/* 当前回调更新模板内容。 */ event => setPoolItem(index, event.target.value)} className="flex-1 ios-input px-3 py-2 rounded-xl h-16 resize-none" />
                    <button type="button" aria-label={`上移模板 ${index + 1}`} onClick={/* 当前回调上移模板。 */ () => movePoolItem(index, -1)} disabled={index === 0} className="w-9 h-9 flex items-center justify-center rounded-lg text-gray-500 hover:bg-gray-50 disabled:opacity-25"><ArrowUp className="w-4 h-4" /></button>
                    <button type="button" aria-label={`下移模板 ${index + 1}`} onClick={/* 当前回调下移模板。 */ () => movePoolItem(index, 1)} disabled={index === plan.copy.pool.length - 1} className="w-9 h-9 flex items-center justify-center rounded-lg text-gray-500 hover:bg-gray-50 disabled:opacity-25"><ArrowDown className="w-4 h-4" /></button>
                    <button type="button" aria-label={`删除模板 ${index + 1}`} onClick={/* 当前回调删除模板。 */ () => patchCopy({ pool: plan.copy.pool.filter(/* 当前回调保留其他模板。 */ (_, itemIndex) => itemIndex !== index) })} className="w-9 h-9 flex items-center justify-center rounded-lg text-red-500 hover:bg-red-50"><Trash2 className="w-4 h-4" /></button>
                  </div>
                ))}
                <button type="button" onClick={/* 当前回调追加一条空模板。 */ () => patchCopy({ pool: [...plan.copy.pool, ''] })} disabled={plan.copy.pool.length >= reviewPoolLimit} className="flex items-center gap-1 text-sm font-bold text-amber-600 disabled:opacity-40">
                  <Plus className="w-4 h-4" />添加模板
                </button>
              </div>
            )}
            {showAI && (
              <div className="grid grid-cols-1 md:grid-cols-4 gap-3">
                <div>
                  <label className="block text-xs font-bold text-gray-600 mb-1">评价风格</label>
                  <select aria-label="AI 评价风格" value={plan.copy.aiStyle} onChange={/* 当前回调切换评价风格。 */ event => patchCopy({ aiStyle: event.target.value as ReviewCopyStyle })} className="w-full ios-input px-3 py-2 rounded-xl">
                    {styleOptions.map(/* 当前回调渲染风格选项。 */ option => <option key={option.value} value={option.value}>{option.label}</option>)}
                  </select>
                </div>
                <div>
                  <label className="block text-xs font-bold text-gray-600 mb-1">最少字数</label>
                  <input aria-label="AI 最少字数" type="number" min="5" max="200" value={plan.copy.aiMinChars} onChange={/* 当前回调更新最少字数。 */ event => patchCopy({ aiMinChars: clampNumber(event.target.value, 0, 20) })} className="w-full ios-input px-3 py-2 rounded-xl" />
                </div>
                <div>
                  <label className="block text-xs font-bold text-gray-600 mb-1">最多字数</label>
                  <input aria-label="AI 最多字数" type="number" min="5" max="200" value={plan.copy.aiMaxChars} onChange={/* 当前回调更新最多字数。 */ event => patchCopy({ aiMaxChars: clampNumber(event.target.value, 0, 50) })} className="w-full ios-input px-3 py-2 rounded-xl" />
                </div>
                {plan.copy.source === 'mixed' && (
                  <div>
                    <label className="block text-xs font-bold text-gray-600 mb-1">AI 使用比例（%）</label>
                    <input aria-label="AI 使用比例" type="number" min="0" max="100" value={plan.copy.aiRatio} onChange={/* 当前回调更新 AI 比例。 */ event => patchCopy({ aiRatio: clampNumber(event.target.value, 0, 30) })} className="w-full ios-input px-3 py-2 rounded-xl" />
                  </div>
                )}
                <div className="md:col-span-4">
                  <label className="block text-xs font-bold text-gray-600 mb-1">附加要求（可选）</label>
                  <input aria-label="AI 附加要求" value={plan.copy.aiPrompt} onChange={/* 当前回调更新附加要求。 */ event => patchCopy({ aiPrompt: event.target.value })} className="w-full ios-input px-3 py-2 rounded-xl" placeholder="例如：提到包装完好、发货快" />
                  <p className="text-xs text-gray-500 mt-1">AI 使用「设置」页中的全局 AI 配置，只会收到商品标题，不会收到买家和订单信息；AI 失败时混合模式回退到模板池，其余情况本次求评价会重试，不会只发出半套消息。</p>
                </div>
              </div>
            )}
          </>
        )}
      </div>

      <div className="rounded-2xl border border-gray-100 p-4">
        <label className="flex items-center gap-2 text-sm font-bold text-gray-700 mb-2">
          <input type="checkbox" checked={plan.tail.enabled} onChange={/* 当前回调切换复制提示启用状态。 */ event => onChange({ ...plan, tail: { ...plan.tail, enabled: event.target.checked } })} />
          消息 3：复制提示
        </label>
        <textarea
          aria-label="复制提示"
          value={plan.tail.text}
          disabled={!plan.tail.enabled}
          onChange={/* 当前回调更新复制提示正文。 */ event => onChange({ ...plan, tail: { ...plan.tail, text: event.target.value } })}
          className="w-full ios-input px-4 py-3 rounded-xl h-20 resize-none disabled:opacity-50"
        />
      </div>

      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className="block text-sm font-bold text-gray-700 mb-2">消息间隔最小秒数</label>
          <input aria-label="消息间隔最小秒数" type="number" min="0" max={reviewIntervalLimit} value={plan.intervalMin} onChange={/* 当前回调更新最小间隔，并保证最大值不小于它。 */ event => {
            // min 是新的最小间隔。
            const min = clampNumber(event.target.value, 0, 5);
            onChange({ ...plan, intervalMin: min, intervalMax: Math.max(plan.intervalMax, min) });
          }} className="w-full ios-input px-4 py-3 rounded-xl" />
        </div>
        <div>
          <label className="block text-sm font-bold text-gray-700 mb-2">消息间隔最大秒数</label>
          <input aria-label="消息间隔最大秒数" type="number" min="0" max={reviewIntervalLimit} value={plan.intervalMax} onChange={/* 当前回调更新最大间隔。 */ event => onChange({ ...plan, intervalMax: clampNumber(event.target.value, 0, 10) })} className="w-full ios-input px-4 py-3 rounded-xl" />
        </div>
        <p className="col-span-2 text-xs text-gray-500">相邻两条消息之间会随机等待「最小～最大」秒，模拟人工节奏；第一条消息到期后立即发送。</p>
      </div>
    </div>
  );
};

export default ReviewPlanEditor;
