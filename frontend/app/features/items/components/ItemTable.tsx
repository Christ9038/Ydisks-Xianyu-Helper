import { CheckCircle2,CircleDashed,Edit,Link2,Trash2 } from 'lucide-react';
import React from 'react';
import type { Item,ItemAIOverride,ShippingRule } from '../api';
import { countTriggerRules,TRADE_AUTOMATION_TRIGGERS } from '../ruleCoverage';

/** ItemTableProps 描述商品列表视图需要的数据与回调。 */
export interface ItemTableProps {
  /** items 是当前页要展示的商品行。 */
  items: Item[];
  /** rules 是全部自动化规则，用于计算每个商品的各类规则覆盖情况。 */
  rules: ShippingRule[];
  /** accountNickname 把账号标识转换为展示昵称。 */
  accountNickname: (cookieId: string) => string;
  /** formatPrice 把商品价格转换为展示文本。 */
  formatPrice: (price?: string) => string;
  /** aiStatusText 是商品级 AI 三态对应的展示文本。 */
  aiStatusText: Record<ItemAIOverride, string>;
  /** onConfigureDelivery 打开该商品的发货规则配置。 */
  onConfigureDelivery: (item: Item) => void;
  /** onOpenAISettings 打开该商品的 AI 客服配置。 */
  onOpenAISettings: (item: Item) => void;
  /** onEdit 打开商品编辑弹窗。 */
  onEdit: (item: Item) => void;
  /** onDelete 删除商品。 */
  onDelete: (item: Item) => void;
  /** emptyText 是没有商品时的提示文案。 */
  emptyText: string;
}

/** ItemTable 以表格形式展示商品，并逐列标出四类交易自动化规则的配置数量。 */
const ItemTable: React.FC<ItemTableProps> = ({ items, rules, accountNickname, formatPrice, aiStatusText, onConfigureDelivery, onOpenAISettings, onEdit, onDelete, emptyText }) => {
  if (items.length === 0) return <div className="py-20 text-center text-gray-400">{emptyText}</div>;
  return (
    <div className="ios-card overflow-x-auto rounded-2xl">
      <table className="w-full min-w-[960px] text-left text-xs">
        <thead className="bg-gray-50 text-[11px] font-extrabold text-gray-500">
          <tr>
            <th className="px-4 py-3">商品</th>
            <th className="px-4 py-3">账号</th>
            <th className="px-4 py-3">价格</th>
            {TRADE_AUTOMATION_TRIGGERS.map(/* entry 是一类交易自动化触发器列。 */ entry => <th key={entry.trigger} className="px-4 py-3">{entry.label}</th>)}
            <th className="px-4 py-3">AI 客服</th>
            <th className="px-4 py-3 text-right">操作</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-100">
          {items.map(/* item 是当前行对应的商品。 */ item => (
            <tr key={`${item.cookie_id}-${item.item_id}`} className="hover:bg-gray-50/60">
              <td className="max-w-[280px] px-4 py-3">
                <div className="line-clamp-2 font-bold text-gray-900">{item.item_title}</div>
                <div className="mt-1 select-all text-[10px] text-gray-400">ID: {item.item_id}</div>
              </td>
              <td className="px-4 py-3 text-gray-600">{accountNickname(item.cookie_id)}</td>
              <td className="px-4 py-3 text-gray-700">{formatPrice(item.item_price)}</td>
              {TRADE_AUTOMATION_TRIGGERS.map(/* entry 是一类交易自动化触发器列。 */ entry => {
                // ruleCount 是该商品在此触发器下生效的规则条数，0 表示未配置。
                const ruleCount = countTriggerRules(item, entry.trigger, rules);
                return (
                  <td key={entry.trigger} className="px-4 py-3">
                    <span className={`inline-flex items-center gap-1 font-bold ${ruleCount > 0 ? 'text-emerald-600' : 'text-amber-600'}`}>
                      {ruleCount > 0 ? <CheckCircle2 className="h-3 w-3" /> : <CircleDashed className="h-3 w-3" />}
                      {ruleCount > 0 ? `${ruleCount} 条` : '未配置'}
                    </span>
                  </td>
                );
              })}
              <td className="px-4 py-3">
                <button type="button" onClick={/* 点击打开该商品的 AI 客服配置。 */ () => onOpenAISettings(item)} className="font-bold text-gray-600 hover:text-brand" aria-label={`配置商品 AI 客服：${item.item_title || item.item_id}`}>
                  {aiStatusText[item.ai_override || 'inherit']}
                </button>
              </td>
              <td className="px-4 py-3">
                <div className="flex justify-end gap-1.5">
                  <button type="button" onClick={/* 点击打开该商品的发货规则配置。 */ () => onConfigureDelivery(item)} className="inline-flex items-center gap-1 rounded-lg bg-contrast px-2.5 py-1.5 font-bold text-on-contrast hover:bg-contrast/90"><Link2 className="h-3 w-3" />发货规则</button>
                  <button type="button" onClick={/* 点击编辑商品。 */ () => onEdit(item)} className="rounded-lg bg-gray-100 p-1.5 text-gray-600 hover:bg-brand hover:text-white" title="编辑" aria-label="编辑"><Edit className="h-3.5 w-3.5" /></button>
                  <button type="button" onClick={/* 点击删除商品。 */ () => onDelete(item)} className="rounded-lg bg-gray-100 p-1.5 text-red-500 hover:bg-red-100" title="删除" aria-label="删除"><Trash2 className="h-3.5 w-3.5" /></button>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
};

export default ItemTable;
