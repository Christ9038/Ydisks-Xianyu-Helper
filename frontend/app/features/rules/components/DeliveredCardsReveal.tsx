import { Eye,EyeOff } from 'lucide-react';
import React from 'react';
import { useDeliveredCardsReveal } from '../auditLog';

// DeliveredCardsRevealProps 描述按需查看已发卡密所需的订单号。
export interface DeliveredCardsRevealProps {
  // orderId 是要查看已发卡密的订单号；切换订单会清空已展示的明文。
  orderId: string;
}

// DeliveredCardsReveal 默认遮罩已发卡密，用户点击后才读取并显示明文，再次点击或离开即清空。
const DeliveredCardsReveal: React.FC<DeliveredCardsRevealProps> = ({ orderId }) => {
  // reveal 保存按需读取状态与操作。
  const reveal = useDeliveredCardsReveal(orderId);
  return (
    <div className="rounded-2xl border border-gray-100 bg-gray-50 p-4 space-y-3">
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="text-sm font-bold text-gray-900">已发卡密</p>
          <p className="text-xs text-gray-500">卡密正文属于敏感内容，默认遮罩，仅在你主动查看时读取。</p>
        </div>
        {reveal.cards === null ? (
          <button type="button" onClick={reveal.reveal} disabled={reveal.loading} className="inline-flex items-center gap-1.5 px-3 py-2 rounded-xl bg-white text-sm font-bold text-gray-700 hover:bg-gray-100 disabled:opacity-50">
            <Eye className="w-4 h-4" />
            {reveal.loading ? '读取中…' : '查看卡密'}
          </button>
        ) : (
          <button type="button" onClick={reveal.hide} className="inline-flex items-center gap-1.5 px-3 py-2 rounded-xl bg-white text-sm font-bold text-gray-700 hover:bg-gray-100">
            <EyeOff className="w-4 h-4" />
            隐藏
          </button>
        )}
      </div>
      {reveal.error ? <p className="text-sm text-red-600">{reveal.error}</p> : null}
      {reveal.cards === null ? (
        <p className="font-mono text-sm tracking-widest text-gray-400">••••••••••••</p>
      ) : reveal.cards.length === 0 ? (
        <p className="text-sm text-gray-500">该订单没有可查看的发卡记录。</p>
      ) : (
        <ul className="space-y-2">
          {reveal.cards.map(/* card 是一条已发卡密记录。 */ card => (
            <li key={card.id} className="rounded-xl bg-white p-3 text-sm">
              <p className="text-xs text-gray-500">{card.cardName}</p>
              <p className="font-mono break-all text-gray-900">{card.content}</p>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};

export default DeliveredCardsReveal;
