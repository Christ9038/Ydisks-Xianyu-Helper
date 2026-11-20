# Quick Start Guide (Step by Step)

[中文](新手入门.zh-CN) | English

This page takes you from “nothing installed” to “my first order was delivered automatically” in about 30 minutes. Follow the steps in order and do not skip the test in Step 8. Every step links to the full manual page if you need details.

## What you need

- A computer or server that can stay on (Windows, macOS, Linux, or any Docker host).
- The **Xianyu app on your phone**, logged in with the seller account you want to automate.
- One product for testing (a cheap or virtual item) and **a second Xianyu account** to place the test order (you cannot buy your own item).
- The text you want to deliver, for example a redemption code or a cloud-drive link.

## Step 1: Install and start

Pick the easiest option for you:

| Option | Best for | What to do |
| --- | --- | --- |
| Windows / macOS installer | Personal computer | Run the installer for your system and architecture. It installs the background service, a tray/menu-bar icon, and the browser the program needs. |
| Linux package | Linux server | Extract the package and run `sudo ./install.sh`. |
| Docker Compose | Server, production | Copy `.env.example` to `.env`, fill in the required values, and run `docker compose up -d`. |

Details, required environment variables, and database choices are in [Deployment and Database](Deployment-and-Database). Desktop packages listen on `http://127.0.0.1:59188` (this computer only); Docker and source runs listen on all interfaces by default, so restrict the port with a firewall if the server is public.

> **Back up `XIANYU_DATA_KEY` right away.** It encrypts saved Cookies, passwords, AI keys and notification credentials. Losing it, or changing it later, makes that data unreadable.

## Step 2: Open the console and create the administrator

1. Open `http://127.0.0.1:59188` in a browser (use the server address instead when deployed remotely).
2. On a new database you see the first-run page. Enter a password of at least 8 characters, confirm it, and submit. The administrator user name is `admin`, and you are signed in automatically. (With Docker Compose the password comes from `XIANYU_ADMIN_PASSWORD` instead and you simply sign in.)
3. Remember this password; it protects the whole console.

The sidebar shows all pages. On a phone, the bottom bar has Dashboard, Chat, Accounts, Orders and **More** (the other pages live in the More drawer).

## Step 3: Add your Xianyu account

1. Open **Account Management** and click **Add New Account by QR Code**.
2. Scan the QR code with the Xianyu app of the seller account and confirm on the phone. If Xianyu asks for face or security verification, finish it in the official app only.
3. Wait until the account card appears, then give it a **note** (for example “main store”) so you can tell accounts apart.
4. Make sure the account is **enabled** and shows as online.

More: [Account Management](Account-Management). If the account is blocked by risk control later, see [Risk-Control Troubleshooting](Risk-Control-Troubleshooting).

## Step 4: Create the content to deliver (a card group)

1. Open **Card Inventory** and click **Add New Card**.
2. Enter a name such as “test card”.
3. Choose the type:
   - **Text** — the same text is sent every time (good for a first test, a link or instructions);
   - **Batch inventory** — one code per line, each order uses up one line (redemption codes, accounts).
4. Save. New groups are enabled by default.

More: [Card Inventory](Card-Inventory). To send several messages or images in a fixed order, create a [Delivery Template](Delivery-Templates) later.

## Step 5: Bring your product into the system

1. Open **Product List**, select your account in Sync Product Accounts, and click **Sync Products**.
2. Check that the title, price and product ID of your test product appear.

If the product is not on Xianyu yet, use **Publish Product** on the same page (or [Bulk Listing](Bulk-Listing) for many products). More: [Product Management](Product-Management).

## Step 6: Turn on delivery and create a delivery rule

1. In **Account Management**, click **Edit** on the account and turn on **Auto delivery**. Decide about **Auto confirm shipping**:
   - on: after the card is sent, the program marks the Xianyu order as shipped (good for instant goods);
   - off: the program only sends the content and you ship manually (safer for slow-fulfilment goods).
   Leave **Auto skip-group** off unless you sell two-person bargain (小刀) items; see [Bargain Orders and Delivery Flow](Bargain-Orders-and-Delivery-Flow).
2. Open **Automation** (sidebar name “自动化规则”) → **Trade Automation** → **New automation** → **Paid Delivery**.
3. Select the account and your test product, click **Add delivery content**, choose **card inventory**, pick the card group from Step 4, and keep the quantity per item at 1.
4. Tick **Enable rule** and save.

More: [Automated Delivery and Replies](Automated-Delivery-and-Replies).

## Step 7: Get notified (recommended)

1. Open **Notification Settings** and click **New Channel** (Bark, DingTalk, Feishu, WeCom, Telegram, email or Webhook all work).
2. Fill in the address or token, tick the accounts that should use this channel, and save.
3. Click **Test** on the channel. If the test message arrives, you will also be told when a delivery fails or needs manual handling.

More: [AI, Notifications and Operations](AI-Notifications-and-Operations) and [Notification Events](Notification-Events).

## Step 8: Test with a real small order

1. With the second Xianyu account, buy your test product (a low price is enough).
2. Watch **Orders**: the order should appear and turn to pending shipment/shipped.
3. Check three things: the buyer received the card text in the chat, the card inventory changed (for batch inventory), and the order status is what you expect for your auto-confirm setting.
4. Open **Online Chat** to see the conversation, and **Runtime Logs** (administrator) if something looks wrong.

Only when this works should you link the same kind of rule to your real products. If the card was not sent, go through the checklist in the [FAQ](FAQ).

## Step 9: Optional upgrades, in a sensible order

| Goal | Where | Page |
| --- | --- | --- |
| Reply to common questions automatically | Automation → Keyword Replies / Default Reply | [Automated Delivery and Replies](Automated-Delivery-and-Replies) |
| Let AI handle bargaining | System and AI (administrator) then Account → AI Settings | [System and AI](System-and-AI) |
| Automatic reviews and daily refresh | Account → Automatic Reviews and Daily Refresh | [Chat and Account Automation](Chat-and-Account-Automation) |
| Publish many products from a spreadsheet | Bulk Listing / Resource Library | [Bulk Listing](Bulk-Listing), [Resource Library](Resource-Library) |
| Let your own program call the console | API Management | [Admin API](Admin-API) |
| See revenue and trends | Dashboard, Sales Statistics | [Dashboard](Dashboard), [Sales Statistics](Sales-Statistics) |

## Daily routine

- Glance at the **Dashboard** and the account status (online, enabled, no risk-control banner).
- Keep **card inventory** stocked; when it is empty, delivery cannot happen and you are notified.
- Handle any “manual handling required” notification from **Orders** instead of waiting.
- Back up the database and `XIANYU_DATA_KEY` regularly.
- Do not run many bulk operations at once: the program deliberately spaces out its Xianyu requests to avoid risk control (see [Request Pacing and Risk Control](Request-Pacing-and-Risk-Control)).

## Using it on a phone

The console adapts to small screens. The bottom bar offers Dashboard, Chat, Accounts, Orders and More; More opens a drawer with Resource Library, Card Inventory, Product List, Sales Statistics, Delivery Templates, Automation, Notification Settings and API Management, plus System and AI and Runtime Logs for administrators, the theme button (Follow system / Light / Dark) and the version. Online Chat shows one pane at a time: the conversation list first, then the messages after you tap a conversation (go back to return to the list). Orders are shown as one card per order. When the server is not on your local network, put it behind HTTPS before using it from a phone.

## Common first-day problems

| Symptom | What to check |
| --- | --- |
| Console page does not open | Service running? Correct address and port 59188? Firewall? See [Deployment and Database](Deployment-and-Database). |
| QR login fails or the account goes offline | Re-scan from Account Management → reauthorize; for security verification use the official Xianyu app. |
| Card was not sent after payment | Account online and enabled? Auto delivery on? Rule enabled and linked to the right product/specification? Card group enabled with stock? See [FAQ](FAQ). |
| Order stays “pending shipment” | Auto confirm shipping is off, or the platform rejected it; use Orders → Deliver Now. |
| “Risk control” or `RGV587` in logs | Stop bulk actions and follow [Risk-Control Troubleshooting](Risk-Control-Troubleshooting). |

## Words you will see

- **Card group (卡密)**: the content that is delivered — text, a list of codes, an image or an API call.
- **Delivery template (发货模板)**: an ordered list of messages and images sent as one delivery.
- **Trade automation (交易自动化)**: rules for unpaid price change, paid delivery, review gift and review reminder.
- **Bargain / 小刀**: a two-person bargain order with a fixed order of steps.
- **Skip-group (免拼)**: ask Xianyu to complete a group-buy order without waiting for more buyers.
- **Takeover**: when you reply to a buyer yourself, automatic replies pause for a while.
