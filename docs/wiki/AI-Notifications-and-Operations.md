# AI, Notifications and Operations Guide

[中文](AI、通知与运维.zh-CN) | English

## Configure the AI service

The system-level connection is configured under System and AI and can be changed only by an administrator:

1. Enter the OpenAI-compatible API base URL, such as `https://api.openai.com/v1` or the Qwen-compatible endpoint `https://dashscope.aliyuncs.com/compatible-mode/v1`. Enter the version root, not `/chat/completions`.
2. Enter the API key and click Read Models. If the service does not support model listing, enter the model name manually.
3. Save System and AI. The API key is stored as a sensitive field; still restrict database and administrator access.
4. Open Account Management and select AI Settings for the target account.
5. Enable automatic AI replies. Set the maximum discount percentage, maximum discount amount, maximum bargaining rounds, and custom prompt, then save.
6. Test with a realistic but low-risk bargaining conversation. Confirm amount limits and wording before enabling a production account.

AI handles buyer bargaining messages. A maximum discount percentage or amount of `0` forbids that type of discount; keep bargaining rounds conservative. Prompts should define product boundaries, forbidden promises, tone, and when to hand off to a person. Do not put Cookies, passwords, complete private order data, or other secrets in prompts.

## Configure notifications

1. Open Notification Settings. Configure system SMTP when using email. Email inherits system SMTP by default; enable Use Independent SMTP when another sender is required and fill in the complete independent configuration.
2. Create a channel: Bark, DingTalk, Feishu, WeCom, Telegram, email, or custom Webhook. Enter the address or token required by that channel.
3. Select events. Selecting none means all events. The four automation categories—price adjustment after order creation, paid-order delivery, review gifts, and review requests—can now be disabled independently. Other options include disconnection, recovery, disabling, security verification, renewal, manual delivery results, and system errors.
4. Save and click Test Notification. Confirm delivery in the target channel.
5. Return to Account Management → Edit and bind the channel to each account. Creating a channel alone does not make an account use it.

A test notification verifies only channel configuration. Automated task notifications additionally require the account binding and the corresponding automation event filter. Automation results send when a task reaches completed, failed, or manual-review status.

SMTP authentication prefers `PLAIN` and selects `LOGIN` when the server offers only `LOGIN`. For SendCloud, use `API_USER` as the SMTP username and its `API_KEY` as the password; set the sender email address separately. Use STARTTLS on port `587`, with implicit SSL/TLS disabled.

Webhook URLs and bot tokens are equivalent to passwords. Do not paste them into screenshots, Issues, or chat. Update and retest a channel immediately after rotating a token.

## Daily checks

Check the following every day or before a major promotion:

- online accounts, orders, inventory, and exception trends on Dashboard;
- pending automation exceptions or delayed tasks;
- whether batch-card inventory is below its safety line;
- whether the latest notification is reachable;
- whether login renewal or security verification needs manual action.

## Troubleshooting order

1. **Account offline:** check whether it is paused, whether its Cookie expired, and whether QR login or security verification is required. Confirm the recovery notification afterward.
2. **Delivery did not run:** check account ownership, product/specification match, enabled rules, card inventory, and automation exceptions. First confirm whether content was already sent.
3. **AI did not reply:** verify the system API URL, key, and model; then verify AI is enabled for the account and that the message is a bargaining message.
4. **Notification did not arrive:** send a test notification, then check the matching automation event filter and account binding. Paid delivery requires the Paid Delivery event; email also requires valid SMTP. If it still fails, inspect logs for notification trigger, queue insertion, and send result.
5. **Database cannot connect:** check `DATABASE_URL`, network/TLS, database permissions, and service logs. Do not delete the database as a repair step.

## Backup and recovery

Back up at least the database, `.env` (especially `XIANYU_DATA_KEY`), and required uploaded resources. To restore on a new machine, restore the database and the same key before starting the service. A different key makes encrypted Cookies, AI settings, and notification configuration unreadable. Disable high-risk automation after recovery and verify each flow with test orders.
