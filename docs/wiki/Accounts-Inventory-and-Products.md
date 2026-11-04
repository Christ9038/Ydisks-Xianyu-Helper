# Accounts, Inventory and Products: Pre-launch Checklist

[中文](账号、库存与商品.zh-CN) | English

This page is a connected pre-launch check. For page-specific steps, read [Account Management](Account-Management), [Card Inventory](Card-Inventory), [Product Management](Product-Management), and [Delivery Templates](Delivery-Templates).

## 1. Connect an account

1. Open Account Management and click “扫码添加新账号” (Add Account by QR Scan).
2. Scan with the target Xianyu account in the official client. Complete face or security verification only through the official flow shown by the page.
3. Wait for login data to be saved, then confirm nickname, account ID, and runtime state. “Runtime state conflict” means the database says the account is disabled while an old runtime is still alive; do not treat it as fully stopped. Wait or retry the stop operation and inspect service logs.
4. Add a note, enable or disable the account as needed, and bind notification channels in “通知渠道绑定” (Notification Channel Binding) on its edit page.
5. Sync products once after first connection and confirm the account is enabled before enabling automation.

Never send Cookies manually to other people. Login state, fingerprints, and passwords are encrypted by the system and depend on a stable `XIANYU_DATA_KEY`.

## 2. Create card inventory

Paid delivery and review gifts both depend on card inventory. In Card Inventory, click Add New Card:

| Type | Suitable content | Entry method |
| --- | --- | --- |
| Text | Fixed instructions or links | Enter the text. |
| Batch Stock (`data`) | One-time codes and serial numbers | One card per line; delivery consumes inventory. |
| Image | Fixed image resources | Enter a publicly accessible image URL, or pick an image from the image library (one or the other). |
| API Interface (`api`) | External code-delivery service | Enter URL, GET/POST, timeout, headers, parameters, and response path; validate with non-sensitive test data first. |

Common fields are name, description, and delivery delay in seconds (0–3600); the enabled state is switched when editing a card. After saving, copy the card-group ID (“卡密组ID”) from the list; it is the only identifier used by automation rules and bulk-listing tables.

API cards support `{order_id}`, `{item_id}`, `{buyer_id}`, `{chat_id}`, `{account_id}`, `{cookie_id}`, `{spec_name}`, `{spec_value}`, `{quantity}`, `{order_quantity}`, `{amount}`, `{order_amount}`, `{item_detail}`, `{trigger_type}`, `{timestamp}`, `{delivery_unit_index}`, `{delivery_total_count}`, and `{idempotency_key}`. Variables are substituted in header, query-parameter, and request-body values, not in URLs or header names. GET puts parameters in the query; POST sends the request body using the selected Content-Type (JSON by default). A response path can be `data.card.code` or `data.cards[0].code`; when empty, extraction checks `data`, `content`, and `card` in that order.

Headers, parameters, and the request body are encrypted as a whole. Card lists, details, and rule APIs return only whether headers and parameters are configured and never echo template plaintext. Only when the owner opens a saved API card for editing, a dedicated owner-only endpoint returns the saved template (sent with no-store caching); secret-bearing inputs are masked by default and the draft is cleared when the dialog closes or another card is opened. Saved cards can be tested directly without retyping their secrets. Editing headers or parameters replaces the corresponding stored template, and leaving them untouched keeps the old one (at the API level the `retain`, `replace`, and `clear` actions mean keep, replace, and remove). A new card can also be filled in from a pasted cURL command. Before enabling idempotent retries, put `{idempotency_key}` in a place that is actually sent: a header works for both GET and POST, GET may also use a query parameter, and POST only sends the request body (when the body is empty, parameters are sent as the body). Saving is rejected otherwise; configurations already stored are marked not ready (a validation error is returned and automation rules cannot select them) until they are fixed and re-saved, and even if a stored configuration can still deliver, delivery never retries without a delivered idempotency key. The system makes at most three requests and retries only network errors, timeouts, 408, 429, and 5xx responses.

Administrators can enable “限制用户配置的 HTTP 出站请求只能访问公网” (restrict user-configured HTTP outbound requests to public addresses) in “系统与AI” (System and AI). Once enabled, API delivery, AI, HTTP notifications, remote images, and remote slider services cannot reach local, private, link-local, reserved, or cloud-metadata addresses; DNS resolution and redirects are checked again. The option is off by default and takes effect immediately after saving. It does not replace API-key or request-template security.

Batch cards can be created with Batch Import, or appended to an existing `data` group. Check inventory before listing; replenish first instead of relying on manual recovery.

## 3. Delivery templates

Delivery Templates can create, edit, enable, and disable ordered multi-message templates. Templates support order, buyer, card, and custom variables. After selecting one in an automation rule, bind inventory to every card variable and fill the required custom variables. Templates work only for paid delivery and review gifts, not price changes or review reminders. A referenced template cannot be deleted, and its messages are sent independently in order.

## 4. Sync or publish products

In Product Management (the sidebar entry is “商品列表”, Product List):

- For products already on Xianyu, choose the account under “同步商品账号” (Sync Account), click “同步商品” (Sync Products), and confirm the products belong to the correct account.
- For a new product, click “发布商品” (Publish Product) and enter title, description, price, inventory, shipping, and images (up to 9). Verify that images work and recheck the target account before submitting.
- Single-product publishing supports up to two specification types. Each type needs at least two values; the system generates combinations with separate price and inventory.
- Once a specification type has “支持规格图片” (supports specification images) ticked, its values can have specification images. Product images can be added, removed, dragged, or reordered; the first image is the main image.
- Bulk Listing’s default category may be blank or fetched by keyword. Priority is row category, page default, automatic recognition, and finally Electronic Materials.
- Bulk listing targets virtual products; the optional batch location may be left blank. The category fallback does not change paid virtual delivery.
- Successful publishing does not enable automatic delivery. Bind card groups by product/specification in Automation or declare the configuration in the bulk-listing table.

## 5. Pre-launch checks

1. Account is online and enabled.
2. Product was synced or published successfully, with correct price, inventory, and images.
3. Bulk-listing preflight shows the intended category strategy.
4. Required card groups are enabled and sufficiently stocked.
5. Automation matches the product correctly; verify every specification.
6. Use a test order and inspect Orders, automation exceptions, and notification records.
