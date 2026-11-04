# Dashboard Guide

[中文](仪表盘.zh-CN) | English

## Purpose

Dashboard shows the current operating overview. It does not modify orders, inventory, or products. Metrics are grouped by time range, cover only the orders of accounts owned by the signed-in user, and are useful for daily checks and pre-campaign reviews.

## View operating data

1. Sign in and open Dashboard.
2. Select Today, Yesterday, Last 3 Days, Last 7 Days (the default), or Last Month (30 days).
3. Select Custom to enter a start and end date, then click Apply.
4. Review cumulative revenue (the percentage on the card compares it with the previous period of equal length), active accounts/total accounts, order count, and remaining card inventory.
5. Continue with revenue trends, product sales ranking, item order share, item amount analysis (TOP5), and the order details included in the statistics; at most the 500 most recent (by creation time, newest first) are loaded, and the page shows "showing the latest N / total" when there are more.

Included orders are pending shipment, shipped, and completed orders with valid amounts. Orders fall into the range by their **creation time**, using the dates of the browser's local time zone. Order details can be searched by order ID, product, or buyer, and each row links to the Xianyu order detail.

The revenue trend uses an area chart when there are more than 2 data points and bars otherwise, and shows "no revenue data" when the range has no revenue. "Product sales ranking" lists the top 10 products by order count; "item order share" is a donut chart of the top 6 products by order count; "item amount (TOP5)" is a donut chart of the top 5 products by amount. Order-detail search only covers already loaded rows. A custom date range takes effect only after you click Apply; failed validation just shows a hint and keeps the current range.

## Daily checks

- If the number of active accounts drops, check login state and risk-control notices in Account Management.
- If inventory approaches its safety threshold, replenish Card Inventory before continuing promotion.
- If orders exist but revenue or details look wrong, refresh first, then compare with the original order in Orders.
- Dashboard reflects saved data. Refresh again later if a recent sync or delivery is not immediately visible.
