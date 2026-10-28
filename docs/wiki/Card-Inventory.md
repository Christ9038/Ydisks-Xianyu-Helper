# Card Inventory Guide

[中文](卡密库存.zh-CN) | English

## Create a card group

1. Open Card Inventory and click Add New Card.
2. Enter a group name and optional description.
3. Select a delivery type and enter its content:

| Type | Use | How to enter content |
| --- | --- | --- |
| Text | Fixed instructions, links, or text | The same content is sent on every delivery. |
| Batch inventory | Redemption codes, serial numbers, or account credentials | Enter one value per line; values are consumed one by one. |
| Image | A fixed image resource | Enter an accessible image URL; it is sent when needed. |
| API | Dynamic code retrieval from an external system | Enter the URL, method, parameters, and response path requested by the page. |

4. Optionally set delivery delay from 0 to 3600 seconds; `0` means send immediately.
5. Confirm that the group is enabled and save it.
6. Record the card-group ID for automation rules and bulk listing.

## Add batch cards

- Enter one card per line when creating a batch-inventory group.
- For an existing batch-inventory group, click Batch Import, choose the target group, and append inventory.
- Check the success and failed-row counts after import. Do not import the same batch twice.

## API cards

API cards are suitable for external code-delivery systems that issue a code per order. You can paste a cURL command to fill in the request, and an existing card can be reopened for editing and tested without retyping its secrets. When idempotent retry is enabled, `{idempotency_key}` must appear in a header, query parameter, or body that is actually sent; otherwise saving is rejected and delivery makes only one request. Test the endpoint with non-sensitive data and confirm the response shape before enabling production automation. Request headers, parameters, and keys are sensitive; do not expose them in screenshots or chat.

Common order variables include order ID, product ID, buyer ID, account ID, quantity, amount, and trigger type. A response path may be `data.card.code`. If the external response changes, disable the group first so automatic delivery does not fail repeatedly.

## Edit, disable, or delete

- Disabling a group prevents automation from using it; use this during maintenance or when inventory is low.
- Changing the delay affects future deliveries only.
- Before deleting, confirm that no automation rule or bulk-listing task references the group.
- Before launch, check that the group is enabled, inventory is sufficient, and card values contain no unintended spaces or blank lines.

## Sales ledger (sold / unsold)

Open a card stock's **Inventory records** dialog (batch-data and API cards):

- **Unsold** tab: remaining cards (preview shows the first part only) and **Export unsold TXT**, one card per line, ready to import again.
- **Sold** tab: every card sent, with status, order ID and time; filter by status and search by order ID or card text; **export sold CSV**.
- Status **sold** means the send was confirmed. **Pending check** (`reserved`) means the send result was uncertain and needs manual verification; do not resend blindly.
- Stock deduction and the ledger entry are written in one transaction. Cards sent before the ledger existed are not listed.
- Records are visible only to the owning user. The cards sent for one order can also be viewed in Orders (see [Orders](Orders)).
