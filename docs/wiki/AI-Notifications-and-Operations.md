# AI, Notifications and Operations Guide

[中文](AI、通知与运维.zh-CN) | English

## Configure the AI service

The system-level connection is configured under System and AI and can be changed only by an administrator:

1. Enter the OpenAI-compatible API base URL, such as `https://api.openai.com/v1` or the Qwen-compatible endpoint `https://dashscope.aliyuncs.com/compatible-mode/v1` (used when the field is empty). Enter the version root, not `/chat/completions`.
2. Enter the API key and click Read Models. If the service does not support model listing, enter the model name manually.
3. Click Save All Configuration. The API key is stored as a sensitive field; still restrict database and administrator access. You can also click Test connection first to verify the URL, key and model.
4. Open Account Management and click the AI Settings icon on the target account to open the AI Assistant Settings dialog.
5. Enable automatic AI replies and choose the working mode: Bargaining Only (default), or Full Service, which also answers consultation, shipping, and after-sales messages. Under Bargaining Strategy set the maximum discount percentage (0–100%), maximum discount amount (yuan, not below 0), maximum bargaining rounds (1–10), and the custom prompt (shop rules in Full Service mode), then save.
6. Test with a realistic but low-risk bargaining conversation. Confirm amount limits and wording before enabling a production account.

In Bargaining Only mode AI handles buyer bargaining messages; in Full Service mode it also answers other messages, but discount, refund, and compensation promises are still forbidden outside the bargaining flow and the buyer is referred to a person. Shipping or postage amounts are not treated as bargaining. Each product can override the account switch (Inherit, Force Enable, or Force Disable) and carry up to 12,000 characters of product-specific reference material; open it from the AI Customer Service entry in Product List (a table column or a card button). That material is treated as facts only and cannot override shop rules or price limits. A maximum discount percentage or amount of `0` forbids that type of discount; once a buyer exceeds the maximum bargaining rounds the AI stops discounting and only politely says the price will not go lower; if the AI names a price below the floor derived from the discount percentage and amount, the system replaces the reply with a safe one; keep bargaining rounds conservative. Prompts should define product boundaries, forbidden promises, tone, and when to hand off to a person. Do not put Cookies, passwords, complete private order data, or other secrets in prompts.

There is also an "auto-apply AI quote price change" switch (it requires automatic AI replies to be enabled and is turned off together with it): once the AI has named a clear price and that reply has been sent to the buyer, an order for that product placed within 30 minutes has its pending-payment price changed automatically; only valid quotes already sent to the buyer are used. While automatic AI replies are enabled, the fixed price-change rules under Automation Rules cannot be enabled at the same time, and saving reports the conflict. Keyword replies still take priority over AI.

## Configure notifications

1. Open Notification Settings. Configure system SMTP when using email. Email inherits system SMTP by default; enable Use Independent SMTP when another sender is required and fill in the complete independent configuration.
2. Click New Channel and choose Bark, DingTalk bot, Feishu bot, WeCom bot, Telegram, email, or custom Webhook. Enter the address or token required by that channel; the fields of each channel are listed in [Notification Events](Notification-Events).
3. Select events. Selecting none means all events. The four automation categories—price adjustment after order creation, paid-order delivery, review gifts, and review requests—can now be disabled independently. Other options are 掉线通知 (offline), 恢复通知 (recovered), 禁用通知 (disabled), 风控验证 (security verification), 续期通知 (renewal), 手动发货结果 (manual delivery result), 需要人工处理 (manual intervention required), and 系统错误 (system error); when each fires is described in [Notification Events](Notification-Events).
4. Save and click Test on the channel row. Confirm delivery in the target channel.
5. Return to Account Management → Edit and bind the channel to each account. Creating a channel alone does not make an account use it.

A test notification verifies only channel configuration. Automated task notifications additionally require the account binding and the corresponding automation event filter. Automation results send when a task reaches completed, failed, or manual-review status (manual review sends a "manual handling required" notification, which channels subscribed only to the matching automation category also receive).

The system SMTP is saved by an administrator under "SMTP mail settings" at the bottom of the Notification Settings page. SMTP authentication prefers `PLAIN` and selects `LOGIN` when the server offers only `LOGIN`. For SendCloud, use `API_USER` as the SMTP username and its `API_KEY` as the password; set the sender email address separately. Use STARTTLS on port `587`, with implicit SSL/TLS disabled (the two options are mutually exclusive in the UI).

Webhook URLs and bot tokens are equivalent to passwords. Do not paste them into screenshots, Issues, or chat. Update and retest a channel immediately after rotating a token.

## Daily checks

Check the following every day or before a major promotion:

- active accounts / total, order count, and remaining card stock on Dashboard (Operations Overview);
- whether "Automation tasks needing manual handling" on the Automation Rules page has pending items;
- whether batch-card inventory is below its safety line;
- whether the latest notification is reachable;
- whether login renewal or security verification needs manual action.

## Troubleshooting order

1. **Account offline:** check whether it is paused, whether its Cookie expired, and whether QR login or security verification is required. Confirm the recovery notification afterward.
2. **Delivery did not run:** check account ownership, product/specification match, enabled rules, card inventory, and automation exceptions. First confirm whether content was already sent.
3. **AI did not reply:** verify the system API URL, key, and model; then verify AI is enabled for the account (and not forced off for the product) and that the message suits the mode: in Bargaining Only mode it must be a bargaining message.
4. **Notification did not arrive:** click Test on the channel, then check the matching automation event filter and account binding. Paid delivery requires the Paid Delivery event; email also requires valid SMTP. If it still fails, inspect logs for notification trigger, queue insertion, and send result.
5. **Database cannot connect:** check `DATABASE_URL`, network/TLS, database permissions, and service logs. Do not delete the database as a repair step.

## Backup and recovery

Back up at least the database, `.env` (especially `XIANYU_DATA_KEY`), and required uploaded resources. To restore on a new machine, restore the database and the same key before starting the service. A different key makes encrypted Cookies, AI settings, and notification configuration unreadable. Disable high-risk automation after recovery and verify each flow with test orders.
