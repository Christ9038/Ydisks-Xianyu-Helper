# Automated Delivery and Replies Guide

[中文](自动化发货与回复.zh-CN) | English

## Four automation triggers

| Trigger | When it runs | Standard flow |
| --- | --- | --- |
| Price change for unpaid order | A buyer’s unpaid order card arrives | Match product/specification → change to target price → optionally send a reminder. |
| Paid delivery | A payment system card arrives | Match product/specification → send cards → confirm shipment only when the account setting enables it. |
| Review gift | A review system card arrives | Match product/specification → send gift cards. |
| Review reminder | A scheduled scan finds a shipped, unreviewed order | Wait until the threshold → send the reminder copy. |

System notification cards enter automation decisions only. In the current production wiring, ordinary buyer messages go through keyword replies, AI replies, and default replies in that order; the code retains an extension point for an external API reply handler.

Payment reminders, successful bargaining cards awaiting shipment, and review reminders can use different system-message formats. The system recognizes their transaction semantics. Ordinary buyer chat does not trigger transaction automation. Result notifications are sent when the account has a bound channel whose event filters include the corresponding automation category, and the automation reaches completed, failed, or manual-review status.

Account-level automatic reviews and daily refresh are separate from the order rules above. Configure them under Account Management → Automatic Reviews and Daily Refresh; see [Chat and Account Automation](Chat-and-Account-Automation).

## Configure unpaid-order price changes

1. Create an Unpaid Order Price Change rule, select the account and product/specification, and enter the target price.
2. Optionally enter reminder text to send after a successful change; blank means only change the price.
3. Save and enable the rule. It applies only before payment. Paid or closed orders and platform-rejected changes are recorded as failures.

## Configure paid delivery

Prerequisites: the account is online, Automatic Delivery is enabled for the account, the product is synced, and an enabled card group has sufficient inventory. The separate Automatic Shipping Confirmation setting controls whether the platform order is marked shipped.

1. Open Automation, choose Paid Delivery, and click Add.
2. Set a rule name and select the target account and product. For products with specifications, create a matching configuration for each specification that needs delivery. If you leave the product empty to create an account-level rule, you must tick “I confirm this delivery content applies to all products of this account”; without it the rule is not used as a fallback when a product has no rule.
3. Add a Send Cards action, choose card inventory, set quantity per item, and choose whether the action is enabled.
4. To override the card group’s default delay, enable the override and enter seconds; otherwise the group delay is used.
5. Save and confirm the rule is enabled.
6. Use a test order to verify card content is sent and inventory is deducted correctly; if Automatic Shipping Confirmation is enabled for the account, also verify that the platform status changes to shipped afterward.

For result notifications, bind a notification channel to the account and select the corresponding automation category in the channel’s events. Quantity per item is multiplied by the order quantity; do not enter the per-order total as the per-item quantity.

## Configure review gifts and reminders

Review gifts use the same configuration pattern as paid delivery, with Review Gift as the trigger. Use a separate card group for gifts so inventory and replenishment remain easy to audit.

To configure review reminders:

1. Create a Review Reminder rule and select account and product.
2. Set how many hours after shipment to wait, the repeat interval, and the maximum number of reminders.
3. Set "Only process orders shipped within the last N hours". Orders shipped earlier than this window are never reminded, so re-enabling a rule after a pause does not message old orders. The default is the time needed to finish all reminders plus 24 hours, and it is never below the first wait time.
4. Enter the copy, save, and enable the rule.
5. Use a test order to confirm that no message is sent early and that reminders stop at the maximum.

## Exceptions and manual delivery

The exception area of Automation lists actionable runs and delayed tasks. Check the order, inventory, chat history, and failure reason before using Process/Resolve. If delivery status is uncertain, do not retry immediately; duplicate cards may be sent.

For a manual resend, locate the order in Orders, confirm buyer, product, and delivery history, then use manual delivery/resend. Record the reason and check inventory afterward.

Paid delivery is normally triggered only by the platform's paid system message. If that message is lost, or the order fails during preparation (not even leaving a run record), the order stays in pending shipment forever. The scheduler scans for such orders once a minute, but waits until the local pending-shipment fact has been observed for at least two minutes before re-triggering delivery from status; this lets the real-time paid card establish its run first. Bargain free shipping is a separate account-level switch: the “I have made a small cut, awaiting completion” WebSocket card may call free shipping only when that switch is enabled; the later “successful cut, awaiting shipment” card then sends delivery content, followed by ordinary confirmation shipment if enabled. The fallback never calls free shipping. It may recover a bargain order only after the successful free-shipping stage has been recorded. Once an order has any `order_paid` run it is no longer picked up, and further handling goes through the failed runs in the exception area. Two fallback triggers for the same order are at least 10 minutes apart. Unfinished pending-shipment runs are resumed as well, but only idempotent `confirm_shipment` tail actions that never contact the buyer are allowed. Set `XIANYU_PENDING_SHIP_CATCHUP=0` to disable both fallback paths.

## Keyword and default replies

In Automation → Keyword Replies, select an account, add a rule, and save. Keyword rules apply to ordinary buyer messages.

- **Expressions:** one rule can hold several expressions that share the same reply; any one matching triggers it. In contains mode, an expression may also list several candidate words separated by `|`, commas, or line breaks.
- **Match mode:** choose Contains or Regular Expression. Regular expressions use Go/RE2 syntax and ignore case; the page pre-checks them and the server validates them again on save.
- **Products:** a rule can be linked to several synced products. Leaving it empty makes it an account-level rule.
- **Reply:** enter text or an image URL.

Default replies are the last level of the current production chain: keyword reply first, AI reply second, and default reply last. By default AI handles bargaining only; when the account’s AI mode is Full Service, it also answers ordinary consultation, shipping, and after-sales messages that no keyword rule matched. A product can also force AI on or off (see [Product Management](Product-Management)). If AI is disabled, lacks an API key, does not apply to the message, has no usable result, or fails, the system continues to the default reply. A default reply may contain text, an image URL, and “reply only once per conversation”. A product-specific default reply takes precedence over the account default. The external API extension point, if later wired, takes precedence over keyword replies.

## Item first-reply (greeting)

Rules page → **Item first reply** tab. When a buyer first inquires about a selected item, the system sends one preset text (and optional image URL) automatically.

- A rule has a name, enabled switch, reply text, optional image URL and one or more items of the account. Within one account **an item can belong to only one rule** (a conflict is rejected).
- Text may use `{send_user_name}`, `{send_user_id}`, `{send_message}` and `{item_id}`.
- It is sent once per buyer conversation and item; sent records can be cleared. If it fails, only a log line is written and the normal reply chain continues. For a conversation that already received it, the account default reply set to "reply once" yields to it.

## Rule log

Rules page → **Rule log** tab is an append-only audit of automation behaviour, so you can see whether a delivery happened, what was sent and why something was skipped.

- Filter by status (success, failed, queued, running, skipped, pending manual, cancelled), trigger type and order ID; open an order's **run timeline**.
- Events cover skipping, queueing, run start, action success/failure, final state, free-group result and dead-lettered deferred tasks. Only summaries are stored, never card or message bodies; delivered cards are masked and loaded only on demand.
- Events older than 90 days are cleaned daily.
- Runs that need manual attention are listed in the "automation tasks needing manual handling" panel and trigger the separate **Manual intervention required** notification category; enable it in the channel's event filter (existing automation categories keep working).
