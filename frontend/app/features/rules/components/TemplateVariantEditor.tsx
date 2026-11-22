import React from 'react';
import SearchableSelect from '../../../../shared/ui/SearchableSelect';
import type { Card, DeliveryTemplate, ShippingVariant } from '../types';
import { isDeliveryCardReady } from '../utils';

/** 模板变体编辑器的输入参数。 */
export interface TemplateVariantEditorProps {
  /** 当前变体所在的编辑器行号。 */
  index: number;
  /** 当前正在编辑的变体。 */
  variant: ShippingVariant;
  /** 当前用户可用的卡密库存。 */
  cards: Card[];
  /** 当前用户可用的发货模板。 */
  deliveryTemplates: DeliveryTemplate[];
  /** 更新当前变体字段。 */
  updateVariant: (index: number, patch: Partial<ShippingVariant>) => void;
  /** 请求为指定模板变量就地新建卡密；缺省时不显示“新建卡密”入口。 */
  onCreateCard?: (variableKey: string) => void;
}

/** 渲染模板选择和模板变量卡密绑定，按需加载以控制规则页首屏分片大小。 */
const TemplateVariantEditor: React.FC<TemplateVariantEditorProps> = ({ index, variant, cards, deliveryTemplates, updateVariant, onCreateCard }) => {
  // template 保存当前选择的发货模板。
  const template = deliveryTemplates.find(/* templateCandidate 是待匹配的发货模板。 */ templateCandidate => templateCandidate.id === variant.delivery_template_id);
  // availableCards 保存模板执行器支持的启用文本、批量或已就绪 API 卡密库存；图片卡券仍不支持嵌入文本模板。
  const availableCards = cards.filter(/* card 是当前待筛选的卡密库存。 */ card => isDeliveryCardReady(card) && card.type !== 'image');
  // customKeys 保存当前模板要求规则提供的自定义变量键。
  const customKeys = template?.custom_keys || [];
  return (
    <>
      <div className="min-w-0">
        <label className="block text-xs font-bold text-gray-600 mb-2">发货模板</label>
        <select
          value={variant.delivery_template_id || ''}
          onChange={/* callback 选择发货模板并初始化变量绑定。 */ event => {
            // templateID 保存用户选中的发货模板标识。
            const templateID = Number(event.target.value);
            // selectedTemplate 保存用户选中的发货模板摘要。
            const selectedTemplate = deliveryTemplates.find(/* templateCandidate 是待匹配的发货模板。 */ templateCandidate => templateCandidate.id === templateID);
            // customValues 保存模板要求的自定义变量键值初始表。
            const customValues = Object.fromEntries((selectedTemplate?.custom_keys || []).map(/* key 是需要初始化的自定义变量键。 */ key => [key, '']));
            updateVariant(index, { delivery_template_id: templateID, template_bindings: (selectedTemplate?.keys || []).map(/* key 是需要初始化绑定的变量键。 */ key => ({ variable_key: key, card_id: 0, delivery_count: 1 })), custom_variables: customValues });
          }}
          className="w-full ios-input px-3 py-2.5 rounded-lg"
        >
          <option value="">请选择发货模板</option>
          {deliveryTemplates.filter(/* templateCandidate 是待筛选的启用模板或当前既有引用。 */ templateCandidate => templateCandidate.enabled || templateCandidate.id === variant.delivery_template_id).map(/* templateCandidate 是待渲染的模板选项。 */ templateCandidate => <option key={templateCandidate.id} value={templateCandidate.id} disabled={!templateCandidate.enabled}>{templateCandidate.name}{templateCandidate.enabled ? '' : '（已停用，仅保留现有引用）'}</option>)}
        </select>
      </div>
      {customKeys.length > 0 && <div className="min-w-0 md:col-span-full rounded-xl bg-amber-50 p-3 space-y-2">
        <div>
          <p className="text-xs font-bold text-amber-800">发货规则自定义变量</p>
          <p className="mt-1 text-xs leading-5 text-amber-700">按模板中的 key 填写字符串；例如 <code>{'{{custom.vip}}'}</code> 对应下方 key 为 <code>vip</code> 的值。</p>
        </div>
        {customKeys.map(/* key 是模板引用的自定义变量键。 */ key => <label key={key} className="grid min-w-0 grid-cols-[minmax(7rem,0.45fr)_minmax(0,1fr)] items-center gap-2 text-xs font-semibold text-amber-900">
          <code className="break-all">{`{{custom.${key}}}`}</code>
          <input value={variant.custom_variables?.[key] || ''} onChange={/* callback 更新指定键名的自定义字符串。 */ event => {
            // nextValues 保存更新后的自定义变量键值表副本。
            const nextValues = { ...(variant.custom_variables || {}) };
            nextValues[key] = event.target.value;
            updateVariant(index, { custom_variables: nextValues });
          }} className="ios-input rounded-lg px-3 py-2" placeholder={`填写 ${key} 对应的字符串`} />
        </label>)}
      </div>}
      <div className="min-w-0 md:col-span-full rounded-xl bg-sky-50 p-3 space-y-2">
        <p className="text-xs font-bold text-sky-700">模板变量绑定</p>
        {(template?.keys || []).map(/* key 是当前模板要求绑定的变量键。 */ key => {
          // binding 保存当前变量已有的卡密绑定。
          const binding = variant.template_bindings?.find(/* bindingCandidate 是待匹配的变量绑定。 */ bindingCandidate => bindingCandidate.variable_key === key);
          return (
            <div key={key} className="grid min-w-0 grid-cols-[minmax(0,1fr)_minmax(0,1.5fr)_90px] gap-2 items-center">
              <code className="min-w-0 break-all text-xs text-sky-800">{`{{cards.${key}}}`}</code>
              <SearchableSelect
                options={availableCards.map(/* card 转换为可搜索选项，仅保留主键与名称。 */ card => ({ value: card.id, label: card.name }))}
                value={binding?.card_id}
                onChange={/* cardID 是用户为该变量选中的卡密库存主键，0 表示清空。 */ cardID => updateVariant(index, { template_bindings: (variant.template_bindings || []).map(/* bindingCandidate 是待处理的变量绑定。 */ bindingCandidate => bindingCandidate.variable_key === key ? { ...bindingCandidate, card_id: cardID } : bindingCandidate) })}
                placeholder="请选择卡密库存"
                ariaLabel={`${key} 卡密库存`}
                className="ios-input min-w-0 w-full px-2 py-2 rounded-lg text-xs"
              />
              <input type="number" min="1" value={binding?.delivery_count || 1} onChange={/* callback 更新变量取货数量。 */ event => updateVariant(index, { template_bindings: (variant.template_bindings || []).map(/* bindingCandidate 是待处理的变量绑定。 */ bindingCandidate => bindingCandidate.variable_key === key ? { ...bindingCandidate, delivery_count: Math.max(1, Number(event.target.value) || 1) } : bindingCandidate) })} className="ios-input px-2 py-2 rounded-lg text-xs" aria-label={`${key} 每件份数`} />
              {onCreateCard && <button type="button" aria-label={`为 ${key} 新建卡密库存`} className="col-span-full justify-self-start text-xs font-bold text-sky-700 hover:underline" onClick={/* 新建并回填到该变量绑定。 */ () => onCreateCard(key)}>+ 新建卡密</button>}
            </div>
          );
        })}
      </div>
    </>
  );
};

export default TemplateVariantEditor;
