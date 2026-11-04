# Card Inventory Guide

[中文](卡密库存.zh-CN) | English

## Create a card group

1. Open Card Inventory and click Add New Card.
2. Enter a card name and optional description.
3. Select a delivery type and enter its content:

| Type | Use | How to enter content |
| --- | --- | --- |
| Text | Fixed instructions, links, or text | The same content is sent on every delivery. |
| Batch inventory | Redemption codes, serial numbers, or account credentials | Enter one value per line; values are consumed one by one. |
| Image | A fixed image resource | Pick an image from the image library in the [Resource Library](Resource-Library) (read by the server at delivery), or enter an accessible external image URL (downloaded temporarily and uploaded to Xianyu at delivery); the two are mutually exclusive. |
| API (shown as "API 接口") | Dynamic code retrieval from an external system | Enter the API URL, method (GET/POST), timeout (1-60 seconds, default 10), headers, query parameters, body and optional response extraction path. |

4. Optionally set delivery delay from 0 to 3600 seconds; `0` means send immediately.
5. Click Add Card to save. New groups are enabled by default; later switch them with the status toggle in the list or "Enabled" in the edit dialog.
6. Record the card-group ID (click the ID in the list to copy it) for automation rules and bulk listing. The list can be filtered by type and name.

## Add batch cards

- Enter one card per line when creating a batch-inventory group.
- For an existing batch-inventory group, click Batch Import, switch to the "Add cards to one group" tab (往单个组充卡密), choose the target group, enter one card per line and click Append; blank lines are ignored.
- Check the success and failed-row counts after import. Do not import the same batch twice.

## Batch-create card groups

In the import dialog switch to "Batch create card groups" and upload an `.xlsx`, `.csv` or `.tsv` table: one group per row with headers `名称,类型,内容,描述,启用,延迟秒,多规格,规格名,规格值` (Chinese headers exactly as shown; download the template first). Type is `text`, `data` or `image`; for `data` the content holds one card per line (wrap multi-line CSV cells in quotes); an empty enabled column means enabled and an empty delay column means 0 (range 0-3600). At most 200 rows per upload and the file must not exceed 6 MiB. Check the success and failed rows afterwards; fix failed rows and retry without re-importing rows that already succeeded. The card group ID needed by bulk-listing tables can be copied from the card list.

## API cards

API cards are suitable for external code-delivery systems that issue a code per order. You can paste a cURL command to fill in the request, and an existing card can be reopened for editing and tested without retyping its secrets. When idempotent retry is enabled, `{idempotency_key}` must appear in a header, query parameter, or body that is actually sent; otherwise saving is rejected and delivery makes only one request. Test the endpoint with non-sensitive data and confirm the response shape before enabling production automation. Saved headers, parameters and keys are not shown in the list; when reopening a card for editing, leaving a sensitive template empty keeps the saved value. Request headers, parameters, and keys are sensitive; do not expose them in screenshots or chat.

Headers, query parameters and the body may use these variables (replaced with the current order's values at send time):

| Variable | Meaning |
| --- | --- |
| `{order_id}` `{item_id}` `{buyer_id}` `{chat_id}` | Order ID, item ID, buyer ID, chat ID |
| `{account_id}` (same as `{cookie_id}`) | Account ID |
| `{spec_name}` `{spec_value}` | Order specification name and value |
| `{quantity}` (same as `{order_quantity}`), `{amount}` (same as `{order_amount}`) | Purchased quantity, order amount |
| `{item_detail}` `{trigger_type}` `{timestamp}` | Item detail, trigger type, UTC timestamp (RFC3339, with nanoseconds) |
| `{delivery_unit_index}` `{delivery_total_count}` | Which unit this is and the total (each unit is requested separately when several are bought) |
| `{idempotency_key}` | Stable key per card unit across retries; must appear in the request when retry is enabled; at most 3 attempts |

With "enable idempotent retry" checked, the same card unit is requested at most 3 times; otherwise only once. The optional response extraction path may be `data.card.code`. If the external response changes, disable the group first so automatic delivery does not fail repeatedly.

## Edit, disable, or delete

- Disabling a group prevents automation from using it; use this during maintenance or when inventory is low.
- Changing the delay affects future deliveries only.
- Deleting asks for confirmation; the system does not check references for you, so confirm beforehand that no automation rule or bulk-listing task references the group.
- Before launch, check that the group is enabled, inventory is sufficient, and card values contain no unintended spaces or blank lines.

## Sales ledger (sold / unsold)

Click the "Inventory records" icon in the list's action column to open the **Inventory records** dialog (available for batch-data and API cards):

- **Unsold** tab (batch-data cards only; API cards fetch codes remotely at delivery and only have the Sold tab): remaining cards (preview shows the first part only) and **Export unsold TXT**, one card per line, ready to import again.
- **Sold** tab: every card sent, with status, order ID and time; filter by status (all / sold / pending check) and search by order ID (exact match) or card text; **export sold CSV** (columns: card, status, card group, account, order ID, item ID, buyer ID, sold time in Beijing time; the headers are in Chinese).
- Status **sold** means the send was confirmed. **Pending check** (`reserved`) means the send result was uncertain and needs manual verification; do not resend blindly.
- Stock deduction and the ledger entry are written in one transaction. Cards sent before the ledger existed are not listed.
- Records are visible only to the owning user. The cards sent for one order can also be viewed in Orders (see [Orders](Orders)).
