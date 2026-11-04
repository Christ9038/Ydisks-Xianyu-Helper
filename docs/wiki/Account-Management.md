# Account Management Guide

[中文](账号管理.zh-CN) | English

## Add a Xianyu account

1. Open Account Management and click Add New Account by QR Code.
2. Scan with the target Xianyu account and follow the page for login or security verification.
3. Wait for the account to appear, then add a note so multiple accounts are easy to distinguish.
4. Confirm the account is enabled before syncing products or configuring automation.

For face or risk-control verification, use only the official Xianyu app or the official flow shown by the page. Do not forward verification URLs. If only the account ID is shown, click Refresh Nickname and Avatar; scan again if refresh fails.

## Daily account operations

- **Enable/disable:** a disabled account does not process backend tasks; tasks resume when it is enabled.
- **Search:** the search box (placeholder "搜索昵称 / 备注 / 账号ID") filters the list by nickname, note, account ID, login username, or the account status message; a banner above the list shows how many accounts are displayed out of the total.
- **Edit:** change the note, the three switches (Auto delivery / Auto confirm shipping / Auto skip-group), processing pause duration, Cookie, "Save login info" (Xianyu's official long-login setting), and notification channel bindings. The account ID is read-only. When an account hits risk control, scanning again does not help: the edit dialog tells you to pass verification manually on the web Xianyu messages page, search the browser network panel for the token request, and paste that request's Cookie into the Cookie box; the account card then shows "粘贴无风控 Cookie" (paste risk-free Cookie) and "查看操作教程" (view guide). The Notification Channel Binding section appears only when at least one channel exists.
- **Reauthorize by QR:** use after Cookie expiry, account disconnection, or failed profile refresh. QR login is the current production login path.
- **AI Settings:** enable AI for this account, choose Bargaining Only or Full Service mode, and set bargaining limits. The system-wide API and model are configured in System and AI.
- **Automatic Reviews and Daily Refresh:** open it from the account card, save settings, or click Review Now/Refresh Now for one run.
- **Delete:** clicking delete opens a "Delete this account?" confirmation; once confirmed, the account's related configuration is cleaned up as well and this cannot be undone. Delete only after confirming that related products, rules, and orders no longer need processing.

## Pause and the three automation switches

In Edit Account, Processing Pause Duration (minutes, 0–1440; 0 means no pause) temporarily stops order handling and resumes it when the duration expires; the account card shows "暂停处理中" while paused. Use Pause Again Now to restart the pause using the current duration.

The three switches are independent and follow the fixed bargain-order sequence (see [Bargain Orders and Delivery Flow](Bargain-Orders-and-Delivery-Flow)):

- **Auto delivery (自动发货):** master automation switch. After payment it sends the card or template messages; it never changes the Xianyu order status.
- **Auto confirm shipping (自动确认发货):** after the card is sent, calls Xianyu's "shipped" API so the order becomes shipped. Requires Auto delivery. Turn it off for slow-fulfilment goods to avoid starting the buyer's auto-receive countdown.
- **Auto skip-group (自动免拼):** only on the "我已小刀，待刀成" system message it calls the skip-group API; cards are sent only after "我已成功小刀，待发货"; skip-group is not a substitute for shipping confirmation. Turning it off does not block delivery for orders that already reached that stage.

Automatic Shipping Confirmation only marks the Xianyu order as shipped; it does not prove that a card was sent. Complete automatic delivery still requires the product, automation rule, and inventory to be configured correctly.

## AI settings and bargaining policy

Open **AI Settings** on the account card:

- **Enable AI auto-reply**: the account-level master switch (dialog title "AI助手设置"); turning it off also turns off "Apply AI quotes automatically".
- **Work mode**: "Bargain only" calls AI only when bargaining intent is detected and keeps the default reply otherwise; "Full service" lets AI handle product questions, delivery, after-sales and bargaining when no keyword matched (add shop rules such as delivery times). Keyword replies always take priority over AI.
- **Bargaining policy**: maximum discount percentage (%, 0–100), maximum discount amount, and maximum bargaining rounds (1–10). 0 for percentage or amount means no discount is allowed. Bargaining messages are always bound by these limits.
- **Apply AI quotes automatically**: when AI has made a clear quote and the buyer places the order **within 30 minutes**, the unpaid order is repriced to that quote. While this is on, the fixed "unpaid order auto price change" rule cannot be enabled at the same time.
- Individual products can override the account AI settings ("Product AI service" in the product list).

Model URL and key are configured in [System and AI](System-and-AI). The "username / login password / clear saved password / show browser on login / run account-password login (password-login refresh authorization)" controls in the account edit dialog are a retired compatibility entry: the server's password-login endpoint is disabled. When a login expires, scan the QR code again; for risk control see [Risk-Control Troubleshooting](Risk-Control-Troubleshooting).

## Bind notification channels

1. Create and test a channel in Notification Settings.
2. Return to Account Management and edit the target account.
3. Select channels under Notification Channel Binding and save.
4. To receive delivery, price-change, or review-gift results, select the corresponding Paid Delivery, Unpaid Price Change, or Review Gift event in the channel’s event filters.

Account alerts and transaction-automation notices follow the account binding. Creating a channel without binding it to an account produces no notices for that account.

## Login information

Login Cookies are encrypted by the system. After updating the Cookie or long-login setting, confirm that the account is online before re-enabling automation. Never send Cookies, passwords, or QR screenshots through the Wiki, tickets, or chat.
