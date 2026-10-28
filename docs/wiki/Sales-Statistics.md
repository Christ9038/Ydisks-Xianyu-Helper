# Sales Statistics

[中文](销售统计.zh-CN) | English

**Sales Statistics** (`/app/sales`) summarises revenue over a period. **Only administrators can open this page**, and it aggregates orders of all accounts.

## Time range

Today, Yesterday, Last 3 days, Last 7 days, Last 30 days, or a custom date range (start and end dates, then *Apply*). Orders are attributed by their **creation time** in your local time zone.

## Metrics

Three figures are shown side by side and are **not netted against each other**:

| Metric | Definition |
| --- | --- |
| Sales amount (CNY) | Orders with status shipped + completed |
| Received payment (CNY) | Orders with status completed |
| Refunded payment (CNY) | Orders with status cancelled (a cancelled order is treated as refunded) |

Below the cards you get a **daily trend chart** and a **daily detail table** (order count, sales, received, refunded). The empty state means no shipped, completed or cancelled orders exist in the range.

For revenue, status and product distribution use the [Dashboard](Dashboard).
