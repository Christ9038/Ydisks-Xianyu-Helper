# Bargain Orders and Delivery Flow

[中文](砍价订单与自动发货流程.zh-CN) | English

Automatic delivery is driven by Xianyu's **WebSocket system messages**. The order-status scanner is only a safety net for lost messages and never jumps ahead of a WebSocket stage. This page explains the four delivery paths, the order of actions for two-person bargain ("小刀") orders, and how failed runs are recovered.

## Two account switches

| Switch (Account Management) | Role |
| --- | --- |
| Auto delivery | Master switch for paid-order automation on the account. |
| Auto confirm shipment | After the card or template is sent successfully, call the normal "confirm shipment" API. |
| Auto skip group-buy ("自动免拼") | Independent switch, read only when the "bargain waiting" message arrives. |

## The four paths

| # | Scenario | Order of events |
| --- | --- | --- |
| 1 | Regular order | Payment message → read amount, quantity, spec → match the delivery rule → send card/template → confirm shipment if enabled. |
| 2 | Multi-spec order | Payment message → read the **complete** SKU combination → run only the rule matching that exact combination → send card → confirm shipment if enabled. Partial specs or guessed amounts are never used. |
| 3 | Bargain order, auto skip off | "Waiting for bargain" message is ignored → on "bargain succeeded, waiting for shipment" send card/template → confirm shipment if enabled. |
| 4 | Bargain order, auto skip on | "Waiting for bargain" message → call skip-group-buy **once** → wait for "bargain succeeded, waiting for shipment" → send card → confirm shipment if enabled. |

Hard rules:

- Nothing is sent and nothing is confirmed while the order is only at "waiting for bargain".
- Sending the card is always the last step before confirming shipment. Skip-group-buy is never a substitute for confirming shipment, and bargain orders still use the normal confirm API.
- Turning auto skip off does not block orders that have already reached "waiting for shipment".

## Safety gates

- A payment event without an order number is rejected; the system never guesses an order from the conversation.
- Purchase quantity must be known reliably before sending.
- A payment amount gate stops zero-amount orders (for example service-appointment orders) from being delivered by mistake.
- Orders whose item is not in the local item library are skipped silently instead of raising a manual-review alert.

## Fallback and recovery

- **Pending-ship catch-up** scans orders every minute. It triggers a delivery only when the order has been seen as pending shipment for at least 2 minutes with no payment run, and at most once every 10 minutes per order. It never calls skip-group-buy; a bargain order is only caught up after skip-group-buy success was recorded or the final "waiting for shipment" stage was received.
- **Resuming unfinished runs** only lets idempotent closing actions such as `confirm_shipment` run, because they never contact the buyer again.
- Before replaying any failed run the system re-checks that the run came from a WebSocket (or a legitimate catch-up), the order is still `pending_ship`, the order still belongs to the same account, and auto delivery is still on.
- Cancelled, completed or already-shipped orders cancel the old run. Missing order facts, mismatched identity or an uncertain external result stop automatic replay and send a **manual handling** notification.
- Both fallback chains can be switched off with `XIANYU_PENDING_SHIP_CATCHUP=0`; see [Configuration Reference](Configuration-Reference).

## Where to look when something stops

1. [Automated Delivery and Replies](Automated-Delivery-and-Replies) → the "needs manual handling" panel and the rule log.
2. [Orders](Orders) for the delivered-card record and manual shipment.
3. [Runtime Logs](Runtime-Logs) filtered by `automation`.
4. Subscribe the notification channel to the **manual handling** event category so these stops reach you.
