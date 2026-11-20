# Orders Guide

[中文](订单管理.zh-CN) | English

## View and filter orders

1. Open Orders in the sidebar (the page title is “订单中心”, Order Center).
2. Use status tabs for All, Processing, Shipped, Pending Shipment, Completed, Cancelled, or Refunding.
3. Filter by a specific Xianyu account.
4. Search in the search box (placeholder “搜索订单号/商品/买家...”). Besides order ID, product, and buyer, it also matches product ID, receiver name, and phone number.
5. The list shows 20 orders per page. The icons on each row are, in order: view on Xianyu (opens the Xianyu order page), view details, edit order, sync order, and delete order; a refresh-list button sits at the top right. Pending-shipment orders also show Deliver Now, and unpaid (Processing) orders also show Change Price. Orders whose paid amount has not been fetched yet show “待获取” (pending).

## Sync orders

Click Sync All Orders to fetch Xianyu orders. It reads the Xianyu sold-order list for the account chosen in the account drop-down (all of the current user’s accounts when none is chosen); the status tabs do not narrow it. Bulk sync does not read each order’s details; information the list does not provide, such as the specification, is filled in by Sync Order or on demand when automatic delivery is prepared. After a complete successful sync, local orders that no longer exist on Xianyu are marked deleted. Review the message (for example “订单列表同步完成，发现 N 个新订单”, with “恢复 X 单，修正 Y 单” appended when orders were restored or corrected) and any partial failures — the message lists the failed accounts or orders with reasons; handle them by checking account login state, network, and order data. The page waits about 90 seconds; after that it reports that the sync is still running in the background, already-synced orders are shown, and you should refresh later.

Use Sync Order on one order to fetch it again through the order-detail interface (quantity, specification, amount, status, and so on). Sync does not replace paid-delivery rules; rules still match by account, product, and specification.

## Manual delivery

Only orders in Pending Shipment status can be shipped manually (Deliver Now is on the list row and in the order details). Click it and choose one method:

- **Change Xianyu shipping status only:** sends no card and deducts no inventory; it only marks the Xianyu order as shipped. Use this when delivery happened elsewhere and the platform status was forgotten.
- **Full delivery (match and send cards):** matches automation, obtains cards, sends them to the buyer, and changes the shipping status; this confirm-shipment is not limited by the account’s auto-confirm-shipment switch. The order’s account must be running online, not paused, and enabled, and the order needs a product ID, chat_id, and buyer ID. Use it only when no content has been sent and the status has not been changed.

If you are unsure whether a card was already sent, inspect chat history, inventory changes, and automation exceptions before choosing full delivery. If the order already has a successful delivery-content snapshot but the Xianyu status was not synced, the system tells you to choose “change Xianyu shipping status only”.

## Changing the price of an unpaid order

Only orders the buyer has not yet paid for (status Processing in the list) can be repriced. Click **改价** (Change Price) on the row, enter the new **order total in yuan**, and confirm; the app then calls Xianyu's seller price-change endpoint.

- The amount must be greater than 0 with at most two decimals (for example `8.8` or `8.80`; a leading `¥` is accepted). Shipping is fixed at 0; it cannot be changed separately.
- The order must belong to one of your accounts. Paid, shipped, cancelled, and other non-unpaid orders are refused with "仅未付款订单可以改价" (only unpaid orders can be repriced). The local status can lag behind Xianyu: if the buyer just paid and the list has not synced yet, the platform rejects the change, the dialog shows the platform's reason, and the local amount is left unchanged.
- A submitted price change cannot be undone from the page; reprice the same order again to adjust further. Each click on 确认改价 (Confirm) sends exactly one platform request, failures are never retried automatically, and the page blocks duplicate submits until the request finishes.
- On success the order amount is updated to the new price and the list refreshes. If Xianyu accepted the change but writing the local amount failed, the dialog says "请勿重复改价，同步该订单即可校正" (do not reprice again; sync the order to correct it) — use the per-order Sync button to pull the real amount back from Xianyu.
- Repricing does not trigger automatic delivery, bargain-free-group, or confirm-shipment; after the buyer pays, delivery is still driven by the original WebSocket payment message. If the account login is expired or risk control interrupts, the change fails with a reason; restore the account login first, then retry.

The endpoint is `POST /api/v1/orders/{order_id}/adjust-price` with body `{"price": "8.80"}`.

## Edit and delete

Edit Order can correct status (Processing, Pending Shipment, Shipped, Completed, Cancelled, Refunding), buyer ID, paid amount, receiver name, phone number, shipping address, and product title. Order IDs cannot be changed in the page. Deleting asks for confirmation and is a logical delete (order history is kept for automation audit); before deleting, confirm the record is not needed by automation, statistics, or after-sales handling.

## Delivered cards

Order details include a **Delivered cards** section listing the cards sent for that order (from the card sales ledger), shown in plain text with a copy button; records whose delivery result is uncertain are tagged “待核对” (to verify). Content is read on demand and returned with `no-store` caching; only the owner can see it. With no records it shows “暂无自动发货卡密记录” (orders from before the upgrade, manual delivery, or non-card delivery produce no records — the UI hint text). See [Card Inventory](Card-Inventory).
