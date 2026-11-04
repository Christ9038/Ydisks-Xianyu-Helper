# Accounts, Inventory and Products: Pre-launch Checklist

[中文](账号、库存与商品.zh-CN) | English

This page is a connected pre-launch check. For page-specific steps, read [Account Management](Account-Management), [Card Inventory](Card-Inventory), [Product Management](Product-Management), and [Delivery Templates](Delivery-Templates).

## 1. Connect an account

1. Open Account Management and choose Add Account/QR Login.
2. Scan with the target Xianyu account in the official client. Complete face or security verification only through the official flow shown by the page.
3. Wait for login data to be saved, then confirm nickname, account ID, and runtime state. “Runtime state conflict” means the database says the account is disabled while an old runtime is still alive; do not treat it as fully stopped. Wait or retry the stop operation and inspect service logs.
4. Add a note, enable or pause the account as needed, and bind notification channels from its edit page.
5. Sync products once after first connection and confirm the account is enabled before enabling automation.

Never send Cookies manually to other people. Login state, fingerprints, and passwords are encrypted by the system and depend on a stable `XIANYU_DATA_KEY`.

## 2. Create card inventory

Paid delivery and review gifts both depend on card inventory. In Card Inventory, click Add New Card:

| Type | Suitable content | Entry method |
| --- | --- | --- |
| Text | Fixed instructions or links | Enter the text. |
| Batch cards (`data`) | One-time codes and serial numbers | One card per line; delivery consumes inventory. |
| Image | Fixed image resources | Enter a publicly accessible image URL. |
| API | External code-delivery service | Enter URL, GET/POST, timeout, headers, parameters, and response path; validate with non-sensitive test data first. |

Common fields are name, description, enabled state, and default delay in seconds. Copy the card-group ID after saving; it is the only identifier used by automation rules and bulk-listing tables.

API cards support `{order_id}`, `{item_id}`, `{buyer_id}`, `{chat_id}`, `{account_id}`, `{cookie_id}`, `{spec_name}`, `{spec_value}`, `{quantity}`, `{order_quantity}`, `{amount}`, `{order_amount}`, `{item_detail}`, `{trigger_type}`, `{timestamp}`, `{delivery_unit_index}`, `{delivery_total_count}`, and `{idempotency_key}`. Variables are substituted in header, query-parameter, and request-body values, not in URLs or header names. GET puts parameters in the query; POST sends the request body using the selected Content-Type (JSON by default). A response path can be `data.card.code` or `data.cards[0].code`; when empty, extraction checks `data`, `content`, and `card` in that order.

Headers, parameters, and the request body are encrypted as a whole. Card lists, details, and rule APIs return only whether headers and parameters are configured and never echo template plaintext. Only when the owner opens a saved API card for editing, a dedicated owner-only endpoint returns the saved template (sent with no-store caching); secret-bearing inputs are masked by default and the draft is cleared when the dialog closes or another card is opened. Saved cards can be tested directly without retyping their secrets. Editing headers or parameters replaces the stored template, Keep preserves the old one, and Clear removes it. A new card can also be filled in from a pasted cURL command. Before enabling idempotent retries, put `{idempotency_key}` in a place that is actually sent: a header works for both GET and POST, GET may also use a query parameter, and POST only sends the request body (when the body is empty, parameters are sent as the body). Saving is rejected otherwise; configurations already stored are marked unusable until they are fixed and re-saved, and delivery never retries without a delivered idempotency key. The system makes at most three requests and retries only network errors, timeouts, 408, 429, and 5xx responses.

Administrators can enable “限制用户配置的 HTTP 出站请求只能访问公网” (restrict user-configured HTTP outbound requests to public addresses) in System and AI. Once enabled, API delivery, AI, HTTP notifications, remote images, and remote slider services cannot reach local, private, link-local, reserved, or cloud-metadata addresses; DNS resolution and redirects are checked again. The option is off by default and takes effect immediately after saving. It does not replace API-key or request-template security.

Batch cards can be created with Batch Import, or appended to an existing `data` group. Check inventory before listing; replenish first instead of relying on manual recovery.

## 3. Delivery templates

Delivery Templates can create, edit, enable, and disable ordered multi-message templates. Templates support order, buyer, card, and custom variables. After selecting one in an automation rule, bind inventory to every card variable and fill the required custom variables. Templates work only for paid delivery and review gifts, not price changes or review reminders. A referenced template cannot be deleted, and its messages are sent independently in order.

## 4. Sync or publish products

In Product Management:

- For products already on Xianyu, sync all or sync by page and confirm ownership by the correct account.
- For a new product, enter title, description, price, inventory, shipping, and images. Verify that images work and recheck the target account before submitting.
- Single-product publishing supports up to two specification types. Each type needs at least two values; the system generates combinations with separate price and inventory.
- Specification values can have specification images. Product images can be added, removed, dragged, or reordered; the first image is the main image.
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
