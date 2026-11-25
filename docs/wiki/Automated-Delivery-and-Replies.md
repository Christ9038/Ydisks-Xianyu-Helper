# Automated Delivery and Replies Guide

[中文](自动化发货与回复.zh-CN) | English

## Four automation triggers

| Trigger | When it runs | Standard flow |
| --- | --- | --- |
| Price change for unpaid order | A buyer’s unpaid order card arrives | Match product/specification → change to target price → optionally send a reminder. |
| Paid delivery | A payment system card arrives | Match product/specification → send cards → confirm shipment only when the account setting enables it. |
| Review gift | A review system card arrives | Match product/specification → send gift cards. |
| Review reminder | A scheduled scan finds a shipped, unreviewed order | Wait until the threshold → send the reminder copy. |

System notification cards enter automation decisions only. ordinary buyer messages go through keyword replies, Agent replies (enabled in the Agent Reply tab), AI replies, and default replies in that order.

Payment reminders, successful bargaining cards awaiting shipment, and review reminders can use different system-message formats. The system recognizes their transaction semantics. Ordinary buyer chat does not trigger transaction automation. Result notifications are sent when the account has a bound channel whose event filters include the corresponding automation category, and the automation reaches completed, failed, or manual-review status.

Account-level automatic reviews and daily refresh are separate from the order rules above. Configure them under Account Management → Automatic Reviews and Daily Refresh; see [Chat and Account Automation](Chat-and-Account-Automation).

The Automation page (自动化规则) has five tabs: Trade Automation (交易自动化), Keyword Replies (关键词回复), Account Default Reply (账号默认回复), Item First Reply (商品首次回复) and Rule Log (规则日志). The four triggers above are created on the Trade Automation tab with “New automation” (新建自动化). The form also has a Priority field (a smaller number wins; for the same account, product and trigger only the highest-priority rule runs, and when a product-level rule exists the account-level rule does not apply) and an Enable rule switch.

## Configure unpaid-order price changes

1. Click New automation, choose Unpaid Order Price Change, select the account and linked product, and enter the target price (CNY, 0.01–1000000, at most two decimals).
2. Optionally enter the “remind buyer after price change” text; blank means only change the price.
3. Save and enable the rule. It applies only before payment. Paid or closed orders and platform-rejected changes are recorded as failures.

## Configure paid delivery

Prerequisites: the account is online, Automatic Delivery is enabled for the account, the product is synced, and an enabled card group has sufficient inventory. The separate Automatic Shipping Confirmation setting controls whether the platform order is marked shipped.

1. Open Automation → Trade Automation, click New automation and choose Paid Delivery.
2. Set a rule name and select the target account and product. For products with specifications (the multi-spec status comes from the Xianyu item and cannot be changed here), fill in the spec names and spec values (multiple SKUs joined with “；”, in the same order) for each specification that needs delivery. If you leave the product empty to create an account-level rule, you must tick “I confirm this delivery content applies to all products of this account”; without it the rule is not used as a fallback when a product has no rule.
3. Click Add delivery content, choose the delivery method (card inventory or delivery template), choose the card inventory (or click “+ New card” to create one in place — it is selected automatically once it is ready for automatic delivery; an API card whose configuration is not ready is created but not selected) and set quantity per item (1–100). A specification can have several contents, all sent in order after payment.
4. To override the card’s default delay, tick “Override default card delay” and enter seconds (0–3600); otherwise the card’s default delay is used.
5. Tick Enable rule and save.
6. Use a test order to verify card content is sent and inventory is deducted correctly; if Automatic Shipping Confirmation is enabled for the account, also verify that the platform status changes to shipped afterward.

For result notifications, bind a notification channel to the account and select the corresponding automation category in the channel’s events. Quantity per item is multiplied by the order quantity; do not enter the per-order total as the per-item quantity.

## Configure review gifts and reminders

Review gifts use the same configuration pattern as paid delivery, with Review Gift as the trigger. Use a separate card group for gifts so inventory and replenishment remain easy to audit.

To configure review reminders:

1. Click New automation, choose Review Reminder, and select account and product.
2. Set hours to wait after shipment (default 72), hours between reminders (default 24), and the maximum number of reminders (default 1).
3. Set "Only process orders shipped within the last N hours". Orders shipped earlier than this window are never reminded, so re-enabling a rule after a pause does not message old orders. The default is the time needed to finish all reminders plus 24 hours, and it is never below the first wait time.
4. Enter the reminder copy, save, and enable the rule.
5. Use a test order to confirm that no message is sent early and that reminders stop at the maximum.

## Exceptions and manual delivery

The exception area of Automation lists actionable runs and delayed tasks. Check the order, inventory, chat history, and failure reason before using the matching action ("已执行，继续下一步" continue, "未执行，安全重试" safe retry, "终止" cancel; delayed tasks: "重新入队" requeue, "忽略" dismiss). If delivery status is uncertain, do not retry immediately; duplicate cards may be sent.

For a manual resend, locate the order in Orders, confirm buyer, product, and delivery history, then use manual delivery/resend. Record the reason and check inventory afterward.

Paid delivery is normally triggered only by the platform's paid system message. If that message is lost, or the order fails during preparation (not even leaving a run record), the order stays in pending shipment forever. The scheduler scans for such orders once a minute, but waits until the local pending-shipment fact has been observed for at least two minutes before re-triggering delivery from status; this lets the real-time paid card establish its run first. Bargain free shipping is a separate account-level switch: the “I have made a small cut, awaiting completion” WebSocket card may call free shipping only when that switch is enabled; the later “successful cut, awaiting shipment” card then sends delivery content, followed by ordinary confirmation shipment if enabled. The fallback never calls free shipping. It may recover a bargain order only after the successful free-shipping stage has been recorded. Once an order has any `order_paid` run it is no longer picked up, and further handling goes through the failed runs in the exception area. Two fallback triggers for the same order are at least 10 minutes apart. Each scan round triggers at most 20 orders within a shared 30-second budget (the rest wait for the next round), and a run is resumed at most 5 times. Unfinished pending-shipment runs are resumed as well, but only idempotent `confirm_shipment` tail actions that never contact the buyer are allowed. Set `XIANYU_PENDING_SHIP_CATCHUP` to `0` to disable both fallback paths.

## Keyword and default replies

In Automation → Keyword Replies tab, select an account, click Add keyword (新增关键词), and save. Keyword rules apply to ordinary buyer messages.

- **Expressions:** one rule can hold several expressions that share the same reply; any one matching triggers it. In contains mode, an expression may also list several candidate words separated by `|`, commas, or line breaks.
- **Match mode:** choose Contains or Regular Expression. Regular expressions use Go/RE2 syntax and ignore case; the page pre-checks them and the server validates them again on save.
- **Products:** a rule can be linked to several synced products. Leaving it empty makes it an account-level rule.
- **Reply type and content:** choose Text or Image, then enter the reply text or the reply image.
- **Notify a human on match (命中后通知人工):** when ticked, a matching buyer message still gets the rule's reply, and you are also alerted through the "Human agent requested" notification category (`chat_manual_requested`); the conversation then enters the account's manual takeover pause (see [Account Management](Account-Management)). Use it for keywords such as "转人工" (transfer to a human) or "人工客服" (human agent); at most one notification per conversation every 10 minutes.

Default replies are the last level of the current production chain: keyword reply first, AI reply second, and default reply last. By default AI handles bargaining only (the account AI setting “Bargaining only”); when the account’s AI mode is Full Service, it also answers ordinary consultation, shipping, and after-sales messages that no keyword rule matched. A product can also force AI on or off (see [Product Management](Product-Management)). If AI is disabled, lacks an API key, does not apply to the message, has no usable result, or fails, the system continues to the default reply. The account default reply is maintained on the Account Default Reply tab (button “Edit default reply”) and has an enable switch, text, an image, and “send the default reply only once per conversation”. A product-specific default reply takes precedence over the account default. The external API extension point, if later wired, takes precedence over keyword replies.

## Agent reply

The Agent Reply tab of Automation forwards buyer messages (text or image) that match no keyword to your own external HTTP agent and sends the agent's text or image replies back to the buyer. Priority: keyword reply → Agent reply → AI reply → default reply. When disabled or unconfigured, behavior is unchanged; if the agent returns `handled:false`, the first request fails, or the whole-turn time limit expires before the agent has taken over, AI and default replies take over.

Protocol choice: the tab has a **Reply protocol** selector grouped into "Agent platforms" (Dify, FastGPT, self-hosted v1) and "direct model". `v1` (default, all existing settings) is the self-hosted Agent protocol below. OpenAI-compatible and Anthropic call a model API directly instead (no workflow; AI reply itself supports both too) (address = Base URL, token = API Key, plus model name and optional system prompt; Anthropic defaults to `claude-opus-5-5`). In these two modes every buyer message makes one single request: no polling, no chat history, no item details, at most one text reply, and the buyer's image is attached (plain-text retry once if the endpoint rejects images). Failures, refusals and empty answers fall through to AI and default replies; order stage (if ticked) is appended to the system prompt. Test connection sends a minimal `ping` request with a 16-token limit. The v1 details below apply only to the `v1` protocol.

**Dify and FastGPT (workflow agent platforms):** choose `Dify` or `FastGPT` as the reply protocol to connect without writing any code.

- **Custom address:** both cloud and self-hosted work; enter the platform address. Dify cloud: `https://api.dify.ai/v1`; self-hosted: `https://your-domain/v1` (a bare domain is completed to `/v1/chat-messages`). FastGPT cloud: `https://cloud.fastgpt.cn/api` (international `https://cloud.fastgpt.io/api`); self-hosted: `https://your-domain/api` (a bare domain is completed to `/api/v1/chat/completions`, and the helper retries `/v2` if the endpoint returns 404).
- **Token:** the app's API key (Dify: the app's API access page). It is stored encrypted and the page only shows "configured". Newer FastGPT (tested on v4.17.5) uses team-level API keys that are not bound to an app, so write the token as `apiKey-appId` (joined with a hyphen; the app ID is in the app detail URL); an older app-bound key is entered as is.
- **Conversation memory lives on the platform:** the helper sends no chat history. Dify's `conversation_id` is stored per account and buyer conversation in the local database (cleaned up after 30 days unused; if the platform deleted or ended the conversation the helper retries once with a new one); FastGPT uses a stable `chatId` derived from the account and buyer conversation. System prompt, knowledge base and workflow are configured on the platform, so there is no model name or system prompt field here and no order stage is sent.
- **One request is one whole turn:** each (merged) batch of buyer messages makes a single request and at most one text reply; buyer images are sent with the request and dropped with a plain-text retry if the platform rejects them. The request timeout is the whole-turn limit ("request timeout" is only used by Test connection). Dify is called in streaming mode so Chatbot, Chatflow and Agent apps all work; if a Dify app has required start-node variables, make them optional or give defaults because the helper sends empty `inputs`.
- If the platform errors, returns no text or times out before the helper started sending, AI and default replies take over. When merged buyer messages interrupt a request, the platform may keep a record of the interrupted one; the merged request repeats that text, which does not affect the reply.
- Test connection sends one message `ping`, which leaves a test conversation on the platform.

Protocol v1: the helper POSTs JSON to the configured address with an optional `Authorization: Bearer <token>`. Request fields are `version`, `event` (`message`, `poll`, `ping`), `turn_id`, `sequence`, `account_id`, `chat_id`, `item_id`, `buyer` and `message`; response fields are `handled` (defaults to true), `working`, `replies` (each with `text` or `image_url`) and `poll_after_ms`. While `working` is true the helper keeps sending `poll` events after `poll_after_ms`.

- Request timeout 5–30 s (default 15), response body at most 256 KiB, whole turn 10–300 s (default 120), poll interval clamped to 1–10 s, at most 60 requests per turn.
- At most 10 replies per turn, of which at most 3 images; once 10 replies have been sent the turn ends without further polling, and an uncertain send result ends the turn without retry.
- Split-message merging: buyers often split one thought into several messages, so the helper waits for the account's "Buyer message merge wait" (see Account Management; default 3 s) and then hands the whole batch to the agent at once. The `message` event therefore gains two optional fields: `messages` (every message of the batch, oldest first, each shaped like `message`) and `replaces_turn_id` (the `turn_id` of an older turn that was replaced **before it had sent any reply**, so the agent can drop that task). The `message` field stays and holds the merged text (several messages joined by newlines), so agents that only read `message` need no change. The helper sends no chat history; keep conversation memory yourself keyed by `chat_id`.
- Replacement rules: if a new message arrives before the agent has produced any reply, the old turn is cancelled and the new turn asks again with all merged messages; if it arrives after the agent has already sent its first reply, the old turn stops between sends and the new turn carries only the new message. Example request after the buyer sent two messages:

```json
{"version":1,"event":"message","turn_id":"t-2","replaces_turn_id":"t-1","sequence":1,"account_id":"acc1","chat_id":"chat-1","item_id":"item-1","buyer":{"id":"b1"},"message":{"text":"这个还在吗\n能便宜点吗"},"messages":[{"text":"这个还在吗"},{"text":"能便宜点吗"}]}
```
- The agent cannot trigger automatic price changes; the token is stored encrypted, the page only shows “configured”, and logs and errors never contain it. A saved token is only used for the address it was saved with: after changing the protocol, host or port you must enter the token again (or clear it) before saving or testing; changing only the path or query keeps the token.
- The Test connection button sends a `ping` event with no buyer data.
- Order stage (optional, off by default): after ticking “请求中携带买家订单阶段” in the tab, the `message` event gains an `order_context` field such as `{"orders":[{"stage":"paid","quantity":"1","spec_name":"颜色","spec_value":"红"}]}`, listing up to 3 of the buyer's most recent orders for the current item, newest first. `stage` is one of `created` (ordered, payment not yet confirmed), `paid` (paid, awaiting shipment), `shipped`, `completed`, `refunding` or `cancelled`; `quantity`, `spec_name` and `spec_value` are omitted when empty. An empty `orders` array means the lookup ran and found no order; an absent field means the option is off, the message has no associated item, or the stage could not be determined—it does not mean the buyer has no order. The data comes only from the helper's local order records, may lag behind the platform, and never includes order numbers, amounts or shipping details; `poll` and `ping` events never carry it. With the option off the request body is unchanged.

## Item first-reply (greeting)

Rules page → **Item first reply** tab (button “Add first reply”). When a buyer first inquires about a selected item, the system sends one preset text (and optional image URL) automatically.

- A rule has a name, enabled switch, reply text, optional image URL and one or more items of the account. Within one account **an item can belong to only one rule** (a conflict is rejected).
- Text may use `{send_user_name}`, `{send_user_id}`, `{send_message}` and `{item_id}`.
- It is sent once per buyer conversation and item; sent records can be cleared. If it fails, only a log line is written and the normal reply chain continues. For a conversation that already received it, the account default reply set to "只回复一次" (reply once) yields to it.

## Rule log

Rules page → **Rule log** tab is an append-only audit of automation behaviour, so you can see whether a delivery happened, what was sent and why something was skipped.

- Filter by status (success, failed, queued, running, skipped, pending manual, cancelled), trigger type and order ID; open an order's **run timeline**.
- Events cover skipping, queueing, run start, action success/failure, final state, free-group result and dead-lettered deferred tasks. Only summaries are stored, never card or message bodies; delivered cards are masked and loaded only on demand.
- Events older than 90 days are cleaned daily.
- Runs that need manual attention are listed in the "automation tasks needing manual handling" panel and trigger the separate **Manual intervention required** notification category; enable it in the channel's event filter (existing automation categories keep working).
