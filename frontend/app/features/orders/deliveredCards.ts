import { useEffect,useState } from 'react';
import { getOrderDeliveredCards } from './api';
import type { OrderDeliveredCard } from './models';

// DeliveredCardsState 描述订单已发卡密的加载状态。
export interface DeliveredCardsState {
  // cards 是已加载的发货记录；加载完成前为 null，以区分“尚未加载”和“没有记录”。
  cards: OrderDeliveredCard[] | null;
  // loading 表示请求是否仍在进行。
  loading: boolean;
  // error 是加载失败的提示，成功后为 null。
  error: string | null;
  // reload 重新请求当前订单的发货记录。
  reload: () => void;
}

// useOrderDeliveredCards 加载订单的已发卡密。调用方应以订单号作为 key 挂载，使用的组件卸载时请求被取消，
// 重载或订单变化产生的旧响应不会覆盖当前结果。
export const useOrderDeliveredCards = (orderId: string): DeliveredCardsState => {
  // cards 保存最近一次成功加载的发货记录。
  const [cards, setCards] = useState<OrderDeliveredCard[] | null>(null);
  // loading 表示请求进行中。
  const [loading, setLoading] = useState(true);
  // error 保存最近一次失败提示。
  const [error, setError] = useState<string | null>(null);
  // reloadToken 递增时重新发起请求。
  const [reloadToken, setReloadToken] = useState(0);

  useEffect(
    // 加载副作用：订单或重载令牌变化时发起请求，清理函数取消尚未完成的旧请求。
    () => {
      // controller 绑定本次请求的生命周期。
      const controller = new AbortController();
      setLoading(true);
      setError(null);
      getOrderDeliveredCards(orderId, { signal: controller.signal })
        .then(/* result 是本次请求返回的发货记录，请求已取消时丢弃。 */ result => {
          if (!controller.signal.aborted) setCards(result);
        })
        .catch(/* failure 是本次请求失败的原因，请求已取消时不展示。 */ failure => {
          if (!controller.signal.aborted) setError(failure instanceof Error && failure.message ? failure.message : '加载已发卡密失败');
        })
        .finally(/* 仅当前请求结束时才关闭加载状态。 */ () => {
          if (!controller.signal.aborted) setLoading(false);
        });
      // 清理函数：切换订单或卸载时中止未完成的请求。
      return /* 清理函数：中止未完成的请求。 */ () => controller.abort();
    },
    [orderId, reloadToken],
  );

  // reload 递增重新请求令牌以触发重新加载；previous 是上一个令牌值。
  return { cards, loading, error, reload: /* 递增令牌触发重新请求。 */ () => setReloadToken(/* previous 是上一个令牌值。 */ previous => previous + 1) };
};
