# Orders Guide

[中文](订单管理.zh-CN) | English

## View and filter orders

1. Open Orders.
2. Use status tabs for All, Processing, Pending Shipment, Shipped, Completed, Cancelled, or Refunding.
3. Filter by a specific Xianyu account.
4. Search by order ID, product, or buyer keyword.
5. Open an order for details. The list supports refresh, single-order sync, edit, and delete.

## Sync orders

Click Sync All Orders to fetch Xianyu orders. Review the result and any partial failures; handle them by checking account login state, network, and order data.

Use Sync Order on one order to fetch it again. Sync does not replace paid-delivery rules; rules still match by account, product, and specification.

## Manual delivery

For a pending-shipment order, click Deliver Now and choose one method:

- **Change Xianyu shipping status only:** sends no card and deducts no inventory; it only marks the Xianyu order as shipped. Use this when delivery happened elsewhere and the platform status was forgotten.
- **Full delivery (match and send cards):** matches automation, obtains cards, sends them to the buyer, and changes the shipping status. Use it only when no content has been sent and the status has not been changed.

If you are unsure whether a card was already sent, inspect chat history, inventory changes, and automation exceptions before choosing full delivery.

## Edit and delete

Edit Order can correct status, buyer ID, paid amount, shipping information, and product title. Order IDs cannot be changed in the page. Before deleting, confirm the record is not needed by automation, statistics, or after-sales handling.

## Delivered cards

Order details include a **Delivered cards** section listing the cards sent for that order (from the card sales ledger). Content is read on demand and returned with `no-store` caching; only the owner can see it. Orders sent before the ledger existed show nothing. See [Card Inventory](Card-Inventory).
