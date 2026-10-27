import { Copy } from 'lucide-react';
import React,{ useState } from 'react';
import { formatLocalDateTime } from '../../../../dateTime';
import { useOrderDeliveredCards } from '../deliveredCards';

// DeliveredCardsSectionProps 描述已发卡密区块需要的订单号。
export interface DeliveredCardsSectionProps {
  // orderId 是被查看的订单号；组件以它作为 key 挂载，切换订单会重新加载。
  orderId: string;
}

// DeliveredCardsSection 在订单详情中展示该订单自动发货时发出的卡密，明文显示并支持复制。
export const DeliveredCardsSection: React.FC<DeliveredCardsSectionProps> = ({ orderId }) => {
  // delivered 是该订单已发卡密的加载状态。
  const delivered = useOrderDeliveredCards(orderId);
  // copiedId 是刚复制成功的记录标识，用于短暂显示“已复制”；属于瞬时 UI 状态。
  const [copiedId, setCopiedId] = useState<number | null>(null);

  // copyContent 把卡密写入剪贴板；剪贴板不可用时静默忽略，内容仍可手动选中复制。
  const copyContent = async (id: number, content: string) => {
    try {
      await navigator.clipboard.writeText(content);
      setCopiedId(id);
    } catch {
      setCopiedId(null);
    }
  };

  return (
    <div className="space-y-4">
      <h4 className="text-lg font-bold text-gray-800">已发卡密</h4>
      {delivered.loading && <p role="status" className="text-sm text-gray-500">加载中…</p>}
      {delivered.error && (
        <div role="alert" className="text-sm text-red-600">
          {delivered.error} <button type="button" className="underline" onClick={delivered.reload}>重试</button>
        </div>
      )}
      {delivered.cards && delivered.cards.length === 0 && !delivered.loading && (
        <p className="text-sm text-gray-400">暂无自动发货卡密记录（升级前的订单、人工发货或非卡密发货不会产生记录）。</p>
      )}
      {delivered.cards && delivered.cards.length > 0 && (
        <ul className="space-y-2">
          {delivered.cards.map(/* card 是该订单发出的一条卡密。 */ card => (
            <li key={card.id} className="flex items-start justify-between gap-3 rounded-xl bg-gray-50 p-3">
              <div className="min-w-0">
                <div className="break-all font-mono text-sm font-bold text-gray-900">{card.content}</div>
                <div className="mt-1 text-xs text-gray-500">
                  {card.cardName} · {formatLocalDateTime(card.deliveredAtMs)}
                  {card.status === 'reserved' && <span className="ml-2 rounded-md bg-amber-50 px-2 py-0.5 font-bold text-amber-600">待核对</span>}
                </div>
              </div>
              <button type="button" onClick={/* 点击复制该条卡密。 */ () => void copyContent(card.id, card.content)} aria-label={`复制卡密 ${card.content}`}
                className="shrink-0 rounded-lg p-1.5 text-gray-400 hover:bg-gray-100 hover:text-black">
                {copiedId === card.id ? <span className="text-xs font-bold text-green-600">已复制</span> : <Copy className="h-4 w-4" />}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};
